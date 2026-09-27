package cli

// 本文件实现 brickkit upgrade（命令表 9，提案 §12，附录 A4、A20、A24）：移动组件的默认版本，
// 三份文件跟着改——brickkit.yaml 的版本与 requiredBy、部署条目、config/ 的迁移。
//
// 整次升级先算好、再一次落盘（失败全部还原）：全量升级时第二个升不了，第一个也不会改——
// §12.1 的"不留半成品"。判断在 internal/install（PlanUpgrade），迁移在 internal/configdir（Migrate）。
//
// 配置冲突（使用者改过、默认值也变了）：终端里逐条问；--yes 或没有输入时写两行重复键，
// up 在使用者解决之前拒绝启动（§12.3）。所以改完只核对拓扑，冲突块是给使用者的待办。

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/install"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

type upgradeFlags struct {
	dryRun, yes bool
}

func newUpgradeCommand(opts *Options) *cobra.Command {
	var f upgradeFlags
	cmd := &cobra.Command{
		Use:     i18n.T(msgid.CliUpgradeUse),
		Short:   i18n.T(msgid.CliUpgradeShort),
		Long:    i18n.T(msgid.CliUpgradeLong),
		Example: i18n.T(msgid.CliUpgradeExample),
		GroupID: groupComponent,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			return runUpgrade(ctx, opts, arg, f)
		},
	}
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, i18n.T(msgid.CliUpgradeFlagDryRun))
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, i18n.T(msgid.CliUpgradeFlagYes))
	return cmd
}

func runUpgrade(ctx context.Context, opts *Options, arg string, f upgradeFlags) error {
	proj, err := loadForInstall(opts)
	if err != nil {
		return err
	}
	client, err := newSourceClient(opts, proj.Layout, proj.Decl, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	moves, err := upgradeTargets(ctx, opts, proj, client, arg)
	if err != nil || len(moves) == 0 {
		return err
	}
	moves, err = withShellMoves(ctx, proj, client, moves)
	if err != nil {
		return err
	}
	r := resolver.New(resolver.FromSource(client))
	oldGraph, err := install.ResolveWorld(ctx, r, install.Defaults(proj, nil))
	if err != nil {
		return err
	}
	newGraph, err := install.ResolveWorld(ctx, r, install.Defaults(proj, moves))
	if err != nil {
		return err
	}
	plan, err := install.PlanUpgrade(proj, oldGraph, newGraph, moves)
	if err != nil {
		return err
	}

	if f.dryRun {
		return renderUpgradePreview(opts, proj, plan)
	}
	res, err := applyPlanWith(opts, proj, plan, applyOptions{topologyOnly: true, choose: conflictChooser(opts, f.yes)})
	if err != nil {
		return err
	}
	for _, m := range plan.Moves {
		if node := newGraph.Node(resolver.Ref{ID: m.ID, Version: m.To}); node != nil {
			if dl, err := client.DownloadArtifacts(ctx, node.Manifest); err == nil {
				renderWarnings(opts, dl.Warnings)
			}
		}
	}
	renderUpgradeResult(opts, plan, res)
	return nil
}

// upgradeTargets 定下这次移动哪些默认版本：点名的组件（写了版本就是那个版本，否则最新）；
// 不点名就是每个有更新版本的默认版本。本地源的组件"最新"就是它目录里的版本（附录 A8）。
func upgradeTargets(ctx context.Context, opts *Options, proj *project.Project, client *source.Client, arg string) ([]install.Move, error) {
	if arg == "" {
		var moves []install.Move
		for _, c := range proj.Decl.Components {
			if !proj.Decl.IsDefault(c.ID, c.Version) {
				continue
			}
			latest, err := client.LatestVersion(ctx, c.ID)
			if err != nil {
				return nil, err
			}
			if manifest.CompareVersions(latest.Version, c.Version) > 0 {
				moves = append(moves, install.Move{ID: c.ID, From: c.Version, To: latest.Version})
			}
		}
		if len(moves) == 0 {
			opts.Printf("%s\n", i18n.T(msgid.CliUpgradeAllUpToDate))
		}
		return moves, nil
	}
	id, version, err := parseComponentRef(arg)
	if err != nil {
		return nil, err
	}
	current, ok := proj.Decl.DefaultVersion(id)
	if !ok {
		return nil, clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.CliRemoveNotInProject, id)).
			WithHint(i18n.T(msgid.CliUpgradeHintAddFirst, id))
	}
	if version == "" {
		latest, err := client.LatestVersion(ctx, id)
		if err != nil {
			return nil, err
		}
		version = latest.Version
	}
	if version == current {
		opts.Printf("%s\n", i18n.T(msgid.CliUpgradeUpToDate, id+"@"+current))
		return nil, nil
	}
	return []install.Move{{ID: id, From: current, To: version}}, nil
}

// withShellMoves：升级外壳时，成员跟着换成新外壳编进的版本（附录 A24）。
func withShellMoves(ctx context.Context, proj *project.Project, client *source.Client, moves []install.Move) ([]install.Move, error) {
	out := append([]install.Move{}, moves...)
	moved := map[string]bool{}
	for _, m := range moves {
		moved[m.ID] = true
	}
	for _, m := range moves {
		if !proj.Decl.IsShellID(m.ID) {
			continue
		}
		oldShell, err := client.Manifest(ctx, m.ID, m.From)
		if err != nil {
			return nil, err
		}
		newShell, err := client.Manifest(ctx, m.ID, m.To)
		if err != nil {
			return nil, err
		}
		for _, member := range install.ShellMoves(proj, oldShell.Manifest, newShell.Manifest) {
			if !moved[member.ID] {
				moved[member.ID] = true
				out = append(out, member)
			}
		}
	}
	return out, nil
}

// conflictChooser：终端里逐条问（m 留自己的值、n 用新默认值、直接回车写成冲突块）；
// --yes 或没有输入时一律写冲突块（附录 A4）。
func conflictChooser(opts *Options, yes bool) func(install.ConfigMigration, configdir.Conflict) configdir.Choice {
	return func(m install.ConfigMigration, c configdir.Conflict) configdir.Choice {
		if yes || opts.Stdin == nil {
			return configdir.ChooseDuplicate
		}
		answer := ask(opts, i18n.T(msgid.CliUpgradeConflictPrompt, c.Key, m.ID, c.UserYAML, m.From, c.NewDefault, m.To))
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "m", "mine":
			return configdir.ChooseMine
		case "n", "new":
			return configdir.ChooseNew
		}
		return configdir.ChooseDuplicate
	}
}

// renderUpgradePreview 是 --dry-run：版本移动、进出项目的版本、每份配置的迁移结果，一个文件都不写。
func renderUpgradePreview(opts *Options, proj *project.Project, plan *install.Plan) error {
	opts.Printf("%s\n", i18n.T(msgid.CliUpgradePreviewHeader))
	renderUpgradeLines(opts, plan)
	for _, m := range plan.MigrateConfigs {
		source, err := readOptional(filepath.Join(proj.Layout.ConfigDir(), configFileName(m.Source)))
		if err != nil {
			return err
		}
		_, report, err := migrateConfig(m, source, nil)
		if err != nil {
			return err
		}
		renderMigrationReport(opts, "config/"+configFileName(m.Target), report)
	}
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpgradeDryRunNothingWritten))
	return nil
}

func renderUpgradeResult(opts *Options, plan *install.Plan, res *applied) {
	renderUpgradeLines(opts, plan)
	var conflicted []string
	for _, m := range res.Migrations {
		renderMigrationReport(opts, m.File, m.Report)
		for _, c := range m.Report.Conflicts {
			if m.Report.Resolved[c.Key] == configdir.ChooseDuplicate {
				conflicted = append(conflicted, m.File)
				break
			}
		}
	}
	for _, a := range res.ConfigsArchived {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveConfigArchived, a[0], a[1]))
	}
	opts.Printf("%s\n", i18n.T(msgid.CliInstallWritten, strings.Join(append([]string{project.FileDecl}, res.DeployFiles...), ", ")))
	if len(res.OtherDeployFiles) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliInstallOtherDeployFiles, strings.Join(res.OtherDeployFiles, ", ")))
	}
	if len(conflicted) > 0 {
		opts.Printf("\n%s\n", i18n.T(msgid.CliUpgradeConflictsLeft, strings.Join(conflicted, ", ")))
	}
}

// renderUpgradeLines 列出版本移动、加进来的、留作兼容版本的、移除的。
func renderUpgradeLines(opts *Options, plan *install.Plan) {
	for _, m := range plan.Moves {
		opts.Printf("   %s\n", i18n.T(msgid.CliUpgradeMove, m.ID, m.From, m.To))
	}
	for _, l := range plan.AddLines {
		if len(l.RequiredBy) > 0 {
			opts.Printf("   %s\n", i18n.T(msgid.CliAddLineRequiredBy, l.Ref().String(), strings.Join(l.RequiredBy, ", ")))
		} else {
			opts.Printf("   %s\n", i18n.T(msgid.CliAddLine, l.Ref().String()))
		}
	}
	for _, ref := range plan.Removed {
		opts.Printf("   %s\n", i18n.T(msgid.CliRemoveCascaded, ref.String()))
	}
	for _, id := range plan.LiftEntries {
		opts.Printf("   %s\n", i18n.T(msgid.CliUpgradeLifted, id))
	}
}

func renderMigrationReport(opts *Options, file string, r configdir.MigrateReport) {
	opts.Printf("📝 %s\n", file)
	rows := []struct {
		label string
		keys  []string
	}{
		{msgid.CliUpgradeKeysCopied, r.Copied},
		{msgid.CliUpgradeKeysAdded, r.Added},
		{msgid.CliUpgradeKeysDropped, r.Dropped},
		{msgid.CliUpgradeKeysFollowed, r.Followed},
	}
	for _, row := range rows {
		if len(row.keys) > 0 {
			opts.Printf("   %s\n", i18n.T(row.label, strings.Join(row.keys, ", ")))
		}
	}
	for _, c := range r.Conflicts {
		opts.Printf("   %s\n", i18n.T(msgid.CliUpgradeKeyConflict, c.Key, c.UserYAML, c.NewDefault))
	}
}
