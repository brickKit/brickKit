package cli

// 本文件测 mode: local 探测启动命令失败时的报错：runcmd 只给结构化的原因，
// 话由 CLI 按当前语言说——中文模式下不能在中文标题下面夹一句英文诊断。

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/runcmd"
)

// 每个原因在每种语言里都有说法，而且真的翻译了（不是照抄源语言）。
func TestEveryDetectionReasonHasAMessage(t *testing.T) {
	src := i18n.CatalogFor(i18n.SourceLang())
	for _, reason := range runcmd.Reasons() {
		id, ok := detectionReasonMessages[reason]
		require.True(t, ok, "runcmd.%s 没有对应的文案", reason)
		assert.NotEmpty(t, src[id], "%s", reason)
		for _, lang := range i18n.SupportedLangs() {
			if lang == i18n.SourceLang() {
				continue
			}
			text := i18n.CatalogFor(lang)[id]
			assert.NotEmpty(t, text, "%s %s", lang, reason)
			assert.NotEqual(t, src[id], text, "%s 的 %s 没有翻译", lang, reason)
		}
	}
}

func TestDetectionErrorsSpeakTheCurrentLanguage(t *testing.T) {
	prev := i18n.Current()
	i18n.SetCurrent(i18n.ZH)
	t.Cleanup(func() { i18n.SetCurrent(prev) })

	ref := resolver.Ref{ID: "demo/hello", Version: "1.0.0"}
	var problems []runcmd.Problem
	for _, r := range runcmd.Reasons() {
		problems = append(problems, runcmd.Problem{Language: "go", Reason: r, Detail: "go.mod", Options: []string{"cmd/a", "cmd/b"}})
	}
	cases := map[string]error{
		"no language":    &runcmd.NoCommandError{Dir: "/src/hello"},
		"no command":     &runcmd.NoCommandError{Dir: "/src/hello", Problems: problems},
		"ambiguous":      &runcmd.AmbiguousError{Candidates: []runcmd.Candidate{{Language: "go", Argv: []string{"go", "run", "."}}, {Language: "node", Argv: []string{"npm", "start"}}}},
		"unknown":        &runcmd.UnknownLanguageError{Language: "cobol"},
		"not a dir":      &runcmd.DirError{Dir: "/src/hello"},
		"unreadable dir": &runcmd.DirError{Dir: "/src/hello", Err: errors.New("permission denied")},
	}
	for name, err := range cases {
		out := clierr.As(detectionError(ref, err)).Format()
		assert.NotContains(t, out, "runcmd:", "%s：%s", name, out)
		assert.NotContains(t, out, "no start command", "%s：%s", name, out)
		for _, r := range runcmd.Reasons() {
			assert.NotContains(t, out, string(r), "%s：原因要说成人话，不是枚举值 %s", name, r)
		}
	}
	missing := clierr.As(programMissingError(ref, &runcmd.ProgramMissingError{Program: "go"})).Format()
	assert.NotContains(t, missing, "runcmd:", missing)
	assert.Contains(t, missing, "go")
	assert.True(t, strings.Contains(missing, "找不到"), missing)
}
