package mdtext

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fence = strings.Repeat("`", 3)

func TestLinksSkipsCodeAndFindsEveryForm(t *testing.T) {
	body := "See [a](a.md) and [b](b.md \"t\").\n" +
		fence + "\n[x](inside.md)\n" + fence + "\n" +
		"`[y](inline.md)` <a href=\"c.md\">c</a>\n" +
		"[ref]: d.md\n"
	var got []string
	for _, l := range Links(body) {
		got = append(got, l.Target)
	}
	assert.Equal(t, []string{"a.md", "b.md", "c.md", "d.md"}, got)
	assert.Equal(t, 1, Links(body)[0].Line)
	assert.Equal(t, 6, Links(body)[3].Line)
}

func TestTildeFenceClosesOnlyWithTilde(t *testing.T) {
	body := "~~~\n" + fence + "\n[x](in.md)\n~~~\n[y](out.md)\n"
	ls := Links(body)
	assert.Len(t, ls, 1)
	assert.Equal(t, "out.md", ls[0].Target)
}

func TestMapLinksRewritesOnlyOutsideCode(t *testing.T) {
	body := "[a](a.md)\n" + fence + "\n[b](b.md)\n" + fence + "\n"
	got := MapLinks(body, func(t string) string { return "x/" + t })
	assert.Equal(t, "[a](x/a.md)\n"+fence+"\n[b](b.md)\n"+fence+"\n", got)
}

func TestSectionsLevelTwoOutsideFences(t *testing.T) {
	body := "# T\n\n## One\n\na\n" + fence + "\n## not a heading\n" + fence + "\n## Two\nb\n"
	s := Sections(body)
	assert.Len(t, s, 2)
	assert.Equal(t, "One", s[0].Heading)
	assert.Equal(t, 3, s[0].Line)
	assert.Contains(t, s[0].Body, "## not a heading")
	assert.Equal(t, "Two", s[1].Heading)
	assert.Equal(t, "b\n", s[1].Body)
}

func TestProseLinesDropCode(t *testing.T) {
	body := "keep TODO\n" + fence + "\nTODO hidden\n" + fence + "\nalso `TODO` gone\n"
	var texts []string
	for _, l := range ProseLines(body) {
		texts = append(texts, strings.TrimSpace(l.Text))
	}
	assert.Equal(t, []string{"keep TODO", "also  gone", ""}, texts)
	assert.Equal(t, 5, ProseLines(body)[1].N)
}

func TestTableCellsAndInlineCode(t *testing.T) {
	assert.Equal(t, []string{"`a/`", "Owns"}, TableCells("| `a/` | Owns |"))
	assert.Nil(t, TableCells("|---|:---:|"))
	assert.Nil(t, TableCells("plain"))
	assert.Equal(t, []string{"x/y.go", "z"}, InlineCode("see `x/y.go` and `z` here"))
}

func TestTableCellsKeepEscapedPipes(t *testing.T) {
	assert.Equal(t, []string{"a/b", `x \| y`, "z"}, TableCells(`| a/b | x \| y | z |`))
}

// CommonMark：关闭围栏要同一种字符、不短于开头、后面不跟别的；四个反引号的块里可以有三个反引号的例子。
func TestNestedFencesCloseOnlyOnAMatchingFence(t *testing.T) {
	four := strings.Repeat("`", 4)
	body := four + "markdown\n" + fence + "\n[in](inner.md)\n" + fence + "\n[still](in.md)\n" + four + "\n[out](out.md)\n"
	ls := Links(body)
	require.Len(t, ls, 1)
	assert.Equal(t, "out.md", ls[0].Target)
	assert.Equal(t, []bool{true, true, true, true, true, true, false, false}, CodeLines(body))

	// 带信息串的那一行不能关闭代码块
	body = fence + "\n" + fence + "go\n[in](x.md)\n" + fence + "\n[out](y.md)\n"
	assert.Equal(t, "y.md", Links(body)[len(Links(body))-1].Target)
}
