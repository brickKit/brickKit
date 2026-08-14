package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/inject"
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

当前版本：--only 与 --check-resources 见 Step 15-C。`,
		Example: `  brickkit up
  brickkit up --dry-run                             只生成文件，不启动
  brickkit up --config brickkit.prod.yaml           使用指定配置文件`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(only) > 0 || checkResources {
				return clierr.NotImplemented("brickkit up --only / --check-resources", 15)
			}
			return runUp(cmd.Context(), opts, dryRun)
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil, "只启动指定组件及其依赖，逗号分隔，支持 @版本")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只生成部署文件，不启动（升级时额外输出变更摘要）")
	cmd.Flags().BoolVar(&checkResources, "check-resources", false, "启动前检查基础资源可达性（不可达时警告但不阻断）")
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
	// services 是本次要交给引擎启动的 service（不含 local 组件与迁移容器）。
	services []string
	// migrations 是本次会执行的迁移，供输出（15.25）。
	migrations []migrationInfo
	// images 是要检查拉取权限的镜像（15.19）。
	images []imageInfo
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

// runUp 执行 brickkit up。
func runUp(ctx context.Context, opts *Options, dryRun bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	plan, err := buildUpPlan(ctx, opts)
	if err != nil || plan.done {
		return err
	}

	path, err := writeGenerated(plan.layout, plan.generated.YAML)
	if err != nil {
		return err
	}
	if err := writeLocalEnvFiles(opts, plan.layout, plan.generated.LocalEnvFiles); err != nil {
		return err
	}
	opts.Printf("📄 已生成：%s\n", displayPath(opts.WorkDir, path))
	renderDatabaseRequirements(opts, plan.generated)

	if dryRun {
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
func buildUpPlan(ctx context.Context, opts *Options) (*upPlan, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return nil, err
	}

	plan := &upPlan{layout: layout, cfg: cfg}
	if len(cfg.Components) == 0 {
		opts.Printf("📋 当前项目没有组件\n")
		opts.Printf("   用 brickkit add <组件ID>@<版本> 添加第一个组件\n")
		plan.done = true
		return plan, nil
	}

	opts.Printf("🚀 启动项目 %s（deploy.target: %s）\n", cfg.Project, cfg.Deploy.Target)

	client, err := source.New(layout, cfg, source.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

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

	plan.generated, err = compose.Generate(cfg, plan.graph, plan.states, env, compose.Options{
		Now:    opts.Now,
		Engine: engineName(opts),
	})
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, plan.generated.Warnings)

	plan.collectTargets(order)
	return plan, nil
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
func start(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan, file string,
) error {
	project := engine.ProjectName(plan.cfg.Project)

	opts.Printf("\n🐳 正在启动（%s）...\n", eng.Name())
	if err := eng.Up(ctx, engine.UpRequest{
		File: file, Project: project, Services: plan.services,
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
		return err.WithHint(
			"看日志定位：docker compose -f "+displayPath(opts.WorkDir, file)+" logs <服务名>",
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
	opts.Printf("   查看日志：docker compose -f %s logs -f\n", displayPath(opts.WorkDir, file))
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

// checkImages 检测镜像拉取权限（15.19、004 §10.2）。
//
// 放在启动之前：镜像取不到还硬启，只会得到一堆 ImagePullBackOff，
// 而真正的原因（没登录）埋在引擎的输出里。
func checkImages(ctx context.Context, opts *Options, eng engine.Engine, images []imageInfo) error {
	if len(images) == 0 {
		return nil
	}

	opts.Printf("\n🔍 检测镜像拉取权限...")
	for _, item := range images {
		if err := eng.CheckImage(ctx, item.image); err != nil {
			opts.Printf(" ❌\n")
			return clierr.As(err).WithDetail("组件", item.component)
		}
	}
	opts.Printf(" ✅ 全部通过\n")
	return nil
}

// resolveEngine 返回要用的容器引擎：注入优先，否则自动检测（005 §7.4）。
func resolveEngine(opts *Options) (engine.Engine, error) {
	if opts.Engine != nil {
		return opts.Engine, nil
	}
	if forced := os.Getenv("BRICKKIT_ENGINE"); strings.TrimSpace(forced) != "" {
		switch strings.ToLower(strings.TrimSpace(forced)) {
		case engine.Podman:
			return engine.NewPodman(), nil
		case engine.Docker:
			return engine.NewDocker(), nil
		}
	}
	return engine.Detect()
}

// engineName 决定生成部署文件时用哪个宿主机别名（005 §7.5）。
//
// 与 resolveEngine 分开：生成文件不需要引擎真的可用
// （--dry-run 在没装 Docker 的机器上也该能跑）。
func engineName(opts *Options) string {
	if opts.Engine != nil {
		return opts.Engine.Name()
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BRICKKIT_ENGINE"))) {
	case engine.Podman:
		return compose.EnginePodman
	case engine.Docker:
		return compose.EngineDocker
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return compose.EngineDocker
	}
	if _, err := exec.LookPath("podman-compose"); err == nil {
		return compose.EnginePodman
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
func renderDatabaseRequirements(opts *Options, result *compose.Result) {
	if len(result.Databases) == 0 {
		return
	}

	opts.Printf("\n📌 以下数据库需要预先创建（平台不代建，见 006 §9.5）：\n")
	for _, db := range result.Databases {
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
