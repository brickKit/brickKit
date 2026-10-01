package agentsmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

func TestMain(m *testing.M) {
	i18n.SetCurrent(i18n.EN)
	os.Exit(m.Run())
}

var fence = strings.Repeat("`", 3)

func block(lang, body string) string {
	return "<!-- brickkit:managed:begin lang=" + lang + " -->\n" + body + "<!-- brickkit:managed:end -->\n"
}

func TestFindReadsLang(t *testing.T) {
	doc := "# P\n\nmine\n\n" + block("zh", "x\n")
	b, err := Find(doc)
	require.NoError(t, err)
	assert.Equal(t, "zh", b.Lang)
	assert.Equal(t, "# P\n\nmine\n\n", doc[:b.Start])
	assert.Equal(t, len(doc), b.End)
}

func TestFindIgnoresMarkersInFences(t *testing.T) {
	doc := "# P\n" + fence + "\n" + block("en", "example\n") + fence + "\n"
	_, err := Find(doc)
	assert.ErrorIs(t, err, ErrNoBlock)
}

func TestFindRejectsMalformed(t *testing.T) {
	for name, doc := range map[string]string{
		"begin only":   "<!-- brickkit:managed:begin lang=en -->\nx\n",
		"two begins":   block("en", "<!-- brickkit:managed:begin lang=en -->\n"),
		"end first":    "<!-- brickkit:managed:end -->\n" + block("en", ""),
		"bad language": block("EN!", ""),
		"no language":  "<!-- brickkit:managed:begin -->\n<!-- brickkit:managed:end -->\n",
	} {
		_, err := Find(doc)
		assert.ErrorIs(t, err, ErrMalformed, name)
	}
}

func TestReplaceKeepsCRLFOutside(t *testing.T) {
	doc := "# P\r\n\r\nmine\r\n" + strings.ReplaceAll(block("en", "old\n"), "\n", "\r\n") + "tail\r\n"
	b, err := Find(doc)
	require.NoError(t, err)
	out := Replace(doc, b, block("en", "new\n"))
	assert.True(t, strings.HasPrefix(out, "# P\r\n\r\nmine\r\n"))
	assert.True(t, strings.HasSuffix(out, "tail\r\n"))
	assert.Contains(t, out, "new\n")
	assert.NotContains(t, out, "old")
}

func TestRenderUsesRecordedLanguage(t *testing.T) {
	zh := Render(Content{Lang: "zh", Project: true})
	assert.Contains(t, zh, "lang=zh")
	assert.Contains(t, zh, "## 组件")
	en := Render(Content{Lang: "en", Project: true})
	assert.Contains(t, en, "## Components")
	ja := Render(Content{Lang: "ja", Project: true})
	assert.Contains(t, ja, "lang=ja", "the recorded language stays even without a template")
	assert.Contains(t, ja, "## Components")
}

func TestRenderKinds(t *testing.T) {
	comp := Render(Content{Lang: "en", Component: true})
	assert.Contains(t, comp, "reserved name")
	assert.NotContains(t, comp, "## Components")
	wb := Render(Content{Lang: "en", Component: true, Project: true})
	assert.Contains(t, wb, "reserved name")
	assert.Contains(t, wb, "## Components")
	assert.Equal(t, 1, strings.Count(wb, "## BrickKit"))
}

func TestRenderEscapesCells(t *testing.T) {
	out := Render(Content{Lang: "en", Project: true, Rows: []Row{{ID: "a/b", Version: "1.0.0", Does: "x | y\nz", Docs: "BRICKKIT.md", Home: ""}}})
	assert.Contains(t, out, "| a/b | 1.0.0 | x \\| y z | BRICKKIT.md | — |\n")
}

func TestRenderSixtyRowsStaysSmall(t *testing.T) {
	var rows []Row
	for i := 0; i < 60; i++ {
		rows = append(rows, Row{ID: "domain/component-name", Version: "1.0.26", Does: strings.Repeat("d", 40), Docs: "BRICKKIT.md +zh", Home: "https://git.example.com/domain-component-name"})
	}
	assert.Less(t, len(Render(Content{Lang: "en", Project: true, Rows: rows})), 12_000)
}

func TestParseRowsRoundTrip(t *testing.T) {
	want := Row{ID: "a/b", Version: "1.0.0", Does: "does | more", Docs: "BRICKKIT.md", Home: "https://h"}
	doc := Render(Content{Lang: "en", Project: true, Rows: []Row{{ID: "a/b", Version: "1.0.0", Does: "does | more", Docs: "BRICKKIT.md", Home: "https://h"}}})
	b, err := Find(doc)
	require.NoError(t, err)
	got := ParseRows(doc, b)["a/b@1.0.0"]
	assert.Equal(t, want, got)
	again := Render(Content{Lang: "en", Project: true, Rows: []Row{got}})
	assert.Equal(t, doc, again, "a kept row renders back byte for byte")
}

func TestSkeletonHasRequiredSectionsThenBlock(t *testing.T) {
	s := Skeleton("demo/quote", Content{Lang: "en", Component: true})
	assert.True(t, strings.HasPrefix(s, "# demo/quote\n"))
	for _, h := range []string{"## Code map", "## Build and test", "## Design decisions", "## Pitfalls", "## Before changing code"} {
		assert.Contains(t, s, h)
	}
	b, err := Find(s)
	require.NoError(t, err)
	assert.Greater(t, b.Start, strings.Index(s, "## Before changing code"))
	assert.Contains(t, s, "<!-- TODO:")
	p := Skeleton("shop", Content{Lang: "zh", Project: true})
	for _, h := range []string{"## 项目概述", "## 项目约定", "## 查找路由", "## 易错点"} {
		assert.Contains(t, p, h)
	}
}

func TestEnsureModes(t *testing.T) {
	root := t.TempDir()
	c := Content{Lang: "en", Project: true}

	res, err := Ensure(root, c, ModeInit, nil)
	require.NoError(t, err)
	assert.True(t, res.AgentsCreated)
	assert.True(t, res.ClaudeCreated)
	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	assert.Equal(t, ClaudeContent, string(claude))

	// 作者自己的文件：init 一个字节都不碰，只报问题；repair 追加
	mine := "# Mine\n\nnotes\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(mine), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("own\n"), 0o644))
	res, err = Ensure(root, c, ModeInit, nil)
	require.NoError(t, err)
	assert.ErrorIs(t, res.Problem, ErrNoBlock)
	assert.True(t, res.ClaudeMissingImport)
	got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.Equal(t, mine, string(got))

	res, err = Ensure(root, c, ModeRewrite, nil)
	require.NoError(t, err)
	assert.Equal(t, Result{Problem: ErrNoBlock}, res, "rewrite neither creates nor appends; it says the block is missing")

	res, err = Ensure(root, c, ModeRepair, nil)
	require.NoError(t, err)
	assert.True(t, res.BlockAppended)
	assert.True(t, res.ClaudeAppended)
	got, _ = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(string(got), mine))
	claude, _ = os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	assert.Equal(t, "own\n@AGENTS.md\n", string(claude))

	// rewrite：只改块里面
	c.Rows = []Row{{ID: "a/b", Version: "1.0.0", Does: "d", Docs: "—", Home: "—"}}
	res, err = Ensure(root, c, ModeRewrite, nil)
	require.NoError(t, err)
	assert.True(t, res.BlockRewritten)
	got, _ = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(string(got), mine))
	assert.Contains(t, string(got), "| a/b | 1.0.0 |")

	res, err = Ensure(root, c, ModeRewrite, nil)
	require.NoError(t, err)
	assert.False(t, res.BlockRewritten, "nothing changed, nothing written")
}

func TestEnsureMalformedBlockIsNotTouched(t *testing.T) {
	root := t.TempDir()
	broken := "# P\n<!-- brickkit:managed:begin lang=en -->\nno end\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(broken), 0o644))
	res, err := Ensure(root, Content{Lang: "en", Project: true}, ModeRepair, nil)
	require.NoError(t, err)
	assert.ErrorIs(t, res.Problem, ErrMalformed)
	got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.Equal(t, broken, string(got))
}

func TestEnsureKeepsRecordedLanguage(t *testing.T) {
	root := t.TempDir()
	_, err := Ensure(root, Content{Lang: "zh", Project: true}, ModeInit, nil)
	require.NoError(t, err)
	_, err = Ensure(root, Content{Lang: "en", Project: true}, ModeRewrite, nil)
	require.NoError(t, err)
	lang, ok := BlockLang(root)
	assert.True(t, ok)
	assert.Equal(t, "zh", lang, "a teammate with another CLI language must not flip the committed file")

	_, err = Ensure(root, Content{Lang: "en", ForceLang: true, Project: true}, ModeRepair, nil)
	require.NoError(t, err)
	lang, _ = BlockLang(root)
	assert.Equal(t, "en", lang, "skills update --lang switches it")
}

func TestEnsureReplacesLegacyAsset(t *testing.T) {
	root := t.TempDir()
	old := "# This project is assembled with BrickKit\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(old), 0o644))
	res, err := Ensure(root, Content{Lang: "en", Project: true, Title: "shop"}, ModeInit, isExactly(old))
	require.NoError(t, err)
	assert.True(t, res.LegacyReplaced)
	got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(string(got), "# shop\n"))
	assert.Contains(t, string(got), "## Overview")

	edited := root + "/x"
	require.NoError(t, os.MkdirAll(edited, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(edited, "AGENTS.md"), []byte(old+"my line\n"), 0o644))
	res, err = Ensure(edited, Content{Lang: "en", Project: true}, ModeInit, isExactly(old))
	require.NoError(t, err)
	assert.False(t, res.LegacyReplaced, "an edited copy of the old asset is the author's now")
}

func TestDocsCell(t *testing.T) {
	assert.Equal(t, "BRICKKIT.md +ja +zh", DocsCell(true, []string{"ja", "zh"}))
	assert.Equal(t, "+zh", DocsCell(false, []string{"zh"}))
	assert.Equal(t, "—", DocsCell(false, nil))
}

// 作者在四个反引号的代码块里举例（例子里又有三个反引号的块，和维护区的标记）：真正的维护区照样找得到。
func TestFindIgnoresMarkersInNestedFences(t *testing.T) {
	four := strings.Repeat("`", 4)
	example := four + "markdown\n" + block("en", "example\n") + fence + "\n" + four + "\n"
	doc := "# P\n\n" + example + "\n" + block("zh", "real\n")
	b, err := Find(doc)
	require.NoError(t, err)
	assert.Equal(t, "zh", b.Lang)
	assert.Contains(t, doc[b.Start:b.End], "real")
}

func TestFindAcceptsIndentedMarkers(t *testing.T) {
	_, err := Find("# P\n  <!-- brickkit:managed:begin lang=en -->\n  <!-- brickkit:managed:end -->\n")
	assert.NoError(t, err)
}

// 标记坏了的原因要跟着 CLI 语言说，并点出标记在哪几行：人照着就能去改。
func TestMalformedReasonIsLocalisedAndNamesLines(t *testing.T) {
	doc := "# P\n" + block("en", "x\n") + "<!-- brickkit:managed:begin lang=en -->\n"
	_, err := Find(doc)
	require.ErrorIs(t, err, ErrMalformed)

	prev := i18n.Current()
	defer i18n.SetCurrent(prev)
	i18n.SetCurrent(i18n.ZH)
	zh := Reason(err)
	assert.NotContains(t, zh, "malformed")
	assert.Contains(t, zh, "2")
	assert.Contains(t, zh, "5", "the stray begin marker's line")

	i18n.SetCurrent(i18n.EN)
	assert.Contains(t, Reason(err), "lines 2, 4, 5")
	assert.NotEmpty(t, Reason(ErrNoBlock))
}

// isExactly 是测试用的"旧版 CLI 装的那份"：内容一字不差才算。
func isExactly(old string) func([]byte) bool {
	return func(data []byte) bool { return string(data) == old }
}
