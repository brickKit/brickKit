package inject

// 本文件测保留变量：改名建议，以及 up 注入与 lint 共用的那份"哪些名字被保留"的判断。
//
// configSchema 的键就是环境变量名（附录 A10）；资源废除之后 DATABASE_* 等前缀不再保留。
// market-server 的 validator 仍是旧规则（附录 A2：市场代码本次不动），两边暂时不一致。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 建议必须**真的避得开**那条模式：*_ENDPOINT 是后缀规则，加前缀躲不开。
func TestRenameSuggestionAvoidsThePattern(t *testing.T) {
	cases := []struct{ key, pattern, want string }{
		{"DEPARTMENT_TREE_ENDPOINT", "*_ENDPOINT", "DEPARTMENT_TREE_BASE_URL"},
		{"NOTIFIER_ENDPOINT", "*_ENDPOINT", "NOTIFIER_BASE_URL"},
		{"_ENDPOINT", "*_ENDPOINT", "_ENDPOINT_VALUE"},
		{"PORT", "PORT", "CUSTOM_PORT"},
		{"COMPONENT_ID", "COMPONENT_ID", "CUSTOM_COMPONENT_ID"},
	}
	for _, c := range cases {
		got := renameSuggestion(c.key, c.pattern)
		assert.Equal(t, c.want, got, c.key)
		_, hit := staticReserved(got)
		assert.False(t, hit, "按建议改成 %q 之后仍然命中保留规则——照着做不管用的建议比不给更糟", got)
	}
}

func manifestWithConfigKeys(keys ...string) *manifest.Manifest {
	props := map[string]manifest.ConfigProperty{}
	for _, k := range keys {
		props[k] = manifest.ConfigProperty{Type: "string"}
	}
	return &manifest.Manifest{
		Metadata:     manifest.Metadata{ID: "demo/hello"},
		ConfigSchema: &manifest.ConfigSchema{Properties: props},
	}
}

func TestReservedKeyWarningsFlagsEveryReservedShape(t *testing.T) {
	m := manifestWithConfigKeys(
		"NOTIFIER_ENDPOINT", // *_ENDPOINT 后缀
		"COMPONENT_ID",      // 精确匹配
		"PORT",              // 精确匹配
		"DATABASE_HOST",     // 资源废除后不再保留
		"PAGE_SIZE",         // 不冲突
	)
	warnings := ReservedKeyWarnings(m)
	require.Len(t, warnings, 3)
	var keys []string
	for _, w := range warnings {
		assert.True(t, w.Warning, "是警告不是错误：写错一个配置项名不该让整个项目起不来")
		assert.Equal(t, clierr.CodeConfigConflict, w.Code)
		for _, d := range w.Details {
			if d.Key == "Config item" {
				keys = append(keys, d.Value)
			}
		}
	}
	assert.Equal(t, []string{"COMPONENT_ID", "NOTIFIER_ENDPOINT", "PORT"}, keys, "按配置项名字排序，输出稳定")
}

func TestReservedKeyWarningsNothingToCheck(t *testing.T) {
	assert.Nil(t, ReservedKeyWarnings(nil))
	assert.Nil(t, ReservedKeyWarnings(&manifest.Manifest{}))
	assert.Nil(t, ReservedKeyWarnings(manifestWithConfigKeys("PAGE_SIZE", "DATABASE_URL", "REDIS_HOST")))
}
