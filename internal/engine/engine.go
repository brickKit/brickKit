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
	// Services 为空表示全部启动；非空时只把这些 service 交给引擎。
	//
	// 现在它总是本次会启动的完整一批（`--only` 已删，003 §4.3：要收窄范围就改
	// enabled）。留着这个字段是因为引擎不该假设调用方永远想全起。
	Services []string
	// Context 是 kubeconfig 上下文，只对 K8s 目标有意义；为空表示用当前 context。
	Context string
	// MigrationGroups 是本次要执行的迁移 Job，**按组件 ID 分组**，只对 K8s 目标有意义。
	//
	// compose 用 depends_on + service_completed_successfully 表达"等迁移跑完"，
	// K8s 没有这种东西，只能由 CLI 串行控制：清理旧 Job → apply → wait（005 §6.3）。
	//
	// 组内必须**一个跑完再下发下一个**：同一组件的多个版本共用一个库、共用一个
	// component_id，同时下发会在空库上撞主键（分组理由见 k8s.Result.MigrationGroups）。
	// 组间彼此独立，先全部下发再逐个等，不白白串行。
	MigrationGroups [][]string
	// Desired 是本次生成的**每一个** K8s 对象，写成 `<小写类型>/<名字>`
	// （如 `deployment/people-basic-1-0-0`、`secret/pg-main-secret`）。
	// 只对 K8s 目标有意义，由 k8s.Result.Desired 直接给出。
	//
	// 孤儿清理拿它与集群里带本项目标签的资源比对，判据只有一句：
	// **集群里有、本次没生成，就是孤儿**。
	//
	// 带上类型是必需的：Ingress / NetworkPolicy / ServiceAccount / PDB 都与
	// Deployment **同名**，且都是条件生成的。只比名字的话 Deployment 还在、
	// 名字就还在期望里，于是把 expose 改成 false 之后 Ingress 一直留着
	// 继续对外路由（详见 orphansIn）。
	Desired []string
	// PruneSelector 是"本次允不允许清理孤儿"的开关，两种目标共用一个判据。
	//
	// **空表示不清理**——引擎不替调用方假设"总是想清理"，那是命令层的判断。
	// 现在命令层总会给出选择器（每次 up 都按完整配置生成），但这个表达留着：
	// 一旦哪天又出现"只部署一个子集"的场景，Services 里没有的组件就不是孤儿了。
	//
	// 两种目标各自怎么落地：
	//
	//	K8s     `kubectl apply` 默认不删目录里没有的资源，CLI 按这个标签
	//	        选择器（如 `brickkit.io/project=my-erp`）比对集群实际资源。
	//	Docker  compose 的 `--remove-orphans` 就是这件事，选择器的值用不上，
	//	        只用"空 / 非空"决定带不带这个参数。
	//
	// 早先这里有过一个分支：`--only` 只起子集时两边都不清理，否则那份只含
	// 被点名组件的 compose 文件会让其余正在服务的组件全成了 orphan，
	// 一条 `up --only` 就把它们删光。`--only` 删除之后分支跟着消失了——
	// 现在每次 up 都按完整配置生成，文件里没有的只可能是使用者关掉的，
	// 删掉它的容器正是他要的（005 §5.9.2）。
	PruneSelector string
	// OnPrune 在清理掉一个孤儿资源时回调，供命令层如实汇报；为 nil 时不回调。
	//
	// 悄悄删东西是不可接受的：使用者得知道集群里少了什么，
	// 尤其在他其实是误删了配置、本意并非下线那个组件的时候。
	OnPrune func(resource string)
}

// DownRequest 是一次停止请求。
//
// **刻意没有 File 字段。** down 的身份是项目名（compose 的标签 / K8s 的
// brickkit.io/project），不是那份生成出来的部署文件——文件回答的是"这次打算
// 跑什么"，而 `up --dry-run` 也会重写它。拿它当"上次实际部署了什么"来删，
// 少一个 service 就漏停一个容器，命令却照样报成功（005 §5.9.3）。
//
// 字段留着就迟早有人用回去，所以这里把它整个拿掉：两个引擎都不可能再读到它。
type DownRequest struct {
	// Project 是引擎侧的项目名：Docker 下是 compose 项目名，K8s 下是**命名空间**。
	Project string
	// Selector 是本项目生成物共有的标签选择器（`brickkit.io/project=<项目名>`）。
	//
	// **它与 Project 不是一回事**，这正是它单独存在的理由：K8s 侧 Project 是
	// 命名空间，而标签值是 `brickkit.yaml` 里的项目名——写了 `deploy.namespace`
	// 时两者是完全无关的两个词。引擎从前自己用 Project 拼这个选择器，
	// 于是 `createNamespace: false` 那条路上一个资源都匹配不到：
	// 八条 delete 全部命中 0 个对象、退出码 0，而 CLI 报"已停止全部组件"。
	//
	// 值由命令层的 projectSelector 算出，与 UpRequest.PruneSelector 同源——
	// 两边各算一遍正是上面那个 bug 的成因，所以引擎侧不再有拼装它的能力。
	//
	// 只有 K8s 目标、且命名空间不是我们建的那条路会用到它；
	// 那条路上它**不能为空**，否则 `-l ""` 会匹配到别人的资源（见 Kubectl.Down）。
	Selector string
	// Context 同 UpRequest.Context。
	Context string
	// DeleteNamespace 表示可以连命名空间一起删（只对 K8s 目标有意义）。
	//
	// 命名空间不是我们建的（deploy.createNamespace: false）就不能由我们删——
	// 那是别人的命名空间，里面可能还跑着别的东西。
	DeleteNamespace bool
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
	//
	// **只认项目名，不认部署文件**（与 Down 同一条规则，005 §5.9.3）。
	// 两种引擎都做得到：compose 从容器标签认项目（`-p X ps` 不需要 `-f`），
	// kubectl 本来就只按命名空间查。
	//
	// 从前这里有个 file 参数。kubectl 侧从来没用过它，compose 侧则把
	// `-f <生成的部署文件>` 传了下去——于是那份文件被 `git clean -xdf` 清掉之后
	// （它在 .gitignore 里，而且文档明说这个目录是可再生的），
	// `status` 与 `down` 会双双报"项目尚未启动过"，而容器还跑着。
	// 参数整个拿掉，这条路就再也走不回去了。
	Status(ctx context.Context, project string) ([]Status, error)
	// CheckImage 检查镜像是否可用（本地已有，或能从 registry 取到）。
	CheckImage(ctx context.Context, image string) error
	// CurrentContext 返回引擎当前指向的部署上下文。
	//
	// 只有 K8s 有意义（kubeconfig 的 current-context）；compose 引擎返回空串。
	// 命令层据此拦下"部到了错误的集群"。
	CurrentContext(ctx context.Context) (string, error)
}
