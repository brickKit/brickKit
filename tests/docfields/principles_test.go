// 本文件守着 docs/{en,zh}/architecture/design-principles.md 与根目录 AGENTS.md /
// AGENTS.zh.md §4「十二条设计原则」**不漂移**。
//
// # 为什么要有它
//
// 同一份原则清单现在有两个家：AGENTS.md 是写给 AI 的压缩版（一页表格，一条一行），
// design-principles.md 是写给人的论证版（每条原则一节：是什么、为什么、代价、拒绝了什么）。
// 两份内容天然要互相引用，也就天然会分叉——一边新增了第十三条、改了某条的名字，
// 另一边没人记得跟着改。它不会让任何测试失败，只会让读到不同版本的人对"BrickKit
// 到底有哪几条原则"得出不同答案。
//
// # 判据
//
// AGENTS 文件 §4 的表格是清单的来源：每行第一列 `**原则名**` 一条。论证版文档里
// 「十二条原则」那一节下，必须恰好有同样的十二个三级标题——名字逐字相同，顺序相同。
// 标题带编号，形如 `### 3. 环境无关`：去掉编号后的名字才与 AGENTS 表格逐字对照；
// 编号本身必须是按出现顺序连续的 1、2、3……——它是给人看的目录，错位了比没有更糟。
// 英文与中文各自对着自己的 AGENTS 文件比：两个语言树是对等的，不是翻译附属，
// 原则的措辞在两边本来就不同（"精确优于隐式" / "Explicit over implicit"）。
//
// 形状上照着 reference_test.go：真相来源是另一份文件本身，不是又抄一份清单；
// 检测器自己要有一条测试证明两个方向都抓得到，且解析坏了会直接失败而不是给出一个
// 漂亮的全绿。
package docfields_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// principleCount 是 AGENTS §4 里原则的条数。写死它是为了让解析坏掉时失败：
// 抽出 0 条时，"文档与清单一致"是空真。
const principleCount = 12

// principlePairs 是每种语言的一对文件：清单在哪，论证版文档里哪一节放三级标题。
var principlePairs = []struct {
	lang    string
	agents  string
	section string
}{
	{"en", "AGENTS.md", "The twelve principles"},
	{"zh", "AGENTS.zh.md", "十二条原则"},
}

// principleRow 匹配 §4 表格里的一行：第一列是加粗的原则名。
// 表头（Principle / 原则）与分隔行不加粗，所以不会被抽进来。
var principleRow = regexp.MustCompile(`^\|\s*\*\*(.+?)\*\*\s*\|`)

// agentsPrinciples 抽出 AGENTS 文件 §4 表格里的原则名，按出现顺序。
//
// §4 从 "## 4." 开头的标题起，到下一个 "### " 或 "## " 止——"### 4.1" 的
// 拒绝清单也是一张表，第一列同样加粗，必须截在它前面。
func agentsPrinciples(markdown string) []string {
	var out []string
	inSection := false
	for _, line := range strings.Split(markdown, "\n") {
		switch {
		case strings.HasPrefix(line, "## 4."):
			inSection = true
			continue
		case inSection && (strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "## ")):
			return out
		}
		if !inSection {
			continue
		}
		if m := principleRow.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// docPrinciples 抽出论证版文档里 section 那一节（"## <section>"）下的全部三级标题。
func docPrinciples(markdown, section string) []string {
	var out []string
	inSection := false
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "## ") {
			inSection = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == section
			continue
		}
		if inSection && strings.HasPrefix(line, "### ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "### ")))
		}
	}
	return out
}

// numberedHeading 匹配 "3. 环境无关" 这样的标题：编号、点、空格、名字。
var numberedHeading = regexp.MustCompile(`^(\d+)\.\s+(.+)$`)

// splitNumbered 把带编号的标题拆成名字与问题清单。编号必须从 1 开始、按出现顺序
// 连续递增；没有编号、编号跳号或重复，都记成 problems。返回的 names 已去掉编号，
// 没有编号的标题原样保留，这样名字对照仍能给出有用的报错。
func splitNumbered(headings []string) (names []string, problems []string) {
	for i, h := range headings {
		m := numberedHeading.FindStringSubmatch(h)
		if m == nil {
			problems = append(problems, fmt.Sprintf("标题「%s」没有编号，应为「%d. %s」", h, i+1, h))
			names = append(names, h)
			continue
		}
		if n, _ := strconv.Atoi(m[1]); n != i+1 {
			problems = append(problems, fmt.Sprintf("标题「%s」的编号是 %d，按顺序应为 %d", h, n, i+1))
		}
		names = append(names, m[2])
	}
	return names, problems
}

// nameDrift 按名字比较两份清单：missing 是 want 有、got 没有的，extra 反过来。
// 原则清单与错误码清单共用它。
func nameDrift(want, got []string) (missing, extra []string) {
	contains := func(list []string, name string) bool {
		for _, item := range list {
			if item == name {
				return true
			}
		}
		return false
	}
	for _, name := range want {
		if !contains(got, name) {
			missing = append(missing, name)
		}
	}
	for _, name := range got {
		if !contains(want, name) {
			extra = append(extra, name)
		}
	}
	return missing, extra
}

// 论证版文档必须为 AGENTS §4 的每一条原则各写一节，名字与顺序都一致。
func TestPrinciplesDocMirrorsAgents(t *testing.T) {
	for _, pair := range principlePairs {
		agentsBody, err := os.ReadFile(filepath.Join(repoRoot, pair.agents))
		require.NoError(t, err)
		want := agentsPrinciples(string(agentsBody))
		require.Len(t, want, principleCount,
			"%s §4 抽出了 %d 条原则，应该是 %d——agentsPrinciples 坏了，这条测试的结论不可信",
			pair.agents, len(want), principleCount)

		rel := filepath.Join("docs", pair.lang, "architecture", "design-principles.md")
		docBody, err := os.ReadFile(filepath.Join(repoRoot, rel))
		require.NoError(t, err, "%s 不存在：这份文档是 %s §4 十二条原则的论证版", rel, pair.agents)
		got, numberingProblems := splitNumbered(docPrinciples(string(docBody), pair.section))
		for _, problem := range numberingProblems {
			t.Errorf("%s：%s", rel, problem)
		}

		missing, extra := nameDrift(want, got)
		for _, name := range missing {
			t.Errorf("%s：%s §4 有原则「%s」，文档「%s」一节下没有对应的三级标题\n"+
				"   去掉编号后，标题要与 AGENTS 表格第一列逐字相同", rel, pair.agents, name, pair.section)
		}
		for _, name := range extra {
			t.Errorf("%s：文档「%s」一节下有三级标题「%s」，%s §4 里没有这条原则\n"+
				"   原则改名了/被删了，或者这个标题不该放在这一节里", rel, pair.section, name, pair.agents)
		}
		if len(missing) == 0 && len(extra) == 0 {
			require.Equal(t, want, got, "%s：十二条原则的顺序要与 %s §4 一致", rel, pair.agents)
		}
	}
}

// 检测器自己要能两个方向都抓得到，并且认得出 §4.1 那张同样加粗第一列的表——
// 否则上面那条测试的"全绿"没有意义。
func TestPrincipleDriftDetectorCatchesBothDirections(t *testing.T) {
	agents := "## 4. Principles\n\n" +
		"| Principle | Explanation |\n| --- | --- |\n" +
		"| **A** | first |\n| **B** | second |\n\n" +
		"### 4.1 Things we refuse\n\n" +
		"| **Not-a-principle** | third |\n"
	require.Equal(t, []string{"A", "B"}, agentsPrinciples(agents),
		"§4.1 之后的表格不能算进原则清单")

	doc := "# Title\n\n## Intro\n\n### Z\n\n## The twelve principles\n\n### 1. A\n\n### 2. X\n\n## Next\n\n### Y\n"
	headings := docPrinciples(doc, "The twelve principles")
	require.Equal(t, []string{"1. A", "2. X"}, headings, "只认目标那一节下的三级标题")
	got, problems := splitNumbered(headings)
	require.Equal(t, []string{"A", "X"}, got, "名字要去掉编号再与清单对照")
	require.Empty(t, problems, "1、2 是合规的编号")

	missing, extra := nameDrift([]string{"A", "B"}, got)
	require.Equal(t, []string{"B"}, missing, "清单有、文档没写的原则必须被报出来")
	require.Equal(t, []string{"X"}, extra, "文档写了、清单里没有的原则必须被报出来")

	// 编号本身也要有人守：跳号、没编号都得报出来，重复编号同样算错位。
	_, problems = splitNumbered([]string{"1. A", "3. B", "C", "3. D"})
	require.Len(t, problems, 3, "跳号（3 应为 2）、没有编号（C）、重复编号（3 应为 4）各一条")
}
