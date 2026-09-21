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

func TestBannerWrapsInRulesAndLeavesBlankLine(t *testing.T) {
	got := string(yamlcomment.Banner("hello\nworld"))
	rule := "# ============================================================\n"
	assert.Equal(t, rule+"# hello\n# world\n"+rule+"\n", got)
}
