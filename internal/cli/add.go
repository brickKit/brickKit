package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/backup"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// addFlags 是 brickkit add 的参数（004 §3.3）。
type addFlags struct {
	yes     bool
	refresh bool
	repo    bool
	repoAll bool
}

// newAddCommand 实现 brickkit add（004 §3.3）。
func newAddCommand(opts *Options) *cobra.Command {
	var f addFlags

	cmd := &cobra.Command{
		Use:     "add <组件ID>@<精确版本>",
		Short:   "添加组件，递归拉取依赖与产物，写入 brickkit.yaml",
		GroupID: groupComponent,
		Long: `添加组件到项目（004 §3.3）。

行为：
  1. 从安装源获取 Manifest（市场 / Git / 本地目录）
  2. 递归解析 dependencies.components
  3. 强依赖不可获取 → 报错终止；弱依赖不可获取 → 警告但继续
  4. 同 ID 不同版本 → 自动添加第二个条目（多版本默认共存）
  5. 下载 artifacts 到 .brickkit/artifacts/<版本化服务名>/
  6. 写入 brickkit.yaml（不写 enabled 字段）

修改 brickkit.yaml 前会自动备份到 .brickkit/backup/brickkit.yaml.last，
可用 brickkit reset --last 撤销。

版本必须是精确版本（major.minor.patch），不接受 ^ 或 ~ 范围约束。`,
		Example: `  brickkit add people/basic@1.0.0
  brickkit add people/basic@1.0.0 --yes         非交互模式
  brickkit add people/basic@1.0.0 --repo        额外 clone 该组件源码
  brickkit add erp/backend@1.0.0 --repo-all     clone 所有开源依赖源码`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定要添加的组件").
					WithDetail("用法", "brickkit add <组件ID>@<精确版本>").
					WithDetail("示例", "brickkit add people/basic@1.0.0").
					WithExit(clierr.ExitUsage)
			}
			return runAdd(cmd.Context(), opts, args[0], f)
		},
	}

	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, "非交互模式，跳过所有确认提示（适用于 CI/CD）")
	cmd.Flags().BoolVar(&f.refresh, "refresh", false, "强制重新拉取 Manifest 和 artifacts，忽略缓存")
	cmd.Flags().BoolVar(&f.repo, "repo", false, "额外 clone 该组件的完整 Git 仓库到 components/（仅开源组件）")
	cmd.Flags().BoolVar(&f.repoAll, "repo-all", false, "clone 所有递归依赖中开源组件的 Git 仓库（闭源组件跳过）")
	return cmd
}

func runAdd(ctx context.Context, opts *Options, arg string, f addFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}

	id, version, err := parseComponentRef(arg, true)
	if err != nil {
		return err
	}
	target := resolver.Ref{ID: id, Version: version}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	// 同 ID 同版本已存在：确认是否刷新缓存（--yes 直接刷新，004 §3.3）
	existing := hasComponent(cfg, id, version)
	refresh := f.refresh
	if existing {
		if f.yes {
			opts.Printf("ℹ️ %s 已存在于 brickkit.yaml，--yes 已指定：直接刷新缓存\n", target)
		} else {
			opts.Printf("⚠️ %s 已存在于 brickkit.yaml\n", target)
			if !confirm(opts, "   是否刷新 Manifest 与 artifacts 缓存？[y/N]: ") {
				opts.Printf("已取消，brickkit.yaml 未修改\n")
				return nil
			}
		}
		refresh = true
	}

	// 改动配置前先备份（003 §7.1），失败可用 brickkit reset --last 回退
	if _, err := backup.SaveLast(layout); err != nil {
		return err
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{Refresh: refresh})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	graph, err := resolver.New(resolver.FromSource(client)).Resolve(ctx, target)
	if err != nil {
		return err
	}

	// --repo / --repo-all 的资格检查放在写配置之前：
	// 闭源组件或目录已存在时直接失败，不留下"配置写了一半"的现场。
	clones, err := planClones(ctx, opts, client, layout, graph, target, f)
	if err != nil {
		return err
	}

	artifacts := downloadArtifacts(ctx, client, graph)
	added, err := writeComponents(layout, graph)
	if err != nil {
		return err
	}

	renderAddTree(opts, graph, target, artifacts)
	renderSignatures(opts, client.SignatureStatuses())
	renderWarnings(opts, graph.Warnings)
	renderWarnings(opts, artifacts.warnings)

	switch {
	case added > 0:
		opts.Printf("✅ 已写入 brickkit.yaml（%d 个组件）\n", added)
	case existing:
		opts.Printf("✅ 已刷新 %s 的 Manifest 与 artifacts 缓存\n", target)
	default:
		opts.Printf("✅ brickkit.yaml 未变更（组件已在配置中）\n")
	}
	if artifacts.downloaded > 0 {
		opts.Printf("📁 已下载 artifacts 到 .brickkit/artifacts/（%d 个文件）\n", artifacts.downloaded)
	} else if artifacts.cached > 0 {
		opts.Printf("📁 artifacts 已是最新（缓存中 %d 个文件）\n", artifacts.cached)
	}
	renderCoexistence(opts, layout, id)

	if err := runClones(ctx, opts, layout, clones, f); err != nil {
		return err
	}

	logging.Info("组件已添加",
		"component", target.String(),
		"written", added,
		"artifacts", artifacts.downloaded,
	)
	return nil
}

// ============================================================
// 产物下载
// ============================================================

type artifactSummary struct {
	downloaded int
	cached     int
	// perNode 是每个组件下载/命中的文件数，用于渲染树状输出。
	perNode  map[resolver.Ref]int
	warnings []*clierr.Error
}

func downloadArtifacts(ctx context.Context, client *source.Client, graph *resolver.Graph) *artifactSummary {
	sum := &artifactSummary{perNode: map[resolver.Ref]int{}}
	for _, node := range graph.Nodes {
		res, err := client.DownloadArtifacts(ctx, node.Manifest)
		if err != nil {
			// 产物是开发时辅助，不阻断安装（004 §10.1）
			sum.warnings = append(sum.warnings, clierr.As(err))
			continue
		}
		sum.downloaded += len(res.Downloaded)
		sum.cached += len(res.Cached)
		sum.perNode[node.Ref] = len(res.Downloaded) + len(res.Cached)
		sum.warnings = append(sum.warnings, res.Warnings...)
	}
	return sum
}

// ============================================================
// 写入 brickkit.yaml
// ============================================================

// writeComponents 把依赖图中尚未配置的组件写入 brickkit.yaml，返回新增条目数。
func writeComponents(layout config.Layout, graph *resolver.Graph) (int, error) {
	edit, err := config.OpenEdit(layout.ConfigPath())
	if err != nil {
		return 0, err
	}

	added := 0
	for _, node := range graph.Nodes {
		if edit.AddComponent(node.Ref.ID, node.Ref.Version) {
			added++
		}
	}
	if added == 0 {
		return 0, nil
	}
	if err := edit.Save(); err != nil {
		return 0, err
	}
	return added, nil
}

// ============================================================
// 输出渲染（004 §3.3 输出样例）
// ============================================================

func renderAddTree(opts *Options, graph *resolver.Graph, target resolver.Ref, artifacts *artifactSummary) {
	opts.Printf("📦 添加 %s\n", target)

	optionalOnly := dependencyKinds(graph)
	lines := make([]string, 0, len(graph.Nodes)+1)
	lines = append(lines, "Manifest ✅")
	for _, node := range graph.Nodes {
		if node.Ref == target {
			continue
		}
		label := "依赖"
		if optionalOnly[node.Ref] {
			label = "弱依赖"
		}
		line := label + " " + node.Ref.String() + " ✅ 已拉取"
		if n := artifacts.perNode[node.Ref]; n > 0 {
			line += "（artifacts " + itoa(n) + " 个文件）"
		}
		lines = append(lines, line)
	}
	if n := artifacts.perNode[target]; n > 0 {
		lines = append(lines, "artifacts ✅（"+itoa(n)+" 个文件）")
	}

	for i, line := range lines {
		branch := "├──"
		if i == len(lines)-1 {
			branch = "└──"
		}
		opts.Printf("   %s %s\n", branch, line)
	}
}

// renderSignatures 输出签名校验结果（008 §8 的「签名：✅ 已校验」）。
//
// 只报**验过的**：没验过的组件不列一行"未校验"，那会把正常状态渲染成一屏噪音
// ——绝大多数项目还没用上签名。真正需要使用者知道的落差（要求强制却没配公钥、
// 发布者不在信任列表）走警告，那才是该占屏幕的东西。
func renderSignatures(opts *Options, statuses []source.SignatureStatus) {
	for _, st := range statuses {
		if !st.Verified {
			continue
		}
		line := "🔏 签名：✅ 已校验 " + st.Ref()
		if st.Signature != nil && st.Signature.SignedBy != "" {
			line += "（发布者 " + st.Signature.SignedBy + "）"
		}
		opts.Printf("%s\n", line)
	}
	for _, st := range statuses {
		renderWarnings(opts, st.Warnings)
	}
}

func renderWarnings(opts *Options, warnings []*clierr.Error) {
	for _, w := range warnings {
		opts.Printf("%s", w.Format())
	}
}

// renderCoexistence 在同 ID 出现多个版本时提示多版本共存（002 §3.6）。
func renderCoexistence(opts *Options, layout config.Layout, id string) {
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return
	}
	versions := make([]string, 0, 2)
	for _, c := range cfg.Components {
		if c.ID == id {
			versions = append(versions, c.Version)
		}
	}
	if len(versions) < 2 {
		return
	}
	opts.Printf("ℹ️ %s 多版本共存：%s（各自生成独立容器与版本化服务名）\n",
		id, strings.Join(versions, "、"))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ============================================================
// --repo / --repo-all（004 §3.3）
// ============================================================

// clonePlan 是一个待 clone（或跳过）的组件。
type clonePlan struct {
	ref     resolver.Ref
	gitURL  string
	skip    bool
	skipMsg string
}

// planClones 解析 --repo / --repo-all 涉及组件的来源信息，并做资格检查。
func planClones(
	ctx context.Context,
	opts *Options,
	client *source.Client,
	layout config.Layout,
	graph *resolver.Graph,
	target resolver.Ref,
	f addFlags,
) ([]clonePlan, error) {
	if !f.repo && !f.repoAll {
		return nil, nil
	}

	refs := []resolver.Ref{target}
	if f.repoAll {
		refs = graph.Refs()
	}

	plans := make([]clonePlan, 0, len(refs))
	for _, ref := range refs {
		origin, err := client.Origin(ctx, ref.ID, ref.Version)
		if err != nil {
			return nil, err
		}

		switch {
		case origin.Type == source.OriginRegistry:
			if f.repo {
				// 004 §3.3 输出样例（闭源组件）
				return nil, clierr.New(clierr.CodeCloneFailed, "clone 失败：该组件为闭源组件").
					WithDetail("组件", ref.String()).
					WithDetail("来源类型", "registry（闭源）").
					WithDetail("原因", "闭源组件没有 Git 仓库，无法 clone 源码").
					WithTip("你仍然可以正常安装和使用该组件（不含 --repo）：",
						"   brickkit add "+ref.String())
			}
			plans = append(plans, clonePlan{ref: ref, skip: true, skipMsg: "闭源组件，跳过 clone"})
		case !origin.IsOpenSource():
			if f.repo {
				return nil, clierr.New(clierr.CodeCloneFailed, "clone 失败：没有可用的 Git 仓库地址").
					WithDetail("组件", ref.String()).
					WithDetail("安装源", origin.SourceID).
					WithDetail("原因", "该组件来自本地安装源（type: local），安装源本身没有记录 Git 仓库地址").
					WithHint(
						"改用市场或 git 类型的安装源安装该组件",
						"或去掉 --repo，直接使用本地目录中的源码",
					)
			}
			plans = append(plans, clonePlan{ref: ref, skip: true, skipMsg: "无 Git 仓库地址，跳过 clone"})
		case workspace.Exists(layout, ref.ID):
			if f.repo {
				return nil, clierr.New(clierr.CodeCloneFailed, "clone 失败：目录已存在").
					WithDetail("组件", ref.String()).
					WithDetail("目录", workspace.DisplayDir(ref.ID)).
					WithDetail("原因", "该目录已存在，可能包含你正在开发的组件源码").
					WithHint(
						"如果是误操作，请先删除或重命名该目录",
						"如果已有源码，无需再次 clone",
					)
			}
			// --repo-all 是批量操作：已有源码目录跳过即可，不该因为一个目录已存在就整体失败
			plans = append(plans, clonePlan{ref: ref, skip: true, skipMsg: "已有源码目录，跳过 clone"})
		default:
			plans = append(plans, clonePlan{ref: ref, gitURL: origin.GitURL})
		}
	}
	_ = opts
	return plans, nil
}

// runClones 执行 clone 并渲染结果。
func runClones(ctx context.Context, opts *Options, layout config.Layout, plans []clonePlan, f addFlags) error {
	if len(plans) == 0 {
		return nil
	}

	cloned, skipped := 0, 0
	for _, p := range plans {
		if p.skip {
			skipped++
			if f.repoAll {
				opts.Printf("   ⏭️ %-22s → %s\n", p.ref.ID, p.skipMsg)
			}
			continue
		}
		if _, err := workspace.Clone(ctx, layout, p.ref.ID, p.ref.String(), p.gitURL); err != nil {
			return err
		}
		cloned++
		if f.repoAll {
			opts.Printf("   ✅ %-22s → clone 完成（%s）\n", p.ref.ID, workspace.DisplayDir(p.ref.ID))
		}
	}

	switch {
	case f.repoAll:
		opts.Printf("📁 已 clone %d 个开源组件仓库（跳过 %d 个闭源组件）\n", cloned, skipped)
	case cloned > 0:
		opts.Printf("📁 已 clone 源码到 %s\n", workspace.DisplayDir(plans[0].ref.ID))
		opts.Printf("💡 如需修改源码并上传，请参考文档\"修改开源组件源码后如何上传\"\n")
	}

	logging.Info("源码 clone 完成", "cloned", cloned, "skipped", skipped)
	return nil
}
