package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/msgid"
)

// newDownCommand 实现 brickkit down。
func newDownCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "down",
		Short:   i18n.T(msgid.CliDownStopEveryComponentInOne),
		GroupID: groupLifecycle,
		Long:    i18n.T(msgid.CliDownStopTheProjectTheStop),
		Example: "  brickkit down",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDown(cmd.Context(), opts)
		},
	}

	addDeployFileFlags(cmd, opts)
	return cmd
}

// runDown 停止整个项目。
func runDown(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 只装载三层文件，不碰安装源：down 交给引擎的只有项目名与目标，
	// 依赖图里的东西它一个都用不上（见 loadConfig 的说明）。目标、要不要删命名空间
	// 都看生效的那份部署文件（本地模式、-f 都会改变它）。
	p, err := loadConfig(opts)
	if err != nil {
		return err
	}
	proj := p.proj

	eng, err := resolveEngineFor(opts, proj)
	if err != nil {
		return err
	}
	kubeContext := proj.Deploy.Settings().Context
	if err := requireContext(ctx, opts, proj, eng, kubeContext); err != nil {
		return err
	}

	opts.Printf("%s\n", i18n.T(msgid.CliDownStoppingProject, proj.Decl.Project))

	// 先问一句"现在有没有东西在跑"，只为决定最后那句话怎么说。
	//
	// 从前这里判的是"生成的部署文件在不在"，据此直接返回"项目尚未启动过"——
	// 而那份文件在 .gitignore 里、文档还明说可以随时删。
	// 一次 git clean 之后，down 就成了一条什么都不做却报成功的命令。
	//
	// 探测失败**不阻断**：停止本身照做。这一步只影响措辞，
	// 拿它挡住真正的清理，等于用一个装饰性的检查换掉一次必要的操作。
	running, probed := runningCount(ctx, eng, p.engineProject())

	// 只交项目名，不交部署文件：停的是"这个项目现在跑着的一切"，
	// 而不是"生成目录里此刻写着的那些"。停止顺序也在引擎里。
	if err := eng.Down(ctx, engine.DownRequest{
		Project: p.engineProject(),
		// 标签值是项目名，与 Project（K8s 下是命名空间）不是一回事——
		// 引擎从前拿 Project 拼这个选择器，于是 createNamespace: false 那条路上
		// 一个资源都匹配不到，而命令照样报成功。与 up 的孤儿清理同源。
		Selector: projectSelector(proj),
		Context:  kubeContext,
		// 命名空间不是我们建的就不能由我们删
		DeleteNamespace: proj.Deploy.ShouldCreateNamespace(),
	}); err != nil {
		return engineFailure(i18n.T(msgid.CliDownStop), err)
	}

	renderDownResult(opts, proj.Deploy.Target == deployfile.TargetK8s, eng.Name(), running, probed)
	renderLocalModeSessionHint(opts, proj)
	logging.Info(i18n.T(msgid.LogProjectStopped), "project", proj.Decl.Project, "stopped", running)
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
func renderDownResult(opts *Options, k8sTarget bool, engineName string, running int, probed bool) {
	if probed && running == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliDownThisProjectHasNoContainers))
		opts.Printf("%s\n", i18n.T(msgid.CliDownStartItWithBrickkitUp))
		return
	}
	opts.Printf("%s\n", i18n.T(msgid.CliDownAllComponentsStopped))

	// down 不删数据卷。这一点必须主动说——
	// 使用者最怕的就是"我停一下会不会把数据弄没了"
	if k8sTarget {
		// K8s 下基础资源由运维部署，本来就不归 CLI 管，更不会被 down 碰到
		opts.Printf("\n%s\n", i18n.T(msgid.CliDownBaseResourcesDatabasesAndSo))
	} else {
		opts.Printf("\n%s\n", i18n.T(msgid.CliDownDataVolumesWereNotDeleted))
		// engineName 得跟 up 起这批容器用的引擎一致——docker volume rm
		// 找不到 podman 建的卷，反过来也一样，两个引擎的卷各自存在自己的存储里。
		opts.Printf("%s\n", i18n.T(msgid.CliDownForAFullCleanupRun, engineName))
	}
	opts.Printf("%s\n", i18n.T(msgid.CliDownStartAgainWithBrickkitUp))
}
