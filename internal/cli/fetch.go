package cli

// 本文件实现 brickkit fetch：只取一个组件的产物，不把它装进项目（003 §4.9）。
//
// # 为什么需要一条自己的命令
//
// 跨项目调用一个共享服务时，你需要它的 `.proto` / `openapi.json` 来生成客户端，
// 但**不能**把它写进 brickkit.yaml——那样平台会在你自己的项目里再部署一份
// （两个实例、两套定时任务，而两边都以为自己是唯一那个）。
//
// `add` 做不到这件事：它的语义就是"把它装进来"，加个"只下产物"的开关等于
// 在一个命令里塞两种意图。所以是一条只读的独立命令。
//
// # 它替使用者省掉的不是下载，是"文件该放哪"
//
// 手工拿到的契约文件，每个项目都会自己发明一个目录。`fetch` 让它落到
// 与 `add` 完全相同的位置：.brickkit/artifacts/<版本化服务名>/<type>/...
// 目录名带版本，于是"我的客户端是照哪个版本写的"有了唯一答案；
// 对方升版本时是新建一个目录，`git diff` 里看得见。
//
// # 与 add 的一处刻意不同：产物下载失败是错误，不是警告
//
// `add` 把产物失败降级为警告，因为组件本身已经装好了，产物只是开发时辅助。
// `fetch` 反过来——产物就是它的全部目的。失败还退出码 0，
// 使用者会拿着一个空目录去生成客户端。

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/source"
)

// newFetchCommand 实现 brickkit fetch（003 §4.9）。
func newFetchCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "fetch <组件ID>[@<版本>]",
		Short:   "下载组件的产物（API 契约、SDK 等），不装进项目",
		GroupID: groupComponent,
		Long: `下载一个组件声明的产物文件，但**不把它装进本项目**（003 §4.9）。

用于跨项目调用：你要对方的 .proto / openapi.json 来生成客户端，
但那个服务由别的项目部署，写进 brickkit.yaml 会让平台在你这边再部一份。

不写版本号时取安装源里的最新版本。

产物落到 .brickkit/artifacts/<版本化服务名>/<type>/... ——
与 brickkit add 下来的完全同一个位置，默认跟着项目提交、团队共享。

这条命令只读：不修改 brickkit.yaml，不生成部署文件，不启动任何东西。`,
		Example: `  brickkit fetch infra/notifier@1.0.0   取指定版本的产物
  brickkit fetch infra/notifier         取最新版本的产物`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFetch(cmd.Context(), opts, args[0])
		},
	}
	return cmd
}

func runFetch(ctx context.Context, opts *Options, arg string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	id, version, err := parseComponentRef(arg, false)
	if err != nil {
		return err
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if version == "" {
		// 不问"要不要多版本共存"：那是 add 的顾虑（它会多起一个容器）。
		// fetch 什么都不部署，取第二个版本的契约来对比反而是正常操作。
		latest, err := client.LatestVersion(ctx, id)
		if err != nil {
			return err
		}
		opts.Printf("🔎 未指定版本，解析到 %s@%s（来自安装源 %s，类型 %s）\n",
			id, latest.Version, latest.SourceID, latest.SourceKind)
		version = latest.Version
	}

	fetched, err := client.Manifest(ctx, id, version)
	if err != nil {
		return err
	}
	renderWarnings(opts, fetched.Warnings)

	ref := id + "@" + version
	if len(fetched.Manifest.Artifacts) == 0 {
		// 说清楚而不是打印一个空的成功：使用者会以为下载失败了，
		// 转头去查网络——而真相是这个组件根本没声明产物。
		opts.Printf("ℹ️ %s 没有声明任何产物，无可下载\n", ref)
		opts.Printf("   产物由组件作者在 component.yaml 的 artifacts 里声明（002 §2.3）\n")
		logging.Info("产物下载完成", "component", ref, "downloaded", 0)
		return nil
	}

	res, err := client.DownloadArtifacts(ctx, fetched.Manifest)
	if err != nil {
		return err
	}
	if err := fetchFailure(ref, res); err != nil {
		return err
	}

	// Downloaded / Cached 里的路径是相对 .brickkit/artifacts/ 的，开头就带着
	// 版本化服务名。上面已经把它作为目录打过一次，这里去掉，免得同一个名字
	// 在每一行里重复一遍。
	service := manifest.ServiceName(id, version)
	dir := displayPath(opts.WorkDir, client.ArtifactDir(id, version))
	opts.Printf("📦 已下载 %s 的产物（未写入 %s）\n", ref, layout.ConfigName())
	opts.Printf("   %s/\n", dir)
	for _, file := range res.Downloaded {
		opts.Printf("     %s\n", strings.TrimPrefix(file, service+"/"))
	}
	for _, file := range res.Cached {
		opts.Printf("     %s（已存在，跳过）\n", strings.TrimPrefix(file, service+"/"))
	}
	renderWarnings(opts, res.Warnings)

	opts.Printf("\n")
	opts.Printf("💡 这个组件不会被本项目部署。要连它，把对方给的地址填进依赖方的 config\n")
	opts.Printf("   （003 §4.9 跨项目共用组件）\n")

	logging.Info("产物下载完成", "component", ref,
		"downloaded", len(res.Downloaded), "cached", len(res.Cached))
	return nil
}

// fetchFailure 在"一个产物都没拿到"时报错。
//
// 与 add 相反：add 装的是组件，产物失败只是少了开发时辅助；
// fetch 的全部目的就是产物，一个都没拿到还退出码 0，
// 使用者会拿着一个空目录去生成客户端，直到编译报错才发现。
func fetchFailure(ref string, res *source.ArtifactResult) error {
	if len(res.Downloaded) > 0 || len(res.Cached) > 0 {
		return nil
	}
	err := clierr.Newf(clierr.CodeNetworkUnreachable, "错误：%s 的产物一个都没下载成功", ref)
	for _, w := range res.Warnings {
		err = err.WithDetail("失败", w.Message)
	}
	return err.WithHint(
		"检查网络与安装源地址",
		"确认该版本在安装源里确实带着产物文件",
	)
}
