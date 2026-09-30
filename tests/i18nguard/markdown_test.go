package i18nguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

// 守卫 5：终端不渲染 Markdown。消息里写 **加粗**，用户看到的是两对星号，而不是强调。
// 需要强调的词靠措辞（"only publishing needs cosign"），不靠标记。
func TestCatalogMessagesCarryNoMarkdownBold(t *testing.T) {
	for _, lang := range i18n.SupportedLangs() {
		catalog := i18n.CatalogFor(lang)
		require.Greater(t, len(catalog), 1000, "%s 的目录只有 %d 条——读坏了，结论不可信", lang, len(catalog))
		for id, text := range catalog {
			assert.NotContains(t, text, "**", "%s 的 %s 带 Markdown 加粗，终端里会原样显示星号：%q", lang, id, abbreviate(text))
		}
	}
}
