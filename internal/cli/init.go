package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newInitCommand 实现 brickkit init（004 §3.2，完整实现见 Step 3）。
func newInitCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "init <项目名称>",
		Short:   "初始化项目，生成 brickkit.yaml 骨架",
		GroupID: groupProject,
		Long: `在当前目录初始化一个 BrickKit 项目。

行为（004 §3.2）：
  1. 创建项目目录结构（.brickkit/、components/）
  2. 生成 brickkit.yaml 骨架
  3. 生成 .brickkit/backup/brickkit.yaml.initial（初始备份，供 reset 使用）

项目名称必须显式指定，且只允许小写字母、数字与中划线。`,
		Example: "  brickkit init my-project",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定项目名称").
					WithDetail("用法", "brickkit init <项目名称>").
					WithExit(clierr.ExitUsage)
			}
			return clierr.NotImplemented("brickkit init", 3)
		},
	}
	return cmd
}
