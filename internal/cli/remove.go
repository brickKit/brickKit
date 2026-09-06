package cli

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// newRemoveCommand 实现 brickkit remove（004 §3.4）。
func newRemoveCommand(opts *Options) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "remove <组件ID>[@版本]",
		Short:   "移除组件，并删除对应的源码目录与缓存",
		GroupID: groupComponent,
		Long: `从项目中移除组件（004 §3.4）。

行为：
  1. 检查是否有其他组件强依赖它 → 有则阻止移除
  2. 从 brickkit.yaml 中移除条目
  3. 解除 resources[].bindings 中指向它的绑定（同 ID 还有其他版本时保留）
  4. 清理 Manifest 缓存与 artifacts 缓存
  5. 自动删除源码目录：components/<scope>/<name>/ 与归档中的
     components/.archived/<scope>/<name>/（同 ID 还有其他版本时保留）


多版本共存时必须指定版本，否则报错。`,
		Example: `  brickkit remove people/basic
  brickkit remove people/basic@1.0.0    多版本共存时指定版本`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定要移除的组件").
					WithDetail("用法", "brickkit remove <组件ID>[@版本]").
					WithDetail("示例", "brickkit remove people/basic@1.0.0").
					WithExit(clierr.ExitUsage)
			}
			return runRemove(cmd.Context(), opts, args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false,
		"源码删了就找不回来时（不是 Git 仓库、有未提交的改动、有没推的提交）照样删")
	return cmd
}

func runRemove(ctx context.Context, opts *Options, arg string, force bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	id, version, err := parseComponentRef(arg, false)
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
		return clierr.Newf(clierr.CodeDependencyMissing, "无法移除 %s", target.ID).
			WithDetail("版本", target.Version).
			WithDetail("以下组件强依赖它", strings.Join(dep.strong, "、")).
			WithHint("请先移除依赖方")
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
	opts.Printf("✅ 已移除 %s\n", target)
	if len(unbound) > 0 {
		opts.Printf("   🗑️ 已解除资源绑定：%s\n", strings.Join(unbound, "、"))
	}
	if cleanup.sourceRemoved {
		opts.Printf("   🗑️ 已删除源码目录 %s\n", workspace.DisplayDir(target.ID))
	}
	if cleanup.archivedRemoved {
		opts.Printf("   🗑️ 已删除归档源码目录 %s\n", workspace.DisplayArchivedDir(target.ID))
	}
	if cleanup.manifestRemoved {
		opts.Printf("   🗑️ 已清理 Manifest 缓存\n")
	}
	if cleanup.artifactsRemoved {
		opts.Printf("   🗑️ 已清理 artifacts 缓存\n")
	}

	logging.Info("组件已移除",
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
			return resolver.Ref{}, clierr.Newf(clierr.CodeVersionAmbiguous,
				"%s 存在多个版本（%s），请指定版本：", id, strings.Join(versions, ", ")).
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
	return resolver.Ref{}, clierr.Newf(clierr.CodeComponentNotFound,
		"错误：%s@%s 不在 brickkit.yaml 中", id, version).
		WithDetail("当前版本", strings.Join(versions, ", ")).
		WithHint("确认版本号是否正确，或执行 brickkit remove " + id + "@" + versions[0])
}

func notInConfigError(cfg *config.Config, id string) error {
	e := clierr.Newf(clierr.CodeComponentNotFound, "错误：%s 不在 brickkit.yaml 中", id).
		WithDetail("原因", "brickkit.yaml 的 components 中没有该组件")
	if len(cfg.Components) > 0 {
		refs := make([]string, 0, len(cfg.Components))
		for _, c := range cfg.Components {
			refs = append(refs, c.Ref())
		}
		e = e.WithDetail("当前组件", strings.Join(refs, "、"))
	}
	return e.WithHint("确认组件 ID 是否正确")
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
				clierr.Warn(clierr.CodeManifestInvalid, "警告：无法确认 "+other.Ref()+" 的依赖关系").
					WithDetail("原因", "该组件的 Manifest 既不在缓存中，也无法从安装源获取").
					WithDetailf("影响", "无法判断它是否依赖 %s，移除后可能导致它启动失败", target).
					WithTip("可执行 brickkit add "+other.Ref()+" 重新拉取缓存后重试（会提示是否刷新）"))
			continue
		}
		for _, d := range dependenciesOf(fetched.Manifest) {
			if d.ID != target.ID || d.Version != target.Version {
				continue
			}
			if d.Optional {
				report.warnings = append(report.warnings,
					clierr.Warn(clierr.CodeDependencyMissing, "警告："+target.String()+" 被弱依赖").
						WithDetail("依赖方", other.Ref()).
						WithDetailf("影响", "移除后 %s 的环境变量 %s 不会再被注入",
							other.Ref(), manifest.EndpointEnvVar(target.ID)).
						WithTip("弱依赖降级由组件自行处理（002 §3.4）"))
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
		return res, cleanupError("清理 Manifest 缓存", manifestPath, err)
	}

	// 签名缓存跟 Manifest 是一对，必须一起删。留下孤儿签名的话，
	// 下次重新 add 同一版本时会先命中一份对不上的旧签名（虽然会退回重新拉取，
	// 但那已经是在靠兜底逻辑救场了）。删不掉不阻断——它只是一份缓存。
	_ = os.Remove(client.SignatureCachePath(target.ID, target.Version))

	artifactDir := client.ArtifactDir(target.ID, target.Version)
	if _, err := os.Stat(artifactDir); err == nil {
		if err := os.RemoveAll(artifactDir); err != nil {
			return res, cleanupError("清理 artifacts 缓存", artifactDir, err)
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
	return clierr.Newf(clierr.CodeConfigInvalid, "错误：%s失败", action).
		WithDetail("路径", path).
		WithDetail("原因", cause.Error()).
		WithHint("检查文件与目录权限").
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
		return clierr.New(clierr.CodeConfigInvalid, "错误：源码删掉就找不回来了").
			WithDetail("组件", target.String()).
			WithDetail("目录", candidate.display).
			WithDetail("原因", risk).
			WithHint(
				"先把它保住：提交并推到远端，或者把这个目录拷走 / 改名",
				"确认不要了就加 --force：brickkit remove "+target.ID+" --force",
				"只是暂时不用的话，给它写 enabled: false 再 brickkit sync"+
					"——那会把源码收进归档目录，而不是删掉（004 §3.9）",
			)
	}
	return nil
}
