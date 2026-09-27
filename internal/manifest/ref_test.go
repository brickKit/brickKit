package manifest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/manifest"
)

// 组件引用 <组件ID>@<版本> 全仓库只有这一种拆法：在第一个 @ 处切开。组件 ID 不许含 @，
// 多出来的 @ 落进版本里，由精确版本校验拦下。
func TestSplitRef(t *testing.T) {
	cases := []struct {
		ref, id, version string
		hasVersion       bool
	}{
		{"erp/api@1.2.0", "erp/api", "1.2.0", true},
		{"erp/api", "erp/api", "", false},
		{"erp/api@", "erp/api", "", true},
		{"@1.2.0", "", "1.2.0", true},
		{"erp/api@1.2.0@x", "erp/api", "1.2.0@x", true},
		{"", "", "", false},
	}
	for _, c := range cases {
		id, version, hasVersion := manifest.SplitRef(c.ref)
		assert.Equal(t, []any{c.id, c.version, c.hasVersion}, []any{id, version, hasVersion}, c.ref)
	}
}
