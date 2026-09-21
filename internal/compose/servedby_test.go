// 本文件测试 servedBy（外壳合并部署）在 Docker 目标下的渲染，覆盖
// servedBy 设计书 §6-§8。local: true 的既有行为不受影响，回归覆盖见
// TestLocalStillWorksAlongsideServedBy；local 组件依赖 servedBy 成员时的
// 宿主机端口映射，见文件末尾 "local: true 依赖 servedBy 成员" 一节。
package compose_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedByEntry 是一个 servedBy 组件的 brickkit.yaml 条目。
func servedByEntry(shellID, shellVersion string) config.Component {
	return config.Component{ServedBy: shellID + "@" + shellVersion}
}

// ---- 不生成自己的容器/迁移 ----

func TestServedByComponentGeneratesNoContainer(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(withMigration(simple("mdm/customer", "1.0.7", 8080)),
		servedByEntry("infra/shell-go-core", "1.0.0"))

	services := servicesOf(t, b.parsed())
	assert.NotContains(t, services, "mdm-customer-1-0-7", "servedBy 组件不该有自己的 service")
	assert.NotContains(t, services, "mdm-customer-1-0-7-migration", "也不该有自己的迁移 service")
	assert.Contains(t, services, "infra-shell-go-core-1-0-0", "外壳自己照常生成")
}

// ---- 网络别名 ----

func TestShellGetsNetworkAliasForEachMember(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	svc := serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0")
	networks, ok := svc["networks"].(map[string]any)
	require.True(t, ok, "有 servedBy 成员时，networks 必须是带别名的映射形式：%v", svc["networks"])
	net, ok := networks["brickkit-net"].(map[string]any)
	require.True(t, ok)
	aliases, ok := net["aliases"].([]any)
	require.True(t, ok)
	assert.Contains(t, aliases, "mdm-customer-1-0-7")
}

func TestShellWithoutMembersUsesPlainNetworkList(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})

	svc := serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0")
	_, isList := svc["networks"].([]any)
	assert.True(t, isList, "没有 servedBy 成员时，普通组件的 networks 渲染形式不该变")
}

// ---- 合并环境变量 + BRICKKIT_SERVED_MEMBERS ----

func TestShellEnvGetsMergedEndpointsAndServedMembers(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0"),
		servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("infra/database", "1.0.0", 5432), config.Component{})

	env := envOf(t, serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0"))
	assert.Equal(t, "http://infra-database-1-0-0:5432", env["INFRA_DATABASE_ENDPOINT"])
	assert.Equal(t, "mdm-customer-1-0-7", env[shell.EnvVarServedMembers])
}

func TestShellServedMembersIsEmptyStringWhenMemberNotRunning(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	disabled := false
	b.component(simple("mdm/customer", "1.0.7", 8080),
		config.Component{ServedBy: "infra/shell-go-core@1.0.0", Enabled: &disabled})

	env := envOf(t, serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0"))
	value, ok := env[shell.EnvVarServedMembers]
	require.True(t, ok, "外壳一直在跑，变量必须存在，即使是空字符串")
	assert.Equal(t, "", value)
}

// ---- 迁移 / 健康检查 / 不支持字段的警告 ----

func TestServedByMigrationWarns(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(withMigration(simple("mdm/customer", "1.0.7", 8080)), servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build(compose.Options{})
	require.NoError(t, err)
	// 注意：simple() 默认带一个 HTTP 健康检查，所以这个组件同时触发
	// 迁移警告与健康检查警告（两条各自独立，见 servedHealthCheckWarnings），
	// 这里只断言迁移那一条存在，测健康检查警告的是 TestServedByHealthCheckWarns。
	found := false
	for _, w := range result.Warnings {
		if w.Code == clierr.CodeMigrationSkipped {
			found = true
			assert.Contains(t, w.Format(), "Shell")
		}
	}
	assert.True(t, found, "应该有一条关于迁移不会自动执行的警告：%+v", result.Warnings)
}

func TestServedByHealthCheckWarns(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build(compose.Options{})
	require.NoError(t, err)
	found := false
	for _, w := range result.Warnings {
		if w.Code == clierr.CodeConfigInvalid && strings.Contains(w.Format(), "health check") {
			found = true
		}
	}
	assert.True(t, found, "应该有一条关于健康检查不生效的警告：%+v", result.Warnings)
}

// ---- labels：成员自己的不参与合并，只有外壳自己的算数 ----

// 两个成员各自声明了同名不同值的标签（典型例子：prometheus.io/port，
// 值本该是各自的端口号）——从前会被当成"同名不同值"报错，真实的 11 个
// Go 组件几乎必然撞上这条（brickKit 反馈：servedBy 的 labels 合并漏了
// 排除规则）。现在成员的 labels 完全不参与合并，生成必须成功，且外壳
// 自己的 service 上不该出现任何一个成员的标签值。
func TestServedByMemberLabelsDoNotAffectShellService(t *testing.T) {
	shell := simple("infra/shell-go-core", "1.0.0", 9000)
	a := simple("mdm/customer", "1.0.7", 8080)
	a.Deployment.Labels = map[string]string{"prometheus.io/port": "8080"}
	b := simple("mdm/product", "1.0.9", 8082)
	b.Deployment.Labels = map[string]string{"prometheus.io/port": "8082"}

	b2 := newBuilder(t)
	b2.component(shell, config.Component{})
	b2.component(a, servedByEntry("infra/shell-go-core", "1.0.0"))
	b2.component(b, servedByEntry("infra/shell-go-core", "1.0.0"))

	svc := serviceOf(t, b2.parsed(), "infra-shell-go-core-1-0-0")
	_, present := svc["labels"]
	assert.False(t, present, "外壳自己没声明 labels，成员的不该被合并上来：%v", svc)
}

// 外壳自己的 labels 不受影响，成员声明了什么都不会覆盖或污染它。
func TestServedByShellOwnLabelsAreUnaffectedByMembers(t *testing.T) {
	shell := simple("infra/shell-go-core", "1.0.0", 9000)
	shell.Deployment.Labels = map[string]string{"team.owner": "platform"}
	member := simple("mdm/customer", "1.0.7", 8080)
	member.Deployment.Labels = map[string]string{"team.owner": "mdm", "prometheus.io/port": "8080"}

	b := newBuilder(t)
	b.component(shell, config.Component{})
	b.component(member, servedByEntry("infra/shell-go-core", "1.0.0"))

	labels := labelsOf(t, serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0"))
	assert.Equal(t, map[string]string{"team.owner": "platform"}, labels,
		"外壳自己的 labels 原样保留，成员的一个键都不该混进来")
}

// 成员声明了 labels（不管是 component.yaml 还是 brickkit.yaml 覆盖）就该
// 警告——它没有自己的容器，这些 labels 落不到任何地方。
func TestServedByLabelsWarn(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	member := simple("mdm/customer", "1.0.7", 8080)
	member.Deployment.Labels = map[string]string{"prometheus.io/port": "8080"}
	b.component(member, servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build(compose.Options{})
	require.NoError(t, err)
	var found string
	for _, w := range result.Warnings {
		if strings.Contains(w.Format(), "labels") {
			found = w.Format()
		}
	}
	require.NotEmpty(t, found, "应该有一条关于 labels 不生效的警告：%+v", result.Warnings)
	assert.Contains(t, found, "mdm/customer")
}

func TestServedByUnsupportedFieldsWarn(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Expose = true
	entry.Hostname = "mdm.example.com"
	b.component(simple("mdm/customer", "1.0.7", 8080), entry)

	result, err := b.build(compose.Options{})
	require.NoError(t, err)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w.Format(), "expose") {
			found = true
		}
	}
	assert.True(t, found, "应该警告 expose 不生效：%+v", result.Warnings)
}

// ---- local: true 回归：完全不受影响 ----

func TestLocalStillWorksAlongsideServedBy(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("erp/backend", "1.0.0", 8080), config.Component{Local: true, LocalPort: 8888})

	doc := b.parsed()
	services := servicesOf(t, doc)
	assert.NotContains(t, services, "erp-backend-1-0-0", "local: true 组件依旧不生成容器")
	assert.NotContains(t, services, "mdm-customer-1-0-7")
}

// ---- 版本迁移期间的混合场景：旧调用方依赖的旧版本独立部署，
// 新调用方依赖的新版本收编进外壳，互不干扰、都不用感知对方存在 ----

func TestOldCallerAndNewCallerGetDifferentAddressesForDifferentVersions(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), config.Component{})                            // 旧版本，独立部署
	b.component(simple("mdm/customer", "2.0.0", 8081), servedByEntry("infra/shell-go-core", "1.0.0")) // 新版本，收编
	b.component(dependsOn(simple("erp/legacy-caller", "1.0.0", 8080), "mdm/customer", "1.0.7"), config.Component{})
	b.component(dependsOn(simple("erp/new-caller", "1.0.0", 8080), "mdm/customer", "2.0.0"), config.Component{})

	doc := b.parsed()
	legacyEnv := envOf(t, serviceOf(t, doc, "erp-legacy-caller-1-0-0"))
	newEnv := envOf(t, serviceOf(t, doc, "erp-new-caller-1-0-0"))

	// 两边调用方拿到的都是各自依赖版本**自己的**版本化服务名——`inject.Build`
	// 完全不知道 servedBy 存在，从不改写地址值；地址真正指向外壳，靠的是
	// 外壳容器挂上 mdm-customer-2-0-0 这个网络别名（TestShellGetsNetworkAliasForEachMember
	// 已经验证过这一半），不是靠改写调用方的环境变量值。这正是"调用方永远
	// 不需要知道对方是不是被收编"这条设计承诺的字面体现：地址字符串本身
	// 与独立部署时一模一样，只是它现在解析到别处。
	assert.Equal(t, "http://mdm-customer-1-0-7:8080", legacyEnv["MDM_CUSTOMER_ENDPOINT"],
		"旧调用方依赖的旧版本自己独立部署，地址指向它自己的 service")
	assert.Equal(t, "http://mdm-customer-2-0-0:8081", newEnv["MDM_CUSTOMER_ENDPOINT"],
		"新调用方依赖的新版本被收编，地址值依然是它自己的版本化服务名——"+
			"重定向发生在网络层（外壳的别名），不是在这个环境变量的值上")
}

// ---- servedBy + depends_on：依赖被收编成员时要等外壳，而不是不写 depends_on ----

func TestServedByMemberDependencyGetsDependsOn(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(dependsOn(simple("erp/caller", "1.0.0", 8080), "mdm/customer", "1.0.7"), config.Component{})

	svc := serviceOf(t, b.parsed(), "erp-caller-1-0-0")
	dependsOn, ok := svc["depends_on"].(map[string]any)
	require.True(t, ok, "依赖被收编成员时也必须有 depends_on，等的是外壳：%v", svc)

	dep, ok := dependsOn["infra-shell-go-core-1-0-0"].(map[string]any)
	require.True(t, ok, "depends_on 的目标应该是外壳自己的 service，不是成员自己（它没有 service）：%v", keysOf(dependsOn))
	assert.Equal(t, "service_healthy", dep["condition"], "等外壳健康，因为外壳有健康检查")
}

// ---- local: true 依赖 servedBy 成员：映射到外壳身上的宿主机端口 ----
//
// brickKit 反馈：local 组件依赖 servedBy 成员时本地调试地址错误。
// mapDependencyToHost 对 servedBy 成员整个 return（它没有自己的 compose
// service block），额外端口那条路径从前误把这当成"依赖也是 local"的唯一
// 剩余情况，凭空拼出一个 localhost:<声明端口>——宿主机上根本没人监听。
//
// 现在 mapDependencyToHost 认得出这是一个 servedBy 成员，把宿主机端口
// 映射发布到它的外壳身上——外壳容器本来就要在这个端口上监听（成员自己
// 声明的端口），这是 internal/shell/shell.go 的 checkPortConflicts 已经
// 校验过的不变量，不是这里凭空新增的假设。

func TestLocalDependencyOnServedByMemberGetsHostPortOnShell(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(withExtraPort(simple("mdm/customer", "1.0.9", 8080), "grpc", 9090),
		servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(dependsOn(simple("infra/bff-mobile", "1.0.19", 8080), "mdm/customer", "1.0.9"),
		config.Component{Local: true, LocalPort: 8081})

	result := b.generate()
	doc := docOf(t, result)
	env := localEnv(t, result, "infra-bff-mobile-1-0-19")
	shellPorts := portsOf(t, serviceOf(t, doc, "infra-shell-go-core-1-0-0"))

	assert.Equal(t, "http://localhost:18080", env["MDM_CUSTOMER_ENDPOINT"])
	assert.Equal(t, "http://localhost:19090", env["MDM_CUSTOMER_GRPC_ENDPOINT"])
	// 端口映射发布在外壳身上——mdm/customer 自己没有 compose service，
	// 没有地方可以发布这条 ports:。
	assert.ElementsMatch(t, []string{"18080:8080", "19090:9090"}, shellPorts)
	assert.NotContains(t, servicesOf(t, doc), "mdm-customer-1-0-9",
		"servedBy 成员依旧不生成自己的 service")
}

// 两个不同的 local 组件依赖同一个 servedBy 成员：只应该映射、发布一次，
// 不能在外壳的 ports: 列表里重复出现同一条。
func TestLocalDependencyOnServedByMemberIsMappedOnceForMultipleLocalCallers(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.9", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(dependsOn(simple("infra/bff-mobile", "1.0.19", 8080), "mdm/customer", "1.0.9"),
		config.Component{Local: true, LocalPort: 8081})
	b.component(dependsOn(simple("infra/bff-web", "1.0.0", 8080), "mdm/customer", "1.0.9"),
		config.Component{Local: true, LocalPort: 8082})

	result := b.generate()
	doc := docOf(t, result)
	mobileEnv := localEnv(t, result, "infra-bff-mobile-1-0-19")
	webEnv := localEnv(t, result, "infra-bff-web-1-0-0")
	shellPorts := portsOf(t, serviceOf(t, doc, "infra-shell-go-core-1-0-0"))

	assert.Equal(t, "http://localhost:18080", mobileEnv["MDM_CUSTOMER_ENDPOINT"])
	assert.Equal(t, "http://localhost:18080", webEnv["MDM_CUSTOMER_ENDPOINT"],
		"两个调用方共享同一个宿主机端口，不应该各分配一个")
	assert.Equal(t, []string{"18080:8080"}, shellPorts, "只应该发布一条，不能重复")
}

// 同一个外壳收编了两个不同的成员，各自被 local 组件依赖：两个成员各自
// 声明的端口都要单独映射、单独发布，互不覆盖。
func TestLocalDependenciesOnDifferentServedByMembersOfSameShellGetDistinctPorts(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.9", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("erp/sales", "2.0.0", 8081), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(dependsOn(simple("infra/bff-mobile", "1.0.19", 8080), "mdm/customer", "1.0.9"),
		config.Component{Local: true, LocalPort: 8082})
	b.component(dependsOn(simple("erp/legacy-caller", "1.0.0", 8080), "erp/sales", "2.0.0"),
		config.Component{Local: true, LocalPort: 8083})

	result := b.generate()
	doc := docOf(t, result)
	shellPorts := portsOf(t, serviceOf(t, doc, "infra-shell-go-core-1-0-0"))

	assert.Equal(t, "http://localhost:18080", localEnv(t, result, "infra-bff-mobile-1-0-19")["MDM_CUSTOMER_ENDPOINT"])
	assert.Equal(t, "http://localhost:18081", localEnv(t, result, "erp-legacy-caller-1-0-0")["ERP_SALES_ENDPOINT"])
	assert.ElementsMatch(t, []string{"18080:8080", "18081:8081"}, shellPorts)
}

// 依赖既不是 local 也不是 servedBy 成员时，外壳不该被平白发布端口——
// 回归覆盖，避免以后改动误伤普通场景。
func TestShellWithoutLocalDependentsPublishesNoExtraPorts(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.9", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	svc := serviceOf(t, b.parsed(), "infra-shell-go-core-1-0-0")

	assert.Empty(t, portsOf(t, svc))
}
