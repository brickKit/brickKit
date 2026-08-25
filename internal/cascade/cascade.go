// Package cascade 计算"这次到底跑哪些组件"（003 §4.3）。
//
// 规则只有一句：
//
//	配置里有它、又没写 enabled: false，就启动。
//
// # 强依赖与弱依赖在"跑不跑"上一视同仁
//
// `optional: true` 管的是另外两件事，都不在这一层：
//
//	解析期   取不到它算警告，不算错误（强依赖取不到直接阻断）
//	注入期   它没在跑时不注入 *_ENDPOINT，让调用方自己降级
//
// 早先这里还有第三件事——"就算装了也别自动启动"。那条已经删掉：
// 它与"brickkit.yaml 就是声明"（012 §2.3：写了就启动）直接冲突，
// 造出了"配置里有它、`up` 却不起它"这种要专门写一节去解释的现象。
// 而它防的东西并不存在：起来了也不等于必启，调用方照样会在它挂掉时降级。
//
// 真正的代价是发现性——一堆弱依赖默认关着，使用者装了组件却发现一半功能
// 是哑的，而没有任何一处告诉他为什么。反过来（默认全开、嫌重再关）
// 失败方式温和得多：多几个容器，`docker ps` 里看得见，写一行 enabled: false 就没了。
//
// # 关掉一个组件不会连累别人
//
// 早先有一套"级联"：关掉一个组件，依赖链上无人需要的组件自动跟着跳过。
// 它已经删掉，理由是它在做一次**隐式**决定——而平台的原则是"精确优于隐式"。
// 现在关掉一个被强依赖的组件会**报错**并点名依赖方，由使用者自己决定
// 要不要把它们也关掉。想收窄这次启动范围，用 `up --only`（显式说要哪几个），
// 而不是关掉一个再让平台倒推还剩哪几个。
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
	// StateSkipped 表示这次没被点名（只在 `--only` 下出现）。
	StateSkipped State = "skipped"
)

// 判定理由，直接出现在 CLI 输出里。
const (
	reasonRunning     = "启动"
	reasonDisabled    = "显式禁用（enabled: false）"
	reasonNotSelected = "未被 --only 选中"
)

// Component 是一个组件的判定结果。
type Component struct {
	Ref   resolver.Ref
	State State
	// Reason 是给人看的判定理由。
	Reason string
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

// Compute 判定整个项目本次启动哪些组件。
//
// 没写 enabled 就是启动；写了 enabled: false 就是不启动。
// 启动中的组件如果强依赖了一个被显式关闭的组件，报错（见 disabledRequirementError）。
func Compute(cfg *config.Config, graph *resolver.Graph) (*Result, error) {
	return compute(cfg, graph, nil)
}

// Focus 按 `brickkit up --only` 判定：只启动点名的组件**及其全部依赖**。
//
// 依赖闭包强弱都算——既然"跑不跑"上两者一视同仁，收窄范围时也该一致。
// 只取强依赖的话，点名一个组件会得到一个异步功能是哑的它，
// 而这正是本包开头那段说的发现性问题。
//
// 显式关闭的组件即使落在闭包里也不启动：`enabled: false` 是长期声明，
// `--only` 是这一次的意图，前者更强。若被关掉的是**强**依赖，照常报错。
//
// only 为空时退化成 Compute。
func Focus(cfg *config.Config, graph *resolver.Graph, only []resolver.Ref) (*Result, error) {
	if len(only) == 0 {
		return Compute(cfg, graph)
	}
	return compute(cfg, graph, only)
}

// DependencyClosure 返回点名的组件加上它们递归依赖的全部组件（强弱都算）。
//
// 导出是给 `brickkit sync --only` 用的：它要留下的源码目录与
// `up --only` 会启动的组件必须是同一批，否则会出现"跑着的组件源码被归档了"。
// 两边各算一次迟早会分叉——这在合并前就真的分叉过（sync 取强依赖闭包、
// up 走级联，同一组参数给出不同的集合）。
func DependencyClosure(graph *resolver.Graph, roots []resolver.Ref) map[resolver.Ref]bool {
	keep := map[resolver.Ref]bool{}
	if graph == nil {
		return keep
	}

	var visit func(ref resolver.Ref)
	visit = func(ref resolver.Ref) {
		if keep[ref] {
			return
		}
		keep[ref] = true

		node := graph.Node(ref)
		if node == nil {
			return
		}
		for _, dep := range node.Requires {
			visit(dep)
		}
		for _, dep := range node.Optional {
			visit(dep)
		}
	}
	for _, ref := range roots {
		visit(ref)
	}
	return keep
}

// compute 是 Compute 与 Focus 的共同实现。focus 为 nil 表示不收窄。
func compute(cfg *config.Config, graph *resolver.Graph, focus []resolver.Ref) (*Result, error) {
	if graph == nil {
		return &Result{running: map[resolver.Ref]bool{}}, nil
	}

	decl := declarations(cfg)
	var selected map[resolver.Ref]bool
	if focus != nil {
		selected = DependencyClosure(graph, focus)
	}

	running := map[resolver.Ref]bool{}
	for _, node := range graph.Nodes {
		switch {
		case decl.disabled(node.Ref):
		case selected != nil && !selected[node.Ref]:
		default:
			running[node.Ref] = true
		}
	}

	if err := checkDisabledRequirements(graph, decl, running); err != nil {
		return nil, err
	}

	result := &Result{running: running}
	for _, node := range graph.Nodes {
		result.Components = append(result.Components, classify(node.Ref, decl, running))
	}
	return result, nil
}

// classify 把一个组件归入三态之一，并给出理由。
func classify(ref resolver.Ref, decl declSet, running map[resolver.Ref]bool) Component {
	switch {
	case running[ref]:
		return Component{Ref: ref, State: StateRunning, Reason: reasonRunning}
	case decl.disabled(ref):
		return Component{Ref: ref, State: StateDisabled, Reason: reasonDisabled}
	default:
		return Component{Ref: ref, State: StateSkipped, Reason: reasonNotSelected}
	}
}

// ============================================================
// brickkit.yaml 中的声明
// ============================================================

// declSet 是 brickkit.yaml 里对各组件的 enabled 声明。
//
// 依赖图里可能有配置里没写的组件（使用者手工编辑过配置），这类按"未禁用"处理。
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

func (d declSet) disabled(ref resolver.Ref) bool {
	enabled, ok := d[ref]
	return ok && enabled != nil && !*enabled
}

// ============================================================
// 强依赖被显式关闭
// ============================================================

// checkDisabledRequirements 拦下"启动中的组件强依赖了一个被关掉的组件"。
//
// 一次报全部，不是遇到第一个就返回：关掉一个底层组件往往同时影响好几个依赖方，
// 一次只说一个会让人"改一行、跑一次"来回好几趟。
func checkDisabledRequirements(
	graph *resolver.Graph, decl declSet, running map[resolver.Ref]bool,
) error {
	dependents := map[resolver.Ref][]string{}
	var order []resolver.Ref

	for _, node := range graph.Nodes {
		if !running[node.Ref] {
			continue
		}
		for _, dep := range node.Requires {
			if !decl.disabled(dep) {
				continue
			}
			if _, seen := dependents[dep]; !seen {
				order = append(order, dep)
			}
			dependents[dep] = append(dependents[dep], node.Ref.String())
		}
	}
	if len(order) == 0 {
		return nil
	}

	sort.Slice(order, func(i, j int) bool { return order[i].String() < order[j].String() })
	return disabledRequirementError(order, dependents)
}

// disabledRequirementError 报告"强依赖被显式关闭"。
//
// # 为什么是报错，而不是把依赖方也一起跳过
//
// 一起跳过是平台替使用者做了一次**隐式**决定：他关的是 A，平台顺手把 B、C 也停了，
// 而输出里那几行"级联跳过"很容易被当成正常现象略过去。等他发现 B 没跑，
// 线索已经断了。报错则把决定权交回去——要一起关就自己写上，一目了然。
//
// 弱依赖不在此列：那本来就是"有就用、没有就降级"，关掉它是完全正常的用法。
func disabledRequirementError(order []resolver.Ref, dependents map[resolver.Ref][]string) error {
	err := clierr.New(clierr.CodeComponentDisabled, "错误：强依赖被显式关闭")
	for _, ref := range order {
		list := dependents[ref]
		sort.Strings(list)
		err = err.WithDetailf("被关闭的组件", "%s（enabled: false）", ref)
		err = err.WithDetail("强依赖它的", strings.Join(dedupe(list), "、"))
	}

	first := order[0]
	return err.
		WithDetail("原因", "依赖方启动后必然连不上它；平台不会替你把依赖方一起关掉——"+
			"那是一次隐式决定，而它们没跑的线索只有输出里一闪而过的一行").
		WithHint(
			"要一起关掉就给依赖方也写 enabled: false",
			"或者移除 "+first.ID+" 的 enabled: false",
			"只是这一次想跑一个子集的话，用 brickkit up --only <组件>，不必动配置",
		)
}

func dedupe(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}
