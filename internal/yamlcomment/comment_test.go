package yamlcomment_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/yamlcomment"
)

func TestBlockPrefixesEveryLine(t *testing.T) {
	assert.Equal(t, "# one\n# two\n", yamlcomment.Block("", "one\ntwo"))
	assert.Equal(t, "  # one\n  #   indented\n", yamlcomment.Block("  ", "one\n  indented"),
		"缩进前缀加在 # 之前，行内自带的缩进原样保留")
}

func TestBlockKeepsBlankCommentLinesWithoutTrailingSpace(t *testing.T) {
	assert.Equal(t, "# a\n#\n# b\n", yamlcomment.Block("", "a\n\nb"))
}

func TestBannerWrapsInRulesAndLeavesBlankLine(t *testing.T) {
	got := string(yamlcomment.Banner("hello\nworld"))
	rule := "# ============================================================\n"
	assert.Equal(t, rule+"# hello\n# world\n"+rule+"\n", got)
}

// 分节标题按形状认，与标题文字（哪种语言、哪一版措辞）无关；Banner 的横线不是标题。
func TestSectionIsRecognisedByShapeOnly(t *testing.T) {
	assert.Equal(t, "# === Required ===\n", yamlcomment.Section("", "Required"))
	for _, title := range []string{"Required: startup is blocked", "必填：没有值就无法启动", "any older wording"} {
		assert.True(t, yamlcomment.IsSection(yamlcomment.Section("  ", title)), title)
	}
	for _, line := range []string{"# ============================================================", "# a user note", "# === unterminated", "KEY: \"=== x ===\""} {
		assert.False(t, yamlcomment.IsSection(line), line)
	}
}
