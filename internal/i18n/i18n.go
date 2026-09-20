// Package i18n 是 BrickKit CLI 的消息目录：给一个 internal/msgid 里声明的
// key，返回当前语言的文案。只管"文字选哪种语言"，不掺和错误怎么渲染
// （那是 internal/clierr 的事），也不掺和 cobra（那是 internal/cli 的事）。
package i18n

import "fmt"

// Lang 是已支持的语言。
type Lang string

const (
	EN Lang = "en"
	ZH Lang = "zh"
)

// current 是当前进程的语言。默认 EN；CLI 每次构建命令树时都会重新解析
// 并设置一次（跟 internal/logging 的 SetLevel 是同一种"进程级全局状态，
// 但每次入口调用都重新初始化"的用法），所以测试不需要手动复位。
var current = EN

// SetCurrent 设置当前进程使用的语言。
func SetCurrent(l Lang) {
	current = l
}

// Current 返回当前生效的语言。
func Current() Lang {
	return current
}

func catalogFor(l Lang) map[string]string {
	if l == ZH {
		return zh
	}
	return en
}

// T 返回 id 对应的当前语言文案，用 args 做位置参数插值
// （目录里的动词一律是 %[1]s 这种位置 verb，因为中英文语序经常不同）。
//
// id 必须是 internal/msgid 里声明的常量。两份目录都缺失同一个 key 会被
// TestCatalogParity 在测试期拦住，正常运行不会走到"查不到"这条分支；
// 万一真的走到了（比如新增 key 时漏了一份目录、测试又没跑），直接暴露
// 问题而不是悄悄回落到另一种语言——这是这个项目一贯的态度（§9.13）。
func T(id string, args ...any) string {
	text, ok := catalogFor(current)[id]
	if !ok {
		return fmt.Sprintf("!missing-i18n-key:%s!", id)
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// CatalogFor 返回给定语言目录的只读快照，供工具类代码使用
// （tests/docfields 核对错误码文档标题要用到），不用于运行时查文案——
// 运行时一律用 T()。返回值是拷贝，调用方改它不会影响真正的目录。
func CatalogFor(l Lang) map[string]string {
	src := catalogFor(l)
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
