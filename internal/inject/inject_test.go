// 本文件是 「环境变量注入引擎」的业务行为测试，
// 包括注入规则、保留变量冲突、资源配额合并。
package inject_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/resolver"
	"gopkg.in/yaml.v3"
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

// entry 是测试里对一个组件的全部项目侧声明：部署条目 + 它的 config/ 文件内容。
type entry struct {
	Mode      string
	Config    map[string]any
	Resources *manifest.Resources
	Labels    map[string]string
	// Members 非空表示这个组件是外壳，承载这些组件 ID。
	Members []string
}

// builder 用链式写法搭出一套组件 + 三层文件。
type builder struct {
	t        *testing.T
	provider stubProvider
	roots    []resolver.Ref
	entries  []entry
	vars     map[string]any
}

func newBuilder(t *testing.T) *builder {
	return &builder{t: t, provider: stubProvider{}}
}

// component 登记一个组件的 Manifest 与它在项目里的声明。
func (b *builder) component(m *manifest.Manifest, e entry) *builder {
	b.t.Helper()
	if m.Deployment.Type == "" {
		m.Deployment.Type = "container"
	}
	if m.Deployment.Image == "" {
		m.Deployment.Image = "registry.example.com/" + m.Metadata.Version
	}
	b.provider[m.Metadata.ID+"@"+m.Metadata.Version] = m
	b.roots = append(b.roots, resolver.Ref{ID: m.Metadata.ID, Version: m.Metadata.Version})
	b.entries = append(b.entries, e)
	return b
}

// sharedVars 写 config/vars.yaml。
func (b *builder) sharedVars(vars map[string]any) *builder {
	b.vars = vars
	return b
}

// project 把登记过的组件写成三层文件并装载。
func (b *builder) project() *project.Project {
	b.t.Helper()
	root := b.t.TempDir()
	decl := &projfile.File{Project: "my-erp"}
	deploy := struct {
		Target     string                 `yaml:"target"`
		Components []deployfile.Component `yaml:"components"`
	}{Target: deployfile.TargetDocker}
	files := projecttest.Files{}
	// 成员条目嵌在外壳条目下面
	shellOf := map[string]bool{}
	for _, e := range b.entries {
		for _, m := range e.Members {
			shellOf[m] = true
		}
	}
	for i, ref := range b.roots {
		e := b.entries[i]
		c := projfile.Component{ID: ref.ID, Version: ref.Version}
		if len(e.Members) > 0 {
			c.Kind = projfile.KindShell
		}
		decl.Components = append(decl.Components, c)
		if shellOf[ref.ID] {
			continue
		}
		var members []deployfile.Entry
		for _, id := range e.Members {
			for j, r := range b.roots {
				if r.ID == id {
					m := b.entries[j]
					members = append(members, deployfile.Entry{
						ID: r.String(), Mode: m.Mode, Resources: m.Resources, Labels: m.Labels,
					})
				}
			}
		}
		deploy.Components = append(deploy.Components, deployfile.Component{Entry: deployfile.Entry{
			ID: ref.String(), Mode: e.Mode, Resources: e.Resources, Labels: e.Labels,
		}, Members: members})
		if len(e.Config) > 0 {
			files["config/"+configdir.FileName(ref.ID, ref.Version)] = mustYAML(b.t, e.Config)
		}
	}
	files["brickkit.yaml"] = mustYAML(b.t, decl)
	files["deploy.yaml"] = mustYAML(b.t, deploy)
	if b.vars != nil {
		files["config/vars.yaml"] = mustYAML(b.t, b.vars)
	}
	projecttest.Write(b.t, root, files)
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(b.t, err)
	return p
}

func mustYAML(t *testing.T, v any) string {
	t.Helper()
	data, err := yaml.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

// build 解析依赖、算级联、跑注入。
func (b *builder) build() *inject.Result {
	b.t.Helper()
	result, err := b.run()
	require.NoError(b.t, err)
	return result
}

// buildErr 跑注入并要求它失败，返回错误。
func (b *builder) buildErr() error {
	b.t.Helper()
	_, err := b.run()
	require.Error(b.t, err, "本用例期待注入失败")
	return err
}

func (b *builder) run() (*inject.Result, error) {
	b.t.Helper()
	p := b.project()
	graph, err := resolver.New(b.provider).Resolve(context.Background(), b.roots...)
	require.NoError(b.t, err)
	states, err := cascade.Compute(p, graph)
	require.NoError(b.t, err)
	return inject.Build(p, graph, states)
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

// varOf 取某个组件某一条环境变量的完整 Var（不止值，含 Source/Key）。
func varOf(t *testing.T, r *inject.Result, id, name string) inject.Var {
	t.Helper()
	for _, c := range r.Components {
		if c.Ref.ID != id {
			continue
		}
		for _, v := range c.Env {
			if v.Name == name {
				return v
			}
		}
	}
	require.Failf(t, "结果里没有该变量", "%s 的 %s", id, name)
	return inject.Var{}
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

// ============================================================
// 平台通用变量
// ============================================================

func TestComponentIdentityVariables(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.2.0", 8080), entry{})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "people/basic", env["COMPONENT_ID"], "变量名不带版本，值是组件 ID")
	assert.Equal(t, "1.2.0", env["COMPONENT_VERSION"])
}

// ============================================================
// 依赖地址注入
// ============================================================

// 变量名基于组件 ID（不带版本），值指向版本化服务名。
func TestDependencyEndpointInjection(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "department/tree", "1.0.0"),
		entry{})
	b.component(simple("department/tree", "1.0.0", 8080), entry{})

	env := envOf(t, b.build(), "erp/backend")

	assert.Equal(t, "http://department-tree-1-0-0:8080", env["DEPARTMENT_TREE_ENDPOINT"])
}

// 额外端口：PEOPLE_BASIC_GRPC_ENDPOINT。
func TestExtraPortEndpointInjection(t *testing.T) {
	dep := simple("people/basic", "1.0.0", 8080)
	dep.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		entry{})
	b.component(dep, entry{})

	env := envOf(t, b.build(), "erp/backend")

	assert.Equal(t, "http://people-basic-1-0-0:8080", env["PEOPLE_BASIC_ENDPOINT"])
	assert.Equal(t, "http://people-basic-1-0-0:9090", env["PEOPLE_BASIC_GRPC_ENDPOINT"])
}

// 组件 ID 含中划线时的变量名。
func TestEnvVarNameForHyphenatedComponentID(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "infra/redis-event-bus", "1.0.0"),
		entry{})
	b.component(simple("infra/redis-event-bus", "1.0.0", 6379), entry{})

	env := envOf(t, b.build(), "erp/backend")

	assert.Contains(t, env, "INFRA_REDIS_EVENT_BUS_ENDPOINT")
	assert.Equal(t, "http://infra-redis-event-bus-1-0-0:6379", env["INFRA_REDIS_EVENT_BUS_ENDPOINT"])
}

// ============================================================
// 弱依赖缺失
// ============================================================

// 弱依赖没启动时**完全不注入**，不是注入空值 ——
// 组件靠 os.environ.get() 判断"有没有"，注入空串会让它以为有。
func TestWeakDependencyNotRunningIsNotInjected(t *testing.T) {
	weak := simple("infra/redis-event-bus", "1.0.0", 6379)
	weak.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "metrics", Port: 9100}}

	b := newBuilder(t)
	b.component(weaklyDependsOn(simple("erp/backend", "1.0.0", 8080), "infra/redis-event-bus", "1.0.0"),
		entry{})
	b.component(weak, entry{Mode: deployfile.ModeDisable})

	env := envOf(t, b.build(), "erp/backend")

	assert.NotContains(t, env, "INFRA_REDIS_EVENT_BUS_ENDPOINT", "11.3")
	assert.NotContains(t, env, "INFRA_REDIS_EVENT_BUS_METRICS_ENDPOINT", "额外端口也不注入")
	for name := range env {
		assert.NotContains(t, name, "REDIS_EVENT_BUS", "不该残留任何该组件的变量：%s", name)
	}
}

// 弱依赖确实在启动时，照常注入。
func TestRunningWeakDependencyIsInjected(t *testing.T) {
	b := newBuilder(t)
	b.component(weaklyDependsOn(simple("erp/backend", "1.0.0", 8080), "infra/redis-event-bus", "1.0.0"),
		entry{})
	b.component(simple("infra/redis-event-bus", "1.0.0", 6379), entry{Mode: deployfile.ModeEnabled})

	env := envOf(t, b.build(), "erp/backend")

	assert.Equal(t, "http://infra-redis-event-bus-1-0-0:6379", env["INFRA_REDIS_EVENT_BUS_ENDPOINT"])
}

// 级联跳过的组件本身不产出环境变量表：它这次根本不启动。
func TestSkippedComponentProducesNoEnv(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("erp/backend", "1.0.0", 8080), entry{Mode: deployfile.ModeDisable})

	result := b.build()

	assert.Empty(t, result.Components, "不启动的组件不该出现在注入结果里")
}

// ============================================================
// 配置项注入
// ============================================================

// configSchema 是默认值来源，brickkit.yaml 的 config 是覆盖值。
func TestConfigDefaultsAndOverrides(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEFAULT_PAGE_SIZE": {Type: "integer", Default: 20},
		"ENABLE_AUDIT":      {Type: "boolean", Default: true},
		"CACHE_TTL_SECONDS": {Type: "integer", Default: 300},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{
		"DEFAULT_PAGE_SIZE": 50,
		"ENABLE_AUDIT":      false,
	}})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "50", env["DEFAULT_PAGE_SIZE"], "覆盖值优先")
	assert.Equal(t, "false", env["ENABLE_AUDIT"], "false 也是有效覆盖，不能当成没写")
	assert.Equal(t, "300", env["CACHE_TTL_SECONDS"], "没覆盖的用默认值")
}

// Var.Key 要记住原始 configSchema key（驼峰形式），不只是转换后的环境变量名——
// 外壳的 BRICKKIT_SERVED_MEMBERS_CONFIG 要把合并后的 config
// 原样交给外壳作者，用的就是这个原始 key，不是转换后的大写下划线名。
func TestConfigVarRecordsOriginalKeyForOverride(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEFAULT_PAGE_SIZE": {Type: "integer", Default: 20},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{"DEFAULT_PAGE_SIZE": 50}})

	v := varOf(t, b.build(), "people/basic", "DEFAULT_PAGE_SIZE")
	assert.Equal(t, "DEFAULT_PAGE_SIZE", v.Key)
	assert.Equal(t, inject.SourceConfig, v.Source)
}

func TestConfigVarRecordsOriginalKeyForDefault(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"CACHE_TTL_SECONDS": {Type: "integer", Default: 300},
	}}

	b := newBuilder(t)
	b.component(m, entry{})

	v := varOf(t, b.build(), "people/basic", "CACHE_TTL_SECONDS")
	assert.Equal(t, "CACHE_TTL_SECONDS", v.Key, "默认值（SourceConfig）同样要记原始 key，不只是覆盖值")
}

// configSchema 里声明了 secret: true 的配置项，注入结果里要标成敏感变量，
// 并记着它属于哪个组件（K8s 据此起 Secret 名）。
func TestSecretConfigVarIsMarkedSensitive(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"API_KEY": {Type: "string", Secret: true},
		"REGION":  {Type: "string", Default: "eu-west-1"},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{"API_KEY": "${THIRD_PARTY_KEY}"}})
	result := b.build()

	key := varOf(t, result, "people/basic", "API_KEY")
	assert.True(t, key.Secret, "声明了 secret: true")
	assert.Equal(t, "people-basic-1-0-0", key.Owner)
	assert.Equal(t, configdir.KindEnvTemplate, key.Value.Kind, "值保留引用种类，求值是渲染器的事")
	assert.Equal(t, "${THIRD_PARTY_KEY}", key.Value.Text)

	region := varOf(t, result, "people/basic", "REGION")
	assert.False(t, region.Secret, "没声明 secret 的照常明文")
	assert.Equal(t, "people-basic-1-0-0", region.Owner, "所有配置类变量都带 Owner")
}

// 非配置来源的变量没有 Owner——它们不属于任何"某个组件的配置项"。
func TestNonConfigVarsHaveNoOwner(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), entry{})

	assert.Empty(t, varOf(t, b.build(), "people/basic", "COMPONENT_ID").Owner)
}

// 非 config 来源的变量（依赖地址、资源连接、平台变量）不是靠某个 configSchema
// key 转换出来的，Key 该保持空——不能误导外壳作者以为它对应一个 config 项。
func TestNonConfigVarsHaveNoKey(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)

	b := newBuilder(t)
	b.component(m, entry{})

	v := varOf(t, b.build(), "people/basic", "COMPONENT_ID")
	assert.Empty(t, v.Key)
}

// CLI 不校验 config 的值类型：原样转成字符串注入。
func TestConfigValuesAreInjectedVerbatim(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEFAULT_PAGE_SIZE": {Type: "integer", Default: 20},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{"DEFAULT_PAGE_SIZE": "五十"}})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "五十", env["DEFAULT_PAGE_SIZE"],
		"configSchema 是配置说明书不是安检机：填错类型也原样注入，后果由使用者承担")
}

// 声明了 minimum / maximum / pattern 也一样：越界、不匹配的值照样原样注入，
// 既不阻断也不改写。这三栏是给读 schema 的人看的说明，没有代码去执行它们。
func TestConfigValuesOutsideDeclaredBoundsAreInjectedVerbatim(t *testing.T) {
	lo, hi := 1.0, 100.0
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEFAULT_PAGE_SIZE": {Type: "integer", Default: 20, Minimum: &lo, Maximum: &hi},
		"TENANT_SLUG":       {Type: "string", Pattern: "^[a-z]+$"},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{
		"DEFAULT_PAGE_SIZE": 100000,
		"TENANT_SLUG":       "NOT-A-SLUG",
	}})

	env := envOf(t, b.build(), "people/basic")

	assert.Equal(t, "100000", env["DEFAULT_PAGE_SIZE"])
	assert.Equal(t, "NOT-A-SLUG", env["TENANT_SLUG"])
}

// 没有默认值、也没有覆盖的配置项不注入 ——
// 注入空串会让组件以为"配置过了但值是空的"。
func TestConfigWithoutDefaultOrOverrideIsNotInjected(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"API_KEY": {Type: "string"},
	}}

	b := newBuilder(t)
	b.component(m, entry{})

	assert.NotContains(t, envOf(t, b.build(), "people/basic"), "API_KEY")
}

// configSchema.required 且没有默认值、也没有覆盖 → **阻断**。
//
// 组件作者写下 required 又不给默认值，说的正是"这一项我猜不出来，必须由项目给"。
// 跨项目服务的地址就是典型：那台服务归别人管，平台推导不出来。
// 从前这里只是不注入，组件看到"未配置"，而使用者以为自己配好了。
func TestRequiredConfigWithoutValueBlocks(t *testing.T) {
	m := simple("shop/order", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{"NOTIFIER_BASE_URL": {Type: "string"}},
		Required:   []string{"NOTIFIER_BASE_URL"},
	}

	b := newBuilder(t)
	b.component(m, entry{})

	out := clierr.As(b.buildErr()).Format()
	assert.Contains(t, out, "a required component config item has no value")
	assert.Contains(t, out, "shop/order@1.0.0 → NOTIFIER_BASE_URL")
	assert.Contains(t, out, "NOTIFIER_BASE_URL", "要说清楚它会变成哪个环境变量")
}

// required 有默认值时照常注入：默认值就是"值"，不该被当成缺失。
func TestRequiredConfigWithDefaultPasses(t *testing.T) {
	m := simple("shop/order", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{"PAGE_SIZE": {Type: "integer", Default: 20}},
		Required:   []string{"PAGE_SIZE"},
	}

	b := newBuilder(t)
	b.component(m, entry{})

	assert.Equal(t, "20", envOf(t, b.build(), "shop/order")["PAGE_SIZE"])
}

// required 无默认值、但项目在 config 里给了值 → 通过。这是跨项目场景的正常写法。
func TestRequiredConfigWithOverridePasses(t *testing.T) {
	m := simple("shop/order", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{"NOTIFIER_BASE_URL": {Type: "string"}},
		Required:   []string{"NOTIFIER_BASE_URL"},
	}

	b := newBuilder(t)
	b.component(m, entry{
		Config: map[string]any{"NOTIFIER_BASE_URL": "http://notify.internal.corp"}})

	assert.Equal(t, "http://notify.internal.corp",
		envOf(t, b.build(), "shop/order")["NOTIFIER_BASE_URL"])
}

// 不在 required 里的配置项没值时仍然只是不注入，不阻断（保持既有行为）。
func TestOptionalConfigWithoutValueStillSilent(t *testing.T) {
	m := simple("shop/order", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{"API_KEY": {Type: "string"}},
	}

	b := newBuilder(t)
	b.component(m, entry{})

	assert.NotContains(t, envOf(t, b.build(), "shop/order"), "API_KEY")
}

// ============================================================
// 升级时 configSchema 变更
// ============================================================

// 新版本新增配置项 → 用新默认值（使用者没覆盖过它）。
func TestUpgradeAddedConfigKeyUsesDefault(t *testing.T) {
	m := simple("people/basic", "2.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEFAULT_PAGE_SIZE":   {Default: 20},
		"NEW_IN_THIS_VERSION": {Default: "hello"},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{"DEFAULT_PAGE_SIZE": 50}})

	env := envOf(t, b.build(), "people/basic")
	assert.Equal(t, "hello", env["NEW_IN_THIS_VERSION"], "11.13")
	assert.Equal(t, "50", env["DEFAULT_PAGE_SIZE"])
}

// 新版本删掉了配置项，而 brickkit.yaml 里还留着旧的覆盖 →
// 不注入、不阻断，但**警告一声**。
//
// 这一条曾经是"静默忽略"，改成警告的理由：使用者刚升完级，他配的那一行
// 从此不起作用了，而他多半以为还在生效——那正是最该说一句的时刻。
// 警告不阻断任何事，代价只是多一行输出。
func TestUpgradeRemovedConfigKeyWarnsButDoesNotBlock(t *testing.T) {
	m := simple("people/basic", "2.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEFAULT_PAGE_SIZE": {Default: 20},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{
		"DEFAULT_PAGE_SIZE": 50,
		"REMOVED_IN_V2":     "旧配置",
	}})

	result := b.build()
	env := envOf(t, result, "people/basic")

	assert.NotContains(t, env, "REMOVED_IN_V2", "删掉的配置项不注入")
	assert.Equal(t, "50", env["DEFAULT_PAGE_SIZE"], "还在的配置项照常生效")

	require.Len(t, result.Warnings, 1, "该出一条警告：%v", result.Warnings)
	text := result.Warnings[0].Format()
	assert.Contains(t, text, "REMOVED_IN_V2", "要点名是哪一项")
	assert.Contains(t, text, "has no effect")
}

// ============================================================
// config 里写了组件不认识的配置项（B3）
// ============================================================

// 拼错一个字母 → 警告，并猜出他想写的那个。
//
// 这是最难查的一类：变量根本不出现，组件走进 os.environ.get(k, 默认值)
// 的默认分支，一切正常运行——只是不按你配的运行。没有任何运行时失败兜底。
func TestUnknownConfigKeyWarnsWithSuggestion(t *testing.T) {
	m := simple("demo/hello", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"GREETING": {Default: "你好"},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{"GREETTING": "哈罗"}})

	result := b.build()
	env := envOf(t, result, "demo/hello")
	assert.Equal(t, "你好", env["GREETING"], "写错的键不生效，用的是默认值")
	assert.NotContains(t, env, "GREETTING")

	require.Len(t, result.Warnings, 1, "%v", result.Warnings)
	text := result.Warnings[0].Format()
	assert.Contains(t, text, "GREETTING", "要点名是哪一项")
	assert.Contains(t, text, "Did you mean GREETING?", "猜拼写用的是 yamlcheck 那一份实现")
}

// 猜不出来时不硬猜，改成把可用的配置项列出来。
func TestUnknownConfigKeyWithoutSuggestionListsKnownKeys(t *testing.T) {
	m := simple("demo/hello", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"GREETING": {Default: "你好"},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{"TOTALLY_UNRELATED": 1}})

	text := b.build().Warnings[0].Format()
	assert.NotContains(t, text, "Did you mean", "八竿子打不着就别硬猜")
	assert.Contains(t, text, "GREETING", "至少告诉他有哪些可用")
}

// 组件根本没声明 configSchema，而项目写了 config → 整块蒸发，必须出声。
//
// 这一种大概比拼错还常见：先写个最小组件跑通，再想让它可配置，
// 直觉是去 brickkit.yaml 加 config，而正确做法是先回 component.yaml
// 加 configSchema。
func TestConfigOnComponentWithoutSchemaWarns(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("demo/nocfg", "1.0.0", 8080), entry{
		Config: map[string]any{"anything": "随便写", "LOG_LEVEL": "debug"},
	})

	result := b.build()
	env := envOf(t, result, "demo/nocfg")
	assert.NotContains(t, env, "ANYTHING")
	assert.NotContains(t, env, "LOG_LEVEL")

	require.Len(t, result.Warnings, 1, "整块只说一次，不是每项一条：%v", result.Warnings)
	text := result.Warnings[0].Format()
	assert.Contains(t, text, "declares no configSchema")
	assert.Contains(t, text, "anything")
	assert.Contains(t, text, "LOG_LEVEL")
}

// 没写 config 的组件不该被打扰——哪怕它也没有 configSchema。
func TestNoConfigNoWarning(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("demo/nocfg", "1.0.0", 8080), entry{})

	assert.Empty(t, b.build().Warnings)
}

// 键都对得上时一条警告都不该有。
//
// 这条比"能不能报出来"更要紧：一个见谁都喊的告警，两天之内就会被无视。
func TestCorrectConfigKeysProduceNoWarning(t *testing.T) {
	m := simple("demo/hello", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"GREETING":   {Default: "你好"},
		"LOG_LEVEL":  {Default: "info"},
		"PAGE_SIZE":  {Default: 20},
		"ENABLE_FOO": {Default: true},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{
		"GREETING": "哈罗", "LOG_LEVEL": "debug", "PAGE_SIZE": 50, "ENABLE_FOO": false,
	}})

	assert.Empty(t, b.build().Warnings)
}

// 新版本改了默认值且使用者没覆盖 → 用新默认值。
func TestUpgradeChangedDefaultTakesEffect(t *testing.T) {
	m := simple("people/basic", "2.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEFAULT_PAGE_SIZE": {Default: 100},
	}}

	b := newBuilder(t)
	b.component(m, entry{})

	assert.Equal(t, "100", envOf(t, b.build(), "people/basic")["DEFAULT_PAGE_SIZE"])
}

// 新版本改了默认值但使用者覆盖过 → 覆盖值优先。
func TestUpgradeOverrideBeatsNewDefault(t *testing.T) {
	m := simple("people/basic", "2.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEFAULT_PAGE_SIZE": {Default: 100},
	}}

	b := newBuilder(t)
	b.component(m, entry{Config: map[string]any{"DEFAULT_PAGE_SIZE": 50}})

	assert.Equal(t, "50", envOf(t, b.build(), "people/basic")["DEFAULT_PAGE_SIZE"])
}

// ============================================================
// 保留变量冲突
// ============================================================

// 冲突时"警告但跳过，平台注入的值优先"——
// 报错阻断会让一个配置项名字写错就整个项目起不来。
func TestReservedVariableConflictWarnsAndSkips(t *testing.T) {
	m := dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0")
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEPARTMENT_TREE_ENDPOINT": {Type: "string", Default: "http://用户写死的地址"},
	}}

	b := newBuilder(t)
	b.component(m, entry{})
	b.component(simple("department/tree", "1.0.0", 8080), entry{})

	result := b.build()
	env := envOf(t, result, "people/basic")

	assert.Equal(t, "http://department-tree-1-0-0:8080", env["DEPARTMENT_TREE_ENDPOINT"],
		"平台注入的值必须赢")
	require.Len(t, result.Warnings, 1)

	warning := result.Warnings[0].Format()
	assert.Contains(t, warning, "DEPARTMENT_TREE_ENDPOINT")
	assert.Contains(t, warning, "DEPARTMENT_TREE_ENDPOINT")
	assert.Contains(t, warning, "people/basic")
	assert.Contains(t, warning, "⚠️", "是警告不是错误")
}

// 冲突警告给出的新名字，必须**真的避得开**那条模式。
//
// 从前一律建议加 custom 前缀：对 `DATABASE_*` 这类前缀模式有效，
// 对 `*_ENDPOINT` 这类后缀模式完全无效——customNotifierEndpoint 照样以
// _ENDPOINT 结尾，改完再跑还是同一条警告。照着做不管用的建议比没有更糟。
func TestReservedConflictSuggestionActuallyAvoidsThePattern(t *testing.T) {
	m := dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0")
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DEPARTMENT_TREE_ENDPOINT": {Type: "string", Default: "http://写死的地址"},
	}}

	b := newBuilder(t)
	b.component(m, entry{})
	b.component(simple("department/tree", "1.0.0", 8080), entry{})

	warning := b.build().Warnings[0].Format()
	assert.Contains(t, warning, "DEPARTMENT_TREE_BASE_URL")
	assert.NotContains(t, warning, "CUSTOM_DEPARTMENT_TREE_ENDPOINT",
		"这个建议改完还是以 _ENDPOINT 结尾，等于没改")
}

// ============================================================
// 资源配额合并
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
	b.component(m, entry{Resources: &manifest.Resources{
		Limits: spec2("", "2Gi"),
	}})

	quota := quotaOf(t, b.build(), "people/basic")

	assert.Equal(t, "2Gi", quota.Limits.Memory, "使用者覆盖优先")
	assert.Equal(t, "1", quota.Limits.CPU, "使用者没写的字段沿用组件推荐值")
	assert.Equal(t, "200m", quota.Requests.CPU)
	assert.Equal(t, "256Mi", quota.Requests.Memory)
}

// 组件没声明、使用者也没覆盖 → requests 用 CLI 默认值，**limits 不生成**。
//
// 两个方向的失败代价不对称：不设上限最差是节点变紧、按 QoS 驱逐，运维查得出；
// 而平台凭空猜一个上限，会去 OOMKill 一个跑得好好的组件——它真的需要 600Mi，
// 而 512Mi 是平台编的，配置里一个字都没写过。
func TestResourceQuotaFallsBackToCLIDefaults(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), entry{})

	quota := quotaOf(t, b.build(), "people/basic")

	assert.Equal(t, "100m", quota.Requests.CPU)
	assert.Equal(t, "128Mi", quota.Requests.Memory)
	assert.Nil(t, quota.Limits, "没人写上限就不该有上限")
}

// 组件声明了推荐值、使用者没覆盖 → 用组件的；没写的那半边不补默认值。
func TestResourceQuotaUsesManifestWhenNotOverridden(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.Resources = &manifest.Resources{Requests: spec2("300m", "384Mi")}

	b := newBuilder(t)
	b.component(m, entry{})

	quota := quotaOf(t, b.build(), "people/basic")

	assert.Equal(t, "300m", quota.Requests.CPU)
	assert.Equal(t, "384Mi", quota.Requests.Memory)
	assert.Nil(t, quota.Limits, "组件只声明了 requests，就只有 requests")
}

// 只写内存上限时，CPU 上限不该被凭空补出来。
//
// 这是推荐写法：内存 requests = limits 拿 Guaranteed，
// 而 CPU 不设上限——CPU limit 走 CFS quota，即使节点空闲也会在每个
// 100ms 周期里限流，表现成毫无来由的 p99 毛刺。
func TestMemoryLimitOnlyDoesNotInventCPULimit(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), entry{
		Resources: &manifest.Resources{Limits: &manifest.ResourceSpec{Memory: "1Gi"}},
	})

	quota := quotaOf(t, b.build(), "people/basic")

	require.NotNil(t, quota.Limits)
	assert.Equal(t, "1Gi", quota.Limits.Memory)
	assert.Empty(t, quota.Limits.CPU, "只写了内存上限，CPU 就不该有上限")
}

func quotaOf(t *testing.T, r *inject.Result, id string) manifest.Resources {
	t.Helper()
	for _, c := range r.Components {
		if c.Ref.ID == id {
			require.NotNil(t, c.Resources.Requests, "requests 永远有默认值")
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
		"ZEBRA": {Default: 1}, "ALPHA": {Default: 2}, "MIDDLE": {Default: 3},
	}}

	b := newBuilder(t)
	b.component(m, entry{})
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
		b.component(m, entry{})
		return b.build().Components[0].Env
	}

	assert.Equal(t, build(), build())
}

// ============================================================
// 三层文件（P2）：值保留引用种类、资源前缀不再保留
// ============================================================

func TestBuildKeepsValueKinds(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DSN":    {Type: "string"},
		"CERT":   {Type: "string"},
		"TOKEN":  {Type: "string", Secret: true},
		"DB_PWD": {Type: "string", Secret: true},
		"HOST":   {Type: "string"},
	}}
	b := newBuilder(t).sharedVars(map[string]any{"PG_HOST": "pg.internal"})
	b.component(m, entry{Config: map[string]any{
		"DSN":    "pg://${PG_USER}@db",
		"CERT":   "file://secrets/ca.pem",
		"TOKEN":  "plain-token",
		"DB_PWD": map[string]any{"existingSecret": "db", "key": "password"},
		"HOST":   "$var:PG_HOST",
	}})
	result := b.build()

	assert.Equal(t, configdir.KindEnvTemplate, varOf(t, result, "people/basic", "DSN").Value.Kind)
	assert.Equal(t, configdir.KindFileRef, varOf(t, result, "people/basic", "CERT").Value.Kind)
	token := varOf(t, result, "people/basic", "TOKEN")
	assert.True(t, token.Secret)
	assert.Equal(t, configdir.KindLiteral, token.Value.Kind)
	assert.True(t, varOf(t, result, "people/basic", "DB_PWD").IsSecretRef())
	host := varOf(t, result, "people/basic", "HOST")
	assert.Equal(t, "pg.internal", host.Value.Text, "$var: 在注入前就已解析成目标值")
}

func TestBuildDatabasePrefixNoLongerReserved(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DATABASE_URL": {Type: "string", Default: "pg://x"},
		"REDIS_HOST":   {Type: "string", Default: "cache"},
	}}
	b := newBuilder(t)
	b.component(m, entry{})
	result := b.build()
	assert.Empty(t, result.Warnings)
	env := envOf(t, result, "people/basic")
	assert.Equal(t, "pg://x", env["DATABASE_URL"])
	assert.Equal(t, "cache", env["REDIS_HOST"])
}

func TestBuildReservedEndpointSuffixStillBlocked(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"NOTIFIER_ENDPOINT": {Type: "string", Default: "http://x"},
	}}
	b := newBuilder(t)
	b.component(m, entry{})
	result := b.build()
	require.Len(t, result.Warnings, 1)
	assert.NotContains(t, envOf(t, result, "people/basic"), "NOTIFIER_ENDPOINT")
}

// 依赖一个被外壳承载的成员：地址指向外壳（网络拓扑层由 CLI 重写），
// 端口仍是成员自己的端口——外壳进程在那个端口上替它监听。
func TestEndpointOfHostedMemberPointsAtShell(t *testing.T) {
	a := simple("erp/a", "1.0.0", 8081)
	a.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9091}}
	b := newBuilder(t)
	b.component(simple("erp/shell", "1.0.0", 8080), entry{Members: []string{"erp/a"}})
	b.component(a, entry{})
	b.component(dependsOn(simple("erp/caller", "1.0.0", 8090), "erp/a", "1.0.0"), entry{})

	env := envOf(t, b.build(), "erp/caller")
	assert.Equal(t, "http://erp-shell-1-0-0:8081", env["ERP_A_ENDPOINT"])
	assert.Equal(t, "http://erp-shell-1-0-0:9091", env["ERP_A_GRPC_ENDPOINT"])
}

// 成员以裸进程运行或外壳这次没跑：地址指向成员自己。
func TestEndpointOfUnhostedMemberPointsAtMember(t *testing.T) {
	for name, e := range map[string][2]entry{
		"成员 mode: local": {{Members: []string{"erp/a"}}, {Mode: deployfile.ModeLocal}},
		"外壳 disable":     {{Members: []string{"erp/a"}, Mode: deployfile.ModeDisable}, {Mode: deployfile.ModeEnabled}},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBuilder(t)
			b.component(simple("erp/shell", "1.0.0", 8080), e[0])
			b.component(simple("erp/a", "1.0.0", 8081), e[1])
			b.component(dependsOn(simple("erp/caller", "1.0.0", 8090), "erp/a", "1.0.0"), entry{})

			env := envOf(t, b.build(), "erp/caller")
			assert.Equal(t, "http://erp-a-1-0-0:8081", env["ERP_A_ENDPOINT"])
		})
	}
}
