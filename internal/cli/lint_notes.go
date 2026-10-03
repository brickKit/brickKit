package cli

import (
	"slices"
	"strings"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// 本文件是 lint 给的提示（ℹ️）：两条关于 brickkit.yaml，一条关于事件。都符合规则、up 也照常跑，但多半不是使用者想要的，
// 而且别处没有任何东西会说出来——brickkit.yaml 只在显式的 add / upgrade / remove 时才动，事件平台从不核对。
// 是提示不是警告：--strict 不因为它们失败，说的也都可能正是使用者有意留着的状态。

// declNotes 是整个项目的三类提示；only 非空时只看那一个组件。
func declNotes(opts *Options, proj *project.Project, only string) []string {
	notes := append(sourceVersionNotes(opts, proj, only), orphanVersionNotes(proj, only)...)
	return append(notes, eventNotes(proj, only)...)
}

// eventNotes：组件订阅了一个项目里没有任何组件发布的事件（component.yaml 的 events）。多半是名字写错了、
// 发布方升级后不再发这个事件，或者发布方还没加进项目；但事件也可以来自项目之外，所以只是提示。
// 反过来"发布了没人订阅"不提：发给项目之外、或者暂时没人用，都是常态，brickkit deps 里看得到。
//
// 只在说得准时才提：项目里任何一个组件版本的 Manifest 盘上没有或读不了，发布方可能正是它，就一条都不提。
func eventNotes(proj *project.Project, only string) []string {
	var index resolver.EventIndex
	for _, c := range proj.Decl.Components {
		m, problem, found := diskManifest(proj, c.ID, c.Version)
		if !found || problem != "" {
			return nil
		}
		if m.Events != nil {
			index.Parties = append(index.Parties, resolver.EventParty{
				Ref: resolver.Ref{ID: c.ID, Version: c.Version}, Events: m.Events,
			})
		}
	}
	var notes []string
	for _, u := range index.Unpublished() {
		if only != "" && u.Ref.ID != only {
			continue
		}
		notes = append(notes, i18n.T(msgid.CliLintNoteEventUnpublished, u.Ref, u.Subscription))
	}
	return notes
}

// sourceVersionNotes：组件的本地源目录里是另一个版本（开发时把 metadata.version 升了），项目钉着的还是原来那个——
// 以容器方式跑的仍是钉着的版本，很容易以为跑的是新代码。本地源的版本正是项目里声明的某一个时不提。
func sourceVersionNotes(opts *Options, proj *project.Project, only string) []string {
	var notes []string
	for _, id := range proj.Decl.IDs() {
		if only != "" && id != only {
			continue
		}
		dir, ok := proj.LocalRepo(id)
		if !ok {
			continue
		}
		version, err := project.LocalRepoVersion(dir)
		if err != nil || version == "" || slices.Contains(proj.Decl.Versions(id), version) {
			continue
		}
		pinned, _ := proj.Decl.DefaultVersion(id)
		notes = append(notes, i18n.T(msgid.CliLintNoteSourceVersion, id, opts.display(dir), version, pinned))
	}
	return notes
}

// orphanVersionNotes：一个兼容版本（带 requiredBy 的行）列出的组件，按它们现在的 component.yaml 已经没有谁
// 依赖这个版本、也没有外壳编进它——依赖方在版本号不变的情况下把依赖改钉到了别的版本（本地源开发时常见），
// upgrade 认为它已是最新、什么都不做，这一行就一直留着：多一套容器、多一份配置。
//
// 只在说得准时才提：requiredBy 里任何一个组件的 Manifest 盘上没有或读不了，就不提。
func orphanVersionNotes(proj *project.Project, only string) []string {
	var notes []string
	for _, c := range proj.Decl.Components {
		if len(c.RequiredBy) == 0 || (only != "" && c.ID != only) {
			continue
		}
		if needed, known := stillRequired(proj, c.ID, c.Version, c.RequiredBy); needed || !known {
			continue
		}
		notes = append(notes, i18n.T(msgid.CliLintNoteOrphanVersion, c.Ref(),
			strings.Join(c.RequiredBy, i18n.T(msgid.ListSeparator))))
	}
	return notes
}

// stillRequired 报告 requiredBy 里的组件（项目里声明的每个版本）是否还有谁依赖 id@version 或把它编进外壳。
// known 为假表示有 Manifest 读不到，结论不可信。
func stillRequired(proj *project.Project, id, version string, requiredBy []string) (needed, known bool) {
	for _, dependent := range requiredBy {
		for _, v := range proj.Decl.Versions(dependent) {
			m, problem, found := diskManifest(proj, dependent, v)
			if !found || problem != "" {
				return false, false
			}
			if hosted, ok := m.HostedVersion(id); ok && hosted == version {
				return true, true
			}
			if m.Dependencies == nil {
				continue
			}
			for _, dep := range m.Dependencies.Components {
				if dep.ID == id && dep.Version == version {
					return true, true
				}
			}
		}
	}
	return false, true
}
