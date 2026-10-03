package k8s

// 本文件渲染 Deployment：镜像、环境变量、探针、资源配额。

import (
	"strings"

	"github.com/brickkit/brickkit/internal/deployfile"

	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 平台标签与注解。
const (
	labelApp              = "app"
	labelComponent        = "brickkit.io/component"
	labelComponentVersion = "brickkit.io/component-version"
	labelProject          = LabelProject
	// labelRole 区分同一个组件的不同角色（目前只有迁移 Job）。
	labelRole     = "brickkit.io/role"
	roleMigration = "migration"

	// annotationComponentID 保存**原样**的组件 ID。
	//
	// 它不能当标签：K8s 的标签**值**只允许字母数字与 - _ .，而组件 ID 是
	// `scope/name` 带斜杠的（斜杠只在标签**键**的前缀里合法）。
	// 早先设计里的样例 `brickkit.io/component-id: people/basic`
	// 会被 API Server 整份拒绝，连 Deployment 都建不出来。
	annotationComponentID = "brickkit.io/component-id"
	// annotationSecretDigest 是 Pod 模板上的密钥摘要（见 secretDigest）。
	annotationSecretDigest = "brickkit.io/secret-digest"
)

// 探针参数。
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
	// startupPeriod 是启动探针的检查周期。
	//
	// 比存活/就绪都密：它决定"多快发现组件已经起来了"。5 秒的粒度让
	// 快速启动的组件几乎不受影响（第一次探测就在 t=5s，与原来的就绪探针同时），
	// 而慢启动的组件靠 failureThreshold 把总预算拉长。
	startupPeriod = 5
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
		labelProject:          p.proj.Decl.Project,
	}
}

// annotationsOf 是一个组件在所有清单里共用的注解。
func (p *plan) annotationsOf(c componentPlan) map[string]any {
	return map[string]any{annotationComponentID: c.Ref.ID}
}

// passthroughAnnotationsOf 是平台注解加上使用者的透传 labels。
//
// # 为什么 K8s 下落在 annotations 而不是 labels
//
// 两条理由，各自都足够：
//
//	打架  平台的 `app: <版本化服务名>` 是 Deployment 找到自己 Pod 的唯一依据，
//	      也是 NetworkPolicy 的匹配依据。使用者的键透传进
//	      labels，一旦撞上就是选择器选空或策略放行错对象——两者都不报错
//	限制  labels 的**值**只许 [A-Za-z0-9._-]，而要透传的键值里全是斜杠、
//	      反引号与括号（`PathPrefix(`+"`"+`/erp/sales`+"`"+`)`）。写进 labels 会被
//	      API Server 整份拒绝；annotations 没有这个约束
//
// 而 K8s 生态里真正需要透传的那些（`prometheus.io/*`、各类 CRD 提示）
// 本来就是 annotation。一个目标，一条规则。
//
// # 为什么只落在 Deployment 与 Pod
//
// Service / Ingress / PDB / NetworkPolicy 都不加：Ingress 的注解口已经是
// `deploy.ingressAnnotations`，两个口子写同一处会互相覆盖而且没人
// 说得清谁赢。Pod 必须加，`prometheus.io/scrape` 这一类抓的是 Pod。
// 迁移 Job 不加，理由与 Docker 侧相同（见 compose.componentService）。
func (p *plan) passthroughAnnotationsOf(c componentPlan) map[string]any {
	out := p.annotationsOf(c)
	for key, value := range c.Env.Labels {
		out[key] = value
	}
	return out
}

// deploymentDoc 渲染一个组件的 Deployment。
func (p *plan) deploymentDoc(c componentPlan) map[string]any {
	labels := p.labelsOf(c)
	annotations := p.passthroughAnnotationsOf(c)
	// 只加在 Pod 模板上：要的是"密钥变了就滚动更新"，而触发滚动更新的只有模板
	podAnnotations := p.passthroughAnnotationsOf(c)
	if digest := p.secretDigest(c); digest != "" {
		podAnnotations[annotationSecretDigest] = digest
	}

	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":        c.Service,
			"namespace":   p.namespace,
			"labels":      labels,
			"annotations": annotations,
		},
		"spec": map[string]any{
			// 副本数由 brickkit.yaml 的 replicas 决定，不写就是 1。
			// HPA 自动扩容仍是后期能力
			"replicas": c.Entry.ReplicaCount(),
			// selector 只认 app：它是 K8s 里 Deployment 找到自己 Pod 的唯一依据，
			// 多写一个会变的标签（比如版本）就会在升级时选空
			"selector": map[string]any{"matchLabels": map[string]any{labelApp: c.Service}},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": labels,
					// Pod 也要带：抓取类注解（prometheus.io/*）读的是 Pod，
					// 只写在 Deployment 上等于没写
					"annotations": podAnnotations,
				},
				"spec": p.podSpec(c, p.containerDoc(c)),
			},
		},
	}
}

// podSpec 渲染 Pod 规格：容器 + 集群侧要求。
func (p *plan) podSpec(c componentPlan, container map[string]any) map[string]any {
	spec := map[string]any{"containers": []any{container}}
	// 以文件交付的配置项：卷在 Pod 上，挂载点在容器上。主容器与迁移容器都从这里过，所以两边一样
	if volumes, mounts := p.secretVolumes(c); len(volumes) > 0 {
		container["volumeMounts"] = mounts
		spec["volumes"] = volumes
	}
	if seconds := c.Env.StopGracePeriodSeconds; seconds > 0 {
		spec["terminationGracePeriodSeconds"] = seconds
	}

	p.applyServiceAccount(spec, c)

	if secrets := p.proj.Deploy.Settings().ImagePullSecrets; len(secrets) > 0 {
		refs := make([]any, 0, len(secrets))
		for _, name := range secrets {
			refs = append(refs, map[string]any{"name": name})
		}
		spec["imagePullSecrets"] = refs
	}
	return spec
}

// securityContext 按 Pod Security Standards 的 restricted 级别生成。
//
// 不写 deploy.podSecurity 时**什么都不生成**：加上它可能让本来跑得好好的组件
// 起不来（镜像以 root 运行、要绑 1024 以下的端口……），不能默默替使用者决定。
//
// 刻意**不**生成 readOnlyRootFilesystem：restricted 并不要求它，
// 而它会让任何往 /tmp 写东西的组件直接挂掉。
func (p *plan) securityContext() map[string]any {
	if p.proj.Deploy.Settings().PodSecurity != deployfile.PodSecurityRestricted {
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
		"image": manifest.ImageRef(c.Manifest),
		"ports": containerPorts(c.Manifest),
	}

	if env := p.envDoc(c); len(env) > 0 {
		container["env"] = env
	}
	if probe := livenessProbe(c.Manifest); probe != nil {
		// 顺序无关（YAML 是映射），但有 healthCheck 时三者必须一起出现（就绪探针在下面，此时一定有）：
		// startupProbe 在通过之前会**同时**禁用 liveness 与 readiness，
		// 只生成其中一部分会让慢启动的组件在两种目标下表现不一致
		container["startupProbe"] = startupProbe(c.Manifest)
		container["livenessProbe"] = probe
	}
	// healthCheck 是 none 时也可以单独声明 readinessCheck：那就只有就绪探针
	if probe := readinessProbe(c.Manifest); probe != nil {
		container["readinessProbe"] = probe
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

// containerPorts 渲染 containerPort 列表：主端口 + extraPorts。
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
		if envPlacement(v) != placePlain {
			name, key := secretRef(v)
			out = append(out, map[string]any{
				"name": v.Name,
				"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{"name": name, "key": key},
				},
			})
			continue
		}
		// value 必须是字符串：K8s 的 env.value 是 string 类型，
		// 写成数字会被 API Server 直接拒绝。求值错误已经在 collectSecrets 里报过
		value, _ := p.valueOf(v)
		out = append(out, map[string]any{"name": v.Name, "value": kubeletEscape(value)})
	}
	return out
}

// livenessProbe 渲染存活探针。
//
// ⚠️ 探的是主端口上的 /healthz，而 /healthz 只应检查本进程存活：
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

// startupProbe 渲染启动探针。
//
// # 为什么必须有它，而不是把 livenessProbe 的 initialDelaySeconds 调大
//
// 没有启动探针时，一个冷启动 45 秒的组件会在 t≈30s 被 livenessProbe 判死
// （10s 初始延迟 + 10s × 3 次失败）→ Pod 被 kill → 重启 → 再走一遍同样的
// 30 秒 → **永久 CrashLoopBackOff**。而容器日志一路正常，最难联想到探针。
//
// 调大 initialDelaySeconds 只是把这条线往后挪，代价是**所有**组件的故障
// 发现时间都跟着变慢——快启动的组件本来 10 秒就能被发现死了。
// 启动探针把两件事分开：起来之前给足时间，起来之后照常严格。
// 它一旦通过就不再执行，之后完全由 liveness / readiness 接管。
//
// ⚠️ 启动探针通过之前，**readinessProbe 也是禁用的**——Service 不会把流量
// 转给它。这正是想要的：还没起来的实例不该收请求。
func startupProbe(m *manifest.Manifest) map[string]any {
	action := probeAction(m)
	if action == nil {
		return nil
	}
	probe := map[string]any{
		"periodSeconds":  startupPeriod,
		"timeoutSeconds": probeTimeout,
		// 总预算 = periodSeconds × failureThreshold ≈ 组件声明的启动宽限期。
		// 向上取整：宁可多给几秒，也不要因为除不尽而比声明的少
		"failureThreshold": startupFailureThreshold(m.HealthCheck.StartPeriod()),
	}
	for k, v := range action {
		probe[k] = v
	}
	return probe
}

// startupFailureThreshold 把"宽限期多少秒"换算成启动探针的失败次数。
func startupFailureThreshold(seconds int) int {
	threshold := (seconds + startupPeriod - 1) / startupPeriod
	if threshold < 1 {
		return 1
	}
	return threshold
}

// readinessProbe 渲染就绪探针：探的是 readinessCheck（声明了的话），否则与存活探针同一道检查。
func readinessProbe(m *manifest.Manifest) map[string]any {
	action := readinessAction(m)
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
// 因此那边必须凑出一条镜像里真有的命令）。
func probeAction(m *manifest.Manifest) map[string]any {
	if m == nil {
		return nil
	}
	return checkAction(m, m.HealthCheck.Type, m.HealthCheck.Path)
}

// readinessAction 是就绪探针的动作：声明了 readinessCheck 探它，否则与存活探针相同（manifest.ReadyCheck）。
func readinessAction(m *manifest.Manifest) map[string]any {
	if m == nil {
		return nil
	}
	checkType, path := m.ReadyCheck()
	return checkAction(m, checkType, path)
}

// checkAction 把一道检查（类型 + 路径）转成探针动作，探的都是主端口。
func checkAction(m *manifest.Manifest, checkType, path string) map[string]any {
	switch checkType {
	case manifest.HealthCheckHTTP:
		return map[string]any{"httpGet": map[string]any{
			"path": path,
			"port": m.Deployment.Port,
		}}
	case manifest.HealthCheckTCP:
		return map[string]any{"tcpSocket": map[string]any{"port": m.Deployment.Port}}
	default:
		// none：不生成探针。探不通的探针会让 K8s 反复杀掉一个其实健康的 Pod
		return nil
	}
}

// resourcesDoc 渲染资源配额。
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

// kubeletEscape 让 env[].value 原样到达容器：K8s 会对它做自己的展开（$(VAR) 换成前面的
// 环境变量，$$ 缩成 $），而 brickkit 从不生成 $(VAR) 引用，所以每个 $ 都写成 $$。
// 值此时已经按 brickkit 的规则求过值（${VAR} 已展开），这里只防 K8s 再改一遍。
func kubeletEscape(value string) string { return strings.ReplaceAll(value, "$", "$$") }
