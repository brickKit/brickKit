package cli

// 本文件是 deploy.target: k8s 那条路（005 §5）。
//
// 与 Docker 那条路共用前半段：读配置 → 升级检查 → 解析依赖 → 级联 → 注入。
// 从"生成什么文件"开始分岔，因为两边确实是两回事：
//
//	Docker  一份 docker-compose.yaml，交给 docker compose
//	K8s     一整个目录的清单，按顺序 kubectl apply，迁移还要 CLI 自己串行等待

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/logging"
)

// k8sDirName 是 K8s 清单在 .brickkit/generated/ 下的子目录名。
const k8sDirName = "k8s"

// upK8s 生成 K8s 清单并交给集群。
func upK8s(ctx context.Context, opts *Options, flags upOptions, plan *upPlan) error {
	dir := filepath.Join(plan.layout.GeneratedDir(), k8sDirName)
	if err := k8s.WriteFiles(dir, plan.k8s.Files); err != nil {
		return err
	}

	opts.Printf("📄 已生成 %d 份清单：%s/\n",
		len(plan.k8s.Files), displayPath(opts.WorkDir, dir))
	opts.Printf("   命名空间：%s\n", plan.k8s.Namespace)
	renderDatabaseRequirements(opts, plan.k8s.Databases)

	if flags.checkResources {
		// K8s 下不查宿主机端口：没有任何东西会绑到这台机器的端口上
		checkResourceReachability(ctx, opts, plan)
	}

	if flags.dryRun {
		renderUpgradeSummary(opts, plan)
		opts.Printf("\n💡 --dry-run 只生成清单，未部署任何东西\n")
		opts.Printf("   查看：ls -R %s\n", displayPath(opts.WorkDir, dir))
		logging.Info("K8s 清单已生成", "dir", dir, "files", len(plan.k8s.Files))
		return nil
	}

	eng, err := resolveEngineFor(opts, plan.cfg)
	if err != nil {
		return err
	}
	renderMigrations(opts, plan.migrations)

	return applyK8s(ctx, opts, eng, plan, dir)
}

// applyK8s 把清单交给集群，然后如实汇报。
func applyK8s(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan, dir string,
) error {
	opts.Printf("\n☸️  正在部署到 Kubernetes（命名空间 %s）...\n", plan.k8s.Namespace)
	if len(plan.k8s.MigrationJobs) > 0 {
		// 16.14：先清旧 Job 再 apply，然后等它跑完
		opts.Printf("   先执行数据库迁移，完成后才启动主服务\n")
	}

	if err := eng.Up(ctx, engine.UpRequest{
		File:          dir,
		Project:       plan.k8s.Namespace,
		Services:      plan.services,
		MigrationJobs: plan.k8s.MigrationJobs,
	}); err != nil {
		return engineFailure("部署", err)
	}

	statuses, err := eng.Status(ctx, dir, plan.k8s.Namespace)
	if err != nil {
		// 部署是部署了，只是问不到状态：不该因此判定失败
		opts.Printf("⚠️ 无法读取集群状态：%s\n", clierr.As(err).Message)
		opts.Printf("   用 brickkit status 再看一次\n")
		return nil
	}
	return reportStarted(opts, plan, statuses, dir)
}

// resolveEngineFor 按部署目标选引擎。
//
// K8s 与 Docker/Podman 不是"同一类引擎的两个牌子"，而是两种部署目标：
// 前者把清单交给集群，后者在本机起容器。选错的后果在 Step 16 之前撞到过一次——
// 一个 target: k8s 的项目被按 Docker 处理，文件生成了、命令也成功了，
// 只是整个项目跑在了错误的编排器上。
func resolveEngineFor(opts *Options, cfg *config.Config) (engine.Engine, error) {
	if cfg != nil && cfg.Deploy.Target == config.TargetK8s {
		if opts.Engine != nil {
			return opts.Engine, nil
		}
		return engine.NewKubectl(), nil
	}
	return resolveEngine(opts)
}

// ============================================================
// ${VAR} 的取值来源
// ============================================================

// envLookup 返回 K8s 生成时用的变量查找函数：先看进程环境，再看项目根的 .env。
//
// 顺序不能反：CI 里靠环境变量注入真实密码，本地靠 .env——
// 让 .env 盖掉环境变量，等于让开发机上的假密码顶掉 CI 传进来的真密码。
func envLookup(workDir string) func(string) (string, bool) {
	var dotenv map[string]string

	return func(name string) (string, bool) {
		if value, ok := os.LookupEnv(name); ok {
			return value, true
		}
		if dotenv == nil {
			dotenv = readDotEnv(filepath.Join(workDir, ".env"))
		}
		value, ok := dotenv[name]
		return value, ok
	}
}

// readDotEnv 读项目根目录的 .env。
//
// 只认最基本的 `KEY=value`：这个文件同时被 docker compose 读，
// 在这里支持一套更花哨的语法，只会让两边对同一个文件解释不一致。
// 文件不存在是正常情况（很多项目不用 .env），返回空表即可。
func readDotEnv(path string) map[string]string {
	out := map[string]string{}

	file, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return out
}
