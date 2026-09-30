package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/msgid"
)

// 每种语言的每条消息，用到的参数（位置与 verb）与源语言那一条完全一致——否则运行时冒出 %!s(MISSING)。
//
// 源语言是空字符串的 key（cobra.* 那几条："保留 cobra 自带的英文"）没有可比的文案，不比；
// 它们由 TestKeysWithoutSourceTextUseOnlyTheArgumentsPassed 按代码实际传的参数检查。
func TestCatalogVerbsMatchSource(t *testing.T) {
	source := catalogFor(SourceLang())
	for _, l := range registry[1:] {
		for key, text := range catalogFor(l.Code) {
			want, ok := source[pluralBase(key)]
			if !ok || want == "" {
				continue // 缺的 key 由 parity 测试报
			}
			assert.Equal(t, verbSet(want), verbSet(text), "%s %s", l.Code, key)
		}
	}
}

// argsOfKeysWithoutSourceText 是源语言留空的 key（"保留 cobra 自带的英文"）实际收到的参数：
// 没有源文案可比，就按代码传的参数比。键就是 root.go 的 localize 怎么调它们。
var argsOfKeysWithoutSourceText = map[msgid.ID][]string{
	msgid.CobraUsageTemplate:   nil,
	msgid.CobraHelpShort:       nil,
	msgid.CobraCompletionShort: nil,
	msgid.CobraHelpFlag:        {"1s", "2s"}, // 命令的显示名、命令路径
}

// 源语言留空的 key，译文只能用代码真的会传的参数——否则 -h 里冒出 %!s(BADINDEX)。
// 新加一条留空的 key，要先在上面那张表里写明它收到哪些参数。
func TestKeysWithoutSourceTextUseOnlyTheArgumentsPassed(t *testing.T) {
	source := catalogFor(SourceLang())
	for key, text := range source {
		if text != "" {
			continue
		}
		allowed, ok := argsOfKeysWithoutSourceText[key]
		if !assert.True(t, ok, "%s 在 %s 里是空的：在 argsOfKeysWithoutSourceText 里写明它收到的参数", key, SourceLang()) {
			continue
		}
		for _, l := range registry[1:] {
			for _, v := range verbSet(catalogFor(l.Code)[key]) {
				assert.Contains(t, allowed, v, "%s %s 用了代码不会传的参数 %s", l.Code, key, v)
			}
		}
	}
}

// anyVerbRE 是任意一个格式动词，带不带 [n] 都算。
var anyVerbRE = regexp.MustCompile(`%[-+# 0]*\d*(?:\.\d+)?(\[\d+\])?([a-zA-Z%])`)

// 目录里的参数一律写成带位置的 %[1]s：不带位置的 %s 按出现顺序取参数，
// 译者调整语序就会把参数对错位，而且上面的比较看不见它。
func TestCatalogVerbsArePositional(t *testing.T) {
	for _, l := range registry {
		for key, text := range catalogFor(l.Code) {
			for _, m := range anyVerbRE.FindAllStringSubmatch(text, -1) {
				if m[1] == "" && m[2] != "%" {
					t.Errorf("%s %s: %q has no position; write it as %%[n]%s", l.Code, key, m[0], m[2])
				}
			}
		}
	}
}

var verbWithTypeRE = regexp.MustCompile(`%[-+# 0]*\d*(?:\.\d+)?\[(\d+)\]([a-zA-Z])`)

// verbSet：%[2]d → "2d"，去重排序——位置与类型都要对上。
func verbSet(tmpl string) []string {
	seen := map[string]bool{}
	for _, m := range verbWithTypeRE.FindAllStringSubmatch(tmpl, -1) {
		seen[m[1]+m[2]] = true
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// 译文的行结构与源语言一致：开头与结尾的换行数都相同。CLI 靠它们排版——多一个结尾换行
// 就多一行空行。写成 | 块的译文最容易多出一个结尾换行（| 默认保留一个）。
// 开头的空格不比：英文在括号前空一格、中文用全角括号不空，是各自的排版习惯。
func TestCatalogLineBreaksMatchSource(t *testing.T) {
	lead := func(s string) int { return len(s) - len(strings.TrimLeft(s, "\n")) }
	trail := func(s string) int { return len(s) - len(strings.TrimRight(s, "\n")) }
	source := catalogFor(SourceLang())
	for _, l := range registry[1:] {
		for key, text := range catalogFor(l.Code) {
			want, ok := source[pluralBase(key)]
			if !ok || want == "" {
				continue
			}
			assert.Equal(t, lead(want), lead(text), "%s %s: starts with a different number of newlines from %s", l.Code, key, SourceLang())
			assert.Equal(t, trail(want), trail(text), "%s %s: ends with a different number of newlines from %s", l.Code, key, SourceLang())
		}
	}
}
