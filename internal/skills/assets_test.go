package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

// 下面这批测试对每种支持的语言各跑一遍：两棵资产树（assets/en、assets/zh）
// 是独立撰写、内容对等的，任何一棵出问题都要在这里被发现，不能只测其中一种
// 语言、假装另一种"应该也一样"。
func forEachLang(t *testing.T, f func(t *testing.T, lang i18n.Lang)) {
	t.Helper()
	for _, lang := range i18n.SupportedLangs() {
		t.Run(string(lang), func(t *testing.T) { f(t, lang) })
	}
}

// 资产清单声明的每个源路径都必须真实存在，内容非空。
// 清单与文件分头维护，漏一个就在这里响。
func TestAssetsAllReadable(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		list := Assets(lang)
		require.NotEmpty(t, list)
		for _, a := range list {
			b, err := a.Content()
			require.NoError(t, err, "读不到内嵌资产：%s", a.Source)
			assert.NotEmpty(t, b, "内嵌资产是空的：%s", a.Source)
		}
	})
}

// 落点必须是项目内的相对路径：绝对路径或 .. 会写到项目外面去。
func TestAssetTargetsAreSafeRelativePaths(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		for _, a := range Assets(lang) {
			assert.False(t, strings.HasPrefix(a.Target, "/"),
				"落点不能是绝对路径：%s", a.Target)
			assert.NotContains(t, a.Target, "..",
				"落点不能含 ..：%s", a.Target)
		}
	})
}

// 落点不能重复——两份资产写同一个文件，后者会静默覆盖前者。
func TestAssetTargetsUnique(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		seen := map[string]string{}
		for _, a := range Assets(lang) {
			if prev, ok := seen[a.Target]; ok {
				t.Fatalf("落点重复：%s 与 %s 都写 %s", prev, a.Source, a.Target)
			}
			seen[a.Target] = a.Source
		}
	})
}

func TestSumIsStableAndPrefixed(t *testing.T) {
	s := Sum([]byte("hello"))
	assert.True(t, strings.HasPrefix(s, "sha256:"))
	assert.Equal(t, s, Sum([]byte("hello")))
	assert.NotEqual(t, s, Sum([]byte("hello!")))
}

// 两种语言的落点必须逐个对应：同一份资产在两棵树里必须写到同一个位置，
// 否则语言切换（Apply 靠"落点相同、内容不同"这一点让旧语言的文件被判成
// "待更新"而不是"缺失"+"多余"）就会失效。
func TestAssetTargetsMatchAcrossLanguages(t *testing.T) {
	targets := func(lang i18n.Lang) map[string]bool {
		out := map[string]bool{}
		for _, a := range Assets(lang) {
			out[a.Target] = true
		}
		return out
	}
	en, zh := targets(i18n.EN), targets(i18n.ZH)
	for t2 := range en {
		assert.True(t, zh[t2], "en 有 %s，zh 没有", t2)
	}
	for t2 := range zh {
		assert.True(t, en[t2], "zh 有 %s，en 没有", t2)
	}
}

// 我们刻意不写用户的 CLAUDE.md（那是他自己的流程文件），但 Claude Code 只读
// CLAUDE.md 而不读 AGENTS.md。所以 AGENTS.md 里必须留着那行接线说明——
// 少了它，想接上的人根本不知道有这个选项。
func TestAgentsMdTellsHowToWireUpClaudeCode(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		for _, a := range Assets(lang) {
			if a.Target != "AGENTS.md" {
				continue
			}
			b, err := a.Content()
			require.NoError(t, err)
			assert.Contains(t, string(b), "@AGENTS.md",
				"要写出那行让人照抄的导入语句")
			assert.Contains(t, string(b), "CLAUDE.md",
				"要说清这行加到哪个文件里")
			return
		}
		t.Fatal("资产清单里没有 AGENTS.md")
	})
}

// 反过来钉住：CLAUDE.md 绝不能出现在资产清单里。
// 往使用者的流程文件里写东西是这套方案里唯一真正侵入的动作，已经明确拒绝。
func TestClaudeMdIsNotShipped(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		for _, a := range Assets(lang) {
			assert.NotEqual(t, "CLAUDE.md", a.Target,
				"不装用户的 CLAUDE.md：那是他自己的流程文件")
		}
	})
}
