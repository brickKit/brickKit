package compose

// 本文件是外壳（提案 §8）在 Docker 下的渲染：被承载的成员不生成主容器，外壳挂上成员的
// 网络别名，外壳的环境里多出 BRICKKIT_SERVED_MEMBERS 与（放进 env 文件的）
// BRICKKIT_SERVED_MEMBERS_CONFIG。谁被承载由 cascade.Result.HostOf 决定，与 K8s 渲染器同一个判据。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedComponent 是一个 servedBy 组件：不生成自己的容器/迁移，代码跑
// 在 Shell 那个组件的容器里。
type servedComponent struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    deployfile.Component
	Shell    resolver.Ref
}

// applyShellGroups 把每个外壳分组合并进外壳自己的环境变量/labels，并
// 记下它要挂哪些网络别名（供 componentService 渲染 networks 段用）。
func (p *plan) applyShellGroups(groups []shell.Group) {
	byShell := make(map[resolver.Ref]shell.Group, len(groups))
	for _, g := range groups {
		byShell[g.Shell] = g
	}

	// shell.Resolve 只看 states.Running()：一个 servedBy 成员如果自己被
	// mode: disable 关掉，它压根不出现在 states.Running() 里，于是
	// shell.Resolve 不会为它的外壳产出任何 Group。但外壳本身如果还在跑，
	// BRICKKIT_SERVED_MEMBERS 依旧必须显式写成空字符串，不能让整个变量
	// 消失——"空字符串"（零个成员激活）与"变量不存在"（不受平台管辖）
	// 语义相反，不能合并处理（servedBy 设计书 §7）。这里只在本渲染器内部
	// 兜底一个空 Group，不改 shell.Resolve 的行为——那是与 K8s 渲染器
	// 共用的逻辑，不属于这个包的职责范围。
	referencedShells := map[resolver.Ref]bool{}
	for _, c := range p.proj.Decl.Components {
		if ref, ok := cascade.ShellOf(p.proj, resolver.Ref{ID: c.ID, Version: c.Version}); ok {
			referencedShells[ref] = true
		}
	}

	for i := range p.components {
		ref := p.components[i].Ref
		g, ok := byShell[ref]
		if !ok {
			if !referencedShells[ref] {
				continue
			}
			g = shell.Group{Shell: ref}
		}
		p.components[i].Env.Env = shell.Apply(p.components[i].Env.Env, g)

		aliases := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			aliases = append(aliases, manifest.ServiceName(m.Ref.ID, m.Ref.Version))
		}
		sort.Strings(aliases)
		p.shellAliases[p.components[i].Service] = aliases
	}
}

// shellOf 判断 ref 是不是某个 servedBy 成员，是则返回它指向的外壳 ref。
func (p *plan) shellOf(ref resolver.Ref) (resolver.Ref, bool) {
	for _, s := range p.served {
		if s.Ref == ref {
			return s.Shell, true
		}
	}
	return resolver.Ref{}, false
}

// fallbackStandaloneWarnings 提醒"这个组件本来声明了 servedBy，但这次它
// 指向的外壳没跑，所以按自己的镜像独立部署了"——不说清楚的话，使用者
// 会以为代码照常跑在外壳里，实际上跑的是它自己的镜像，而且它自己的迁移
// 这次是真的会执行（外壳独立部署回落设计书 §6.1/§7）。
func (p *plan) fallbackStandaloneWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, c := range p.components {
		shellRef, ok := cascade.ShellOf(p.proj, c.Ref)
		if !ok {
			continue
		}
		hints := []string{i18n.T(msgid.HintFallbackEnableShellToMergeAgain)}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.ServedByFallbackStandalone)).
			WithDetail(i18n.T(msgid.LabelComponent), refText(c.Ref)).
			WithDetail(i18n.T(msgid.LabelShell), refText(shellRef)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ServedByFallbackReasonDetail)).
			WithHint(hints...))
	}
	return out
}

// servedHealthCheckWarnings 提醒"servedBy 组件自己的健康检查不会独立
// 生效"——它没有自己的容器，健康检查完全是外壳实现者自己的责任，平台
// 不做任何聚合、也不替外壳生成任何健康检查逻辑。
func (p *plan) servedHealthCheckWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.HealthCheck.Type == manifest.HealthCheckNone {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.ServedHealthCheckNotIndependent)).
			WithDetail(i18n.T(msgid.LabelComponent), refText(s.Ref)).
			WithDetail(i18n.T(msgid.LabelShell), refText(s.Shell)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ComposeServedHealthCheckReasonDetail)))
	}
	return out
}

// servedUnsupportedFieldWarnings 提醒"这些字段对 servedBy 组件不生效"。
//
// expose / exposePort / hostname / replicas / resources /
// serviceAccountName / labels 描述的都是"我自己这个容器该怎么部署"——而
// servedBy 组件没有自己的容器，这些字段天然没有对象可以落地（v1 范围裁剪，
// 见设计书实施记录）。labels 原先走的是合并路线，直到真机复现出
// prometheus.io/port 这类"语义上必然因组件而异"的键必然合并冲突，才改成
// 跟这一批字段同样的"警告 + 忽略"（brickKit 反馈：servedBy 的 labels
// 合并漏了排除规则；见 mergeGroup 的注释）。
func (p *plan) servedUnsupportedFieldWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		var fields []string
		if s.Entry.Expose {
			fields = append(fields, "expose")
		}
		if s.Entry.ExposePort != 0 {
			fields = append(fields, "exposePort")
		}
		if s.Entry.Hostname != "" {
			fields = append(fields, "hostname")
		}
		if s.Entry.Replicas != nil {
			fields = append(fields, "replicas")
		}
		if s.Entry.Resources != nil {
			fields = append(fields, "resources")
		}
		if s.Entry.ServiceAccountName != "" {
			fields = append(fields, "serviceAccountName")
		}
		if shell.MemberLabels(s.Manifest, s.Entry) != nil {
			fields = append(fields, "labels")
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.ServedFieldsIgnored, strings.Join(fields, "/"))).
			WithDetail(i18n.T(msgid.LabelComponent), refText(s.Ref)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ComposeServedFieldsReasonDetail, s.Shell.ID)).
			WithHint(i18n.T(msgid.HintDropServedBy)))
	}
	return out
}
