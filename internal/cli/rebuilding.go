package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 三层文件重构期间，remove 按新的组件分发模型整体重写，在 P4 落地。在那之前命令照常出现在
// 帮助里，但执行时明确说明它正在重建——不能静默失败，也不能按旧模型写出一份新模型读不懂的文件。

func newRemoveCommand(opts *Options) *cobra.Command {
	return rebuildingCommand("remove", i18n.T(msgid.CliRemoveRemoveComponentIdVersion), i18n.T(msgid.CliRemoveShort), "P4")
}

func rebuildingCommand(name, use, short, phase string) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		GroupID: groupComponent,
		// 旧参数（--yes / --repo …）照样收下再统一报"正在重建"，而不是先报"未知参数"
		// 把人带偏；--help 仍由 cobra 处理，帮助照常可看。
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		Args:               cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return clierr.New(clierr.CodeNotImplemented, i18n.T(msgid.CliCommandRebuilding, name, phase))
		},
	}
}

// renderWarnings 逐条打印不阻断的问题。
func renderWarnings(opts *Options, warnings []*clierr.Error) {
	for _, w := range warnings {
		opts.Printf("%s", w.Format())
	}
}

// itoa 把非负整数转成十进制字符串。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
