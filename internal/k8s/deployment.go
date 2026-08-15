package k8s

// 本文件渲染 Deployment（005 §5.3）：镜像、环境变量、探针、资源配额。

import (
	"strings"

	"github.com/brickkit/brickkit/internal/config"

	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 平台标签与注解（005 §5.3）。
const (
	labelApp              = "app"
	labelComponent        = "brickkit.io/component"
	labelComponentVersion = "brickkit.io/component-version"
	labelProject          = "brickkit.io/project"
	// labelRole 区分同一个组件的不同角色（目前只有迁移 Job）。
	labelRole     = "brickkit.io/role"
	roleMigration = "migration"

	// annotationComponentID 保存**原样**的组件 ID。
	//
	// 它不能当标签：K8s 的标签**值**只允许字母数字与 - _ .，而组件 ID 是
	// `scope/name` 带斜杠的（斜杠只在标签**键**的前缀里合法）。
	// 设计书 005 §5.3 原来的样例 `brickkit.io/component-id: people/basic`
	// 会被 API Server 整份拒绝，连 Deployment 都建不出来。
	annotationComponentID = "brickkit.io/component-id"
)

// 探针参数（005 §5.3）。
//
// 就绪探针比存活探针更早、更密：它决定"能不能开始收流量"，
// 而存活探针决定"要不要杀掉重启"——后者错杀的代价大得多，所以给得更宽。
const (
	livenessInitialDelay  = 10
	livenessPeriod        = 10
	readinessInitialDelay = 5
	readinessPeriod       = 5
	probeTimeout          = 3
	probeFailureThreshold = 3
)

// mainPortName 是主端口在 Service / containerPort 里的名字。
const mainPortName = "http"

// labelsOf 是一个组件在所有清单里共用的标签。
//
// brickkit.io/component 放的是组件 ID 的**服务名写法**（people/basic → people-basic）：
// 标签值不允许出现斜杠，原样的 ID 放在注解里（见 annotationComponentID）。
func (p *plan) labelsOf(c componentPlan) map[string]any {
	return map[string]any{
		labelApp:              c.Service,
		labelComponent:        containerName(c.Ref.ID),
		labelComponentVersion: c.Ref.Version,
		labelProject:          p.cfg.Project,
	}
}

// annotationsOf 是一个组件在所有清单里共用的注解。
func (p *plan) annotationsOf(c componentPlan) map[string]any {
	return map[string]any{annotationComponentID: c.Ref.ID}
}

// deploymentDoc 渲染一个组件的 Deployment。
func (p *plan) deploymentDoc(c componentPlan) map[string]any {
	labels := p.labelsOf(c)

	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":        c.Service,
			"namespace":   p.namespace,
			"labels":      labels,
			"annotations": p.annotationsOf(c),
		},
		"spec": map[string]any{
			// 多实例与 HPA 是后期能力（005 §5.8），先固定 1 个副本
			"replicas": 1,
			// selector 只认 app：它是 K8s 里 Deployment 找到自己 Pod 的唯一依据，
			// 多写一个会变的标签（比如版本）就会在升级时选空
			"selector": map[string]any{"matchLabels": map[string]any{labelApp: c.Service}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec":     p.podSpec(p.containerDoc(c)),
			},
		},
	}
}

// podSpec 渲染 Pod 规格：容器 + 集群侧要求（005 §5.12）。
func (p *plan) podSpec(container map[string]any) map[string]any {
	spec := map[string]any{"containers": []any{container}}

	if secrets := p.cfg.Deploy.ImagePullSecrets; len(secrets) > 0 {
		refs := make([]any, 0, len(secrets))
		for _, name := range secrets {
			refs = append(refs, map[string]any{"name": name})
		}
		spec["imagePullSecrets"] = refs
	}
	return spec
}

// securityContext 按 Pod Security Standards 的 restricted 级别生成（005 §14.3）。
//
// 不写 deploy.podSecurity 时**什么都不生成**：加上它可能让本来跑得好好的组件
// 起不来（镜像以 root 运行、要绑 1024 以下的端口……），不能默默替使用者决定。
//
// 刻意**不**生成 readOnlyRootFilesystem：restricted 并不要求它，
// 而它会让任何往 /tmp 写东西的组件直接挂掉。
func (p *plan) securityContext() map[string]any {
	if p.cfg.Deploy.PodSecurity != config.PodSecurityRestricted {
		return nil
	}
	return map[string]any{
		"allowPrivilegeEscalation": false,
		"runAsNonRoot":             true,
		"capabilities":             map[string]any{"drop": []any{"ALL"}},
		"seccompProfile":           map[string]any{"type": "RuntimeDefault"},
	}
}

// containerDoc 渲染 Pod 里的那个容器。
func (p *plan) containerDoc(c componentPlan) map[string]any {
	container := map[string]any{
		// 容器名用不带版本的组件 ID：kubectl logs / exec 要手敲它，
		// Pod 名本身已经带了版本，这里再带一遍只是更难打
		"name":  containerName(c.Ref.ID),
		"image": c.Manifest.Deployment.Image,
		"ports": containerPorts(c.Manifest),
	}

	if env := p.envDoc(c); len(env) > 0 {
		container["env"] = env
	}
	if probe := livenessProbe(c.Manifest); probe != nil {
		container["livenessProbe"] = probe
		container["readinessProbe"] = readinessProbe(c.Manifest)
	}
	if resources := resourcesDoc(c.Env); len(resources) > 0 {
		container["resources"] = resources
	}
	if sc := p.securityContext(); sc != nil {
		container["securityContext"] = sc
	}
	return container
}

// containerName 是容器名：组件 ID 去掉 scope 分隔符。
func containerName(id string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(strings.ToLower(id))
}

// containerPorts 渲染 containerPort 列表：主端口 + extraPorts（附录 B.7）。
func containerPorts(m *manifest.Manifest) []any {
	ports := []any{map[string]any{"name": mainPortName, "containerPort": m.Deployment.Port}}
	for _, extra := range m.Deployment.ExtraPorts {
		ports = append(ports, map[string]any{"name": extra.Name, "containerPort": extra.Port})
	}
	return ports
}

// envDoc 渲染 env 数组。
//
// 敏感变量走 secretKeyRef，其余明文；顺序与 inject 一致（按变量名排序），
// 这样两次生成的文件可以逐字节比对。
func (p *plan) envDoc(c componentPlan) []any {
	out := make([]any, 0, len(c.Env.Env))
	for _, v := range c.Env.Env {
		if v.IsSecret() {
			out = append(out, map[string]any{
				"name": v.Name,
				"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{
						"name": secretName(v.ResourceID),
						"key":  v.SecretKey,
					},
				},
			})
			continue
		}
		// value 必须是字符串：K8s 的 env.value 是 string 类型，
		// 写成数字会被 API Server 直接拒绝
		out = append(out, map[string]any{"name": v.Name, "value": p.expand.value(v.Value)})
	}
	return out
}

// livenessProbe 渲染存活探针（005 §5.3）。
//
// ⚠️ 探的是主端口上的 /healthz，而 /healthz 只应检查本进程存活（002 §9.4）：
// 存活探针失败会让 K8s 直接杀掉 Pod，在里面检查数据库等于让下游故障
// 变成自己的重启风暴。
func livenessProbe(m *manifest.Manifest) map[string]any {
	action := probeAction(m)
	if action == nil {
		return nil
	}
	probe := map[string]any{
		"initialDelaySeconds": livenessInitialDelay,
		"periodSeconds":       livenessPeriod,
		"timeoutSeconds":      probeTimeout,
		"failureThreshold":    probeFailureThreshold,
	}
	for k, v := range action {
		probe[k] = v
	}
	return probe
}

// readinessProbe 渲染就绪探针（005 §5.3）。
func readinessProbe(m *manifest.Manifest) map[string]any {
	action := probeAction(m)
	if action == nil {
		return nil
	}
	probe := map[string]any{
		"initialDelaySeconds": readinessInitialDelay,
		"periodSeconds":       readinessPeriod,
		"timeoutSeconds":      probeTimeout,
		"failureThreshold":    probeFailureThreshold,
	}
	for k, v := range action {
		probe[k] = v
	}
	return probe
}

// probeAction 把 Manifest 的健康检查转成 K8s 的探针动作。
//
// 与 compose 那边的一处根本差别：K8s 的 httpGet 由 kubelet 从**容器外**发起，
// 不要求镜像里有 wget / curl（compose 的 healthcheck 跑在容器内部，
// 因此那边必须凑出一条镜像里真有的命令，见 002 §9.6）。
func probeAction(m *manifest.Manifest) map[string]any {
	if m == nil {
		return nil
	}
	switch m.HealthCheck.Type {
	case manifest.HealthCheckHTTP:
		return map[string]any{"httpGet": map[string]any{
			"path": m.HealthCheck.Path,
			"port": m.Deployment.Port,
		}}
	case manifest.HealthCheckTCP:
		return map[string]any{"tcpSocket": map[string]any{"port": m.Deployment.Port}}
	default:
		// none：不生成探针。探不通的探针会让 K8s 反复杀掉一个其实健康的 Pod
		return nil
	}
}

// resourcesDoc 渲染资源配额（005 §5.3）。
//
// 直接用 Manifest 里的写法（100m / 128Mi）——那本来就是 K8s 的写法，
// 不需要 compose 那边的换算。
func resourcesDoc(c inject.Component) map[string]any {
	out := map[string]any{}
	if spec := quotaDoc(c.Resources.Requests); len(spec) > 0 {
		out["requests"] = spec
	}
	if spec := quotaDoc(c.Resources.Limits); len(spec) > 0 {
		out["limits"] = spec
	}
	return out
}

func quotaDoc(spec *manifest.ResourceSpec) map[string]any {
	if spec == nil {
		return nil
	}
	out := map[string]any{}
	if spec.CPU != "" {
		out["cpu"] = spec.CPU
	}
	if spec.Memory != "" {
		out["memory"] = spec.Memory
	}
	return out
}
