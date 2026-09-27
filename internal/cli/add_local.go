package cli

// 本文件实现 brickkit add --local：把本地安装源里的组件一次全部加进项目，版本写
// component.yaml 里的真实版本（附录 A8）。一批组件一份计划、一次落盘：互相依赖的
// 本地组件谁先谁后都一样，也不会加到一半停下。

import (
	"context"
	"slices"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
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
