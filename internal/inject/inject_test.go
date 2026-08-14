// 本文件是 Step 11「环境变量注入引擎」的业务行为测试，
// 覆盖开发计划 11.1–11.18，以及 004 §5.6（注入规则）、§5.6.1（保留变量冲突）、
// §5.6.2（资源配额合并，延后项 P2 的前半部分）、006 §5（资源连接变量）。
package inject_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// ============================================================
// 夹具
// ============================================================

// stubProvider 让测试走真实解析器建图。
type stubProvider map[string]*manifest.Manifest

func (p stubProvider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	m, ok := p[id+"@"+version]
	if !ok {
		return nil, assertMissing(id, version)
	}
	return m, nil
}

func assertMissing(id, version string) error {
	return &missingError{id: id, version: version}
}

type missingError struct{ id, version string }

func (e *missingError) Error() string { return "夹具里没有 " + e.id + "@" + e.version }

// builder 用链式写法搭出一套组件 + 配置。
type builder struct {
	t        *testing.T
	provider stubProvider
	roots    []resolver.Ref
	cfg      *config.Config
}

func newBuilder(t *testing.T) *builder {
	return &builder{t: t, provider: stubProvider{}, cfg: &config.Config{Project: "my-erp"}}
}

// component 登记一个组件的 Manifest，并写入 brickkit.yaml。
func (b *builder) component(m *manifest.Manifest, entry config.Component) *builder {
	b.t.Helper()
	if m.Deployment.Type == "" {
		m.Deployment.Type = "container"
	}
	if m.Deployment.Image == "" {
		m.Deployment.Image = "registry.example.com/" + m.Metadata.Version
	}
	b.provider[m.Metadata.ID+"@"+m.Metadata.Version] = m

	entry.ID, entry.Version = m.Metadata.ID, m.Metadata.Version
	b.cfg.Components = append(b.cfg.Components, entry)
	b.roots = append(b.roots, resolver.Ref{ID: entry.ID, Version: entry.Version})
	return b
}

func (b *builder) resource(r config.Resource) *builder {
	b.cfg.Resources = append(b.cfg.Resources, r)
	return b
}

// build 解析依赖、算级联、跑注入。
func (b *builder) build() *inject.Result {
	b.t.Helper()

	graph, err := resolver.New(b.provider).Resolve(context.Background(), b.roots...)
	require.NoError(b.t, err)

	states, err := cascade.Compute(b.cfg, graph)
	require.NoError(b.t, err)

	result, err := inject.Build(b.cfg, graph, states)
	require.NoError(b.t, err)
	return result
}

// envOf 取某个组件的环境变量表。
func envOf(t *testing.T, r *inject.Result, id string) map[string]string {
	t.Helper()
	for _, c := range r.Components {
		if c.Ref.ID == id {
			return c.EnvMap()
		}
	}
	require.Failf(t, "结果里没有该组件", "%s", id)
	return nil
}

// simple 造一个最简单的组件 Manifest。
func simple(id, version string, port int) *manifest.Manifest {
	return &manifest.Manifest{
		Metadata:   manifest.Metadata{ID: id, Name: id, Version: version},
		Deployment: manifest.Deployment{Type: "container", Image: "registry.example.com/x:" + version, Port: port},
	}
}

// dependsOn 给 Manifest 加一条强依赖。
func dependsOn(m *manifest.Manifest, id, version string) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Components = append(m.Dependencies.Components,
		manifest.ComponentDep{ID: id, Version: version})
	return m
}

// weaklyDependsOn 给 Manifest 加一条弱依赖。
func weaklyDependsOn(m *manifest.Manifest, id, version string) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Components = append(m.Dependencies.Components,
		manifest.ComponentDep{ID: id, Version: version, Optional: true})
	return m
}

func off() *bool { v := false; return &v }
func on() *bool  { v := true; return &v }

// ============================================================
// 11.9 / 11.10 平台通用变量
// ============================================================

func TestComponentIdentityVariables(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.2.0", 8080), config.Component{})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "people/basic", env["COMPONENT_ID"], "11.9：变量名不带版本，值是组件 ID")
	assert.Equal(t, "1.2.0", env["COMPONENT_VERSION"])
}

// ============================================================
// 11.1 / 11.2 依赖地址注入
// ============================================================

// 11.1：变量名基于组件 ID（不带版本），值指向版本化服务名（004 §5.6 关键规则 1/2）。
func TestDependencyEndpointInjection(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "department/tree", "1.0.0"),
		config.Component{})
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{})

	env := envOf(t, b.build(), "erp/backend")

	assert.Equal(t, "http://department-tree-1-0-0:8080", env["DEPARTMENT_TREE_ENDPOINT"])
}

// 11.2 额外端口：PEOPLE_BASIC_GRPC_ENDPOINT。
func TestExtraPortEndpointInjection(t *testing.T) {
	dep := simple("people/basic", "1.0.0", 8080)
	dep.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(dep, config.Component{})

	env := envOf(t, b.build(), "erp/backend")

	assert.Equal(t, "http://people-basic-1-0-0:8080", env["PEOPLE_BASIC_ENDPOINT"])
	assert.Equal(t, "http://people-basic-1-0-0:9090", env["PEOPLE_BASIC_GRPC_ENDPOINT"])
}

// 11.18 组件 ID 含中划线时的变量名。
func TestEnvVarNameForHyphenatedComponentID(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "infra/redis-event-bus", "1.0.0"),
		config.Component{})
	b.component(simple("infra/redis-event-bus", "1.0.0", 6379), config.Component{})

	env := envOf(t, b.build(), "erp/backend")

	assert.Contains(t, env, "INFRA_REDIS_EVENT_BUS_ENDPOINT")
	assert.Equal(t, "http://infra-redis-event-bus-1-0-0:6379", env["INFRA_REDIS_EVENT_BUS_ENDPOINT"])
}

// ============================================================
// 11.3 / 11.4 弱依赖缺失
// ============================================================

// 弱依赖没启动时**完全不注入**，不是注入空值 ——
// 组件靠 os.environ.get() 判断"有没有"，注入空串会让它以为有（002 §3.4）。
func TestWeakDependencyNotRunningIsNotInjected(t *testing.T) {
	weak := simple("infra/redis-event-bus", "1.0.0", 6379)
	weak.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "metrics", Port: 9100}}

	b := newBuilder(t)
	b.component(weaklyDependsOn(simple("erp/backend", "1.0.0", 8080), "infra/redis-event-bus", "1.0.0"),
		config.Component{})
	b.component(weak, config.Component{Enabled: off()})

	env := envOf(t, b.build(), "erp/backend")

	assert.NotContains(t, env, "INFRA_REDIS_EVENT_BUS_ENDPOINT", "11.3")
	assert.NotContains(t, env, "INFRA_REDIS_EVENT_BUS_METRICS_ENDPOINT", "11.4：额外端口也不注入")
	for name := range env {
		assert.NotContains(t, name, "REDIS_EVENT_BUS", "不该残留任何该组件的变量：%s", name)
	}
}

// 弱依赖确实在启动时，照常注入。
func TestRunningWeakDependencyIsInjected(t *testing.T) {
	b := newBuilder(t)
	b.component(weaklyDependsOn(simple("erp/backend", "1.0.0", 8080), "infra/redis-event-bus", "1.0.0"),
		config.Component{})
	b.component(simple("infra/redis-event-bus", "1.0.0", 6379), config.Component{Enabled: on()})

	env := envOf(t, b.build(), "erp/backend")

	assert.Equal(t, "http://infra-redis-event-bus-1-0-0:6379", env["INFRA_REDIS_EVENT_BUS_ENDPOINT"])
}

// 级联跳过的组件本身不产出环境变量表：它这次根本不启动。
func TestSkippedComponentProducesNoEnv(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("erp/backend", "1.0.0", 8080), config.Component{Enabled: off()})

	result := b.build()

	assert.Empty(t, result.Components, "不启动的组件不该出现在注入结果里")
}

// ============================================================
// 11.7 / 11.8 / 11.17 配置项注入
// ============================================================

// configSchema 是默认值来源，brickkit.yaml 的 config 是覆盖值。
func TestConfigDefaultsAndOverrides(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"defaultPageSize": {Type: "integer", Default: 20},
		"enableAudit":     {Type: "boolean", Default: true},
		"cacheTtlSeconds": {Type: "integer", Default: 300},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{
		"defaultPageSize": 50,
		"enableAudit":     false,
	}})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "50", env["DEFAULT_PAGE_SIZE"], "11.7：覆盖值优先")
	assert.Equal(t, "false", env["ENABLE_AUDIT"], "11.7：false 也是有效覆盖，不能当成没写")
	assert.Equal(t, "300", env["CACHE_TTL_SECONDS"], "11.8：没覆盖的用默认值")
}

// 11.17 驼峰转大写下划线。
func TestConfigKeyToEnvVarName(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"defaultPageSize": {Default: 20},
		"httpTimeoutMs":   {Default: 3000},
		"enableV2Api":     {Default: false},
		"snake_case_key":  {Default: "x"},
		"kebab-case-key":  {Default: "y"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	env := envOf(t, b.build(), "people/basic")

	for _, name := range []string{
		"DEFAULT_PAGE_SIZE", "HTTP_TIMEOUT_MS", "ENABLE_V2_API",
		"SNAKE_CASE_KEY", "KEBAB_CASE_KEY",
	} {
		assert.Contains(t, env, name)
	}
}

// CLI 不校验 config 的值类型（004 §5.6 关键规则 4）：原样转成字符串注入。
func TestConfigValuesAreInjectedVerbatim(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"defaultPageSize": {Type: "integer", Default: 20},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{"defaultPageSize": "五十"}})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "五十", env["DEFAULT_PAGE_SIZE"],
		"configSchema 是配置说明书不是安检机：填错类型也原样注入，后果由使用者承担")
}

// 没有默认值、也没有覆盖的配置项不注入 ——
// 注入空串会让组件以为"配置过了但值是空的"。
func TestConfigWithoutDefaultOrOverrideIsNotInjected(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"apiKey": {Type: "string"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})

	assert.NotContains(t, envOf(t, b.build(), "people/basic"), "API_KEY")
}

// ============================================================
// 11.13–11.16 升级时 configSchema 变更
// ============================================================

// 新版本新增配置项 → 用新默认值（使用者没覆盖过它）。
func TestUpgradeAddedConfigKeyUsesDefault(t *testing.T) {
	m := simple("people/basic", "2.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"defaultPageSize":  {Default: 20},
		"newInThisVersion": {Default: "hello"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{"defaultPageSize": 50}})

	env := envOf(t, b.build(), "people/basic")
	assert.Equal(t, "hello", env["NEW_IN_THIS_VERSION"], "11.13")
	assert.Equal(t, "50", env["DEFAULT_PAGE_SIZE"])
}

// 新版本删掉了配置项，而 brickkit.yaml 里还留着旧的覆盖 →
// 静默忽略，不报错（11.14）。使用者升级时不该被一堆遗留配置卡住。
func TestUpgradeRemovedConfigKeyIsSilentlyIgnored(t *testing.T) {
	m := simple("people/basic", "2.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"defaultPageSize": {Default: 20},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{
		"defaultPageSize": 50,
		"removedInV2":     "旧配置",
	}})

	result := b.build()
	env := envOf(t, result, "people/basic")

	assert.NotContains(t, env, "REMOVED_IN_V2")
	assert.Empty(t, result.Warnings, "这是升级的正常现象，不该产生警告")
}

// 新版本改了默认值且使用者没覆盖 → 用新默认值（11.15）。
func TestUpgradeChangedDefaultTakesEffect(t *testing.T) {
	m := simple("people/basic", "2.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"defaultPageSize": {Default: 100},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})

	assert.Equal(t, "100", envOf(t, b.build(), "people/basic")["DEFAULT_PAGE_SIZE"])
}

// 新版本改了默认值但使用者覆盖过 → 覆盖值优先（11.16）。
func TestUpgradeOverrideBeatsNewDefault(t *testing.T) {
	m := simple("people/basic", "2.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"defaultPageSize": {Default: 100},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{"defaultPageSize": 50}})

	assert.Equal(t, "50", envOf(t, b.build(), "people/basic")["DEFAULT_PAGE_SIZE"])
}

// ============================================================
// 11.5 保留变量冲突（004 §5.6.1）
// ============================================================

// 冲突时"警告但跳过，平台注入的值优先"——
// 报错阻断会让一个配置项名字写错就整个项目起不来。
func TestReservedVariableConflictWarnsAndSkips(t *testing.T) {
	m := dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0")
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"departmentTreeEndpoint": {Type: "string", Default: "http://用户写死的地址"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{})

	result := b.build()
	env := envOf(t, result, "people/basic")

	assert.Equal(t, "http://department-tree-1-0-0:8080", env["DEPARTMENT_TREE_ENDPOINT"],
		"平台注入的值必须赢")
	require.Len(t, result.Warnings, 1)

	warning := result.Warnings[0].Format()
	assert.Contains(t, warning, "departmentTreeEndpoint")
	assert.Contains(t, warning, "DEPARTMENT_TREE_ENDPOINT")
	assert.Contains(t, warning, "people/basic")
	assert.Contains(t, warning, "⚠️", "是警告不是错误")
}

// 平台通用变量与资源前缀同样受保护。
func TestReservedPatternsCoverPlatformAndResourceVariables(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"componentId":  {Default: "冒充组件 ID"},
		"databaseHost": {Default: "冒充数据库地址"},
		"redisPort":    {Default: 1234},
		"smtpUser":     {Default: "x"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})

	result := b.build()
	env := envOf(t, result, "people/basic")

	assert.Equal(t, "people/basic", env["COMPONENT_ID"], "平台值优先")
	assert.NotContains(t, env, "DATABASE_HOST", "没绑数据库就不该凭空出现这个变量")
	assert.NotContains(t, env, "REDIS_PORT")
	assert.NotContains(t, env, "SMTP_USER")
	assert.Len(t, result.Warnings, 4, "四个冲突各有一条警告")
}

// envPrefix 是使用者在 brickkit.yaml 里定的，市场发布时无从校验，
// 只能在注入时防御（004 §5.6.1 的 {envPrefix}_* 那一行）。
func TestConflictWithUserDefinedEnvPrefix(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{}
	m.Dependencies.Resources = []manifest.ResourceDep{{Kind: "database", Engine: "postgresql"}}
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"primaryDatabaseName": {Default: "冒充主库名"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: "database", Engine: "postgresql", ID: "pg-primary",
		Host: "primary-db", Port: 5432,
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people", EnvPrefix: "PRIMARY"}},
	})

	result := b.build()
	env := envOf(t, result, "people/basic")

	assert.Equal(t, "people", env["PRIMARY_DATABASE_NAME"], "资源注入的值优先")
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0].Format(), "PRIMARY_")
}

// ============================================================
// 11.11 / 11.12 资源连接变量（006 §5）
// ============================================================

func TestResourceConnectionVariables(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{}
	m.Dependencies.Resources = []manifest.ResourceDep{{Kind: "database", Engine: "postgresql"}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: "database", Engine: "postgresql", ID: "postgres-main",
		Host: "localhost", Port: 5432, Username: "dev", Password: "secret",
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people"}},
	})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "localhost", env["DATABASE_HOST"])
	assert.Equal(t, "5432", env["DATABASE_PORT"])
	assert.Equal(t, "people", env["DATABASE_NAME"])
	assert.Equal(t, "dev", env["DATABASE_USER"])
	assert.Equal(t, "secret", env["DATABASE_PASSWORD"])
}

// 每类资源有各自的变量名（006 §5.2）。
func TestResourceVariableNamesPerKind(t *testing.T) {
	m := simple("erp/backend", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{}
	m.Dependencies.Resources = []manifest.ResourceDep{
		{Kind: "cache", Engine: "redis"},
		{Kind: "storage", Engine: "s3"},
	}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: "cache", Engine: "redis", ID: "redis-main", Host: "redis", Port: 6379, Password: "pw",
		Bindings: []config.Binding{{ComponentID: "erp/backend"}},
	})
	b.resource(config.Resource{
		Kind: "storage", Engine: "s3", ID: "rustfs", Host: "http://rustfs:9000",
		Username: "ak", Password: "sk",
		Bindings: []config.Binding{{ComponentID: "erp/backend", Database: "brickkit-artifacts"}},
	})

	env := envOf(t, b.build(), "erp/backend")

	assert.Equal(t, "redis", env["REDIS_HOST"])
	assert.Equal(t, "6379", env["REDIS_PORT"])
	assert.Equal(t, "pw", env["REDIS_PASSWORD"])
	assert.Equal(t, "http://rustfs:9000", env["STORAGE_ENDPOINT"])
	assert.Equal(t, "brickkit-artifacts", env["STORAGE_BUCKET"])
	assert.Equal(t, "ak", env["STORAGE_ACCESS_KEY"])
	assert.Equal(t, "sk", env["STORAGE_SECRET_KEY"])
}

// 11.12 一个组件绑多个同类资源时用 envPrefix 区分（006 §5.7）。
func TestMultipleResourcesUseEnvPrefix(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{}
	m.Dependencies.Resources = []manifest.ResourceDep{{Kind: "database", Engine: "postgresql"}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: "database", Engine: "postgresql", ID: "postgres-primary",
		Host: "primary-db", Port: 5432,
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people", EnvPrefix: "PRIMARY"}},
	})
	b.resource(config.Resource{
		Kind: "database", Engine: "postgresql", ID: "postgres-archive",
		Host: "archive-db", Port: 5432,
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people_archive", EnvPrefix: "ARCHIVE"}},
	})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "primary-db", env["PRIMARY_DATABASE_HOST"])
	assert.Equal(t, "people", env["PRIMARY_DATABASE_NAME"])
	assert.Equal(t, "archive-db", env["ARCHIVE_DATABASE_HOST"])
	assert.Equal(t, "people_archive", env["ARCHIVE_DATABASE_NAME"])
	assert.NotContains(t, env, "DATABASE_HOST", "都带前缀时不该再出现无前缀的变量")
}

// 资源只注入给绑定了它的组件。
func TestResourceIsInjectedOnlyToBoundComponents(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{})
	b.resource(config.Resource{
		Kind: "database", Engine: "postgresql", ID: "postgres-main", Host: "localhost", Port: 5432,
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people"}},
	})

	result := b.build()

	assert.Contains(t, envOf(t, result, "people/basic"), "DATABASE_HOST")
	assert.NotContains(t, envOf(t, result, "department/tree"), "DATABASE_HOST")
}

// ============================================================
// 11.6 资源配额合并（004 §5.6.2，延后项 P2）
// ============================================================

func spec2(cpu, memory string) *manifest.ResourceSpec {
	return &manifest.ResourceSpec{CPU: cpu, Memory: memory}
}

// 优先级：brickkit.yaml > component.yaml > CLI 默认值。
func TestResourceQuotaMergePriority(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.Resources = &manifest.Resources{
		Requests: spec2("200m", "256Mi"),
		Limits:   spec2("1", "1Gi"),
	}

	b := newBuilder(t)
	b.component(m, config.Component{Resources: &manifest.Resources{
		Limits: spec2("", "2Gi"),
	}})

	quota := quotaOf(t, b.build(), "people/basic")

	assert.Equal(t, "2Gi", quota.Limits.Memory, "使用者覆盖优先")
	assert.Equal(t, "1", quota.Limits.CPU, "使用者没写的字段沿用组件推荐值")
	assert.Equal(t, "200m", quota.Requests.CPU)
	assert.Equal(t, "256Mi", quota.Requests.Memory)
}

// 组件没声明、使用者也没覆盖 → CLI 默认值（004 §5.6.2 第 4 条）。
func TestResourceQuotaFallsBackToCLIDefaults(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	quota := quotaOf(t, b.build(), "people/basic")

	assert.Equal(t, "100m", quota.Requests.CPU)
	assert.Equal(t, "128Mi", quota.Requests.Memory)
	assert.Equal(t, "500m", quota.Limits.CPU)
	assert.Equal(t, "512Mi", quota.Limits.Memory)
}

// 组件声明了推荐值、使用者没覆盖 → 用组件的。
func TestResourceQuotaUsesManifestWhenNotOverridden(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.Resources = &manifest.Resources{Requests: spec2("300m", "384Mi")}

	b := newBuilder(t)
	b.component(m, config.Component{})

	quota := quotaOf(t, b.build(), "people/basic")

	assert.Equal(t, "300m", quota.Requests.CPU)
	assert.Equal(t, "384Mi", quota.Requests.Memory)
	assert.Equal(t, "500m", quota.Limits.CPU, "没声明的那半边仍用 CLI 默认值")
}

func quotaOf(t *testing.T, r *inject.Result, id string) manifest.Resources {
	t.Helper()
	for _, c := range r.Components {
		if c.Ref.ID == id {
			require.NotNil(t, c.Resources.Requests)
			require.NotNil(t, c.Resources.Limits)
			return c.Resources
		}
	}
	require.Failf(t, "结果里没有该组件", "%s", id)
	return manifest.Resources{}
}

// ============================================================
// 输出的稳定性
// ============================================================

// 环境变量按名字排序输出：生成的部署文件不能因为 map 遍历顺序而每次都变，
// 否则 git diff 全是噪音，也没法判断"这次改了什么"。
func TestEnvVarsAreSortedForStableOutput(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"zebra": {Default: 1}, "alpha": {Default: 2}, "middle": {Default: 3},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	result := b.build()

	var names []string
	for _, v := range result.Components[0].Env {
		names = append(names, v.Name)
	}
	assert.Equal(t, []string{
		"ALPHA", "COMPONENT_ID", "COMPONENT_VERSION", "MIDDLE", "ZEBRA",
	}, names)
}

// 同一份输入跑两次结果必须完全一致。
func TestBuildIsDeterministic(t *testing.T) {
	build := func() []inject.Var {
		m := simple("people/basic", "1.0.0", 8080)
		m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
			"a": {Default: 1}, "b": {Default: 2}, "c": {Default: 3}, "d": {Default: 4},
		}}
		b := newBuilder(t)
		b.component(m, config.Component{})
		return b.build().Components[0].Env
	}

	assert.Equal(t, build(), build())
}
