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
// 它们接收哪些参数写在 en.yaml 里那条 key 上方的注释里。
func TestCatalogVerbsMatchSource(t *testing.T) {
	source := catalogFor(SourceLang())
	for _, l := range registry[1:] {
		for key, text := range catalogFor(l.Code) {
			base := strings.TrimSuffix(key, msgid.PluralOneSuffix)
			want, ok := source[base]
			if !ok || want == "" {
				continue // 缺的 key 由 parity 测试报
			}
			assert.Equal(t, verbSet(want), verbSet(text), "%s %s", l.Code, key)
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
