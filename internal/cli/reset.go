package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/backup"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
)

// newResetCommand 实现 brickkit reset（004 §3.10）。
func newResetCommand(opts *Options) *cobra.Command {
	var last bool

	cmd := &cobra.Command{
		Use:     "reset",
		Short:   "恢复 brickkit.yaml 到初始备份或上一次备份",
		GroupID: groupProject,
		Long: `恢复 brickkit.yaml（004 §3.10）。

备份机制（003 §7.1）：
  brickkit init         → .brickkit/backup/brickkit.yaml.initial
  每次 add / remove 前  → .brickkit/backup/brickkit.yaml.last

恢复只改写配置文件本身，不会动 .brickkit/ 下的缓存与生成物。
恢复后需要重新执行 brickkit up 使变更生效。`,
		Example: `  brickkit reset          恢复到初始状态
  brickkit reset --last   恢复到上一次备份`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset(opts, last)
		},
	}

	cmd.Flags().BoolVar(&last, "last", false, "恢复到上一次操作前的备份（brickkit.yaml.last）")
	return cmd
}

func runReset(opts *Options, last bool) error {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	kind := backup.Initial
	if last {
		kind = backup.Last
	}

	rec, err := backup.Restore(layout, kind)
	if err != nil {
		return err
	}

	logging.Info("配置已恢复",
		"config", rec.ConfigPath,
		"backup", rec.Path,
		"kind", string(rec.Kind),
	)

	// 输出格式与 003 §7.2 / 004 §3.10 的样例逐字一致。
	opts.Printf("🔄 已恢复 %s 到%s\n", layout.ConfigName(), rec.Kind.Label())
	opts.Printf("   备份位置：%s\n", rec.RelPath)
	opts.Printf("   恢复时间：%s\n", opts.now().UTC().Format(time.RFC3339))
	opts.Printf("\n")
	opts.Printf("⚠️ 注意：恢复后需要重新执行 brickkit up 使变更生效\n")
	return nil
}
