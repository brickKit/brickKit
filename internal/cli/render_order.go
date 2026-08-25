package cli

import (
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/resolver"
)

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
		// 措辞要点：不是"可以跳过"，是"默认就不跑"。
		//
		// 从前这里写的是"可跳过（弱依赖）"，它暗示这些组件本来会启动、
		// 你可以选择跳过——而级联的规矩恰好相反：只被弱依赖引用的组件
		// **默认不启动**（003 §4.3），要它跑得显式写 enabled: true。
		// 一句反过来的提示比没有提示更糟：使用者会去 docker ps 里找它。
		opts.Printf("只被弱依赖引用（默认不启动，需要时写 enabled: true）：%s\n",
			strings.Join(ids, "、"))
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
