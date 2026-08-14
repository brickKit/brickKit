// 本文件是 Step 12「docker-compose.yaml 生成」的业务行为测试，
// 覆盖开发计划 12.1–12.16，以及延后项 P2（配额写进部署文件）、P4（expose 端口冲突）、
// P20（注入引擎接线）、P21（extraPorts 变量出现在 compose 里）。
//
// 生成的文件是给人看、也给 docker 读的：断言尽量落在"最终 YAML 里有什么"，
// 而不是内部结构，这样重构渲染方式不会让测试全红。
package compose_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// ============================================================
// 夹具
// ============================================================

type stubProvider map[string]*manifest.Manifest

func (p stubProvider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	m, ok := p[id+"@"+version]
	if !ok {
		return nil, errNotFound{id + "@" + version}
	}
	return m, nil
}

type errNotFound struct{ ref string }

func (e errNotFound) Error() string { return "夹具里没有 " + e.ref }

type builder struct {
	t        *testing.T
	provider stubProvider
	roots    []resolver.Ref
	cfg      *config.Config
}

func newBuilder(t *testing.T) *builder {
	return &builder{
		t:        t,
		provider: stubProvider{},
		cfg:      &config.Config{Project: "my-erp", Deploy: config.Deploy{Target: config.TargetDocker}},
	}
}

func (b *builder) component(m *manifest.Manifest, entry config.Component) *builder {
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

// build 跑完整条链路：解析 → 级联 → 注入 → 生成 compose，允许失败。
func (b *builder) build(opts compose.Options) (*compose.Result, error) {
	b.t.Helper()

	graph, err := resolver.New(b.provider).Resolve(context.Background(), b.roots...)
	require.NoError(b.t, err)

	states, err := cascade.Compute(b.cfg, graph)
	require.NoError(b.t, err)

	env, err := inject.Build(b.cfg, graph, states)
	require.NoError(b.t, err)

	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC) }
	}
	return compose.Generate(b.cfg, graph, states, env, opts)
}

// generate 跑完整条链路，失败即断言失败。
func (b *builder) generate() *compose.Result {
	b.t.Helper()

	result, err := b.build(compose.Options{})
	require.NoError(b.t, err)
	return result
}

// parsed 把生成的 YAML 解析成通用结构，便于断言。
func (b *builder) parsed() map[string]any {
	b.t.Helper()

	var doc map[string]any
	require.NoError(b.t, yaml.Unmarshal(b.generate().YAML, &doc), "生成的内容必须是合法 YAML")
	return doc
}

func servicesOf(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	services, ok := doc["services"].(map[string]any)
	require.True(t, ok, "compose 必须有 services 段：%v", doc)
	return services
}

func serviceOf(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	svc, ok := servicesOf(t, doc)[name].(map[string]any)
	require.True(t, ok, "应存在服务 %s，实际有：%v", name, keysOf(servicesOf(t, doc)))
	return svc
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// envOf 把 environment 列表转成 map。
func envOf(t *testing.T, svc map[string]any) map[string]string {
	t.Helper()

	raw, ok := svc["environment"].([]any)
	if !ok {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, item := range raw {
		name, value, _ := strings.Cut(item.(string), "=")
		out[name] = value
	}
	return out
}

// simple 造一个最小组件。
func simple(id, version string, port int) *manifest.Manifest {
	return &manifest.Manifest{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Metadata:   manifest.Metadata{ID: id, Name: id, Version: version},
		Deployment: manifest.Deployment{
			Type: manifest.DeploymentTypeContainer, Image: "registry.example.com/" + strings.ReplaceAll(id, "/", "-") + ":" + version,
			Port: port,
		},
		HealthCheck: manifest.HealthCheck{Type: manifest.HealthCheckHTTP, Path: "/healthz"},
	}
}

func withDatabase(m *manifest.Manifest) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Resources = append(m.Dependencies.Resources,
		manifest.ResourceDep{Kind: "database", Engine: "postgresql"})
	return m
}

func dependsOn(m *manifest.Manifest, id, version string) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Components = append(m.Dependencies.Components,
		manifest.ComponentDep{ID: id, Version: version})
	return m
}

// pgResource 是一个由 CLI 托管的 PostgreSQL（host 是 Docker Network 内的服务名）。
func pgResource(bindings ...config.Binding) config.Resource {
	return config.Resource{
		Kind: config.ResourceKindDatabase, Engine: "postgresql", ID: "postgres-main",
		Host: "postgres", Port: 5432, Username: "brickkit", Password: "${POSTGRES_PASSWORD}",
		Bindings: bindings,
	}
}

// ============================================================
// 12.16 文件头 / 12.8 网络
// ============================================================

func TestGeneratedFileHasHeaderComment(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	text := string(b.generate().YAML)

	assert.Contains(t, text, "由 BrickKit CLI 自动生成", "12.16")
	assert.Contains(t, text, "请勿手动编辑")
	assert.Contains(t, text, "2026-08-14T10:00:00Z", "生成时间要写进头部")
	assert.Contains(t, text, "my-erp", "项目名要写进头部")
}

// 12.8 网络名是 brickkit-<项目名>-net。
func TestNetworkName(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	networks, ok := b.parsed()["networks"].(map[string]any)
	require.True(t, ok, "必须有 networks 段")
	net, ok := networks["brickkit-net"].(map[string]any)
	require.True(t, ok, "网络别名应为 brickkit-net：%v", keysOf(networks))

	assert.Equal(t, "brickkit-my-erp-net", net["name"], "12.8")
	assert.Equal(t, "bridge", net["driver"])
}

// 每个 service 都要接进项目网络，否则组件之间用服务名互相访问不到。
func TestEveryServiceJoinsTheNetwork(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)),
		config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	for name, raw := range servicesOf(t, b.parsed()) {
		svc := raw.(map[string]any)
		assert.Contains(t, svc["networks"], "brickkit-net", "服务 %s 没接进网络", name)
	}
}

// ============================================================
// 12.2 / 12.12 依赖顺序
// ============================================================

// 12.2：强依赖体现为 depends_on + service_healthy。
func TestDependsOnUsesHealthCondition(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	svc := serviceOf(t, b.parsed(), "erp-backend-1-0-0")
	dependsOn, ok := svc["depends_on"].(map[string]any)
	require.True(t, ok, "12.2 应有 depends_on：%v", svc)

	dep, ok := dependsOn["people-basic-1-0-0"].(map[string]any)
	require.True(t, ok, "应依赖 people-basic-1-0-0：%v", keysOf(dependsOn))
	assert.Equal(t, "service_healthy", dep["condition"],
		"要等依赖健康，而不是只等容器起来")
}

// 弱依赖不进 depends_on：它可能根本不启动，写进去会把整个项目卡住。
func TestWeakDependencyIsNotInDependsOn(t *testing.T) {
	m := simple("erp/backend", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{Components: []manifest.ComponentDep{
		{ID: "infra/redis-event-bus", Version: "1.0.0", Optional: true},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.component(simple("infra/redis-event-bus", "1.0.0", 6379), config.Component{})

	svc := serviceOf(t, b.parsed(), "erp-backend-1-0-0")
	if dependsOn, ok := svc["depends_on"].(map[string]any); ok {
		assert.NotContains(t, dependsOn, "infra-redis-event-bus-1-0-0")
	}
}

// ============================================================
// 12.6 / 12.11 / 12.12 迁移
// ============================================================

func withMigration(m *manifest.Manifest) *manifest.Manifest {
	m.Migration = &manifest.Migration{Command: []string{"/app/people-basic", "migrate"}}
	return m
}

// 12.6：声明了 migration 的组件生成一个一次性 service。
func TestMigrationServiceIsGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0-migration")

	assert.Equal(t, "registry.example.com/people-basic:1.0.0", svc["image"],
		"002 §8.4：迁移用组件自己的镜像")
	assert.Equal(t, "no", svc["restart"], "12.11：一次性任务不能自动重启")

	// 必须同时覆盖 entrypoint 与 command。
	// 这是真跑起来才发现的：组件镜像普遍带 ENTRYPOINT（002 §1.4 推荐的写法），
	// 而 compose 的 command 只覆盖 CMD——只写 command 会变成
	// `/app/people-basic /app/people-basic migrate`，参数错位，
	// 结果迁移容器把**服务**起起来了，主服务永远等不到"迁移完成"。
	assert.Equal(t, []any{"/app/people-basic"}, svc["entrypoint"],
		"entrypoint 取命令的第一段")
	assert.Equal(t, []any{"migrate"}, svc["command"], "其余作为参数")
}

// 002 §8.5：迁移容器要拿到与主容器一样的环境变量。
func TestMigrationServiceInheritsEnvironment(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.parsed()
	main := envOf(t, serviceOf(t, doc, "people-basic-1-0-0"))
	migration := envOf(t, serviceOf(t, doc, "people-basic-1-0-0-migration"))

	assert.Equal(t, main, migration, "迁移容器的环境变量应与主容器完全一致")
	assert.Equal(t, "people", migration["DATABASE_NAME"])
}

// 12.12：主服务必须等迁移**成功结束**再启动。
func TestMainServiceWaitsForMigration(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0")
	dependsOn := svc["depends_on"].(map[string]any)
	migration, ok := dependsOn["people-basic-1-0-0-migration"].(map[string]any)

	require.True(t, ok, "12.12 主服务要依赖迁移 service：%v", keysOf(dependsOn))
	assert.Equal(t, "service_completed_successfully", migration["condition"])
}

// 迁移容器也要等数据库健康——它是第一个连库的东西。
func TestMigrationServiceWaitsForDatabase(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0-migration")
	dependsOn := svc["depends_on"].(map[string]any)
	pg, ok := dependsOn["postgres"].(map[string]any)

	require.True(t, ok, "迁移要等数据库就绪：%v", keysOf(dependsOn))
	assert.Equal(t, "service_healthy", pg["condition"])
}

// 多段命令同样正确拆分：python -m app.main migrate。
func TestMigrationCommandWithInterpreterIsSplitCorrectly(t *testing.T) {
	m := withDatabase(simple("people/basic", "1.0.0", 8080))
	m.Migration = &manifest.Migration{Command: []string{"python", "-m", "app.main", "migrate"}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0-migration")

	assert.Equal(t, []any{"python"}, svc["entrypoint"])
	assert.Equal(t, []any{"-m", "app.main", "migrate"}, svc["command"])
}

// 单段命令（如 ["/app/migrate"]）也要能处理。
func TestSingleWordMigrationCommand(t *testing.T) {
	m := withDatabase(simple("people/basic", "1.0.0", 8080))
	m.Migration = &manifest.Migration{Command: []string{"/app/migrate"}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0-migration")

	assert.Equal(t, []any{"/app/migrate"}, svc["entrypoint"])
	assert.NotContains(t, svc, "command", "没有额外参数时不写 command")
}

// 没声明 migration 的组件不生成迁移 service（002 §8.8）。
func TestComponentWithoutMigrationHasNoMigrationService(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.NotContains(t, servicesOf(t, b.parsed()), "people-basic-1-0-0-migration")
}

// 14.6：环境变量"原封不动复制"（002 §8.5）这条承诺，在依赖被搬去本地调试
// 之后也要成立——地址被改写成 localPort 时，两边必须一起改。
func TestMigrationServiceInheritsRewrittenEnvironment(t *testing.T) {
	b := newBuilder(t)
	b.component(
		withMigration(withDatabase(dependsOn(simple("erp/backend", "1.0.0", 8080),
			"people/basic", "1.0.0"))),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8081})
	b.resource(pgResource(config.Binding{ComponentID: "erp/backend", Database: "erp"}))

	doc := b.parsed()
	main := envOf(t, serviceOf(t, doc, "erp-backend-1-0-0"))
	migration := envOf(t, serviceOf(t, doc, "erp-backend-1-0-0-migration"))

	assert.Equal(t, main, migration, "14.6")
	assert.Equal(t, "http://people-basic-1-0-0:8081", migration["PEOPLE_BASIC_ENDPOINT"])
}

// 环境变量一致，寻址方式也得一致：迁移容器拿到了一个指向宿主机的地址，
// 却没有 extra_hosts 的话，这个主机名在它那儿根本解析不了。
func TestMigrationServiceGetsTheSameExtraHosts(t *testing.T) {
	b := newBuilder(t)
	b.component(
		withMigration(withDatabase(dependsOn(simple("erp/backend", "1.0.0", 8080),
			"people/basic", "1.0.0"))),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8081})
	b.resource(pgResource(config.Binding{ComponentID: "erp/backend", Database: "erp"}))

	doc := b.parsed()

	assert.Equal(t,
		extraHostsOf(t, serviceOf(t, doc, "erp-backend-1-0-0")),
		extraHostsOf(t, serviceOf(t, doc, "erp-backend-1-0-0-migration")))
}

// 迁移只等基础资源，不等别的组件。
//
// 迁移动的是自己的库，不该跟别人的启动顺序绑在一起；把强依赖写进去，
// 一条依赖链上的迁移就会被串成串行，弱依赖更是可能根本不启动。
func TestMigrationServiceDoesNotWaitForOtherComponents(t *testing.T) {
	b := newBuilder(t)
	b.component(
		withMigration(withDatabase(dependsOn(simple("erp/backend", "1.0.0", 8080),
			"people/basic", "1.0.0"))),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "erp/backend", Database: "erp"}))

	svc := serviceOf(t, b.parsed(), "erp-backend-1-0-0-migration")
	dependsOn := svc["depends_on"].(map[string]any)

	assert.Contains(t, dependsOn, "postgres")
	assert.NotContains(t, dependsOn, "people-basic-1-0-0")
}

// 迁移容器不占宿主机端口：它和主容器同镜像同环境，
// 要是把 expose 的端口也映射一份，两个容器会抢同一个宿主机端口。
func TestMigrationServiceDoesNotPublishPorts(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))),
		config.Component{Expose: true, ExposePort: 18080})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.parsed()

	assert.Equal(t, []string{"18080:8080"}, portsOf(t, serviceOf(t, doc, "people-basic-1-0-0")))
	assert.Empty(t, portsOf(t, serviceOf(t, doc, "people-basic-1-0-0-migration")),
		"一次性任务不该占宿主机端口")
}

// 迁移容器不做健康检查：它跑完就退出，healthcheck 只会给出一个
// "unhealthy 然后消失"的假象。
func TestMigrationServiceHasNoHealthcheck(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))),
		config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0-migration")

	assert.NotContains(t, svc, "healthcheck")
}

// ============================================================
// 12.3 健康检查
// ============================================================

func TestHealthcheckFromManifest(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0")
	health, ok := svc["healthcheck"].(map[string]any)
	require.True(t, ok, "12.3 应有 healthcheck：%v", svc)

	assert.Equal(t,
		[]any{"CMD", "wget", "-q", "--spider", "http://localhost:8080/healthz"},
		health["test"])
	assert.NotEmpty(t, health["interval"])
	assert.NotEmpty(t, health["retries"])
}

// 健康检查探的是**主端口**，不是额外端口（002 §5.5）。
func TestHealthcheckUsesMainPortNotExtraPort(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	b := newBuilder(t)
	b.component(m, config.Component{})

	health := serviceOf(t, b.parsed(), "people-basic-1-0-0")["healthcheck"].(map[string]any)
	assert.Contains(t, health["test"].([]any)[4], ":8080/healthz")
}

// healthCheck.type: none 的组件不生成 healthcheck ——
// 生成一个探不通的检查会让依赖它的组件永远等不到 service_healthy。
func TestHealthcheckNoneGeneratesNothing(t *testing.T) {
	m := simple("infra/worker", "1.0.0", 8080)
	m.HealthCheck = manifest.HealthCheck{Type: manifest.HealthCheckNone}

	b := newBuilder(t)
	b.component(m, config.Component{})

	svc := serviceOf(t, b.parsed(), "infra-worker-1-0-0")
	assert.NotContains(t, svc, "healthcheck")
}

// 依赖一个没有健康检查的组件时，只能等它启动（service_started）。
func TestDependencyWithoutHealthcheckUsesStartedCondition(t *testing.T) {
	dep := simple("infra/worker", "1.0.0", 8080)
	dep.HealthCheck = manifest.HealthCheck{Type: manifest.HealthCheckNone}

	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "infra/worker", "1.0.0"),
		config.Component{})
	b.component(dep, config.Component{})

	dependsOn := serviceOf(t, b.parsed(), "erp-backend-1-0-0")["depends_on"].(map[string]any)
	assert.Equal(t, "service_started",
		dependsOn["infra-worker-1-0-0"].(map[string]any)["condition"])
}

// ============================================================
// 12.4 资源配额（回填 P2 的后半部分）
// ============================================================

// K8s 风格的 100m / 128Mi 要转成 compose 的 cpus / memory 写法。
func TestResourceQuotaConversion(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.Resources = &manifest.Resources{
		Requests: &manifest.ResourceSpec{CPU: "100m", Memory: "128Mi"},
		Limits:   &manifest.ResourceSpec{CPU: "1", Memory: "1Gi"},
	}

	b := newBuilder(t)
	b.component(m, config.Component{})

	deploy := serviceOf(t, b.parsed(), "people-basic-1-0-0")["deploy"].(map[string]any)
	resources := deploy["resources"].(map[string]any)

	limits := resources["limits"].(map[string]any)
	assert.Equal(t, "1.00", limits["cpus"])
	assert.Equal(t, "1G", limits["memory"])

	reservations := resources["reservations"].(map[string]any)
	assert.Equal(t, "0.10", reservations["cpus"], "compose 用 reservations 表达 requests")
	assert.Equal(t, "128M", reservations["memory"])
}

// 没声明配额的组件用 CLI 默认值（004 §5.6.2）。
func TestResourceQuotaFallsBackToDefaults(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	deploy := serviceOf(t, b.parsed(), "people-basic-1-0-0")["deploy"].(map[string]any)
	limits := deploy["resources"].(map[string]any)["limits"].(map[string]any)

	assert.Equal(t, "0.50", limits["cpus"])
	assert.Equal(t, "512M", limits["memory"])
}

// brickkit.yaml 里的覆盖要落到最终文件里（P2）。
func TestResourceQuotaOverrideReachesTheFile(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.Resources = &manifest.Resources{
		Limits: &manifest.ResourceSpec{CPU: "500m", Memory: "512Mi"},
	}

	b := newBuilder(t)
	b.component(m, config.Component{Resources: &manifest.Resources{
		Limits: &manifest.ResourceSpec{Memory: "2Gi"},
	}})

	limits := serviceOf(t, b.parsed(), "people-basic-1-0-0")["deploy"].(map[string]any)["resources"].(map[string]any)["limits"].(map[string]any)
	assert.Equal(t, "2G", limits["memory"], "使用者覆盖优先")
	assert.Equal(t, "0.50", limits["cpus"], "没覆盖的沿用组件推荐值")
}

// ============================================================
// 12.5 expose 端口映射（含 P4 冲突判定）
// ============================================================

// expose: true 且没写 exposePort 时，用组件的主端口做宿主机端口。
func TestExposeUsesComponentPortByDefault(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true})

	svc := serviceOf(t, b.parsed(), "portal-user-frontend-1-0-0")
	assert.Equal(t, []any{"80:80"}, svc["ports"])
}

// 12.5：exposePort 自定义宿主机端口。
func TestExposePortMapsToCustomHostPort(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, ExposePort: 8080})

	svc := serviceOf(t, b.parsed(), "portal-user-frontend-1-0-0")
	assert.Equal(t, []any{"8080:80"}, svc["ports"])
}

// 没有 expose 的组件不映射端口：容器网络内互相访问不需要暴露到宿主机。
func TestComponentWithoutExposeHasNoPorts(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.NotContains(t, serviceOf(t, b.parsed(), "people-basic-1-0-0"), "ports")
}

// P4：两个组件抢同一个宿主机端口 → 报错并指出改哪里（004 §10.3）。
func TestConflictingExposePortsIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80), config.Component{Expose: true, ExposePort: 8080})
	b.component(simple("admin/console", "1.0.0", 80), config.Component{Expose: true, ExposePort: 8080})

	graph, err := resolver.New(b.provider).Resolve(context.Background(), b.roots...)
	require.NoError(t, err)
	states, err := cascade.Compute(b.cfg, graph)
	require.NoError(t, err)
	env, err := inject.Build(b.cfg, graph, states)
	require.NoError(t, err)

	_, err = compose.Generate(b.cfg, graph, states, env, compose.Options{})

	require.Error(t, err)
	// 建议在 hints 里，要看渲染后的完整错误
	rendered := clierr.As(err).Format()
	assert.Contains(t, rendered, "8080")
	assert.Contains(t, rendered, "portal/user-frontend")
	assert.Contains(t, rendered, "admin/console")
	assert.Contains(t, rendered, "exposePort", "要告诉使用者改哪个字段")
}

// 默认端口相同也算冲突：两个组件都 expose 且主端口都是 80。
func TestConflictingDefaultExposePortsIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80), config.Component{Expose: true})
	b.component(simple("admin/console", "1.0.0", 80), config.Component{Expose: true})

	graph, _ := resolver.New(b.provider).Resolve(context.Background(), b.roots...)
	states, _ := cascade.Compute(b.cfg, graph)
	env, _ := inject.Build(b.cfg, graph, states)

	_, err := compose.Generate(b.cfg, graph, states, env, compose.Options{})
	assert.Error(t, err)
}

// ============================================================
// 12.7 local: true
// ============================================================

// local: true 的组件在宿主机（IDE）里跑，不生成容器。
func TestLocalComponentGeneratesNoService(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Local: true})

	services := servicesOf(t, b.parsed())

	assert.NotContains(t, services, "people-basic-1-0-0", "12.7")
	assert.Contains(t, services, "erp-backend-1-0-0")
}

// 依赖一个 local 组件时不能写 depends_on：那个 service 根本不存在，
// compose 会直接报"依赖了未定义的服务"。
func TestDependencyOnLocalComponentIsNotInDependsOn(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Local: true})

	svc := serviceOf(t, b.parsed(), "erp-backend-1-0-0")
	if dependsOn, ok := svc["depends_on"].(map[string]any); ok {
		assert.NotContains(t, dependsOn, "people-basic-1-0-0")
	}
}

// ============================================================
// 12.13 / 12.14 / 12.9 基础资源
// ============================================================

// 12.13：host 是 Docker Network 内的服务名 → CLI 生成资源容器（006 §10.4）。
func TestResourceContainerIsGeneratedForServiceNameHost(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	svc := serviceOf(t, b.parsed(), "postgres")

	assert.Contains(t, svc["image"], "postgres")
	assert.Equal(t, "unless-stopped", svc["restart"])
	health := svc["healthcheck"].(map[string]any)
	assert.Contains(t, health["test"].([]any)[1], "pg_isready")
}

// 12.14：host 是 IP / 域名 → 假设运维已部署，不生成容器（006 §10.4）。
func TestResourceContainerIsNotGeneratedForExternalHost(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	r := pgResource(config.Binding{ComponentID: "people/basic", Database: "people"})
	r.Host = "10.0.1.10"
	b.resource(r)

	services := servicesOf(t, b.parsed())

	assert.NotContains(t, services, "postgres")
	assert.NotContains(t, services, "10.0.1.10")
	// 但环境变量照常注入，组件连的是外部地址
	assert.Equal(t, "10.0.1.10", envOf(t, serviceOf(t, b.parsed(), "people-basic-1-0-0"))["DATABASE_HOST"])
}

func TestExternalHostByDomainIsNotGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	r := pgResource(config.Binding{ComponentID: "people/basic", Database: "people"})
	r.Host = "mydb.rds.amazonaws.com"
	b.resource(r)

	assert.NotContains(t, servicesOf(t, b.parsed()), "mydb.rds.amazonaws.com")
}

// 12.9：CLI 托管的资源要有数据卷，否则 down 一次数据就没了。
func TestResourceVolumeIsDeclared(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.parsed()
	volumes, ok := doc["volumes"].(map[string]any)
	require.True(t, ok, "12.9 应有 volumes 段：%v", keysOf(doc))
	assert.Contains(t, volumes, "postgres-data")

	svc := serviceOf(t, doc, "postgres")
	assert.Contains(t, svc["volumes"], "postgres-data:/var/lib/postgresql/data")
}

// 组件要等资源健康再启动。
func TestComponentWaitsForItsResource(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	dependsOn := serviceOf(t, b.parsed(), "people-basic-1-0-0")["depends_on"].(map[string]any)
	pg, ok := dependsOn["postgres"].(map[string]any)

	require.True(t, ok, "应依赖 postgres：%v", keysOf(dependsOn))
	assert.Equal(t, "service_healthy", pg["condition"])
}

// 缓存资源（redis）同样能被生成。
func TestCacheResourceContainer(t *testing.T) {
	m := simple("erp/backend", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{Resources: []manifest.ResourceDep{
		{Kind: "cache", Engine: "redis"},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: config.ResourceKindCache, Engine: "redis", ID: "redis-main",
		Host: "redis", Port: 6379,
		Bindings: []config.Binding{{ComponentID: "erp/backend"}},
	})

	svc := serviceOf(t, b.parsed(), "redis")
	assert.Contains(t, svc["image"], "redis")
	assert.Contains(t, svc["healthcheck"].(map[string]any)["test"].([]any), "ping")
}

// ============================================================
// 006 §9.5：库不存在时平台的责任是"说清楚"
// ============================================================

// 生成时把"需要哪些数据库"整理出来，供 up 前提示使用者建库。
// 平台不建库（006 §9.1），但必须让人知道要建什么。
func TestResultReportsRequiredDatabases(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.component(withDatabase(simple("department/tree", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(
		config.Binding{ComponentID: "people/basic", Database: "brickkit_people"},
		config.Binding{ComponentID: "department/tree", Database: "brickkit_department"},
	))

	result := b.generate()

	require.Len(t, result.Databases, 2)
	names := []string{result.Databases[0].Name, result.Databases[1].Name}
	assert.ElementsMatch(t, []string{"brickkit_people", "brickkit_department"}, names)
	assert.Equal(t, "postgres", result.Databases[0].Host)
	assert.NotEmpty(t, result.Databases[0].CreateSQL, "要给出可直接执行的 SQL")
	assert.Contains(t, result.Databases[0].CreateSQL, "CREATE DATABASE")
}

// 没绑定 database 的资源（如 redis）不算"需要建的库"。
func TestCacheResourceIsNotReportedAsDatabase(t *testing.T) {
	m := simple("erp/backend", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{Resources: []manifest.ResourceDep{{Kind: "cache", Engine: "redis"}}}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: config.ResourceKindCache, Engine: "redis", ID: "redis-main", Host: "redis", Port: 6379,
		Bindings: []config.Binding{{ComponentID: "erp/backend"}},
	})

	assert.Empty(t, b.generate().Databases)
}

// ============================================================
// 12.10 / 12.15 其他
// ============================================================

func TestRestartPolicy(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.Equal(t, "unless-stopped", serviceOf(t, b.parsed(), "people-basic-1-0-0")["restart"], "12.10")
}

// 12.15：多版本各自独立 service。
func TestMultipleVersionsGenerateSeparateServices(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.component(simple("people/basic", "2.0.0", 8080), config.Component{})

	services := servicesOf(t, b.parsed())

	assert.Contains(t, services, "people-basic-1-0-0")
	assert.Contains(t, services, "people-basic-2-0-0")
}

// 级联跳过的组件不生成 service。
func TestSkippedComponentGeneratesNoService(t *testing.T) {
	off := false
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Enabled: &off})

	assert.NotContains(t, servicesOf(t, b.parsed()), "people-basic-1-0-0")
}

// ============================================================
// P20 / P21：注入引擎的结果真的写进了文件
// ============================================================

func TestInjectedEnvironmentReachesTheFile(t *testing.T) {
	dep := simple("people/basic", "1.0.0", 8080)
	dep.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	m := dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0")
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"sessionTtlSeconds": {Type: "integer", Default: 3600},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{Config: map[string]any{"sessionTtlSeconds": 7200}})
	b.component(dep, config.Component{})

	env := envOf(t, serviceOf(t, b.parsed(), "erp-backend-1-0-0"))

	assert.Equal(t, "erp/backend", env["COMPONENT_ID"])
	assert.Equal(t, "1.0.0", env["COMPONENT_VERSION"])
	assert.Equal(t, "http://people-basic-1-0-0:8080", env["PEOPLE_BASIC_ENDPOINT"])
	// 22.3：extraPorts 的地址变量必须出现在最终的 compose 里
	assert.Equal(t, "http://people-basic-1-0-0:9090", env["PEOPLE_BASIC_GRPC_ENDPOINT"])
	assert.Equal(t, "7200", env["SESSION_TTL_SECONDS"], "config 覆盖要落到文件里")
}

// ${ENV_VAR} 必须原样保留，绝不能把展开后的密钥写进生成文件（003 §5.4）。
func TestSecretsStayAsEnvironmentReferences(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	text := string(b.generate().YAML)

	assert.Contains(t, text, "${POSTGRES_PASSWORD}", "密钥引用要原样落盘")
}

// 环境变量按名字排序：生成文件要稳定可比对，否则每次 diff 都是噪音。
func TestEnvironmentIsSorted(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"zebra": {Default: 1}, "alpha": {Default: 2},
	}}

	b := newBuilder(t)
	b.component(m, config.Component{})

	svc := serviceOf(t, b.parsed(), "people-basic-1-0-0")
	raw := svc["environment"].([]any)

	var names []string
	for _, item := range raw {
		name, _, _ := strings.Cut(item.(string), "=")
		names = append(names, name)
	}
	assert.Equal(t, []string{"ALPHA", "COMPONENT_ID", "COMPONENT_VERSION", "ZEBRA"}, names)
}

// 同一份输入生成两次，结果必须字节一致。
func TestGenerationIsDeterministic(t *testing.T) {
	build := func() string {
		b := newBuilder(t)
		b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
		b.component(simple("department/tree", "1.0.0", 8080), config.Component{})
		b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))
		return string(b.generate().YAML)
	}

	assert.Equal(t, build(), build())
}

// ============================================================
// 12.1 真实 docker compose 校验
// ============================================================

// 12.1：生成的文件必须能被真实的 docker compose 解析。
//
// 这条是这一整套测试的锚：前面所有断言都建立在"我认为 compose 长这样"上，
// 只有真的让 docker 读一遍，才知道有没有写出它不认的字段。
func TestGeneratedFileIsValidForDockerCompose(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("未安装 docker，跳过 compose 语法校验")
	}

	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))),
		config.Component{Config: map[string]any{}})
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{Expose: true, ExposePort: 18080})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	dir := t.TempDir()
	path := dir + "/docker-compose.yaml"
	require.NoError(t, writeFile(path, b.generate().YAML))

	cmd := exec.Command("docker", "compose", "-f", path, "config", "--quiet")
	cmd.Env = append(cmd.Environ(), "POSTGRES_PASSWORD=dev")
	output, err := cmd.CombinedOutput()

	require.NoError(t, err, "12.1 docker compose 无法解析生成的文件：\n%s\n----\n%s",
		output, b.generate().YAML)
	assert.NotContains(t, string(output), "warning",
		"不该有警告（如已废弃的 version 字段）：%s", output)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
