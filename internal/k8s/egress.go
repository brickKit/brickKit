package k8s

// 本文件渲染出站策略（P37）。
//
// # 出站与入站不是对称的
//
// NetworkPolicy 的语义是：一个 Pod 只要在 **Egress 方向**被任何策略选中，
// 该方向**未明确允许的一律拒绝**。所以生成第一条出站规则的那一刻，
// 组件就从"想连谁连谁"翻转成了"只能连白名单"。
//
// 漏掉一项的后果取决于组件**什么时候建连**（都在 calico 上实测过）：
//
//	漏了 DNS      什么都不通
//	启动时建连    组件起不来，rollout 失败——显眼，但要等到部署时才发现
//	首次请求建连  健康检查照过（/healthz 只查本进程，002 §9.4），业务请求失败
//
// 更阴险的是**改策略不会杀掉已建立的连接**：正在跑的组件照常工作，
// 问题要等到下一次重启（节点排空、升级、扩缩容）才暴露，可能是几周以后。
//
// 因此这里的设计重点不是"能不能生成"，而是**不让人漏**：
//
//	DNS       平台自动放行（谁都要，漏了必挂）
//	组件依赖  从依赖图推导（平台已经知道，让人再写一遍必然过期）
//	外部目标  数据库等外部服务现在只是某个组件 config 里的一串地址，平台不认识它们，
//	          由使用者在 k8s.networkPolicy.egress.allowTo 里直接写位置与端口（附录 A15）

import (
	"sort"

	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// dnsPort 是 DNS 端口。UDP 与 TCP 都要放：大响应会退回 TCP，
// 只放 UDP 的表现是"平时好好的，偶尔解析失败"。
const dnsPort = 53

// egressRules 渲染一个组件的出站规则。
func (p *plan) egressRules(c componentPlan) []any {
	rules := []any{dnsRule()}

	if to := p.dependencyTargets(c); len(to) > 0 {
		rules = append(rules, to...)
	}
	rules = append(rules, p.declaredTargets(c)...)
	return rules
}

// dnsRule 放行集群内任意命名空间的 53 端口。
//
// 为什么不钉死 kube-system：kube-dns / CoreDNS 在哪个命名空间、什么标签，
// 各集群不一样。逼使用者去查一次再抄进配置，只会制造一个必踩的坑——
// 而漏了 DNS 的后果是**什么都不通**。
//
// 安全上的让步很小：`namespaceSelector: {}` 只匹配**集群内**的 Pod，
// 出不了集群，够不着外部的 DNS 服务器。
func dnsRule() map[string]any {
	return map[string]any{
		"to": []any{map[string]any{"namespaceSelector": map[string]any{}}},
		"ports": []any{
			map[string]any{"protocol": "UDP", "port": dnsPort},
			map[string]any{"protocol": "TCP", "port": dnsPort},
		},
	}
}

// dependencyTargets 是"我能连谁"：本组件依赖的那些组件。
//
// 与入站方向是同一张图的两面（dependentSources 问的是"谁能连我"）。
// 强弱依赖都算，理由与 D381 相同：弱依赖在对方存在时是真会去连的。
//
// 外壳还包括它承载的成员的依赖（shell.Dependencies）：成员的代码跑在外壳 Pod 里，
// 流量是从外壳 Pod 发出去的。
func (p *plan) dependencyTargets(c componentPlan) []any {
	requires, optional := shell.Dependencies(p.proj, p.graph, p.states, c.Ref)

	running := map[resolver.Ref]componentPlan{}
	for _, other := range p.components {
		running[other.Ref] = other
	}

	type depTarget struct {
		service string
		mf      *manifest.Manifest
	}
	var deps []depTarget
	// 强依赖与弱依赖都算：弱依赖在对方存在时是真会去连的（D381）
	for _, ref := range append(requires, optional...) {
		if dep, ok := running[ref]; ok {
			deps = append(deps, depTarget{service: dep.Service, mf: dep.Manifest})
			continue
		}
		// 依赖的这个组件是 servedBy 成员：它没有自己的 Pod，真正的连接
		// 目的地是它的外壳，端口用它自己声明的那个（不是外壳的端口）。
		if shellRef, ok := p.shellOf(ref); ok {
			memberNode := p.graph.Node(ref)
			if memberNode == nil || memberNode.Manifest == nil {
				continue
			}
			deps = append(deps, depTarget{
				service: manifest.ServiceName(shellRef.ID, shellRef.Version),
				mf:      memberNode.Manifest,
			})
		}
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].service < deps[j].service })

	out := make([]any, 0, len(deps))
	for _, dep := range deps {
		out = append(out, map[string]any{
			// 不带 namespaceSelector 就是"同命名空间内"——一个项目的组件都在一起
			"to": []any{map[string]any{
				"podSelector": map[string]any{
					"matchLabels": map[string]any{labelApp: dep.service},
				},
			}},
			"ports": policyPorts(allPortsOf(dep.mf)),
		})
	}
	return out
}

// declaredTargets 渲染使用者声明的出站目标。
func (p *plan) declaredTargets(c componentPlan) []any {
	var out []any
	for _, target := range p.proj.Deploy.K8s.NetworkPolicy.Egress.AllowTo {
		out = append(out, map[string]any{
			"to":    []any{targetLocation(target)},
			"ports": policyPorts(target.Ports),
		})
	}
	return out
}

// targetLocation 渲染目标位置：集群内是命名空间 + 标签，集群外是 CIDR。
func targetLocation(target deployfile.AllowToTarget) map[string]any {
	if target.CIDR != "" {
		return map[string]any{"ipBlock": map[string]any{"cidr": target.CIDR}}
	}
	return namespacedSource(target.Namespace, target.PodSelector)
}
