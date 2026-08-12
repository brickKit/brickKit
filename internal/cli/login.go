package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newLoginCommand 实现 brickkit login（004 §3.12，完整实现见 Step 19）。
func newLoginCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "login",
		Short:   "登录组件市场，Token 存入 .brickkit/credentials",
		GroupID: groupMarket,
		Long: `登录 BrickKit 市场（004 §3.12）。

行为：
  1. 终端输入用户名
  2. 终端输入密码（隐藏输入）
  3. 调用市场 API 验证凭据
  4. 成功则把 Token 写入 .brickkit/credentials

Token 优先级：.brickkit/credentials > brickkit.yaml 中的 sources.authToken。
CLI 每次使用 Token 前检查 expiresAt，过期则提示重新登录（不做自动刷新）。`,
		Example: "  brickkit login",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return clierr.NotImplemented("brickkit login", 19)
		},
	}
	return cmd
}
