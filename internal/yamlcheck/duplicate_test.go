package yamlcheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

func duplicates(t *testing.T, doc string) map[string]string {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &root), "重复键在节点层面照样解得出来")
	got := map[string]string{}
	DuplicateKeys(&root, func(field, message string) { got[field] = message })
	return got
}

func TestDuplicateKeysNamesTheLaterOne(t *testing.T) {
	got := duplicates(t, "target: docker\ntarget: k8s\n")
	assert.Equal(t, map[string]string{"target": i18n.T(msgid.YamlcheckDuplicateKey, 2, 1)}, got)
}

// 整棵树都查：嵌套映射、数组里的条目、自由映射（vars）都算。
func TestDuplicateKeysWalksTheWholeTree(t *testing.T) {
	got := duplicates(t, `vars:
  A: 1
  A: 2
components:
  - id: a/b
    mode: local
    mode: debug
  - id: c/d
`)
	assert.Contains(t, got, "vars.A")
	assert.Contains(t, got, "components[0].mode")
	assert.Len(t, got, 2)
}

func TestDuplicateKeysAcceptsDistinctKeys(t *testing.T) {
	assert.Empty(t, duplicates(t, "a: 1\nb:\n  a: 1\nc:\n  - a: 1\n  - a: 2\n"),
		"同名键在不同映射里不算重复")
}
