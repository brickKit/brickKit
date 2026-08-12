package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newResetCommand 实现 brickkit reset（004 §3.10，完整实现见 Step 8）。
func newResetCommand(opts *Options) *cobra.Command {
	var last bool

	cmd := &cobra.Command{
		Use:     "reset",
		Short:   "恢复 brickkit.yaml 到初始备份或上一次备份",
		GroupID: groupProject,
		Long: `恢复 brickkit.yaml（004 §3.10）。

备份机制（004 §8.3）：
  brickkit init         → .brickkit/backup/brickkit.yaml.initial
  每次 add / remove 前  → .brickkit/backup/brickkit.yaml.last

恢复后需要重新执行 brickkit up 使变更生效。`,
		Example: `  brickkit reset          恢复到初始状态
  brickkit reset --last   恢复到上一次备份`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = last
			return clierr.NotImplemented("brickkit reset", 8)
		},
	}

	cmd.Flags().BoolVar(&last, "last", false, "恢复到上一次操作前的备份（brickkit.yaml.last）")
	return cmd
}
