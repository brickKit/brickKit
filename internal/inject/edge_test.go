// 本文件是 Step 11 注入引擎的代码级测试：取值转换、各类资源、异常输入。
package inject_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
)

// YAML 里的整数常被解析成 float64，注入时不能变成 "20.000000"。
func TestNumericValuesKeepIntegerForm(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"asFloat64":  {Default: float64(20)},
		"asInt":      {Default: 30},
		"asFraction": {Default: 1.5},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "20", env["AS_FLOAT64"])
	assert.Equal(t, "30", env["AS_INT"])
	assert.Equal(t, "1.5", env["AS_FRACTION"])
}

// 数组、对象这类复杂值原样字符串化：CLI 不做类型转换也不报错。
func TestComplexValuesAreStringified(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"allowedOrigins": {Default: []any{"a.com", "b.com"}},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})

	assert.NotEmpty(t, envOf(t, b.build(), "people/basic")["ALLOWED_ORIGINS"])
}

// 消息队列、搜索、邮件三类资源的变量名（006 §5.2）。
func TestRemainingResourceKinds(t *testing.T) {
	m := simple("erp/backend", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{Resources: []manifest.ResourceDep{
		{Kind: "mq", Engine: "rabbitmq"},
		{Kind: "search", Engine: "elasticsearch"},
		{Kind: "smtp", Engine: "smtp"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: "mq", Engine: "rabbitmq", ID: "mq-main", Host: "rabbit", Port: 5672,
		Username: "guest", Password: "guest",
		Bindings: []config.Binding{{ComponentID: "erp/backend", Database: "/erp"}},
	})
	b.resource(config.Resource{
		Kind: "search", Engine: "elasticsearch", ID: "es", Host: "es", Port: 9200,
		Bindings: []config.Binding{{ComponentID: "erp/backend", Database: "erp-index"}},
	})
	b.resource(config.Resource{
		Kind: "smtp", Engine: "smtp", ID: "mail", Host: "smtp.example.com", Port: 587,
		Username: "noreply", Password: "pw",
		Bindings: []config.Binding{{ComponentID: "erp/backend"}},
	})

	env := envOf(t, b.build(), "erp/backend")

	assert.Equal(t, "rabbit", env["MQ_HOST"])
	assert.Equal(t, "5672", env["MQ_PORT"])
	assert.Equal(t, "/erp", env["MQ_VHOST"])
	assert.Equal(t, "es", env["SEARCH_HOST"])
	assert.Equal(t, "erp-index", env["SEARCH_INDEX"])
	assert.Equal(t, "smtp.example.com", env["SMTP_HOST"])
	assert.Equal(t, "noreply", env["SMTP_USER"])
}

// 资源里没填的字段不注入空值：组件据此判断"这项没提供"。
func TestResourceEmptyFieldsAreNotInjected(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{
		Resources: []manifest.ResourceDep{{Kind: "database", Engine: "postgresql"}},
	}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: "database", Engine: "postgresql", ID: "pg", Host: "localhost",
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people"}},
	})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "localhost", env["DATABASE_HOST"])
	assert.NotContains(t, env, "DATABASE_PORT", "没填端口就不注入")
	assert.NotContains(t, env, "DATABASE_USER")
	assert.NotContains(t, env, "DATABASE_PASSWORD")
}

// 认不出的资源类型不注入任何变量，也不 panic。
func TestUnknownResourceKindIsIgnored(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.resource(config.Resource{
		Kind: "quantum-storage", ID: "q", Host: "q", Port: 1,
		Bindings: []config.Binding{{ComponentID: "people/basic"}},
	})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "people/basic", env["COMPONENT_ID"])
	assert.Len(t, env, 2, "只有平台通用变量：%v", env)
}

// 组件没有 configSchema 时只注入平台变量。
func TestComponentWithoutConfigSchema(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	env := envOf(t, b.build(), "people/basic")

	assert.Len(t, env, 2)
}

// brickkit.yaml 里写了组件没声明的 config 项：忽略（与升级删字段同一条路径）。
func TestConfigKeyNotInSchemaIsIgnored(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{Config: map[string]any{"notDeclared": "x"}})

	assert.NotContains(t, envOf(t, b.build(), "people/basic"), "NOT_DECLARED")
}

// 空输入不 panic。
func TestBuildWithNilInputs(t *testing.T) {
	result, err := inject.Build(nil, nil, nil)

	require.NoError(t, err)
	assert.Empty(t, result.Components)
}

// EnvVarName 的转换规则必须与市场侧一致，否则会出现
// "发布时说没冲突、注入时却冲突"的怪事。
func TestEnvVarNameConversion(t *testing.T) {
	cases := map[string]string{
		"defaultPageSize":        "DEFAULT_PAGE_SIZE",
		"departmentTreeEndpoint": "DEPARTMENT_TREE_ENDPOINT",
		"enableV2Api":            "ENABLE_V2_API",
		"httpTimeoutMs":          "HTTP_TIMEOUT_MS",
		"kebab-case-key":         "KEBAB_CASE_KEY",
		"snake_case_key":         "SNAKE_CASE_KEY",
		"dotted.key":             "DOTTED_KEY",
		"ALREADYUPPER":           "ALREADYUPPER",
		"a":                      "A",
	}

	for input, want := range cases {
		assert.Equal(t, want, inject.EnvVarName(input), "输入 %q", input)
	}
}

// 依赖组件没有额外端口时不生成多余变量。
func TestDependencyWithoutExtraPorts(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	env := envOf(t, b.build(), "erp/backend")

	assert.Len(t, env, 3, "COMPONENT_ID / COMPONENT_VERSION / PEOPLE_BASIC_ENDPOINT：%v", env)
}

// 资源配额只覆盖 requests 时，limits 仍走组件推荐值。
func TestResourceQuotaPartialOverride(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.Resources = &manifest.Resources{
		Requests: spec2("200m", "256Mi"),
		Limits:   spec2("2", "2Gi"),
	}

	b := newBuilder(t)
	b.component(m, config.Component{Resources: &manifest.Resources{Requests: spec2("500m", "")}})

	quota := quotaOf(t, b.build(), "people/basic")

	assert.Equal(t, "500m", quota.Requests.CPU, "被覆盖")
	assert.Equal(t, "256Mi", quota.Requests.Memory, "没覆盖的沿用组件推荐值")
	assert.Equal(t, "2", quota.Limits.CPU)
	assert.Equal(t, "2Gi", quota.Limits.Memory)
}
