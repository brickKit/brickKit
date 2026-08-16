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
//	资源      平台知道谁要用哪个，只是不知道它在集群哪儿——声明不全就阻断

import (
	"fmt"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/resolver"
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
func (p *plan) dependencyTargets(c componentPlan) []any {
	node := p.graph.Node(c.Ref)
	if node == nil {
		return nil
	}

	running := map[resolver.Ref]componentPlan{}
	for _, other := range p.components {
		running[other.Ref] = other
	}

	var deps []componentPlan
	// 强依赖与弱依赖都算：弱依赖在对方存在时是真会去连的（D381）
	for _, ref := range append(append([]resolver.Ref{}, node.Requires...), node.Optional...) {
		if dep, ok := running[ref]; ok {
			deps = append(deps, dep)
		}
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Service < deps[j].Service })

	out := make([]any, 0, len(deps))
	for _, dep := range deps {
		out = append(out, map[string]any{
			// 不带 namespaceSelector 就是"同命名空间内"——一个项目的组件都在一起
			"to": []any{map[string]any{
				"podSelector": map[string]any{
					"matchLabels": map[string]any{labelApp: dep.Service},
				},
			}},
			"ports": policyPorts(allPortsOf(dep.Manifest)),
		})
	}
	return out
}

// declaredTargets 渲染使用者声明的出站目标。
func (p *plan) declaredTargets(c componentPlan) []any {
	var out []any
	for _, target := range p.cfg.Deploy.NetworkPolicy.Egress.AllowTo {
		// 资源类目标只加给**真的绑了它**的组件：
		// 给用不到那个库的组件开口子，等于白白放宽了范围
		if target.Resource != "" && !p.usesResource(c, target.Resource) {
			continue
		}
		out = append(out, map[string]any{
			"to":    []any{targetLocation(target)},
			"ports": policyPorts(p.portsFor(target)),
		})
	}
	return out
}

// targetLocation 渲染目标位置：集群内是命名空间 + 标签，集群外是 CIDR。
func targetLocation(target config.AllowToTarget) map[string]any {
	if target.CIDR != "" {
		return map[string]any{"ipBlock": map[string]any{"cidr": target.CIDR}}
	}
	return namespacedSource(target.Namespace, target.PodSelector)
}

// portsFor 决定放行哪些端口。
//
// 写了 resource 就从 `resources[].port` 取——那个值配置里已经有了，
// 让人再抄一遍只会出现两处不一致，而不一致的表现是"策略看着对，组件连不上库"。
func (p *plan) portsFor(target config.AllowToTarget) []int {
	if target.Resource != "" {
		for _, r := range p.cfg.Resources {
			if r.ID == target.Resource && r.Port != 0 {
				return []int{r.Port}
			}
		}
	}
	return target.Ports
}

// usesResource 判断某个组件有没有绑定某个资源。
func (p *plan) usesResource(c componentPlan, resourceID string) bool {
	for _, r := range p.cfg.Resources {
		if r.ID != resourceID {
			continue
		}
		for _, b := range r.Bindings {
			if b.ComponentID == c.Ref.ID {
				return true
			}
		}
	}
	return false
}

// checkEgressCoverage 拦下"打开了出站策略，却有资源没说在哪儿"。
//
// 这是整块出站设计的核心。不拦的话生成的策略会把数据库挡在外面，
// 而后果取决于组件什么时候建连：启动时建连的会起不来（rollout 失败），
// 首次请求才建连的则健康检查照过、业务请求失败。两种都没有任何一处提示
// 这是刚打开的那个开关干的。
//
// 最阴险的是改策略**不会杀掉已建立的连接**：正在跑的组件照常工作，
// 问题要等到下一次重启才暴露——那时离改配置可能已经过去几周。
//
// 平台有能力拦住它：谁绑了哪个资源，`resources[].bindings` 里写着。
func (p *plan) checkEgressCoverage() error {
	if !p.cfg.Deploy.EgressEnabled() {
		return nil
	}

	declared := map[string]bool{}
	for _, target := range p.cfg.Deploy.NetworkPolicy.Egress.AllowTo {
		if target.Resource != "" {
			declared[target.Resource] = true
		}
	}

	// 资源 ID → 用到它的组件，按资源 ID 排序保证报错顺序稳定
	users := map[string][]string{}
	for _, c := range p.components {
		for _, r := range p.cfg.Resources {
			if declared[r.ID] {
				continue
			}
			for _, b := range r.Bindings {
				if b.ComponentID == c.Ref.ID {
					users[r.ID] = append(users[r.ID], c.Ref.ID)
				}
			}
		}
	}
	if len(users) == 0 {
		return nil
	}

	missing := make([]string, 0, len(users))
	for id := range users {
		missing = append(missing, id)
	}
	sort.Strings(missing)

	err := clierr.New(clierr.CodeConfigInvalid,
		"错误：打开了 deploy.networkPolicy.egress，但有资源没在 allowTo 里说明位置")
	for _, id := range missing {
		err = err.WithDetail("缺少声明的资源",
			fmt.Sprintf("%s（%s 要用它）", id, joinUnique(users[id])))
	}
	return err.
		WithDetail("原因", "出站方向一旦生效，未明确允许的一律拒绝。漏掉数据库时，"+
			"启动就建连的组件会起不来，首次请求才建连的则健康检查照过、业务请求失败。"+
			"而且已经跑着的实例不受影响，问题要到下次重启才暴露").
		WithHint(
			"补上它在集群里的位置，端口由平台从 resources[].port 取：\n"+
				"    deploy:\n"+
				"      networkPolicy:\n"+
				"        egress:\n"+
				"          allowTo:\n"+
				"            - name: "+missing[0]+"\n"+
				"              resource: "+missing[0]+"\n"+
				"              namespace: infra          # 资源在集群内\n"+
				"              podSelector: {app: postgres}",
			"集群外的托管实例改写 cidr: 10.20.0.0/16",
			"不需要出站策略就去掉 deploy.networkPolicy.egress",
		)
}

// joinUnique 把组件 ID 去重后拼成一行。
func joinUnique(ids []string) string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)

	text := ""
	for i, id := range out {
		if i > 0 {
			text += "、"
		}
		text += id
	}
	return text
}
