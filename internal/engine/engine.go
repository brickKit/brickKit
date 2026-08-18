// Package engine 抽象底层容器引擎（Docker）与部署目标（Kubernetes）。
//
// CLI 自己不懂容器，它只做三件事：把 brickkit.yaml 翻译成部署文件、
// 决定谁该启动、然后把文件交给引擎。这一层的存在是为了让"决定"与"执行"
// 分开——命令层的逻辑因此可以在没有 Docker 的机器上被完整测试。
//
// 目前只有 Docker 一种容器引擎。Podman 写过、跑过，因 `down` 无法可靠工作
// 而移除（005 §7）；`Detect` 在只装了 Podman 的机器上会如实说明这件事。
package engine

import "context"

// 引擎名。与 compose 包的 EngineDocker 取值一致。
const (
	Docker = "docker"
	// K8s 是 kubectl 引擎。它不是"另一种容器引擎"，而是另一种**部署目标**
	// （005 §5）：清单交给集群，跑不跑得起来是集群的事。
	K8s = "k8s"
)

// Status 是一个 service 的运行状态。
type Status struct {
	// Service 是 compose 里的 service 名（即版本化服务名）。
	Service string
	// State 是引擎报告的状态：running / exited / created / restarting …
	State string
	// Health 是健康检查结论：healthy / unhealthy / starting；
	// 没有健康检查时为空（002 §9：不是所有组件都有健康检查）。
	Health string
	// Ports 是端口映射的原始描述，如 "0.0.0.0:18080->8080/tcp"。
	Ports string
	// ExitCode 只在 State 为 exited 时有意义。
	ExitCode int
}

// Running 判断该 service 是否处于"跑着且没坏"的状态。
//
// 有健康检查时以健康检查为准：一个 running 但 unhealthy 的容器
// 对使用者来说并不是"好的"。
func (s Status) Running() bool {
	if s.State != "running" {
		return false
	}
	return s.Health == "" || s.Health == "healthy"
}

// UpRequest 是一次启动请求。
type UpRequest struct {
	// File 是部署文件路径。
	File string
	// Project 是引擎侧的项目名。
	//
	// 必须显式传：compose 默认拿部署文件所在目录名当项目名，而我们的文件
	// 固定放在 .brickkit/generated/ 下——那样**所有** BrickKit 项目在同一台
	// 机器上都会叫 "generated"，彼此的容器互相顶替。
	Project string
	// ProjectDir 是项目根目录（brickkit.yaml 所在处）。
	//
	// compose 默认在**部署文件旁边**找 .env，而我们的文件固定放在
	// .brickkit/generated/ 下——使用者的 .env 在项目根，压根读不到。
	// 而 compose 对未定义的变量不报错，直接替换成空串：密码就这样静默变空了。
	// K8s 目标不需要它（${VAR} 在生成时已求值）。
	ProjectDir string
	// Services 为空表示全部启动；非空时只启动这些 service（--only）。
	Services []string
	// Context 是 kubeconfig 上下文，只对 K8s 目标有意义；为空表示用当前 context。
	Context string
	// MigrationJobs 是本次要执行的迁移 Job 名，只对 K8s 目标有意义。
	//
	// compose 用 depends_on + service_completed_successfully 表达"等迁移跑完"，
	// K8s 没有这种东西，只能由 CLI 串行控制：清理旧 Job → apply → wait（005 §6.3）。
	MigrationJobs []string
	// DesiredPDBs 是本次期望存在的 PodDisruptionBudget 名（P35）。
	//
	// 单列一份是因为 PDB **只在多副本时生成**，却与 Deployment 同名：
	// 清理时若只看名字，副本数从 3 改回 1 之后那份 PDB 会被当成"该留的"，
	// 从此永远拦着 kubectl drain（minikube 上真跑到过）。
	DesiredPDBs []string
	// PruneSelector 是清理孤儿资源用的标签选择器（如 `brickkit.io/project=my-erp`）。
	//
	// **空表示不清理**，这不是省略而是一种明确的表达：`--only` 只部署子集，
	// 那时 Services 里没有的组件并不是孤儿，照着清理会把它们全删掉——
	// 比 P38 本身危险得多。命令层用"不给选择器"表达这件事。
	//
	// 只对 K8s 目标有意义：compose 有 `--remove-orphans` 兜底，
	// 而 `kubectl apply` 默认不删目录里没有的资源（P38）。
	PruneSelector string
	// OnPrune 在清理掉一个孤儿资源时回调，供命令层如实汇报；为 nil 时不回调。
	//
	// 悄悄删东西是不可接受的：使用者得知道集群里少了什么，
	// 尤其在他其实是误删了配置、本意并非下线那个组件的时候。
	OnPrune func(resource string)
}

// DownRequest 是一次停止请求。
type DownRequest struct {
	File    string
	Project string
	// ProjectDir 同 UpRequest.ProjectDir。
	ProjectDir string
	// Context 同 UpRequest.Context。
	Context string
	// DeleteNamespace 表示可以连命名空间一起删（只对 K8s 目标有意义）。
	//
	// 命名空间不是我们建的（deploy.createNamespace: false）就不能由我们删——
	// 那是别人的命名空间，里面可能还跑着别的东西。
	DeleteNamespace bool
	// Services 为空表示整个项目停掉；非空时只停这些 service。
	Services []string
}

// Engine 是容器引擎的统一抽象。
type Engine interface {
	// Name 是引擎名（Docker / K8s）。
	Name() string
	// Up 启动（等价 compose up -d）。
	Up(ctx context.Context, req UpRequest) error
	// Down 停止。
	//
	// **不删除数据卷**（004 §3.6）：数据库数据始终保留。
	// 需要彻底清理时由使用者自己执行 docker volume rm。
	Down(ctx context.Context, req DownRequest) error
	// Status 返回该项目下所有 service 的状态。
	Status(ctx context.Context, file, project string) ([]Status, error)
	// CheckImage 检查镜像是否可用（本地已有，或能从 registry 取到）。
	CheckImage(ctx context.Context, image string) error
	// CurrentContext 返回引擎当前指向的部署上下文。
	//
	// 只有 K8s 有意义（kubeconfig 的 current-context）；compose 引擎返回空串。
	// 命令层据此拦下"部到了错误的集群"。
	CurrentContext(ctx context.Context) (string, error)
}
