package install

// 本文件是"成员由哪个外壳承载"的规则，add、upgrade、remove 共用（lint 的孤儿版本检查也按它）。
//
// 外壳的 component.yaml 列的是它编进了哪些成员版本（shell.members）；这次由谁承载，由部署文件决定：
// 成员条目嵌在哪个外壳下面就由哪个外壳承载，一个组件 ID 只进一个外壳（project 的 checkMembers），
// 运行时也只核对承载着的成员（shell 的 checkMemberVersions）。两个外壳可以编进同一个成员——按负载
// 拆外壳时常见——只有承载它的那个外壳需要它编进的版本；另一个外壳编进的版本没人跑，不进项目。
//
// 一个世界里，成员 ID → 承载它的外壳：
//
//  1. 部署文件里嵌在外壳下面的：还是那个外壳。那个外壳（的新版本）不再编进它时，HostOf 为假，
//     条目由各自的规划挪到顶层
//  2. 这次 add / upgrade 的外壳（acting，按顺序）：编进的、还没有外壳承载的成员归它——项目里没有的
//     成员跟着它进来。在顶层独立运行的成员只在 add 外壳时收编（add 外壳就是决定把成员合进去）；
//     升级外壳不改使用者定下的承载位置。"不在项目里"也是一种承载位置：升级前就有外壳编进、项目里
//     却没有的成员是使用者移除的（removedMembers），升级不把它加回来——除非这个世界里有组件需要它
//  3. 别的外壳：项目里没有、这次又进了世界的成员（比如 add 的正是它），编进了这个版本的外壳承载它

import (
	"slices"

	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// Hosting 是一次操作里决定承载关系的输入。
type Hosting struct {
	p *project.Project
	// acting 是这次 add / upgrade 的外壳 ID，按顺序：同一个新成员，排在前面的承载。
	acting []string
	// claimStandalone：acting 的外壳也收编在顶层独立运行的成员（add 外壳时）。
	claimStandalone bool
	// removed 是使用者移除的成员 ID：acting 的外壳也只在世界里有它时才收（upgrade 时）。
	removed map[string]bool
}

// Placement 是一个世界里的承载关系。
type Placement struct {
	host   map[string]string
	shells map[string]*manifest.Manifest
	// order 是这个世界里的外壳 ID，按图的解析顺序。
	order []string
}

// Place 按本文件开头的三条规则，算出 graph 这个世界里每个成员 ID 由哪个外壳承载。
func (h Hosting) Place(graph *resolver.Graph) Placement {
	pl := Placement{host: map[string]string{}, shells: map[string]*manifest.Manifest{}}
	for _, n := range graph.Nodes {
		if n.Manifest.IsShell() && pl.shells[n.Ref.ID] == nil {
			pl.shells[n.Ref.ID] = n.Manifest
			pl.order = append(pl.order, n.Ref.ID)
		}
	}
	h.placeWritten(pl)
	for _, id := range h.acting {
		if m := pl.shells[id]; m != nil {
			h.claim(pl, id, m, true, graph.Has)
		}
	}
	for _, id := range pl.order {
		if !slices.Contains(h.acting, id) {
			h.claim(pl, id, pl.shells[id], false, graph.Has)
		}
	}
	return pl
}

// Hosted 是这个世界里被外壳承载的全部成员版本（外壳按图的顺序，成员按外壳声明的顺序）。
func (pl Placement) Hosted() []resolver.Ref {
	var out []resolver.Ref
	for _, shellID := range pl.order {
		for _, member := range pl.shells[shellID].Shell.Members {
			id, version, _ := manifest.SplitRef(member)
			ref := resolver.Ref{ID: id, Version: version}
			if host, ok := pl.HostOf(ref); ok && host == shellID {
				out = append(out, ref)
			}
		}
	}
	return out
}

// placeWritten 是第 1 条：部署文件里写下的承载关系。
func (h Hosting) placeWritten(pl Placement) {
	for _, c := range h.p.Deploy.Components {
		shellID, _ := c.Key()
		for _, m := range c.Members {
			id, _ := m.Key()
			pl.host[id] = shellID
		}
	}
}

// claim 是第 2、3 条：外壳 shellID 收下它编进的、还没有外壳承载的成员，返回收下的成员版本。
// acting 的外壳编进的成员跟着它进来；别的外壳、以及 acting 的外壳遇到使用者移除的成员时，只收 has
// 报告这个世界里已经有的版本（has 为 nil：还没有世界，什么都不算已有）。
func (h Hosting) claim(pl Placement, shellID string, m *manifest.Manifest, acting bool, has func(resolver.Ref) bool) []resolver.Ref {
	var claimed []resolver.Ref
	for _, member := range m.Shell.Members {
		id, version, _ := manifest.SplitRef(member)
		ref := resolver.Ref{ID: id, Version: version}
		if _, placed := pl.host[id]; placed {
			continue
		}
		declared := len(h.p.Decl.Versions(id)) > 0
		if declared && (!acting || !h.claimStandalone) {
			continue // 在顶层独立运行：使用者定下的，只有 add 外壳时收编
		}
		if (!acting || h.removed[id]) && (has == nil || !has(ref)) {
			continue
		}
		pl.host[id] = shellID
		claimed = append(claimed, ref)
	}
	return claimed
}

// HostOf 是承载 ref 这个版本的外壳：成员 ID 归它，而且它编进的正是这个版本。
func (pl Placement) HostOf(ref resolver.Ref) (string, bool) {
	shellID, ok := pl.host[ref.ID]
	if !ok || pl.shells[shellID] == nil {
		return "", false
	}
	if v, ok := pl.shells[shellID].HostedVersion(ref.ID); !ok || v != ref.Version {
		return "", false
	}
	return shellID, true
}

// Owner 是成员 ID 归哪个外壳（不管那个外壳编进的是哪个版本）。
func (pl Placement) Owner(id string) (string, bool) {
	shellID, ok := pl.host[id]
	return shellID, ok
}

// AddRoots 是 add 解析依赖图的根：项目里声明的全部版本、targets，以及 targets 里的外壳会承载的
// 成员（manifests 是 targets 的 Manifest）。别的外壳已经承载着的成员不跟进来——那个版本没人跑。
func AddRoots(p *project.Project, targets []resolver.Ref, manifests map[resolver.Ref]*manifest.Manifest) []resolver.Ref {
	roots := make([]resolver.Ref, 0, len(p.Decl.Components)+len(targets))
	for _, c := range p.Decl.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	h := addHosting(p, targets, manifests)
	pl := Placement{host: map[string]string{}}
	h.placeWritten(pl)
	for _, t := range targets {
		roots = append(roots, t)
		if m := manifests[t]; m.IsShell() {
			roots = append(roots, h.claim(pl, t.ID, m, true, nil)...)
		}
	}
	return roots
}

// addHosting：add 的 acting 外壳是 targets 里的外壳，按 targets 的顺序。
func addHosting(p *project.Project, targets []resolver.Ref, manifests map[resolver.Ref]*manifest.Manifest) Hosting {
	h := Hosting{p: p, claimStandalone: true}
	for _, t := range targets {
		if m := manifests[t]; m.IsShell() && !slices.Contains(h.acting, t.ID) {
			h.acting = append(h.acting, t.ID)
		}
	}
	return h
}

// worldHosting：upgrade 的 acting 外壳是被移动的外壳（不是外壳的 ID 在 Place 里找不到 Manifest，
// 自然跳过）；moves 为空就只有部署文件写下的承载关系（remove、升级前的世界）。removed 见 removedMembers。
func worldHosting(p *project.Project, moves []Move, removed map[string]bool) Hosting {
	h := Hosting{p: p, removed: removed}
	for _, m := range moves {
		h.acting = append(h.acting, m.ID)
	}
	return h
}

// removedMembers 是使用者移除的成员 ID：before（升级前的世界）里有外壳编进它，项目里却没有它。
// add 外壳总把编进的成员一起带进来，所以它不在项目里只能是使用者移除的——这个"不在"就是记录，
// 不另设字段。
func removedMembers(p *project.Project, before *resolver.Graph) map[string]bool {
	removed := map[string]bool{}
	for _, n := range before.Nodes {
		if !n.Manifest.IsShell() {
			continue
		}
		for _, member := range n.Manifest.Shell.Members {
			if id, _, _ := manifest.SplitRef(member); len(p.Decl.Versions(id)) == 0 {
				removed[id] = true
			}
		}
	}
	return removed
}
