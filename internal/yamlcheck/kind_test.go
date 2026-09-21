package yamlcheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

// KindName 是形状错误文案的一部分（"必须是数组格式（当前是 ××）"），
// 对每种节点类型直接断言一次。
func TestKindName(t *testing.T) {
	cases := map[yaml.Kind]string{
		yaml.ScalarNode:   "scalar",
		yaml.MappingNode:  "mapping",
		yaml.SequenceNode: "array",
		yaml.AliasNode:    "alias",
		yaml.DocumentNode: "unknown type",
	}
	for kind, want := range cases {
		assert.Equal(t, want, KindName(&yaml.Node{Kind: kind}))
	}
	assert.Equal(t, "unknown type", KindName(&yaml.Node{}))
}
