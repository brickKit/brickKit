package i18n

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 译者自然写下的各种文字，读出来必须一字不差——包括首尾空格、结尾换行、制表符、%、#、": "。
func TestParseCatalogPreservesTextExactly(t *testing.T) {
	data := []byte("a.lead: \"  two leading spaces\"\n" +
		"a.trail: \"ends with newline\\n\"\n" +
		"a.tab: \"col\\tcol\"\n" +
		"a.verbs: \"%[1]s: 100% # not a comment\"\n" +
		"a.block: |\n  line one\n  line two\n" +
		"a.keep: |+\n  kept\n\n" +
		"a.strip: |-\n  no newline\n")
	keys, texts, err := parseCatalog(data, "locales/xx.yaml")
	require.NoError(t, err)
	assert.Equal(t, []string{"a.lead", "a.trail", "a.tab", "a.verbs", "a.block", "a.keep", "a.strip"}, keys)
	assert.Equal(t, "  two leading spaces", texts["a.lead"])
	assert.Equal(t, "ends with newline\n", texts["a.trail"])
	assert.Equal(t, "col\tcol", texts["a.tab"])
	assert.Equal(t, "%[1]s: 100% # not a comment", texts["a.verbs"])
	assert.Equal(t, "line one\nline two\n", texts["a.block"])
	assert.Equal(t, "kept\n\n", texts["a.keep"])
	assert.Equal(t, "no newline", texts["a.strip"])
}

// 纯量的其余写法会被 YAML 改写（yes、裸露的 %、折行）：不猜，报出文件、行号与 key。
func TestParseCatalogRejectsAmbiguousStyles(t *testing.T) {
	for _, bad := range []string{
		"a.plain: plain text\n",
		"a.single: 'single quoted'\n",
		"a.folded: >\n  folded\n  text\n",
		"a.map:\n  nested: \"x\"\n",
		"a.list: [\"x\"]\n",
	} {
		_, _, err := parseCatalog([]byte(bad), "locales/xx.yaml")
		var ce *CatalogError
		require.True(t, errors.As(err, &ce), bad)
		assert.Equal(t, 1, ce.Line, bad)
		assert.Contains(t, err.Error(), "locales/xx.yaml:1: ", bad)
	}
}

// 同一个 key 写了两遍：不许后一条悄悄覆盖前一条。
func TestParseCatalogRejectsDuplicateKeys(t *testing.T) {
	_, _, err := parseCatalog([]byte("a.x: \"one\"\nb.y: \"two\"\na.x: \"three\"\n"), "locales/xx.yaml")
	var ce *CatalogError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, 3, ce.Line)
	assert.Equal(t, "a.x", ce.Key)
	assert.Contains(t, ce.Reason, "line 1")
}

func TestParseCatalogRejectsNonMappingDocument(t *testing.T) {
	_, _, err := parseCatalog([]byte("- a\n- b\n"), "locales/xx.yaml")
	require.Error(t, err)
}
