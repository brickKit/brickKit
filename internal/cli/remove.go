package cli

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// newRemoveCommand 实现 brickkit remove（004 §3.4）。
func newRemoveCommand(opts *Options) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     i18n.T(msgid.CliRemoveRemoveComponentIdVersion),
		Short:   i18n.T(msgid.CliRemoveShort),
		GroupID: groupComponent,
		Long:    i18n.T(msgid.CliRemoveLong),
		Example: i18n.T(msgid.CliRemoveExample2),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliRemovePleaseSpecifyTheComponentTo)).
					WithDetail(i18n.T(msgid.CliRootUsage), i18n.T(msgid.CliRemoveBrickkitRemoveComponentIdVersion)).
					WithDetail(i18n.T(msgid.CliRemoveExample), "brickkit remove people/basic@1.0.0").
					WithExit(clierr.ExitUsage)
			}
			return runRemove(cmd.Context(), opts, args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false,
		i18n.T(msgid.CliRemoveDeleteTheSourceEvenWhen))
	return cmd
}

func runRemove(ctx context.Context, opts *Options, arg string, force bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	id, version, err := parseComponentRef(arg)
	if err != nil {
		return err
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	target, err := resolveRemoveTarget(cfg, id, version)
	if err != nil {
		return err
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	dep := findDependents(ctx, client, cfg, target)
	if len(dep.strong) > 0 {
		// 002 §3.9 / 004 §3.4 输出样例
		return clierr.New(clierr.CodeDependencyMissing, i18n.T(msgid.CliRemoveCannotRemove, target.ID)).
			WithDetail(i18n.T(msgid.CliStatusVersion), target.Version).
			WithDetail(i18n.T(msgid.CliRemoveTheseComponentsDependOnIt), strings.Join(dep.strong, i18n.T(msgid.ListSeparator))).
			WithHint(i18n.T(msgid.CliRemoveRemoveTheDependentsFirst))
	}

	// 不在 git 仓库里时 repo 为 nil：submodule 阻断自己会跳过，现有行为不变。
	repo, _ := gitrepo.Open(layout.Root)

	// 删源码之前先确认它找得回来、也确认它不是已登记的 submodule。
	// 放在改配置**之前**：拦下时不该留下"配置改了一半、源码还在"的现场
	// （与 add 的 planClones 同一个道理——2026-09-06 gap report 之后补的
	// submodule 阻断最早只写在 removeDir 里，复核时发现它晚了一步：
	// edit.Save() 已经跑完，配置早就先改了）。
	if err := checkSourceDeletable(layout, cfg, target, repo, force); err != nil {
		return err
	}

	edit, err := config.OpenEdit(layout.ConfigPath())
	if err != nil {
		return err
	}
	if !edit.RemoveComponent(target.ID, target.Version) {
		return notInConfigError(cfg, target.ID)
	}
	// 组件的最后一个版本也走了，指着它的资源绑定就是一条谁也用不上的配置。
	// 多版本共存时不动：绑定按组件 ID 记（003 §5.3），剩下的版本还要用它。
	var unbound []string
	if !edit.HasComponentID(target.ID) {
		unbound = edit.RemoveBindings(target.ID)
	}
	if err := edit.Save(); err != nil {
		return err
	}

	cleanup, err := cleanupComponent(layout, client, cfg, target, repo)
	if err != nil {
		return err
	}

	for _, w := range dep.warnings {
		opts.Printf("%s", w.Format())
	}
	opts.Printf("%s\n", i18n.T(msgid.CliRemoveRemoved, target))
	if len(unbound) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveResourceBindingsDropped, strings.Join(unbound, i18n.T(msgid.ListSeparator))))
	}
	if cleanup.sourceRemoved {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveDeletedSourceDirectory, workspace.DisplayDir(target.ID)))
	}
	if cleanup.archivedRemoved {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveDeletedArchivedSourceDirectory, workspace.DisplayArchivedDir(target.ID)))
	}
	if cleanup.manifestRemoved {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveCleanedTheManifestCache))
	}
	if cleanup.artifactsRemoved {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveCleanedTheArtifactsCache))
	}

	logging.Info(i18n.T(msgid.LogComponentRemoved),
		"component", target.String(),
		"unbound_resources", len(unbound),
		"source_dir_removed", cleanup.sourceRemoved,
		"archived_dir_removed", cleanup.archivedRemoved,
	)
	return nil
}

// resolveRemoveTarget 依据 brickkit.yaml 中的条目确定要移除的精确版本。
func resolveRemoveTarget(cfg *config.Config, id, version string) (resolver.Ref, error) {
	entries := cfg.ComponentsByID(id)
	if len(entries) == 0 {
		return resolver.Ref{}, notInConfigError(cfg, id)
	}

	if version == "" {
		if len(entries) > 1 {
			versions := make([]string, 0, len(entries))
			for _, e := range entries {
				versions = append(versions, e.Version)
			}
			// 004 §3.4 输出样例
			return resolver.Ref{}, clierr.New(clierr.CodeVersionAmbiguous,
				i18n.T(msgid.CliRemoveHasSeveralVersionsPleaseSpecify, id, strings.Join(versions, ", "))).
				WithHint("brickkit remove " + id + "@" + versions[0])
		}
		return resolver.Ref{ID: id, Version: entries[0].Version}, nil
	}

	for _, e := range entries {
		if e.Version == version {
			return resolver.Ref{ID: id, Version: version}, nil
		}
	}

	versions := make([]string, 0, len(entries))
	for _, e := range entries {
		versions = append(versions, e.Version)
	}
	return resolver.Ref{}, clierr.New(clierr.CodeComponentNotFound,
		i18n.T(msgid.CliRemoveErrorIsNotInBrickkit2, id, version)).
		WithDetail(i18n.T(msgid.CliRemoveCurrentVersion), strings.Join(versions, ", ")).
		WithHint(i18n.T(msgid.CliRemoveCheckThatTheVersionNumber, id, versions[0]))
}

func notInConfigError(cfg *config.Config, id string) error {
	e := clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.CliRemoveErrorIsNotInBrickkit, id)).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliRemoveBrickkitYamlHasNoSuch))
	if len(cfg.Components) > 0 {
		refs := make([]string, 0, len(cfg.Components))
		for _, c := range cfg.Components {
			refs = append(refs, c.Ref())
		}
		e = e.WithDetail(i18n.T(msgid.CliRemoveCurrentComponents), strings.Join(refs, i18n.T(msgid.ListSeparator)))
	}
	return e.WithHint(i18n.T(msgid.CliRemoveCheckThatTheComponentId))
}

// ============================================================
// 依赖方检查（002 §3.9）
// ============================================================

type dependentReport struct {
	strong   []string
	warnings []*clierr.Error
}

// findDependents 检查 brickkit.yaml 中其他组件是否依赖目标组件。
//
// 这里**不做递归解析**：卸载检查只关心配置里已有的组件怎么声明依赖，
// 递归解析会因为某个无关组件的依赖缺失而整体失败，把"移除"这条退路也堵死。
// 取不到某个组件的 Manifest 时给出警告，而不是假装它没有依赖。
func findDependents(
	ctx context.Context,
	client *source.Client,
	cfg *config.Config,
	target resolver.Ref,
) dependentReport {
	var report dependentReport

	for _, other := range cfg.Components {
		if other.ID == target.ID && other.Version == target.Version {
			continue
		}
		fetched, err := client.Manifest(ctx, other.ID, other.Version)
		if err != nil {
			report.warnings = append(report.warnings,
				clierr.Warn(clierr.CodeManifestInvalid, i18n.T(msgid.CliRemoveWarningCannotConfirmTheDependencies, other.Ref())).
					WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliRemoveThatComponentSManifestIs)).
					WithDetail(i18n.T(msgid.LabelImpact), i18n.T(msgid.CliRemoveCannotTellWhetherItDepends, target)).
					WithTip(i18n.T(msgid.CliRemoveRunBrickkitAddToPull, other.Ref())))
			continue
		}
		for _, d := range dependenciesOf(fetched.Manifest) {
			if d.ID != target.ID || d.Version != target.Version {
				continue
			}
			if d.Optional {
				report.warnings = append(report.warnings,
					clierr.Warn(clierr.CodeDependencyMissing, i18n.T(msgid.CliRemoveWarningIsDependedOnAs, target.String())).
						WithDetail(i18n.T(msgid.CliRemoveDependent), other.Ref()).
						WithDetail(i18n.T(msgid.LabelImpact), i18n.T(msgid.CliRemoveAfterRemovalTheEnvironmentVariable, other.Ref(), manifest.EndpointEnvVar(target.ID))).
						WithTip(i18n.T(msgid.CliRemoveOptionalDependencyDegradationIsHandled)))
				continue
			}
			report.strong = append(report.strong, other.Ref())
		}
	}
	return report
}

func dependenciesOf(m *manifest.Manifest) []manifest.ComponentDep {
	if m == nil || m.Dependencies == nil {
		return nil
	}
	return m.Dependencies.Components
}

// ============================================================
// 缓存与源码清理
// ============================================================

type cleanupResult struct {
	manifestRemoved  bool
	artifactsRemoved bool
	sourceRemoved    bool
	archivedRemoved  bool
}

// cleanupComponent 清理 Manifest 缓存、artifacts 缓存与源码目录。
//
// 缓存按版本区分，直接删；源码目录按组件 ID 组织，
// 只有同 ID 的最后一个版本被移除时才能删。
//
// 源码要连归档目录一起清：sync 归档过的组件一旦从 brickkit.yaml 里移除，
// sync 就再也不会整理它，留在 .archived/ 里就是永久孤儿。
func cleanupComponent(
	layout config.Layout,
	client *source.Client,
	cfg *config.Config,
	target resolver.Ref,
	repo *gitrepo.Repo,
) (cleanupResult, error) {
	var res cleanupResult

	manifestPath := client.ManifestCachePath(target.ID, target.Version)
	switch err := os.Remove(manifestPath); {
	case err == nil:
		res.manifestRemoved = true
	case !os.IsNotExist(err):
		return res, cleanupError(i18n.T(msgid.CliRemoveCleanTheManifestCache), manifestPath, err)
	}

	// 签名缓存跟 Manifest 是一对，必须一起删。留下孤儿签名的话，
	// 下次重新 add 同一版本时会先命中一份对不上的旧签名（虽然会退回重新拉取，
	// 但那已经是在靠兜底逻辑救场了）。删不掉不阻断——它只是一份缓存。
	_ = os.Remove(client.SignatureCachePath(target.ID, target.Version))

	artifactDir := client.ArtifactDir(target.ID, target.Version)
	if _, err := os.Stat(artifactDir); err == nil {
		if err := os.RemoveAll(artifactDir); err != nil {
			return res, cleanupError(i18n.T(msgid.CliRemoveCleanTheArtifactsCache), artifactDir, err)
		}
		res.artifactsRemoved = true
	}

	if remainingVersions(cfg, target) > 0 {
		return res, nil
	}
	sourceRemoved, err := workspace.RemoveSource(layout, target.ID, repo)
	if err != nil {
		return res, err
	}
	res.sourceRemoved = sourceRemoved

	archivedRemoved, err := workspace.RemoveArchived(layout, target.ID, repo)
	if err != nil {
		return res, err
	}
	res.archivedRemoved = archivedRemoved
	return res, nil
}

// remainingVersions 返回移除目标版本后，同 ID 还剩几个版本。
func remainingVersions(cfg *config.Config, target resolver.Ref) int {
	n := 0
	for _, c := range cfg.Components {
		if c.ID == target.ID && c.Version != target.Version {
			n++
		}
	}
	return n
}

func cleanupError(action, path string, cause error) error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.IOFailed, action)).
		WithDetail(i18n.T(msgid.LabelPath), path).
		WithDetail(i18n.T(msgid.LabelReason), cause.Error()).
		WithHint(i18n.T(msgid.CliRemoveCheckTheFileAndDirectory)).
		WithCause(cause)
}

// checkSourceDeletable 在删源码之前确认它删了还找得回来。
//
// # 为什么这一步必须有
//
// `init` 的骨架把本地安装源指向 `./components`，试用指南 17 教的正是在那儿手写
// 自己的组件。于是这条路完全由默认约定铺出来：照着写 → `add --local` 加进来 →
// 觉得暂时不用了 → `remove` → **源码永久没了**，没有确认、没有 `--yes`、
// 一句提示都没有。真跑验证过：手写的 `main.go` 一并消失。
//
// 012 §2.20 论证过"未提交的修改是用户自己的问题，应该先 commit + push"。
// 那句话的前提是**有地方可 push**——即源码来自 `--repo` clone。手写的组件没有
// 远端，那条论证覆盖不到它。
//
// 所以判据不是"你有没有做对"，而是"这些字节在别的地方还有没有"
// （见 workspace.DeletionRisk）。干净又推过的 clone 照常删，行为一点没变。
//
// # 多版本共存时不查
//
// 那时源码目录根本不会被删（同 ID 的其他版本还要用它），没有可丢的东西。
//
// # submodule 阻断不受 --force 影响
//
// --force 是"数据会不会丢"这类风险判断的明确出路；已登记的 git submodule
// 不是这类风险——直接删只会把 .gitmodules、superproject 索引、
// .git/modules/ 的账目搞乱，--force 掉这一步不会让账目变干净，只会把同一个
// 坑推到 workspace.removeDir 里，那时 brickkit.yaml 已经改完存盘了
// （2026-09-06 gap report 之后发现的时序问题）。所以这一条查在 force 短路
// 之前，且不受它影响。
func checkSourceDeletable(
	layout config.Layout, cfg *config.Config, target resolver.Ref, repo *gitrepo.Repo, force bool,
) error {
	if len(cfg.ComponentsByID(target.ID)) > 1 {
		return nil
	}

	candidates := []struct{ dir, display string }{
		{workspace.SourceDir(layout, target.ID), workspace.DisplayDir(target.ID)},
		{workspace.ArchivedDir(layout, target.ID), workspace.DisplayArchivedDir(target.ID)},
	}

	for _, candidate := range candidates {
		if err := workspace.SubmoduleRemoveGuard(repo, candidate.dir, target.ID, candidate.display); err != nil {
			return err
		}
	}

	if force {
		return nil
	}

	for _, candidate := range candidates {
		risk := workspace.DeletionRisk(candidate.dir)
		if risk == "" {
			continue
		}
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliRemoveErrorTheSourceCanT)).
			WithDetail(i18n.T(msgid.LabelComponent), target.String()).
			WithDetail(i18n.T(msgid.LabelDir), candidate.display).
			WithDetail(i18n.T(msgid.LabelReason), risk).
			WithHint(
				i18n.T(msgid.CliRemoveFirstKeepItSafeCommit),
				i18n.T(msgid.CliRemoveIfYouAreSureYou, target.ID),
				i18n.T(msgid.CliRemoveIfYouOnlyDonT),
			)
	}
	return nil
}
