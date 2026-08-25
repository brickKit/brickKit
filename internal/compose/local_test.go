// 本文件是 Step 13「本地调试（local: true）与 local-debug.env」的业务行为测试，
// 覆盖开发计划 13.1–13.14，以及延后项 P3（localPort 自动分配）。
//
// local: true 有两个方向要打通，测试也按这两个方向组织：
//
//	容器 → 宿主机：依赖方容器用 extra_hosts 把 local 组件的服务名指到宿主机，
//	               端口换成 localPort；
//	宿主机 → 容器：local 组件在 IDE 里跑，需要访问容器里的依赖与基础资源，
//	               CLI 把它们映射到宿主机端口，并写进 local-debug.<服务名>.env。
package compose_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// ============================================================
// 夹具
// ============================================================

// generateErr 跑完整条链路但允许失败，供错误路径断言。
func (b *builder) generateErr() error {
	b.t.Helper()
	_, err := b.build(compose.Options{})
	return err
}

// docOf 把一次生成的 YAML 解析成通用结构。
//
// 有些用例既要看 compose 里的 ports，又要看 env 文件里的地址——
// 两者必须来自**同一次**生成，否则自动分配的端口对不上。
func docOf(t *testing.T, result *compose.Result) map[string]any {
	t.Helper()

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(result.YAML, &doc))
	return doc
}

// withExtraPort 给组件加一个额外端口（002 §5.4）。
func withExtraPort(m *manifest.Manifest, name string, port int) *manifest.Manifest {
	m.Deployment.ExtraPorts = append(m.Deployment.ExtraPorts,
		manifest.ExtraPort{Name: name, Port: port})
	return m
}

// localEnv 取出某个 local 组件的调试 env 文件，并解析成 map。
func localEnv(t *testing.T, result *compose.Result, service string) map[string]string {
	t.Helper()

	name := "local-debug." + service + ".env"
	for _, file := range result.LocalEnvFiles {
		if file.Name != name {
			continue
		}
		out := map[string]string{}
		for _, line := range strings.Split(string(file.Content), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			require.True(t, ok, "env 文件每一行都应是 KEY=VALUE：%q", line)
			out[key] = value
		}
		return out
	}

	t.Fatalf("应生成 %s，实际生成了 %v", name, envFileNames(result))
	return nil
}

func envFileNames(result *compose.Result) []string {
	out := make([]string, 0, len(result.LocalEnvFiles))
	for _, file := range result.LocalEnvFiles {
		out = append(out, file.Name)
	}
	return out
}

// portsOf 取出 service 的 ports 列表。
func portsOf(t *testing.T, svc map[string]any) []string {
	t.Helper()
	return stringsOf(t, svc["ports"])
}

// extraHostsOf 取出 service 的 extra_hosts 列表。
func extraHostsOf(t *testing.T, svc map[string]any) []string {
	t.Helper()
	return stringsOf(t, svc["extra_hosts"])
}

func stringsOf(t *testing.T, raw any) []string {
	t.Helper()
	if raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	require.True(t, ok, "应是字符串列表，实际是 %T", raw)

	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.(string))
	}
	return out
}

// localProject 是贯穿本文件的场景：
//
//	people/basic  local: true，在 IDE 里跑，强依赖 department/tree
//	department/tree  在容器里跑
//	erp/backend      在容器里跑，强依赖 people/basic
//
// 两个方向同时存在：erp/backend 要访问宿主机上的 people/basic，
// 而 people/basic 要访问容器里的 department/tree。
func localProject(t *testing.T, local config.Component) *builder {
	t.Helper()

	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0"),
		local)
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{})
	return b
}

// ============================================================
// 13.1 local 组件不生成容器
// ============================================================

// local 组件的迁移容器同样不生成：迁移由开发者在本机自己跑
// （容器里那份代码根本不是他正在调试的代码）。
func TestLocalComponentGeneratesNoMigrationService(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))),
		config.Component{Local: true})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	services := servicesOf(t, b.parsed())

	assert.NotContains(t, services, "people-basic-1-0-0", "13.1")
	assert.NotContains(t, services, "people-basic-1-0-0-migration",
		"13.1：local 组件不生成容器，它的迁移容器也不该生成")
}

// 迁移不再由 CLI 代跑时必须说一声，否则开发者会对着"表不存在"发懵。
func TestLocalComponentWithMigrationWarns(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))),
		config.Component{Local: true})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	result := b.generate()

	require.NotEmpty(t, result.Warnings, "local 组件带迁移时应给出提示")
	assert.Contains(t, joinWarnings(result.Warnings), "迁移")
	assert.Contains(t, joinWarnings(result.Warnings), "people/basic")
}

func joinWarnings(warnings []*clierr.Error) string {
	var b strings.Builder
	for _, w := range warnings {
		b.WriteString(w.Format())
		b.WriteString("\n")
	}
	return b.String()
}

// ============================================================
// 13.2 extra_hosts 映射
// ============================================================

func TestDependentGetsExtraHostsForLocalComponent(t *testing.T) {
	b := localProject(t, config.Component{Local: true, LocalPort: 8081})

	svc := serviceOf(t, b.parsed(), "erp-backend-1-0-0")

	assert.Equal(t, []string{"people-basic-1-0-0:host-gateway"}, extraHostsOf(t, svc), "13.2")
}

// 不依赖 local 组件的容器不该平白多出 extra_hosts。
func TestNonDependentHasNoExtraHosts(t *testing.T) {
	b := localProject(t, config.Component{Local: true, LocalPort: 8081})

	svc := serviceOf(t, b.parsed(), "department-tree-1-0-0")

	assert.Empty(t, extraHostsOf(t, svc), "13.2：department/tree 不依赖 local 组件")
}

// 完全没有 local 组件时，生成的文件里不该出现 extra_hosts。
func TestNoLocalComponentMeansNoExtraHosts(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	result := b.generate()

	assert.NotContains(t, string(result.YAML), "extra_hosts")
	assert.Empty(t, result.LocalEnvFiles, "13.4：没有 local 组件就不生成 env 文件")
}

// 多个组件同时本地调试时，依赖方要为每一个都写 extra_hosts（005 §4.4）。
func TestExtraHostsForMultipleLocalComponents(t *testing.T) {
	b := newBuilder(t)
	b.component(
		dependsOn(dependsOn(simple("erp/backend", "1.0.0", 8080),
			"people/basic", "1.0.0"), "department/tree", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8081})
	b.component(simple("department/tree", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8082})

	svc := serviceOf(t, b.parsed(), "erp-backend-1-0-0")
	env := envOf(t, svc)

	assert.ElementsMatch(t,
		[]string{"people-basic-1-0-0:host-gateway", "department-tree-1-0-0:host-gateway"},
		extraHostsOf(t, svc))
	assert.Equal(t, "http://people-basic-1-0-0:8081", env["PEOPLE_BASIC_ENDPOINT"])
	assert.Equal(t, "http://department-tree-1-0-0:8082", env["DEPARTMENT_TREE_ENDPOINT"])
}

// ============================================================
// 13.10 / 13.11 / 13.12 localPort
// ============================================================

// 13.10：用户指定了 localPort，依赖方的地址就用这个端口。
func TestExplicitLocalPortIsUsed(t *testing.T) {
	b := localProject(t, config.Component{Local: true, LocalPort: 9999})

	env := envOf(t, serviceOf(t, b.parsed(), "erp-backend-1-0-0"))

	assert.Equal(t, "http://people-basic-1-0-0:9999", env["PEOPLE_BASIC_ENDPOINT"], "13.10")
}

// 13.11：没写 localPort 时默认用组件**自己声明的主端口**。
//
// 搬到宿主机上跑的是同一份代码，它监听的还是 Manifest 里那个端口。
// 直接分配 8081 会得到一个没人监听的端口，依赖方连过去只有 connection refused
// ——这是真跑起来验出来的（调用方稳定 503）。
func TestAutoAssignedLocalPortDefaultsToDeclaredPort(t *testing.T) {
	b := localProject(t, config.Component{Local: true})

	env := envOf(t, serviceOf(t, b.parsed(), "erp-backend-1-0-0"))

	assert.Equal(t, "http://people-basic-1-0-0:8080", env["PEOPLE_BASIC_ENDPOINT"], "13.11")
}

// 声明端口被占了才退到 8081 起递增（005 §4.6）。
func TestAutoAssignedLocalPortFallsBackTo8081(t *testing.T) {
	b := newBuilder(t)
	b.component(
		dependsOn(dependsOn(simple("erp/backend", "1.0.0", 8080),
			"people/basic", "1.0.0"), "department/tree", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Local: true})
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{Local: true})

	env := envOf(t, serviceOf(t, b.parsed(), "erp-backend-1-0-0"))

	assert.NotEqual(t, env["PEOPLE_BASIC_ENDPOINT"], env["DEPARTMENT_TREE_ENDPOINT"], "13.11")
	// 按版本化服务名排序分配，结果才不会随 map 遍历顺序漂移
	assert.Equal(t, "http://department-tree-1-0-0:8080", env["DEPARTMENT_TREE_ENDPOINT"],
		"先到的用自己声明的端口")
	assert.Equal(t, "http://people-basic-1-0-0:8081", env["PEOPLE_BASIC_ENDPOINT"],
		"13.11：撞车的退到 8081")
}

// 自动分配要绕开用户已经钉死的端口，而不是硬撞上去。
func TestAutoAssignedLocalPortSkipsExplicitOne(t *testing.T) {
	b := newBuilder(t)
	b.component(
		dependsOn(dependsOn(simple("erp/backend", "1.0.0", 8080),
			"people/basic", "1.0.0"), "department/tree", "1.0.0"),
		config.Component{})
	// department/tree 钉死 8080，people/basic 想用的也是 8080，只能让开
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Local: true})
	b.component(simple("department/tree", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8080})

	env := envOf(t, serviceOf(t, b.parsed(), "erp-backend-1-0-0"))

	assert.Equal(t, "http://department-tree-1-0-0:8080", env["DEPARTMENT_TREE_ENDPOINT"])
	assert.Equal(t, "http://people-basic-1-0-0:8081", env["PEOPLE_BASIC_ENDPOINT"], "13.11")
}

// local 组件同样要连自己的库，别把它从"需要预先创建的数据库"里漏掉。
//
// 漏掉的后果是使用者照着 CLI 的清单建完库，一在 IDE 里启动就撞上
// `database "xxx" does not exist`，而 CLI 从头到尾没提过这个库。
func TestLocalComponentDatabaseIsStillReported(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)),
		config.Component{Local: true, LocalPort: 8081})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "brickkit_people"}))

	requirements := b.generate().Resources

	require.Len(t, requirements, 1)
	require.Len(t, requirements[0].Databases, 1)
	assert.Equal(t, "brickkit_people", requirements[0].Databases[0].Name)
	assert.Equal(t, []string{"people/basic"}, requirements[0].Databases[0].Components)
}

// 13.12：两个 local 组件抢同一个 localPort 直接报错。
func TestConflictingLocalPortsIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8081})
	b.component(simple("department/tree", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8081})

	err := b.generateErr()

	require.Error(t, err, "13.12")
	assert.Contains(t, err.Error(), "8081")
	assert.Equal(t, clierr.CodePortConflict, clierr.As(err).Code)
}

// localPort 撞上另一个组件的 exposePort 同样是宿主机端口冲突——
// 它们抢的是同一台机器上的同一个端口，只是来路不同。
func TestLocalPortConflictingWithExposePortIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 18080})
	b.component(simple("portal/user-frontend", "1.0.0", 8080),
		config.Component{Expose: true, ExposePort: 18080})

	err := b.generateErr()

	require.Error(t, err, "13.12")
	assert.Contains(t, err.Error(), "18080")
}

// 两个 local 组件声明了同一个额外端口：容器里互不干扰，
// 搬到宿主机上就只有一个 9090，必须在生成阶段说清楚。
func TestConflictingLocalExtraPortsIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(withExtraPort(simple("people/basic", "1.0.0", 8080), "grpc", 9090),
		config.Component{Local: true, LocalPort: 8081})
	b.component(withExtraPort(simple("department/tree", "1.0.0", 8082), "grpc", 9090),
		config.Component{Local: true, LocalPort: 8083})

	err := b.generateErr()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "9090")
	assert.Equal(t, clierr.CodePortConflict, clierr.As(err).Code)
}

// local 组件的额外端口不改写：宿主机上的进程监听的还是声明的那个端口，
// 依赖方经 extra_hosts 用同一个端口号就能到。
func TestExtraPortOfLocalComponentKeepsDeclaredPort(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(withExtraPort(simple("people/basic", "1.0.0", 8080), "grpc", 9090),
		config.Component{Local: true, LocalPort: 8081})

	env := envOf(t, serviceOf(t, b.parsed(), "erp-backend-1-0-0"))

	assert.Equal(t, "http://people-basic-1-0-0:8081", env["PEOPLE_BASIC_ENDPOINT"])
	assert.Equal(t, "http://people-basic-1-0-0:9090", env["PEOPLE_BASIC_GRPC_ENDPOINT"])
}

// ============================================================
// 13.3 / 13.13 local 组件的依赖：映射到宿主机端口
// ============================================================

// 13.3：local 组件要访问的容器依赖，自动映射一个宿主机端口。
func TestDependencyOfLocalComponentGetsHostPort(t *testing.T) {
	b := localProject(t, config.Component{Local: true, LocalPort: 8081})

	svc := serviceOf(t, b.parsed(), "department-tree-1-0-0")

	assert.Equal(t, []string{"18080:8080"}, portsOf(t, svc), "13.3")
}

// 不被任何 local 组件依赖的容器不该被平白映射到宿主机。
func TestUnrelatedComponentIsNotMappedToHost(t *testing.T) {
	b := localProject(t, config.Component{Local: true, LocalPort: 8081})

	svc := serviceOf(t, b.parsed(), "erp-backend-1-0-0")

	assert.Empty(t, portsOf(t, svc), "13.3：erp/backend 不是 local 组件的依赖")
}

// 13.13：依赖组件已经 expose 过了就用现成的端口，不重复映射。
func TestDependencyWithExposeReusesItsHostPort(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0"),
		config.Component{Local: true, LocalPort: 8081})
	b.component(simple("department/tree", "1.0.0", 8080),
		config.Component{Expose: true, ExposePort: 9100})

	result := b.generate()
	svc := serviceOf(t, docOf(t, result), "department-tree-1-0-0")

	assert.Equal(t, []string{"9100:8080"}, portsOf(t, svc), "13.13：不重复映射")
	assert.Equal(t, "http://localhost:9100",
		localEnv(t, result, "people-basic-1-0-0")["DEPARTMENT_TREE_ENDPOINT"], "13.13")
}

// 多个依赖需要映射时端口依次分配、互不冲突。
func TestMultipleDependenciesGetDistinctHostPorts(t *testing.T) {
	b := newBuilder(t)
	b.component(
		dependsOn(dependsOn(simple("people/basic", "1.0.0", 8080),
			"department/tree", "1.0.0"), "authorization/rbac", "1.0.0"),
		config.Component{Local: true, LocalPort: 8081})
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{})
	b.component(simple("authorization/rbac", "1.0.0", 8080), config.Component{})

	doc := b.parsed()
	tree := portsOf(t, serviceOf(t, doc, "department-tree-1-0-0"))
	rbac := portsOf(t, serviceOf(t, doc, "authorization-rbac-1-0-0"))

	require.Len(t, tree, 1)
	require.Len(t, rbac, 1)
	assert.NotEqual(t, tree[0], rbac[0], "13.3：两个依赖不能抢同一个宿主机端口")
}

// ============================================================
// 13.4 / 13.6 / 13.7 env 文件
// ============================================================

// 13.4 / 13.7：文件按版本化服务名命名，且带上组件身份。
func TestLocalDebugEnvFileIsGenerated(t *testing.T) {
	b := localProject(t, config.Component{Local: true, LocalPort: 8081})

	result := b.generate()
	env := localEnv(t, result, "people-basic-1-0-0")

	assert.Equal(t, []string{"local-debug.people-basic-1-0-0.env"}, envFileNames(result), "13.4")
	assert.Equal(t, "people/basic", env["COMPONENT_ID"], "13.7")
	assert.Equal(t, "1.0.0", env["COMPONENT_VERSION"], "13.7")
}

// 文件是给人打开看的，也会被 IDE 读：头部要说明来历。
func TestLocalDebugEnvFileHasHeader(t *testing.T) {
	b := localProject(t, config.Component{Local: true, LocalPort: 8081})

	result := b.generate()
	require.Len(t, result.LocalEnvFiles, 1)
	text := string(result.LocalEnvFiles[0].Content)

	assert.Contains(t, text, "由 BrickKit CLI 自动生成")
	assert.Contains(t, text, "people/basic@1.0.0")
	assert.Contains(t, text, "8081", "要写清这个进程该监听哪个端口")
}

// 13.6：同一组件的两个版本同时本地调试，两份 env 文件互不覆盖。
func TestMultipleVersionsGetSeparateEnvFiles(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8081})
	b.component(simple("people/basic", "2.0.0", 8080),
		config.Component{Local: true, LocalPort: 8082})

	result := b.generate()

	assert.ElementsMatch(t, []string{
		"local-debug.people-basic-1-0-0.env",
		"local-debug.people-basic-2-0-0.env",
	}, envFileNames(result), "13.6")
	assert.Equal(t, "1.0.0", localEnv(t, result, "people-basic-1-0-0")["COMPONENT_VERSION"])
	assert.Equal(t, "2.0.0", localEnv(t, result, "people-basic-2-0-0")["COMPONENT_VERSION"])
}

// ============================================================
// 13.5 env 文件里的依赖地址指向 localhost
// ============================================================

func TestLocalDebugEnvPointsDependenciesAtLocalhost(t *testing.T) {
	b := localProject(t, config.Component{Local: true, LocalPort: 8081})

	env := localEnv(t, b.generate(), "people-basic-1-0-0")

	assert.Equal(t, "http://localhost:18080", env["DEPARTMENT_TREE_ENDPOINT"], "13.5")
}

// 两个 local 组件互相依赖时，彼此都在宿主机上，直接走各自的 localPort。
func TestLocalDependencyOnAnotherLocalComponentUsesItsLocalPort(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0"),
		config.Component{Local: true, LocalPort: 8081})
	b.component(simple("department/tree", "1.0.0", 8080),
		config.Component{Local: true, LocalPort: 8082})

	env := localEnv(t, b.generate(), "people-basic-1-0-0")

	assert.Equal(t, "http://localhost:8082", env["DEPARTMENT_TREE_ENDPOINT"])
}

// 额外端口同样要能从宿主机访问（002 §5.4、P21）。
func TestLocalDebugEnvMapsExtraPorts(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{Local: true, LocalPort: 8081})
	b.component(withExtraPort(simple("people/basic", "1.0.0", 8080), "grpc", 9090),
		config.Component{})

	result := b.generate()
	env := localEnv(t, result, "erp-backend-1-0-0")
	ports := portsOf(t, serviceOf(t, docOf(t, result), "people-basic-1-0-0"))

	assert.Equal(t, "http://localhost:18080", env["PEOPLE_BASIC_ENDPOINT"])
	assert.Equal(t, "http://localhost:19090", env["PEOPLE_BASIC_GRPC_ENDPOINT"])
	assert.ElementsMatch(t, []string{"18080:8080", "19090:9090"}, ports)
}

// ⚠️ 回归：依赖**既 expose 又有 extraPorts** 时，额外端口也必须映射。
//
// `expose: true` 只发布主端口。而 mapDependencyToHost 从前在 exposedPort
// 命中时整个 return，于是这一种组合下额外端口一个都不映射——compose 里
// 只有 "8090:8080"，local-debug.env 里却照样写着 http://localhost:9090，
// 而宿主机的 9090 根本没人监听。
//
// 表现是 HTTP 通、gRPC 稳定 connection refused，两边配置看上去都没毛病：
// 使用者会一路去查 gRPC 客户端、查组件代码，而问题在一行 return 上。
func TestLocalDebugEnvMapsExtraPortsOfExposedDependency(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{Local: true, LocalPort: 8081})
	b.component(withExtraPort(simple("people/basic", "1.0.0", 8080), "grpc", 9090),
		config.Component{Expose: true, ExposePort: 8090})

	result := b.generate()
	env := localEnv(t, result, "erp-backend-1-0-0")
	ports := portsOf(t, serviceOf(t, docOf(t, result), "people-basic-1-0-0"))

	// 主端口用 expose already 映射好的那个，不重复占一个宿主机端口
	assert.Equal(t, "http://localhost:8090", env["PEOPLE_BASIC_ENDPOINT"])
	// 额外端口必须另外映射一个出来
	assert.Equal(t, "http://localhost:19090", env["PEOPLE_BASIC_GRPC_ENDPOINT"])
	assert.ElementsMatch(t, []string{"8090:8080", "19090:9090"}, ports,
		"expose 只管主端口，额外端口得自己映射")
}

// env 文件里写的每个 localhost 端口，compose 里都必须真有人把它发布出来。
//
// 这条守的是**不变量**而不是某一种组合：宿主机上的进程照着 env 文件去连，
// 连到一个没发布的端口就是 connection refused，而它没有任何线索指向端口映射。
// 上面那条 bug 正是这个不变量被破坏的一个实例。
func TestEveryLocalhostEndpointIsActuallyPublished(t *testing.T) {
	cases := []struct {
		name  string
		entry config.Component
	}{
		{"依赖不 expose", config.Component{}},
		{"依赖 expose 且自定义端口", config.Component{Expose: true, ExposePort: 8090}},
		{"依赖 expose 用默认端口", config.Component{Expose: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBuilder(t)
			b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
				config.Component{Local: true, LocalPort: 8081})
			b.component(withExtraPort(simple("people/basic", "1.0.0", 8080), "grpc", 9090),
				tc.entry)

			result := b.generate()
			env := localEnv(t, result, "erp-backend-1-0-0")
			published := map[string]bool{}
			for _, mapping := range portsOf(t, serviceOf(t, docOf(t, result), "people-basic-1-0-0")) {
				host, _, _ := strings.Cut(mapping, ":")
				published[host] = true
			}

			for name, value := range env {
				rest, ok := strings.CutPrefix(value, "http://localhost:")
				if !ok {
					continue
				}
				assert.True(t, published[rest],
					"%s=%s，但 compose 里没有把宿主机 %s 发布出来（已发布：%v）",
					name, value, rest, published)
			}
		})
	}
}

// 弱依赖没启动时，env 文件里同样一个字都不该有（002 §3.4）。
func TestLocalDebugEnvOmitsMissingWeakDependency(t *testing.T) {
	b := newBuilder(t)
	m := simple("people/basic", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{Components: []manifest.ComponentDep{
		{ID: "infra/bus", Version: "1.0.0", Optional: true},
	}}
	b.component(m, config.Component{Local: true, LocalPort: 8081})

	text := string(b.generate().LocalEnvFiles[0].Content)

	assert.NotContains(t, text, "INFRA_BUS_ENDPOINT")
}

// ============================================================
// 13.8 env 文件里的资源连接
// ============================================================

// 资源跑在**本机**时，容器里写的是 host.docker.internal，
// 而这个名字在 Linux 的宿主机上解析不了——它是 Docker 注入到容器
// /etc/hosts 里的。给 IDE 的那份必须换成 localhost。
//
// 不换的话，IDE 里的进程拿着一个解析不了的主机名去连库，
// 而容器里的同一个组件跑得好好的，最难联想到是这里。
func TestLocalDebugEnvRewritesHostMachineAliasToLocalhost(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)),
		config.Component{Local: true, LocalPort: 8081})
	r := pgResource(config.Binding{ComponentID: "people/basic", Database: "brickkit_people"})
	r.Host = "host.docker.internal"
	b.resource(r)

	result := b.generate()
	env := localEnv(t, result, "people-basic-1-0-0")

	assert.Equal(t, "localhost", env["DATABASE_HOST"], "13.8")
	assert.Equal(t, "5432", env["DATABASE_PORT"], "端口不用改：资源本来就在宿主机上")
	assert.Equal(t, "brickkit_people", env["DATABASE_NAME"], "库名不变，变的只是怎么连过去")
}

// 外部资源（运维已部署）本来就在宿主机之外，地址原样保留——
// 改写成 localhost 只会让本地进程连到一个不存在的服务。
func TestLocalDebugEnvKeepsExternalResourceHost(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)),
		config.Component{Local: true, LocalPort: 8081})
	external := pgResource(config.Binding{ComponentID: "people/basic", Database: "brickkit_people"})
	external.Host = "db.internal.example.com"
	b.resource(external)

	env := localEnv(t, b.generate(), "people-basic-1-0-0")

	assert.Equal(t, "db.internal.example.com", env["DATABASE_HOST"], "13.8")
	assert.Equal(t, "5432", env["DATABASE_PORT"])
}

// 密码是 ${VAR} 引用时保持原样：env 文件由 shell / IDE 再展开，
// CLI 在这里替换掉反而会把密码落进磁盘。
func TestLocalDebugEnvKeepsSecretReference(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)),
		config.Component{Local: true, LocalPort: 8081})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "brickkit_people"}))

	env := localEnv(t, b.generate(), "people-basic-1-0-0")

	assert.Equal(t, "${POSTGRES_PASSWORD}", env["DATABASE_PASSWORD"])
}

// ============================================================
// 13.9 env 文件里的 config
// ============================================================

func TestLocalDebugEnvContainsConfigValues(t *testing.T) {
	b := newBuilder(t)
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{
		Type: "object",
		Properties: map[string]manifest.ConfigProperty{
			"pageSize": {Type: "integer", Default: 20},
			"logLevel": {Type: "string", Default: "info"},
		},
	}
	b.component(m, config.Component{
		Local: true, LocalPort: 8081,
		Config: map[string]any{"logLevel": "debug"},
	})

	env := localEnv(t, b.generate(), "people-basic-1-0-0")

	assert.Equal(t, "debug", env["LOG_LEVEL"], "13.9：覆盖值优先")
	assert.Equal(t, "20", env["PAGE_SIZE"], "13.9：没覆盖的用默认值")
}

// ============================================================
// 13.14 Podman
// ============================================================

// ============================================================
// 确定性
// ============================================================

// 端口是自动分配的，更要保证两次生成完全一致，否则 git diff 全是噪音。
func TestLocalPortAllocationIsDeterministic(t *testing.T) {
	build := func() *compose.Result {
		b := newBuilder(t)
		b.component(
			dependsOn(dependsOn(simple("people/basic", "1.0.0", 8080),
				"department/tree", "1.0.0"), "authorization/rbac", "1.0.0"),
			config.Component{Local: true})
		b.component(simple("department/tree", "1.0.0", 8080), config.Component{})
		b.component(simple("authorization/rbac", "1.0.0", 8080), config.Component{})
		return b.generate()
	}

	first, second := build(), build()

	assert.Equal(t, string(first.YAML), string(second.YAML))
	require.Len(t, second.LocalEnvFiles, 1)
	assert.Equal(t, string(first.LocalEnvFiles[0].Content), string(second.LocalEnvFiles[0].Content))
}

// local 组件自己不在容器里，服务名却仍要能被解析——
// 生成的文件必须能被 docker compose 接受。
func TestLocalModeFileIsValidForDockerCompose(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("未安装 docker，跳过 compose 语法校验")
	}

	b := localProject(t, config.Component{Local: true, LocalPort: 8081})
	yamlBytes := b.generate().YAML

	dir := t.TempDir()
	path := dir + "/docker-compose.yaml"
	require.NoError(t, writeFile(path, yamlBytes))

	output, err := exec.Command("docker", "compose", "-f", path, "config", "--quiet").
		CombinedOutput()

	require.NoError(t, err, "生成的文件 docker compose 解析不了：\n%s\n----\n%s", output, yamlBytes)
}

// local-debug 环境变量文件里的 ${VAR} 必须在生成时求值。
//
// 这个文件是给 **IDE** 读的（VS Code 的 envFile、IntelliJ 的 EnvFile 插件），
// 它们都**不做变量替换**——留着占位符，IDE 里的进程就会拿着字面量
// "${PG_PASSWORD}" 去连库，认证失败，而 .env 里明明写着密码。
//
// 与 compose 文件本身的处理**刻意不同**：那份文件由 docker compose 自己
// 从 .env 展开，所以必须留占位符，绝不能把明文密码写进去。
func TestLocalEnvFileResolvesPlaceholders(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)),
		config.Component{Local: true})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	result, err := b.build(compose.Options{
		Lookup: func(name string) (string, bool) {
			if name == "POSTGRES_PASSWORD" {
				return "s3cr3t", true
			}
			return "", false
		},
	})
	require.NoError(t, err)
	require.Len(t, result.LocalEnvFiles, 1)

	text := string(result.LocalEnvFiles[0].Content)

	assert.Contains(t, text, "DATABASE_PASSWORD=s3cr3t", "IDE 不做变量替换，必须给真值")
	assert.NotContains(t, text, "${POSTGRES_PASSWORD}")
}

// compose 文件本身照旧留占位符：那份文件会被人打开看、进 git diff，
// 密码进去就等于泄露；而 docker compose 会自己从 .env 展开。
func TestComposeFileKeepsPlaceholders(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	result, err := b.build(compose.Options{
		Lookup: func(string) (string, bool) { return "s3cr3t", true },
	})
	require.NoError(t, err)

	text := string(result.YAML)

	assert.Contains(t, text, "${POSTGRES_PASSWORD}", "compose 自己会展开")
	assert.NotContains(t, text, "s3cr3t", "明文密码绝不能写进 compose 文件")
}

// extra_hosts 里必须是 `host-gateway`，绝不能是 `host.containers.internal`。
//
// 这条断言保留着一段历史：设计书原来写"Podman 用 host.containers.internal 替代"，
// 那是把两件事搞混了——`host.containers.internal` 是**自动注入到 /etc/hosts 的
// 主机名**，不是 `--add-host` 能接受的**值**。按原文生成的话容器根本创建不出来。
// Podman 支持已经移除（005 §7），但这个错误的值一旦被谁"顺手补回来"，
// Docker 上同样是坏的，所以断言留着。
func TestExtraHostsUsesHostGateway(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Local: true})
	b.component(dependsOn(simple("department/tree", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})

	result, err := b.build(compose.Options{Engine: compose.EngineDocker})
	require.NoError(t, err)

	text := string(result.YAML)
	assert.Contains(t, text, "people-basic-1-0-0:host-gateway")
	assert.NotContains(t, text, "host.containers.internal",
		"这个名字不能出现在 extra_hosts 里——它是主机名，不是 add-host 的值")
}

// ============================================================
// 宿主机上的资源（P34 —— 真实装配时踩到的）
// ============================================================

// TestHostMachineResourceGetsExtraHosts 是这条修复存在的理由。
//
// 把资源 host 写成 host.docker.internal 是完全合理的写法——"连宿主机上那个
// 已经跑着的库"。但它带点，不会被判成服务名，因此 CLI 不托管它；
// 而容器里默认解析不了这个名字，迁移容器直接报：
//
//	dial tcp: lookup host.docker.internal ... no such host
//
// 症状（组件连不上库）与原因（少了一行 extra_hosts）完全不搭。
func TestHostMachineResourceGetsExtraHosts(t *testing.T) {
	doc := newBuilder(t).
		component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{}).
		resource(hostMachineDatabase("people/basic")).
		parsed()

	hosts := stringsOf(t, serviceOf(t, doc, "people-basic-1-0-0")["extra_hosts"])
	assert.Contains(t, hosts, "host.docker.internal:host-gateway",
		"宿主机上的资源要靠 extra_hosts 才解析得了")
}

// TestMigrationAlsoGetsExtraHosts：迁移容器同样需要。
//
// 迁移是**第一个**连库的东西。主容器有 extra_hosts、迁移容器没有的话，
// 迁移会先失败，而平台会把它当成"迁移失败"阻断整个启动。
func TestMigrationAlsoGetsExtraHosts(t *testing.T) {
	doc := newBuilder(t).
		component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{}).
		resource(hostMachineDatabase("people/basic")).
		parsed()

	hosts := stringsOf(t, serviceOf(t, doc, "people-basic-1-0-0-migration")["extra_hosts"])
	assert.Contains(t, hosts, "host.docker.internal:host-gateway",
		"迁移是第一个连库的东西，它也得能解析这个名字")
}

// TestUnboundComponentGetsNoExtraHosts：没绑这个资源的组件不该被加上。
//
// 无差别地给所有容器加 extra_hosts 也能"跑通"，但那会让生成的文件
// 说不清"这个容器到底要连宿主机上的什么"。
func TestUnboundComponentGetsNoExtraHosts(t *testing.T) {
	caller := dependsOn(simple("demo/caller", "1.0.0", 8080), "people/basic", "1.0.0")

	doc := newBuilder(t).
		component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{}).
		component(caller, config.Component{}).
		resource(hostMachineDatabase("people/basic")).
		parsed()

	assert.NotContains(t, serviceOf(t, doc, "demo-caller-1-0-0"), "extra_hosts",
		"没绑这个资源的组件不该被加上宿主机映射")
}

// host 写成服务名时，既不生成容器也不加 extra_hosts——那个名字是使用者自己的事。
//
// 这条从前是反过来的（"host 是服务名时 CLI 仍然自己起容器"）。
// 平台不再部署基础资源之后，这个写法只会换来一条警告 + 运行时的 no such host，
// 所以它现在守的是"平台确实什么都没做"。
func TestServiceNameResourceIsNotManaged(t *testing.T) {
	doc := newBuilder(t).
		component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{}).
		resource(config.Resource{
			ID: "pg", Kind: config.ResourceKindDatabase, Engine: "postgresql",
			Host: "postgres", Port: 5432, Username: "postgres", Password: "q",
			Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people"}},
		}).
		parsed()

	assert.NotContains(t, servicesOf(t, doc), "postgres")
	assert.NotContains(t, serviceOf(t, doc, "people-basic-1-0-0"), "extra_hosts",
		"只有 host.docker.internal 才需要 extra_hosts")
}

// hostMachineDatabase 造一个"跑在宿主机上"的数据库资源。
func hostMachineDatabase(componentID string) config.Resource {
	return config.Resource{
		ID: "pg", Kind: config.ResourceKindDatabase, Engine: "postgresql",
		Host: "host.docker.internal", Port: 5432, Username: "postgres", Password: "q",
		Bindings: []config.Binding{{ComponentID: componentID, Database: "people"}},
	}
}
