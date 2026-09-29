package shell

// 本文件回答"一个工作负载（容器 / Pod）这次真正要连谁"。
//
// 外壳进程是成员的物理宿主：成员的代码跑在外壳里，成员要连的依赖就是外壳容器要连的依赖。
// 启动顺序（depends_on）、寻址（extra_hosts）、网络策略（egress）都按这一份算——
// 只看外壳自己的依赖，外壳就会在成员需要的东西还没起来时启动、解析不了成员要用的主机名、
// 被网络策略挡在成员的依赖外面。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// Dependencies 返回 ref 的工作负载这次真正要连的强依赖与弱依赖：ref 自己的依赖，加上它这次
// 承载的成员的依赖；去掉它承载的成员本身（进程内调用，不走网络）与它自己。
// 不是外壳的组件就是它自己的依赖。不按跑没跑过滤——那由调用方按各自的规则决定。
func Dependencies(
	p *project.Project, graph *resolver.Graph, states *cascade.Result, ref resolver.Ref,
) (requires, optional []resolver.Ref) {
	return dependencies(p, graph, states, ref, false)
}

// WaitFor 返回 ref 的工作负载启动前要等的强依赖：Dependencies 的强依赖，去掉各来源条目
// （组件自己，外壳还有它承载的每个成员）在 skipWaitFor 里写的那些（附录 A23）。两个成员都依赖
// erp/pay、只有一个写了 skipWaitFor 时照样要等——另一个成员没接受这个代价。
// 只管"等"，不管"连"：网络策略、extra_hosts 仍按 Dependencies。
func WaitFor(p *project.Project, graph *resolver.Graph, states *cascade.Result, ref resolver.Ref) []resolver.Ref {
	requires, _ := dependencies(p, graph, states, ref, true)
	return requires
}

// SkippedWaits 返回 ref 的工作负载因 skipWaitFor 而不等的强依赖（Dependencies 减去 WaitFor）。
func SkippedWaits(p *project.Project, graph *resolver.Graph, states *cascade.Result, ref resolver.Ref) []resolver.Ref {
	all, _ := Dependencies(p, graph, states, ref)
	waited := map[resolver.Ref]bool{}
	for _, dep := range WaitFor(p, graph, states, ref) {
		waited[dep] = true
	}
	var out []resolver.Ref
	for _, dep := range all {
		if !waited[dep] {
			out = append(out, dep)
		}
	}
	return out
}

// skipApplies 报告 ref 这个工作负载采不采纳 skipWaitFor：只有 docker / podman 上的容器才有
// depends_on 可去。K8s 的 Pod 之间没有启动顺序，裸进程（含裸进程外壳里的成员）不在 compose
// 文件里——这两种情况下它不起作用（附录 A23），启动顺序也就不能照它排。
func skipApplies(p *project.Project, states *cascade.Result, ref resolver.Ref) bool {
	if p.Deploy.Target == deployfile.TargetK8s || p.DeployEntry(ref.ID, ref.Version).IsBareProcess() {
		return false
	}
	host, hosted := states.HostOf(p, ref)
	return !hosted || !p.DeployEntry(host.ID, host.Version).IsBareProcess()
}

func dependencies(
	p *project.Project, graph *resolver.Graph, states *cascade.Result, ref resolver.Ref, honorSkip bool,
) (requires, optional []resolver.Ref) {
	honorSkip = honorSkip && skipApplies(p, states, ref)
	sources := []resolver.Ref{ref}
	inside := map[resolver.Ref]bool{ref: true}
	for _, running := range states.Running() {
		if host, ok := states.HostOf(p, running); ok && host == ref {
			sources = append(sources, running)
			inside[running] = true
		}
	}

	seenRequired, seenOptional := map[resolver.Ref]bool{}, map[resolver.Ref]bool{}
	for _, source := range sources {
		node := graph.Node(source)
		if node == nil {
			continue
		}
		skipped := map[string]bool{}
		if honorSkip {
			for _, id := range p.DeployEntry(source.ID, source.Version).SkipWaitFor {
				skipped[id] = true
			}
		}
		for _, dep := range node.Requires {
			if !inside[dep] && !skipped[dep.ID] {
				seenRequired[dep] = true
			}
		}
		for _, dep := range node.Optional {
			if !inside[dep] {
				seenOptional[dep] = true
			}
		}
	}
	for dep := range seenRequired {
		requires = append(requires, dep)
		delete(seenOptional, dep) // 有人强依赖它，就按强依赖算
	}
	for dep := range seenOptional {
		optional = append(optional, dep)
	}
	sortRefs(requires)
	sortRefs(optional)
	return requires, optional
}

func sortRefs(refs []resolver.Ref) {
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
}

// Workloads 返回这次真正要起的工作负载组成的依赖图：只含这次在跑的组件，外壳承载的成员
// 并进外壳——强依赖是 WaitFor 的结果（外壳的是它自己的加上成员的，去掉 skipWaitFor），别的
// 组件对成员的依赖改成对外壳的依赖。启动顺序按它排，与生成文件里的 depends_on 说的是同一件事。
func Workloads(p *project.Project, graph *resolver.Graph, states *cascade.Result) *resolver.Graph {
	target := func(ref resolver.Ref) resolver.Ref {
		if host, ok := states.HostOf(p, ref); ok {
			return host
		}
		return ref
	}
	remap := func(self resolver.Ref, refs []resolver.Ref) []resolver.Ref {
		var out []resolver.Ref
		seen := map[resolver.Ref]bool{self: true}
		for _, ref := range refs {
			if t := target(ref); !seen[t] && states.IsRunning(t) {
				seen[t] = true
				out = append(out, t)
			}
		}
		return out
	}

	var nodes []*resolver.Node
	for _, ref := range states.Running() {
		if _, hosted := states.HostOf(p, ref); hosted {
			continue
		}
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		// 普通组件保留 Manifest 里的声明顺序（启动顺序与最长链的并列项按它排），只滤掉 skipWaitFor；
		// 外壳的依赖是合并出来的，用 WaitFor
		requires, optional := node.Requires, node.Optional
		if skipApplies(p, states, ref) {
			requires = withoutSkipped(requires, p.DeployEntry(ref.ID, ref.Version).SkipWaitFor)
		}
		if p.Decl.IsShellID(ref.ID) {
			requires = WaitFor(p, graph, states, ref)
			_, optional = Dependencies(p, graph, states, ref)
		}
		nodes = append(nodes, &resolver.Node{
			Ref: ref, Manifest: node.Manifest,
			Requires: remap(ref, requires), Optional: remap(ref, optional),
			MissingOptional: node.MissingOptional,
		})
	}
	return resolver.NewGraph(nodes)
}

// MergeCycleError 解释一个只在工作负载图里才有的环（Workloads 把成员并进外壳之后才出现）：
// 组件层面谁也没依赖谁成环，是"外壳里的成员 A 依赖外面的 X、X 又依赖外壳里的成员 B"把外壳与 X
// 绑成了互等。报错点名每一条造成它的组件依赖，使用者才知道该动哪一条。work 是 Workloads 的结果。
//
// 返回 nil 表示这个环不拦人：找不到环，或者环上有以裸进程运行的外壳——它不在 compose 文件里，
// 没有 depends_on，也就不存在互等（启动顺序只是参考）。
func MergeCycleError(p *project.Project, graph *resolver.Graph, states *cascade.Result, work *resolver.Graph) error {
	cycle := findCycle(work)
	if len(cycle) == 0 {
		return nil
	}
	for _, ref := range cycle {
		if p.Decl.IsShellID(ref.ID) && p.DeployEntry(ref.ID, ref.Version).IsBareProcess() {
			return nil
		}
	}
	inside := func(ref resolver.Ref) []resolver.Ref {
		out := []resolver.Ref{ref}
		for _, running := range states.Running() {
			if host, ok := states.HostOf(p, running); ok && host == ref {
				out = append(out, running)
			}
		}
		return out
	}
	describe := func(ref resolver.Ref) string {
		if host, ok := states.HostOf(p, ref); ok {
			return i18n.T(msgid.ShellMergeCycleInShell, ref.String(), host.String())
		}
		return ref.String()
	}

	skips := func(from, to resolver.Ref) bool {
		for _, id := range p.DeployEntry(from.ID, from.Version).SkipWaitFor {
			if id == to.ID {
				return true
			}
		}
		return false
	}
	var shells, edges []string
	var cycleEdges [][2]resolver.Ref
	seenShell := map[resolver.Ref]bool{}
	for i, from := range cycle {
		to := cycle[(i+1)%len(cycle)]
		if p.Decl.IsShellID(from.ID) && !seenShell[from] {
			seenShell[from] = true
			shells = append(shells, from.String())
		}
		for _, x := range inside(from) {
			node := graph.Node(x)
			if node == nil {
				continue
			}
			for _, y := range inside(to) {
				if containsRef(node.Requires, y) && !skips(x, y) {
					edges = append(edges, describe(x)+" → "+describe(y))
					cycleEdges = append(cycleEdges, [2]resolver.Ref{x, y})
				}
			}
		}
	}
	err := clierr.New(clierr.CodeDependencyCycle, i18n.T(msgid.ShellMergeCycle, strings.Join(shells, i18n.T(msgid.ListSeparator))))
	for _, edge := range edges {
		err = err.WithDetail(i18n.T(msgid.ShellLabelMergeCycleEdge), edge)
	}
	hints := []string{i18n.T(msgid.ShellHintMergeCycleHostBoth), i18n.T(msgid.ShellHintMergeCycleOptional)}
	if target, from := skipSuggestion(p, states, cycleEdges); len(from) > 0 {
		id := msgid.ShellHintMergeCycleSkipWait
		if len(from) == 1 {
			id = msgid.ShellHintMergeCycleSkipWaitOne
		}
		hints = append(hints, i18n.T(id, target.ID, strings.Join(from, i18n.T(msgid.ListSeparator))))
	}
	return err.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellMergeCycleReason)).WithHint(hints...)
}

// skipSuggestion 挑一条能直接照抄的 skipWaitFor：优先让外壳里的成员不等外面的组件，并且把
// 环上所有指向同一个组件的成员都列出来——只写其中一个，其他成员照样让外壳等它，环还在。
func skipSuggestion(p *project.Project, states *cascade.Result, edges [][2]resolver.Ref) (resolver.Ref, []string) {
	if len(edges) == 0 {
		return resolver.Ref{}, nil
	}
	target := edges[0][1]
	for _, e := range edges {
		if _, hosted := states.HostOf(p, e[0]); hosted {
			target = e[1]
			break
		}
	}
	var from []string
	seen := map[resolver.Ref]bool{}
	for _, e := range edges {
		if e[1] == target && !seen[e[0]] {
			seen[e[0]] = true
			from = append(from, e[0].String())
		}
	}
	return target, from
}

// findCycle 在图里找一个强依赖环，按依赖方向返回环上的节点；没有环时返回 nil。
func findCycle(g *resolver.Graph) []resolver.Ref {
	const (
		unvisited = iota
		onStack
		done
	)
	state := map[resolver.Ref]int{}
	var stack []resolver.Ref
	var found []resolver.Ref
	var visit func(ref resolver.Ref) bool
	visit = func(ref resolver.Ref) bool {
		state[ref] = onStack
		stack = append(stack, ref)
		if node := g.Node(ref); node != nil {
			for _, dep := range node.Requires {
				switch state[dep] {
				case onStack:
					for i, r := range stack {
						if r == dep {
							found = append([]resolver.Ref(nil), stack[i:]...)
						}
					}
					return true
				case unvisited:
					if visit(dep) {
						return true
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[ref] = done
		return false
	}
	for _, n := range g.Nodes {
		if state[n.Ref] == unvisited && visit(n.Ref) {
			return found
		}
	}
	return nil
}

// withoutSkipped 去掉 skip 里点名的组件 ID，保留原有顺序。
func withoutSkipped(refs []resolver.Ref, skip []string) []resolver.Ref {
	if len(skip) == 0 {
		return refs
	}
	skipped := map[string]bool{}
	for _, id := range skip {
		skipped[id] = true
	}
	var out []resolver.Ref
	for _, ref := range refs {
		if !skipped[ref.ID] {
			out = append(out, ref)
		}
	}
	return out
}

func containsRef(refs []resolver.Ref, ref resolver.Ref) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}
