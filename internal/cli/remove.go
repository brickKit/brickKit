package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newRemoveCommand 实现 brickkit remove（004 §3.4，完整实现见 Step 9）。
func newRemoveCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <组件ID>[@版本]",
		Short:   "移除组件，并删除对应的源码目录与缓存",
		GroupID: groupComponent,
		Long: `从项目中移除组件（004 §3.4）。

行为：
  1. 检查是否有其他组件强依赖它 → 有则阻止移除
  2. 从 brickkit.yaml 中移除条目
  3. 清理 Manifest 缓存与 artifacts 缓存
  4. 自动删除 components/<scope>/<name>/ 源码目录

多版本共存时必须指定版本，否则报错。`,
		Example: `  brickkit remove people/basic
  brickkit remove people/basic@1.0.0    多版本共存时指定版本`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定要移除的组件").
					WithDetail("用法", "brickkit remove <组件ID>[@版本]").
					WithDetail("示例", "brickkit remove people/basic@1.0.0").
					WithExit(clierr.ExitUsage)
			}
			return clierr.NotImplemented("brickkit remove", 9)
		},
	}
	return cmd
}
