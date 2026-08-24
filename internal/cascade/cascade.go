// Package cascade 计算"这次到底跑哪些组件"（003 §4.3）。
//
// brickkit.yaml 里的 enabled 是三态字段：
//
//	enabled: true   钉住，无论如何都要跑
//	不写            默认开启，但可被级联关闭
//	enabled: false  显式关闭，一定不跑
//
// 级联的意义在于：使用者关掉一个后端，不用再手工把它下面那一串依赖也逐个关掉。
package cascade

import (
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/resolver"
)

// State 是一个组件这次的启停状态。
type State string

const (
	// StateRunning 表示本次会启动。
	StateRunning State = "running"
	// StateDisabled 表示被 enabled: false 显式关闭。
	StateDisabled State = "disabled"
	// StateSkipped 表示被级联跳过（没人需要它，或它的强依赖不启动）。
	StateSkipped State = "skipped"
)

// Component 是一个组件的判定结果。
type Component struct {
	Ref   resolver.Ref
	State State
	// Reason 是给人看的判定理由，直接出现在 CLI 输出里。
	Reason string
}

// Result 是整个项目的级联计算结果。
type Result struct {
	// Components 按依赖图顺序排列（依赖先于依赖方）。
	Components []Component

	running map[resolver.Ref]bool
}

// IsRunning 判断某个组件本次是否启动。
func (r *Result) IsRunning(ref resolver.Ref) bool { return r.running[ref] }

// Running 返回本次启动的组件，顺序与 Components 一致。
func (r *Result) Running() []resolver.Ref {
	out := make([]resolver.Ref, 0, len(r.running))
	for _, c := range r.Components {
		if c.State == StateRunning {
			out = append(out, c.Ref)
		}
	}
	return out
}

// Empty 表示这次一个组件都不启动。
func (r *Result) Empty() bool { return len(r.Running()) == 0 }

// Compute 按 003 §4.3 计算级联结果。
//
// 算法分三步：
//
//  1. **可行性**：显式关闭的组件不可行；强依赖不可行的组件也不可行
//     （它跑起来也必然连不上依赖）。这一步是自下而上的传递闭包。
//  2. **种子**：钉住的组件 + 根组件（没有任何组件依赖它的入口组件），
//     取其中可行的。
//  3. **闭包**：从种子出发沿**强依赖**展开——只有强依赖会被级联拉起，
//     弱依赖本来就是"有就用、没有就降级"，要它跑就显式钉住。
//
// 钉住的组件如果不可行，说明使用者的两个意图直接冲突（既要它跑、又关掉了它的
// 强依赖），这时报错而不是静默跳过（004 §10.3，延后项 P14）。
func Compute(cfg *config.Config, graph *resolver.Graph) (*Result, error) {
	return compute(cfg, graph, nil)
}

// Focus 按 `brickkit up --only` 计算：**点名的组件就是种子**。
//
// # 为什么不能拿 Compute 的结果做交集
//
// 那是原来的做法（`Result.Restrict`），它把 `--only` 理解成"在会启动的那些里
// 再挑几个"。于是点名一个**只被弱依赖引用**的组件时，什么都不会启动——
// 它本来就不在级联结果里（003 §4.3：弱依赖不会被自动拉起）。
// 而 004 §3.5 承诺的是"只启动指定组件及其依赖"。
//
// 正确的理解是：级联回答的是"**你没说的时候**该跑什么"。你在命令行上点了名，
// 那就是最明确的意图，与 `enabled: true` 同级——所以点名等于钉住，
// 根组件则不再自动启动（那正是 `--only` 要收窄掉的东西）。
//
// 这也让一件事自动正确：点名的组件的**强依赖被关掉**时照样报错，
// 与钉住的组件走同一条路。
//
// only 为空时退化成 Compute。
func Focus(cfg *config.Config, graph *resolver.Graph, only []resolver.Ref) (*Result, error) {
	if len(only) == 0 {
		return Compute(cfg, graph)
	}
	return compute(cfg, graph, only)
}

// compute 是 Compute 与 Focus 的共同实现。
//
// focus 为 nil 时按 003 §4.3 的三态 + 根组件计算；非 nil 时只有 focus 里的组件
// 是种子。两条路共用可行性传播与强依赖闭包——分开写迟早会出现
// "up 会启动它、up --only 点名它却不启动"这种自相矛盾。
func compute(cfg *config.Config, graph *resolver.Graph, focus []resolver.Ref) (*Result, error) {
	if graph == nil {
		return &Result{running: map[resolver.Ref]bool{}}, nil
	}

	decl := declarations(cfg)
	viable, blocker := computeViability(graph, decl)
	focused := setOf(focus)

	// 必须跑、却不可行 → 意图冲突，必须报错
	for _, node := range graph.Nodes {
		mustRun, why := decl.pinned(node.Ref), "enabled: true，已钉住"
		if focus != nil {
			mustRun, why = focused[node.Ref], "被 --only 点名"
		}
		if mustRun && !viable[node.Ref] {
			return nil, disabledDependencyError(graph, node.Ref, blocker, why)
		}
	}

	running, reason := computeRunning(graph, decl, viable, focused, focus != nil)

	result := &Result{running: running}
	for _, node := range graph.Nodes {
		result.Components = append(result.Components,
			classify(graph, node, decl, viable, running, reason, blocker, focus != nil))
	}
	return result, nil
}

func setOf(refs []resolver.Ref) map[resolver.Ref]bool {
	out := make(map[resolver.Ref]bool, len(refs))
	for _, ref := range refs {
		out[ref] = true
	}
	return out
}

// classify 把一个组件归入三态之一，并给出理由。
func classify(
	graph *resolver.Graph, node *resolver.Node, decl declSet, viable map[resolver.Ref]bool,
	running map[resolver.Ref]bool, reason map[resolver.Ref]string,
	blocker map[resolver.Ref]resolver.Ref, focusMode bool,
) Component {
	ref := node.Ref
	switch {
	case decl.disabled(ref):
		return Component{Ref: ref, State: StateDisabled, Reason: "显式禁用（enabled: false）"}

	case running[ref]:
		return Component{Ref: ref, State: StateRunning, Reason: reason[ref]}

	case focusMode:
		// --only 模式下别的理由都不准确："没有启用中的组件依赖它"听上去像
		// 配置有问题，而真实原因只是这一次没点它的名
		return Component{Ref: ref, State: StateSkipped, Reason: "未被 --only 选中"}

	case !viable[ref]:
		return Component{Ref: ref, State: StateSkipped,
			Reason: "级联跳过（强依赖 " + blocker[ref].ID + " 不启动）"}

	case onlyWeaklyNeeded(graph, ref):
		// 说清楚"为什么没跑"与"想让它跑该怎么办"：
		// 弱依赖不会被级联拉起，这是规则，不是漏算。
		return Component{Ref: ref, State: StateSkipped,
			Reason: "级联跳过（只被弱依赖引用；需要它时请显式写 enabled: true）"}

	default:
		return Component{Ref: ref, State: StateSkipped,
			Reason: "级联跳过（没有启用中的组件依赖它）"}
	}
}

// onlyWeaklyNeeded 判断某个组件是否只被弱依赖指向。
func onlyWeaklyNeeded(graph *resolver.Graph, ref resolver.Ref) bool {
	node := graph.Node(ref)
	if node == nil || len(node.Dependents) == 0 {
		return false
	}
	for _, dependent := range node.Dependents {
		parent := graph.Node(dependent)
		if parent == nil {
			continue
		}
		for _, required := range parent.Requires {
			if required == ref {
				return false
			}
		}
	}
	return true
}

// ============================================================
// brickkit.yaml 中的声明
// ============================================================

// declSet 是 brickkit.yaml 里对各组件的 enabled 声明。
//
// 依赖图里可能有配置里没写的组件（使用者手工编辑过配置），
// 这类组件按"未钉住、未禁用"处理。
type declSet map[resolver.Ref]*bool

func declarations(cfg *config.Config) declSet {
	out := declSet{}
	if cfg == nil {
		return out
	}
	for _, c := range cfg.Components {
		out[resolver.Ref{ID: c.ID, Version: c.Version}] = c.Enabled
	}
	return out
}

func (d declSet) pinned(ref resolver.Ref) bool {
	enabled, ok := d[ref]
	return ok && enabled != nil && *enabled
}

func (d declSet) disabled(ref resolver.Ref) bool {
	enabled, ok := d[ref]
	return ok && enabled != nil && !*enabled
}

// ============================================================
// 可行性
// ============================================================

// computeViability 算出每个组件"能不能跑"，以及不能跑时是被谁挡住的。
//
// 不可行是会传染的：A 强依赖 B，B 不可行 → A 也不可行。因此反复迭代到稳定，
// 而不是只看一层。
func computeViability(
	graph *resolver.Graph, decl declSet,
) (viable map[resolver.Ref]bool, blocker map[resolver.Ref]resolver.Ref) {
	viable = map[resolver.Ref]bool{}
	blocker = map[resolver.Ref]resolver.Ref{}

	for _, node := range graph.Nodes {
		viable[node.Ref] = !decl.disabled(node.Ref)
	}

	for changed := true; changed; {
		changed = false
		for _, node := range graph.Nodes {
			if !viable[node.Ref] {
				continue
			}
			for _, dep := range node.Requires {
				// 依赖图里没有的强依赖不在这里处理：解析阶段已经报过错了
				if _, known := blocker[dep]; !viable[dep] && graph.Has(dep) {
					_ = known
					viable[node.Ref] = false
					blocker[node.Ref] = dep
					changed = true
					break
				}
			}
		}
	}
	return viable, blocker
}

// ============================================================
// 实际启动集合
// ============================================================

// computeRunning 从种子出发沿强依赖展开，得到实际启动的组件与各自的理由。
func computeRunning(
	graph *resolver.Graph, decl declSet, viable map[resolver.Ref]bool,
	focused map[resolver.Ref]bool, focusMode bool,
) (map[resolver.Ref]bool, map[resolver.Ref]string) {
	running := map[resolver.Ref]bool{}
	reason := map[resolver.Ref]string{}

	var queue []resolver.Ref
	seed := func(ref resolver.Ref, why string) {
		if running[ref] || !viable[ref] {
			return
		}
		running[ref] = true
		reason[ref] = why
		queue = append(queue, ref)
	}

	for _, node := range graph.Nodes {
		switch {
		case focusMode:
			// 点名的就是全部种子。根组件不再自动启动——把它们收窄掉
			// 正是 `--only` 的意思
			if focused[node.Ref] {
				seed(node.Ref, "被 --only 点名")
			}
		case decl.pinned(node.Ref):
			seed(node.Ref, "显式启用（钉住）")
		case isRoot(node) && !decl.disabled(node.Ref):
			seed(node.Ref, "根组件（没有其他组件依赖它）")
		}
	}

	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]

		node := graph.Node(ref)
		if node == nil {
			continue
		}
		for _, dep := range node.Requires {
			seed(dep, "被 "+ref.ID+" 依赖")
		}
	}
	return running, reason
}

// isRoot 判断组件是不是入口组件：没有任何组件（强或弱）依赖它。
//
// 只被弱依赖指向的组件不算根：弱依赖意味着"用得上就用"，
// 让它自己成为入口会把"可选"悄悄变成"必启"。
func isRoot(node *resolver.Node) bool { return len(node.Dependents) == 0 }

// ============================================================
// P14：强依赖被禁用
// ============================================================

// disabledDependencyError 报告"钉住的组件依赖了一个被关掉的组件"。
//
// 错误里给出完整依赖链：使用者关的是链条末端的某个组件，
// 只说"强依赖被禁用"他还得自己反推是哪一条路径。
func disabledDependencyError(
	graph *resolver.Graph, pinned resolver.Ref, blocker map[resolver.Ref]resolver.Ref,
	why string,
) error {
	chain := []string{pinned.ID}
	current := pinned
	for {
		next, ok := blocker[current]
		if !ok {
			break
		}
		chain = append(chain, next.ID)
		current = next
	}

	culprit := current
	return clierr.New(clierr.CodeComponentDisabled, "错误：强依赖 "+culprit.ID+" 被禁用").
		WithDetail("组件", pinned.ID+"@"+pinned.Version+"（"+why+"）").
		WithDetail("依赖链", strings.Join(chain, " → ")).
		WithDetailf("被禁用的组件", "%s@%s", culprit.ID, culprit.Version).
		WithHint(
			"在 brickkit.yaml 中移除 "+culprit.ID+" 的 enabled: false",
			"或别再要求 "+pinned.ID+" 必须运行，让它随依赖一起被级联跳过",
		)
}
