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
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// composeFileName 是生成的部署文件名（004 §3.5 输出样例）。
const composeFileName = "docker-compose.yaml"

// newUpCommand 实现 brickkit up（004 §3.5）。
//
// 当前只实现了 --dry-run 这条路径（Step 12 的交付物是"生成部署文件"）；
// 镜像权限检测、执行迁移、调用引擎启动属 Step 15。
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
  5. 生成 docker-compose.yaml 或 K8s YAML，注入环境变量、合并资源配额
  6. 有 local: true 组件时生成 local-debug.<版本化服务名>.env
  7. 检测镜像拉取权限（未授权时提示 docker login）
  8. 执行数据库迁移（失败则阻断主服务启动）
  9. 调用底层引擎启动

当前版本：--dry-run（第 1–5 步）已实现；第 6–9 步见 Step 13 / 15。`,
		Example: `  brickkit up
  brickkit up --only people/basic,department/tree   只启动指定组件
  brickkit up --only people/basic@1.0.0             只启动指定版本
  brickkit up --dry-run                             只生成文件，不启动
  brickkit up --check-resources                     启动前检查资源可达性
  brickkit up --config brickkit.prod.yaml           使用指定配置文件`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dryRun {
				_, _ = only, checkResources
				return clierr.NotImplemented("brickkit up", 15)
			}
			return runUpDryRun(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil, "只启动指定组件及其依赖，逗号分隔，支持 @版本")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只生成部署文件，不启动（升级时额外输出变更摘要）")
	cmd.Flags().BoolVar(&checkResources, "check-resources", false, "启动前检查基础资源可达性（不可达时警告但不阻断）")
	return cmd
}

// runUpDryRun 生成部署文件但不启动任何东西。
func runUpDryRun(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	if len(cfg.Components) == 0 {
		opts.Printf("📋 当前项目没有组件\n")
		opts.Printf("   用 brickkit add <组件ID>@<版本> 添加第一个组件\n")
		return nil
	}

	client, err := source.New(layout, cfg, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	graph, err := resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return err
	}
	states, err := cascade.Compute(cfg, graph)
	if err != nil {
		return err
	}

	renderWarnings(opts, graph.Warnings)
	renderStates(opts, states)

	if states.Empty() {
		opts.Printf("📋 本次没有组件会启动\n")
		opts.Printf("   把需要的组件改成 enabled: true，或移除 enabled: false\n")
		return nil
	}

	plan, err := resolver.Order(graph.Subgraph(states.Running()))
	if err != nil {
		return err
	}
	renderOrder(opts, plan, graph)

	env, err := inject.Build(cfg, graph, states)
	if err != nil {
		return err
	}
	renderWarnings(opts, env.Warnings)

	generated, err := compose.Generate(cfg, graph, states, env, compose.Options{
		Now:    opts.Now,
		Engine: detectEngine(),
	})
	if err != nil {
		return err
	}
	renderWarnings(opts, generated.Warnings)

	path, err := writeGenerated(layout, generated.YAML)
	if err != nil {
		return err
	}

	opts.Printf("📄 已生成：%s\n", displayPath(opts.WorkDir, path))
	if err := writeLocalEnvFiles(opts, layout, generated.LocalEnvFiles); err != nil {
		return err
	}
	renderDatabaseRequirements(opts, generated)
	opts.Printf("\n💡 --dry-run 只生成文件，未启动任何组件\n")
	opts.Printf("   查看：cat %s\n", displayPath(opts.WorkDir, path))

	logging.Info("部署文件已生成", "path", path, "components", len(generated.Databases))
	return nil
}

// detectEngine 挑选容器引擎（005 §7.4）。
//
// 在 Step 13 里它只影响一件事：extra_hosts 用哪个宿主机别名
// （Docker 是 host-gateway，Podman 是 host.containers.internal）。
// 真正调用引擎启动是 Step 15 的事，"两个都没装"要到那时才该报错，
// 现在只生成文件，按 Docker 处理即可。
func detectEngine() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BRICKKIT_ENGINE"))) {
	case compose.EnginePodman:
		return compose.EnginePodman
	case compose.EngineDocker:
		return compose.EngineDocker
	}

	if _, err := exec.LookPath("docker"); err == nil {
		return compose.EngineDocker
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return compose.EnginePodman
	}
	return compose.EngineDocker
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
