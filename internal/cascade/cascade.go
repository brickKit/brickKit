// Package cascade 计算"这次到底跑哪些组件"（003 §4.3）。
//
// # 规则：跟着上层走
//
//	顶层组件（没有任何组件依赖它）  没写 enabled → 跑；写 false → 不跑
//	下层组件                        没写 enabled → 跟上层：任一上层在跑，它就跑
//	任何组件                        写了 enabled → 按写的来，不看上层
//
// 一个组件被多个顶层共用时，只要还有一个上层在跑，它就跑。
//
// # 为什么这么写，而不是"没有启用中的组件需要它就不跑"
//
// 两者结论完全一样，但后者是**实现视角**：读的人要在脑子里把依赖图反着推一遍。
// 而"跟着上层走"是**使用者视角**——他要做的决定就是"这个顶层我要不要"，
// 下面那一串自然跟着走。
//
// 这不是措辞洁癖。原来的说法真的把人读岔过：照着它读完会得出
// "这条规则错了、应该删掉级联"的结论，而规则本身是对的。
//
// # 强依赖与弱依赖在"跟不跟得上"这件事上一视同仁
//
// 上层弱依赖它，它照样跟着跑。`optional: true` 管的是另外两件事：
//
//	解析期   取不到它算警告，不算错误（强依赖取不到直接阻断）
//	注入期   它没在跑时不注入 *_ENDPOINT，让调用方自己降级
//
// 早先这里还有第三件事——"只被弱依赖引用的组件不自动拉起"。已删除：
// 它让 `brickkit add` 写进配置的弱依赖默认不启动，使用者装了组件却发现
// 一半功能是哑的，而没有任何一处告诉他为什么。默认全开、嫌重再关掉，
// 失败方式温和得多：多几个容器，`docker ps` 里看得见。
package cascade

import (
	"sort"
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
	// StateSkipped 表示跟着上层一起不启动。
	StateSkipped State = "skipped"
)

// Component 是一个组件的判定结果。
type Component struct {
	Ref   resolver.Ref
	State State
	// Reason 是给人看的判定理由，直接出现在 CLI 输出里。
	Reason string
	// TopLevel 表示它是顶层组件（没有任何组件依赖它）。
	//
	// 输出里要标出来：使用者要关掉一批组件时，该动手的正是这些——
	// 关一个顶层，它下面那一串跟着走。不标的话他得自己把依赖图看一遍。
	TopLevel bool
}

// Result 是整个项目的启停判定结果。
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

// TopLevel 返回顶层组件，顺序与 Components 一致。
//
// 一个都不跑的时候，命令层要靠它说清楚"该去改哪一行"：顶层有两种死法，
// 指向的是配置里**不同的**行（自己被关掉 / 强依赖被关掉后跟着倒下）。
// 拿全体组件去猜会猜错——被关掉的那个往往根本不是顶层。
//
// 返回空表示图里没有顶层：每个组件都被别的组件依赖着（弱依赖成环）。
// 那时"跟着上层走"这句话解释不了任何事——上层是谁？没有上层。
func (r *Result) TopLevel() []Component {
	var out []Component
	for _, c := range r.Components {
		if c.TopLevel {
			out = append(out, c)
		}
	}
	return out
}

// Compute 按 003 §4.3 判定本次启动哪些组件。
//
// 算的是"**谁不跑**"，不是"谁跑"——两者互为补集，但只有前者能把环算对。
// 详见 computeStopped。
func Compute(cfg *config.Config, graph *resolver.Graph) (*Result, error) {
	if graph == nil {
		return &Result{running: map[resolver.Ref]bool{}}, nil
	}

	decl := declarations(cfg)
	stopped, blocker := computeStopped(graph, decl)

	// 钉住的组件撞上被关掉的强依赖 → 两个意图直接冲突，报错而不是二选一
	for _, node := range graph.Nodes {
		if !decl.pinned(node.Ref) {
			continue
		}
		if dep, hit := deadRequirement(node, stopped); hit {
			return nil, disabledDependencyError(node.Ref, dep, blocker)
		}
	}

	running := map[resolver.Ref]bool{}
	for _, node := range graph.Nodes {
		running[node.Ref] = !stopped[node.Ref]
	}

	result := &Result{running: running}
	for _, node := range graph.Nodes {
		result.Components = append(result.Components,
			classify(node, decl, stopped, running, blocker))
	}
	return result, nil
}

// computeStopped 求出"这次不跑"的组件集合。
//
// # 为什么算"谁不跑"而不是"谁跑"
//
// 直觉的写法是从种子（顶层 + 钉住的）出发沿依赖往下展开，谁被走到谁就跑。
// 那样算不对**环**：两个组件互相依赖时，谁都不是顶层，队列一开始就是空的，
// 结果是两个都不跑——而正确答案是两个都跑（环上没有更上层，它们互为顶层）。
//
// 反过来算就自然对了。种子是"显式关掉的"，然后两条传播规则轮流跑到稳定：
//
//	A  强依赖不跑 → 它也不跑（跑起来也必然连不上）
//	B  没写 enabled、有上层、而上层全都不跑 → 它也不跑
//
// 顶层组件没有上层，B 的前提永远不成立 → 除非显式关，否则一定跑。
// 环上的 A、B 互为上层，谁也没先倒下 → 两个都跑。不需要为环写任何特例。
//
// blocker 记下 A 规则里是被哪个组件挡住的，报错时要顺着它打印依赖链。
func computeStopped(
	graph *resolver.Graph, decl declSet,
) (stopped map[resolver.Ref]bool, blocker map[resolver.Ref]resolver.Ref) {
	stopped = map[resolver.Ref]bool{}
	blocker = map[resolver.Ref]resolver.Ref{}

	for _, node := range graph.Nodes {
		if decl.disabled(node.Ref) {
			stopped[node.Ref] = true
		}
	}

	for changed := true; changed; {
		changed = false
		for _, node := range graph.Nodes {
			// 钉住的组件不参与传播：它要么跑，要么在上面那一步就报错了
			if stopped[node.Ref] || decl.pinned(node.Ref) {
				continue
			}

			if dep, hit := deadRequirement(node, stopped); hit {
				stopped[node.Ref], blocker[node.Ref], changed = true, dep, true
				continue
			}
			if len(node.Dependents) > 0 && allStopped(node.Dependents, stopped) {
				stopped[node.Ref], changed = true, true
			}
		}
	}
	return stopped, blocker
}

// deadRequirement 找出该组件第一个不跑的**强**依赖。
//
// 只看强依赖：弱依赖不跑是正常状态，调用方按 002 §3.4 自行降级。
func deadRequirement(node *resolver.Node, stopped map[resolver.Ref]bool) (resolver.Ref, bool) {
	for _, dep := range node.Requires {
		if stopped[dep] {
			return dep, true
		}
	}
	return resolver.Ref{}, false
}

// allStopped 判断这些上层是否全都不跑。
func allStopped(dependents []resolver.Ref, stopped map[resolver.Ref]bool) bool {
	for _, ref := range dependents {
		if !stopped[ref] {
			return false
		}
	}
	return true
}

// classify 把一个组件归入三态之一，并给出理由。
func classify(
	node *resolver.Node, decl declSet,
	stopped, running map[resolver.Ref]bool, blocker map[resolver.Ref]resolver.Ref,
) Component {
	ref := node.Ref
	top := len(node.Dependents) == 0
	c := Component{Ref: ref, TopLevel: top}

	switch {
	case decl.disabled(ref):
		c.State, c.Reason = StateDisabled, "显式禁用（enabled: false）"

	case !stopped[ref]:
		c.State, c.Reason = StateRunning, runningReason(node, decl, running, top)

	case blocker[ref] != (resolver.Ref{}):
		c.State = StateSkipped
		c.Reason = "不启动（强依赖 " + blocker[ref].ID + " 不启动）"

	default:
		c.State, c.Reason = StateSkipped, "不启动（上层都不启动）"
	}
	return c
}

// runningReason 说明它为什么跑。
//
// 三种说法对应三种决定：顶层是使用者直接要的；写了 enabled: true 是他显式钉的；
// 其余都是"跟着上层"——这时要点出跟的是谁，否则他不知道该去关哪个。
func runningReason(
	node *resolver.Node, decl declSet, running map[resolver.Ref]bool, top bool,
) string {
	switch {
	case top:
		return "启动（顶层）"
	case decl.pinned(node.Ref):
		return "启动（enabled: true）"
	}

	// 多个上层在跑时取字典序最前的那个：同一份配置每次都要给出同一句话，
	// 否则连跑两次的输出不一样，使用者会以为哪里在飘
	parents := make([]string, 0, len(node.Dependents))
	for _, dep := range node.Dependents {
		if running[dep] {
			parents = append(parents, dep.ID)
		}
	}
	if len(parents) == 0 {
		return "启动"
	}
	sort.Strings(parents)
	return "启动（" + parents[0] + " 需要）"
}

// ============================================================
// brickkit.yaml 中的声明
// ============================================================

// declSet 是 brickkit.yaml 里对各组件的 enabled 声明。
//
// 依赖图里可能有配置里没写的组件（使用者手工编辑过配置），
// 这类组件按"没写 enabled"处理。
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
// 钉住的组件撞上被关掉的强依赖
// ============================================================

// disabledDependencyError 报告"你要它跑，又关掉了它跑起来必需的东西"。
//
// 打印完整依赖链：使用者关掉的是链条末端的某个组件，只说"强依赖不启动"
// 他还得自己反推是哪一条路径。
//
// 没钉住的组件遇到同样的情况**不报错**——那是"我关掉了一整条链"的正常操作，
// 跟着不跑正是他要的。报不报错的分界就在这里：他有没有说过"这个必须跑"。
func disabledDependencyError(
	pinned, dep resolver.Ref, blocker map[resolver.Ref]resolver.Ref,
) error {
	chain := []string{pinned.ID}
	current := dep
	for {
		chain = append(chain, current.ID)
		next, ok := blocker[current]
		if !ok {
			break
		}
		current = next
	}

	culprit := current
	return clierr.New(clierr.CodeComponentDisabled, "错误：强依赖 "+culprit.ID+" 被禁用").
		WithDetailf("组件", "%s@%s（enabled: true，已钉住）", pinned.ID, pinned.Version).
		WithDetail("依赖链", strings.Join(chain, " → ")).
		WithDetailf("被禁用的组件", "%s@%s", culprit.ID, culprit.Version).
		WithHint(
			"在 brickkit.yaml 中移除 "+culprit.ID+" 的 enabled: false",
			"或去掉 "+pinned.ID+" 的 enabled: true，让它随上层一起不启动",
		)
}
