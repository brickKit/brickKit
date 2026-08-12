package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newUpCommand 实现 brickkit up（004 §3.5，完整实现见 Step 15）。
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
  9. 调用底层引擎启动`,
		Example: `  brickkit up
  brickkit up --only people/basic,department/tree   只启动指定组件
  brickkit up --only people/basic@1.0.0             只启动指定版本
  brickkit up --dry-run                             只生成文件，不启动
  brickkit up --check-resources                     启动前检查资源可达性
  brickkit up --config brickkit.prod.yaml           使用指定配置文件`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, _ = only, dryRun, checkResources
			return clierr.NotImplemented("brickkit up", 15)
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil, "只启动指定组件及其依赖，逗号分隔，支持 @版本")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只生成部署文件，不启动（升级时额外输出变更摘要）")
	cmd.Flags().BoolVar(&checkResources, "check-resources", false, "启动前检查基础资源可达性（不可达时警告但不阻断）")
	return cmd
}
