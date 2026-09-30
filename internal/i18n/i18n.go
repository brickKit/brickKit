// Package i18n 是 BrickKit CLI 的消息目录：给一个 internal/msgid 里声明的
// key，返回当前语言的文案。只管"文字选哪种语言"，不掺和错误怎么渲染
// （那是 internal/clierr 的事），也不掺和 cobra（那是 internal/cli 的事）。
package i18n

import (
	"embed"
	"fmt"
	"sync"

	"github.com/brickkit/brickkit/internal/msgid"
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

//go:embed locales/*.yaml
var localeFS embed.FS

// loadCatalog 读 locales/<l>.yaml。
func loadCatalog(l Lang) (map[msgid.ID]string, error) {
	file := "locales/" + string(l) + ".yaml"
	data, err := localeFS.ReadFile(file)
	if err != nil {
		return nil, err
	}
	_, texts, err := parseCatalog(data, file)
	if err != nil {
		return nil, err
	}
	out := make(map[msgid.ID]string, len(texts))
	for k, v := range texts {
		out[msgid.ID(k)] = v
	}
	return out, nil
}

var (
	loadMu sync.Mutex
	loaded = map[Lang]map[msgid.ID]string{}
)

// catalogFor 返回 l 的目录，第一次用到时才读；没登记的语言用源语言的目录。
func catalogFor(l Lang) map[msgid.ID]string {
	if !registered(l) {
		l = SourceLang()
	}
	loadMu.Lock()
	defer loadMu.Unlock()
	if c, ok := loaded[l]; ok {
		return c
	}
	c, err := loadCatalog(l)
	if err != nil {
		// 目录文件内嵌在二进制里、每次测试都会读：读不通是构建缺陷，不是使用者会遇到的情形
		panic("brickkit: embedded catalog is broken: " + err.Error())
	}
	loaded[l] = c
	return c
}

// T 返回 id 对应的当前语言文案，用 args 做位置参数插值
// （目录里的动词一律是 %[1]s 这种位置 verb，因为中英文语序经常不同）。
//
// id 必须是 internal/msgid 里声明的常量。两份目录都缺失同一个 key 会被
// TestCatalogParity 在测试期拦住，正常运行不会走到"查不到"这条分支；
// 万一真的走到了（比如新增 key 时漏了一份目录、测试又没跑），直接暴露
// 问题而不是悄悄回落到另一种语言——这是这个项目一贯的态度：宁可大声失败，也不悄悄出错。
func T(id msgid.ID, args ...any) string {
	text, ok := catalogFor(current)[id]
	if !ok {
		return fmt.Sprintf("!missing-i18n-key:%s!", id)
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// TN 是 T 的单复数版本：n 决定用哪一种形式，args 照旧是位置参数——
// n 不会自动进 args，模板里要显示这个数就要把它也放进 args（跟 gettext 的
// ngettext 一样，明说比"第一个参数悄悄是 n"少一层要记的约定）。
//
// 目录里 id 本身是"其他"形式（英文的复数，也是没有单复数之分的语言的唯一形式）；
// n == 1 且当前语言的目录里有 id+msgid.PluralOneSuffix 这一条，就用它，否则
// 回落到 id 本身。中文不写单数条目，所以永远走 id——不用为中文再开一份重复文案。
func TN(id msgid.ID, n int, args ...any) string {
	if n == 1 {
		if _, ok := catalogFor(current)[id+msgid.PluralOneSuffix]; ok {
			return T(id+msgid.PluralOneSuffix, args...)
		}
	}
	return T(id, args...)
}

// Count 返回"数字 + 名词"的短语（"3 files" / "1 file"），id 是 msgid 里
// Count* 那一族的 key，模板里 %[1]d 就是 n。要在句子里数一样东西时用它，
// 句子本身只留一个 %s 接这个短语，这样单复数的事全在这一族 key 里解决。
func Count(id msgid.ID, n int) string {
	return TN(id, n, n)
}

// CatalogFor 返回给定语言目录的拷贝，只给测试与工具用（核对各语言目录、把文档里的输出行对回
// 文案）。生产代码只用 T 说当前语言，不翻整份目录——tests/i18nguard 拦着。
// 返回值是拷贝，调用方改它不会影响真正的目录。
func CatalogFor(l Lang) map[msgid.ID]string {
	src := catalogFor(l)
	out := make(map[msgid.ID]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
