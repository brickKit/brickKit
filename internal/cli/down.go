package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newDownCommand 实现 brickkit down（004 §3.6，完整实现见 Step 15）。
func newDownCommand(opts *Options) *cobra.Command {
	var only []string

	cmd := &cobra.Command{
		Use:     "down",
		Short:   "一键停止所有组件（不删除 volume）",
		GroupID: groupLifecycle,
		Long: `停止项目（004 §3.6）。

停止顺序与启动顺序相反（依赖方先停，被依赖方后停）。

重要：down 不删除 volume，数据库数据始终保留。
如需彻底清理，请手动执行 docker volume rm 或 docker compose down -v。`,
		Example: `  brickkit down
  brickkit down --only people/basic   只停止指定组件`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = only
			return clierr.NotImplemented("brickkit down", 15)
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil, "只停止指定组件，逗号分隔，支持 @版本")
	return cmd
}
