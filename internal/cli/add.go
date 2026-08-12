package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newAddCommand 实现 brickkit add（004 §3.3，完整实现见 Step 9）。
func newAddCommand(opts *Options) *cobra.Command {
	var (
		yes     bool
		refresh bool
		repo    bool
		repoAll bool
	)

	cmd := &cobra.Command{
		Use:     "add <组件ID>@<精确版本>",
		Short:   "添加组件，递归拉取依赖与产物，写入 brickkit.yaml",
		GroupID: groupComponent,
		Long: `添加组件到项目（004 §3.3）。

行为：
  1. 从安装源获取 Manifest（市场 / Git / 本地目录）
  2. 递归解析 dependencies.components
  3. 强依赖不可获取 → 报错终止；弱依赖不可获取 → 警告但继续
  4. 同 ID 不同版本 → 自动添加第二个条目（多版本默认共存）
  5. 下载 artifacts 到 .brickkit/artifacts/<版本化服务名>/
  6. 写入 brickkit.yaml（不写 enabled 字段）

版本必须是精确版本（major.minor.patch），不接受 ^ 或 ~ 范围约束。`,
		Example: `  brickkit add people/basic@1.0.0
  brickkit add people/basic@1.0.0 --yes         非交互模式
  brickkit add people/basic@1.0.0 --repo        额外 clone 该组件源码
  brickkit add erp/backend@1.0.0 --repo-all     clone 所有开源依赖源码`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定要添加的组件").
					WithDetail("用法", "brickkit add <组件ID>@<精确版本>").
					WithDetail("示例", "brickkit add people/basic@1.0.0").
					WithExit(clierr.ExitUsage)
			}
			_, _, _, _ = yes, refresh, repo, repoAll
			return clierr.NotImplemented("brickkit add", 9)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "非交互模式，跳过所有确认提示（适用于 CI/CD）")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "强制重新拉取 Manifest 和 artifacts，忽略缓存")
	cmd.Flags().BoolVar(&repo, "repo", false, "额外 clone 该组件的完整 Git 仓库到 components/（仅开源组件）")
	cmd.Flags().BoolVar(&repoAll, "repo-all", false, "clone 所有递归依赖中开源组件的 Git 仓库（闭源组件跳过）")
	return cmd
}
