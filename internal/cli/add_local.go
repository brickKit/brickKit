package cli

// 本文件实现 brickkit add --local：把本地安装源里的组件一次全部加进项目，版本写
// component.yaml 里的真实版本（附录 A8）。一批组件一份计划、一次落盘：互相依赖的
// 本地组件谁先谁后都一样，也不会加到一半停下。
//
// --init 先给还没有 brickkit.yaml 的每个本地组件建一个本地联调工作台（提案 §9.6.1）：
// 与 brickkit init 同一个补全原语，安装源继承自这个项目，再把组件自己的依赖加进去——
// 之后 cd 进组件目录 brickkit up，就能单独拉起它的依赖树联调。

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

func runAddLocal(ctx context.Context, opts *Options, f addFlags) error {
	proj, err := loadForInstall(opts)
	if err != nil {
		return err
	}
	if !hasLocalSource(proj.Decl) {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliAddLocalNoLocalSource)).
			WithHint(i18n.T(msgid.CliAddLocalHintConfigureLocal))
	}
	client, err := newSourceClient(opts, proj.Layout, proj.Decl, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	scan, err := client.LocalComponents(ctx)
	if err != nil {
		return err
	}
	for _, p := range scan.Problems {
		opts.Printf("%s\n", i18n.T(msgid.CliAddLocalProblem, p.ID, p.SourceID, p.Reason))
	}
	renderWarnings(opts, scan.Warnings)
	if f.init {
		// 先给每个子组件建好工作台，再把它们加进顶层项目：子工作台失败时顶层一个字都不改
		if err := initWorkbenches(ctx, opts, proj, client, scan); err != nil {
			return err
		}
	}

	var targets []resolver.Ref
	for _, lc := range scan.Components {
		ref := resolver.Ref{ID: lc.ID, Version: lc.Version}
		versions := proj.Decl.Versions(lc.ID)
		switch {
		case len(versions) == 0:
			targets = append(targets, ref)
		case !slices.Contains(versions, lc.Version):
			// 本地仓库的版本与项目里的对不上：add 不改已有组件的版本（那是 upgrade），
			// 说一声——up 在这个组件从本地仓库运行时会拦下（附录 A22）
			current, _ := proj.Decl.DefaultVersion(lc.ID)
			opts.Printf("%s\n", i18n.T(msgid.CliAddLocalVersionDiffers, lc.ID, lc.Version, current))
		}
	}
	if len(targets) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliAddLocalNothingToAdd))
		return nil
	}
	return installAdd(ctx, opts, proj, client, targets, f)
}

// checkLocalFlagCombo：--local 是"本地源里的全部"，接了组件 ID 就自相矛盾；本地组件的源码
// 本来就在盘上，--repo / --repo-all 没有东西可克隆。
func checkLocalFlagCombo(args []string, f addFlags) error {
	if len(args) > 0 {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliAddLocalTakesNoComponent, args[0])).
			WithHint(i18n.T(msgid.CliAddLocalHintOneComponent, args[0])).WithExit(clierr.ExitUsage)
	}
	for _, bad := range []struct {
		on   bool
		flag string
	}{{f.repo, "--repo"}, {f.repoAll, "--repo-all"}} {
		if bad.on {
			return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliAddLocalWithRepo, bad.flag)).
				WithExit(clierr.ExitUsage)
		}
	}
	return nil
}

func hasLocalSource(decl *projfile.File) bool {
	for _, s := range decl.EnabledSources() {
		if s.Type == projfile.SourceTypeLocal {
			return true
		}
	}
	return false
}

// initWorkbenches 为本地源里每个还没有 brickkit.yaml 的组件建工作台，按组件 ID 顺序，遇错即停。
func initWorkbenches(ctx context.Context, opts *Options, proj *project.Project, client *source.Client, scan *source.LocalScan) error {
	files, err := client.LocalManifestFiles()
	if err != nil {
		return err
	}
	dirs := map[string]string{}
	for _, f := range files {
		dirs[f.ID] = filepath.Dir(f.Path)
	}
	created := 0
	for _, lc := range scan.Components {
		dir, ok := dirs[lc.ID]
		if !ok {
			continue
		}
		child := project.NewLayout(dir)
		if fileExists(child.DeclPath()) {
			continue
		}
		added, err := initWorkbench(ctx, opts, proj, lc.ID, dir)
		if err != nil {
			e := clierr.As(err)
			return clierr.New(e.Code, i18n.T(msgid.CliAddLocalInitFailed, lc.ID)).
				WithDetail(i18n.T(msgid.LabelDir), displayPath(opts.WorkDir, dir)).
				WithDetail(i18n.T(msgid.LabelReason), strings.TrimPrefix(strings.TrimSpace(e.Message), "❌ ")).
				WithHint(append(append([]string{}, e.Hints...), i18n.T(msgid.CliAddLocalInitHintKept))...).
				WithCause(err)
		}
		opts.Printf("   🧰 %s\n", i18n.T(msgid.CliAddLocalInitDone, displayPath(opts.WorkDir, dir), i18n.Count(msgid.CountDependencies, added.deps)))
		renderGitignoreWarning(opts, displayPath(opts.WorkDir, filepath.Join(dir, project.FileGitignore)), added.gitignoreMissing)
		created++
	}
	if created > 0 {
		// release 要求组件目录干净：没提交的工作台文件会让它拒绝发布
		opts.Printf("   💡 %s\n", i18n.T(msgid.CliAddLocalInitCommit))
	}
	return nil
}

// workbenchResult 是建一个工作台的结果。
type workbenchResult struct {
	deps             int
	gitignoreMissing []string
}

// initWorkbench 在 dir 里补全出工作台，再把组件自己的依赖（强弱都算）加进去。
func initWorkbench(ctx context.Context, opts *Options, parent *project.Project, id, dir string) (workbenchResult, error) {
	var res workbenchResult
	m, err := manifest.ParseFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return res, err
	}
	name := strings.ReplaceAll(id, "/", "-")
	if s := projfile.SuggestProjectName(name); projfile.ProjectNameProblem(name) != "" && s != "" {
		name = s
	}
	child := project.NewLayout(dir)
	plan, err := project.PlanWorkbench(child, name, inheritSources(parent, dir))
	if err != nil {
		return res, err
	}
	if err := plan.Apply(child); err != nil {
		return res, err
	}
	res.gitignoreMissing = plan.GitignoreMissing

	var targets []resolver.Ref
	if m.Dependencies != nil {
		for _, d := range m.Dependencies.Components {
			targets = append(targets, resolver.Ref{ID: d.ID, Version: d.Version})
		}
	}
	if len(targets) == 0 {
		return res, nil
	}
	childOpts := *opts
	childOpts.WorkDir = dir
	childProj, err := loadForInstall(&childOpts)
	if err != nil {
		return res, err
	}
	childClient, err := newSourceClient(&childOpts, childProj.Layout, childProj.Decl, source.Options{})
	if err != nil {
		return res, err
	}
	defer func() { _ = childClient.Close() }()
	if err := installAdd(ctx, &childOpts, childProj, childClient, targets, addFlags{yes: true}); err != nil {
		return res, err
	}
	res.deps = len(targets)
	return res, nil
}

// inheritSources 复制顶层项目的安装源给子工作台；本地源的路径改写成相对子目录，
// 让子工作台照样看得到兄弟组件（它们多半互相依赖）。
func inheritSources(parent *project.Project, dir string) []projfile.Source {
	out := make([]projfile.Source, 0, len(parent.Decl.Sources))
	for _, s := range parent.Decl.Sources {
		if s.Type == projfile.SourceTypeLocal && s.Path != "" {
			abs, _ := filepath.Abs(parent.Layout.Resolve(s.Path))
			from, _ := filepath.Abs(dir)
			if rel, err := filepath.Rel(from, abs); err == nil {
				s.Path = filepath.ToSlash(rel)
			}
		}
		out = append(out, s)
	}
	return out
}
