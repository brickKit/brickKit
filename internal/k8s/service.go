package k8s

// 本文件渲染 Service 与 Ingress（005 §5.4、§5.5）。

import (
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

// checkHostnames 拦下 expose: true 却没写 hostname 的组件。
//
// K8s 下 hostname 是必填（003 §4.5）。生成一条没有 host 的 Ingress，等于把这个组件
// 挂到**所有**进入集群的域名上，谁先匹配上谁生效——一个内部组件可能就这样
// 顶掉了门户站点，而 kubectl apply 不会有任何抱怨。
func (p *plan) checkHostnames() error {
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
