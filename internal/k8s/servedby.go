package k8s

// 本文件是外壳（提案 §8）在 K8s 下的渲染：被承载的成员不生成 Deployment，只生成一个选中
// 外壳 Pod 的 Service（成员自己的服务名照样能解析）；外壳的 JSON 进外壳的 Secret。
// mode: debug / local 在 K8s 下不合法，部署文件解析阶段就拦下了。

import (
	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedPlan 是一个 外壳成员：只生成一个指向外壳 Pod 的 Service，
// 不生成 Deployment/Job——它没有自己的工作负载。
type servedPlan struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    deployfile.Entry
	Shell    resolver.Ref
}

// applyShellGroups 把每个外壳分组合并进外壳自己的环境变量/labels。
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
	}
}

// servedServiceDoc 渲染一个 外壳成员的 Service：selector 指向外壳
// 的 Pod（labelApp: 外壳的服务名），而不是它自己——它没有自己的
// Deployment，这个 Service 存在的唯一目的是让它自己的版本化服务名解析
// 到外壳的 Pod（提案 §8）。
func (p *plan) servedServiceDoc(m servedPlan) map[string]any {
	shellService := manifest.ServiceName(m.Shell.ID, m.Shell.Version)
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      m.Service,
			"namespace": p.namespace,
			"labels": map[string]any{
				labelComponent:        containerName(m.Ref.ID),
				labelComponentVersion: m.Ref.Version,
				labelProject:          p.proj.Decl.Project,
			},
			"annotations": map[string]any{annotationComponentID: m.Ref.ID},
		},
		"spec": map[string]any{
			"selector": map[string]any{labelApp: shellService},
			"ports":    servicePorts(m.Manifest),
			"type":     "ClusterIP",
		},
	}
}

// fallbackStandaloneWarnings 跟 compose 侧同名函数职责相同——见那边的注释。
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
			WithDetail(i18n.T(msgid.LabelComponent), c.Ref.String()).
			WithDetail(i18n.T(msgid.LabelShell), shellRef.String()).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellServedFallbackReasonDetail)).
			WithHint(hints...))
	}
	return out
}

// shellOf 判断 ref 是不是某个 外壳成员，是则返回它指向的外壳 ref。
//
// 供 hardening.go/egress.go 在依赖图上走边时用：一条边的另一端如果是
// 外壳成员，它没有自己的 Pod，真正的流量目的地是它的外壳。
func (p *plan) shellOf(ref resolver.Ref) (resolver.Ref, bool) {
	for _, s := range p.served {
		if s.Ref == ref {
			return s.Shell, true
		}
	}
	return resolver.Ref{}, false
}
