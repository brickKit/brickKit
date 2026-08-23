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
)

// composeFileName 是生成的部署文件名（004 §3.5 输出样例）。
const composeFileName = "docker-compose.yaml"

// newUpCommand 实现 brickkit up（004 §3.5）。
func newUpCommand(opts *Options) *cobra.Command {
	var (
		only           []string
		dryRun         bool
		checkResources bool
		kubeContext    string
	)

	cmd := &cobra.Command{
		Use:     "up",
		Short:   "生成部署文件、执行迁移并一键启动所有组件",
		GroupID: groupLifecycle,
		Long: `一键启动项目（004 §3.5）。

行为流程：
  1. 读取 brickkit.yaml 与所有组件 Manifest
  2. 级联禁用计算（enabled 三种状态：钉住 / 默认开启可被级联 / 显式关闭）
  3. 检查强依赖（缺失报错）与弱依赖（缺失警告，且完全不注入环境变量）
  4. 拓扑排序得出启动顺序
  5. 生成 docker-compose.yaml，注入环境变量、合并资源配额
  6. 有 local: true 组件时生成 local-debug.<版本化服务名>.env
  7. 检测镜像拉取权限（未授权时提示 docker login）
  8. 调用底层引擎启动；数据库迁移由一次性容器执行，失败则阻断主服务

版本号改了就是升级：CLI 自动拉新版本 Manifest 与产物、做兼容性检查（004 §3.5.1）。`,
		Example: `  brickkit up
  brickkit up --only people/basic,department/tree   只启动指定组件及其依赖
  brickkit up --only people/basic@1.0.0             只启动指定版本
  brickkit up --dry-run                             只生成文件，不启动
  brickkit up --check-resources                     启动前检查资源可达性与端口占用
  brickkit up --config brickkit.prod.yaml           使用指定配置文件`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUp(cmd.Context(), opts, upOptions{
				only: only, dryRun: dryRun, checkResources: checkResources,
				kubeContext: kubeContext,
			})
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil, "只启动指定组件及其依赖，逗号分隔，支持 @版本")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只生成部署文件，不启动（升级时额外输出变更摘要）")
	cmd.Flags().BoolVar(&checkResources, "check-resources", false, "启动前检查基础资源可达性（不可达时警告但不阻断）")
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
	// services 是本次要交给引擎启动的 service（不含 local 组件、external 组件与迁移容器）。
	services []string
	// external 是 external 组件 → 部署它的项目名（P39），用于输出时如实标注。
	external map[resolver.Ref]string
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
	only           []string
	dryRun         bool
	checkResources bool
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
	renderDatabaseRequirements(opts, plan.generated.Databases)

	if flags.checkResources {
		// --dry-run 时不要求引擎可用：那台机器上也许根本没装 docker，
		// 而"看看会发生什么"本来就不该依赖引擎
		var eng engine.Engine
		if !flags.dryRun {
			if eng, err = resolveEngine(opts); err != nil {
				return err
			}
		}
		checkResources(ctx, opts, eng, plan)
	}

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
	renderMigrations(opts, plan.migrations)

	return start(ctx, opts, eng, plan, path)
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
	warnK8sOnlyFields(opts, cfg)

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

	// 版本号与缓存里的对不上就是升级：拉新版本 Manifest 与产物、做兼容性检查
	// （004 §3.5.1，回填 P10 / P15）。要在解析依赖图之前做完
	if plan.upgrades, err = handleUpgrades(ctx, opts, layout, cfg, client); err != nil {
		return nil, err
	}

	plan.graph, err = resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	plan.states, err = cascade.Compute(cfg, plan.graph)
	if err != nil {
		return nil, err
	}
	if len(flags.only) > 0 {
		if plan.states, err = restrictToOnly(opts, plan, flags.only); err != nil {
			return nil, err
		}
	}

	renderWarnings(opts, plan.graph.Warnings)
	renderStates(opts, plan.states)
	renderExternals(opts, cfg)
	warnHardcodedPasswords(opts, cfg)
	warnConfigSecrets(opts, cfg)

	if plan.states.Empty() {
		opts.Printf("📋 本次没有组件会启动\n")
		opts.Printf("   把需要的组件改成 enabled: true，或移除 enabled: false\n")
		plan.done = true
		return plan, nil
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
	return plan, nil
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
	external := map[resolver.Ref]string{}
	for _, c := range p.cfg.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		if c.Local {
			local[ref] = true
		}
		if c.IsExternal() {
			external[ref] = c.External.Project
		}
	}
	p.external = external

	for _, step := range order.Steps {
		ref := step.Ref
		if local[ref] {
			continue
		}
		if _, isExternal := external[ref]; isExternal {
			// P39：它由别的项目部署，本项目既没有为它生成服务，
			// 也不该点名启动它——点了 docker 会报 `no such service`
			// 并让**整个 up 失败**，连本项目自己的组件都起不来（真跑到过）。
			// 镜像预检同理：镜像由对方项目负责拉，在这边查只会误报别人的问题。
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
func start(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan, file string,
) error {
	project := engine.ProjectName(plan.cfg.Project)

	opts.Printf("\n🐳 正在启动（%s）...\n", eng.Name())
	if err := eng.Up(ctx, engine.UpRequest{
		File: file, Project: project, ProjectDir: opts.WorkDir, Services: plan.services,
	}); err != nil {
		return engineFailure("启动", err)
	}

	statuses, err := eng.Status(ctx, file, project)
	if err != nil {
		// 起是起了，只是问不到状态：不该因此判定失败
		opts.Printf("⚠️ 无法读取容器状态：%s\n", clierr.As(err).Message)
		opts.Printf("   用 brickkit status 再看一次\n")
		return nil
	}
	return reportStarted(opts, plan, statuses, file)
}

// reportStarted 汇报启动结果，并在有组件没起来时给出非零退出码。
func reportStarted(
	opts *Options, plan *upPlan, statuses []engine.Status, file string,
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
				"看日志定位："+logsCommand(engine.K8s, plan.k8s.Namespace, file, "<服务名>"),
				"看事件：kubectl describe deployment/<服务名> -n "+plan.k8s.Namespace,
			)
		}
		return err.WithHint(
			"看日志定位："+logsCommand(engineName(opts), engine.ProjectName(plan.cfg.Project),
				displayPath(opts.WorkDir, file), "<服务名>"),
			"迁移失败会让主服务停在 Created，先看该组件的 -migration 容器",
		)
	}

	opts.Printf("✅ 全部组件已启动（%d 个）\n", len(plan.services))
	renderNextSteps(opts, plan, file)
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
	if e := clierr.As(err); e != nil && e.Code != clierr.CodeInternal {
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
func renderNextSteps(opts *Options, plan *upPlan, file string) {
	opts.Printf("\n💡 查看状态：brickkit status\n")

	if plan.k8s != nil {
		opts.Printf("   查看日志：%s\n",
			logsCommand(engine.K8s, plan.k8s.Namespace, file, ""))
		opts.Printf("   查看 Pod：kubectl get pods -n %s\n", plan.k8s.Namespace)
		return
	}

	opts.Printf("   查看日志：%s -f\n", logsCommand(engineName(opts),
		engine.ProjectName(plan.cfg.Project), displayPath(opts.WorkDir, file), ""))
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

// renderExternals 如实说明哪些依赖由别的项目部署（P39）。
//
// 不说的话，使用者在上面的状态表里看到「✅ demo/hello@1.0.0」，
// 会以为是这次起的，于是去 `docker ps` 里找它——找不到，然后开始怀疑平台。
// 顺带提醒一句"对面没部它就连不上"：这是 external 唯一新增的失败模式，
// 而它的表现（连接超时）跟本项目的任何配置都对不上号。
func renderExternals(opts *Options, cfg *config.Config) {
	var rows []config.Component
	for _, c := range cfg.Components {
		if c.IsExternal() {
			rows = append(rows, c)
		}
	}
	if len(rows) == 0 {
		return
	}

	opts.Printf("🔗 以下依赖由**别的项目**部署，本项目只连接、不启动也不停止：\n")
	for _, c := range rows {
		opts.Printf("   %s@%s  ← 项目 %s\n", c.ID, c.Version, c.External.Project)
	}
	opts.Printf("   对方没部署时，本项目会正常起来但调用它时连接失败\n\n")
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

// renderDatabaseRequirements 告诉使用者还需要建哪些数据库。
//
// 006 §9.1/§9.5：CLI 不创建数据库。但平台有责任说清楚要建什么——
// 否则组件会在迁移阶段抛出一句难以定位的 `database "xxx" does not exist`。
func renderDatabaseRequirements(opts *Options, databases []deploy.DatabaseRequirement) {
	if len(databases) == 0 {
		return
	}

	opts.Printf("\n📌 以下数据库需要预先创建（平台不代建，见 006 §9.5）：\n")
	for _, db := range databases {
		opts.Printf("   %s  （%s:%d，供 %s 使用）\n",
			db.Name, db.Host, db.Port, joinComponents(db.Components))
		opts.Printf("      %s;\n", db.CreateSQL)
	}
	opts.Printf("   已经建过就无需再执行，建库是一次性操作\n")
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
