package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/manifest"
)

// newStatusCommand 实现 brickkit status（004 §3.7）。
func newStatusCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "status",
		Short:   "查看组件运行状态（读取底层引擎）",
		GroupID: groupLifecycle,
		Long: `查看当前项目所有组件的运行状态（004 §3.7）。

CLI 本身不存储运行状态，查询时直接调用底层引擎：
  Docker  docker compose ps --format json

输出包含：运行中的组件、未启动的组件及原因、
本地调试组件（local: true）、基础资源可达性。`,
		Example: "  brickkit status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), opts)
		},
	}
	return cmd
}

// runStatus 汇报项目现状。
func runStatus(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}

	p, err := loadProject(ctx, opts)
	if err != nil {
		return err
	}

	opts.Printf("📊 项目状态：%s（deploy.target: %s）\n\n", p.cfg.Project, p.cfg.Deploy.Target)
	if len(p.cfg.Components) == 0 {
		opts.Printf("📋 当前项目没有组件\n")
		// init 的骨架已经把 ./components 配成了本地安装源，所以 --local 是最短的一条路。
		// 两条都给：有的人手上已经有组件源码，有的人要从市场装。
		opts.Printf("   用 brickkit add --local 把 %s/ 下的组件全加进来\n", config.DirComponents)
		opts.Printf("   或 brickkit add <组件ID> 从安装源添加\n")
		return nil
	}
	// 直接问引擎。
	//
	// 从前这里先看 `.brickkit/generated/` 下那份生成物在不在，不在就断言
	// "项目尚未启动过"并返回，引擎一次都不调。而那份文件在 .gitignore 里、
	// 003 §7.1 还明说可以随时删——一次 `git clean -xdf` 之后，
	// status 会对着一屋子正在跑的容器说"尚未启动过"。
	//
	// 容器跑没跑只有引擎知道，所以只问它一处。"还没起过"与"已经 down 过"
	// 引擎本来就分不出，从前那句"尚未启动过"也只是拿一个文件在猜。
	eng, err := resolveEngineFor(opts, p.cfg)
	if err != nil {
		return err
	}
	if err := requireContext(ctx, opts, p.cfg, eng, p.cfg.Deploy.Context); err != nil {
		return err
	}
	statuses, err := eng.Status(ctx, p.engineProject())
	if err != nil {
		return engineFailure("查询状态", err)
	}

	byService := map[string]engine.Status{}
	for _, s := range statuses {
		byService[s.Service] = s
	}

	renderComponentStatus(opts, p, byService)
	renderSkipped(opts, p)
	renderLocalDebug(opts, p)
	renderResourceStatus(ctx, opts, p)
	return nil
}

// renderComponentStatus 输出"该跑的组件现在怎么样了"。
//
// 只列 brickkit.yaml 里的组件：迁移容器与基础资源是平台的实现细节，
// 使用者装的是组件，看到的也该是组件（资源单独一节汇报）。
func renderComponentStatus(opts *Options, p *project, byService map[string]engine.Status) {
	refs := p.containerRefs()
	if len(refs) == 0 {
		opts.Printf("⬜ 本次没有需要容器化启动的组件\n\n")
		return
	}

	running := newTable("组件", "版本", "状态", "端口")
	stopped := newTable("组件", "版本", "状态")
	stoppedCount := 0

	for _, ref := range refs {
		service := manifest.ServiceName(ref.ID, ref.Version)
		status, ok := byService[service]
		if ok && status.Running() {
			running.add(ref.ID, ref.Version, statusText(status, ok), status.Ports)
			continue
		}
		stopped.add(ref.ID, ref.Version, statusText(status, ok))
		stoppedCount++
	}

	if len(running.rows) > 0 {
		opts.Printf("✅ 运行中（%d 个组件）\n", len(running.rows))
		opts.Printf("%s\n", running.render(" "))
	}
	if stoppedCount > 0 {
		opts.Printf("❌ 未在运行（%d 个组件）\n", stoppedCount)
		opts.Printf("%s", stopped.render(" "))
		opts.Printf("   看日志定位：%s\n\n",
			logsCommand(engineName(opts), p.engineProject(), "<服务名>"))
	}
	if len(running.rows) == 0 && stoppedCount > 0 {
		opts.Printf("📋 没有正在运行的组件（可能已经 brickkit down 过）\n")
		opts.Printf("   重新启动：brickkit up\n\n")
	}
}

// statusText 把引擎的状态说成人话。
//
// 刻意不带 ● / ○ 这类符号：它们在 Unicode 里是"东亚宽度：模糊"，
// 同一个字符在不同终端下可能占 1 格也可能占 2 格，进了表格就对不齐
// （状态标记在表格外的小节标题里，✅ / ❌）。
func statusText(s engine.Status, found bool) string {
	if !found {
		// 部署文件里有、引擎里没有：多半是被 down 掉了
		return "未创建"
	}
	switch {
	case s.Running():
		if s.Health != "" {
			return "运行中（" + s.Health + "）"
		}
		return "运行中"
	case s.State == "exited":
		return fmt.Sprintf("exited（退出码 %d）", s.ExitCode)
	case s.State == "running":
		// running 但健康检查没过：对使用者来说它并不能用
		return "运行中但不健康（" + s.Health + "）"
	default:
		return s.State
	}
}

// renderSkipped 输出没启动的组件及原因（15.16）。
func renderSkipped(opts *Options, p *project) {
	if p.states == nil {
		return
	}

	t := newTable("组件", "版本", "原因")
	for _, c := range p.states.Components {
		if c.State == cascade.StateRunning {
			continue
		}
		t.add(c.Ref.ID, c.Ref.Version, c.Reason)
	}
	if len(t.rows) == 0 {
		return
	}

	opts.Printf("⬜ 未启动（%d 个组件）\n", len(t.rows))
	opts.Printf("%s\n", t.render(" "))
}

// renderLocalDebug 输出本地调试的组件（15.17）。
//
// 它们没有容器，引擎里查不到——不单独说一句的话，
// 使用者会以为这些组件"消失了"。
func renderLocalDebug(opts *Options, p *project) {
	refs := p.localRefs()
	if len(refs) == 0 {
		return
	}

	t := newTable("组件", "版本", "本地地址")
	for _, ref := range refs {
		port := p.entry(ref).LocalPort
		if port == 0 {
			// 没写 localPort 时默认取组件声明的主端口（005 §4.6）
			if node := p.graph.Node(ref); node != nil && node.Manifest != nil {
				port = node.Manifest.Deployment.Port
			}
		}
		t.add(ref.ID, ref.Version, fmt.Sprintf("localhost:%d（IDE 调试模式）", port))
	}

	opts.Printf("🔧 本地调试（local: true，不由平台启动）\n")
	opts.Printf("%s\n", t.render(" "))
}

// renderResourceStatus 输出基础资源的可达性（15.18）。
//
// 平台不部署基础资源（006 §9.1），所以判据只有一条：Docker 目标下真的拨一次号，
// K8s 目标下只把地址列出来（集群内的 DNS 名，本机解析不了）。
//
// 从前这里按"host 含不含点"分成两类，托管的看容器状态、外部的才拨号。
// 那条分叉随托管资源一起取消了。
func renderResourceStatus(ctx context.Context, opts *Options, p *project) {
	resources := usedResources(p)
	if len(resources) == 0 {
		return
	}

	k8sTarget := p.cfg.Deploy.Target == config.TargetK8s

	t := newTable("资源", "类型", "状态")
	for _, r := range resources {
		t.add(r.ID, r.Kind, resourceState(ctx, opts, r, k8sTarget))
	}

	opts.Printf("📦 资源状态\n")
	opts.Printf("%s\n", t.render(" "))
	if k8sTarget {
		opts.Printf("   资源在集群内访问，本机不做探测；组件跑起来了就说明它连得上\n")
		opts.Printf("   想从集群内验证：kubectl run -n %s --rm -it netcheck --image=busybox -- nc -zv <主机> <端口>\n\n",
			p.engineProject())
	}
}

// usedResources 返回被本次启动的组件用到的资源。
func usedResources(p *project) []config.Resource {
	used := map[string]bool{}
	for _, ref := range p.order {
		used[ref.ID] = true
	}

	var out []config.Resource
	for _, r := range p.cfg.Resources {
		for _, binding := range r.Bindings {
			if used[binding.ComponentID] {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// resourceState 判定一个资源现在通不通。
func resourceState(
	ctx context.Context, opts *Options, r config.Resource, k8sTarget bool,
) string {
	if k8sTarget {
		// K8s 下的资源地址是**集群内**的 DNS 名（postgres.infra），
		// 开发者本机根本解析不了。照 Docker 那套拨一次号，会对一个完全健康的
		// 部署报"不可达"——而组件正连着这个库跑得好好的。接上真集群第一次就撞到了
		return fmt.Sprintf("%s:%d（集群内地址，本机不探测）", r.Host, r.Port)
	}
	// Docker 目标下一律拨号。
	//
	// 从前这里还有一条分支：`host` 不含点时被判为"由 CLI 托管的资源容器"，
	// 于是改看容器状态而不是拨号。那条路已经取消——平台不再部署基础资源
	// （006 §9.1），所有资源都在容器网络之外，拨号是唯一说得通的判据。
	address := fmt.Sprintf("%s:%d", deploy.DialHost(r.Host), r.Port)
	if err := opts.probe(ctx, address); err != nil {
		return "不可达（" + address + "：" + reasonText(err) + "）"
	}
	return "可达（" + address + "）"
}

// reasonText 从拨号错误里取出人能看懂的那一句。
//
// net 包的错误长这样：`dial tcp 10.0.0.9:6379: connect: connection refused`，
// 前半截对使用者没有意义。
func reasonText(err error) string {
	text := err.Error()
	if idx := strings.LastIndex(text, ": "); idx >= 0 && idx+2 < len(text) {
		return text[idx+2:]
	}
	return text
}
