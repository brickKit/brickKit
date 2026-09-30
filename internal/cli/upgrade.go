package cli

// 本文件实现 brickkit upgrade（命令表 9，提案 §12，附录 A4、A20、A24）：移动组件的默认版本，
// 三份文件跟着改——brickkit.yaml 的版本与 requiredBy、部署条目、config/ 的迁移。
//
// 整次升级先算好、再一次落盘（失败全部还原）：全量升级时第二个升不了，第一个也不会改——
// 提案 §12.1 的"不留半成品"。判断在 internal/install（PlanUpgrade），迁移在 internal/configdir（Migrate）。
//
// 配置冲突（使用者改过、默认值也变了）：终端里逐条问；--yes 或没有输入时写两行重复键，
// up 在使用者解决之前拒绝启动（提案 §12.3）。所以改完只核对拓扑，冲突块是给使用者的待办。

import (
	"context"
	"os"
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
		return previewUpgrade(opts, proj, plan)
	}
	res, err := applyPlanWith(opts, proj, plan, applyOptions{allowConflicts: true, choose: conflictChooser(opts, f.yes)})
	if err != nil {
		return err
	}
	fresh := append([]resolver.Ref{}, plan.Added...)
	for _, m := range plan.Moves {
		fresh = append(fresh, resolver.Ref{ID: m.ID, Version: m.To})
	}
	_, warnings := downloadAddedArtifacts(ctx, client, newGraph, fresh)
	renderUpgradeResult(opts, plan, res, false)
	renderWarnings(opts, warnings)
	refreshProjectDoc(opts, proj.Layout)
	return nil
}

// upgradeTargets 定下这次移动哪些默认版本：点名的组件（写了版本就是那个版本，否则最新）；
// 不点名就是每个有更新版本的默认版本。本地源的组件"最新"就是它目录里的版本（附录 A8）。
func upgradeTargets(ctx context.Context, opts *Options, proj *project.Project, client *source.Client, arg string) ([]install.Move, error) {
	if arg == "" {
		var moves []install.Move
		var fromLocal []localLatest
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
			} else if latest.SourceKind == source.OriginLocal {
				fromLocal = append(fromLocal, localLatest{ref: c.Ref(), source: latest.SourceID})
			}
		}
		if len(moves) == 0 {
			opts.Printf("%s\n", i18n.T(msgid.CliUpgradeAllUpToDate))
		}
		renderLocalLatest(opts, fromLocal)
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
	var fromLocal []localLatest
	if version == "" {
		latest, err := client.LatestVersion(ctx, id)
		if err != nil {
			return nil, err
		}
		version = latest.Version
		if latest.SourceKind == source.OriginLocal {
			fromLocal = append(fromLocal, localLatest{ref: id + "@" + current, source: latest.SourceID})
		}
		// 不写版本只往上走：安装源里最新的比项目里的旧，就是已经最新，绝不悄悄降级
		if manifest.CompareVersions(version, current) < 0 {
			version = current
		}
	}
	if version == current {
		opts.Printf("%s\n", i18n.T(msgid.CliUpgradeUpToDate, id+"@"+current))
		renderLocalLatest(opts, fromLocal)
		return nil, nil
	}
	return []install.Move{{ID: id, From: current, To: version}}, nil
}

// localLatest 是一个"最新版本"由本地源回答、因而没有移动的组件（附录 A8）。
type localLatest struct{ ref, source string }

// renderLocalLatest 说明这些组件的"最新"来自本地工作区：不说的话，"都是最新"会让人
// 以为远端没有新版本，而其实只是本地源排在前面、它的工作区还在旧版本上。
func renderLocalLatest(opts *Options, items []localLatest) {
	if len(items) == 0 {
		return
	}
	opts.Printf("%s\n", i18n.T(msgid.CliUpgradeLocalLatestHeader))
	for _, it := range items {
		opts.Printf("   %s\n", i18n.T(msgid.CliUpgradeLocalLatestLine, it.ref, it.source))
	}
	opts.Printf("   %s\n", i18n.T(msgid.CliUpgradeLocalLatestHint))
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

// previewUpgrade 是 --dry-run：在项目文件的一份临时副本上真的做一遍、核对一遍（冲突一律写成冲突块），
// 把结果原样报出来——会失败的升级在预览里就失败。项目本身一个文件都不动。
func previewUpgrade(opts *Options, proj *project.Project, plan *install.Plan) error {
	tmp, err := os.MkdirTemp("", "brickkit-upgrade-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := copyProjectFiles(proj.Layout.Root, tmp); err != nil {
		return err
	}
	copyProj, err := project.Load(tmp, project.LoadOptions{NoLocal: true})
	if err != nil {
		return err
	}
	res, err := applyPlanWith(opts, copyProj, plan, applyOptions{allowConflicts: true})
	if err != nil {
		return err
	}
	opts.Printf("%s\n", i18n.T(msgid.CliUpgradePreviewHeader))
	renderUpgradeResult(opts, plan, res, true)
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpgradeDryRunNothingWritten))
	return nil
}

// copyProjectFiles 复制三份文件（brickkit.yaml、deploy*.yaml、config/）到 dst。
func copyProjectFiles(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		top := strings.Split(filepath.ToSlash(rel), "/")[0]
		keep := rel == "." || top == project.DirConfig || top == project.FileDecl ||
			(strings.HasPrefix(top, "deploy") && strings.HasSuffix(top, ".yaml"))
		if !keep {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// renderUpgradeResult 报告一次升级；preview（--dry-run）时用"会"的语气——那些文件只在副本上改过。
func renderUpgradeResult(opts *Options, plan *install.Plan, res *applied, preview bool) {
	archived, written, conflicts := msgid.CliRemoveConfigArchived, msgid.CliInstallWritten, msgid.CliUpgradeConflictsLeft
	if preview {
		archived, written, conflicts = msgid.CliUpgradeWouldArchive, msgid.CliUpgradeWouldWrite, msgid.CliUpgradeConflictsWouldLeave
	}
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
		opts.Printf("%s\n", i18n.T(archived, a[0], a[1]))
	}
	opts.Printf("%s\n", i18n.T(written, strings.Join(append([]string{project.FileDecl}, res.DeployFiles...), ", ")))
	if len(res.OtherDeployFiles) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliInstallOtherDeployFiles, strings.Join(res.OtherDeployFiles, ", ")))
	}
	if len(conflicted) > 0 {
		opts.Printf("\n%s\n", i18n.T(conflicts, strings.Join(conflicted, ", ")))
	}
}

// renderUpgradeLines 列出版本移动、加进来的、留作兼容版本的、移除的。
func renderUpgradeLines(opts *Options, plan *install.Plan) {
	for _, m := range plan.Moves {
		label := msgid.CliUpgradeMove
		if manifest.CompareVersions(m.To, m.From) < 0 {
			label = msgid.CliUpgradeMoveDown
		}
		opts.Printf("   %s\n", i18n.T(label, m.ID, m.From, m.To))
	}
	for _, note := range plan.Notes {
		opts.Printf("   %s\n", i18n.T(msgid.CliInstallNote, note))
	}
	for _, l := range plan.AddLines {
		if len(l.RequiredBy) > 0 {
			opts.Printf("   %s\n", i18n.T(msgid.CliAddLineRequiredBy, l.Ref().String(), strings.Join(l.RequiredBy, ", ")))
		} else {
			opts.Printf("   %s\n", i18n.T(msgid.CliAddLine, l.Ref().String()))
		}
	}
	oldDefaults := map[resolver.Ref]bool{}
	for _, m := range plan.Moves {
		oldDefaults[resolver.Ref{ID: m.ID, Version: m.From}] = true
	}
	for _, ref := range plan.Removed {
		label := msgid.CliRemoveCascaded
		if oldDefaults[ref] {
			label = msgid.CliUpgradeOldRemoved
		}
		opts.Printf("   %s\n", i18n.T(label, ref.String()))
	}
	for _, l := range plan.SetRequiredBy {
		if len(l.RequiredBy) > 0 {
			opts.Printf("   %s\n", i18n.T(msgid.CliRemoveRequiredByNow, l.Ref().String(), strings.Join(l.RequiredBy, ", ")))
		}
	}
	for _, id := range plan.LiftEntries {
		opts.Printf("   %s\n", i18n.T(msgid.CliUpgradeLifted, id))
	}
}

func renderMigrationReport(opts *Options, file string, r configdir.MigrateReport) {
	opts.Printf("📝 %s\n", file)
	rows := []struct {
		label msgid.ID
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
