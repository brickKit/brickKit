package inject

// 本文件测保留变量的警告：up 注入与 lint 共用。
//
// configSchema 的键就是环境变量名（附录 A10）；资源废除之后 DATABASE_* 等前缀不再保留。
// 判断本身在 manifest.ReservedHitFor，这里只测警告。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)


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

