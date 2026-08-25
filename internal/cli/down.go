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

	eng, err := resolveEngineFor(opts, p.cfg)
	if err != nil {
		return err
	}
	if err := requireContext(ctx, opts, p.cfg, eng, contextOf(p.cfg, kubeContext)); err != nil {
		return err
	}

	opts.Printf("🛑 停止项目 %s\n", p.cfg.Project)

	// 先问一句"现在有没有东西在跑"，只为决定最后那句话怎么说。
	//
	// 从前这里判的是"生成的部署文件在不在"，据此直接返回"项目尚未启动过"——
	// 而那份文件在 .gitignore 里、文档还明说可以随时删（003 §7.1）。
	// 一次 git clean 之后，down 就成了一条什么都不做却报成功的命令。
	//
	// 探测失败**不阻断**：停止本身照做。这一步只影响措辞，
	// 拿它挡住真正的清理，等于用一个装饰性的检查换掉一次必要的操作。
	running, probed := runningCount(ctx, eng, p.engineProject())

	// 只交项目名，不交部署文件：停的是"这个项目现在跑着的一切"，
	// 而不是"生成目录里此刻写着的那些"（005 §5.9.3）。停止顺序也在引擎里。
	if err := eng.Down(ctx, engine.DownRequest{
		Project: p.engineProject(),
		// 标签值是项目名，与 Project（K8s 下是命名空间）不是一回事——
		// 引擎从前拿 Project 拼这个选择器，于是 createNamespace: false 那条路上
		// 一个资源都匹配不到，而命令照样报成功。与 up 的孤儿清理同源。
		Selector: projectSelector(p.cfg),
		Context:  contextOf(p.cfg, kubeContext),
		// 命名空间不是我们建的就不能由我们删
		DeleteNamespace: p.cfg.Deploy.ShouldCreateNamespace(),
	}); err != nil {
		return engineFailure("停止", err)
	}

	renderDownResult(opts, p.cfg.Deploy.Target == config.TargetK8s, running, probed)
	logging.Info("项目已停止", "project", p.cfg.Project, "stopped", running)
	return nil
}

// runningCount 数一下引擎里现在有几个本项目的容器 / Deployment。
//
// probed 为 false 表示没问出来（引擎报错）。那时不猜，按"有东西"处理——
// 停止照做，只是最后那句话说得笼统些。
func runningCount(ctx context.Context, eng engine.Engine, project string) (n int, probed bool) {
	statuses, err := eng.Status(ctx, project)
	if err != nil {
		return 0, false
	}
	return len(statuses), true
}

// renderDownResult 汇报停止结果，并说清数据还在。
//
// 引擎里本来就一个都没有时说实话，而不是照例打印"已停止全部组件"——
// 那句话在一个从没 up 过的项目上是句空话，而使用者真正想知道的是
// "所以我现在该干什么"。
func renderDownResult(opts *Options, k8sTarget bool, running int, probed bool) {
	if probed && running == 0 {
		opts.Printf("📋 本项目当前没有容器在跑（引擎里一个都没有）\n")
		opts.Printf("   用 brickkit up 启动\n")
		return
	}
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
