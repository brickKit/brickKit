package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/logging"
)

// newDownCommand 实现 brickkit down（004 §3.6）。
func newDownCommand(opts *Options) *cobra.Command {
	var kubeContext string

	cmd := &cobra.Command{
		Use:     "down",
		Short:   "一键停止所有组件（不删除 volume）",
		GroupID: groupLifecycle,
		Long: `停止项目（004 §3.6）。

停止顺序与启动顺序相反（依赖方先停，被依赖方后停），交给引擎处理。

只想停其中几个：在 brickkit.yaml 里给它们写 enabled: false 再 brickkit up。
生成的部署文件里没有它们，引擎会把对应的容器一并移除——效果与"只停这几个"
相同，而且配置里留下了痕迹，下次 up 不会又把它们拉起来。

重要：down 不删除 volume，数据库数据始终保留。
如需彻底清理，请手动执行 docker volume rm 或 docker compose down -v。`,
		Example: "  brickkit down",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDown(cmd.Context(), opts, kubeContext)
		},
	}

	cmd.Flags().StringVar(&kubeContext, "context", "", "kubeconfig 上下文，覆盖 deploy.context（仅 deploy.target: k8s）")
	return cmd
}

// runDown 停止整个项目。
func runDown(ctx context.Context, opts *Options, kubeContext string) error {
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

	eng, err := resolveEngineFor(opts, p.cfg)
	if err != nil {
		return err
	}
	if err := requireContext(ctx, opts, p.cfg, eng, contextOf(p.cfg, kubeContext)); err != nil {
		return err
	}

	opts.Printf("🛑 停止项目 %s\n", p.cfg.Project)

	// Services 为空 = 停整个项目，停止顺序交给引擎（compose 本身就按依赖倒序停）
	if err := eng.Down(ctx, engine.DownRequest{
		File: p.file, Project: p.engineProject(), ProjectDir: opts.WorkDir,
		Context: contextOf(p.cfg, kubeContext),
		// 命名空间不是我们建的就不能由我们删
		DeleteNamespace: p.cfg.Deploy.ShouldCreateNamespace(),
	}); err != nil {
		return engineFailure("停止", err)
	}

	renderDownResult(opts, p.cfg.Deploy.Target == config.TargetK8s)
	logging.Info("项目已停止", "project", p.cfg.Project)
	return nil
}

// renderDownResult 汇报停止结果，并说清数据还在。
func renderDownResult(opts *Options, k8sTarget bool) {
	opts.Printf("✅ 已停止全部组件\n")

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
