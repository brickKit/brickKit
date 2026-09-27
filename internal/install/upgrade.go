package install

// 本文件规划 upgrade（提案 §12，附录 A4、A20、A24）：把一组默认版本的移动变成对三份文件的改动。
//
// # 思路：比较升级前后的"世界"
//
// 项目要跑的版本 = 默认版本、外壳编进的成员，以及它们的依赖闭包（ResolveWorld）。升级前后各算一次：
//
//	新世界有、项目里没有           加进来（像 add：默认版本，或带 requiredBy 的兼容版本）
//	旧世界有、新世界没有           移除（配置归档）——只动升级带来的变化，旧世界里本来就没人要的
//	                                行（手写的）不碰
//	两边都有的兼容版本             谁需要它变了，requiredBy 跟着改成新世界里真正需要它的组件
//
// 被移动的默认版本 v1 → v2：brickkit.yaml 那一行原地改版本；v1 在新世界里还有人要（依赖方、编进
// 它的外壳）就另起一行带 requiredBy 留下，配置文件改名成带版本号的；v2 原本就是兼容版本时它转正。
//
// # 部署条目
//
// 每个版本在新世界里写成什么 id：默认版本写裸 ID，其余写 id@version。裸条目（带着使用者的字段）
// 跟着默认版本走——例外是它嵌在一个编进了 v1 的外壳下面、而 v1 留下了：那它留给 v1（改名成
// id@v1），v2 在顶层另起一个裸条目，P3f 的核对照样成立。改名时先降级（裸 → id@v）再升级
// （id@v → 裸），同一时刻不会有两个裸条目。嵌在外壳下面、而新外壳没编进这个版本的条目挪到顶层。

import (
	"context"
	"slices"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// Move 是一个组件默认版本的移动。
type Move struct{ ID, From, To string }

// ConfigMigration 是一次配置迁移：读 Source（升级前的文件），按新版本的 configSchema 写 Target。
type ConfigMigration struct {
	ID, From, To         string
	Source, Target       ConfigRef
	OldSchema, NewSchema *manifest.ConfigSchema
}

// Defaults 是移动之后的默认版本（移动的组件换成新版本），按 brickkit.yaml 的顺序。
func Defaults(p *project.Project, moves []Move) []resolver.Ref {
	to := map[string]string{}
	for _, m := range moves {
		to[m.ID] = m.To
	}
	var out []resolver.Ref
	for _, c := range p.Decl.Components {
		if !p.Decl.IsDefault(c.ID, c.Version) {
			continue
		}
		v := c.Version
		if moved, ok := to[c.ID]; ok {
			v = moved
		}
		out = append(out, resolver.Ref{ID: c.ID, Version: v})
	}
	return out
}

// ResolveWorld 解析一个世界：以默认版本为根，外壳编进的成员也是根（它们不是依赖边），
// 直到不再出现新的外壳成员。
func ResolveWorld(ctx context.Context, r *resolver.Resolver, defaults []resolver.Ref) (*resolver.Graph, error) {
	roots := append([]resolver.Ref{}, defaults...)
	seen := map[resolver.Ref]bool{}
	for _, ref := range roots {
		seen[ref] = true
	}
	for {
		graph, err := r.Resolve(ctx, roots...)
		if err != nil {
			return nil, err
		}
		grew := false
		for _, n := range graph.Nodes {
			if !n.Manifest.IsShell() {
				continue
			}
			for _, member := range n.Manifest.Shell.Members {
				id, version, _ := manifest.SplitRef(member)
				ref := resolver.Ref{ID: id, Version: version}
				if !seen[ref] {
					seen[ref] = true
					roots = append(roots, ref)
					grew = true
				}
			}
		}
		if !grew {
			return graph, nil
		}
	}
}

// ShellMoves 把外壳的升级展开成成员的版本移动（附录 A24，取代 §8.6）：默认版本正是旧外壳编进的
// 那个版本的成员，跟着换成新外壳编进的版本。只为外壳保留的兼容版本、新成员、被去掉的成员由
// PlanUpgrade 按新旧世界的差处理。
func ShellMoves(p *project.Project, oldShell, newShell *manifest.Manifest) []Move {
	var moves []Move
	for _, member := range newShell.Shell.Members {
		id, to, _ := manifest.SplitRef(member)
		from, ok := oldShell.HostedVersion(id)
		if !ok || from == to || !p.Decl.IsDefault(id, from) {
			continue
		}
		moves = append(moves, Move{ID: id, From: from, To: to})
	}
	return moves
}

// PlanUpgrade 规划一组移动。oldGraph / newGraph 由 ResolveWorld 以 Defaults(p, nil) / Defaults(p, moves) 解析。
func PlanUpgrade(p *project.Project, oldGraph, newGraph *resolver.Graph, moves []Move) (*Plan, error) {
	for _, m := range moves {
		if !p.Decl.IsDefault(m.ID, m.From) {
			ref := resolver.Ref{ID: m.ID, Version: m.From}
			return nil, clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.InstallUpgradeNotDefault, ref.String())).
				WithHint(i18n.T(msgid.InstallHintUpgradeDependents, ref.String()))
		}
	}
	u := newUpgrader(p, oldGraph, newGraph, moves)
	u.lines()
	u.entries()
	u.configs()
	sortEntries(u.plan.AddEntries)
	return u.plan, nil
}

type upgrader struct {
	p                  *project.Project
	oldGraph, newGraph *resolver.Graph
	moves              map[string]Move
	plan               *Plan
	declared           map[resolver.Ref]bool
	// shells 是新世界里的外壳（ID → Manifest），按出现顺序。
	shells   map[string]*manifest.Manifest
	shellIDs []string
}

func newUpgrader(p *project.Project, oldGraph, newGraph *resolver.Graph, moves []Move) *upgrader {
	u := &upgrader{p: p, oldGraph: oldGraph, newGraph: newGraph, moves: map[string]Move{}, plan: &Plan{},
		declared: map[resolver.Ref]bool{}, shells: map[string]*manifest.Manifest{}}
	for _, m := range moves {
		u.moves[m.ID] = m
		u.plan.Moves = append(u.plan.Moves, m)
	}
	for _, c := range p.Decl.Components {
		u.declared[resolver.Ref{ID: c.ID, Version: c.Version}] = true
	}
	for _, n := range newGraph.Nodes {
		if n.Manifest.IsShell() && u.shells[n.Ref.ID] == nil {
			u.shells[n.Ref.ID] = n.Manifest
			u.shellIDs = append(u.shellIDs, n.Ref.ID)
		}
	}
	return u
}

// isDefault 报告 ref 在新世界里是不是默认版本。
func (u *upgrader) isDefault(ref resolver.Ref) bool {
	if m, ok := u.moves[ref.ID]; ok {
		return m.To == ref.Version
	}
	if len(u.p.Decl.Versions(ref.ID)) > 0 {
		return u.p.Decl.IsDefault(ref.ID, ref.Version)
	}
	// 新出现的组件：默认版本是新世界里它最高的版本
	versions := u.newGraph.Versions(ref.ID)
	return versions[len(versions)-1] == ref.Version
}

// entryID 是 ref 在新世界里的部署条目 id。
func (u *upgrader) entryID(ref resolver.Ref) string {
	return EntryID(ref.ID, ref.Version, u.isDefault(ref))
}

func (u *upgrader) inNew(ref resolver.Ref) bool { return u.newGraph.Has(ref) }
func (u *upgrader) inOld(ref resolver.Ref) bool { return u.oldGraph.Has(ref) }

// neededBy 是 graph 所在的世界里需要 ref 的组件 ID：依赖方、编进了它的外壳。
func neededIn(graph *resolver.Graph, ref resolver.Ref) []string {
	var out []string
	if n := graph.Node(ref); n != nil {
		for _, d := range n.Dependents {
			if d.ID != ref.ID {
				out = append(out, d.ID)
			}
		}
	}
	for _, n := range graph.Nodes {
		if !n.Manifest.IsShell() {
			continue
		}
		if v, ok := n.Manifest.HostedVersion(ref.ID); ok && v == ref.Version {
			out = append(out, n.Ref.ID)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// hostOf 是新世界里编进了 ref 的外壳。
func (u *upgrader) hostOf(ref resolver.Ref) (string, bool) {
	for _, id := range u.shellIDs {
		if v, ok := u.shells[id].HostedVersion(ref.ID); ok && v == ref.Version {
			return id, true
		}
	}
	return "", false
}

func (u *upgrader) lines() {
	for _, c := range u.p.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		m, moved := u.moves[c.ID]
		switch {
		case moved && c.Version == m.From:
			to := resolver.Ref{ID: m.ID, Version: m.To}
			kept := u.inNew(ref)
			if u.declared[to] {
				// 新版本原本就是兼容版本：它转正，旧默认版本要么留作兼容版本，要么移除
				u.plan.SetRequiredBy = append(u.plan.SetRequiredBy, Line{ID: m.ID, Version: m.To})
				if kept {
					u.plan.SetRequiredBy = append(u.plan.SetRequiredBy, Line{ID: c.ID, Version: c.Version, RequiredBy: neededIn(u.newGraph, ref)})
				} else {
					u.remove(ref)
				}
			} else {
				u.plan.ChangeVersions = append(u.plan.ChangeVersions, m)
				if kept {
					u.plan.AddLines = append(u.plan.AddLines, Line{ID: c.ID, Version: c.Version, RequiredBy: neededIn(u.newGraph, ref)})
				}
			}
		case moved && c.Version == m.To:
			// 在上面转正了
		case !u.inNew(ref) && u.inOld(ref) && !u.p.Decl.IsDefault(c.ID, c.Version):
			u.remove(ref)
		case len(c.RequiredBy) > 0 && u.inNew(ref):
			before, after := neededIn(u.oldGraph, ref), neededIn(u.newGraph, ref)
			if !slices.Equal(before, after) {
				u.plan.SetRequiredBy = append(u.plan.SetRequiredBy, Line{ID: c.ID, Version: c.Version, RequiredBy: after})
			}
		}
	}
	for _, n := range u.newGraph.Nodes {
		ref := n.Ref
		if u.declared[ref] {
			continue
		}
		if m, ok := u.moves[ref.ID]; ok && m.To == ref.Version {
			continue // 移动的新版本：那一行原地改了版本
		}
		line := Line{ID: ref.ID, Version: ref.Version, Shell: n.Manifest.IsShell()}
		if !u.isDefault(ref) {
			line.RequiredBy = neededIn(u.newGraph, ref)
		}
		u.plan.AddLines = append(u.plan.AddLines, line)
		u.plan.Added = append(u.plan.Added, ref)
	}
}

func (u *upgrader) remove(ref resolver.Ref) {
	u.plan.RemoveLines = append(u.plan.RemoveLines, ref)
	u.plan.Removed = append(u.plan.Removed, ref)
}

func (u *upgrader) removed(ref resolver.Ref) bool { return slices.Contains(u.plan.RemoveLines, ref) }

func (u *upgrader) entries() {
	var demote, promote []Rename
	for _, c := range u.p.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		old, found := u.p.Deploy.EntryAt(c.ID, c.Version, u.p.Decl.IsDefault(c.ID, c.Version))
		if !found {
			continue
		}
		owner, _, _ := manifest.SplitRef(old.Shell)
		m, moved := u.moves[c.ID]
		if u.removed(ref) {
			u.plan.RemoveEntries = append(u.plan.RemoveEntries, old.ID)
			continue
		}
		if moved && c.Version == m.From && !u.declared[resolver.Ref{ID: m.ID, Version: m.To}] {
			// 旧默认版本那一行原地改了版本：裸条目默认跟着新版本走
			to := resolver.Ref{ID: m.ID, Version: m.To}
			if !u.inNew(ref) {
				u.liftIfNotHosted(owner, to, old.ID)
				continue
			}
			// 旧版本留下了：嵌在编进了它的外壳下面的裸条目留给它，新版本在顶层另起一个
			if host, ok := u.hostOf(ref); ok && owner == host {
				demote = append(demote, Rename{From: old.ID, To: EntryID(ref.ID, ref.Version, false)})
				entry := Entry{ID: m.ID}
				if h, ok := u.hostOf(to); ok {
					entry.Under = h
				}
				u.plan.AddEntries = append(u.plan.AddEntries, entry)
				continue
			}
			entry := Entry{ID: EntryID(ref.ID, ref.Version, false)}
			if h, ok := u.hostOf(ref); ok {
				entry.Under = h
			}
			u.plan.AddEntries = append(u.plan.AddEntries, entry)
			u.liftIfNotHosted(owner, to, old.ID)
			continue
		}
		want := u.entryID(ref)
		switch want {
		case old.ID:
		case ref.ID:
			promote = append(promote, Rename{From: old.ID, To: want})
		default:
			demote = append(demote, Rename{From: old.ID, To: want})
		}
		u.liftIfNotHosted(owner, ref, want)
	}
	u.plan.RenameEntries = append(demote, promote...)
	for _, ref := range u.plan.Added {
		entry := Entry{ID: u.entryID(ref)}
		if h, ok := u.hostOf(ref); ok {
			entry.Under = h
		}
		u.plan.AddEntries = append(u.plan.AddEntries, entry)
	}
}

// liftIfNotHosted：条目嵌在外壳 owner 下面，而新世界里 owner 不编进 ref——挪到顶层独立运行（§8.7）。
func (u *upgrader) liftIfNotHosted(owner string, ref resolver.Ref, entryID string) {
	if owner == "" {
		return
	}
	shell := u.shells[owner]
	if v, ok := shell.HostedVersion(ref.ID); shell != nil && ok && v == ref.Version {
		return
	}
	u.plan.LiftEntries = append(u.plan.LiftEntries, entryID)
}

func (u *upgrader) configs() {
	for _, c := range u.p.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		oldDefault := u.p.Decl.IsDefault(c.ID, c.Version)
		m, moved := u.moves[c.ID]
		if moved && c.Version == m.From {
			u.moveConfig(m, ref)
			continue
		}
		if u.removed(ref) {
			u.plan.ArchiveConfigs = append(u.plan.ArchiveConfigs, ConfigRef{ID: c.ID, Version: c.Version, Versioned: !oldDefault})
		}
	}
	for _, ref := range u.plan.Added {
		if n := u.newGraph.Node(ref); n != nil && hasSchema(n.Manifest) {
			u.plan.AddConfigs = append(u.plan.AddConfigs,
				ConfigFile{ID: ref.ID, Version: ref.Version, Versioned: !u.isDefault(ref), Schema: n.Manifest.ConfigSchema})
		}
	}
}

// moveConfig 处理被移动的默认版本的配置：旧版本留下就改名成带版本号的文件，否则归档；新版本
// 原本有自己的配置（兼容版本）就转正它，否则从旧文件迁移过来（§12.2）。
func (u *upgrader) moveConfig(m Move, from resolver.Ref) {
	to := resolver.Ref{ID: m.ID, Version: m.To}
	kept := u.inNew(from)
	// Source 是升级之前文件所在的位置（落盘时先读来源、再挪文件）
	source := ConfigRef{ID: from.ID, Version: from.Version}
	if kept {
		u.plan.DemoteConfigs = append(u.plan.DemoteConfigs, source)
	} else {
		u.plan.ArchiveConfigs = append(u.plan.ArchiveConfigs, source)
	}
	if u.declared[to] && u.p.Config(to.ID, to.Version) != nil {
		u.plan.RenameConfigs = append(u.plan.RenameConfigs, ConfigRef{ID: to.ID, Version: to.Version, Versioned: true})
		return
	}
	newNode := u.newGraph.Node(to)
	if newNode == nil || !hasSchema(newNode.Manifest) {
		return
	}
	migration := ConfigMigration{ID: m.ID, From: m.From, To: m.To, Source: source,
		Target: ConfigRef{ID: to.ID, Version: to.Version}, NewSchema: newNode.Manifest.ConfigSchema}
	if oldNode := u.oldGraph.Node(from); oldNode != nil {
		migration.OldSchema = oldNode.Manifest.ConfigSchema
	}
	u.plan.MigrateConfigs = append(u.plan.MigrateConfigs, migration)
}
