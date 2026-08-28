package k8s

// 本文件渲染 Service 与 Ingress（005 §5.4、§5.5）。

import (
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// serviceDoc 渲染一个组件的 Service（005 §5.4）。
//
// Service 名就是版本化服务名：依赖方注入的 `http://people-basic-1-0-0:8080`
// 指的正是它，两处必须同源（002 §5.3）。
func (p *plan) serviceDoc(c componentPlan) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":        c.Service,
			"namespace":   p.namespace,
			"labels":      p.labelsOf(c),
			"annotations": p.annotationsOf(c),
		},
		"spec": map[string]any{
			"selector": map[string]any{labelApp: c.Service},
			"ports":    servicePorts(c.Manifest),
			// ClusterIP：默认不暴露到集群外，要对外只能显式 expose（005 §5.4）
			"type": "ClusterIP",
		},
	}
}

// servicePorts 渲染 ports：主端口 + extraPorts（附录 B.7）。
//
// 端口一律带 name：K8s 要求"一个 Service 里的端口要么都有名字、要么只有一个端口"，
// 加了 extraPorts 之后再补名字，会变成一次破坏性的改动。
func servicePorts(m *manifest.Manifest) []any {
	ports := []any{map[string]any{
		"name": mainPortName, "port": m.Deployment.Port, "targetPort": m.Deployment.Port,
	}}
	for _, extra := range m.Deployment.ExtraPorts {
		ports = append(ports, map[string]any{
			"name": extra.Name, "port": extra.Port, "targetPort": extra.Port,
		})
	}
	return ports
}

// ingressDoc 渲染 Ingress（005 §5.5）。只有 expose: true 的组件才有。
func (p *plan) ingressDoc(c componentPlan) map[string]any {
	// 集群侧的注解（cert-manager 签证书、nginx 调参数……）原样透传：
	// 平台不认识它们，也不该认识。平台自己的注解放在后面，不会被挤掉
	annotations := map[string]any{}
	for key, value := range p.cfg.Deploy.IngressAnnotations {
		annotations[key] = value
	}
	for key, value := range p.annotationsOf(c) {
		annotations[key] = value
	}

	spec := map[string]any{
		"rules": []any{map[string]any{
			"host": c.Entry.Hostname,
			"http": map[string]any{
				"paths": []any{map[string]any{
					"path":     "/",
					"pathType": "Prefix",
					"backend": map[string]any{
						"service": map[string]any{
							"name": c.Service,
							"port": map[string]any{"number": c.Manifest.Deployment.Port},
						},
					},
				}},
			},
		}},
	}

	// 不写 ingressClassName 时，只有集群配了"默认 class"才会有人认领这条
	// Ingress——没有默认 class 的集群上 apply 成功、域名却打不开
	if class := p.cfg.Deploy.IngressClass; class != "" {
		spec["ingressClassName"] = class
	}
	if secret := c.Entry.TLSSecret; secret != "" {
		spec["tls"] = []any{map[string]any{
			"hosts":      []any{c.Entry.Hostname},
			"secretName": secret,
		}}
	}

	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "Ingress",
		"metadata": map[string]any{
			"name":        c.Service,
			"namespace":   p.namespace,
			"labels":      p.labelsOf(c),
			"annotations": annotations,
		},
		"spec": spec,
	}
}

// checkHostnames 守住 Ingress 的两条：**每个对外的组件都要有域名，
// 而且一个域名只能归一个组件。**
//
// 两条都指向同一个失败：一条规则匹配上了它不该匹配的请求，而 `kubectl apply`
// 一句抱怨都没有。
func (p *plan) checkHostnames() error {
	if err := p.checkHostnamePresent(); err != nil {
		return err
	}
	return p.checkHostnameUnique()
}

// checkHostnamePresent 拦下 expose: true 却没写 hostname 的组件。
//
// K8s 下 hostname 是必填（003 §4.5）。生成一条没有 host 的 Ingress，等于把这个组件
// 挂到**所有**进入集群的域名上，谁先匹配上谁生效——一个内部组件可能就这样
// 顶掉了门户站点，而 kubectl apply 不会有任何抱怨。
func (p *plan) checkHostnamePresent() error {
	var missing []resolver.Ref
	for _, c := range p.components {
		if c.Entry.Expose && c.Entry.Hostname == "" {
			missing = append(missing, c.Ref)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	err := clierr.New(clierr.CodeConfigInvalid,
		"错误：deploy.target: k8s 下 expose: true 的组件必须写 hostname")
	for _, ref := range missing {
		err = err.WithDetail("组件", ref.ID+"@"+ref.Version)
	}
	return err.
		WithDetail("原因", "Ingress 靠域名路由，没有 host 的规则会匹配所有进入集群的域名").
		WithHint(
			"在 brickkit.yaml 里给这些组件补上 hostname: xxx.example.com",
			"组件之间在集群内互访不需要 expose，只有要对外开放才写",
		)
}

// checkHostnameUnique 拦下两个组件占同一个域名。
//
// # 为什么这是错的
//
// 平台生成的每条 Ingress 规则都是 `host: <hostname>` + `path: /`（005 §5.5）。
// 两个组件共用一个 hostname，就是两条一模一样的规则指向不同的后端——K8s 对此
// 没有定义行为（nginx-ingress 取创建时间最早的那份并记一条冲突日志），表现是
// 外面打进来的请求稳定落到其中一个上，而生成、apply、`kubectl get ingress`
// 全都看不出任何问题。
//
// # 为什么是报错而不是警告
//
// 与 Docker 侧对称：那边两个组件抢同一个宿主机端口同样是硬错误
// （compose.checkExposePorts），而且两条出路的形状完全一样——给其中一个换个值。
// 一个"生成成功、apply 成功、路由随机"的部署，比一次生成期的失败难查得多。
//
// # 多版本共存时几乎必然踩到
//
// 照 003 §8.3 加第二个版本时，整个组件条目是复制出来的，hostname 跟着一起复制。
// 而这里没有"两个版本轮流服务"这种解释可用——两份 Ingress 不是负载均衡。
//
// # 那想让一个域名下挂多个组件怎么办
//
// 平台不做 path 路由（`example.com/api` → A、`example.com/` → B）：那是一个新字段、
// 新语义，而 K8s 的常规做法本来就是一个组件一个子域名。真的需要按路径分流时，
// 那是集群侧的路由策略，自己写一份 Ingress——平台只负责"把这个组件按域名暴露出去"。
func (p *plan) checkHostnameUnique() error {
	// 按 hostname 归集，值是占用它的组件（按服务名排序，输出稳定）
	claimed := map[string][]resolver.Ref{}
	var hosts []string
	for _, c := range p.components {
		if !c.Entry.Expose || c.Entry.Hostname == "" {
			continue
		}
		host := c.Entry.Hostname
		if _, seen := claimed[host]; !seen {
			hosts = append(hosts, host)
		}
		claimed[host] = append(claimed[host], c.Ref)
	}
	sort.Strings(hosts)

	for _, host := range hosts {
		refs := claimed[host]
		if len(refs) < 2 {
			continue
		}
		err := clierr.Newf(clierr.CodeConfigInvalid,
			"错误：域名 %s 被多个组件占用", host)
		for _, ref := range refs {
			// 必须带版本号：多版本共存时两行组件 ID 一模一样
			err = err.WithDetail("组件", ref.ID+"@"+ref.Version)
		}
		return err.
			WithDetail("原因",
				"每个 expose 的组件都生成一条 host + path: / 的 Ingress 规则；"+
					"两条一模一样的规则指向不同后端，K8s 没有定义行为——"+
					"请求会稳定落到其中一个上，而 apply 不会有任何抱怨").
			WithHint(
				"在 brickkit.yaml 中给其中一个组件换一个 hostname（一个组件一个子域名）",
				"或去掉其中一个组件的 expose: true（组件之间在集群内互访不需要 expose）",
			)
	}
	return nil
}
