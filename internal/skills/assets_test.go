package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 资产清单声明的每个源路径都必须真实存在，内容非空。
// 清单与文件分头维护，漏一个就在这里响。
func TestAssetsAllReadable(t *testing.T) {
	list := Assets()
	require.NotEmpty(t, list)
	for _, a := range list {
		b, err := a.Content()
		require.NoError(t, err, "读不到内嵌资产：%s", a.Source)
		assert.NotEmpty(t, b, "内嵌资产是空的：%s", a.Source)
	}
}

// 落点必须是项目内的相对路径：绝对路径或 .. 会写到项目外面去。
func TestAssetTargetsAreSafeRelativePaths(t *testing.T) {
	for _, a := range Assets() {
		assert.False(t, strings.HasPrefix(a.Target, "/"),
			"落点不能是绝对路径：%s", a.Target)
		assert.NotContains(t, a.Target, "..",
			"落点不能含 ..：%s", a.Target)
	}
}

// 落点不能重复——两份资产写同一个文件，后者会静默覆盖前者。
func TestAssetTargetsUnique(t *testing.T) {
	seen := map[string]string{}
	for _, a := range Assets() {
		if prev, ok := seen[a.Target]; ok {
			t.Fatalf("落点重复：%s 与 %s 都写 %s", prev, a.Source, a.Target)
		}
		seen[a.Target] = a.Source
	}
}

func TestSumIsStableAndPrefixed(t *testing.T) {
	s := Sum([]byte("hello"))
	assert.True(t, strings.HasPrefix(s, "sha256:"))
	assert.Equal(t, s, Sum([]byte("hello")))
	assert.NotEqual(t, s, Sum([]byte("hello!")))
}

// 我们刻意不写用户的 CLAUDE.md（那是他自己的流程文件），但 Claude Code 只读
// CLAUDE.md 而不读 AGENTS.md。所以 AGENTS.md 里必须留着那行接线说明——
// 少了它，想接上的人根本不知道有这个选项。
func TestAgentsMdTellsHowToWireUpClaudeCode(t *testing.T) {
	for _, a := range Assets() {
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
}

// 反过来钉住：CLAUDE.md 绝不能出现在资产清单里。
// 往使用者的流程文件里写东西是这套方案里唯一真正侵入的动作，已经明确拒绝。
func TestClaudeMdIsNotShipped(t *testing.T) {
	for _, a := range Assets() {
		assert.NotEqual(t, "CLAUDE.md", a.Target,
			"不装用户的 CLAUDE.md：那是他自己的流程文件")
	}
}
