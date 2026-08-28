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
	"github.com/brickkit/brickkit/internal/resolver"
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

	view := buildView(p, byService)
	renderDegradedNotice(opts, p)
	renderComponentStatus(opts, p, view)
	renderSkipped(opts, view)
	renderLocalDebug(opts, p, view)
	renderResourceStatus(ctx, opts, p)
	return nil
}

// ============================================================
// 归属判定
// ============================================================

// reasonUnknown 是降级时给不出真实原因的那一句。
//
// 写"未知"而不是编一个：使用者据此去改配置，一句猜出来的原因会把他引向
// 一个根本没问题的地方。上面那条 renderDegradedNotice 已经说清楚为什么未知。
const reasonUnknown = "原因未知（依赖图取不到）"

// statusRow 是表格里的一行：一个组件 + 一句话。
type statusRow struct {
	ref resolver.Ref
	// text 是状态（运行中 / exited…）或不启动的原因。
	text  string
	ports string
}

// componentView 是"每个组件这次归到哪一节"的**唯一**判定。
//
// 正常与降级两条路的差别全部收在 buildView 里：渲染只认这份结果，
// 不再各自去问 p.order / p.states——那样两条路会各写一遍分类逻辑，
// 而它们必须永远给出同一种归属。
type componentView struct {
	// running 是引擎里在跑的；failed 是该跑却没跑的。
	running []statusRow
	failed  []statusRow
	// skipped 是本次不启动的组件及原因。
	skipped []statusRow
	// local 是 local: true、由使用者自己在 IDE 里跑的组件（没有容器）。
	local []resolver.Ref
}

func buildView(p *project, byService map[string]engine.Status) componentView {
	if p.degraded != nil {
		return degradedView(p, byService)
	}
	return resolvedView(p, byService)
}

// resolvedView 是依赖图解析成功时的归属：判定说了算。
func resolvedView(p *project, byService map[string]engine.Status) componentView {
	var v componentView
	for _, ref := range p.containerRefs() {
		status, ok := byService[manifest.ServiceName(ref.ID, ref.Version)]
		row := statusRow{ref: ref, text: statusText(status, ok), ports: status.Ports}
		if ok && status.Running() {
			v.running = append(v.running, row)
			continue
		}
		v.failed = append(v.failed, row)
	}
	if p.states != nil {
		for _, c := range p.states.Components {
			if c.State != cascade.StateRunning {
				v.skipped = append(v.skipped, statusRow{ref: c.Ref, text: c.Reason})
			}
		}
	}
	v.local = p.localRefs()
	return v
}

// degradedView 是依赖图取不到时的归属：改由**引擎里有没有它**说了算。
//
//	在跑        ✅ 运行中
//	有记录没跑  ❌ 未在运行 —— 引擎里有它，说明它确实被部署过
//	查不到      判不出它该不该跑，一律进"未启动"，原因写实话：
//	            enabled: false 是 brickkit.yaml 里就写着的，其余写"原因未知"
//
// **绝不把"查不到"算成"未在运行"**：那一节的意思是"该跑却没跑"，
// 而这时恰恰不知道它该不该跑——说成没起来，是在冤枉一个本来就该停着的组件，
// 而使用者会照着这句话去查一个根本不存在的故障。
//
// 同样绝不把它整个略去：配置里声明过的组件凭空消失，使用者只会以为组件没了。
func degradedView(p *project, byService map[string]engine.Status) componentView {
	var v componentView
	for _, c := range p.cfg.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		status, ok := byService[manifest.ServiceName(ref.ID, ref.Version)]
		switch {
		case ok && status.Running():
			v.running = append(v.running,
				statusRow{ref: ref, text: statusText(status, ok), ports: status.Ports})
		case ok:
			v.failed = append(v.failed, statusRow{ref: ref, text: statusText(status, ok)})
		case c.IsDisabled():
			v.skipped = append(v.skipped, statusRow{ref: ref, text: reasonDisabled})
		case c.Local:
			// local 组件本来就不会出现在引擎里，"查不到"是它的正常状态
			v.local = append(v.local, ref)
		default:
			v.skipped = append(v.skipped, statusRow{ref: ref, text: reasonUnknown})
		}
	}
	return v
}

// renderDegradedNotice 说明"依赖图没取到，因此下面有一节不完整"。
//
// 放在最前面：使用者要先知道这份报告哪里打了折扣，再去读它。
// 解析失败的原因原样带出来（哪个文件、第几行、哪个字段）——那正是他要改的地方，
// 只说一句"解析失败"等于让他自己再跑一次别的命令去问。
func renderDegradedNotice(opts *Options, p *project) {
	if p.degraded == nil {
		return
	}

	opts.Printf("\u26a0\ufe0f 未能解析依赖图，「未启动」那一节只能给出部分原因\n")
	opts.Printf("   %s\n", strings.TrimPrefix(p.degraded.Message, "错误："))
	for _, d := range p.degraded.Details {
		opts.Printf("   %s：%s\n", d.Key, d.Value)
	}
	opts.Printf("   「运行中」与「资源状态」不受影响——它们只问引擎和 brickkit.yaml\n\n")
}

// renderComponentStatus 输出"该跑的组件现在怎么样了"。
//
// 只列 brickkit.yaml 里的组件：迁移容器与基础资源是平台的实现细节，
// 使用者装的是组件，看到的也该是组件（资源单独一节汇报）。
func renderComponentStatus(opts *Options, p *project, v componentView) {
	if len(v.running) == 0 && len(v.failed) == 0 && len(v.skipped) == 0 && len(v.local) == 0 {
		opts.Printf("⬜ 本次没有需要容器化启动的组件\n\n")
		return
	}

	if len(v.running) > 0 {
		t := newTable("组件", "版本", "状态", "端口")
		for _, row := range v.running {
			t.add(row.ref.ID, row.ref.Version, row.text, row.ports)
		}
		opts.Printf("✅ 运行中（%d 个组件）\n", len(v.running))
		opts.Printf("%s\n", t.render(" "))
	}
	if len(v.failed) > 0 {
		t := newTable("组件", "版本", "状态")
		for _, row := range v.failed {
			t.add(row.ref.ID, row.ref.Version, row.text)
		}
		opts.Printf("❌ 未在运行（%d 个组件）\n", len(v.failed))
		opts.Printf("%s", t.render(" "))
		opts.Printf("   看日志定位：%s\n\n",
			logsCommand(engineName(opts), p.engineProject(), "<服务名>"))
	}
	if len(v.running) == 0 && len(v.failed) > 0 {
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
func renderSkipped(opts *Options, v componentView) {
	if len(v.skipped) == 0 {
		return
	}

	t := newTable("组件", "版本", "原因")
	for _, row := range v.skipped {
		t.add(row.ref.ID, row.ref.Version, row.text)
	}
	opts.Printf("⬜ 未启动（%d 个组件）\n", len(v.skipped))
	opts.Printf("%s\n", t.render(" "))
}

// renderLocalDebug 输出本地调试的组件（15.17）。
//
// 它们没有容器，引擎里查不到——不单独说一句的话，
// 使用者会以为这些组件"消失了"。
func renderLocalDebug(opts *Options, p *project, v componentView) {
	if len(v.local) == 0 {
		return
	}

	t := newTable("组件", "版本", "本地地址")
	for _, ref := range v.local {
		t.add(ref.ID, ref.Version, localAddress(p, ref))
	}

	opts.Printf("🔧 本地调试（local: true，不由平台启动）\n")
	opts.Printf("%s\n", t.render(" "))
}

// localAddress 是 local 组件在宿主机上的地址。
//
// 没写 localPort 时默认取组件自己声明的主端口（005 §4.6）——那要读 Manifest。
// 降级时读不到，就老实说读不到：编一个端口号出来，使用者会照着它去连一个没人监听的口。
func localAddress(p *project, ref resolver.Ref) string {
	if port := p.entry(ref).LocalPort; port > 0 {
		return fmt.Sprintf("localhost:%d（IDE 调试模式）", port)
	}
	if p.graph != nil {
		if node := p.graph.Node(ref); node != nil && node.Manifest != nil {
			return fmt.Sprintf("localhost:%d（IDE 调试模式）", node.Manifest.Deployment.Port)
		}
	}
	return "端口未知（没写 localPort，而组件声明的端口取不到）"
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
	for _, ref := range p.componentRefs() {
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
