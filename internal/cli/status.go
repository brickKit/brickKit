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
  Podman  podman-compose ps

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

	eng, err := resolveEngine(opts)
	if err != nil {
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

	var running, stopped []string
	for _, ref := range refs {
		service := manifest.ServiceName(ref.ID, ref.Version)
		status, ok := byService[service]
		line := fmt.Sprintf("   %-24s %-8s %s", ref.ID, ref.Version, statusText(status, ok))
		if ok && status.Running() {
			if status.Ports != "" {
				line += "  " + status.Ports
			}
			running = append(running, line)
			continue
		}
		stopped = append(stopped, line)
	}

	if len(running) > 0 {
		opts.Printf("✅ 运行中（%d 个组件）\n", len(running))
		for _, line := range running {
			opts.Println(line)
		}
		opts.Printf("\n")
	}
	if len(stopped) > 0 {
		opts.Printf("❌ 未在运行（%d 个组件）\n", len(stopped))
		for _, line := range stopped {
			opts.Println(line)
		}
		opts.Printf("   看日志定位：docker compose -f %s logs <服务名>\n\n",
			displayPath(opts.WorkDir, p.file))
	}
	if len(running) == 0 && len(stopped) > 0 {
		opts.Printf("📋 没有正在运行的组件（可能已经 brickkit down 过）\n")
		opts.Printf("   重新启动：brickkit up\n\n")
	}
}

// statusText 把引擎的状态说成人话。
func statusText(s engine.Status, found bool) string {
	if !found {
		// 部署文件里有、引擎里没有：多半是被 down 掉了
		return "⬜ 未创建"
	}
	switch {
	case s.Running():
		if s.Health != "" {
			return "● 运行中（" + s.Health + "）"
		}
		return "● 运行中"
	case s.State == "exited":
		return fmt.Sprintf("○ exited（退出码 %d）", s.ExitCode)
	case s.State == "running":
		// running 但健康检查没过：对使用者来说它并不能用
		return "◐ 运行中但不健康（" + s.Health + "）"
	default:
		return "○ " + s.State
	}
}

// renderSkipped 输出没启动的组件及原因（15.16）。
func renderSkipped(opts *Options, p *project) {
	if p.states == nil {
		return
	}

	var lines []string
	for _, c := range p.states.Components {
		if c.State == cascade.StateRunning {
			continue
		}
		lines = append(lines, fmt.Sprintf("   %-24s %-8s %s", c.Ref.ID, c.Ref.Version, c.Reason))
	}
	if len(lines) == 0 {
		return
	}

	opts.Printf("⬜ 未启动（%d 个组件）\n", len(lines))
	for _, line := range lines {
		opts.Println(line)
	}
	opts.Printf("\n")
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

	opts.Printf("🔧 本地调试（local: true，不由平台启动）\n")
	for _, ref := range refs {
		port := p.entry(ref).LocalPort
		if port == 0 {
			// 没写 localPort 时默认取组件声明的主端口（005 §4.6）
			if node := p.graph.Node(ref); node != nil && node.Manifest != nil {
				port = node.Manifest.Deployment.Port
			}
		}
		opts.Printf("   %-24s %-8s → localhost:%d（IDE 调试模式）\n", ref.ID, ref.Version, port)
	}
	opts.Printf("\n")
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

	opts.Printf("📦 资源状态\n")
	for _, r := range resources {
		opts.Printf("   %-24s %s\n", r.ID, resourceState(ctx, opts, r, byService))
	}
	opts.Printf("\n")
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
	ctx context.Context, opts *Options, r config.Resource, byService map[string]engine.Status,
) string {
	if compose.IsManagedHost(r.Host) {
		status, ok := byService[r.Host]
		switch {
		case !ok:
			return "○ 不可达（容器 " + r.Host + " 未创建）"
		case status.Running():
			return "● 可达（容器 " + r.Host + " 运行中）"
		default:
			return "○ 不可达（容器 " + r.Host + " " + status.State + "）"
		}
	}

	address := fmt.Sprintf("%s:%d", r.Host, r.Port)
	if err := opts.probe(ctx, address); err != nil {
		return "○ 不可达（" + address + "：" + reasonText(err) + "）"
	}
	return "● 可达（" + address + "）"
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
