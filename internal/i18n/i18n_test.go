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

// 三层文件里每个字段都有固定的家：mode / expose / hostname 这些部署字段在部署文件里，
// 不在 brickkit.yaml 里；override.yaml 与 servedBy 已经废除。一条提示把人指到错的文件，
// 比不给提示更糟——使用者会在那里找半天找不到这个字段。
func TestMessagesNameTheFileThatHoldsTheField(t *testing.T) {
	// 只认"把字段当值来写"的形状（mode: disable、mode to disable、exposePort……）：光出现 mode、
	// members 这样的词不算——"brickkit.yaml 里有 kind: shell、部署文件里有 members"是对的。
	// 查不到的：跨行的一句话、经 %s 填进去的文件名，这两种只能靠写文案的人自己留意。
	deployField := regexp.MustCompile(`\b(mode: ?\w|mode to \w|exposePort|hostname|localPort|expose: |replicas)`)
	legacy := regexp.MustCompile(`override\.yaml|servedBy|served-by|brickkit override`)
	for _, lang := range []Lang{EN, ZH} {
		for id, text := range CatalogFor(lang) {
			// 逐行看：同一句话里既说 brickkit.yaml 又说部署字段，才是把字段指错了文件
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "brickkit.yaml") && deployField.MatchString(line) {
					t.Errorf("%s %s：把部署字段说成在 brickkit.yaml 里：%q", lang, id, line)
				}
			}
			if legacy.MatchString(text) {
				t.Errorf("%s %s：提到已废除的概念：%q", lang, id, text)
			}
		}
	}
}
