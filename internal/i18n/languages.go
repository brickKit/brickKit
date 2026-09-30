package i18n

// 本文件是 CLI 支持哪些语言的唯一登记处。加一种语言 = 在 registry 里加一行，
// 再放一份 locales/<代码>.yaml。别处一律从这里推出语言列表，不写死。

// Lang 是语言代码（BRICKKIT_LANG、brickkit lang set 的取值）。
type Lang string

const (
	EN Lang = "en"
	ZH Lang = "zh"
)

// Language 是登记的一种语言。
type Language struct {
	Code Lang
}

// registry 按展示顺序列出全部语言；第一项是源语言：每个 key 先在它的目录里声明，
// 其余语言的目录与它逐 key 对应。
var registry = []Language{
	{Code: EN},
	{Code: ZH},
}

// SourceLang 返回源语言。
func SourceLang() Lang { return registry[0].Code }

// registered 报告 l 是否是登记过的语言。
func registered(l Lang) bool {
	for _, lang := range registry {
		if lang.Code == l {
			return true
		}
	}
	return false
}
