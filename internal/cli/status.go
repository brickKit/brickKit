package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
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
		opts.Printf("   用 brickkit add <组件ID>@<版本> 添加第一个组件\n")
		return nil
	}
	if !p.deployed {
		// 先把"为什么一个都没跑"说清楚，再说"还没启动过"。
		// 配置里全都 enabled: false 时，一句"尚未启动过"等于什么都没回答。
		renderSkipped(opts, p)
		renderLocalDebug(opts, p)
		if p.states != nil && p.states.Empty() {
			opts.Printf("📋 按当前配置，本次没有组件会启动（原因见上）\n")
			return nil
		}
		opts.Printf("📋 项目尚未启动过（没有找到 %s）\n", displayPath(opts.WorkDir, p.file))
		opts.Printf("   用 brickkit up 启动\n")
		return nil
	}

	eng, err := resolveEngineFor(opts, p.cfg)
	if err != nil {
		return err
	}
	if err := requireContext(ctx, opts, p.cfg, eng, p.cfg.Deploy.Context); err != nil {
		return err
	}
	statuses, err := eng.Status(ctx, p.file, p.engineProject())
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
	renderResourceStatus(ctx, opts, p, byService)
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
		opts.Printf("   看日志定位：%s\n\n", logsCommand(engineName(opts), p.engineProject(),
			displayPath(opts.WorkDir, p.file), "<服务名>"))
	}
	if len(running.rows) == 0 && stoppedCount > 0 {
		opts.Printf("📋 没有正在运行的组件（可能已经 brickkit down 过）\n")
		opts.Printf("   重新启动：brickkit up\n\n")
	}
}

// statusText 把引擎的状态说成人话。
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
// 两类资源判定方式完全不同：
//
//	CLI 托管的（host 是服务名）  它在容器网络里，宿主机拨号根本解析不了这个名字，
//	                            只能看容器状态
//	外部的（IP / 域名）          运维已部署，真的拨一次号才知道通不通
func renderResourceStatus(
	ctx context.Context, opts *Options, p *project, byService map[string]engine.Status,
) {
	resources := usedResources(p)
	if len(resources) == 0 {
		return
	}

	k8sTarget := p.cfg.Deploy.Target == config.TargetK8s

	t := newTable("资源", "类型", "状态")
	for _, r := range resources {
		t.add(r.ID, r.Kind, resourceState(ctx, opts, r, byService, k8sTarget))
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
	ctx context.Context, opts *Options, r config.Resource,
	byService map[string]engine.Status, k8sTarget bool,
) string {
	if k8sTarget {
		// K8s 下的资源地址是**集群内**的 DNS 名（postgres.infra），
		// 开发者本机根本解析不了。照 Docker 那套拨一次号，会对一个完全健康的
		// 部署报"不可达"——而组件正连着这个库跑得好好的。接上真集群第一次就撞到了
		return fmt.Sprintf("%s:%d（集群内地址，本机不探测）", r.Host, r.Port)
	}
	if compose.IsManagedHost(r.Host) {
		status, ok := byService[r.Host]
		switch {
		case !ok:
			return "不可达（容器 " + r.Host + " 未创建）"
		case status.Running():
			return "可达（容器 " + r.Host + " 运行中）"
		default:
			return "不可达（容器 " + r.Host + " " + status.State + "）"
		}
	}

	address := fmt.Sprintf("%s:%d", r.Host, r.Port)
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
