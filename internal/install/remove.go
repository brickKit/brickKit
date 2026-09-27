package install

import (
	"slices"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/resolver"
)

// PlanRemove 算出把 target 移出项目要做的改动。graph 以项目里声明的全部组件为根解析。
//
// 规则：
//   - 还有留下的组件强依赖 target：报错（弱依赖方不拦，它本来就能在对方缺席时运行）
//   - target 的行、部署条目删掉，配置归档（§7.7）；target 是外壳时成员挪回顶层（§8.7）
//   - 受这次移除影响的兼容版本（依赖它、或编进了它的组件被移除了）：留下的组件里再没有谁
//     依赖它、也没有外壳编进它，它没人要了，一并移除并继续往下传；还有人要的，requiredBy
//     改成真正还需要它的那些组件（附录 A20）。按依赖图判断而不是只看 requiredBy 这张清单——
//     清单点名的组件可能还有别的版本留着、而那个版本并不需要它，清单也可能是手改过的
//   - 删的是默认版本、这个 ID 还剩版本：只剩一个就转正（去掉 requiredBy、条目 id@v → id、
//     配置文件 <base>@v.yaml → <base>.yaml）；剩好几个报错（用户裁定，2026-09-27）
func PlanRemove(p *project.Project, graph *resolver.Graph, target resolver.Ref) (*Plan, error) {
	if err := checkDependents(p, graph, target); err != nil {
		return nil, err
	}
	plan := &Plan{}
	removed, affected := cascade(p, graph, target)
	plan.Notes = optionalDependentNotes(graph, removed)

	remaining := remainingVersions(p, target.ID, removed)
	var promote *projfile.Component
	if p.Decl.IsDefault(target.ID, target.Version) && len(remaining) > 0 {
		if len(remaining) > 1 {
			return nil, ambiguousDefaultError(target, remaining)
		}
		promote = &remaining[0]
	}

	if p.Decl.IsShellID(target.ID) {
		plan.UnnestShells = append(plan.UnnestShells, target.ID)
	}
	for _, c := range targetFirst(p.Decl.Components, target) {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		isDefault := p.Decl.IsDefault(c.ID, c.Version)
		switch {
		case removed[ref]:
			plan.RemoveLines = append(plan.RemoveLines, ref)
			plan.Removed = append(plan.Removed, ref)
			if l, ok := p.Deploy.EntryAt(c.ID, c.Version, isDefault); ok {
				plan.RemoveEntries = append(plan.RemoveEntries, l.ID)
			}
			plan.ArchiveConfigs = append(plan.ArchiveConfigs, ConfigRef{ID: c.ID, Version: c.Version, Versioned: !isDefault})
		case promote != nil && c.ID == promote.ID && c.Version == promote.Version:
			plan.SetRequiredBy = append(plan.SetRequiredBy, Line{ID: c.ID, Version: c.Version})
			if l, ok := p.Deploy.EntryAt(c.ID, c.Version, false); ok && l.ID != c.ID {
				plan.RenameEntries = append(plan.RenameEntries, Rename{From: l.ID, To: c.ID})
			}
			plan.RenameConfigs = append(plan.RenameConfigs, ConfigRef{ID: c.ID, Version: c.Version, Versioned: true})
			plan.Promoted = ref
		case affected[ref] && !isDefault:
			if kept := neededBy(p, graph, ref, removed); !slices.Equal(kept, c.RequiredBy) {
				plan.SetRequiredBy = append(plan.SetRequiredBy, Line{ID: c.ID, Version: c.Version, RequiredBy: kept})
			}
		}
	}
	return plan, nil
}

// optionalDependentNotes：留下的组件里弱依赖被移除版本的，不拦，但要说一声——它照样运行，
// 只是那个依赖的地址不再注入（组件自己的降级逻辑接手）。
func optionalDependentNotes(graph *resolver.Graph, removed map[resolver.Ref]bool) []string {
	var notes []string
	for _, n := range graph.Nodes {
		if removed[n.Ref] {
			continue
		}
		for _, opt := range n.Optional {
			if removed[opt] {
				notes = append(notes, i18n.T(msgid.InstallNoteOptionalDependent, n.Ref.String(), opt.String()))
			}
		}
	}
	return notes
}

// targetFirst 把 target 那一行排到最前面（输出先说删了谁，再说连带删了谁），其余保持声明顺序。
func targetFirst(components []projfile.Component, target resolver.Ref) []projfile.Component {
	out := make([]projfile.Component, 0, len(components))
	for _, c := range components {
		if c.ID == target.ID && c.Version == target.Version {
			out = append(out, c)
		}
	}
	for _, c := range components {
		if c.ID != target.ID || c.Version != target.Version {
			out = append(out, c)
		}
	}
	return out
}

// checkDependents：留下的组件里还有强依赖 target 的，不能删。
func checkDependents(p *project.Project, graph *resolver.Graph, target resolver.Ref) error {
	node := graph.Node(target)
	if node == nil {
		return nil
	}
	var blockers []resolver.Ref
	for _, d := range node.Dependents {
		if d == target {
			continue
		}
		if dn := graph.Node(d); dn != nil && slices.Contains(dn.Requires, target) {
			blockers = append(blockers, d)
		}
	}
	if len(blockers) == 0 {
		return nil
	}
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.InstallRemoveHasDependents, target.String(), joinRefs(blockers))).
		WithHint(i18n.T(msgid.InstallHintRemoveDependentsFirst))
}

// cascade 算出连带移除的组件版本：从 target 开始，受影响的兼容版本里再没人要的一个接一个跟着走。
// affected 是依赖方或宿主外壳被移除了的版本（它们的 requiredBy 要重新算）。
func cascade(p *project.Project, graph *resolver.Graph, target resolver.Ref) (removed, affected map[resolver.Ref]bool) {
	removed = map[resolver.Ref]bool{target: true}
	affected = map[resolver.Ref]bool{}
	for changed := true; changed; {
		changed = false
		for _, c := range p.Decl.Components {
			ref := resolver.Ref{ID: c.ID, Version: c.Version}
			if removed[ref] || p.Decl.IsDefault(c.ID, c.Version) || !lostANeeder(p, graph, ref, removed) {
				continue
			}
			affected[ref] = true
			if len(neededBy(p, graph, ref, removed)) == 0 {
				removed[ref] = true
				changed = true
			}
		}
	}
	return removed, affected
}

// lostANeeder 报告这个版本的依赖方（或编进了它的外壳）里有没有被这次移除的。
func lostANeeder(p *project.Project, graph *resolver.Graph, ref resolver.Ref, removed map[resolver.Ref]bool) bool {
	if n := graph.Node(ref); n != nil {
		for _, d := range n.Dependents {
			if removed[d] {
				return true
			}
		}
	}
	for r := range removed {
		if hosts(graph, r, ref) {
			return true
		}
	}
	return false
}

// neededBy 是移除之后还需要这个版本的组件 ID：留下的依赖方（强依赖、弱依赖都算——它们都会
// 连它），以及留下的、编进了它的外壳。排好序、去重。
func neededBy(p *project.Project, graph *resolver.Graph, ref resolver.Ref, removed map[resolver.Ref]bool) []string {
	var out []string
	if n := graph.Node(ref); n != nil {
		for _, d := range n.Dependents {
			if !removed[d] && d.ID != ref.ID {
				out = append(out, d.ID)
			}
		}
	}
	for _, c := range p.Decl.Components {
		shell := resolver.Ref{ID: c.ID, Version: c.Version}
		if c.IsShell() && !removed[shell] && hosts(graph, shell, ref) {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// hosts 报告外壳 shell 的 component.yaml 是否编进了 ref 这个版本（附录 A24）。
func hosts(graph *resolver.Graph, shell, ref resolver.Ref) bool {
	n := graph.Node(shell)
	if n == nil || n.Manifest == nil {
		return false
	}
	v, ok := n.Manifest.HostedVersion(ref.ID)
	return ok && v == ref.Version
}

// remainingVersions 是这个组件 ID 在移除之后还留下的行。
func remainingVersions(p *project.Project, id string, removed map[resolver.Ref]bool) []projfile.Component {
	var out []projfile.Component
	for _, c := range p.Decl.ComponentsByID(id) {
		if !removed[resolver.Ref{ID: c.ID, Version: c.Version}] {
			out = append(out, c)
		}
	}
	return out
}

func ambiguousDefaultError(target resolver.Ref, remaining []projfile.Component) error {
	lines := make([]string, len(remaining))
	for i, c := range remaining {
		lines[i] = i18n.T(msgid.InstallRemainingVersion, c.Ref(), strings.Join(c.RequiredBy, ", "))
	}
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.InstallRemoveDefaultAmbiguous, target.String())).
		WithDetail(i18n.T(msgid.InstallLabelRemainingVersions), strings.Join(lines, "\n")).
		WithHint(
			i18n.T(msgid.InstallHintRemoveOthersFirst, target.String()),
			i18n.T(msgid.InstallHintUpgradeToChooseDefault, target.ID),
		)
}
