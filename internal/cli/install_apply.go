package cli

// 本文件把 install.Plan 落到三份文件上（add / remove 共用）。
//
// # 要么全改，要么一个字节都不动
//
// 一次 add 要改 brickkit.yaml、一到两份部署文件、若干配置文件。任何一步写不进去、
// 或者改完之后项目装载不了（两份部署文件对不上、默认版本有两份配置……），全部还原：
// add / remove 绝不留下一个 `up` 读不了的项目。判据就是装载本身——project.Load 是
// 所有命令的入口，它说能读就是能读，不在这里另写一套规则。
//
// # 部署文件写哪几份
//
// deploy.yaml 永远写；deploy.local.yaml 存在就一起写——它是同一个人的完整副本（附录 A1），
// 自己刚 add 完、下一次 up 就因为本地文件缺条目而报错，没有道理。-f 用的其他部署文件
// （deploy.prod.yaml……）是别的环境，不替使用者决定，只在输出里点名。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/install"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// componentsKey 是 brickkit.yaml 与部署文件里组件列表的键。
const componentsKey = "components"

// applied 是一次落盘的结果（给输出用）。
type applied struct {
	// ConfigsWritten 是新生成的配置骨架（相对项目根）。
	ConfigsWritten []string
	// ConfigsArchived 是移进 config/.archive/ 的配置（原路径 → 归档路径，相对项目根）。
	ConfigsArchived [][2]string
	// ArchivedExisting 是这次生成骨架时，归档里已经躺着的同版本旧配置（相对项目根）。
	ArchivedExisting []string
	// DeployFiles 是改过的部署文件名。
	DeployFiles []string
	// OtherDeployFiles 是没有改的其他部署文件（-f 用的环境文件）。
	OtherDeployFiles []string
	// Project 是改完之后重新装载的项目。
	Project *project.Project
}

// fileBackup 记下一个文件改之前的样子：existed 为 false 表示原本没有，还原时删掉。
type fileBackup struct {
	path    string
	data    []byte
	existed bool
}

type applier struct {
	proj    *project.Project
	opts    *Options
	backups map[string]*fileBackup
	order   []string
	result  *applied
}

// applyPlan 把 plan 写进三份文件，varRefs 是配置骨架里改成 $var: 引用的键（按组件版本）。
func applyPlan(opts *Options, proj *project.Project, plan *install.Plan, varRefs map[install.ConfigRef]map[string]string) (*applied, error) {
	a := &applier{proj: proj, opts: opts, backups: map[string]*fileBackup{}, result: &applied{}}
	if err := a.apply(plan, varRefs); err != nil {
		a.rollback()
		return nil, err
	}
	reloaded, err := checkInstalled(proj.Layout)
	if err != nil {
		a.rollback()
		return nil, wouldBreakError(err)
	}
	a.result.Project = reloaded
	return a.result, nil
}

// wouldBreakError 说"改完装载不了、已经还原"，把装载失败的原因、明细与建议原样带上。
func wouldBreakError(cause error) error {
	inner := clierr.As(cause)
	e := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliInstallWouldBreakProject)).
		WithDetail(i18n.T(msgid.LabelReason), strings.TrimPrefix(strings.TrimSpace(inner.Message), "❌ "))
	for _, d := range inner.Details {
		e = e.WithDetail(d.Key, d.Value)
	}
	return e.WithHint(append(append([]string{}, inner.Hints...), i18n.T(msgid.CliInstallHintFixFirst))...).WithCause(cause)
}

// loadForInstall 装载 add / remove 的出发点：以团队的 deploy.yaml 为准（本地文件是它的副本，
// 按条目 id 跟着改），与 -f、本地模式开没开都无关。
func loadForInstall(opts *Options) (*project.Project, error) {
	return project.Load(opts.WorkDir, project.LoadOptions{NoLocal: true})
}

// checkInstalled 核对改完的项目：deploy.yaml 走一遍完整装载；deploy.local.yaml 存在时也按
// 本地角色解析并核对拓扑——两份都改了，两份都要读得了。
func checkInstalled(l project.Layout) (*project.Project, error) {
	p, err := project.Load(l.Root, project.LoadOptions{NoLocal: true})
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(l.DeployLocalPath()); err == nil {
		local, _, err := deployfile.ParseFile(l.DeployLocalPath(), deployfile.RoleLocal)
		if err != nil {
			return nil, err
		}
		if _, err := project.Assemble(l, p.Decl, local); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (a *applier) apply(plan *install.Plan, varRefs map[install.ConfigRef]map[string]string) error {
	if err := a.editDecl(plan); err != nil {
		return err
	}
	for _, path := range a.deployFiles() {
		if err := a.editDeploy(path, plan); err != nil {
			return err
		}
	}
	return a.editConfigs(plan, varRefs)
}

// deployFiles 是要改的部署文件：deploy.yaml，以及存在的 deploy.local.yaml。其余的只记名字。
func (a *applier) deployFiles() []string {
	l := a.proj.Layout
	files := []string{l.DeployPath()}
	if _, err := os.Stat(l.DeployLocalPath()); err == nil {
		files = append(files, l.DeployLocalPath())
	}
	matches, _ := filepath.Glob(filepath.Join(l.Root, "deploy.*.yaml"))
	sort.Strings(matches)
	for _, m := range matches {
		if m != l.DeployLocalPath() {
			a.result.OtherDeployFiles = append(a.result.OtherDeployFiles, filepath.Base(m))
		}
	}
	return files
}

func (a *applier) editDecl(plan *install.Plan) error {
	path := a.proj.Layout.DeclPath()
	if err := a.backup(path); err != nil {
		return err
	}
	e, err := yamlfile.OpenEdit(path)
	if err != nil {
		return err
	}
	for _, l := range plan.AddLines {
		fields := []yamlfile.Field{{Key: "id", Value: l.ID}, {Key: "version", Value: l.Version}}
		if l.Shell {
			fields = append(fields, yamlfile.Field{Key: "kind", Value: projfile.KindShell})
		}
		if len(l.RequiredBy) > 0 {
			fields = append(fields, yamlfile.Field{Key: "requiredBy", Value: l.RequiredBy})
		}
		e.AppendMapping(componentsKey, fields)
	}
	for _, l := range plan.SetRequiredBy {
		e.SetList(componentsKey, yamlfile.Selector{ID: l.ID, Version: l.Version}, "requiredBy", l.RequiredBy)
	}
	for _, r := range plan.RemoveLines {
		e.RemoveWhere(componentsKey, yamlfile.Selector{ID: r.ID, Version: r.Version})
	}
	return e.Save()
}

// editDeploy 改一份部署文件。条目按 id 找；本地文件里使用者挪动过的条目找不到就跳过——
// 改完的装载核对会说出两份文件哪里对不上。
func (a *applier) editDeploy(path string, plan *install.Plan) error {
	if err := a.backup(path); err != nil {
		return err
	}
	e, err := yamlfile.OpenEdit(path)
	if err != nil {
		return err
	}
	for _, shell := range plan.UnnestShells {
		e.Unnest(componentsKey, shell)
	}
	for _, entry := range plan.AddEntries {
		if entry.Under == "" {
			e.AppendEntry(componentsKey, entry.ID)
		} else {
			e.Nest(componentsKey, entry.Under, entry.ID)
		}
	}
	for _, entry := range plan.NestEntries {
		e.Nest(componentsKey, entry.Under, entry.ID)
	}
	// 先删再改名：转正时被移除的默认版本条目是裸 ID，兼容版本的条目改名后也是裸 ID——
	// 先改名的话，按 ID 找到的可能是刚改名的那一条（排在前面、或嵌在外壳下面）
	for _, id := range plan.RemoveEntries {
		e.RemoveEntry(componentsKey, id)
	}
	for _, r := range plan.RenameEntries {
		e.RenameID(componentsKey, r.From, r.To)
	}
	if err := e.Save(); err != nil {
		return err
	}
	a.result.DeployFiles = append(a.result.DeployFiles, filepath.Base(path))
	return nil
}

func (a *applier) editConfigs(plan *install.Plan, varRefs map[install.ConfigRef]map[string]string) error {
	l := a.proj.Layout
	for _, ref := range plan.ArchiveConfigs {
		from := filepath.Join(l.ConfigDir(), configFileName(ref))
		if _, err := os.Stat(from); err != nil {
			continue
		}
		// 归档名永远带版本号：无版本号文件归档之后，版本也不能丢（§7.7 恢复时要认出来）
		to := filepath.Join(l.ConfigArchiveDir(), configdir.FileName(ref.ID, ref.Version))
		if err := a.move(from, to); err != nil {
			return err
		}
		a.result.ConfigsArchived = append(a.result.ConfigsArchived, [2]string{a.rel(from), a.rel(to)})
	}
	for _, ref := range plan.RenameConfigs {
		from := filepath.Join(l.ConfigDir(), configdir.FileName(ref.ID, ref.Version))
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := a.move(from, filepath.Join(l.ConfigDir(), configdir.FileName(ref.ID, ""))); err != nil {
			return err
		}
	}
	for _, c := range plan.AddConfigs {
		ref := install.ConfigRef{ID: c.ID, Version: c.Version, Versioned: c.Versioned}
		path := filepath.Join(l.ConfigDir(), configFileName(ref))
		if _, err := os.Stat(path); err == nil {
			continue // 已有的配置文件是使用者的，绝不覆盖
		}
		if err := a.write(path, configdir.Skeleton(c.ID, c.Version, c.Schema, varRefs[ref])); err != nil {
			return err
		}
		a.result.ConfigsWritten = append(a.result.ConfigsWritten, a.rel(path))
		archived := filepath.Join(l.ConfigArchiveDir(), configdir.FileName(c.ID, c.Version))
		if _, err := os.Stat(archived); err == nil {
			a.result.ArchivedExisting = append(a.result.ArchivedExisting, a.rel(archived))
		}
	}
	return nil
}

// configFileName 是组件版本的配置文件名：默认版本用无版本号文件（附录 A5）。
func configFileName(ref install.ConfigRef) string {
	if ref.Versioned {
		return configdir.FileName(ref.ID, ref.Version)
	}
	return configdir.FileName(ref.ID, "")
}

// move 改名（归档、转正），两头都记备份以便还原。目标已存在是错误：不替使用者覆盖。
func (a *applier) move(from, to string) error {
	if _, err := os.Stat(to); err == nil && !strings.HasPrefix(to, a.proj.Layout.ConfigArchiveDir()) {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliInstallTargetExists, a.rel(to))).
			WithDetail(i18n.T(msgid.LabelFile), a.rel(from))
	}
	for _, p := range []string{from, to} {
		if err := a.backup(p); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return writeError(from, err)
	}
	if err := a.writeRaw(to, data); err != nil {
		return err
	}
	if err := os.Remove(from); err != nil {
		return writeError(from, err)
	}
	return nil
}

func (a *applier) write(path string, data []byte) error {
	if err := a.backup(path); err != nil {
		return err
	}
	return a.writeRaw(path, data)
}

func (a *applier) writeRaw(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return writeError(path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return writeError(path, err)
	}
	return nil
}

// backup 在第一次碰一个文件之前记下它的原样。
func (a *applier) backup(path string) error {
	if _, ok := a.backups[path]; ok {
		return nil
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		a.backups[path] = &fileBackup{path: path, data: data, existed: true}
	case errors.Is(err, fs.ErrNotExist):
		a.backups[path] = &fileBackup{path: path}
	default:
		return writeError(path, err)
	}
	a.order = append(a.order, path)
	return nil
}

// rollback 把碰过的文件全部还原：原本有的写回原样，原本没有的删掉。
func (a *applier) rollback() {
	for i := len(a.order) - 1; i >= 0; i-- {
		b := a.backups[a.order[i]]
		if b.existed {
			_ = os.MkdirAll(filepath.Dir(b.path), 0o755)
			_ = os.WriteFile(b.path, b.data, 0o644)
		} else {
			_ = os.Remove(b.path)
		}
	}
}

func (a *applier) rel(path string) string {
	if r, err := filepath.Rel(a.proj.Layout.Root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}

func writeError(path string, err error) error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerWriteFailed, filepath.Base(path))).
		WithDetail(i18n.T(msgid.LabelPath), path).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.ProblemHintCheckPermissions)).
		WithCause(err)
}
