package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

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
