package inject

// 本文件测保留变量：改名建议，以及 up 注入与 lint 共用的那份"哪些名字被保留"的判断。
//
// 改名建议的用例与 market-server 的 validator.TestSuggestionMatchesCLI 是**同一批用例**：
// 两处都会对同一个配置项名提建议（市场在发布时拒绝，CLI 在注入时警告），
// 说法不一致会让人以为自己改错了。两个 module 没法共享代码，
// 所以靠两边钉住同一组期望值——改一边就要改另一边。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 建议必须**真的避得开**那条模式。
//
// 从前一律加 custom 前缀：对 DATABASE_* 这类前缀模式有效，
// 对 *_ENDPOINT 这类后缀模式完全无效——customNotifierEndpoint 照样以
// _ENDPOINT 结尾，改完再跑还是同一条警告。
func TestRenameSuggestionAvoidsThePattern(t *testing.T) {
	cases := []struct{ key, pattern, want string }{
		{"departmentTreeEndpoint", "*_ENDPOINT", "departmentTreeBaseUrl"},
		{"notifierEndpoint", "*_ENDPOINT", "notifierBaseUrl"},
		{"endpoint", "*_ENDPOINT", "endpointValue"},
		{"databaseHost", "DATABASE_*", "customDatabaseHost"},
		{"redisPort", "REDIS_*", "customRedisPort"},
		// 换掉 Endpoint 后缀剩下的词根，恰好又是某个资源类型的前缀词——
		// 换后缀躲得开 *_ENDPOINT，躲不开 {前缀}_*，两条规则撞在一起，
		// 得再加一层 custom 前缀（brickKit 反馈：renameSuggestion 对
		// <资源类型>Endpoint 形的 key 给出的建议仍会撞资源前缀）。
		{"redisEndpoint", "*_ENDPOINT", "customRedisBaseUrl"},
		{"databaseEndpoint", "*_ENDPOINT", "customDatabaseBaseUrl"},
		{"storageEndpoint", "*_ENDPOINT", "customStorageBaseUrl"},
		{"smtpEndpoint", "*_ENDPOINT", "customSmtpBaseUrl"},
		{"mqEndpoint", "*_ENDPOINT", "customMqBaseUrl"},
		{"searchEndpoint", "*_ENDPOINT", "customSearchBaseUrl"},
	}
	for _, c := range cases {
		got := renameSuggestion(c.key, c.pattern)
		if got != c.want {
			t.Errorf("renameSuggestion(%q, %q) = %q，期望 %q", c.key, c.pattern, got, c.want)
		}
		// 照着建议改一次就该不再冲突——否则这条建议照着做不管用
		b := &envBuilder{}
		if pattern, hit := b.matchReserved(EnvVarName(got)); hit {
			t.Errorf("按建议改成 %q 之后仍然命中 %s——照着做不管用的建议比不给更糟", got, pattern)
		}
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
		"databaseHost",     // DATABASE_HOST：资源前缀
		"notifierEndpoint", // NOTIFIER_ENDPOINT：*_ENDPOINT 后缀
		"componentId",      // COMPONENT_ID：精确匹配
		"port",             // PORT：精确匹配（Plan 4a）
		"pageSize",         // 不冲突
	)
	warnings := ReservedKeyWarnings(m)
	require.Len(t, warnings, 4)
	for _, w := range warnings {
		assert.True(t, w.Warning, "是警告不是错误：写错一个配置项名不该让整个项目起不来")
		assert.Equal(t, clierr.CodeConfigConflict, w.Code)
	}

	var keys []string
	for _, w := range warnings {
		for _, d := range w.Details {
			if d.Key == "Config item" {
				keys = append(keys, d.Value)
			}
		}
	}
	assert.Equal(t, []string{"componentId", "databaseHost", "notifierEndpoint", "port"}, keys, "按配置项名字排序，输出稳定")
}

func TestReservedKeyWarningsNamesTheComponent(t *testing.T) {
	warnings := ReservedKeyWarnings(manifestWithConfigKeys("databaseHost"))
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Format(), "demo/hello")
	assert.Contains(t, warnings[0].Format(), "DATABASE_HOST")
}

func TestReservedKeyWarningsNothingToCheck(t *testing.T) {
	assert.Nil(t, ReservedKeyWarnings(nil))
	assert.Nil(t, ReservedKeyWarnings(&manifest.Manifest{}))
	assert.Nil(t, ReservedKeyWarnings(manifestWithConfigKeys("pageSize", "timeoutSeconds")))
}

// up 注入与 lint 必须是同一份规则：没有 envPrefix 时，matchReserved 与 staticReserved 处处一致。
func TestStaticReservedMatchesEnvBuilderWithoutPrefixes(t *testing.T) {
	b := &envBuilder{}
	for _, name := range []string{
		"DATABASE_HOST", "REDIS_URL", "MQ_VHOST", "STORAGE_BUCKET", "SEARCH_INDEX", "SMTP_HOST",
		"COMPONENT_ID", "COMPONENT_VERSION", "BRICKKIT_SERVED_MEMBERS", "BRICKKIT_SERVED_MEMBERS_CONFIG", "PORT",
		"X_ENDPOINT", "PAGE_SIZE", "DATABASE", "ENDPOINT", "",
	} {
		wantPattern, wantHit := staticReserved(name)
		gotPattern, gotHit := b.matchReserved(name)
		assert.Equal(t, wantHit, gotHit, name)
		assert.Equal(t, wantPattern, gotPattern, name)
	}
}

func TestEnvBuilderStillRespectsUserDefinedPrefixes(t *testing.T) {
	b := &envBuilder{reservedPrefixes: []string{"PRIMARY_"}}
	pattern, hit := b.matchReserved("PRIMARY_HOST")
	assert.True(t, hit)
	assert.Equal(t, "PRIMARY_*", pattern)
	_, hit = staticReserved("PRIMARY_HOST")
	assert.False(t, hit, "envPrefix 是项目里才知道的，静态规则不含它")
}
