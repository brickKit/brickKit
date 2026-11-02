package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 本文件是 podman.units: quadlet 的引擎：把单元装进 systemd 的 Quadlet 目录，由 systemd 启动、开机拉起。
// 不经 podman.socket，也不要 docker-compose：只用 systemctl 与 podman 命令。
//
// # 部署了什么，以安装目录为准
//
// 每个项目一个子目录 <Quadlet 目录>/<引擎侧项目名>/（Quadlet 递归扫描子目录）。down 与 status 只认这个目录：
// 它就是“上次实际部署了什么”，与 compose 从容器标签认项目同一个道理——项目里生成的那份（.brickkit/generated/quadlet/）
// 回答的是“这次打算跑什么”，up --dry-run 也会重写它。

// QuadletMinVersion 是 podman.units: quadlet 要的最低 Podman 版本：Notify=healthy 与模板单元 5.0、
// NetworkAlias= 5.2、无根单元自动等网络（podman-user-wait-network-online.service）5.3（Podman 的发布说明）。
const QuadletMinVersion = "5.3"

// UnitFile 是一个要装进 Quadlet 目录的文件；LinkTo 非空时是指向同目录里那个模板的链接（副本的实例）。
type UnitFile struct {
	Name    string
	Content []byte
	LinkTo  string
}

// Secret 是一个要建（或替换）的 Podman secret。
type Secret struct {
	Name  string
	Value []byte
}

// QuadletUnits 是一次 Quadlet 部署：UpRequest.Quadlet。
type QuadletUnits struct {
	// Files 是全部单元文件；安装目录里多出来的会先停掉再删。
	Files []UnitFile
	// Secrets 是组件单元引用的 Podman secret，MigrationSecrets 是迁移单元引用的；本项目多出来的在最后删掉。
	Secrets          []Secret
	MigrationSecrets []Secret
	// Migrations 是迁移单元：先单独跑完它们，全部成功才装组件的单元、换组件的 secret（见 Up）。
	Migrations []string
	// Components 是组件单元：内容变了的重启，其余启动（已经在跑的不动）。
	Components []string
	// Images 是单元用到的镜像：缺的先拉下来，拉镜像不算进单元的启动时限。
	Images []string
}

// Quadlet 是 podman.units: quadlet 的引擎。
type Quadlet struct {
	// rootful：以 root 运行时单元装进系统的 systemd（/etc/containers/systemd），否则装进用户的。
	rootful bool
	// root 是 Quadlet 目录；每个项目在它下面一个子目录。
	root string
	// runner 执行命令，stdin 非空时写给它（podman secret create 从标准输入读值，值不落盘）。测试可替换。
	runner func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
	// otherRoots 是另一边的 systemd 可能装着同一个项目的 Quadlet 目录（见 Elsewhere）。测试可替换。
	otherRoots func(projectDir string) []installRoot
}

// installRoot 是一个 Quadlet 目录与它所属的用户（系统的 systemd 时为空）。
type installRoot struct {
	dir  string
	user string
}

// systemQuadletDir 是系统的 systemd 找 Quadlet 单元的目录。
const systemQuadletDir = "/etc/containers/systemd"

// userQuadletDir 是一个用户的 systemd 找 Quadlet 单元的目录之一（podman-systemd.unit(5)）。不跟 $XDG_CONFIG_HOME：
// snap 里的终端会把它改到别处，而用户的 systemd 读的是它自己的环境。
func userQuadletDir(home string) string { return filepath.Join(home, ".config", "containers", "systemd") }

// NewQuadlet 按当前用户选系统级或用户级的 systemd。
func NewQuadlet() *Quadlet {
	q := &Quadlet{rootful: os.Geteuid() == 0, runner: runWithStdin, otherRoots: otherInstallRoots}
	if q.rootful {
		q.root = systemQuadletDir
	} else {
		home, _ := os.UserHomeDir()
		q.root = userQuadletDir(home)
	}
	return q
}

// otherInstallRoots 是“另一边”：以普通用户运行时是系统的 systemd；以 root 运行时是可能部署过这个项目的用户的——
// sudo 之前的那个用户（SUDO_USER），与项目目录的属主（su 或 ssh 成 root 时没有 SUDO_USER）。别的用户不查：
// 他们的同名项目是另一个项目，root 部署它不会与他们的单元撞名（两个 systemd 各有各的名字空间）。
func otherInstallRoots(projectDir string) []installRoot {
	if os.Geteuid() != 0 {
		return []installRoot{{dir: systemQuadletDir}}
	}
	var names []string
	if name := os.Getenv("SUDO_USER"); name != "" {
		names = append(names, name)
	}
	if uid, ok := fileOwner(projectDir); ok {
		if u, err := user.LookupId(uid); err == nil {
			names = append(names, u.Username)
		}
	}
	var roots []installRoot
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		u, err := user.Lookup(name)
		if err != nil || u.Uid == "0" {
			continue
		}
		roots = append(roots, installRoot{dir: userQuadletDir(u.HomeDir), user: name})
	}
	return roots
}

// elsewhere 找另一边装着这个项目（安装目录里有单元）的那一处。
func (q *Quadlet) elsewhere(project, projectDir string) (installRoot, bool) {
	if q.otherRoots == nil {
		return installRoot{}, false
	}
	for _, root := range q.otherRoots(projectDir) {
		dir := filepath.Join(root.dir, project)
		if files, err := q.installed(dir); err == nil && len(files) > 0 {
			return installRoot{dir: dir, user: root.user}, true
		}
	}
	return installRoot{}, false
}

// CheckElsewhere 在这个项目已经以 Quadlet 单元装在另一边的 systemd 里时报 DEPLOYED_ELSEWHERE：root 的 systemd 与
// 用户的 systemd 各管各的单元，这次再部署一份，就是同一个项目在一台机器上跑两份——连同一批数据库，消费者成双、
// 迁移两边都跑；而 status 与 down 只看得到当前身份那一份（FRB-012：root 装好之后，普通用户在同一个目录 up，
// 静默装出了第二份）。up 在动任何东西之前调用，compose 的 up 也是：另一边的单元同样会与它的容器跑成两份。
func (q *Quadlet) CheckElsewhere(project, projectDir string) error {
	root, found := q.elsewhere(project, projectDir)
	if !found {
		return nil
	}
	return clierr.New(clierr.CodeDeployedElsewhere, i18n.T(msgid.EngineQuadletElsewhere, root.dir)).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.EngineQuadletElsewhereReason)).
		WithHint(elsewhereWho(root), i18n.T(msgid.EngineHintQuadletElsewhereMove))
}

// ElsewhereWarning 是 CheckElsewhere 的警告版，给 down 与 status：它们照样做自己这一边，再说出另一边还有一份。
func (q *Quadlet) ElsewhereWarning(project, projectDir string) *clierr.Error {
	root, found := q.elsewhere(project, projectDir)
	if !found {
		return nil
	}
	return clierr.New(clierr.CodeDeployedElsewhere, i18n.T(msgid.EngineQuadletElsewhereWarning, root.dir)).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.EngineQuadletElsewhereReason)).
		WithHint(elsewhereWho(root), i18n.T(msgid.EngineHintQuadletElsewhereMove)).
		AsWarning()
}

// elsewhereWho 说该用哪个身份运行：部署它的 root，或部署它的那个用户。
func elsewhereWho(root installRoot) string {
	if root.user != "" {
		return i18n.T(msgid.EngineHintQuadletElsewhereAsUser, root.user)
	}
	return i18n.T(msgid.EngineHintQuadletElsewhereAsRoot)
}

// Rootful 报告单元装进的是系统的 systemd。
func (q *Quadlet) Rootful() bool { return q.rootful }

// Name 是引擎名：容器仍由 Podman 运行。
func (q *Quadlet) Name() string { return Podman }

// Dir 是一个项目的安装目录。
func (q *Quadlet) Dir(project string) string { return filepath.Join(q.root, project) }

// JournalCommand 是看一个单元日志的命令。
func (q *Quadlet) JournalCommand(unit string) string {
	if q.rootful {
		return "journalctl -u " + unit
	}
	return "journalctl --user -u " + unit
}

// pendingFile 是安装目录里记着“单元已经换了、容器还没按它重建”的文件（一行一个单元）。
//
// 单元一写进去就是新的，下一次 up 拿磁盘上的文件比，就看不出它变过：上一次 up 写完组件的单元、重启却失败了
// （或者被 Ctrl+C 打断），再 up，组件会照旧跑着旧配置、up 还报成功。所以变了的单元先记在这里，重启成功才划掉。
// 以点开头：Quadlet 只认它认得的后缀，install 与 installed 也跳过它。
const pendingFile = ".brickkit-pending-restart"

// Up 装单元、建 secret、跑迁移、启动组件。
//
// 分两步装，为了“迁移失败时什么都不动”——不只是这一刻，也包括之后的每一次开机与崩溃重启：
//
//  1. 网络与迁移的单元、迁移的 secret，daemon-reload，跑全部迁移。
//  2. 迁移全部成功了，才写组件的单元、换组件的 secret，卸下另一种形式与不再生成的单元，再重启、启动组件。
//
// 组件的单元一写进 Quadlet 目录、daemon-reload 之后就是“开机要起的那个”，它的 secret 一换，下一次崩溃重启
// 拿到的就是新值。要是迁移失败之前就换了，up 报了失败、旧版本照跑，机器一重启起来的却是新版本（升级时
// 服务名带着版本号，新旧两份还会一起起来），面对的是没迁移过的库。迁移不在开机时跑（quadlet 包的 waits），
// 所以“开机时组件的单元就是上一次成功的 up 装的”这一点只能由这里保证。
func (q *Quadlet) Up(ctx context.Context, req UpRequest) error {
	units := req.Quadlet
	if units == nil {
		return errors.New("engine: Quadlet.Up without units")
	}
	if err := q.CheckElsewhere(req.Project, req.ProjectDir); err != nil {
		return err
	}
	if err := q.checkVersion(ctx); err != nil {
		return err
	}
	if req.Network != "" {
		if _, err := q.podman(ctx, nil, "network", "exists", req.Network); err != nil {
			return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.EngineNetworkMissing, req.Network)).
				WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.EngineNetworkMissingReason)).
				WithHint(i18n.T(msgid.EngineHintCreateNetwork, "podman network create "+req.Network)).
				WithCause(err)
		}
	}
	for _, image := range units.Images {
		if _, err := q.podman(ctx, nil, "image", "exists", image); err == nil {
			continue
		}
		if _, err := q.podman(ctx, nil, "pull", image); err != nil {
			return err
		}
	}

	// 第一步：网络与迁移
	dir := q.Dir(req.Project)
	first, components := splitFiles(units)
	if err := q.createSecrets(ctx, units.MigrationSecrets); err != nil {
		return err
	}
	if _, err := q.write(dir, first); err != nil {
		return err
	}
	if _, err := q.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	var networks []string
	for _, f := range first {
		if strings.HasSuffix(f.Name, ".network") {
			networks = append(networks, UnitOf(f.Name))
		}
	}
	if len(networks) > 0 {
		if _, err := q.systemctl(ctx, append([]string{"start"}, networks...)...); err != nil {
			return err
		}
	}
	if len(units.Migrations) > 0 {
		if _, err := q.systemctl(ctx, append([]string{"restart"}, units.Migrations...)...); err != nil {
			return q.unitsFailed(ctx, units.Migrations, true, err)
		}
	}

	// 第二步：迁移都成功了，这才换组件的 secret、装组件的单元，卸下另一种形式与不再生成的单元，再动组件
	if err := q.createSecrets(ctx, units.Secrets); err != nil {
		return err
	}
	changed, err := q.installComponents(dir, components)
	if err != nil {
		return err
	}
	if _, err := q.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err := q.removeComposeContainers(ctx, req.Project, req.OnPrune); err != nil {
		return err
	}
	stale, err := q.stale(dir, units.Files)
	if err != nil {
		return err
	}
	if err := q.removeStale(ctx, dir, stale, req.OnPrune); err != nil {
		return err
	}
	var restart []string
	for _, unit := range units.Components {
		if changed[unit] {
			restart = append(restart, unit)
		}
	}
	if len(restart) > 0 {
		if _, err := q.systemctl(ctx, append([]string{"restart"}, restart...)...); err != nil {
			return q.unitsFailed(ctx, units.Components, false, err)
		}
	}
	if len(units.Components) > 0 {
		if _, err := q.systemctl(ctx, append([]string{"start"}, units.Components...)...); err != nil {
			return q.unitsFailed(ctx, units.Components, false, err)
		}
	}
	if err := os.Remove(filepath.Join(dir, pendingFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return installError(dir, err)
	}
	return q.pruneSecrets(ctx, req.Project, append(append([]Secret{}, units.MigrationSecrets...), units.Secrets...))
}

// splitFiles 把单元文件分成 Up 的两步：网络与迁移的，和组件的（含副本的模板与实例）。
func splitFiles(units *QuadletUnits) (first, components []UnitFile) {
	migrations := map[string]bool{}
	for _, unit := range units.Migrations {
		migrations[unit] = true
	}
	for _, f := range units.Files {
		if strings.HasSuffix(f.Name, ".network") || migrations[UnitOf(f.Name)] {
			first = append(first, f)
			continue
		}
		components = append(components, f)
	}
	return first, components
}

func (q *Quadlet) createSecrets(ctx context.Context, secrets []Secret) error {
	for _, s := range secrets {
		if _, err := q.podman(ctx, s.Value, "secret", "create", "--replace", s.Name, "-"); err != nil {
			return err
		}
	}
	return nil
}

// write 把单元写进安装目录（不删、不停任何东西），返回内容变了的单元。副本的实例是指向模板的链接；模板变了，
// 它的每个实例都算变了。
func (q *Quadlet) write(dir string, files []UnitFile) (map[string]bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, installError(dir, err)
	}
	changed := map[string]bool{}
	templateChanged := map[string]bool{}
	for _, f := range files {
		if f.LinkTo != "" {
			continue
		}
		path := filepath.Join(dir, f.Name)
		if old, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(old, f.Content) {
			continue
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			return nil, installError(dir, err)
		}
		if strings.HasSuffix(f.Name, "@.container") {
			templateChanged[f.Name] = true
			continue
		}
		changed[UnitOf(f.Name)] = true
	}
	for _, f := range files {
		if f.LinkTo == "" {
			continue
		}
		path := filepath.Join(dir, f.Name)
		if target, readErr := os.Readlink(path); readErr == nil && target == f.LinkTo {
			if templateChanged[f.LinkTo] {
				changed[UnitOf(f.Name)] = true
			}
			continue
		}
		_ = os.Remove(path)
		if err := os.Symlink(f.LinkTo, path); err != nil {
			return nil, installError(dir, err)
		}
		changed[UnitOf(f.Name)] = true
	}
	return changed, nil
}

// installComponents 写组件的单元，返回要重启的——这次内容变了的，加上之前记着还没重启的（pendingFile）。
// 要重启的这一份同时写回 pendingFile，重启成功之后 Up 才把它删掉。
func (q *Quadlet) installComponents(dir string, files []UnitFile) (map[string]bool, error) {
	changed, err := q.write(dir, files)
	if err != nil {
		return nil, err
	}
	if pending, err := os.ReadFile(filepath.Join(dir, pendingFile)); err == nil {
		for _, unit := range strings.Fields(string(pending)) {
			changed[unit] = true
		}
	}
	var pending []string
	for unit := range changed {
		pending = append(pending, unit)
	}
	sort.Strings(pending)
	if err := os.WriteFile(filepath.Join(dir, pendingFile), []byte(strings.Join(pending, "\n")+"\n"), 0o644); err != nil {
		return nil, installError(dir, err)
	}
	return changed, nil
}

// stale 是安装目录里不再生成的文件。
func (q *Quadlet) stale(dir string, files []UnitFile) ([]string, error) {
	present, err := q.installed(dir)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, f := range files {
		wanted[f.Name] = true
	}
	var stale []string
	for _, name := range present {
		if !wanted[name] {
			stale = append(stale, name)
		}
	}
	return stale, nil
}

// removeStale 停掉并删掉不再生成的单元（删之前先停：文件没了 systemd 就不认得它，容器会一直跑着没人管），
// 经 onPrune 报出。
func (q *Quadlet) removeStale(ctx context.Context, dir string, stale []string, onPrune func(string)) error {
	if len(stale) == 0 {
		return nil
	}
	var stop []string
	for _, name := range stale {
		if unit := UnitOf(name); unit != "" {
			stop = append(stop, unit)
		}
	}
	if err := q.stop(ctx, stop); err != nil {
		return err
	}
	for _, name := range stale {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return installError(dir, err)
		}
		if unit := UnitOf(name); unit != "" && onPrune != nil {
			onPrune(unit)
		}
	}
	_, err := q.systemctl(ctx, "daemon-reload")
	return err
}

// stop 停掉 systemd 加载了的那些单元。一个没加载的（Quadlet 的生成器拒绝了它的文件，或者 up 在写文件与
// daemon-reload 之间被打断）stop 会以 5 退出（"Unit … not loaded"，实测）：它也没有在跑的东西，跳过，
// 否则 up 与 down 都会被它永远卡住。
func (q *Quadlet) stop(ctx context.Context, units []string) error {
	if len(units) == 0 {
		return nil
	}
	out, _ := q.systemctlRaw(ctx, append([]string{"show", "--property=LoadState", "--value"}, units...)...)
	states := strings.Fields(string(out))
	var loaded []string
	for i, unit := range units {
		if i < len(states) && states[i] == "loaded" {
			loaded = append(loaded, unit)
		}
	}
	if len(loaded) == 0 {
		return nil
	}
	_, err := q.systemctl(ctx, append([]string{"stop"}, loaded...)...)
	return err
}

// UnitOf 是一个 Quadlet 文件生成的 systemd 服务名；模板（x@.container）自己不是一个服务，返回空。
//
//	x.container   → x.service
//	x@2.container → x@2.service
//	x.network     → x-network.service
func UnitOf(file string) string {
	switch {
	case strings.HasSuffix(file, "@.container"):
		return ""
	case strings.HasSuffix(file, ".container"):
		return strings.TrimSuffix(file, ".container") + ".service"
	case strings.HasSuffix(file, ".network"):
		return strings.TrimSuffix(file, ".network") + "-network.service"
	}
	return ""
}

// Down 停掉并卸下本项目的全部单元，删掉它的网络与 secret。不删数据卷（BrickKit 本来就不建卷）。
// 一步失败也把能做的做完（单元已经卸下时，secret 照样要删），最后报第一个错。
func (q *Quadlet) Down(ctx context.Context, req DownRequest) error {
	if err := q.removeComposeContainers(ctx, req.Project, nil); err != nil {
		return err
	}
	dir := q.Dir(req.Project)
	files, err := q.installed(dir)
	if err != nil {
		return err
	}
	var units, networks []string
	for _, name := range files {
		if unit := UnitOf(name); unit != "" {
			units = append(units, unit)
		}
		if strings.HasSuffix(name, ".network") {
			if network := networkNameIn(filepath.Join(dir, name)); network != "" {
				networks = append(networks, network)
			}
		}
	}
	if len(units) > 0 {
		if err := q.stop(ctx, units); err != nil {
			return err
		}
	}
	if len(files) > 0 {
		if err := os.RemoveAll(dir); err != nil {
			return installError(dir, err)
		}
		if _, err := q.systemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
		// 失败过的单元留在 systemd 的记录里，status 会一直报它
		_, _ = q.systemctlRaw(ctx, append([]string{"reset-failed"}, units...)...)
	}
	var first error
	if err := q.pruneSecrets(ctx, req.Project, nil); err != nil {
		first = err
	}
	// 停掉网络单元不删网络（2026-11-02 实测），要显式删
	for _, network := range networks {
		if _, err := q.podman(ctx, nil, "network", "exists", network); err != nil {
			continue
		}
		if _, err := q.podman(ctx, nil, "network", "rm", network); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// removeComposeContainers 删掉这个项目由 compose 建的容器（部署文件从 compose 改成了 podman.units: quadlet）：
// 两种都留着，Quadlet 的单元会与它们抢同一批端口。按 compose 写在容器上的项目标签认，直接用 podman，
// 不要 docker-compose——Quadlet 的机器上未必装了它。项目网络与 Quadlet 的同名，留给 Quadlet 接着用。
// 删掉的容器经 onPrune 报出。
func (q *Quadlet) removeComposeContainers(ctx context.Context, project string, onPrune func(string)) error {
	out, err := q.podman(ctx, nil, "ps", "-a", "--filter", "label=com.docker.compose.project="+project, "--format", "{{.Names}}")
	if err != nil {
		return err
	}
	names := strings.Fields(string(out))
	if len(names) == 0 {
		return nil
	}
	if _, err := q.podman(ctx, nil, append([]string{"rm", "-f"}, names...)...); err != nil {
		return err
	}
	if onPrune != nil {
		for _, name := range names {
			onPrune(name)
		}
	}
	return nil
}

// networkNameIn 读 .network 单元里的 NetworkName=。
func networkNameIn(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "NetworkName="); ok {
			return name
		}
	}
	return ""
}

// installed 列出安装目录里的文件；目录不存在时为空。
func (q *Quadlet) installed(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, installError(dir, err)
	}
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// pruneSecrets 删掉本项目不再用的 secret（keep 之外的、名字以 <引擎侧项目名>_ 开头的：项目名里没有 _，
// 所以 shop 删不到 shop-x 的）。
func (q *Quadlet) pruneSecrets(ctx context.Context, project string, keep []Secret) error {
	out, err := q.podman(ctx, nil, "secret", "ls", "--format", "{{.Name}}")
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, s := range keep {
		wanted[s.Name] = true
	}
	var stale []string
	for _, name := range strings.Fields(string(out)) {
		if strings.HasPrefix(name, project+"_") && !wanted[name] {
			stale = append(stale, name)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	_, err = q.podman(ctx, nil, append([]string{"secret", "rm"}, stale...)...)
	return err
}

// Status 返回本项目每个服务的状态：容器的（按 Quadlet 给容器打的 PODMAN_SYSTEMD_UNIT 标签认本项目的单元），
// 加上失败了的单元——迁移的容器跑完就删，失败只在 systemd 那里看得到。
func (q *Quadlet) Status(ctx context.Context, project string) ([]Status, error) {
	files, err := q.installed(q.Dir(project))
	if err != nil || len(files) == 0 {
		return nil, err
	}
	var units []string
	for _, name := range files {
		if unit := UnitOf(name); unit != "" && !strings.HasSuffix(name, ".network") {
			units = append(units, unit)
		}
	}
	if len(units) == 0 {
		return nil, nil
	}
	// 只按“带这个标签”过滤，单元在代码里筛：Podman 的多个 label 过滤条件要同时满足，不是任意一个（实测 5.7.0）
	out, err := q.podman(ctx, nil, "ps", "-a", "--format", "json", "--filter", "label=PODMAN_SYSTEMD_UNIT")
	if err != nil {
		return nil, err
	}
	statuses, err := parsePodmanPS(out, units)
	if err != nil {
		return nil, err
	}
	failed, err := q.failedUnits(ctx, units)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range statuses {
		seen[s.Service] = true
	}
	for _, unit := range failed {
		service := serviceOfUnit(project, unit)
		if !seen[service] {
			statuses = append(statuses, Status{Service: service, State: "failed", Replicas: 1})
		}
	}
	return statuses, nil
}

// serviceOfUnit 从单元名取回服务名：brickkit-<项目>_<服务>[@n].service。
func serviceOfUnit(project, unit string) string {
	service := strings.TrimSuffix(strings.TrimPrefix(unit, project+"_"), ".service")
	if at := strings.Index(service, "@"); at >= 0 {
		service = service[:at]
	}
	return service
}

// failedUnits 是 units 里 systemd 记为 failed 的。
func (q *Quadlet) failedUnits(ctx context.Context, units []string) ([]string, error) {
	// is-failed 对每个单元打印一行状态，有任何一个不是 failed 就以非 0 退出：只看输出
	out, _ := q.systemctlRaw(ctx, append([]string{"is-failed"}, units...)...)
	states := strings.Fields(string(out))
	var failed []string
	for i, state := range states {
		if i < len(units) && state == "failed" {
			failed = append(failed, units[i])
		}
	}
	return failed, nil
}

// podmanPSEntry 是 `podman ps --format json` 的一条记录（只取用到的字段）。
type podmanPSEntry struct {
	State    string            `json:"State"`
	Status   string            `json:"Status"`
	ExitCode int               `json:"ExitCode"`
	Labels   map[string]string `json:"Labels"`
	Ports    []struct {
		HostIP        string `json:"host_ip"`
		HostPort      int    `json:"host_port"`
		ContainerPort int    `json:"container_port"`
		Protocol      string `json:"protocol"`
	} `json:"Ports"`
}

// parsePodmanPS 取 units 里那些单元的容器（PODMAN_SYSTEMD_UNIT 标签），按 brickkit.io/service 标签归到服务，
// 副本合并成一条（与 compose 同一种取最差的规则）。
// podman ps 的 JSON 没有单独的健康字段，健康写在 Status 的括号里（"Up 3 minutes (healthy)"）。
func parsePodmanPS(out []byte, units []string) ([]Status, error) {
	ours := map[string]bool{}
	for _, unit := range units {
		ours[unit] = true
	}
	var entries []podmanPSEntry
	if text := bytes.TrimSpace(out); len(text) > 0 {
		if err := json.Unmarshal(text, &entries); err != nil {
			return nil, statusParseError(err)
		}
	}
	var statuses []Status
	index := map[string]int{}
	for _, e := range entries {
		service := e.Labels["brickkit.io/service"]
		if service == "" || !ours[e.Labels["PODMAN_SYSTEMD_UNIT"]] {
			continue
		}
		var ports []string
		for _, p := range e.Ports {
			if p.HostPort > 0 {
				hostIP := p.HostIP
				if hostIP == "" {
					hostIP = "0.0.0.0" // 与 compose 的写法一致：没指定就是所有地址
				}
				ports = append(ports, fmt.Sprintf("%s:%d->%d/%s", hostIP, p.HostPort, p.ContainerPort, p.Protocol))
			}
		}
		replica := Status{
			Service: service, State: e.State, Health: healthIn(e.Status), Ports: strings.Join(ports, ", "),
			ExitCode: e.ExitCode, Replicas: 1,
		}
		if replica.Running() {
			replica.Ready = 1
		}
		i, seen := index[service]
		if !seen {
			index[service] = len(statuses)
			statuses = append(statuses, replica)
			continue
		}
		statuses[i] = mergeReplica(statuses[i], replica)
	}
	return statuses, nil
}

// healthIn 取 podman ps 的 Status 里括号中的健康状态。
func healthIn(status string) string {
	for _, health := range []string{"healthy", "unhealthy", "starting"} {
		if strings.HasSuffix(status, "("+health+")") {
			return health
		}
	}
	return ""
}

// CheckImage 与 compose 下的 Podman 相同：先看本地，再问 registry。
func (q *Quadlet) CheckImage(ctx context.Context, image string) error {
	if _, err := q.podman(ctx, nil, "image", "exists", image); err == nil {
		return nil
	}
	out, err := q.podman(ctx, nil, "manifest", "inspect", image)
	if err == nil {
		return nil
	}
	return imageError("podman", image, string(out), err)
}

// CurrentContext 没有意义（只有 K8s 有部署上下文）。
func (q *Quadlet) CurrentContext(context.Context) (string, error) { return "", nil }

// checkVersion 拦下太旧的 Podman：它们读不懂单元里的键，单元会以一句 Quadlet 的语法错误失败。
func (q *Quadlet) checkVersion(ctx context.Context) error {
	out, err := q.podman(ctx, nil, "version", "--format", "{{.Client.Version}}")
	if err != nil {
		return err
	}
	version := strings.TrimSpace(string(out))
	if versionAtLeast(version, QuadletMinVersion) {
		return nil
	}
	return clierr.New(clierr.CodeEngineMissing, i18n.T(msgid.EngineQuadletPodmanTooOld, version, QuadletMinVersion)).
		WithHint(i18n.T(msgid.EngineHintQuadletUpgradeOrCompose))
}

// versionAtLeast 比较 主版本.次版本；修订号与预发布标记（5.3.0-rc1）不影响这里的判断。
func versionAtLeast(version, minimum string) bool {
	parse := func(v string) (major, minor int) {
		parts := strings.SplitN(v, ".", 3)
		major, _ = strconv.Atoi(parts[0])
		if len(parts) > 1 {
			digits := strings.TrimRightFunc(parts[1], func(r rune) bool { return r < '0' || r > '9' })
			minor, _ = strconv.Atoi(digits)
		}
		return major, minor
	}
	major, minor := parse(version)
	wantMajor, wantMinor := parse(minimum)
	return major > wantMajor || major == wantMajor && minor >= wantMinor
}

// unitsFailed 在 systemctl 失败之后问一次哪些单元失败了，说清是哪个、日志怎么看。迁移失败报 MIGRATION_FAILED：
// 这时组件一个都没动，旧版本照跑。
func (q *Quadlet) unitsFailed(ctx context.Context, units []string, migration bool, cause error) error {
	failed, _ := q.failedUnits(ctx, units)
	if len(failed) == 0 {
		return cause
	}
	var e *clierr.Error
	if migration {
		e = clierr.New(clierr.CodeMigrationFailed, i18n.T(msgid.EngineMigrationFailed)).
			WithHint(i18n.T(msgid.EngineHintQuadletNothingRestarted), i18n.T(msgid.EngineHintQuadletFixAndRerun))
	} else {
		e = clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.EngineQuadletUnitsFailed)).
			WithHint(i18n.T(msgid.EngineHintQuadletReadJournal))
	}
	for _, unit := range failed {
		e = e.WithDetail(i18n.T(msgid.EngineLabelUnit), unit).
			WithDetail(i18n.T(msgid.EngineLabelLogs), q.JournalCommand(unit))
	}
	return e.WithCause(cause)
}

func (q *Quadlet) systemctl(ctx context.Context, args ...string) ([]byte, error) {
	out, err := q.systemctlRaw(ctx, args...)
	if err != nil {
		return out, q.failure("systemctl", q.systemctlArgs(args), out, err)
	}
	return out, nil
}

func (q *Quadlet) systemctlRaw(ctx context.Context, args ...string) ([]byte, error) {
	return q.runner(ctx, nil, "systemctl", q.systemctlArgs(args)...)
}

func (q *Quadlet) systemctlArgs(args []string) []string {
	if q.rootful {
		return args
	}
	return append([]string{"--user"}, args...)
}

func (q *Quadlet) podman(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	out, err := q.runner(ctx, stdin, "podman", args...)
	if err != nil {
		return out, q.failure("podman", args, out, err)
	}
	return out, nil
}

// failure 与 compose 的同名函数同一种说法：缺二进制、执行失败；Podman 的那几条主机环境提示照样给。
func (q *Quadlet) failure(bin string, args []string, out []byte, err error) error {
	c := &Compose{bin: bin}
	return c.failure(args, out, err, 3)
}

func installError(dir string, err error) error {
	return clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.EngineQuadletInstallFailed, dir)).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.EngineHintQuadletInstallPermissions)).
		WithCause(err)
}

// runWithStdin 与 run 相同，另把 stdin 写给命令。
func runWithStdin(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	if stdin == nil {
		return run(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return append(stderr.Bytes(), stdout.Bytes()...), err
	}
	return stdout.Bytes(), nil
}
