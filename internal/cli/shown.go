package cli

// 本文件是报错与警告"给人看"的那一步：里面项目根下的绝对路径，按使用者所在的目录写成相对路径。
//
// 命令内部一律用绝对路径（向上找到项目之后，项目根和使用者所在目录不是一处，相对路径会算错）；
// 可给人看的 "文件：/home/you/work/my-shop/deploy.local.yaml" 又长又难认——像 git 一样，
// 在项目根就是 deploy.local.yaml，在组件目录里就是 ../../../deploy.local.yaml。
// 日志（stderr 上的 JSON 行）里记的仍是绝对路径：那是给机器和排查用的。

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/brickkit/brickkit/internal/clierr"
)

// render 是报错或警告给人看的样子：命令层渲染 *clierr.Error 一律经过它。
func (o *Options) render(e *clierr.Error) string { return o.shown(e).Format() }

// shown 是给人看的那一份错误：路径按使用者所在的目录写。原错误不变。
func (o *Options) shown(e *clierr.Error) *clierr.Error { return e.MapText(o.relativize) }

// relativize 把 s 里项目根（WorkDir）下的绝对路径换成相对 CallDir 的写法。
// 只认完整的路径：/w/my-shop2 不是 /w/my-shop 下面的东西，/app/caller migrate 也不是。
func (o *Options) relativize(s string) string {
	root := o.WorkDir
	if !filepath.IsAbs(root) || !filepath.IsAbs(o.CallDir) || !strings.Contains(s, root) {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, root)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(root)
		startsPath := i == 0 || endsPath(lastRune(s[:i]))
		if !startsPath || (end < len(s) && s[end] != os.PathSeparator && !endsPath(firstRune(s[end:]))) {
			b.WriteString(s[:end])
			s = s[end:]
			continue
		}
		for end < len(s) {
			r, size := utf8.DecodeRuneInString(s[end:])
			if endsPath(r) {
				break
			}
			end += size
		}
		token := s[i:end]
		if rel, err := filepath.Rel(o.CallDir, token); err == nil {
			token = rel
		}
		b.WriteString(s[:i])
		b.WriteString(token)
		s = s[end:]
	}
}

// endsPath：路径在这些字符处结束——空白、ASCII 的引号括号逗号分号冒号，以及任何非 ASCII 的标点
// （中文句子里的全角括号、逗号、冒号）。
func endsPath(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("\"'`()[]{}<>,;:", r) ||
		(r > unicode.MaxASCII && unicode.IsPunct(r))
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}
