package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/resolver"
)

// newDownCommand 实现 brickkit down（004 §3.6）。
func newDownCommand(opts *Options) *cobra.Command {
	var (
		only        []string
		kubeContext string
	)

	cmd := &cobra.Command{
		Use:     "down",
		Short:   "一键停止所有组件（不删除 volume）",
		GroupID: groupLifecycle,
		Long: `停止项目（004 §3.6）。

停止顺序与启动顺序相反（依赖方先停，被依赖方后停）。

重要：down 不删除 volume，数据库数据始终保留。
如需彻底清理，请手动执行 docker volume rm 或 docker compose down -v。`,
		Example: `  brickkit down
  brickkit down --only people/basic   只停止指定组件`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDown(cmd.Context(), opts, only, kubeContext)
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil, "只停止指定组件，逗号分隔，支持 @版本")
	cmd.Flags().StringVar(&kubeContext, "context", "", "kubeconfig 上下文，覆盖 deploy.context（仅 deploy.target: k8s）")
	return cmd
}

// runDown 停止项目或其中的部分组件。
func runDown(ctx context.Context, opts *Options, only []string, kubeContext string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	p, err := loadProject(ctx, opts)
	if err != nil {
		return err
	}
	if !p.deployed {
		// 部署文件都没有，引擎那边不会有本项目的任何东西
		opts.Printf("📋 项目尚未启动过（没有找到 %s）\n", displayPath(opts.WorkDir, p.file))
		opts.Printf("   用 brickkit up 启动\n")
		return nil
	}

	plan, err := downTargets(p, only)
	if err != nil {
		return err
	}
	// `--only` 点到的组件全都没有容器时**什么都不能做**：
	// 空的 services 会被引擎理解成"停整个项目"，那是彻底的误伤
	if plan.nothingToStop {
		opts.Printf("📋 没有可停止的组件\n")
		renderUntouched(opts, plan.untouched)
		opts.Printf("   本项目的其余组件不受影响；要全部停止请执行 brickkit down（不带 --only）\n")
		return nil
	}
	services := plan.services

	eng, err := resolveEngineFor(opts, p.cfg)
	if err != nil {
		return err
	}
	if err := requireContext(ctx, opts, p.cfg, eng, contextOf(p.cfg, kubeContext)); err != nil {
		return err
	}

	opts.Printf("🛑 停止项目 %s\n", p.cfg.Project)
	if len(services) > 0 {
		opts.Printf("   停止顺序（与启动顺序相反）：%s\n", joinComponents(services))
	}
	renderUntouched(opts, plan.untouched)

	if err := eng.Down(ctx, engine.DownRequest{
		File: p.file, Project: p.engineProject(), ProjectDir: opts.WorkDir,
		Context: contextOf(p.cfg, kubeContext), Services: services,
		// 命名空间不是我们建的就不能由我们删
		DeleteNamespace: p.cfg.Deploy.ShouldCreateNamespace(),
	}); err != nil {
		return engineFailure("停止", err)
	}

	renderDownResult(opts, services, p.cfg.Deploy.Target == config.TargetK8s)
	logging.Info("项目已停止", "project", p.cfg.Project, "services", len(services))
	return nil
}

// downPlan 是"这次 down 要停什么"的结论。
type downPlan struct {
	// services 是要交给引擎停止的服务名，已按停止顺序排好。
	// 只在带 `--only` 时非空——不带时整个项目一起停，顺序交给引擎。
	services []string
	// untouched 是点名了、但本项目根本没有容器可停的组件。
	untouched []untouchedTarget
	// nothingToStop 表示 `--only` 点到的组件**全都**没有容器。
	//
	// 必须与"不带 --only"区分开：那时 services 同样是空的，
	// 而空的 services 意味着**停止整个项目**——把 `down --only <一个 local 组件>`
	// 变成"把所有东西都停了"，是这个命令能造成的最糟的误伤。
	nothingToStop bool
}

// untouchedTarget 是一个被点名、却停不了的组件，以及为什么。
type untouchedTarget struct {
	ref    resolver.Ref
	reason string
}

// downTargets 决定要停哪些 service。
//
// 不带 --only 时返回空 services：整个项目一起停，顺序交给引擎
// （compose 本身就按依赖倒序停）。带 --only 时由 CLI 排出倒序。
//
// # 为什么要把"没有容器的组件"摘出去
//
// `local: true` 的组件在依赖图里、在启动顺序里、`status` 里也看得见，
// 但部署文件里**没有对应的 service**：它跑在开发者的 IDE 里（003 §4.4）。
//
// 把它的服务名递给 docker，换来的是 `no such service`，
// 而且是让**整条命令失败**。
func downTargets(p *project, only []string) (downPlan, error) {
	if len(only) == 0 {
		return downPlan{}, nil
	}

	refs, err := p.selectRefs(only)
	if err != nil {
		return downPlan{}, err
	}

	hasContainer := map[resolver.Ref]bool{}
	for _, ref := range p.containerRefs() {
		hasContainer[ref] = true
	}

	plan := downPlan{}
	var targets []resolver.Ref
	for _, ref := range refs {
		switch entry := p.entry(ref); {
		case hasContainer[ref]:
			targets = append(targets, ref)
		case entry.Local:
			plan.untouched = append(plan.untouched, untouchedTarget{ref,
				"local: true——它跑在你的 IDE 里，本项目没有它的容器"})
		default:
			// 本次级联没让它启动，因此也没生成它的 service
			plan.untouched = append(plan.untouched, untouchedTarget{ref,
				"本次不启动，没有容器可停"})
		}
	}

	plan.nothingToStop = len(targets) == 0
	// 15.21：依赖方先停，被依赖方后停。反过来的话，被依赖方先没了，
	// 依赖方在关闭过程中还在调它，日志里会留下一串没有意义的连接错误
	plan.services = servicesOf(reverse(p.inStartOrder(targets)))
	return plan, nil
}

// renderUntouched 说明点名了却没停的组件，以及为什么。
//
// 静静跳过是不行的：使用者明确点了名，什么都不说他会以为停掉了。
func renderUntouched(opts *Options, untouched []untouchedTarget) {
	if len(untouched) == 0 {
		return
	}
	opts.Printf("   以下组件本项目没有容器可停：\n")
	for _, item := range untouched {
		opts.Printf("     %s@%s —— %s\n", item.ref.ID, item.ref.Version, item.reason)
	}
}

// renderDownResult 汇报停止结果，并说清数据还在。
func renderDownResult(opts *Options, services []string, k8sTarget bool) {
	if len(services) == 0 {
		opts.Printf("✅ 已停止全部组件\n")
	} else {
		opts.Printf("✅ 已停止 %d 个组件\n", len(services))
	}

	// 004 §3.6：down 不删数据卷。这一点必须主动说——
	// 使用者最怕的就是"我停一下会不会把数据弄没了"
	if k8sTarget {
		// K8s 下基础资源由运维部署，本来就不归 CLI 管，更不会被 down 碰到
		opts.Printf("\n💡 基础资源（数据库等）由运维部署，不受 brickkit down 影响\n")
	} else {
		opts.Printf("\n💡 数据卷未删除，数据库数据仍然保留\n")
		opts.Printf("   需要彻底清理时手动执行：docker volume rm <卷名>\n")
	}
	opts.Printf("   重新启动：brickkit up\n")
}
