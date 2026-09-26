// 本文件是 Step 11 注入引擎的代码级测试：取值转换、各类资源、异常输入。
package inject_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
)

// YAML 里的整数常被解析成 float64，注入时不能变成 "20.000000"。
func TestNumericValuesKeepIntegerForm(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"AS_FLOAT64":  {Default: float64(20)},
		"AS_INT":      {Default: 30},
		"AS_FRACTION": {Default: 1.5},
	}}

	b := newBuilder(t)
	b.component(m, entry{})
	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "20", env["AS_FLOAT64"])
	assert.Equal(t, "30", env["AS_INT"])
	assert.Equal(t, "1.5", env["AS_FRACTION"])
}

// 数组、对象这类复杂值原样字符串化：CLI 不做类型转换也不报错。
func TestComplexValuesAreStringified(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"ALLOWED_ORIGINS": {Default: []any{"a.com", "b.com"}},
	}}

	b := newBuilder(t)
	b.component(m, entry{})

	assert.NotEmpty(t, envOf(t, b.build(), "people/basic")["ALLOWED_ORIGINS"])
}

// 组件没有 configSchema 时只注入平台变量。
func TestComponentWithoutConfigSchema(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), entry{})

	env := envOf(t, b.build(), "people/basic")

	assert.Len(t, env, 2)
}

// brickkit.yaml 里写了组件没声明的 config 项：忽略（与升级删字段同一条路径）。
func TestConfigKeyNotInSchemaIsIgnored(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080),
		entry{Config: map[string]any{"NOT_DECLARED": "x"}})

	assert.NotContains(t, envOf(t, b.build(), "people/basic"), "NOT_DECLARED")
}

// 空输入不 panic。
func TestBuildWithNilInputs(t *testing.T) {
	result, err := inject.Build(nil, nil, nil)

	require.NoError(t, err)
	assert.Empty(t, result.Components)
}

// 依赖组件没有额外端口时不生成多余变量。
func TestDependencyWithoutExtraPorts(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		entry{})
	b.component(simple("people/basic", "1.0.0", 8080), entry{})

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
	b.component(m, entry{Resources: &manifest.Resources{Requests: spec2("500m", "")}})

	quota := quotaOf(t, b.build(), "people/basic")

	assert.Equal(t, "500m", quota.Requests.CPU, "被覆盖")
	assert.Equal(t, "256Mi", quota.Requests.Memory, "没覆盖的沿用组件推荐值")
	assert.Equal(t, "2", quota.Limits.CPU)
	assert.Equal(t, "2Gi", quota.Limits.Memory)
}
