package resolver

import (
	"sort"

	"github.com/brickkit/brickkit/internal/manifest"
)

// PlanStep 是启动顺序中的一步。
type PlanStep struct {
	// Position 是启动序号，从 1 开始。
	Position int
	// Ref 是组件引用。
	Ref Ref
	// Service 是版本化服务名（002 §5.3），部署文件与输出都用它。
	Service string
	// Requires 是该组件的直接强依赖（都排在它前面）。
	Requires []Ref
	// RequirePositions 是这些强依赖的启动序号（升序）。
	RequirePositions []int
}

// Plan 是一次拓扑排序的结果（004 §3.8 brickkit order）。
type Plan struct {
	// Steps 按启动顺序排列。
	Steps []PlanStep
	// Optional 是只被弱依赖引入的组件：它们可以不启动（004 §4.5）。
	Optional []Ref
	// Chain 是最长的一条**强依赖**链，从最底层排到最上层（启动的关键路径）。
	//
	// 没有任何强依赖边时它只有一个元素；图为空时为 nil。
	//
	// # 它替掉的是什么
	//
	// 输出里从前有一句"必须最后启动：X（需等前 N 个组件就绪）"，那个 N 取的是
	// **拓扑序号减一**。而序号是把整张图压平成的一条直线——它把毫不相干的另一条
	// 链上的组件也算进了"要等的前 N 个"。设计书自己的例子就是错的：
	// people/basic 只强依赖 department/tree（3 号），却写着"需等前 4 个组件就绪"，
	// 而紧挨着它的那张依赖图正在打脸。
	//
	// 使用者据此得到的印象是"整个 up 是串行的"，于是组件一多就以为启动会线性变慢。
	// 真正决定 up 时长的是这条链的长度，不在链上的组件是并行起的。
	Chain []Ref
}

// Independent 返回可独立启动的组件（没有强依赖）。
func (p *Plan) Independent() []PlanStep {
	var out []PlanStep
	for _, s := range p.Steps {
		if len(s.RequirePositions) == 0 {
			out = append(out, s)
		}
	}
	return out
}

// Order 用 Kahn 算法对依赖图做拓扑排序（004 §4.3）。
//
// 只有**强依赖**参与排序约束：弱依赖可能根本不启动（004 §4.5），
// 让它约束顺序等于把"可选"偷偷变成"必选"。
//
// 同一层内按组件 ID + 版本排序，保证同一份依赖图每次都得到完全相同的顺序——
// 生成的部署文件要可复现，不能因为 map 遍历顺序而变来变去。
func Order(g *Graph) (*Plan, error) {
	plan := &Plan{}
	if g == nil || len(g.Nodes) == 0 {
		return plan, nil
	}

	nodes := make(map[Ref]*Node, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes[n.Ref] = n
	}

	indegree := make(map[Ref]int, len(g.Nodes))
	dependents := make(map[Ref][]Ref, len(g.Nodes))
	for _, n := range g.Nodes {
		for _, dep := range n.Requires {
			if _, ok := nodes[dep]; !ok {
				continue // 不在图里的依赖（不该发生）不参与排序
			}
			indegree[n.Ref]++
			dependents[dep] = append(dependents[dep], n.Ref)
		}
	}

	ready := make([]Ref, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if indegree[n.Ref] == 0 {
			ready = append(ready, n.Ref)
		}
	}
	sortRefs(ready)

	position := make(map[Ref]int, len(g.Nodes))
	for len(ready) > 0 {
		ref := ready[0]
		ready = ready[1:]

		step := PlanStep{
			Position: len(plan.Steps) + 1,
			Ref:      ref,
			Service:  manifest.ServiceName(ref.ID, ref.Version),
		}
		position[ref] = step.Position
		for _, dep := range nodes[ref].Requires {
			if _, ok := nodes[dep]; !ok {
				continue
			}
			step.Requires = append(step.Requires, dep)
			step.RequirePositions = append(step.RequirePositions, position[dep])
		}
		sort.Ints(step.RequirePositions)
		plan.Steps = append(plan.Steps, step)

		var freed []Ref
		for _, d := range dependents[ref] {
			indegree[d]--
			if indegree[d] == 0 {
				freed = append(freed, d)
			}
		}
		if len(freed) > 0 {
			ready = append(ready, freed...)
			sortRefs(ready)
		}
	}

	if len(plan.Steps) < len(g.Nodes) {
		return nil, cycleAmong(nodes, indegree)
	}

	plan.Optional = optionalOnlyRefs(g, nodes)
	plan.Chain = longestChain(plan.Steps, nodes)
	return plan, nil
}

// longestChain 求最长的一条强依赖链（关键路径）。
//
// Steps 已是拓扑序（依赖排在依赖方之前），所以一趟 DP 就够：
// 每个组件的深度 = 它最深的那个强依赖的深度 + 1。
//
// 弱依赖不参与：它可能根本不启动，让它撑长关键路径等于把"可选"偷偷变成"必选"
// （与 Order 的排序约束同一条规则）。
//
// 一样长的链要挑出稳定的那一条：Steps 本身是确定的（同层按 ID + 版本排序），
// 所以只在**严格更深**时才换人，相同深度保留先出现的那个。
func longestChain(steps []PlanStep, nodes map[Ref]*Node) []Ref {
	if len(steps) == 0 {
		return nil
	}

	depth := make(map[Ref]int, len(steps))
	from := make(map[Ref]Ref, len(steps))
	var deepest Ref
	best := 0

	for _, step := range steps {
		d, predecessor := 1, Ref{}
		for _, dep := range nodes[step.Ref].Requires {
			if _, ok := nodes[dep]; !ok {
				continue
			}
			if depth[dep]+1 > d {
				d, predecessor = depth[dep]+1, dep
			}
		}
		depth[step.Ref] = d
		if predecessor != (Ref{}) {
			from[step.Ref] = predecessor
		}
		if d > best {
			best, deepest = d, step.Ref
		}
	}

	chain := make([]Ref, 0, best)
	for ref := deepest; ; {
		chain = append(chain, ref)
		previous, ok := from[ref]
		if !ok {
			break
		}
		ref = previous
	}
	// 回溯是从上往下走的，反过来才是启动顺序
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// optionalOnlyRefs 找出"只被弱依赖引入"的组件。
// 只要有任何组件强依赖它，它就是必须启动的，不算可跳过。
func optionalOnlyRefs(g *Graph, nodes map[Ref]*Node) []Ref {
	optional := map[Ref]bool{}
	for _, n := range g.Nodes {
		for _, ref := range n.Optional {
			if _, seen := optional[ref]; !seen {
				optional[ref] = true
			}
		}
		for _, ref := range n.Requires {
			optional[ref] = false
		}
	}

	var out []Ref
	for ref, only := range optional {
		if only && nodes[ref] != nil {
			out = append(out, ref)
		}
	}
	sortRefs(out)
	return out
}

// cycleAmong 在排序剩下的节点里找出一个真实的环，用于报错。
func cycleAmong(nodes map[Ref]*Node, indegree map[Ref]int) error {
	remaining := make([]Ref, 0, len(indegree))
	for ref, deg := range indegree {
		if deg > 0 {
			remaining = append(remaining, ref)
		}
	}
	sortRefs(remaining)

	state := map[Ref]int{} // 0 未访问 / 1 在路径上 / 2 已完成
	var path []Ref
	var walk func(ref Ref) error
	walk = func(ref Ref) error {
		state[ref] = 1
		path = append(path, ref)
		for _, dep := range nodes[ref].Requires {
			if _, ok := nodes[dep]; !ok || indegree[dep] == 0 {
				continue
			}
			switch state[dep] {
			case 1:
				return cycleError(path, dep)
			case 0:
				if err := walk(dep); err != nil {
					return err
				}
			}
		}
		state[ref] = 2
		path = path[:len(path)-1]
		return nil
	}

	for _, ref := range remaining {
		if state[ref] != 0 {
			continue
		}
		path = path[:0]
		if err := walk(ref); err != nil {
			return err
		}
	}
	// 走到这里说明入度统计与图不一致，属于内部错误；仍然把涉及的组件报出来
	return cycleError(remaining, remaining[0])
}

// sortRefs 按组件 ID、版本升序排序，保证顺序可复现。
func sortRefs(refs []Ref) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ID != refs[j].ID {
			return refs[i].ID < refs[j].ID
		}
		return manifest.CompareVersions(refs[i].Version, refs[j].Version) < 0
	})
}
