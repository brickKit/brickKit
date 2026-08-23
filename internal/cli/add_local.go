package cli

// 本文件实现 brickkit add --local（004 §3.3）：把本地安装源里的组件一次全加进来。
//
// 与单组件 add 的区别在"批量"三个字上：要先把每个组件都解析通，再一次性写配置。
// 任何一个解析不通就整体中止、配置一个字节不动——半份配置比没有配置更难收拾。

import (
	"context"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/backup"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// localPlan 是一次 add --local 的计划：谁要装、谁跳过、为什么跳过。
type localPlan struct {
	// targets 是要安装的组件。
	targets []source.LocalComponent
	// configured 是已在 brickkit.yaml 中的同版本组件（静静跳过）。
	configured []string
	// conflicts 是同 ID 但版本不同的组件，跳过并单独提示。
	conflicts []versionConflict
}

// versionConflict 是"本地一个版本、配置里另一个版本"的落差。
type versionConflict struct {
	id       string
	local    string
	inConfig []string
}

func runAddLocal(ctx context.Context, opts *Options, f addFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	if !hasLocalSource(cfg) {
		return noLocalSourceError()
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{Refresh: f.refresh})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	found, err := client.LocalComponents(ctx)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		opts.Printf("📂 没有扫到组件\n")
		opts.Printf("   本地安装源里没有 <scope>/<name>/%s（003 §6.4）\n", "component.yaml")
		return nil
	}
	renderLocalScan(opts, found)

	plan := planLocalAdd(cfg, found)
	renderLocalSkips(opts, plan)
	if len(plan.targets) == 0 {
		opts.Printf("✅ brickkit.yaml 未变更（本地组件都已在配置中）\n")
		return nil
	}

	// 先把每个组件都解析通，再动配置。任一失败就整体中止。
	graphs, err := resolveLocalTargets(ctx, client, plan.targets)
	if err != nil {
		return err
	}

	if _, err := backup.SaveLast(layout); err != nil {
		return err
	}

	artifacts := downloadLocalArtifacts(ctx, client, graphs)
	added, err := writeLocalComponents(layout, graphs)
	if err != nil {
		return err
	}

	for i, g := range graphs {
		renderAddTree(opts, g, targetRef(plan.targets[i]), artifacts[i])
		renderWarnings(opts, g.Warnings)
		renderWarnings(opts, artifacts[i].warnings)
	}
	renderSignatures(opts, client.SignatureStatuses())

	if added == 0 {
		opts.Printf("✅ brickkit.yaml 未变更（组件已在配置中）\n")
	} else {
		opts.Printf("✅ 已写入 brickkit.yaml（%d 个组件）\n", added)
	}
	logging.Info("本地组件已批量添加", "scanned", len(found), "added", added)
	return nil
}

// targetRef 把本地组件转成解析器认的引用。
func targetRef(lc source.LocalComponent) resolver.Ref {
	return resolver.Ref{ID: lc.ID, Version: lc.Version}
}

// planLocalAdd 把扫到的组件分成三堆：要装的、已装的、版本对不上的。
func planLocalAdd(cfg *config.Config, found []source.LocalComponent) localPlan {
	var plan localPlan
	for _, lc := range found {
		switch {
		case hasComponent(cfg, lc.ID, lc.Version):
			plan.configured = append(plan.configured, lc.Ref())
		case len(otherVersions(cfg, lc.ID, lc.Version)) > 0:
			plan.conflicts = append(plan.conflicts, versionConflict{
				id: lc.ID, local: lc.Version, inConfig: otherVersions(cfg, lc.ID, lc.Version),
			})
		default:
			plan.targets = append(plan.targets, lc)
		}
	}
	return plan
}

// resolveLocalTargets 逐个解析依赖图。任一失败就带上"是哪个组件"的上下文返回。
func resolveLocalTargets(
	ctx context.Context, client *source.Client, targets []source.LocalComponent,
) ([]*resolver.Graph, error) {
	r := resolver.New(resolver.FromSource(client))
	graphs := make([]*resolver.Graph, 0, len(targets))
	for _, lc := range targets {
		graph, err := r.Resolve(ctx, targetRef(lc))
		if err != nil {
			return nil, localResolveError(lc, err)
		}
		graphs = append(graphs, graph)
	}
	return graphs, nil
}

// localResolveError 在原始错误前面点名是哪个组件卡住了，并说明配置没被改动。
//
// 批量操作里"报错但不说是谁"最折磨人：本地源里躺着十几个组件，
// 光看"强依赖缺失"根本不知道该去改哪一个。
func localResolveError(lc source.LocalComponent, cause error) error {
	e := clierr.As(cause)
	dup := *e
	details := []clierr.Detail{
		{Key: "卡在组件", Value: lc.Ref() + "（来自 " + lc.SourceID + "）"},
		{Key: "本次结果", Value: "已中止，brickkit.yaml 未修改"},
	}
	for _, d := range e.Details {
		// 底层已经附过一条"组件：xxx@1.0.0"，与上面那行说的是同一件事，去掉重复
		if d.Key == "组件" && d.Value == lc.Ref() {
			continue
		}
		details = append(details, d)
	}
	dup.Details = details
	dup.Hints = append(append([]string{}, e.Hints...),
		"修好该组件后重试，或先把它移出本地安装源目录")
	return &dup
}

// downloadLocalArtifacts 逐个下载产物。产物失败只警告，不阻断（与单组件 add 一致）。
func downloadLocalArtifacts(
	ctx context.Context, client *source.Client, graphs []*resolver.Graph,
) []*artifactSummary {
	out := make([]*artifactSummary, 0, len(graphs))
	for _, g := range graphs {
		out = append(out, downloadArtifacts(ctx, client, g))
	}
	return out
}

// writeLocalComponents 把所有依赖图里的组件一次性写进配置。
func writeLocalComponents(layout config.Layout, graphs []*resolver.Graph) (int, error) {
	edit, err := config.OpenEdit(layout.ConfigPath())
	if err != nil {
		return 0, err
	}

	added := 0
	for _, g := range graphs {
		for _, node := range g.Nodes {
			if edit.AddComponent(node.Ref.ID, node.Ref.Version) {
				added++
			}
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

// hasLocalSource 判断配置里有没有启用的本地安装源。
func hasLocalSource(cfg *config.Config) bool {
	for _, s := range cfg.Sources {
		if s.Type == config.SourceTypeLocal && s.IsEnabled() {
			return true
		}
	}
	return false
}

func noLocalSourceError() error {
	return clierr.New(clierr.CodeConfigInvalid, "错误：没有可用的本地安装源").
		WithDetail("原因", "brickkit.yaml → sources 中没有 type: local 的安装源（或都是 enabled: false）").
		WithHint(
			"--local 只扫本地安装源；市场与 Git 源请用 brickkit add <组件ID>",
			"本地开发可加一个：sources: - id: local-dev / type: local / path: ./components",
		).WithExit(clierr.ExitUsage)
}

// ============================================================
// 输出渲染
// ============================================================

// renderLocalScan 说清楚"扫了哪些源、各自有几个"。
//
// 只有一个源时不再逐源报数——"local-dev（2 个）扫到 2 个组件"是一句废话。
func renderLocalScan(opts *Options, found []source.LocalComponent) {
	bySource := map[string]int{}
	var order []string
	for _, lc := range found {
		if _, seen := bySource[lc.SourceID]; !seen {
			order = append(order, lc.SourceID)
		}
		bySource[lc.SourceID]++
	}
	sort.Strings(order)

	parts := make([]string, 0, len(order))
	for _, id := range order {
		if len(order) == 1 {
			parts = append(parts, id)
			continue
		}
		parts = append(parts, id+"（"+itoa(bySource[id])+" 个）")
	}
	opts.Printf("🔍 从本地安装源 %s 扫到 %d 个组件\n", strings.Join(parts, "、"), len(found))
}

func renderLocalSkips(opts *Options, plan localPlan) {
	for _, ref := range plan.configured {
		opts.Printf("⏭️ %s 已在 brickkit.yaml 中\n", ref)
	}
	for _, c := range plan.conflicts {
		opts.Printf("⚠️ %s 本地是 %s，配置里是 %s —— 已跳过\n",
			c.id, c.local, strings.Join(c.inConfig, "、"))
		opts.Printf("   要共存请显式执行 brickkit add %s@%s\n", c.id, c.local)
	}
}
