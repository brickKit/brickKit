package compose

// 本文件是外壳在 Docker 下的渲染：被承载的成员不生成主容器，外壳挂上成员的
// 网络别名，外壳的环境里多出 BRICKKIT_SERVED_MEMBERS 与（放进 env 文件的）
// BRICKKIT_SERVED_MEMBERS_CONFIG。谁被承载由 cascade.Result.HostOf 决定，与 K8s 渲染器同一个判据。

import (
	"sort"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedComponent 是一个 外壳成员：不生成自己的容器/迁移，代码跑
// 在 Shell 那个组件的容器里。
type servedComponent struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    deployfile.Entry
	Shell    resolver.Ref
	// Env 是它独立运行时的那份环境：经由外壳的 JSON 交给外壳进程，本地调试的地址改写同样作用于它。
	Env inject.Component
}

// applyShellGroups 把每个外壳分组合并进外壳自己的环境变量/labels，并
// 记下它要挂哪些网络别名（供 componentService 渲染 networks 段用）。
func (p *plan) applyShellGroups(groups []shell.Group) {
	byShell := make(map[resolver.Ref]shell.Group, len(groups))
	for _, g := range groups {
		byShell[g.Shell] = g
	}

	// 每个声明成外壳（kind: shell）、这次在跑的组件都要拿到两个保留变量，哪怕一个成员都没有：
	// 空值说的是"零个成员"，变量不存在说的是"不受平台管辖"——外壳读到后者可能回落成
	// 启动全部模块，与那些组件自己的容器重复运行。
	referencedShells := map[resolver.Ref]bool{}
	for _, c := range p.proj.Decl.Components {
		if c.IsShell() {
			referencedShells[resolver.Ref{ID: c.ID, Version: c.Version}] = true
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
	// 裸进程外壳：两个保留变量进它的本地 env 文件（IDE 加载它；mode: local 时 brickkit 启动进程时带上）
	for i := range p.locals {
		ref := p.locals[i].Ref
		g, ok := byShell[ref]
		if !ok {
			if !referencedShells[ref] {
				continue
			}
			g = shell.Group{Shell: ref}
		}
		p.locals[i].Env.Env = shell.Apply(p.locals[i].Env.Env, g)
	}
}

// shellOf 判断 ref 是不是某个 外壳成员，是则返回它指向的外壳 ref。
func (p *plan) shellOf(ref resolver.Ref) (resolver.Ref, bool) {
	for _, s := range p.served {
		if s.Ref == ref {
			return s.Shell, true
		}
	}
	return resolver.Ref{}, false
}

// fallbackStandaloneWarnings 提醒"这个组件本来写在外壳下面，但这次它
// 指向的外壳没跑，所以按自己的镜像独立部署了"——不说清楚的话，使用者
// 会以为代码照常跑在外壳里，实际上跑的是它自己的镜像，而且它自己的迁移
// 这次是真的会执行。
func (p *plan) fallbackStandaloneWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, c := range p.components {
		shellRef, ok := cascade.ShellOf(p.proj, c.Ref)
		if !ok {
			continue
		}
		hints := []string{i18n.T(msgid.ShellServedHintFallbackEnableShellToMergeAgain)}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.ShellServedFallbackStandalone)).
			WithDetail(i18n.T(msgid.LabelComponent), refText(c.Ref)).
			WithDetail(i18n.T(msgid.LabelShell), refText(shellRef)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellServedFallbackReasonDetail)).
			WithHint(hints...))
	}
	return out
}

// copyComponent 复制一份注入结果：成员交给外壳的那份环境会按外壳所在的位置改写地址
// （外壳是裸进程时改成 localhost），而成员的迁移容器必须保留容器里的地址——两份不能共用底层数组。
func copyComponent(c inject.Component) inject.Component {
	c.Env = append([]inject.Var(nil), c.Env...)
	return c
}

// envForShells 返回交给 shell.Resolve 的注入结果：被承载成员的环境换成计划里改写过的那一份。
func (p *plan) envForShells(env *inject.Result) *inject.Result {
	byRef := map[resolver.Ref]inject.Component{}
	for _, s := range p.served {
		byRef[s.Ref] = s.Env
	}
	out := &inject.Result{Warnings: env.Warnings}
	for _, c := range env.Components {
		if served, ok := byRef[c.Ref]; ok {
			c = served
		}
		out.Components = append(out.Components, c)
	}
	return out
}

// bareShell 报告这个外壳这次是不是裸进程（mode: debug / local）：它承载的成员也就是宿主机上的进程。
func (p *plan) bareShell(shellRef resolver.Ref) bool {
	return p.proj.DeployEntry(shellRef.ID, shellRef.Version).IsBareProcess()
}

// hostMember 报告 ref 是不是被一个裸进程外壳承载的成员（这次在宿主机上，由外壳进程替它监听端口）。
func (p *plan) hostMember(ref resolver.Ref) (servedComponent, bool) {
	for _, s := range p.served {
		if s.Ref == ref && p.bareShell(s.Shell) {
			return s, true
		}
	}
	return servedComponent{}, false
}

// runAfter 是裸进程外壳承载的成员的迁移 service：外壳没有容器，没有 service 等着它们，
// 由引擎在 up 之后单独跑（按服务名排序）。
//
// 例外是迁移链上排在前面的那个：同一组件的另一个版本独立部署、迁移排在它后面时，up 会经
// 后一版迁移的 depends_on 把它带起来——再列进来就会在一次 up 里跑两遍。排在后面的那个 up
// 带不到，列进来；引擎以 --no-deps 跑它，前一版此时已经由 up 跑完。
func (p *plan) runAfter() []string {
	predecessors := map[string]bool{}
	for _, previous := range p.migrationAfter {
		predecessors[previous] = true
	}
	var out []string
	for _, m := range p.memberMigrations {
		service := migrationService(m.Service)
		if shellRef, ok := p.shellOf(m.Ref); ok && p.bareShell(shellRef) && !predecessors[service] {
			out = append(out, service)
		}
	}
	return out
}

// bareSkipWaitForWarnings 提醒"skipWaitFor 这次不起作用"：以裸进程运行的组件、裸进程外壳里的
// 成员都不在 compose 文件里，没有 depends_on 可去。不拦——换回容器时它又会生效。
func (p *plan) bareSkipWaitForWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, ref := range p.states.Running() {
		if len(p.proj.DeployEntry(ref.ID, ref.Version).SkipWaitFor) == 0 {
			continue
		}
		_, hostMember := p.hostMember(ref)
		if p.proj.DeployEntry(ref.ID, ref.Version).IsBareProcess() || hostMember {
			out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
				i18n.T(msgid.ComposeSkipWaitForOnBareProcess, refText(ref))))
			continue
		}
		out = append(out, p.coHostedSkipWarnings(ref)...)
	}
	return out
}

// coHostedSkipWarnings 提醒"跳过的依赖跟它在同一个外壳里"：调用不出进程，本来就没有等待可跳过——
// 写了等于没写，使用者却以为那条等待已经去掉了。
func (p *plan) coHostedSkipWarnings(ref resolver.Ref) []*clierr.Error {
	host, ok := p.states.HostOf(p.proj, ref)
	node := p.graph.Node(ref)
	if !ok || node == nil {
		return nil
	}
	var out []*clierr.Error
	for _, id := range p.proj.DeployEntry(ref.ID, ref.Version).SkipWaitFor {
		for _, dep := range node.Requires {
			if depHost, hosted := p.states.HostOf(p.proj, dep); dep.ID == id && hosted && depHost == host {
				out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
					i18n.T(msgid.ComposeSkipWaitForCoHosted, refText(ref), refText(dep), refText(host))))
			}
		}
	}
	return out
}
