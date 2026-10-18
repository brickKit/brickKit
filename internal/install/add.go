package install

import (
	"slices"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// PlanAdd 算出把 targets 加进项目要做的改动（add 一个组件，或 add --local 一批）。graph 以
// AddRoots 为根解析——图里没在 brickkit.yaml 里的都是这次要加的。
//
// 规则：
//   - 新组件 ID 的第一个版本是默认版本：没有 requiredBy、部署条目写裸 ID、无版本号配置文件；
//     同一次带进同一个 ID 的几个版本时，target 自己的版本（或最高的那个）是默认版本
//   - ID 已有别的版本：这是为依赖方（或外壳）保留的兼容版本，requiredBy 写上它们
//     、部署条目写 id@version、配置文件带版本号
//   - target 本身是已有 ID 的另一个版本、又没有谁需要它：报错，换默认版本归 upgrade
//   - 承载它的外壳（hosting.go 的规则：add 外壳就是决定由它承载编进的成员，已经嵌在别的外壳
//     下面的除外）编进的正是这个版本：条目嵌到外壳下面；已在顶层的挪进去
func PlanAdd(p *project.Project, graph *resolver.Graph, targets ...resolver.Ref) (*Plan, error) {
	a := newAdder(p, graph, targets)
	for _, target := range targets {
		if err := a.checkTarget(target); err != nil {
			return nil, err
		}
	}
	a.chooseDefaults(targets)
	a.addNewRefs()
	a.extendRequiredBy()
	a.nestExisting()
	a.noteMembersHostedElsewhere()
	for _, target := range targets {
		a.fillMissingConfig(target)
	}
	sortEntries(a.plan.AddEntries)
	return a.plan, nil
}

type adder struct {
	p        *project.Project
	graph    *resolver.Graph
	plan     *Plan
	declared map[resolver.Ref]bool
	// fresh 是这次要加的组件版本（图的解析顺序：依赖在前）。
	fresh []resolver.Ref
	// defaults 是这次新组件 ID 的默认版本。
	defaults map[string]string
	// hosting / hosts：加完之后的承载关系；hosting.acting 是 targets 里的外壳。
	hosting Hosting
	hosts   Placement
}

func newAdder(p *project.Project, graph *resolver.Graph, targets []resolver.Ref) *adder {
	a := &adder{p: p, graph: graph, plan: &Plan{}, declared: map[resolver.Ref]bool{}, defaults: map[string]string{}}
	for _, c := range p.Decl.Components {
		a.declared[resolver.Ref{ID: c.ID, Version: c.Version}] = true
	}
	for _, n := range graph.Nodes {
		if !a.declared[n.Ref] {
			a.fresh = append(a.fresh, n.Ref)
		}
	}
	manifests := map[resolver.Ref]*manifest.Manifest{}
	for _, t := range targets {
		if n := graph.Node(t); n != nil {
			manifests[t] = n.Manifest
		}
	}
	a.hosting = addHosting(p, targets, manifests)
	a.hosts = a.hosting.Place(graph)
	return a
}

// checkTarget：直接 add 已有组件的另一个版本（没有依赖方、没有外壳要它）不归 add 管。
func (a *adder) checkTarget(target resolver.Ref) error {
	if a.declared[target] {
		return nil
	}
	existing := a.p.Decl.Versions(target.ID)
	if len(existing) == 0 || len(a.neededBy(target)) > 0 {
		return nil
	}
	current, _ := a.p.Decl.DefaultVersion(target.ID)
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.InstallAddOtherVersion, target.ID+"@"+current, target.String())).
		WithHint(
			i18n.T(msgid.InstallHintUpgradeInstead, target.String()),
			i18n.T(msgid.InstallHintAddDependent, target.String()),
		)
}

// chooseDefaults 给这次新出现的组件 ID 选默认版本：要 add 的那个版本优先，否则取最高的。
func (a *adder) chooseDefaults(targets []resolver.Ref) {
	wanted := map[string]string{}
	for _, t := range targets {
		wanted[t.ID] = t.Version
	}
	for _, ref := range a.fresh {
		if len(a.p.Decl.Versions(ref.ID)) > 0 {
			continue
		}
		if v, ok := wanted[ref.ID]; ok {
			a.defaults[ref.ID] = v
			continue
		}
		if current, seen := a.defaults[ref.ID]; !seen || manifest.CompareVersions(ref.Version, current) > 0 {
			a.defaults[ref.ID] = ref.Version
		}
	}
}

func (a *adder) isDefault(ref resolver.Ref) bool {
	if v, ok := a.defaults[ref.ID]; ok {
		return v == ref.Version
	}
	return a.p.Decl.IsDefault(ref.ID, ref.Version)
}

// neededBy 是需要这个版本的组件 ID（图里的依赖方 + 承载它的外壳），排好序、去重。
func (a *adder) neededBy(ref resolver.Ref) []string {
	var out []string
	if n := a.graph.Node(ref); n != nil {
		for _, d := range n.Dependents {
			if d.ID != ref.ID {
				out = append(out, d.ID)
			}
		}
	}
	if host, ok := a.hosts.HostOf(ref); ok {
		out = append(out, host)
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func (a *adder) addNewRefs() {
	for _, ref := range a.fresh {
		n := a.graph.Node(ref)
		isDefault := a.isDefault(ref)
		line := Line{ID: ref.ID, Version: ref.Version, Shell: n.Manifest.IsShell()}
		if !isDefault {
			line.RequiredBy = a.neededBy(ref)
		}
		a.plan.AddLines = append(a.plan.AddLines, line)
		a.plan.Added = append(a.plan.Added, ref)

		entry := Entry{ID: EntryID(ref.ID, ref.Version, isDefault)}
		if host, ok := a.hosts.HostOf(ref); ok {
			entry.Under = host
		} else {
			a.noteOtherHostedVersion(ref)
		}
		a.plan.AddEntries = append(a.plan.AddEntries, entry)

		if hasSchema(n.Manifest) {
			a.plan.AddConfigs = append(a.plan.AddConfigs,
				ConfigFile{ID: ref.ID, Version: ref.Version, Versioned: !isDefault, Schema: n.Manifest.ConfigSchema})
		}
	}
}

// noteOtherHostedVersion：承载这个组件的外壳（没有外壳承载它时，编进了它的外壳）编进的是另一个
// 版本——这个版本独立运行，说一声为什么。
func (a *adder) noteOtherHostedVersion(ref resolver.Ref) {
	owner, placed := a.hosts.Owner(ref.ID)
	for _, id := range a.hosts.order {
		if placed && id != owner {
			continue
		}
		if v, ok := a.hosts.shells[id].HostedVersion(ref.ID); ok && v != ref.Version {
			a.plan.Notes = append(a.plan.Notes,
				i18n.T(msgid.InstallNoteShellHostsOtherVersion, id, ref.ID+"@"+v, ref.String()))
		}
	}
}

// extendRequiredBy：已有的兼容版本（带 requiredBy 的行）多了依赖方或外壳，追加进去。
func (a *adder) extendRequiredBy() {
	for _, c := range a.p.Decl.Components {
		if len(c.RequiredBy) == 0 {
			continue
		}
		merged := append(append([]string{}, c.RequiredBy...), a.neededBy(resolver.Ref{ID: c.ID, Version: c.Version})...)
		sort.Strings(merged)
		merged = slices.Compact(merged)
		if !slices.Equal(merged, sortedCopy(c.RequiredBy)) {
			a.plan.SetRequiredBy = append(a.plan.SetRequiredBy, Line{ID: c.ID, Version: c.Version, RequiredBy: merged})
		}
	}
}

// nestExisting：已声明、在部署文件顶层的组件版本，正是这次 add 的外壳要承载的那一个——挪进外壳。
//
// 只对这次 add 的外壳做：已在项目里的外壳下面缺的成员，是使用者移出去独立
// 运行的，之后 add 别的组件不能把它塞回去。
func (a *adder) nestExisting() {
	for _, c := range a.p.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		l, found := a.p.Deploy.EntryAt(c.ID, c.Version, a.p.Decl.IsDefault(c.ID, c.Version))
		if !found || l.Shell != "" {
			continue
		}
		if host, ok := a.hosts.HostOf(ref); ok && slices.Contains(a.hosting.acting, host) {
			a.plan.NestEntries = append(a.plan.NestEntries, Entry{ID: l.ID, Under: host})
		}
	}
}

// noteMembersHostedElsewhere：这次 add 的外壳编进的成员已经嵌在别的外壳下面（一个组件只能在一个
// 外壳里）——加外壳的人以为成员会由它承载，说一声。
func (a *adder) noteMembersHostedElsewhere() {
	for _, shellID := range a.hosting.acting {
		for _, member := range a.hosts.shells[shellID].Shell.Members {
			id, version, _ := manifest.SplitRef(member)
			if owner, ok := a.hosts.Owner(id); ok && owner != shellID {
				ref := resolver.Ref{ID: id, Version: version}
				a.plan.Notes = append(a.plan.Notes, i18n.T(msgid.InstallNoteMemberUnderOtherShell, ref.String(), id, owner, shellID))
			}
		}
	}
}

// fillMissingConfig：重复 add 一个已声明的组件、它有 configSchema 却没有配置文件——补上骨架。
// 只补 target：别的组件的配置文件可能是使用者特意删掉的（键都有默认值时文件可以不要）。
func (a *adder) fillMissingConfig(target resolver.Ref) {
	n := a.graph.Node(target)
	if !a.declared[target] || n == nil || !hasSchema(n.Manifest) || a.p.Config(target.ID, target.Version) != nil {
		return
	}
	a.plan.AddConfigs = append(a.plan.AddConfigs, ConfigFile{ID: target.ID, Version: target.Version,
		Versioned: !a.p.Decl.IsDefault(target.ID, target.Version), Schema: n.Manifest.ConfigSchema})
}

// sortEntries 让顶层条目排在嵌套条目前面（外壳条目要先在），各自保持原顺序。
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Under == "" && entries[j].Under != "" })
}

func sortedCopy(list []string) []string {
	out := append([]string{}, list...)
	sort.Strings(out)
	return out
}

// joinRefs 把组件版本列成一行。
func joinRefs(refs []resolver.Ref) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.String()
	}
	return strings.Join(parts, ", ")
}
