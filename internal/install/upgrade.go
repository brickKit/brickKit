package install

// 本文件规划 upgrade：把一组默认版本的移动变成对三份文件的改动。
//
// # 思路：比较升级前后的"世界"
//
// 项目要跑的版本 = 默认版本、外壳承载的成员（外壳编进的那个版本），以及它们的依赖闭包（ResolveWorlds）。
// 哪个外壳承载哪个成员按 hosting.go 的规则：升级外壳不改承载位置——别的外壳承载的、在顶层独立运行的
// 成员不跟着它走，使用者移除的成员不加回来（Notes 里说一声）。升级前后各算一次：
//
//	新世界有、项目里没有           加进来（像 add：默认版本，或带 requiredBy 的兼容版本）
//	旧世界有、新世界没有           移除（配置归档）——只动升级带来的变化，旧世界里本来就没人要的
//	                                行（手写的）不碰
//	两边都有的兼容版本             谁需要它变了，requiredBy 跟着改成新世界里真正需要它的组件
//
// 被移动的默认版本 v1 → v2：brickkit.yaml 那一行原地改版本；v1 在新世界里还有人要（依赖方、承载
// 它的外壳）就另起一行带 requiredBy 留下，配置文件改名成带版本号的；v2 原本就是兼容版本时它转正。
//
// # 部署条目
//
// 每个版本在新世界里写成什么 id：默认版本写裸 ID，其余写 id@version。裸条目（带着使用者的字段）
// 跟着默认版本走——例外是它嵌在一个承载着 v1 的外壳下面、而 v1 留下了：那它留给 v1（改名成
// id@v1），v2 在顶层另起一个裸条目，P3f 的核对照样成立。改名时先降级（裸 → id@v）再升级
// （id@v → 裸），同一时刻不会有两个裸条目。嵌在外壳下面、而新外壳没编进这个版本的条目挪到顶层。

import (
	"context"
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

// ResolveWorlds 解析升级前（before）与 moves 之后（after）的两个世界：以默认版本为根，外壳承载的
// 成员也是根（它们不是依赖边），直到不再出现新的外壳成员。after 要看 before：升级前就有外壳编进、
// 项目里却没有的成员是使用者移除的（removedMembers），升级不把它加回来。
func ResolveWorlds(ctx context.Context, r *resolver.Resolver, p *project.Project, moves []Move) (before, after *resolver.Graph, err error) {
	before, err = resolveWorld(ctx, r, p, nil, worldHosting(p, nil, nil))
	if err != nil {
		return nil, nil, err
	}
	after, err = resolveWorld(ctx, r, p, moves, worldHosting(p, moves, removedMembers(p, before)))
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

func resolveWorld(ctx context.Context, r *resolver.Resolver, p *project.Project, moves []Move, hosting Hosting) (*resolver.Graph, error) {
	roots := Defaults(p, moves)
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
		for _, ref := range hosting.Place(graph).Hosted() {
			if !seen[ref] {
				seen[ref] = true
				roots = append(roots, ref)
				grew = true
			}
		}
		if !grew {
			return graph, nil
		}
	}
}

// ShellMoves 把外壳的升级展开成成员的版本移动（外壳编进的是成员的精确版本）：由这个外壳承载、默认
// 版本正是旧外壳编进的那个版本的成员，跟着换成新外壳编进的版本。别的外壳承载的、在顶层独立运行的
// 成员不动（承载位置是使用者定的）。只为外壳保留的兼容版本、新成员、被去掉的成员由 PlanUpgrade 按
// 新旧世界的差处理。
func ShellMoves(p *project.Project, oldShell, newShell *manifest.Manifest) []Move {
	if !oldShell.IsShell() || !newShell.IsShell() {
		return nil // 新版本不再是外壳（或原来就不是）：没有成员可跟
	}
	var moves []Move
	for _, member := range newShell.Shell.Members {
		id, to, _ := manifest.SplitRef(member)
		from, ok := oldShell.HostedVersion(id)
		if !ok || from == to || !p.Decl.IsDefault(id, from) {
			continue
		}
		if owner, _ := p.MemberShell(id); owner != newShell.Metadata.ID {
			continue
		}
		moves = append(moves, Move{ID: id, From: from, To: to})
	}
	return moves
}

// PlanUpgrade 规划一组移动。oldGraph / newGraph 是 ResolveWorlds 解析的升级前后两个世界。
func PlanUpgrade(p *project.Project, oldGraph, newGraph *resolver.Graph, moves []Move) (*Plan, error) {
	for _, m := range moves {
		if !p.Decl.IsDefault(m.ID, m.From) {
			ref := resolver.Ref{ID: m.ID, Version: m.From}
			return nil, clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.InstallUpgradeNotDefault, ref.String())).
				WithHint(i18n.T(msgid.InstallHintUpgradeDependents, ref.String()))
		}
	}
	u := newUpgrader(p, oldGraph, newGraph, moves)
	if err := u.checkKinds(); err != nil {
		return nil, err
	}
	for _, m := range moves {
		if manifest.CompareVersions(m.To, m.From) < 0 {
			u.plan.Notes = append(u.plan.Notes, i18n.T(msgid.InstallNoteDowngrade, m.ID, m.From, m.To))
		}
	}
	u.lines()
	u.entries()
	u.nestIntoMovedShells()
	u.noteMembersNotFollowing()
	u.noteRemovedMembersBack()
	noteCoexistence(u.plan, u.defaultVersion)
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
	// oldHosts / newHosts 是升级前后两个世界里的承载关系。
	oldHosts, newHosts Placement
	// removedMembers 是使用者移除的成员 ID（见 removedMembers 函数）。
	removedMembers map[string]bool
}

func newUpgrader(p *project.Project, oldGraph, newGraph *resolver.Graph, moves []Move) *upgrader {
	u := &upgrader{p: p, oldGraph: oldGraph, newGraph: newGraph, moves: map[string]Move{}, plan: &Plan{},
		declared: map[resolver.Ref]bool{}}
	for _, m := range moves {
		u.moves[m.ID] = m
		u.plan.Moves = append(u.plan.Moves, m)
	}
	for _, c := range p.Decl.Components {
		u.declared[resolver.Ref{ID: c.ID, Version: c.Version}] = true
	}
	u.removedMembers = removedMembers(p, oldGraph)
	u.oldHosts = worldHosting(p, nil, nil).Place(oldGraph)
	u.newHosts = worldHosting(p, moves, u.removedMembers).Place(newGraph)
	return u
}

// isDefault 报告 ref 在新世界里是不是默认版本。
func (u *upgrader) isDefault(ref resolver.Ref) bool {
	v, ok := u.defaultVersion(ref.ID)
	return ok && v == ref.Version
}

// defaultVersion 是新世界里这个组件 ID 的默认版本。
func (u *upgrader) defaultVersion(id string) (string, bool) {
	if m, ok := u.moves[id]; ok {
		return m.To, true
	}
	if len(u.p.Decl.Versions(id)) > 0 {
		return u.p.Decl.DefaultVersion(id)
	}
	// 新出现的组件：默认版本是新世界里它最高的版本
	versions := u.newGraph.Versions(id)
	if len(versions) == 0 {
		return "", false
	}
	return versions[len(versions)-1], true
}

// entryID 是 ref 在新世界里的部署条目 id。
func (u *upgrader) entryID(ref resolver.Ref) string {
	return EntryID(ref.ID, ref.Version, u.isDefault(ref))
}

func (u *upgrader) inNew(ref resolver.Ref) bool { return u.newGraph.Has(ref) }
func (u *upgrader) inOld(ref resolver.Ref) bool { return u.oldGraph.Has(ref) }

// neededIn 是 graph 所在的世界里需要 ref 的组件 ID：依赖方、承载它的外壳（hosts 是那个世界的承载关系）。
func neededIn(graph *resolver.Graph, hosts Placement, ref resolver.Ref) []string {
	var out []string
	if n := graph.Node(ref); n != nil {
		for _, d := range n.Dependents {
			if d.ID != ref.ID {
				out = append(out, d.ID)
			}
		}
	}
	if host, ok := hosts.HostOf(ref); ok {
		out = append(out, host)
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func (u *upgrader) neededBefore(ref resolver.Ref) []string { return neededIn(u.oldGraph, u.oldHosts, ref) }
func (u *upgrader) neededAfter(ref resolver.Ref) []string  { return neededIn(u.newGraph, u.newHosts, ref) }

// hostOf 是新世界里承载 ref 的外壳。
func (u *upgrader) hostOf(ref resolver.Ref) (string, bool) { return u.newHosts.HostOf(ref) }

// checkKinds：外壳只有一个版本（单版本约束）——还有组件需要旧外壳时升级不了；
// 组件在新版本里成了外壳（或不再是外壳）时 kind 跟着改，同一个 ID 还剩别的版本时就改不了
// （一个组件的几行必须同一个 kind）。
func (u *upgrader) checkKinds() error {
	for _, m := range u.plan.Moves {
		from := resolver.Ref{ID: m.ID, Version: m.From}
		to := resolver.Ref{ID: m.ID, Version: m.To}
		oldShell := u.p.Decl.IsShellID(m.ID)
		newNode := u.newGraph.Node(to)
		if newNode == nil {
			continue
		}
		newShell := newNode.Manifest.IsShell()
		if oldShell && u.inNew(from) {
			return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.InstallUpgradeShellStillNeeded, from.String(), joinStrings(u.neededAfter(from)))).
				WithHint(i18n.T(msgid.InstallHintUpgradeShellDependents, from.String()))
		}
		if oldShell == newShell {
			continue
		}
		if others := len(u.p.Decl.Versions(m.ID)); others > 1 {
			return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.InstallUpgradeKindChange, to.String())).
				WithHint(i18n.T(msgid.InstallHintKindChange, m.ID))
		}
		u.plan.ChangeKinds = append(u.plan.ChangeKinds, Line{ID: m.ID, Version: m.To, Shell: newShell})
		if oldShell {
			u.plan.UnnestShells = append(u.plan.UnnestShells, m.ID)
		}
	}
	return nil
}

func joinStrings(list []string) string {
	out := ""
	for i, s := range list {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
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
					u.plan.SetRequiredBy = append(u.plan.SetRequiredBy, Line{ID: c.ID, Version: c.Version, RequiredBy: u.neededAfter(ref)})
				} else {
					u.remove(ref)
				}
			} else {
				u.plan.ChangeVersions = append(u.plan.ChangeVersions, m)
				if kept {
					u.plan.AddLines = append(u.plan.AddLines, Line{ID: c.ID, Version: c.Version, Shell: c.IsShell(), RequiredBy: u.neededAfter(ref)})
				}
			}
		case moved && c.Version == m.To:
			// 在上面转正了
		case !u.inNew(ref) && u.inOld(ref) && !u.p.Decl.IsDefault(c.ID, c.Version):
			u.remove(ref)
		case len(c.RequiredBy) > 0 && u.inNew(ref):
			before, after := u.neededBefore(ref), u.neededAfter(ref)
			if !slices.Equal(before, after) {
				u.plan.SetRequiredBy = append(u.plan.SetRequiredBy, Line{ID: c.ID, Version: c.Version, RequiredBy: after})
			}
		}
	}
	for _, n := range u.newGraph.Nodes {
		ref := n.Ref
		// 只加这次升级带来的：旧世界里也有、却没声明的版本是使用者移除过的，不加回来
		if u.declared[ref] || u.inOld(ref) {
			continue
		}
		if m, ok := u.moves[ref.ID]; ok && m.To == ref.Version {
			continue // 移动的新版本：那一行原地改了版本
		}
		line := Line{ID: ref.ID, Version: ref.Version, Shell: n.Manifest.IsShell()}
		if !u.isDefault(ref) {
			line.RequiredBy = u.neededAfter(ref)
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
	keepBare := u.promotionsKeepingBare()
	for _, c := range u.p.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		old, found := u.p.Deploy.EntryAt(c.ID, c.Version, u.p.Decl.IsDefault(c.ID, c.Version))
		if !found {
			continue
		}
		owner, _, _ := manifest.SplitRef(old.Shell)
		m, moved := u.moves[c.ID]
		if moved && keepBare[c.ID] {
			switch c.Version {
			case m.From:
				// 裸条目（使用者的字段）留下，现在指新的默认版本
				u.liftIfNotHosted(owner, resolver.Ref{ID: m.ID, Version: m.To}, old.ID)
				continue
			case m.To:
				u.plan.RemoveEntries = append(u.plan.RemoveEntries, old.ID)
				continue
			}
		}
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

// promotionsKeepingBare：兼容版本转正、旧默认版本不留时，裸条目（带着使用者的字段）跟着默认版本
// 走，兼容版本的条目删掉。例外：兼容版本的条目嵌在一个编进了它的外壳下面、而裸条目不在那个外壳
// 下——那就留嵌套的那一条（改名成裸 ID），外壳里的承载关系不能丢。
func (u *upgrader) promotionsKeepingBare() map[string]bool {
	out := map[string]bool{}
	for _, m := range u.moves {
		from := resolver.Ref{ID: m.ID, Version: m.From}
		to := resolver.Ref{ID: m.ID, Version: m.To}
		if !u.declared[to] || u.inNew(from) {
			continue
		}
		toEntry, ok := u.p.Deploy.EntryAt(to.ID, to.Version, false)
		fromEntry, okFrom := u.p.Deploy.EntryAt(from.ID, from.Version, true)
		if !ok || !okFrom {
			continue
		}
		host, hosted := u.hostOf(to)
		toOwner, _, _ := manifest.SplitRef(toEntry.Shell)
		fromOwner, _, _ := manifest.SplitRef(fromEntry.Shell)
		out[m.ID] = !hosted || toOwner != host || fromOwner == host
	}
	return out
}

// nestIntoMovedShells：升级后的外壳承载的、已经声明、却在顶层的版本，挪进外壳——比如它原来承载着
// 兼容版本，新外壳编进的正是顶层的默认版本。只对这次升级的外壳做；在顶层独立运行的成员（使用者
// 移出去的）不归它承载，不动。
func (u *upgrader) nestIntoMovedShells() {
	renamed := map[string]string{}
	for _, r := range u.plan.RenameEntries {
		renamed[r.From] = r.To
	}
	for _, m := range u.plan.Moves {
		for _, ref := range u.movedShellMembers(m) {
			if host, ok := u.hostOf(ref); !ok || host != m.ID || !u.declared[ref] || u.removed(ref) {
				continue
			}
			entry, found := u.p.Deploy.EntryAt(ref.ID, ref.Version, u.p.Decl.IsDefault(ref.ID, ref.Version))
			if !found || entry.Shell != "" {
				continue
			}
			entryID := entry.ID
			if to, ok := renamed[entryID]; ok {
				entryID = to
			}
			u.plan.NestEntries = append(u.plan.NestEntries, Entry{ID: entryID, Under: m.ID})
		}
	}
}

// movedShellMembers 是移动 m 之后的外壳编进的成员版本；m 不是外壳（或新版本不再是）时为空。
func (u *upgrader) movedShellMembers(m Move) []resolver.Ref {
	shell := u.newHosts.shells[m.ID]
	if shell == nil || shell.Metadata.Version != m.To {
		return nil
	}
	var out []resolver.Ref
	for _, member := range shell.Shell.Members {
		id, version, _ := manifest.SplitRef(member)
		out = append(out, resolver.Ref{ID: id, Version: version})
	}
	return out
}

// noteMembersNotFollowing：升级后的外壳编进了成员的另一个版本，成员却不归它承载（嵌在别的外壳下面，
// 或在顶层独立运行）——它留在原来的版本，说一声，免得以为它跟着外壳升级了。使用者移除的成员不加
// 回来，也说一声，并给出加回来的命令。
func (u *upgrader) noteMembersNotFollowing() {
	for _, m := range u.plan.Moves {
		shell := resolver.Ref{ID: m.ID, Version: m.To}
		for _, ref := range u.movedShellMembers(m) {
			owner, placed := u.newHosts.Owner(ref.ID)
			if placed && owner == m.ID {
				continue
			}
			if placed {
				hosted, _ := u.newHosts.shells[owner].HostedVersion(ref.ID)
				if hosted != ref.Version {
					u.plan.Notes = append(u.plan.Notes, i18n.T(msgid.InstallNoteUpgradeMemberUnderOtherShell,
						shell.String(), ref.String(), ref.ID, owner, hosted))
				}
				continue
			}
			current, declared := u.p.Decl.DefaultVersion(ref.ID)
			switch {
			case declared && current != ref.Version:
				u.plan.Notes = append(u.plan.Notes, i18n.T(msgid.InstallNoteUpgradeMemberRunsAlone,
					shell.String(), ref.String(), ref.ID, current))
			case !declared && u.removedMembers[ref.ID]:
				u.plan.Notes = append(u.plan.Notes, i18n.T(msgid.InstallNoteUpgradeMemberRemoved,
					shell.String(), ref.String(), ref.ID))
			}
		}
	}
}

// noteRemovedMembersBack：使用者移除的成员成了新世界里某个组件的强依赖，于是加了回来（不加项目就缺
// 依赖）——说一声是谁要它、谁承载它，免得以为又是升级擅自加回来的。
func (u *upgrader) noteRemovedMembersBack() {
	for _, ref := range u.plan.Added {
		if !u.removedMembers[ref.ID] {
			continue
		}
		requirers := strings.Join(u.requirers(ref), ", ")
		if host, ok := u.hostOf(ref); ok {
			u.plan.Notes = append(u.plan.Notes, i18n.T(msgid.InstallNoteUpgradeRemovedMemberBackHosted, ref.ID, requirers, host))
		} else {
			u.plan.Notes = append(u.plan.Notes, i18n.T(msgid.InstallNoteUpgradeRemovedMemberBack, ref.ID, requirers))
		}
	}
}

// requirers 是新世界里强依赖 ref 的组件版本。
func (u *upgrader) requirers(ref resolver.Ref) []string {
	var out []string
	if n := u.newGraph.Node(ref); n != nil {
		for _, d := range n.Dependents {
			if dn := u.newGraph.Node(d); dn != nil && slices.Contains(dn.Requires, ref) {
				out = append(out, d.String())
			}
		}
	}
	sort.Strings(out)
	return out
}

// liftIfNotHosted：条目嵌在外壳 owner 下面，而新世界里 owner 不承载 ref（新外壳没编进这个版本）——
// 挪到顶层独立运行。
func (u *upgrader) liftIfNotHosted(owner string, ref resolver.Ref, entryID string) {
	if owner == "" {
		return
	}
	if host, ok := u.hostOf(ref); ok && host == owner {
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
// 原本有自己的配置（兼容版本）就转正它，否则从旧文件迁移过来。
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
