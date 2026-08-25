package k8s

// 本文件渲染 NetworkPolicy 与 ServiceAccount（P26），
// 以及依赖图之外的入站来源（P36）。
//
// 两者都是**opt-in** 的，理由与 podSecurity 一样（D246）：加上去可能让本来
// 跑得好好的东西不通，平台不替使用者做这个决定。
//
// NetworkPolicy 之所以值得由平台生成，只有一个理由：**依赖图在平台手里**。
// 手写策略难维护的根源就是那张图得靠人记——加依赖要记得开口子，
// 删依赖没人记得收回，久而久之策略就成了一份谁也不敢动的摆设。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// namespaceNameLabel 是 K8s 自动打在每个命名空间上的标签（1.21+）。
//
// NetworkPolicy 的 namespaceSelector 只认标签、不认名字，所以想表达
// "ingress-nginx 那个命名空间"就得走这个标签。
const namespaceNameLabel = "kubernetes.io/metadata.name"

// ============================================================
// NetworkPolicy
// ============================================================

// networkPolicyDoc 渲染一个组件的 NetworkPolicy。
//
// 形状固定为"默认拒绝入站 + 按依赖图逐条放行"：
//
//	podSelector   选中自己（用 Service 认后端的同一个 app 标签）
//	policyTypes   只有 Ingress
//	ingress       依赖方一条 + ingress controller 一条（对外暴露时）
//	              + allowFrom 每条一条（图外来源，P36）
//
// 空的 ingress 列表不是"没写完"，而是"谁也不许进"——一个 Pod 只要没被
// 任何策略选中就是全放行，所以没人依赖的组件也必须有这么一份。
func (p *plan) networkPolicyDoc(c componentPlan) map[string]any {
	rules := []any{}
	if from := p.dependentSources(c); len(from) > 0 {
		rules = append(rules, map[string]any{
			"from":  from,
			"ports": policyPorts(allPortsOf(c.Manifest)),
		})
	}
	if c.Entry.Expose {
		rules = append(rules, map[string]any{
			"from": []any{p.ingressControllerSource()},
			// Ingress 只会打到主端口，没必要把 extraPorts 也对外放开
			"ports": policyPorts([]int{c.Manifest.Deployment.Port}),
		})
	}
	rules = append(rules, p.allowFromRules(c)...)

	spec := map[string]any{
		"podSelector": map[string]any{
			"matchLabels": map[string]any{labelApp: c.Service},
		},
		"policyTypes": []any{"Ingress"},
		"ingress":     rules,
	}
	if p.cfg.Deploy.EgressEnabled() {
		spec["policyTypes"] = []any{"Ingress", "Egress"}
		spec["egress"] = p.egressRules(c)
	}

	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]any{
			"name":        c.Service,
			"namespace":   p.namespace,
			"labels":      p.labelsOf(c),
			"annotations": p.policyAnnotations(c),
		},
		"spec": spec,
	}
}

// dependentSources 是"谁可以连我"：依赖本组件的那些组件。
//
// 强依赖与弱依赖**都算**。弱依赖的语义是"有就用、没有就降级"（003 §4.3），
// 对方在的时候它是真的会去连的；漏在策略外面的表现极其迷惑——
// 组件装了、起来了、健康检查也过，只有那条"可选"链路永远超时。
//
// 只放行本次真的会跑起来的依赖方：没被选中或显式关掉的组件不在 p.components 里，
// 给它们留个口子没有意义。
func (p *plan) dependentSources(c componentPlan) []any {
	node := p.graph.Node(c.Ref)
	if node == nil {
		return nil
	}

	running := map[resolver.Ref]string{}
	for _, other := range p.components {
		running[other.Ref] = other.Service
	}

	services := make([]string, 0, len(node.Dependents))
	for _, dep := range node.Dependents {
		if service, ok := running[dep]; ok {
			services = append(services, service)
		}
	}
	// 排序保证同一份配置每次生成的文件逐字节一致
	sort.Strings(services)

	out := make([]any, 0, len(services))
	for _, service := range services {
		out = append(out, map[string]any{
			// 不带 namespaceSelector 就是"同命名空间内"，正是我们要的：
			// 一个项目的组件都在同一个命名空间里
			"podSelector": map[string]any{
				"matchLabels": map[string]any{labelApp: service},
			},
		})
	}
	return out
}

// ingressControllerSource 是 ingress controller 那条来源。
func (p *plan) ingressControllerSource() map[string]any {
	controller := p.cfg.Deploy.NetworkPolicy.IngressController
	return namespacedSource(controller.Namespace, controller.PodSelector)
}

// namespacedSource 渲染一条"某个命名空间里（符合标签的）Pod"的来源。
//
// ⚠️ 这里是整份策略里最容易写错的地方，所以只留这一个出口：
// namespaceSelector 与 podSelector 必须在**同一个 from 元素**里。
// 同一元素内是 AND；拆成两个元素就变成 OR——那个命名空间的**所有** Pod，
// 加上**所有**命名空间里符合标签的 Pod。写错了照样 apply 成功、照样通，
// 只是放行范围比你以为的大得多，而且没有任何迹象。
//
// podSelector 为空时只按命名空间放行：各家组件的标签五花八门，
// 不该逼使用者非得写对；命名空间这一级已经收得够紧了。
func namespacedSource(namespace string, podSelector map[string]string) map[string]any {
	source := map[string]any{
		"namespaceSelector": map[string]any{
			"matchLabels": map[string]any{namespaceNameLabel: namespace},
		},
	}
	if len(podSelector) > 0 {
		labels := map[string]any{}
		for key, value := range podSelector {
			labels[key] = value
		}
		source["podSelector"] = map[string]any{"matchLabels": labels}
	}
	return source
}

// annotationAllowFrom 记下额外放行了谁（P36）。
//
// 半年后有人 `kubectl get networkpolicy -o yaml`，看到一条放行某个命名空间的
// 规则，得能立刻知道它是干什么的——否则它只能在"不敢删"里一直躺着。
const annotationAllowFrom = "brickkit.io/allow-from"

// allowFromRules 渲染依赖图之外的入站来源（P36）。
//
// 为什么需要它：生成的规则只放行依赖图里的组件，而监控、备份、服务网格
// 这些都不在那张图上。最典型的是 Prometheus 抓 /metrics——挡掉之后
// **指标悄悄停了**，服务本身完全正常、没有任何报错，是最难查的一类故障。
//
// 每条来源都加到**每一个**组件上：监控要抓的是全部组件，
// 漏掉任何一个的表现都是"那个组件的指标没了"，而它本身好好的。
func (p *plan) allowFromRules(c componentPlan) []any {
	if !p.cfg.Deploy.NetworkPolicyEnabled() {
		return nil
	}

	sources := p.cfg.Deploy.NetworkPolicy.AllowFrom
	out := make([]any, 0, len(sources))
	for _, source := range sources {
		ports := source.Ports
		if len(ports) == 0 {
			// 使用者多半不知道每个组件的端口是几——那本来就是组件自己声明的
			ports = allPortsOf(c.Manifest)
		}
		out = append(out, map[string]any{
			"from":  []any{namespacedSource(source.Namespace, source.PodSelector)},
			"ports": policyPorts(ports),
		})
	}
	return out
}

// policyAnnotations 是 NetworkPolicy 的注解：组件 ID + 额外放行了谁。
func (p *plan) policyAnnotations(c componentPlan) map[string]any {
	annotations := p.annotationsOf(c)
	if !p.cfg.Deploy.NetworkPolicyEnabled() {
		return annotations
	}

	names := make([]string, 0, len(p.cfg.Deploy.NetworkPolicy.AllowFrom))
	for _, source := range p.cfg.Deploy.NetworkPolicy.AllowFrom {
		names = append(names, source.Name)
	}
	if len(names) > 0 {
		annotations[annotationAllowFrom] = strings.Join(names, ",")
	}
	return annotations
}

// allPortsOf 是组件声明过的全部端口：主端口 + extraPorts。
func allPortsOf(m *manifest.Manifest) []int {
	ports := []int{m.Deployment.Port}
	for _, extra := range m.Deployment.ExtraPorts {
		ports = append(ports, extra.Port)
	}
	return ports
}

// policyPorts 渲染 ingress 规则里的 ports。
//
// protocol 显式写 TCP：不写时 K8s 默认也是 TCP，但这份文件是给人读的，
// 而"这条策略到底管不管 UDP"是读的人第一个会问的问题。
func policyPorts(ports []int) []any {
	out := make([]any, 0, len(ports))
	for _, port := range ports {
		out = append(out, map[string]any{"protocol": "TCP", "port": port})
	}
	return out
}

// checkIngressController 拦下"要生成策略、又有对外组件、却没说 controller 在哪"。
//
// 不拦的话生成的策略会把 ingress controller 一起挡在门外，结果是
// **部署全部成功、网站直接打不开**，现象是超时或 504——
// 一眼看去像组件本身的问题，最不容易联想到是刚打开的这个开关。
func (p *plan) checkIngressController() error {
	if !p.cfg.Deploy.NetworkPolicyEnabled() {
		return nil
	}
	if c := p.cfg.Deploy.NetworkPolicy.IngressController; c != nil && c.Namespace != "" {
		return nil
	}

	var exposed []resolver.Ref
	for _, c := range p.components {
		if c.Entry.Expose {
			exposed = append(exposed, c.Ref)
		}
	}
	if len(exposed) == 0 {
		return nil
	}

	err := clierr.New(clierr.CodeConfigInvalid,
		"错误：开了 deploy.networkPolicy 又有 expose: true 的组件时，"+
			"必须写 deploy.networkPolicy.ingressController.namespace")
	for _, ref := range exposed {
		err = err.WithDetail("对外组件", ref.ID+"@"+ref.Version+"（expose: true）")
	}
	return err.
		WithDetail("原因", "生成的策略默认拒绝一切入站；不说明 ingress controller 在哪，"+
			"它也会被挡在门外——部署会全部成功，网站却直接打不开").
		WithHint(
			"查一下 controller 在哪个命名空间：kubectl get pods -A | grep ingress",
			"然后写进 brickkit.yaml：\n"+
				"    deploy:\n"+
				"      networkPolicy:\n"+
				"        enabled: true\n"+
				"        ingressController:\n"+
				"          namespace: ingress-nginx",
			"不需要网络策略就去掉 deploy.networkPolicy",
		)
}

// ============================================================
// ServiceAccount
// ============================================================

// serviceAccountDoc 渲染一个组件专属的 ServiceAccount。
//
// automountServiceAccountToken: false 是这件事的全部意义。默认情况下每个 Pod
// 都会被塞进一张 default SA 的令牌（/var/run/secrets/kubernetes.io/serviceaccount/token），
// 拿着它就能跟 API Server 说话——而 003 的组件模型里根本没有"访问 K8s API"这回事。
// 关掉它是纯收益：任何一个组件被拿下，攻击者也拿不到一张能问集群要东西的票。
func (p *plan) serviceAccountDoc(c componentPlan) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":        c.Service,
			"namespace":   p.namespace,
			"labels":      p.labelsOf(c),
			"annotations": p.annotationsOf(c),
		},
		"automountServiceAccountToken": false,
	}
}

// generatesServiceAccount 表示这个组件的 SA 由平台生成。
//
// 写了 serviceAccountName 就是用运维建好的那个：只引用，不生成。
func (p *plan) generatesServiceAccount(c componentPlan) bool {
	return p.cfg.Deploy.ServiceAccountEnabled() && c.Entry.ServiceAccountName == ""
}

// defaultServiceAccount 是每个命名空间自带的那个 SA。
//
// 不写 serviceAccountName 时 K8s 就是用它，所以显式写出来在语义上是个空操作——
// 但它**必须**被写出来，理由见 applyServiceAccount。
const defaultServiceAccount = "default"

// serviceAccountNameOf 返回该组件的 Pod 该用哪个 SA。
func (p *plan) serviceAccountNameOf(c componentPlan) string {
	if c.Entry.ServiceAccountName != "" {
		return c.Entry.ServiceAccountName
	}
	if p.cfg.Deploy.ServiceAccountEnabled() {
		return c.Service
	}
	return defaultServiceAccount
}

// applyServiceAccount 把 SA 相关字段写进 Pod 规格。
//
// Pod 上再写一次 automountServiceAccountToken 不是冗余：Pod 级别会覆盖 SA 级别，
// 写上它，就算以后有人手工把 SA 上那个开关打开，这些 Pod 也还是不挂载。
//
// 用**别人的** SA 时刻意不写这一行——那个 SA 可能正是靠令牌去调 API 的，
// 平台无权替它决定。
//
// # 为什么关掉开关时也要写出 serviceAccountName: default
//
// **省略一个字段不等于把它取消掉。** `kubectl apply` 的三方合并本该按
// last-applied 的差集把它删掉，但 `spec.serviceAccount` 是
// `spec.serviceAccountName` 的**废弃别名**：置空后 API Server 又从别名字段
// 同步了回来。minikube 上实测——生成物里没有这个字段、last-applied 里也没有，
// 而活着的 Deployment 里 `serviceAccountName` 与 `serviceAccount` 双双还是旧值。
//
// 从前这不会造成故障：SA 永远不被清理，那个陈旧的引用一直指着一个存在的对象。
// 孤儿清理开始清 SA 之后（§5.9.1.1），后果变成**部署直接失败**：
//
//	pods "..." is forbidden: error looking up service account ...:
//	serviceaccount "demo-portal-1-0-0" not found
//
// ReplicaFailure / FailedCreate，rollout 一路超时到 5 分钟上限，
// 而使用者做的只是把 `deploy.serviceAccount` 关掉。
//
// 顺带一提 `automountServiceAccountToken` **没有**这个问题（它没有别名字段，
// 同一次实测里被正确地删掉了）——所以这不是"apply 不会删字段"的通例，
// 而是这一个字段特有的坑。
func (p *plan) applyServiceAccount(spec map[string]any, c componentPlan) {
	spec["serviceAccountName"] = p.serviceAccountNameOf(c)
	if p.generatesServiceAccount(c) {
		spec["automountServiceAccountToken"] = false
	}
}
