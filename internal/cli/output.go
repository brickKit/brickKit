package cli

import (
	"github.com/brickkit/brickkit/internal/clierr"
)

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
