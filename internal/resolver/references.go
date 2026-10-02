package resolver

import (
	"slices"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
)

// 本文件把配置里的 $endpoint: 引用加进依赖图。
//
// # 为什么它不是一条依赖
//
// 引用是项目写的（config/、vars.yaml、部署文件的 vars:），不是组件声明的：组件只说"我要一个地址"，
// 填哪个组件是项目的事——槽位家族的成员正是靠这一点才可以替换。项目里这种引用常常成环
// （权限组件要身份组件的公钥验令牌，身份组件登录时又要问权限组件），而调用双方都按"对方暂时不在就退避重试"写，
// 谁也不必等谁先起来。所以这条边：
//
//	进     地址（注入阶段按它算，外壳承载、本机进程都跟着改写）、级联（被引用的跟着引用它的跑）、
//	       网络策略（放行这条连接）、graph / deps（看得见谁在用它）
//	不进   启动顺序与判环（depends_on 只来自 Manifest 的强依赖）、Dependents（requiredBy 只由 Manifest 决定）
//
// 指向自己的引用不是边：注入时直接算成自己的地址。没在 brickkit.yaml 里声明的目标这里只跳过——
// 建图也服务 add 这类还在补齐项目的命令；它由 up 与 lint 报错。

// Users 是会让这个组件"跟着跑"的上层：依赖它的组件，加上配置引用了它的组件（cascade 用）。
func (n *Node) Users() []Ref {
	out := append([]Ref{}, n.Dependents...)
	for _, r := range n.ReferencedBy {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// addReferences 读每个节点这次生效的配置，把其中的 $endpoint: 引用连成边。
func addReferences(g *Graph, p *project.Project) {
	for _, node := range g.Nodes {
		for _, target := range ReferencedTargets(p, node.Ref, node.Manifest) {
			if target == node.Ref || !g.Has(target) || slices.Contains(node.References, target) {
				continue
			}
			node.References = append(node.References, target)
			t := g.Node(target)
			t.ReferencedBy = append(t.ReferencedBy, node.Ref)
		}
	}
}

// ReferencedTargets 是 ref 这次生效的配置里 $endpoint: 引用的组件版本（没写版本的取项目里的默认版本），
// 按配置键的顺序、去重。配置解析不了（有别的问题）时返回能认出来的那些——那些问题由装载与注入去报。
func ReferencedTargets(p *project.Project, ref Ref, m *manifest.Manifest) []Ref {
	var schema *manifest.ConfigSchema
	if m != nil {
		schema = m.ConfigSchema
	}
	resolved, err := configdir.Resolve(p.ConfigInput(ref.ID, ref.Version, schema))
	if err != nil {
		return nil
	}
	var out []Ref
	for _, r := range resolved.Values {
		if r.Value.Kind != configdir.KindEndpointRef {
			continue
		}
		target, ok := EndpointTarget(p, r.Value.Endpoint)
		if ok && !slices.Contains(out, target) {
			out = append(out, target)
		}
	}
	return out
}

// EndpointTarget 是一条 $endpoint: 引用指向的组件版本：写了版本就是它，没写就是项目里的默认版本。
// ok 为假表示项目里没有这个组件（或没有这个版本）。
func EndpointTarget(p *project.Project, e configdir.EndpointRef) (Ref, bool) {
	if e.Version != "" {
		return Ref{ID: e.ID, Version: e.Version}, slices.Contains(p.Decl.Versions(e.ID), e.Version)
	}
	version, ok := p.Decl.DefaultVersion(e.ID)
	return Ref{ID: e.ID, Version: version}, ok
}
