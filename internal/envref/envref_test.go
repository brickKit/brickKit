package envref_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/envref"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) { v, ok := values[name]; return v, ok }
}

func TestExpandKeepsUnknownReferences(t *testing.T) {
	got := envref.Expand("pg://${USER}:${MISSING}@db", lookup(map[string]string{"USER": "app"}))
	assert.Equal(t, "pg://app:${MISSING}@db", got)
}

func TestRequiredDeduplicatesInOrder(t *testing.T) {
	assert.Equal(t, []string{"B", "A"}, envref.Required("${B}-${A}-${B}"))
	assert.False(t, envref.Has("$var:X"))
	assert.True(t, envref.Has("x${Y}"))
}

// ${NAME:-默认值}：变量取不到时用默认值——与 docker compose、kubectl 之外的 K8s 展开同一种语义。
func TestExpandUsesDefaultWhenUnset(t *testing.T) {
	got := envref.Expand("${A:-x}-${B:-y}-${C:-}", lookup(map[string]string{"A": "a"}))
	assert.Equal(t, "a-y-", got)
}

// 带默认值的引用永远展得开，所以不算"必须有定义"的变量。
func TestRequiredSkipsReferencesWithDefault(t *testing.T) {
	assert.Equal(t, []string{"A"}, envref.Required("${A}${B:-d}${A}"))
	assert.True(t, envref.Has("${B:-d}"), "带默认值的也是引用")
}

// 留给 compose 展开时，带默认值的引用原样保留，其余的 $ 仍转义。
func TestEscapeLiteralsKeepsDefaultReferences(t *testing.T) {
	assert.Equal(t, "$$5 ${A:-x}", envref.EscapeLiterals("$5 ${A:-x}"))
}

func TestExpandNodeHonoursSkip(t *testing.T) {
	t.Setenv("HOST_X", "h")
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: ${HOST_X}\nvars:\n  P: ${HOST_X}\nlist: [\"${HOST_X}\"]\n"), &root))
	envref.ExpandNode(&root, func(path []string) bool { return len(path) > 0 && path[0] == "vars" })

	var out map[string]any
	require.NoError(t, root.Decode(&out))
	assert.Equal(t, "h", out["a"])
	assert.Equal(t, "${HOST_X}", out["vars"].(map[string]any)["P"])
	assert.Equal(t, []any{"h"}, out["list"])
}
