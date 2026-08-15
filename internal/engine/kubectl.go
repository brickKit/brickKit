package engine

// 本文件是 kubectl 引擎（005 §5.7）。
//
// 与 compose 引擎共用同一个 Engine 接口，但两处约定的含义不同：
//
//	UpRequest.File     compose 是**部署文件**；这里是 k8s 清单**目录**
//	UpRequest.Project  compose 是项目名；这里是命名空间（两者取值相同，见 k8s.Namespace）
//
// 之所以还能共用一个接口：命令层要的东西是一样的——"把这些东西起起来、
// 停下来、告诉我现在什么状态"。差别全在这一层里消化掉。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 超时。K8s 侧没有 compose 那样的"等到好为止"，必须自己设上限，
// 否则一个卡住的迁移会让 brickkit up 永远挂着。
const (
	migrationTimeout = "10m"
	rolloutTimeout   = "5m"
)

// Kubectl 是基于 kubectl 的引擎实现。
type Kubectl struct {
	bin string
	// runner 执行命令，测试可替换。
	runner func(ctx context.Context, name string, args ...string) ([]byte, error)
	// exists 判断某个文件或目录是否存在，测试可替换。
	exists func(path string) bool
	// context 是本次操作钉住的 kubeconfig 上下文；空表示用当前 context。
	context string
}

// NewKubectl 返回 kubectl 引擎。
func NewKubectl() *Kubectl {
	return &Kubectl{bin: "kubectl", runner: run, exists: pathExists}
}

// pathExists 判断文件或目录是否存在。
//
// 生成器只会创建**有内容**的子目录（没有 Ingress 就没有 ingress/），
// 命名空间由运维建时也不会有 namespace.yaml——
// 而 `kubectl apply -f 一个不存在的路径` 会直接失败。
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (k *Kubectl) Name() string { return K8s }

// Up 按顺序把清单交给集群（005 §5.7）。
//
// 顺序不是随便排的：
//
//	namespace    别的东西都得放进去
//	secrets      Pod 起来的那一刻就要读密码
//	migrations   业务代码不能撞上旧表结构；K8s 没有 compose 的 depends_on，
//	             只能由 CLI 串行控制：清理旧 Job → apply → wait
//	deployments  最后才是主服务
func (k *Kubectl) Up(ctx context.Context, req UpRequest) error {
	k.context = req.Context

	// 命名空间可能是运维建好的（deploy.createNamespace: false），那时不生成这份文件
	namespace := path.Join(req.File, "namespace.yaml")
	if k.exists(namespace) {
		if err := k.apply(ctx, "", namespace); err != nil {
			return err
		}
	}
	if err := k.applyDir(ctx, req.Project, req.File, "secrets"); err != nil {
		return err
	}
	if err := k.runMigrations(ctx, req); err != nil {
		return err
	}
	for _, dir := range []string{"deployments", "services", "ingress"} {
		if err := k.applyDir(ctx, req.Project, req.File, dir); err != nil {
			return err
		}
	}
	return k.waitRollout(ctx, req)
}

// runMigrations 执行数据库迁移并等它跑完（005 §6.3）。
func (k *Kubectl) runMigrations(ctx context.Context, req UpRequest) error {
	if len(req.MigrationJobs) == 0 {
		return nil
	}

	// 16.14：先清掉可能残留的旧 Job。
	//
	// Job 的 spec 是不可变的：上一次失败留下的同名 Job 还在时，直接 apply 会以
	// "field is immutable" 失败——而使用者只是改了迁移脚本想重跑一次。
	for _, job := range req.MigrationJobs {
		if _, err := k.exec(ctx, k.args(req.Project,
			"delete", "job/"+job, "--ignore-not-found")...); err != nil {
			return err
		}
	}
	if err := k.applyDir(ctx, req.Project, req.File, "migrations"); err != nil {
		return err
	}

	for _, job := range req.MigrationJobs {
		_, err := k.exec(ctx, k.args(req.Project, "wait", "--for=condition=complete",
			"--timeout="+migrationTimeout, "job/"+job)...)
		if err != nil {
			return migrationFailure(job, req.Project, err)
		}
	}
	return nil
}

// waitRollout 等每个 Deployment 的副本真正就绪。
//
// 没有这一步，`kubectl apply` 一返回就去查状态，得到的全是 ContainerCreating——
// 看起来像"全都没起来"。compose 那边用 `--wait` 解决同一个问题。
func (k *Kubectl) waitRollout(ctx context.Context, req UpRequest) error {
	for _, service := range req.Services {
		_, err := k.exec(ctx, k.args(req.Project, "rollout", "status",
			"deployment/"+service, "--timeout="+rolloutTimeout)...)
		if err != nil {
			return err
		}
	}
	return nil
}

// Down 删除本项目在集群里的一切。
func (k *Kubectl) Down(ctx context.Context, req DownRequest) error {
	k.context = req.Context

	if len(req.Services) > 0 {
		// 只停一部分：删对应的 Deployment 就够了。
		// 不能删命名空间——别的组件还在里面跑
		args := k.args(req.Project, "delete")
		for _, service := range req.Services {
			args = append(args, "deployment/"+service)
		}
		_, err := k.exec(ctx, append(args, "--ignore-not-found")...)
		return err
	}

	if !req.DeleteNamespace {
		// 命名空间不是我们建的（deploy.createNamespace: false），就不能由我们删——
		// 那是别人的命名空间，里面多半还跑着别的东西，删掉等于把整个团队一起端了。
		// 因此逐个子目录删，绝不用 -R 扫到 namespace.yaml
		for _, dir := range manifestDirs {
			if err := k.deleteDir(ctx, req.Project, req.File, dir); err != nil {
				return err
			}
		}
		return nil
	}

	// -R 不能省：清单分散在 deployments/ services/ 等子目录里，不加 -R 的话
	// kubectl 只看目录第一层
	_, err := k.exec(ctx, k.args("", "delete", "-R", "-f", req.File, "--ignore-not-found")...)
	return err
}

// manifestDirs 是生成物的全部子目录。
var manifestDirs = []string{"ingress", "services", "deployments", "migrations", "secrets"}

// deleteDir 删掉一个子目录里的清单；目录不存在就跳过。
func (k *Kubectl) deleteDir(ctx context.Context, namespace, root, name string) error {
	target := path.Join(root, name)
	if !k.exists(target) {
		return nil
	}
	_, err := k.exec(ctx, k.args(namespace, "delete", "-f", target, "--ignore-not-found")...)
	return err
}

// CurrentContext 返回 kubeconfig 当前的 context。
func (k *Kubectl) CurrentContext(ctx context.Context) (string, error) {
	out, err := k.exec(ctx, "config", "current-context")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Status 返回该命名空间下所有 Deployment 的状态。
func (k *Kubectl) Status(ctx context.Context, _, project string) ([]Status, error) {
	out, err := k.exec(ctx, k.args(project, "get", "deployments", "-o", "json")...)
	if err != nil {
		// 命名空间不存在只说明"还没 up 过"，不是故障
		if strings.Contains(strings.ToLower(string(out)), "not found") {
			return nil, nil
		}
		return nil, err
	}
	return parseDeployments(out)
}

// CheckImage 在 K8s 下什么都不做。
//
// 拉镜像的是集群里的 kubelet，用的是集群的凭据；开发机上能不能拉到，
// 说明不了任何问题。在这里"检查"一遍，只会得到误导性的结论。
func (k *Kubectl) CheckImage(context.Context, string) error { return nil }

// ============================================================
// 命令拼装
// ============================================================

// args 拼出带命名空间与 context 的参数。
//
// --context 显式传，不能只靠"执行前校验过 current-context"：校验与执行之间
// 有时间差，使用者可能在另一个终端里把 context 切走了。
func (k *Kubectl) args(namespace string, rest ...string) []string {
	var prefix []string
	if k.context != "" {
		prefix = append(prefix, "--context", k.context)
	}
	if namespace != "" {
		prefix = append(prefix, "-n", namespace)
	}
	return append(prefix, rest...)
}

// apply 应用一份清单。namespace 为空表示不带 -n（建命名空间那条命令本身）。
func (k *Kubectl) apply(ctx context.Context, namespace, target string) error {
	_, err := k.exec(ctx, k.args(namespace, "apply", "-f", target)...)
	return err
}

// applyDir 应用一个子目录；目录不存在时（比如没有 Ingress）跳过。
func (k *Kubectl) applyDir(ctx context.Context, namespace, root, name string) error {
	if !k.exists(path.Join(root, name)) {
		return nil
	}
	return k.apply(ctx, namespace, path.Join(root, name))
}

func (k *Kubectl) exec(ctx context.Context, args ...string) ([]byte, error) {
	out, err := k.runner(ctx, k.bin, args...)
	if err == nil {
		return out, nil
	}
	if isMissingBinary(err) {
		return out, clierr.New(clierr.CodeEngineMissing, "错误：找不到 kubectl").
			WithHint(
				"安装 kubectl 后重试：https://kubernetes.io/docs/tasks/tools/",
				"本地开发可以把 deploy.target 改成 docker",
			).
			WithCause(err)
	}
	return out, clierr.New(clierr.CodeEngineFailed, "错误：kubectl 执行失败").
		WithDetail("命令", k.bin+" "+strings.Join(args, " ")).
		WithDetail("输出", tail(string(out), 3)).
		WithCause(err)
}

// migrationFailure 把等待超时/失败翻译成一条能指出下一步的错误。
func migrationFailure(job, namespace string, cause error) error {
	return clierr.New(clierr.CodeMigrationFailed, "错误：数据库迁移失败").
		WithDetail("Job", job).
		WithDetail("命名空间", namespace).
		WithDetail("看事件", fmt.Sprintf("kubectl describe job/%s -n %s", job, namespace)).
		WithDetail("看日志", fmt.Sprintf("kubectl logs job/%s -n %s", job, namespace)).
		WithDetail("先看事件再看日志",
			"Job 迟迟不结束、日志又是空的时，多半是准入控制（PodSecurity / ResourceQuota / "+
				"LimitRange）拒绝了创建 Pod——那时 apply 是成功的，Pod 却根本没被创建，"+
				"一条日志也不会有，原因只写在 Job 的 events 里").
		WithHint(
			"迁移失败时主服务不会启动（backoffLimit: 0，不会自动重试）",
			"修好迁移脚本后重新 brickkit up：CLI 会自动清理这个 Job 再跑一次",
		).
		WithCause(cause)
}

// ============================================================
// 输出解析
// ============================================================

// `kubectl get deployments -o json` 里我们关心的那几个字段。
type (
	deploymentList struct {
		Items []deploymentItem `json:"items"`
	}
	deploymentItem struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []container `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			Replicas      int `json:"replicas"`
			ReadyReplicas int `json:"readyReplicas"`
		} `json:"status"`
	}
	container struct {
		Ports []struct {
			ContainerPort int `json:"containerPort"`
		} `json:"ports"`
	}
)

func parseDeployments(out []byte) ([]Status, error) {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil, nil
	}

	var list deploymentList
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		return nil, clierr.New(clierr.CodeEngineFailed, "错误：无法解析 kubectl 的输出").
			WithDetail("输出", tail(text, 3)).
			WithCause(err)
	}

	statuses := make([]Status, 0, len(list.Items))
	for _, item := range list.Items {
		desired := 1
		if item.Spec.Replicas != nil {
			desired = *item.Spec.Replicas
		}

		status := Status{
			Service: item.Metadata.Name, State: "running",
			Ports: containerPortsOf(item.Spec.Template.Spec.Containers),
		}
		switch {
		case desired == 0:
			// 手工缩到 0：不是坏了，但也确实没在跑
			status.State = "stopped"
		case item.Status.ReadyReplicas >= desired:
			status.Health = "healthy"
		default:
			// 副本没就绪时请求打过去是通不了的，不能算"在跑"
			status.Health = "starting"
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// portsOf 把容器端口拼成与 compose 那边同样风格的一行描述。
func containerPortsOf(containers []container) string {
	var parts []string
	for _, container := range containers {
		for _, port := range container.Ports {
			parts = append(parts, fmt.Sprintf("%d/tcp", port.ContainerPort))
		}
	}
	return strings.Join(parts, ", ")
}
