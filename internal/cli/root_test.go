package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 语言不再写死在 root.go 里：哪种语言的目录给了 cobra 的模板与说明，就用它；空字符串保留 cobra 自带的英文。
func TestCobraTextsComeFromTheCatalog(t *testing.T) {
	t.Cleanup(func() { i18n.SetCurrent(i18n.EN) })
	t.Setenv(i18n.EnvLang, "zh")
	root := NewRootCommand(NewOptions())
	assert.Equal(t, i18n.T(msgid.CobraUsageTemplate), root.UsageTemplate())
	t.Setenv(i18n.EnvLang, "en")
	root = NewRootCommand(NewOptions())
	assert.Equal(t, (&cobra.Command{}).UsageTemplate(), root.UsageTemplate(), "英文目录是空字符串：保留 cobra 自带的模板")
}

// completion 与它的四个子命令的帮助来自目录（两种语言各一份），说的是 install.sh 的做法：
// 放进各 shell 自己会找的用户级目录，从不改 rc 文件。
func TestCompletionHelpComesFromTheCatalog(t *testing.T) {
	t.Cleanup(func() { i18n.SetCurrent(i18n.EN) })
	shells := []struct {
		name        string
		short, long msgid.ID
	}{
		{"bash", msgid.CobraCompletionBashShort, msgid.CobraCompletionBashLong},
		{"zsh", msgid.CobraCompletionZshShort, msgid.CobraCompletionZshLong},
		{"fish", msgid.CobraCompletionFishShort, msgid.CobraCompletionFishLong},
		{"powershell", msgid.CobraCompletionPowershellShort, msgid.CobraCompletionPowershellLong},
	}
	for _, lang := range []string{"en", "zh"} {
		t.Setenv(i18n.EnvLang, lang)
		root := NewRootCommand(NewOptions())
		comp, _, err := root.Find([]string{"completion"})
		require.NoError(t, err)
		assert.Equal(t, i18n.T(msgid.CobraCompletionLong), comp.Long, lang)
		for _, sh := range shells {
			c, _, err := root.Find([]string{"completion", sh.name})
			require.NoError(t, err, sh.name)
			assert.Equal(t, i18n.T(sh.short), c.Short, "%s %s", lang, sh.name)
			assert.Equal(t, i18n.T(sh.long), c.Long, "%s %s", lang, sh.name)
			assert.NotEmpty(t, c.Long)
			assert.Equal(t, i18n.T(msgid.CobraCompletionNoDescriptions), c.Flags().Lookup("no-descriptions").Usage)
		}
		assert.Contains(t, i18n.T(msgid.CobraCompletionBashLong), "~/.local/share/bash-completion/completions/brickkit", lang)
		assert.Contains(t, i18n.T(msgid.CobraCompletionZshLong), "fpath=(~/.zsh/completions $fpath)", lang)
		assert.Contains(t, i18n.T(msgid.CobraCompletionFishLong), "~/.config/fish/completions/brickkit.fish", lang)
	}
}
