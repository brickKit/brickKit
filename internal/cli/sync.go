package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newSyncCommand 实现 brickkit sync（004 §3.9，完整实现见 Step 17）。
func newSyncCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sync",
		Short:   "整理组件源码工作区（按级联计算结果归档/激活）",
		GroupID: groupComponent,
		Long: `根据级联计算结果双向整理 components/ 目录（004 §3.9）。

  会启动的组件            → 保留在 components/（在归档目录中则移回来）
  显式关闭（enabled: false）→ 移到 components/.archived/
  被级联跳过的组件         → 移到 components/.archived/

特点：
  - 与 brickkit up 复用同一套级联计算逻辑，但不影响运行中的容器
  - 只操作已有源码的组件，没有 clone 过源码的组件不受影响
  - local: true 组件一视同仁
  - 不提供 --dry-run：搞错了再执行一次就回来了`,
		Example: "  brickkit sync",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return clierr.NotImplemented("brickkit sync", 17)
		},
	}
	return cmd
}
