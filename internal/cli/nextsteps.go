package cli

import (
	"strings"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// nextStep 是"下一步"里的一行：一条可以照抄的命令（原样，不翻译），和它做什么。
// 没有命令的一行（"改完骨架里的 TODO"）只有说明；没有说明的一行（"cd my-shop"）只有命令。
type nextStep struct {
	cmd  string
	what string
}

// printNextSteps 打印"下一步："和各行，把命令补齐到同一宽度，让说明对成一列。
//
// 宽度在这里算，而不是在文案里数空格：命令里常带着用户给的名字（cd <目录>），
// 文案里数出来的空格只对某一个长度成立。
func printNextSteps(opts *Options, steps []nextStep) {
	opts.Printf("%s\n", i18n.T(msgid.CliNewNextSteps))
	width := 0
	for _, s := range steps {
		if s.what != "" && len(s.cmd) > width {
			width = len(s.cmd)
		}
	}
	for _, s := range steps {
		switch {
		case s.cmd == "":
			opts.Printf("  %s\n", s.what)
		case s.what == "":
			opts.Printf("  %s\n", s.cmd)
		default:
			opts.Printf("  %s%s%s\n", s.cmd, strings.Repeat(" ", width-len(s.cmd)+4), s.what)
		}
	}
}
