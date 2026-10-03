package k8s

// 本文件渲染 Service 与 Ingress。

import (
	"slices"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
)

// serviceDoc 渲染一个组件的 Service。
//
// Service 名就是版本化服务名：依赖方注入的 `http://people-basic-1-0-0:8080`
// 指的正是它，两处必须同源。
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
			"ports":    p.servicePorts(c.Manifest),
			// ClusterIP：默认不暴露到集群外，要对外只能显式 expose
			"type": "ClusterIP",
		},
	}
}

// servicePorts 渲染 ports：主端口 + extraPorts。
//
// 端口一律带 name：K8s 要求"一个 Service 里的端口要么都有名字、要么只有一个端口"，
// 加了 extraPorts 之后再补名字，会变成一次破坏性的改动。
//
// 组件声明了端口协议的，写成 appProtocol：ClusterIP 是四层负载均衡，一条 gRPC 长连接会钉在一个 Pod 上，
// 网格或网关要知道"这个端口是 gRPC"才会按请求分发。写哪个词由部署文件定（File.AppProtocol）。
// 主端口的名字仍是 http——改名是破坏性的，而认 appProtocol 的实现都让它优先于端口名。
func (p *plan) servicePorts(m *manifest.Manifest) []any {
	port := func(name string, number int, protocol string) map[string]any {
		doc := map[string]any{"name": name, "port": number, "targetPort": number}
		if app := p.proj.Deploy.AppProtocol(protocol); app != "" {
			doc["appProtocol"] = app
		}
		return doc
	}
	ports := []any{port(mainPortName, m.Deployment.Port, m.Deployment.Protocol)}
	for _, extra := range m.Deployment.ExtraPorts {
		ports = append(ports, port(extra.Name, extra.Port, extra.Protocol))
	}
	return ports
}

// ingressDoc 渲染 Ingress。只有 expose: true 的组件才有。
func (p *plan) ingressDoc(e exposedEntry) map[string]any {
	// 集群侧的注解（cert-manager 签证书、nginx 调参数……）原样透传：
	// 平台不认识它们，也不该认识。平台自己的注解放在后面，不会被挤掉
	annotations := map[string]any{}
	for key, value := range p.proj.Deploy.Settings().IngressAnnotations {
		annotations[key] = value
	}
	annotations[annotationComponentID] = e.Ref.ID

	// 没写 paths 是整个域名；写了就只接这些前缀——共用域名的几个组件靠它分流（见 checkRouteUnique）
	var paths []any
	for _, path := range e.Entry.RoutePaths() {
		paths = append(paths, map[string]any{
			"path":     path,
			"pathType": "Prefix",
			"backend": map[string]any{
				"service": map[string]any{
					"name": e.Service,
					"port": map[string]any{"number": e.Port},
				},
			},
		})
	}

	spec := map[string]any{
		"rules": []any{map[string]any{
			"host": e.Entry.Hostname,
			"http": map[string]any{
				"paths": paths,
			},
		}},
	}

	// 不写 ingressClassName 时，只有集群配了"默认 class"才会有人认领这条
	// Ingress——没有默认 class 的集群上 apply 成功、域名却打不开
	if class := p.proj.Deploy.Settings().IngressClass; class != "" {
		spec["ingressClassName"] = class
	}
	if secret := e.Entry.TLSSecret; secret != "" {
		spec["tls"] = []any{map[string]any{
			"hosts":      []any{e.Entry.Hostname},
			"secretName": secret,
		}}
	}

	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "Ingress",
		"metadata": map[string]any{
			"name":      e.Name,
			"namespace": p.namespace,
			"labels": map[string]any{
				labelApp:              e.Service,
				labelComponent:        containerName(e.Ref.ID),
				labelComponentVersion: e.Ref.Version,
				labelProject:          p.proj.Decl.Project,
			},
			"annotations": annotations,
		},
		"spec": spec,
	}
}

// checkHostnames 守住 Ingress 的三条：**每个对外的组件都要有域名；同一个域名下，一条路径只能归一个组件；
// 同一个域名只有一张证书。**
//
// 三条都指向同一类失败：一条规则匹配上了它不该匹配的请求（或用了不该用的证书），而 `kubectl apply`
// 一句抱怨都没有。
func (p *plan) checkHostnames() error {
	if err := p.checkHostnamePresent(); err != nil {
		return err
	}
	if err := p.checkRouteUnique(); err != nil {
		return err
	}
	if err := p.checkIngressNameUnique(); err != nil {
		return err
	}
	return p.checkTLSSecretPerHost()
}

// checkIngressNameUnique 拦下两个对外的组件版本算出同一个 Ingress 名字。
//
// 默认版本的名字是组件 ID（demo-web），兼容版本的是版本化服务名（demo-web-1-0-0）；只有 ID 本身以 -数字-数字-数字
// 结尾（demo/web-1-0-0）才会撞上别的组件的版本化服务名。几乎不会发生，但发生时是一份 Ingress 悄悄盖掉另一份。
func (p *plan) checkIngressNameUnique() error {
	seen := map[string]resolver.Ref{}
	for _, e := range p.exposed() {
		if prev, dup := seen[e.Name]; dup {
			return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.K8sIngressNameConflict, e.Name)).
				WithDetail(i18n.T(msgid.LabelComponent), prev.String()).
				WithDetail(i18n.T(msgid.LabelComponent), e.Ref.String()).
				WithHint(i18n.T(msgid.K8sHintDropExpose))
		}
		seen[e.Name] = e.Ref
	}
	return nil
}

// checkHostnamePresent 拦下 expose: true 却没写 hostname 的组件。
//
// K8s 下 hostname 是必填。生成一条没有 host 的 Ingress，等于把这个组件
// 挂到**所有**进入集群的域名上，谁先匹配上谁生效——一个内部组件可能就这样
// 顶掉了门户站点，而 kubectl apply 不会有任何抱怨。
func (p *plan) checkHostnamePresent() error {
	var missing []resolver.Ref
	for _, e := range p.exposed() {
		if e.Entry.Hostname == "" {
			missing = append(missing, e.Ref)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.K8sHostnameMissing))
	for _, ref := range missing {
		err = err.WithDetail(i18n.T(msgid.LabelComponent), ref.ID+"@"+ref.Version)
	}
	return err.
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.K8sHostnameMissingReasonDetail)).
		WithHint(
			i18n.T(msgid.K8sHintAddHostname),
			i18n.T(msgid.K8sHintExposeOnlyForExternal),
		)
}

// checkRouteUnique 拦下两个组件占同一个域名下的同一条路径。
//
// # 为什么这是错的
//
// 每个对外的组件生成一份 Ingress，规则是 `host: <hostname>` + 它的每条路径（没写 paths 就是 `/`）。
// 两个组件在同一个域名下写了同一条路径，就是两条一模一样的规则指向不同的后端——K8s 对此
// 没有定义行为（nginx-ingress 取创建时间最早的那份并记一条冲突日志），表现是
// 外面打进来的请求稳定落到其中一个上，而生成、apply、`kubectl get ingress` 全都看不出任何问题。
//
// # 为什么是报错而不是警告
//
// 与 Docker 侧对称：那边两个组件抢同一个宿主机端口同样是硬错误
// （compose.checkExposePorts），而且两条出路的形状完全一样——给其中一个换个值。
// 一个"生成成功、apply 成功、路由随机"的部署，比一次生成期的失败难查得多。
//
// # 多版本共存时几乎必然踩到
//
// 加第二个共存版本时，整个组件条目是复制出来的，hostname（和 paths）跟着一起复制。
// 而这里没有"两个版本轮流服务"这种解释可用——两份 Ingress 不是负载均衡。
//
// # 一个域名下挂多个组件
//
// 共用 hostname、各写各的 paths：路径不同的规则不冲突，长的前缀先匹配，所以 `/`（门户）与 `/api/sales`（后端）
// 可以并存。每个组件仍是自己的一份 Ingress——同一个 host 的规则由 Ingress 控制器合并，这是 Ingress 的常规行为
// （nginx-ingress、Traefik、HAProxy 都如此）；"每份 Ingress 各建一个负载均衡器"的控制器（GKE 自带的那种）做不到。
func (p *plan) checkRouteUnique() error {
	type route struct{ host, path string }
	claimed := map[route][]resolver.Ref{}
	var routes []route
	for _, e := range p.exposed() {
		if e.Entry.Hostname == "" {
			continue
		}
		for _, path := range e.Entry.RoutePaths() {
			r := route{e.Entry.Hostname, deployfile.NormalizeRoutePath(path)}
			if _, seen := claimed[r]; !seen {
				routes = append(routes, r)
			}
			claimed[r] = append(claimed[r], e.Ref)
		}
	}
	// 按域名、再按路径排：报错稳定
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].host != routes[j].host {
			return routes[i].host < routes[j].host
		}
		return routes[i].path < routes[j].path
	})

	for _, r := range routes {
		refs := claimed[r]
		if len(refs) < 2 {
			continue
		}
		err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.K8sHostnameConflict, r.host, r.path))
		for _, ref := range refs {
			// 必须带版本号：多版本共存时两行组件 ID 一模一样
			err = err.WithDetail(i18n.T(msgid.LabelComponent), ref.ID+"@"+ref.Version)
		}
		return err.
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.K8sHostnameConflictReasonDetail)).
			WithHint(
				i18n.T(msgid.K8sHintChangeHostname),
				i18n.T(msgid.K8sHintSplitByPaths),
				i18n.T(msgid.K8sHintDropExpose),
			)
	}
	return nil
}

// checkTLSSecretPerHost 拦下同一个域名下的条目写了不同的 tlsSecret（包括有的写了、有的没写）。
//
// 一个域名只有一张证书。几份 Ingress 给同一个 host 各说各的证书时，用哪张由控制器决定（通常是最早创建的那份），
// 没写证书的那份在有些控制器上还会让它的路径不跳转 HTTPS——换一个组件的部署顺序，证书就换了。
// 所以共用域名的条目要写同一个 tlsSecret，每一条都写。
func (p *plan) checkTLSSecretPerHost() error {
	byHost := map[string][]exposedEntry{}
	var hosts []string
	for _, e := range p.exposed() {
		host := e.Entry.Hostname
		if host == "" {
			continue
		}
		if _, seen := byHost[host]; !seen {
			hosts = append(hosts, host)
		}
		byHost[host] = append(byHost[host], e)
	}
	sort.Strings(hosts)

	for _, host := range hosts {
		entries := byHost[host]
		if !slices.ContainsFunc(entries, func(e exposedEntry) bool { return e.Entry.TLSSecret != entries[0].Entry.TLSSecret }) {
			continue
		}
		err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.K8sTLSSecretConflict, host))
		for _, e := range entries {
			secret := e.Entry.TLSSecret
			if secret == "" {
				secret = i18n.T(msgid.K8sTLSSecretNone)
			}
			err = err.WithDetail(e.Ref.ID+"@"+e.Ref.Version, secret)
		}
		return err.WithHint(i18n.T(msgid.K8sHintSameTLSSecret))
	}
	return nil
}
