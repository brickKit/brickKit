package inject

// 本文件测保留变量的改名建议。
//
// 与 market-server 的 validator.TestSuggestionMatchesCLI 是**同一批用例**：
// 两处都会对同一个配置项名提建议（市场在发布时拒绝，CLI 在注入时警告），
// 说法不一致会让人以为自己改错了。两个 module 没法共享代码，
// 所以靠两边钉住同一组期望值——改一边就要改另一边。

import "testing"

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
