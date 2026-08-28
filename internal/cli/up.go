package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// composeFileName 是生成的部署文件名（004 §3.5 输出样例）。
const composeFileName = "docker-compose.yaml"

// newUpCommand 实现 brickkit up（004 §3.5）。
func newUpCommand(opts *Options) *cobra.Command {
	var (
		dryRun      bool
		kubeContext string
	)

	cmd := &cobra.Command{
		Use:     "up",
		Short:   "生成部署文件、执行迁移并一键启动所有组件",
		GroupID: groupLifecycle,
		Long: `一键启动项目（004 §3.5）。

行为流程：
  1. 读取 brickkit.yaml 与所有组件 Manifest
  2. 启停判定（跟着上层走：顶层没写 enabled 就跑，下层跟上层，003 §4.3）
  3. 检查强依赖（缺失报错）与弱依赖（缺失警告，且完全不注入环境变量）
  4. 拓扑排序得出启动顺序
  5. 生成 docker-compose.yaml，注入环境变量、合并资源配额
  6. 有 local: true 组件时生成 local-debug.<版本化服务名>.env
  7. 检测镜像拉取权限（未授权时提示 docker login）
  8. 调用底层引擎启动；数据库迁移由一次性容器执行，失败则阻断主服务

版本号改了就是升级：CLI 自动拉新版本 Manifest 与产物、做兼容性检查（004 §3.5.1）。`,
		Example: `  brickkit up
  brickkit up --dry-run                    只生成文件，不启动
  brickkit up --config brickkit.prod.yaml  使用指定配置文件`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUp(cmd.Context(), opts, upOptions{
				dryRun: dryRun, kubeContext: kubeContext,
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只生成部署文件，不启动（升级时额外输出变更摘要）")
	cmd.Flags().StringVar(&kubeContext, "context", "", "kubeconfig 上下文，覆盖 deploy.context（仅 deploy.target: k8s）")
	return cmd
}

// upPlan 是"这次 up 要做什么"的全部结论。
//
// 生成与启动共用同一份计划：--dry-run 与真启动之间唯一的差别应当是
// "有没有真的调引擎"，而不是两条各自算一遍、可能算出不同结果的路径。
type upPlan struct {
	layout    config.Layout
	cfg       *config.Config
	graph     *resolver.Graph
	states    *cascade.Result
	generated *compose.Result
	// k8s 是 deploy.target: k8s 时的生成结果（与 generated 互斥）。
	k8s *k8s.Result
	// kubeContext 是本次钉住的 kubeconfig 上下文（可能来自 --context）。
	kubeContext string
	// services 是本次要交给引擎启动的 service（不含 local 组件与迁移容器）。
	services []string
	// migrations 是本次会执行的迁移，供输出（15.25）。
	migrations []migrationInfo
	// images 是要检查拉取权限的镜像（15.19）。
	images []imageInfo
	// upgrades 是本次检测到的版本变更（004 §3.5.1）。
	upgrades []upgradeInfo
	// done 为 true 表示"没什么可启动的"，已经把话说清楚了。
	done bool
}

type migrationInfo struct {
	component string
	command   string
}

type imageInfo struct {
	component string
	image     string
}

// upOptions 是 up 的命令行选项。
type upOptions struct {
	dryRun bool
	// kubeContext 是 --context 的值，覆盖 deploy.context。
	kubeContext string
}

// runUp 执行 brickkit up。
func runUp(ctx context.Context, opts *Options, flags upOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	plan, err := buildUpPlan(ctx, opts, flags)
	if err != nil || plan.done {
		return err
	}
	if plan.k8s != nil {
		return upK8s(ctx, opts, flags, plan)
	}

	path, err := writeGenerated(plan.layout, plan.generated.YAML)
	if err != nil {
		return err
	}
	if err := writeLocalEnvFiles(opts, plan.layout, plan.generated.LocalEnvFiles); err != nil {
		return err
	}
	opts.Printf("📄 已生成：%s\n", displayPath(opts.WorkDir, path))
	renderResourceRequirements(opts, plan.generated.Resources)
	// 在 --dry-run 的分岔**之前**："这次会动哪些库"正是 dry-run 最该回答的问题
	// 之一，而它与升不升级无关。从前它在分岔之后，于是 dry-run 里一个字都没有，
	// 唯一提到迁移的地方是升级摘要里那一行——还得先检测到升级才会出现
	renderMigrations(opts, plan.migrations)

	if flags.dryRun {
		renderUpgradeSummary(opts, plan)
		opts.Printf("\n💡 --dry-run 只生成文件，未启动任何组件\n")
		opts.Printf("   查看：cat %s\n", displayPath(opts.WorkDir, path))
		logging.Info("部署文件已生成", "path", path)
		return nil
	}

	eng, err := resolveEngine(opts)
	if err != nil {
		return err
	}
	if err := checkImages(ctx, opts, eng, plan.images); err != nil {
		return err
	}

	return start(ctx, opts, eng, plan, path, projectSelector(plan.cfg))
}

// buildUpPlan 从配置一路算到"要启动哪些 service"。
func buildUpPlan(ctx context.Context, opts *Options, flags upOptions) (*upPlan, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return nil, err
	}

	plan := &upPlan{layout: layout, cfg: cfg, kubeContext: contextOf(cfg, flags.kubeContext)}
	if len(cfg.Components) == 0 {
		opts.Printf("📋 当前项目没有组件\n")
		// init 的骨架已经把 ./components 配成了本地安装源，所以 --local 是最短的一条路。
		// 两条都给：有的人手上已经有组件源码，有的人要从市场装。
		opts.Printf("   用 brickkit add --local 把 %s/ 下的组件全加进来\n", config.DirComponents)
		opts.Printf("   或 brickkit add <组件ID> 从安装源添加\n")
		plan.done = true
		return plan, nil
	}

	opts.Printf("🚀 启动项目 %s（deploy.target: %s）\n", cfg.Project, cfg.Deploy.Target)
	warnTargetOnlyFields(opts, cfg)

	// 先确认"要部到哪"，再做任何生成与拉取：部错集群是不可逆的，
	// 而且这时连一份生成物都还没落盘
	if cfg.Deploy.Target == config.TargetK8s && !flags.dryRun {
		eng, err := resolveEngineFor(opts, cfg)
		if err != nil {
			return nil, err
		}
		if err := requireContext(ctx, opts, cfg, eng, contextOf(cfg, flags.kubeContext)); err != nil {
			return nil, err
		}
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	// 版本号变了就报一句（004 §3.5.1）。检测只读配置与本地缓存，不碰网络；
	// 差异描述与产物下载要等依赖图建好，见 describeUpgrades
	plan.upgrades = detectUpgrades(layout, cfg)
	renderUpgradeBanner(opts, plan.upgrades)

	plan.graph, err = resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	plan.states, err = cascade.Compute(cfg, plan.graph)
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, plan.graph.Warnings)
	renderStates(opts, plan.states)
	// "一个都不启动"紧跟在状态表后面：它解释的就是那张全 ⬜ 的表，
	// 中间隔着几条资源警告的话，使用者读到的顺序就成了"先看一堆无关的警告"
	if plan.states.Empty() {
		renderNothingRunning(opts, plan.states)
		renderSyncHint(opts, layout, plan.states)
		plan.done = true
		return plan, nil
	}
	renderSyncHint(opts, layout, plan.states)
	warnDanglingBindings(opts, cfg)
	warnHardcodedPasswords(opts, cfg)
	warnConfigSecrets(opts, cfg)

	// 资源绑定必须在生成之前查（006 §4.4、011 §5.3）：没绑定就一个
	// DATABASE_* 都注不进去，而那份 compose 看上去完全正常——
	// 组件要到运行时才炸成"连不上库"，一句把配置遗漏指向别处的错误。
	//
	// `--dry-run` 时降级成警告：那条命令的语义是"告诉我会发生什么"，
	// 拿它阻断的话，一个还没配资源的项目连"看看会生成什么"都做不到
	// （试用指南 04 讲 enabled 三态时用的正是这条命令，那时资源还没登场）。
	if problem := resolver.CheckRunningResourceBindings(
		cfg, plan.graph, plan.states.Running()); problem != nil {
		if !flags.dryRun {
			return nil, problem
		}
		renderWarnings(opts, []*clierr.Error{dryRunResourceWarning(problem)})
	}

	order, err := resolver.Order(plan.graph.Subgraph(plan.states.Running()))
	if err != nil {
		return nil, err
	}
	renderOrder(opts, order, plan.graph)

	env, err := inject.Build(cfg, plan.graph, plan.states)
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, env.Warnings)

	if err := plan.generate(opts, env); err != nil {
		return nil, err
	}
	plan.collectTargets(order)
	// 放在生成之后：这一步只补摘要用的差异描述与新版本产物，
	// 它取不到东西也不该拦住已经算好的这份计划（004 §10.1）
	describeUpgrades(ctx, opts, layout, client, plan.graph, plan.upgrades)
	return plan, nil
}

// renderNothingRunning 解释"一个组件都不启动"，并说清楚该去改哪一行。
//
// 把顶层逐个列出来，而不是替使用者总结成一句话。顶层有两种死法：自己被
// enabled: false 关掉，或者它的强依赖被关掉、它跟着倒下——两者指向配置里
// **不同的**行。每个顶层自己的理由已经写明是哪一种，照抄比概括可靠。
//
// 从前这里只要看到**任何**组件是 StateDisabled 就断言"顶层都被关掉了"。
// 关掉一个底层组件时那句话是错的：顶层根本没写过 enabled: false，
// 照着去找只会扑空，还把人从上面那张表已经写对的答案上引开
// （表里写的是"不启动（强依赖 X 不启动）"）。
//
// 还有一种成因是**图里根本没有顶层**：每个组件都被别的组件依赖着
// （只可能是弱依赖成环，强依赖成环在解析阶段就报错了）。那时"跟着上层走"
// 解释不了任何事——上层是谁？没有上层。
func renderNothingRunning(opts *Options, states *cascade.Result) {
	opts.Printf("📋 本次没有组件会启动\n")

	tops := states.TopLevel()
	if len(tops) == 0 {
		opts.Printf("   没有找到顶层组件——每个组件都被别的组件依赖着（依赖成了环）\n")
		opts.Printf("   给你想跑的那个写 enabled: true（003 §4.3）\n")
		return
	}

	opts.Printf("   顶层组件（没有别的组件依赖它们）这次都不跑：\n")
	for _, c := range tops {
		opts.Printf("      %s  %s\n", c.Ref, c.Reason)
	}
	opts.Printf("   %s\n", nothingRunningHint(tops))
}

// nothingRunningHint 指出最短的一条出路。
//
// 只要有一个顶层是被显式关掉的，那就是最省事的一行——删掉它，它下面整条链
// 跟着回来。一个都没有时，顶层全是被强依赖拖下水的：那时配置里压根没有
// 可删的 enabled: false，得顺着上面每行的理由往下找真正被关掉的那个。
func nothingRunningHint(tops []cascade.Component) string {
	for _, c := range tops {
		if c.State == cascade.StateDisabled {
			return "移除其中一个的 enabled: false，它下面那条链会跟着回来（003 §4.3）"
		}
	}
	return "顶层自己都没被关掉——要放开的是上面那行理由里点名的组件（003 §4.3）"
}

// renderSyncHint 提醒可以把不启动的组件源码收起来。
//
// sync 不由 up 自动执行（012 §2.17：up 管运行时，sync 管源码目录），
// 但"忘了 sync"是最常见的落差——改完 enabled 跑了 up，源码目录还是老样子。
// 只在真有源码可收时才提，否则每次 up 都多一行噪音。
func renderSyncHint(opts *Options, layout config.Layout, states *cascade.Result) {
	n := 0
	for _, c := range states.Components {
		if c.State != cascade.StateRunning && workspace.Exists(layout, c.Ref.ID) {
			n++
		}
	}
	if n == 0 {
		return
	}
	opts.Printf("💡 有 %d 个组件本次不启动，brickkit sync 可以把它们的源码收进 %s/\n",
		n, workspace.DisplayArchivedRoot())
}

// dryRunResourceWarning 把"资源未绑定"降级成 --dry-run 下的警告。
//
// 换掉标题与建议里"已阻断"那层意思，其余明细原样保留——
// 使用者要看的是"哪个组件缺哪个资源"，那部分两种模式下完全一样。
func dryRunResourceWarning(problem *clierr.Error) *clierr.Error {
	w := clierr.Warn(problem.Code, "警告：资源依赖未满足（--dry-run 不阻断）")
	w.Details = problem.Details
	return w.WithHint(
		"生成的部署文件里**不会有**这些组件的资源连接变量（DATABASE_* 等）",
		"在 brickkit.yaml → resources 中声明并绑定后再 up；不加 --dry-run 时这里会直接阻断",
	)
}

// generate 按部署目标渲染部署文件（005 §5）。
//
// 两种目标共用到这一步为止的**全部**结论（依赖图、级联、注入），
// 只有渲染方式不同——规则写在渲染器里迟早会分叉（D138）。
func (p *upPlan) generate(opts *Options, env *inject.Result) error {
	if p.cfg.Deploy.Target == config.TargetK8s {
		result, err := k8s.Generate(p.cfg, p.graph, p.states, env, k8s.Options{
			Now:    opts.Now,
			Lookup: envLookup(opts.WorkDir),
		})
		if err != nil {
			return err
		}
		p.k8s = result
		renderWarnings(opts, result.Warnings)
		return nil
	}

	result, err := compose.Generate(p.cfg, p.graph, p.states, env, compose.Options{
		Now:    opts.Now,
		Engine: engineName(opts),
		// 只作用于 local-debug 文件：IDE 不做变量替换（见 compose.Options.Lookup）
		Lookup: envLookup(opts.WorkDir),
	})
	if err != nil {
		return err
	}
	p.generated = result
	renderWarnings(opts, result.Warnings)
	return nil
}

// collectTargets 按启动顺序列出要交给引擎的 service、要检查的镜像、会跑的迁移。
//
// local: true 的组件全部跳过：它没有容器，镜像也不必检查
// （使用者是在 IDE 里跑源码，根本不需要那个镜像）。
func (p *upPlan) collectTargets(order *resolver.Plan) {
	local := map[resolver.Ref]bool{}
	for _, c := range p.cfg.Components {
		if c.Local {
			local[resolver.Ref{ID: c.ID, Version: c.Version}] = true
		}
	}

	for _, step := range order.Steps {
		ref := step.Ref
		if local[ref] {
			continue
		}
		node := p.graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		p.services = append(p.services, manifest.ServiceName(ref.ID, ref.Version))
		p.images = append(p.images, imageInfo{
			component: ref.ID + "@" + ref.Version,
			image:     node.Manifest.Deployment.Image,
		})
		if node.Manifest.Migration != nil {
			p.migrations = append(p.migrations, migrationInfo{
				component: ref.ID + "@" + ref.Version,
				command:   strings.Join(node.Manifest.Migration.Command, " "),
			})
		}
	}
}

// start 调引擎把项目跑起来，然后如实汇报每个 service 的状态。
//
// pruneSelector 与 K8s 侧同源，见 projectSelector。Docker 这边只用它的
// "空 / 非空"决定带不带 `--remove-orphans`，值本身用不上。
func start(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan,
	file, pruneSelector string,
) error {
	project := engine.ProjectName(plan.cfg.Project)

	opts.Printf("\n🐳 正在启动（%s）...\n", eng.Name())
	if err := eng.Up(ctx, engine.UpRequest{
		File: file, Project: project, ProjectDir: opts.WorkDir, Services: plan.services,
		PruneSelector: pruneSelector,
	}); err != nil {
		return engineFailure("启动", err)
	}

	statuses, err := eng.Status(ctx, project)
	if err != nil {
		// 起是起了，只是问不到状态：不该因此判定失败
		opts.Printf("⚠️ 无法读取容器状态：%s\n", clierr.As(err).Message)
		opts.Printf("   用 brickkit status 再看一次\n")
		return nil
	}
	return reportStarted(opts, plan, statuses)
}

// reportStarted 汇报启动结果，并在有组件没起来时给出非零退出码。
func reportStarted(
	opts *Options, plan *upPlan, statuses []engine.Status,
) error {
	byService := map[string]engine.Status{}
	for _, s := range statuses {
		byService[s.Service] = s
	}

	var failed []string
	for _, service := range plan.services {
		status, ok := byService[service]
		switch {
		case !ok:
			failed = append(failed, service+"  未创建")
		case status.Running():
			opts.Printf("   %-28s %s\n", service, describeStatus(status))
		default:
			failed = append(failed, service+"  "+describeStatus(status))
		}
	}

	if len(failed) > 0 {
		err := clierr.New(clierr.CodeEngineFailed, "错误：部分组件没有正常启动")
		for _, item := range failed {
			err = err.WithDetail("组件", item)
		}
		if plan.k8s != nil {
			return err.WithHint(
				"看日志定位："+logsCommand(engine.K8s, plan.k8s.Namespace, "<服务名>"),
				"看事件：kubectl describe deployment/<服务名> -n "+plan.k8s.Namespace,
			)
		}
		return err.WithHint(
			"看日志定位："+logsCommand(engineName(opts),
				engine.ProjectName(plan.cfg.Project), "<服务名>"),
			"迁移失败会让主服务停在 Created，先看该组件的 -migration 容器",
		)
	}

	opts.Printf("✅ 全部组件已启动（%d 个）\n", len(plan.services))
	renderNextSteps(opts, plan)
	logging.Info("项目已启动", "project", plan.cfg.Project, "services", len(plan.services))
	return nil
}

// engineFailure 把引擎的失败变成一条能看的错误。
//
// 引擎已经给出结构化错误时原样透传——它比这里更清楚发生了什么
// （P18 的教训：自作主张换掉下层的说法，会把人引向错误的方向）。
// 只有裸 error 才在这里兜住：不然它会被顶层当成"命令用法不正确"，
// 明明是 docker 挂了，却让使用者去查自己的命令怎么写。
func engineFailure(action string, err error) error {
	if e, ok := clierr.Structured(err); ok {
		return e
	}
	return clierr.Newf(clierr.CodeEngineFailed, "错误：%s失败", action).
		WithDetail("原因", err.Error()).
		WithHint("上面一行是容器引擎的原始输出，通常已经说明了原因").
		WithCause(err)
}

// describeStatus 把引擎的状态说成人话。
func describeStatus(s engine.Status) string {
	switch {
	case s.State == "running" && s.Health != "":
		return "running（" + s.Health + "）"
	case s.State == "exited":
		return "exited（退出码 " + itoa(s.ExitCode) + "）"
	default:
		return s.State
	}
}

// renderNextSteps 给出启动之后的常用动作。
func renderNextSteps(opts *Options, plan *upPlan) {
	opts.Printf("\n💡 查看状态：brickkit status\n")

	if plan.k8s != nil {
		opts.Printf("   查看日志：%s\n",
			logsCommand(engine.K8s, plan.k8s.Namespace, ""))
		opts.Printf("   查看 Pod：kubectl get pods -n %s\n", plan.k8s.Namespace)
		return
	}

	opts.Printf("   查看日志：%s -f\n",
		logsCommand(engineName(opts), engine.ProjectName(plan.cfg.Project), ""))
	for _, env := range plan.generated.LocalEnvFiles {
		opts.Printf("   本地调试：在 IDE 中加载 %s 启动 %s\n",
			filepath.Join(".brickkit", "generated", env.Name), env.Ref.ID)
	}
}

// renderMigrations 说明本次会跑哪些迁移（15.25）。
//
// 迁移由部署文件里的一次性容器执行（002 §8.3），CLI 不自己跑；
// 但使用者需要知道"这次会动哪些库"，出问题时也才知道去看哪个容器。
func renderMigrations(opts *Options, migrations []migrationInfo) {
	if len(migrations) == 0 {
		return
	}
	opts.Printf("\n🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：\n")
	for _, m := range migrations {
		opts.Printf("   %s  %s\n", m.component, m.command)
	}
}

// warnDanglingBindings 提醒"这条资源绑定指向一个配置里没有的组件"。
//
// 只警告不阻断（理由见 config.Config.DanglingBindings）：它的唯一后果是
// 那条绑定不生效。但必须说一句——最常见的成因是使用者手工删掉了组件条目、
// 却漏了绑定，而他多半以为那个组件"还配着库"。
func warnDanglingBindings(opts *Options, cfg *config.Config) {
	dangling := cfg.DanglingBindings()
	if len(dangling) == 0 {
		return
	}

	err := clierr.Warn(clierr.CodeConfigInvalid, "有资源绑定指向 components 里不存在的组件")
	for _, d := range dangling {
		err = err.WithDetail("资源 "+d.ResourceID, "绑定了 "+d.ComponentID+"（该组件不在 components 中）")
	}
	renderWarnings(opts, []*clierr.Error{err.
		WithDetail("影响", "这条绑定不会生效，也不会注入任何连接变量；其余组件不受影响").
		WithHint(
			"不再需要它就从 resources[].bindings 里删掉这一条",
			"组件是误删的话，brickkit add "+dangling[0].ComponentID+" 加回来",
		)})
}

// checkImageConcurrency 是同时进行的镜像检查数上限。
//
// 有上限而不是全放出去：这些请求打到的是**同一个 registry**，
// 几百个并发只会撞上限流，那时候不但不快，还会换来一堆 429
// 让人误以为是凭据出了问题。8 足够把串行的时间摊掉一个数量级。
const checkImageConcurrency = 8

// checkImages 检测镜像拉取权限（15.19、004 §10.2）。
//
// 放在启动之前：镜像取不到还硬启，只会得到一堆 ImagePullBackOff，
// 而真正的原因（没登录）埋在引擎的输出里。
//
// # 为什么要并发（36.1）
//
// 本地没有该镜像时，`CheckImage` 会走一次 **registry 往返**。
// 原来这里是串行的，于是 50 个组件就是 50 次串行网络请求：
// 按健康网络 0.3–0.5 秒一次算已经是 15–25 秒，而计划给整个 `up`
// 的预算是 30 秒——第一个容器都还没开始启动。实测这台机器上
// registry 路径慢的时候单次要 12 秒，50 个就是十分钟。
//
// 这是整个 up 链路上唯一不随组件数伸缩的地方（相比之下解析 100 个依赖
// 只要 42µs），而这些检查彼此完全独立，串行没有任何理由。
//
// # 为什么不在第一个失败时就掐断
//
// 掐断能更早报错，但会丢掉两样东西：**哪个**错误被报出来变得不确定
// （同一份配置连跑两次给出不同的错误，使用者会以为问题在飘），
// 以及"还有几个也拉不到"这个信息。让它们跑完，就能一次说清
// 要修几个——总比修一个、重跑、再冒出一个强。
func checkImages(ctx context.Context, opts *Options, eng engine.Engine, images []imageInfo) error {
	if len(images) == 0 {
		return nil
	}

	opts.Printf("\n🔍 检测镜像拉取权限...")

	// 按下标存放结果，取错误时才能按**输入顺序**来，与并发完成的顺序无关
	failures := make([]error, len(images))
	sem := make(chan struct{}, checkImageConcurrency)
	var wg sync.WaitGroup

	for i, item := range images {
		wg.Add(1)
		go func(i int, item imageInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := eng.CheckImage(ctx, item.image); err != nil {
				failures[i] = clierr.As(err).WithDetail("组件", item.component)
			}
		}(i, item)
	}
	wg.Wait()

	first, total := -1, 0
	for i, err := range failures {
		if err == nil {
			continue
		}
		total++
		if first < 0 {
			first = i
		}
	}
	if first < 0 {
		opts.Printf(" ✅ 全部通过\n")
		return nil
	}

	opts.Printf(" ❌\n")
	err := clierr.As(failures[first])
	if total > 1 {
		err = err.WithDetail("另外还有",
			fmt.Sprintf("%d 个组件的镜像也取不到，修完这个再跑一次会看到下一个", total-1))
	}
	return err
}

// resolveEngine 返回要用的容器引擎：注入优先，否则自动检测（005 §7.3）。
func resolveEngine(opts *Options) (engine.Engine, error) {
	if opts.Engine != nil {
		return opts.Engine, nil
	}
	if forced := os.Getenv("BRICKKIT_ENGINE"); strings.TrimSpace(forced) != "" {
		if strings.EqualFold(strings.TrimSpace(forced), engine.Docker) {
			return engine.NewDocker(), nil
		}
	}
	return engine.Detect()
}

// engineName 是生成部署文件时记录的引擎名。
//
// 目前只有 Docker 一种（Podman 见 005 §7）。保留这个函数是因为
// 生成文件与"引擎可不可用"是两回事：--dry-run 在没装 Docker 的机器上也该能跑。
func engineName(opts *Options) string {
	if opts.Engine != nil {
		return opts.Engine.Name()
	}
	return compose.EngineDocker
}

// writeGenerated 把部署文件写进 .brickkit/generated/。
func writeGenerated(layout config.Layout, content []byte) (string, error) {
	dir := layout.GeneratedDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", clierr.New(clierr.CodeInternal, "错误：创建生成目录失败").
			WithDetail("路径", dir).
			WithCause(err)
	}

	path := filepath.Join(dir, composeFileName)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", clierr.New(clierr.CodeInternal, "错误：写入部署文件失败").
			WithDetail("路径", path).
			WithCause(err)
	}
	return path, nil
}

// writeLocalEnvFiles 写出 local: true 组件的调试环境变量文件（005 §4.9）。
func writeLocalEnvFiles(opts *Options, layout config.Layout, files []compose.LocalEnvFile) error {
	if len(files) == 0 {
		return nil
	}

	opts.Printf("\n🔧 本地调试（local: true）：\n")
	for _, file := range files {
		path := filepath.Join(layout.GeneratedDir(), file.Name)
		if err := os.WriteFile(path, file.Content, 0o600); err != nil {
			return clierr.New(clierr.CodeInternal, "错误：写入本地调试环境变量文件失败").
				WithDetail("路径", path).
				WithCause(err)
		}

		relative := displayPath(opts.WorkDir, path)
		opts.Printf("   %s@%s\n", file.Ref.ID, file.Ref.Version)
		opts.Printf("      不生成容器；请在 IDE 里启动它，监听 localhost:%d\n", file.Port)
		opts.Printf("      环境变量：%s\n", relative)
		opts.Printf("      VS Code：launch.json 里配 \"envFile\": \"${workspaceFolder}/%s\"\n", relative)
	}
	return nil
}

// devResourcesCompose 是仓库里那份开箱即用的开发资源栈（postgres + redis）。
//
// 它是一份**手写**的 compose 文件，不是生成的——与 deploy/market/ 同一个做法。
// 平台不部署基础资源，但"本地想快速起一套"是真实需求，给一条能直接粘的命令
// 比让每个人自己去查 postgres 镜像怎么配要便宜得多。
const devResourcesCompose = "deploy/dev-resources/docker-compose.yaml"

// renderResourceRequirements 告诉使用者哪些基础资源必须先跑起来。
//
// 006 §9.1：平台不部署基础资源，也不建库。但"不代为部署"不等于"不说清楚"——
// 不列出来的话，组件会在启动或迁移时抛出 `connection refused` 或
// `database "xxx" does not exist`，一句把**环境没准备好**指向**平台或组件**的错误。
//
// 每次 up 都打印，不是只在出错时：建库是一次性动作，而"资源得先跑着"
// 是每次启动都要满足的前提。
func renderResourceRequirements(opts *Options, requirements []deploy.ResourceRequirement) {
	if len(requirements) == 0 {
		return
	}

	opts.Printf("\n📌 以下基础资源需要先跑起来（平台不代为部署，见 006 §9.1）：\n")
	needDatabase := false
	for _, r := range requirements {
		opts.Printf("   %-12s %-12s %s:%d  供 %s 使用\n",
			r.ID, r.Engine, r.Host, r.Port, joinComponents(r.Components))
		for _, db := range r.Databases {
			needDatabase = true
			opts.Printf("      需要库 %s（供 %s 使用）：%s;\n",
				db.Name, joinComponents(db.Components), db.CreateSQL)
		}
	}
	if needDatabase {
		opts.Printf("   库也要预先建好；已经建过就无需再执行，建库是一次性操作\n")
	}
	opts.Printf("   本地开发想快速起一套：docker compose -f %s up -d\n", devResourcesCompose)
}

func joinComponents(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for _, item := range items[1:] {
		out += "、" + item
	}
	return out
}

// displayPath 把绝对路径显示成相对项目根目录的形式。
func displayPath(workDir, path string) string {
	if rel, err := filepath.Rel(workDir, path); err == nil {
		return rel
	}
	return path
}
