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
	"github.com/brickkit/brickkit/internal/manifest"
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
	// Restored 是从 config/.archive/ 迁移回来的配置（§7.7）。
	Restored []restoreResult
	// DeployFiles 是改过的部署文件名。
	DeployFiles []string
	// OtherDeployFiles 是没有改的其他部署文件（-f 用的环境文件）。
	OtherDeployFiles []string
	// Migrations 是这次迁移过的配置（相对项目根的文件 → 迁移报告，按应用顺序）。
	Migrations []migrationResult
	// Project 是改完之后重新装载的项目。
	Project *project.Project
}

// restoreResult 是一份从归档恢复的配置：从哪个归档文件、迁移成了哪个文件。
type restoreResult struct {
	File, Archive string
	Report        configdir.MigrateReport
}

// migrationResult 是一份迁移过的配置文件。
type migrationResult struct {
	File   string
	Report configdir.MigrateReport
}

// applyOptions 是落盘时的可选行为。
type applyOptions struct {
	// varRefs 是配置骨架里改成 $var: 引用的键（按组件版本）。
	varRefs map[install.ConfigRef]map[string]string
	// choose 决定配置迁移里每一处冲突怎么处理；为空时一律写重复键（附录 A4 的非交互兜底）。
	choose func(install.ConfigMigration, configdir.Conflict) configdir.Choice
	// allowConflicts：改完的核对放行配置冲突块（upgrade 写出的冲突是给使用者的待办，不是这次
	// 改动出了错）；别的问题照样拦下。
	allowConflicts bool
}

// fileBackup 记下一个文件改之前的样子：existed 为 false 表示原本没有，还原时删掉。
type fileBackup struct {
	path    string
	data    []byte
	mode    os.FileMode
	existed bool
}

type applier struct {
	proj    *project.Project
	opts    *Options
	backups map[string]*fileBackup
	order   []string
	// createdDirs 是这次新建的目录（按创建顺序）：还原时删掉，config/ 这种空目录不留下
	createdDirs []string
	result      *applied
	options     applyOptions
	// wroteConflicts：这次写出了冲突块（两行重复键）
	wroteConflicts bool
}

// applyPlan 把 plan 写进三份文件，varRefs 是配置骨架里改成 $var: 引用的键（按组件版本）。
func applyPlan(opts *Options, proj *project.Project, plan *install.Plan, varRefs map[install.ConfigRef]map[string]string) (*applied, error) {
	return applyPlanWith(opts, proj, plan, applyOptions{varRefs: varRefs})
}

// applyPlanWith 同 applyPlan，带上可选行为。
func applyPlanWith(opts *Options, proj *project.Project, plan *install.Plan, ao applyOptions) (*applied, error) {
	a := &applier{proj: proj, opts: opts, backups: map[string]*fileBackup{}, result: &applied{}, options: ao}
	if err := a.apply(plan); err != nil {
		a.rollback()
		return nil, err
	}
	// 迁移（upgrade、从归档恢复）写出了冲突块：那是给使用者的待办，只有冲突块本身放行
	reloaded, err := checkInstalled(proj.Layout, ao.allowConflicts || a.wroteConflicts)
	if err != nil {
		a.rollback()
		return nil, wouldBreakError(err)
	}
	a.result.Project = reloaded
	// 项目 BRICKKIT.md 的组件表跟着三份文件走（§16.2.1）。三份文件此刻已经正确，
	// 文档写不进去不值得把它们还原：说一声，下一次成功的改动会把表补齐
	if _, err := project.WriteProjectDoc(proj.Layout, reloaded); err != nil {
		renderWarnings(opts, []*clierr.Error{clierr.Warn(clierr.CodeInternal, i18n.T(msgid.CliInstallProjectDocFailed, project.FileProjectDoc)).
			WithDetail(i18n.T(msgid.LabelReason), clierr.As(err).Message)})
	}
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
func checkInstalled(l project.Layout, allowConflicts bool) (*project.Project, error) {
	p, err := project.Load(l.Root, project.LoadOptions{NoLocal: true})
	var cliErr *clierr.Error
	if err != nil && allowConflicts && errors.As(err, &cliErr) && cliErr.Code == clierr.CodeConfigConflict {
		// 冲突块是故意写的：配置层只放行它，拓扑照样核对
		p, err = project.LoadTopology(l.Root, project.LoadOptions{NoLocal: true})
	}
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

func (a *applier) apply(plan *install.Plan) error {
	if err := a.editDecl(plan); err != nil {
		return err
	}
	for _, path := range a.deployFiles() {
		if err := a.editDeploy(path, plan); err != nil {
			return err
		}
	}
	return a.editConfigs(plan)
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
	// 先原地改版本号：留下的旧版本另起一行时，按 ID + 版本找不能撞上还没改的那一行
	for _, m := range plan.ChangeVersions {
		e.SetValue(componentsKey, yamlfile.Selector{ID: m.ID, Version: m.From}, "version", m.To)
	}
	// 组件在新版本里成了外壳（或不再是外壳）：kind 跟着改（附录 A11，kind 由 CLI 维护）
	for _, l := range plan.ChangeKinds {
		sel := yamlfile.Selector{ID: l.ID, Version: l.Version}
		if l.Shell {
			e.SetValue(componentsKey, sel, "kind", projfile.KindShell)
		} else {
			e.DeleteFieldWhere(componentsKey, sel, "kind")
		}
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
	// 先删再改名：转正时被移除的默认版本条目是裸 ID，兼容版本的条目改名后也是裸 ID——
	// 先改名的话，按 ID 找到的可能是刚改名的那一条（排在前面、或嵌在外壳下面）
	for _, id := range plan.RemoveEntries {
		e.RemoveEntry(componentsKey, id)
	}
	// 改名按计划的顺序（先降级再升级），同一时刻不会有两个裸条目
	for _, r := range plan.RenameEntries {
		e.RenameID(componentsKey, r.From, r.To)
	}
	for _, id := range plan.LiftEntries {
		e.Lift(componentsKey, id)
	}
	// 新条目最后加：它可能正是刚被改名腾出来的那个裸 ID
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
	if err := e.Save(); err != nil {
		return err
	}
	a.result.DeployFiles = append(a.result.DeployFiles, filepath.Base(path))
	return nil
}

func (a *applier) editConfigs(plan *install.Plan) error {
	l := a.proj.Layout
	// 迁移的来源在任何移动之前读：旧文件马上会被归档或改名
	sources := make([][]byte, len(plan.MigrateConfigs))
	for i, m := range plan.MigrateConfigs {
		data, err := readOptional(filepath.Join(l.ConfigDir(), configFileName(m.Source)))
		if err != nil {
			return err
		}
		sources[i] = data
	}
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
	for _, ref := range plan.DemoteConfigs {
		from := filepath.Join(l.ConfigDir(), configdir.FileName(ref.ID, ""))
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := a.move(from, filepath.Join(l.ConfigDir(), configdir.FileName(ref.ID, ref.Version))); err != nil {
			return err
		}
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
	for i, m := range plan.MigrateConfigs {
		out, report, err := migrateConfig(m, sources[i], a.options.choose)
		if err != nil {
			return err
		}
		path := filepath.Join(l.ConfigDir(), configFileName(m.Target))
		if err := a.write(path, out); err != nil {
			return err
		}
		a.result.Migrations = append(a.result.Migrations, migrationResult{File: a.rel(path), Report: report})
		a.noteConflicts(report)
	}
	for _, c := range plan.AddConfigs {
		ref := install.ConfigRef{ID: c.ID, Version: c.Version, Versioned: c.Versioned}
		path := filepath.Join(l.ConfigDir(), configFileName(ref))
		if _, err := os.Stat(path); err == nil {
			continue // 已有的配置文件是使用者的，绝不覆盖
		}
		content := configdir.Skeleton(c.ID, c.Version, c.Schema, a.options.varRefs[ref])
		// 归档里有这个组件的旧配置（remove 时留下的）：按迁移算法恢复，而不是给一份空骨架（§7.7）
		if archive, version, ok := a.archivedConfig(c.ID, c.Version); ok {
			restored, report, err := a.restore(c, archive, version)
			if err != nil {
				return err
			}
			content = restored
			a.result.Restored = append(a.result.Restored, restoreResult{File: a.rel(path), Archive: a.rel(archive), Report: report})
			a.noteConflicts(report)
		}
		if err := a.write(path, content); err != nil {
			return err
		}
		a.result.ConfigsWritten = append(a.result.ConfigsWritten, a.rel(path))
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
	if err := a.mkdirAll(filepath.Dir(path)); err != nil {
		return writeError(path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return writeError(path, err)
	}
	return nil
}

// mkdirAll 建目录，并记下哪些是这次新建的。
func (a *applier) mkdirAll(dir string) error {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil || filepath.Dir(d) == d {
			break
		}
		missing = append(missing, d)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		a.createdDirs = append(a.createdDirs, missing[i])
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
		mode := os.FileMode(0o644)
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		a.backups[path] = &fileBackup{path: path, data: data, mode: mode, existed: true}
	case errors.Is(err, fs.ErrNotExist):
		a.backups[path] = &fileBackup{path: path}
	default:
		return writeError(path, err)
	}
	a.order = append(a.order, path)
	return nil
}

// rollback 把碰过的文件全部还原：原本有的写回原样（连同权限），原本没有的删掉，
// 这次新建的目录也删掉（从最深的开始，只删空的）。
func (a *applier) rollback() {
	for i := len(a.order) - 1; i >= 0; i-- {
		b := a.backups[a.order[i]]
		if b.existed {
			_ = os.MkdirAll(filepath.Dir(b.path), 0o755)
			_ = os.WriteFile(b.path, b.data, b.mode)
			_ = os.Chmod(b.path, b.mode)
		} else {
			_ = os.Remove(b.path)
		}
	}
	for i := len(a.createdDirs) - 1; i >= 0; i-- {
		_ = os.Remove(a.createdDirs[i])
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

// migrateConfig 按 upgrade 的一处迁移生成新配置文件（upgrade 落盘与 --dry-run 的预览共用）。
func migrateConfig(m install.ConfigMigration, source []byte, choose func(install.ConfigMigration, configdir.Conflict) configdir.Choice) ([]byte, configdir.MigrateReport, error) {
	return configdir.Migrate(configdir.MigrateInput{
		ID: m.ID, FromVersion: m.From, ToVersion: m.To, Old: source,
		OldSchema: m.OldSchema, NewSchema: m.NewSchema,
		Choose: func(c configdir.Conflict) configdir.Choice {
			if choose == nil {
				return configdir.ChooseDuplicate
			}
			return choose(m, c)
		},
	})
}

// readOptional 读一个可能不存在的文件（不存在时返回空）。
func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, writeError(path, err)
	}
	return data, nil
}

// archivedConfig 找 config/.archive/ 里这个组件的配置：同版本的优先，否则取不高于它的最高版本，
// 再没有才取最高的那份。文件名只按 FileBase 匹配会撞（a-b/c 与 a/b-c 都是 a-b-c），
// 所以还要核对文件头里的组件 ID。
func (a *applier) archivedConfig(id, version string) (path, archivedVersion string, ok bool) {
	dir := a.proj.Layout.ConfigArchiveDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", false
	}
	var below, above string
	paths := map[string]string{}
	for _, e := range entries {
		base, v, parsed := configdir.ParseFileName(e.Name())
		if !parsed || base != configdir.FileBase(id) || v == "" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if data, err := os.ReadFile(full); err == nil {
			if headerID, _, found := configdir.Header(data); found && headerID != id {
				continue
			}
		}
		paths[v] = full
		switch cmp := manifest.CompareVersions(v, version); {
		case cmp == 0:
			return full, v, true
		case cmp < 0 && (below == "" || manifest.CompareVersions(v, below) > 0):
			below = v
		case cmp > 0 && (above == "" || manifest.CompareVersions(v, above) > 0):
			above = v
		}
	}
	for _, v := range []string{below, above} {
		if v != "" {
			return paths[v], v, true
		}
	}
	return "", "", false
}

// restore 把归档的配置迁移到这次 add 的版本。旧版本的 configSchema 取自永久的 Manifest 缓存；
// 缓存里没有时不知道旧默认值，归档里的键都当作使用者写的。
func (a *applier) restore(c install.ConfigFile, archive, archivedVersion string) ([]byte, configdir.MigrateReport, error) {
	source, err := readOptional(archive)
	if err != nil {
		return nil, configdir.MigrateReport{}, err
	}
	m := install.ConfigMigration{ID: c.ID, From: archivedVersion, To: c.Version, NewSchema: c.Schema}
	if old, err := manifest.ParseFile(a.proj.Layout.CachedManifestPath(c.ID, archivedVersion)); err == nil {
		m.OldSchema = old.ConfigSchema
	}
	return migrateConfig(m, source, a.options.choose)
}

// noteConflicts 记下这次迁移有没有写出冲突块。
func (a *applier) noteConflicts(r configdir.MigrateReport) {
	for _, choice := range r.Resolved {
		if choice == configdir.ChooseDuplicate {
			a.wroteConflicts = true
		}
	}
}
