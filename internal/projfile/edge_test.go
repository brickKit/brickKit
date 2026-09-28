package projfile

// 本文件是 brickkit.yaml 解析的边界输入：空文件、写坏的 YAML、空列表、很多组件、项目名里的数字。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 空文件要说"是空的"，而不是一句 YAML 库的 EOF。
func TestParseEmptyFile(t *testing.T) {
	for _, data := range []string{"", "\n\n", "# only a comment\n"} {
		_, err := Parse([]byte(data), "brickkit.yaml")
		require.Error(t, err, "%q", data)
		assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code, "%q", data)
		assert.Contains(t, clierr.As(err).Format(), "empty", "%q", data)
	}
}

// 写坏的 YAML 要指出行号：几百行的文件里，没有行号等于让人从头找。
func TestParseInvalidYAMLNamesTheLine(t *testing.T) {
	_, err := Parse([]byte("project: shop\nsources: []\ncomponents:\n  - id: a/b\n   version: 1.0.0\n"), "brickkit.yaml")
	require.Error(t, err)
	assert.Regexp(t, `line \d+`, clierr.As(err).Format())
}

// 刚 init 完的项目就是空列表：它是合法的起点，不是错误。
func TestParseEmptyListsAreValid(t *testing.T) {
	f, err := Parse([]byte("project: shop\nsources: []\ncomponents: []\n"), "brickkit.yaml")
	require.NoError(t, err)
	assert.Empty(t, f.Components)
	assert.Empty(t, f.Sources)
}

// 五十个组件照常解析，一个不丢。
func TestParseManyComponents(t *testing.T) {
	var b strings.Builder
	b.WriteString("project: shop\nsources: []\ncomponents:\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "  - id: scope%d/name%d\n    version: 1.0.%d\n", i, i, i)
	}
	f, err := Parse([]byte(b.String()), "brickkit.yaml")
	require.NoError(t, err)
	require.Len(t, f.Components, 50)
	assert.Equal(t, "scope49/name49", f.Components[49].ID)
}

// 项目名会成为 K8s 命名空间与 compose 项目名的一部分：数字是合法字符。
func TestValidateProjectNameAcceptsDigits(t *testing.T) {
	for _, name := range []string{"shop2026", "2shop", "a1-b2"} {
		assert.NoError(t, ValidateProjectName(name), name)
	}
}
