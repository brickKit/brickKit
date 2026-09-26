package shell

// 本文件回答"一个工作负载（容器 / Pod）这次真正要连谁"。
//
// 外壳进程是成员的物理宿主：成员的代码跑在外壳里，成员要连的依赖就是外壳容器要连的依赖。
// 启动顺序（depends_on）、寻址（extra_hosts）、网络策略（egress）都按这一份算——
// 只看外壳自己的依赖，外壳就会在成员需要的东西还没起来时启动、解析不了成员要用的主机名、
// 被网络策略挡在成员的依赖外面。

import (
	"sort"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// Dependencies 返回 ref 的工作负载这次真正要连的强依赖与弱依赖：ref 自己的依赖，加上它这次
// 承载的成员的依赖；去掉它承载的成员本身（进程内调用，不走网络）与它自己。
// 不是外壳的组件就是它自己的依赖。不按跑没跑过滤——那由调用方按各自的规则决定。
func Dependencies(
	p *project.Project, graph *resolver.Graph, states *cascade.Result, ref resolver.Ref,
) (requires, optional []resolver.Ref) {
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
		for _, dep := range node.Requires {
			if !inside[dep] {
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
// 并进外壳——外壳的依赖是 Dependencies 的结果（它自己的加上成员的），别的组件对成员的依赖
// 改成对外壳的依赖。启动顺序按它排，与生成文件里的 depends_on 说的是同一件事。
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
		requires, optional := node.Requires, node.Optional
		if p.Decl.IsShellID(ref.ID) {
			requires, optional = Dependencies(p, graph, states, ref)
		}
		nodes = append(nodes, &resolver.Node{
			Ref: ref, Manifest: node.Manifest,
			Requires: remap(ref, requires), Optional: remap(ref, optional),
			MissingOptional: node.MissingOptional,
		})
	}
	return resolver.NewGraph(nodes)
}
