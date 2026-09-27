package install

import (
	"slices"
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
//   - 每一行 requiredBy 里点名的组件没有任何版本留下时划掉它；划空了的兼容版本没人要了，
//     一并移除，并继续往下传（附录 A20）
//   - 删的是默认版本、这个 ID 还剩版本：只剩一个就转正（去掉 requiredBy、条目 id@v → id、
//     配置文件 <base>@v.yaml → <base>.yaml）；剩好几个报错（用户裁定，2026-09-27）
func PlanRemove(p *project.Project, graph *resolver.Graph, target resolver.Ref) (*Plan, error) {
	if err := checkDependents(p, graph, target); err != nil {
		return nil, err
	}
	plan := &Plan{}
	removed := cascade(p, target)

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
		default:
			if kept := keptRequiredBy(p, c, removed); !slices.Equal(kept, c.RequiredBy) {
				plan.SetRequiredBy = append(plan.SetRequiredBy, Line{ID: c.ID, Version: c.Version, RequiredBy: kept})
			}
		}
	}
	return plan, nil
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

// cascade 算出连带移除的组件版本：从 target 开始，requiredBy 被划空的兼容版本一个接一个跟着走。
func cascade(p *project.Project, target resolver.Ref) map[resolver.Ref]bool {
	removed := map[resolver.Ref]bool{target: true}
	for changed := true; changed; {
		changed = false
		for _, c := range p.Decl.Components {
			ref := resolver.Ref{ID: c.ID, Version: c.Version}
			if removed[ref] || len(c.RequiredBy) == 0 {
				continue
			}
			if len(keptRequiredBy(p, c, removed)) == 0 {
				removed[ref] = true
				changed = true
			}
		}
	}
	return removed
}

// keptRequiredBy 是 requiredBy 里还留在项目里的组件（有任何一个版本留下就算留下）。
func keptRequiredBy(p *project.Project, c projfile.Component, removed map[resolver.Ref]bool) []string {
	var kept []string
	for _, id := range c.RequiredBy {
		if len(remainingVersions(p, id, removed)) > 0 {
			kept = append(kept, id)
		}
	}
	return kept
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
