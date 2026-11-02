package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	// Secrets 是单元引用的全部 Podman secret；本项目多出来的在最后删掉。
	Secrets []Secret
	// Migrations 是迁移单元：先单独跑完它们，全部成功才动组件——restart 是先停再启，
	// 迁移失败时旧版本要还在跑。
	Migrations []string
	// Components 是组件单元：内容变了的重启，其余启动（已经在跑的不动）。
	Components []string
}

// Quadlet 是 podman.units: quadlet 的引擎。
type Quadlet struct {
	// rootful：以 root 运行时单元装进系统的 systemd（/etc/containers/systemd），否则装进用户的。
	rootful bool
	// root 是 Quadlet 目录；每个项目在它下面一个子目录。
	root string
	// runner 执行命令，stdin 非空时写给它（podman secret create 从标准输入读值，值不落盘）。测试可替换。
	runner func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
}

// NewQuadlet 按当前用户选系统级或用户级的 systemd。
func NewQuadlet() *Quadlet {
	q := &Quadlet{rootful: os.Geteuid() == 0, runner: runWithStdin}
	if q.rootful {
		q.root = "/etc/containers/systemd"
	} else {
		home, _ := os.UserHomeDir()
		// Quadlet 给用户找单元的目录之一（podman-systemd.unit(5)）。不跟 $XDG_CONFIG_HOME：snap 里的终端会把它
		// 改到别处，而用户的 systemd 读的是它自己的环境
		q.root = filepath.Join(home, ".config", "containers", "systemd")
	}
	return q
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

// Up 装单元、建 secret、跑迁移、启动组件。
func (q *Quadlet) Up(ctx context.Context, req UpRequest) error {
	units := req.Quadlet
	if units == nil {
		return errors.New("engine: Quadlet.Up without units")
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
	for _, s := range units.Secrets {
		if _, err := q.podman(ctx, s.Value, "secret", "create", "--replace", s.Name, "-"); err != nil {
			return err
		}
	}

	dir := q.Dir(req.Project)
	changed, err := q.install(ctx, dir, units.Files, req.OnPrune)
	if err != nil {
		return err
	}
	if _, err := q.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}

	if len(units.Migrations) > 0 {
		if _, err := q.systemctl(ctx, append([]string{"start"}, units.Migrations...)...); err != nil {
			return q.unitsFailed(ctx, units.Migrations, true, err)
		}
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
	return q.pruneSecrets(ctx, req.Project, units.Secrets)
}

// install 把单元同步进安装目录，返回内容变了（含新装）的单元；删掉的单元经 onPrune 报出。
// 删之前先停：文件没了 systemd 就不认得它，容器会一直跑着没人管。
func (q *Quadlet) install(
	ctx context.Context, dir string, files []UnitFile, onPrune func(string),
) (map[string]bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, installError(dir, err)
	}
	wanted := map[string]bool{}
	changed := map[string]bool{}
	// 模板变了，它的每个实例都算变了
	templateChanged := map[string]bool{}
	for _, f := range files {
		wanted[f.Name] = true
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

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, installError(dir, err)
	}
	var stale []string
	for _, e := range entries {
		if !wanted[e.Name()] {
			stale = append(stale, e.Name())
		}
	}
	sort.Strings(stale)
	var stop []string
	for _, name := range stale {
		if unit := UnitOf(name); unit != "" {
			stop = append(stop, unit)
		}
	}
	if len(stop) > 0 {
		if _, err := q.systemctl(ctx, append([]string{"stop"}, stop...)...); err != nil {
			return nil, err
		}
	}
	for _, name := range stale {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return nil, installError(dir, err)
		}
		if unit := UnitOf(name); unit != "" && onPrune != nil {
			onPrune(unit)
		}
	}
	return changed, nil
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
func (q *Quadlet) Down(ctx context.Context, req DownRequest) error {
	dir := q.Dir(req.Project)
	files, err := q.installed(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	var stop []string
	var networks []string
	for _, name := range files {
		if unit := UnitOf(name); unit != "" {
			stop = append(stop, unit)
		}
		if strings.HasSuffix(name, ".network") {
			if name := networkNameIn(filepath.Join(dir, name)); name != "" {
				networks = append(networks, name)
			}
		}
	}
	// 迁移单元跑完是 inactive，stop 照样接受
	if len(stop) > 0 {
		if _, err := q.systemctl(ctx, append([]string{"stop"}, stop...)...); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return installError(dir, err)
	}
	if _, err := q.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	// 失败过的单元留在 systemd 的记录里，status 会一直报它
	_, _ = q.systemctl(ctx, append([]string{"reset-failed"}, stop...)...)
	// 停掉网络单元不删网络（2026-11-02 实测），要显式删
	for _, network := range networks {
		if _, err := q.podman(ctx, nil, "network", "exists", network); err == nil {
			if _, err := q.podman(ctx, nil, "network", "rm", network); err != nil {
				return err
			}
		}
	}
	return q.pruneSecrets(ctx, req.Project, nil)
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
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// pruneSecrets 删掉本项目不再用的 secret（keep 之外的、名字以 <项目>- 开头的）。
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
		if strings.HasPrefix(name, project+"-") && !wanted[name] {
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
	args := []string{"ps", "-a", "--format", "json"}
	for _, unit := range units {
		args = append(args, "--filter", "label=PODMAN_SYSTEMD_UNIT="+unit)
	}
	out, err := q.podman(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	statuses, err := parsePodmanPS(out)
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

// serviceOfUnit 从单元名取回服务名：brickkit-<项目>-<服务>[@n].service。
func serviceOfUnit(project, unit string) string {
	service := strings.TrimSuffix(strings.TrimPrefix(unit, project+"-"), ".service")
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

// parsePodmanPS 按 brickkit.io/service 标签把容器归到服务，副本合并成一条（与 compose 同一种取最差的规则）。
// podman ps 的 JSON 没有单独的健康字段，健康写在 Status 的括号里（"Up 3 minutes (healthy)"）。
func parsePodmanPS(out []byte) ([]Status, error) {
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
		if service == "" {
			continue
		}
		var ports []string
		for _, p := range e.Ports {
			if p.HostPort > 0 {
				ports = append(ports, fmt.Sprintf("%s:%d->%d/%s", p.HostIP, p.HostPort, p.ContainerPort, p.Protocol))
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
