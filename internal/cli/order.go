package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newOrderCommand 实现 brickkit order（004 §3.8，完整实现见 Step 10）。
func newOrderCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "order",
		Short:   "查看启动顺序与依赖拓扑",
		GroupID: groupLifecycle,
		Long: `展示依赖拓扑排序结果（004 §3.8）。

排序规则：
  - 被依赖的组件排在前面，依赖方排在后面
  - 无依赖的组件最先启动
  - 弱依赖不参与排序约束
  - 存在循环依赖时报错并指出循环路径`,
		Example: "  brickkit order",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return clierr.NotImplemented("brickkit order", 10)
		},
	}
	return cmd
}
