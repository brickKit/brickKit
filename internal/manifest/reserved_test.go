package manifest

// 本文件测平台保留变量的判断：up 注入与 lint 据此警告，市场发布时据此拒收，都是这一份。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 建议必须**真的避得开**那条模式：*_ENDPOINT 是后缀规则，加前缀躲不开。
func TestRenameSuggestionAvoidsThePattern(t *testing.T) {
	cases := []struct{ key, want string }{
		{"DEPARTMENT_TREE_ENDPOINT", "DEPARTMENT_TREE_BASE_URL"},
		{"NOTIFIER_ENDPOINT", "NOTIFIER_BASE_URL"},
		{"_ENDPOINT", "_ENDPOINT_VALUE"},
		{"PORT", "CUSTOM_PORT"},
		{"COMPONENT_ID", "CUSTOM_COMPONENT_ID"},
	}
	for _, c := range cases {
		hit, ok := ReservedHitFor(c.key)
		assert.True(t, ok, c.key)
		assert.Equal(t, c.want, hit.Suggestion, c.key)
		_, stillReserved := ReservedHitFor(hit.Suggestion)
		assert.False(t, stillReserved, "按建议改成 %q 之后仍然命中保留规则——照着做不管用的建议比不给更糟", hit.Suggestion)
	}
}

func TestReservedHitForPatterns(t *testing.T) {
	for key, pattern := range map[string]string{
		"COMPONENT_ID": "COMPONENT_ID", "COMPONENT_VERSION": "COMPONENT_VERSION", "PORT": "PORT",
		"BRICKKIT_SERVED_MEMBERS": "BRICKKIT_SERVED_MEMBERS", "BRICKKIT_SERVED_MEMBERS_CONFIG": "BRICKKIT_SERVED_MEMBERS_CONFIG",
		"X_ENDPOINT": "*_ENDPOINT",
	} {
		hit, ok := ReservedHitFor(key)
		assert.True(t, ok, key)
		assert.Equal(t, pattern, hit.Pattern, key)
	}
	for _, key := range []string{"PAGE_SIZE", "DATABASE_URL", "REDIS_HOST", "ENDPOINT_URL", "PORTS"} {
		_, ok := ReservedHitFor(key)
		assert.False(t, ok, "%s 不是保留名（资源废除后 DATABASE_* 等前缀也不再保留）", key)
	}
}

func TestReservedConfigKeys(t *testing.T) {
	m := &Manifest{ConfigSchema: &ConfigSchema{Properties: map[string]ConfigProperty{
		"PORT": {Type: "integer"}, "PAGE_SIZE": {Type: "integer"},
		"NOTIFIER_ENDPOINT": {Type: "string"}, "COMPONENT_ID": {Type: "string"},
	}}}
	assert.Equal(t, []ReservedHit{
		{Key: "COMPONENT_ID", Pattern: "COMPONENT_ID", Suggestion: "CUSTOM_COMPONENT_ID"},
		{Key: "NOTIFIER_ENDPOINT", Pattern: "*_ENDPOINT", Suggestion: "NOTIFIER_BASE_URL"},
		{Key: "PORT", Pattern: "PORT", Suggestion: "CUSTOM_PORT"},
	}, m.ReservedConfigKeys(), "按键排序，输出稳定")

	var none *Manifest
	assert.Nil(t, none.ReservedConfigKeys())
	assert.Nil(t, (&Manifest{}).ReservedConfigKeys())
}
