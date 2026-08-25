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
//
// 每行的理由里带着"（顶层）"或"（X 需要）"，这是使用者关组件时唯一的抓手：
// 关一个顶层，它下面那一串跟着走；关一个中间层，只有它自己那条支线受影响。
// 不标的话他得先把依赖图在脑子里过一遍才知道该动哪个。
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
		// 这一行回答的是"哪些是可以关掉的"。
		//
		// 只被弱依赖引用的组件照常启动（003 §4.3：它跟着上层走），
		// 但关掉它们不会连累任何人——调用方拿不到 *_ENDPOINT，自己降级。
		// 嫌容器太多时，这里就是那份可以下手的名单。
		opts.Printf("只被弱依赖引用（关掉不影响别人：enabled: false）：%s\n",
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
