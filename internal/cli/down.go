package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/logging"
)

// newDownCommand 实现 brickkit down（004 §3.6）。
func newDownCommand(opts *Options) *cobra.Command {
	var only []string

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
			return runDown(cmd.Context(), opts, only)
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil, "只停止指定组件，逗号分隔，支持 @版本")
	return cmd
}

// runDown 停止项目或其中的部分组件。
func runDown(ctx context.Context, opts *Options, only []string) error {
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

	services, err := downTargets(p, only)
	if err != nil {
		return err
	}

	eng, err := resolveEngine(opts)
	if err != nil {
		return err
	}

	opts.Printf("🛑 停止项目 %s\n", p.cfg.Project)
	if len(services) > 0 {
		opts.Printf("   停止顺序（与启动顺序相反）：%s\n", joinComponents(services))
	}

	if err := eng.Down(ctx, engine.DownRequest{
		File: p.file, Project: p.engineProject(), Services: services,
	}); err != nil {
		return engineFailure("停止", err)
	}

	renderDownResult(opts, services)
	logging.Info("项目已停止", "project", p.cfg.Project, "services", len(services))
	return nil
}

// downTargets 决定要停哪些 service。
//
// 不带 --only 时返回空：整个项目一起停，顺序交给引擎
// （compose 本身就按依赖倒序停）。带 --only 时由 CLI 排出倒序。
func downTargets(p *project, only []string) ([]string, error) {
	if len(only) == 0 {
		return nil, nil
	}

	refs, err := p.selectRefs(only)
	if err != nil {
		return nil, err
	}
	// 15.21：依赖方先停，被依赖方后停。反过来的话，被依赖方先没了，
	// 依赖方在关闭过程中还在调它，日志里会留下一串没有意义的连接错误
	return servicesOf(reverse(p.inStartOrder(refs))), nil
}

// renderDownResult 汇报停止结果，并说清数据还在。
func renderDownResult(opts *Options, services []string) {
	if len(services) == 0 {
		opts.Printf("✅ 已停止全部组件\n")
	} else {
		opts.Printf("✅ 已停止 %d 个组件\n", len(services))
	}

	// 004 §3.6：down 不删数据卷。这一点必须主动说——
	// 使用者最怕的就是"我停一下会不会把数据弄没了"
	opts.Printf("\n💡 数据卷未删除，数据库数据仍然保留\n")
	opts.Printf("   需要彻底清理时手动执行：docker volume rm <卷名>\n")
	opts.Printf("   重新启动：brickkit up\n")
}
