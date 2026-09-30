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

// 双引号文案写成了两行：YAML 会把换行折成一个空格（行尾空格也丢掉），读出来不是译者写的那句。
// 多行文案要写成 | 块。转义的 \n 照常可以用。
func TestParseCatalogRejectsADoubleQuotedTextBrokenAcrossLines(t *testing.T) {
	for _, bad := range []string{
		"a.x: \"line one\n  line two\"\n",
		"a.x:\n  \"line one\n  line two\"\n",
	} {
		_, _, err := parseCatalog([]byte(bad), "locales/xx.yaml")
		var ce *CatalogError
		require.True(t, errors.As(err, &ce), bad)
		assert.Equal(t, "a.x", ce.Key)
		assert.Contains(t, ce.Reason, "|")
	}
	_, texts, err := parseCatalog([]byte("a.x: \"one \\\" quote\\nand a newline\" # c\nb.y: \"#\"\n"), "locales/xx.yaml")
	require.NoError(t, err)
	assert.Equal(t, "one \" quote\nand a newline", texts["a.x"])
	assert.Equal(t, "#", texts["b.y"])
}

// | 块的第一行缩进多了：YAML 拿第一行的缩进当整块的缩进，多出来的空格悄悄没了。
// 块内容一律缩进两格；文案本身要以空格开头，就写缩进标记（|2）。
func TestParseCatalogRejectsALiteralBlockIndentedDeeper(t *testing.T) {
	_, _, err := parseCatalog([]byte("a.x: |-\n     ✅ foo\n"), "locales/xx.yaml")
	var ce *CatalogError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, "a.x", ce.Key)
	assert.Contains(t, ce.Reason, "|2")

	_, texts, err := parseCatalog([]byte("a.x: |2-\n     ✅ foo\nb.y: |\n\n  after a blank line\n    kept indent\n"), "locales/xx.yaml")
	require.NoError(t, err)
	assert.Equal(t, "   ✅ foo", texts["a.x"])
	assert.Equal(t, "\nafter a blank line\n  kept indent\n", texts["b.y"])
}

// 文件里混进一行 ---：YAML 把后面当成第二份文档，只读第一份就等于悄悄截掉后半截。直接报出那一行。
func TestParseCatalogRejectsASecondDocument(t *testing.T) {
	_, _, err := parseCatalog([]byte("a.x: \"one\"\n---\nb.y: \"two\"\n"), "locales/xx.yaml")
	var ce *CatalogError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, 3, ce.Line)
	assert.Contains(t, ce.Reason, "---")
}
