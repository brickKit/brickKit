package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// newOrderCommand 实现 brickkit order（004 §3.8）。
func newOrderCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "order",
		Short:   "查看组件启动顺序与依赖拓扑",
		GroupID: groupLifecycle,
		Long: `查看当前项目的启动顺序（004 §3.8）。

顺序由拓扑排序（Kahn 算法）得出：被依赖的组件排在前面。
弱依赖不参与排序约束——它可能根本不启动，因此只在"可跳过"一节列出。

本命令只读：不修改配置，也不启动任何容器。`,
		Example: `  brickkit order
  brickkit order --config brickkit.prod.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOrder(cmd.Context(), opts)
		},
	}
	return cmd
}

func runOrder(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	if len(cfg.Components) == 0 {
		opts.Printf("📋 当前项目没有组件\n")
		opts.Printf("   用 brickkit add <组件ID>@<版本> 添加第一个组件\n")
		return nil
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	graph, err := resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return err
	}
	// 先算"这次到底跑哪些组件"（003 §4.3），再对启动集合做拓扑排序。
	// 顺序反过来的话，输出里会出现根本不会启动的组件。
	states, err := cascade.Compute(cfg, graph)
	if err != nil {
		return err
	}

	plan, err := resolver.Order(graph.Subgraph(states.Running()))
	if err != nil {
		return err
	}

	renderWarnings(opts, graph.Warnings)
	renderStates(opts, states)

	if states.Empty() {
		opts.Printf("📋 本次没有组件会启动\n")
		opts.Printf("   把需要的组件改成 enabled: true，或移除 enabled: false\n")
		logging.Info("启动顺序已计算", "components", 0)
		return nil
	}
	renderOrder(opts, plan, graph)

	logging.Info("启动顺序已计算", "components", len(plan.Steps), "optional", len(plan.Optional))
	return nil
}

// renderStates 输出组件状态计算结果（003 §4.3 的输出样例）。
//
// 不启动的组件也要列出来并说明理由：否则使用者只会看到"我加的组件不见了"。
func renderStates(opts *Options, states *cascade.Result) {
	opts.Printf("📋 组件状态计算：\n")

	width := 0
	for _, c := range states.Components {
		if n := len(c.Ref.ID + "@" + c.Ref.Version); n > width {
			width = n
		}
	}
	for _, c := range states.Components {
		mark := "⬜"
		if c.State == cascade.StateRunning {
			mark = "✅"
		}
		opts.Printf("   %s %s  %s\n", mark, pad(c.Ref.ID+"@"+c.Ref.Version, width), c.Reason)
	}
	opts.Printf("\n")
}

// renderOrder 输出启动顺序、要点与依赖图（004 §3.8 输出样例）。
func renderOrder(opts *Options, plan *resolver.Plan, graph *resolver.Graph) {
	opts.Printf("📋 启动顺序（拓扑排序）：\n")

	width := 0
	for _, s := range plan.Steps {
		if n := len(s.Service); n > width {
			width = n
		}
	}
	for _, s := range plan.Steps {
		opts.Printf("   %d. %s  %s\n",
			s.Position, pad(s.Service, width), dependencyNote(s))
	}
	opts.Printf("\n")

	if independent := plan.Independent(); len(independent) > 0 {
		names := make([]string, 0, len(independent))
		for _, s := range independent {
			names = append(names, s.Service)
		}
		opts.Printf("可独立启动：%s（无依赖）\n", strings.Join(names, "、"))
	}
	if len(plan.Optional) > 0 {
		ids := make([]string, 0, len(plan.Optional))
		for _, ref := range plan.Optional {
			ids = append(ids, ref.ID)
		}
		opts.Printf("可跳过（弱依赖）：%s\n", strings.Join(ids, "、"))
	}
	if last := plan.Last(); last != nil && last.Position > 1 {
		opts.Printf("必须最后启动：%s（需等前 %d 个组件就绪）\n",
			last.Ref.ID, last.Position-1)
	}

	renderDependencyGraph(opts, plan, graph)
}

// dependencyNote 生成 "无依赖" 或 "← 依赖 1, 2"。
func dependencyNote(s resolver.PlanStep) string {
	if len(s.RequirePositions) == 0 {
		return "无依赖"
	}
	nums := make([]string, 0, len(s.RequirePositions))
	for _, p := range s.RequirePositions {
		nums = append(nums, strconv.Itoa(p))
	}
	return "← 依赖 " + strings.Join(nums, ", ")
}

// renderDependencyGraph 自上而下打印依赖关系：依赖方在前，被依赖的在后。
func renderDependencyGraph(opts *Options, plan *resolver.Plan, graph *resolver.Graph) {
	type line struct {
		from string
		deps []string
	}

	var lines []line
	for i := len(plan.Steps) - 1; i >= 0; i-- {
		ref := plan.Steps[i].Ref
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		var deps []string
		for _, dep := range node.Requires {
			deps = append(deps, dep.String())
		}
		for _, dep := range node.Optional {
			deps = append(deps, dep.String()+"（弱）")
		}
		for _, dep := range node.MissingOptional {
			deps = append(deps, dep.String()+"（弱，未安装）")
		}
		if len(deps) > 0 {
			lines = append(lines, line{from: ref.String(), deps: deps})
		}
	}
	if len(lines) == 0 {
		return
	}

	opts.Printf("\n依赖图：\n")
	for _, l := range lines {
		opts.Printf("   %s → %s\n", l.from, l.deps[0])
		for _, dep := range l.deps[1:] {
			opts.Printf("   %s → %s\n", strings.Repeat(" ", len([]rune(l.from))), dep)
		}
	}
}

// pad 在右侧补空格到指定宽度（服务名都是 ASCII，按字节对齐即可）。
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
