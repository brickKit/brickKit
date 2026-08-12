package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newPublishCommand 实现 brickkit publish（004 §3.11，完整实现见 Step 19）。
func newPublishCommand(opts *Options) *cobra.Command {
	var (
		path       string
		visibility string
	)

	cmd := &cobra.Command{
		Use:     "publish",
		Short:   "发布组件到市场（需先 brickkit login）",
		GroupID: groupMarket,
		Long: `把组件发布到市场（004 §3.11）。

行为：
  1. 检查登录状态（credentials 或 sources.authToken），未登录报错
  2. 读取组件目录中的 component.yaml 并校验 Manifest 格式
  3. 读取 Manifest 中的 artifacts 字段
  4. 检查镜像引用是否有效
  5. 上传 Manifest + 镜像引用 + 产物文件 + 签名到市场
  6. 设置可见性（public / private）

--path 支持归档目录，例如 ./components/.archived/erp/backend。`,
		Example: `  brickkit publish --path ./components/people/basic
  brickkit publish --path ./components/people/basic --visibility private`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = path, visibility
			return clierr.NotImplemented("brickkit publish", 19)
		},
	}

	cmd.Flags().StringVar(&path, "path", ".", "组件源码目录（含 component.yaml）")
	cmd.Flags().StringVar(&visibility, "visibility", "", "可见性：public | private（默认沿用市场侧设置）")
	return cmd
}
