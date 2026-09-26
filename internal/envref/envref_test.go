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

func TestNamesDeduplicatesInOrder(t *testing.T) {
	assert.Equal(t, []string{"B", "A"}, envref.Names("${B}-${A}-${B}"))
	assert.False(t, envref.Has("$var:X"))
	assert.True(t, envref.Has("x${Y}"))
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
