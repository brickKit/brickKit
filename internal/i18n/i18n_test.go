package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/msgid"
)

func TestCurrentDefaultsToEnglish(t *testing.T) {
	assert.Equal(t, EN, Current())
}

func TestSetCurrentChangesLookupLanguage(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)

	SetCurrent(ZH)
	assert.Equal(t, "建议：", T(msgid.HintLabelMulti))

	SetCurrent(EN)
	assert.Equal(t, "Suggestions:", T(msgid.HintLabelMulti))
}

func TestTInterpolatesPositionalArgs(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)

	SetCurrent(EN)
	assert.Equal(t, "Path: brickkit.yaml", T(msgid.DetailLine, "Path", "brickkit.yaml"))

	SetCurrent(ZH)
	assert.Equal(t, "路径：brickkit.yaml", T(msgid.DetailLine, "路径", "brickkit.yaml"))
}

func TestCatalogForReturnsIndependentSnapshot(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)
	SetCurrent(ZH)

	snap := CatalogFor(ZH)
	snap[msgid.HintLabelMulti] = "被改了"
	assert.Equal(t, "建议：", T(msgid.HintLabelMulti), "CatalogFor 返回的必须是拷贝，不能让调用方改到真的目录")
}

// TestCatalogParity 是双语完整性的门槛：msgid 里声明的每个 key，en/zh
// 两份目录都必须有，缺一个就是半成品——不允许运行时才发现某句话是空字符串。
//
// 单数形式（key 以 msgid.PluralOneSuffix 结尾）例外：那是"这门语言有单复数之分"
// 才写的，中文没有，所以它可以只出现在一份目录里；它自己的完整性由下面两个
// 测试守着。
func TestCatalogParity(t *testing.T) {
	for key := range en {
		if isPluralOne(key) {
			continue
		}
		if _, ok := zh[key]; !ok {
			t.Errorf("zh 目录缺少 key：%s", key)
		}
	}
	for key := range zh {
		if isPluralOne(key) {
			continue
		}
		if _, ok := en[key]; !ok {
			t.Errorf("en 目录缺少 key：%s", key)
		}
	}
}

func isPluralOne(key string) bool {
	return strings.HasSuffix(key, msgid.PluralOneSuffix)
}

// verbs 取出模板里用到的位置 verb 编号（%[2]d → 2），去重排序——单数形式
// 与"其他"形式必须用同一组参数，否则 n==1 时会在运行时冒出 %!s(MISSING)。
var verbRE = regexp.MustCompile(`%[-+# 0]*\d*(?:\.\d+)?\[(\d+)\][a-zA-Z]`)

func verbs(tmpl string) []string {
	seen := map[string]bool{}
	for _, m := range verbRE.FindAllStringSubmatch(tmpl, -1) {
		seen[m[1]] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestPluralOneKeysComplementTheirBase：每条单数形式都必须有对应的"其他"形式，
// 并且两者用到的参数一致。
func TestPluralOneKeysComplementTheirBase(t *testing.T) {
	for name, cat := range map[string]map[string]string{"en": en, "zh": zh} {
		for key, one := range cat {
			if !isPluralOne(key) {
				continue
			}
			base := strings.TrimSuffix(key, msgid.PluralOneSuffix)
			other, ok := cat[base]
			if !ok {
				t.Errorf("%s 目录里 %s 有单数形式却没有\"其他\"形式", name, base)
				continue
			}
			assert.Equal(t, verbs(other), verbs(one), "%s 目录里 %s 的单数与\"其他\"形式用的参数不一致", name, base)
		}
	}
}

func TestTNPicksSingularOnlyForOneAndOnlyWhereTheCatalogHasIt(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)

	SetCurrent(EN)
	assert.Equal(t, "1 file", Count(msgid.CountFiles, 1))
	assert.Equal(t, "0 files", Count(msgid.CountFiles, 0))
	assert.Equal(t, "2 files", Count(msgid.CountFiles, 2))
	assert.Equal(t, "1 file needs refreshing: brickkit skills update", TN(msgid.CliSkillsFilesNeedRefreshing, 1, 1))
	assert.Equal(t, "3 files need refreshing: brickkit skills update", TN(msgid.CliSkillsFilesNeedRefreshing, 3, 3))

	// 中文目录没有单数形式：n==1 也走 key 本身
	SetCurrent(ZH)
	assert.Equal(t, "1 个文件", Count(msgid.CountFiles, 1))
	assert.Equal(t, "3 个文件", Count(msgid.CountFiles, 3))
}
