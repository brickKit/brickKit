// 本文件测试 servedBy（外壳合并部署）在 Docker 目标下的渲染，覆盖
// servedBy 设计书 §6-§8。local: true 的既有行为不受影响，回归覆盖见
// TestLocalStillWorksAlongsideServedBy。
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
			assert.Contains(t, w.Format(), "外壳")
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
		if w.Code == clierr.CodeConfigInvalid && strings.Contains(w.Format(), "健康检查") {
			found = true
		}
	}
	assert.True(t, found, "应该有一条关于健康检查不生效的警告：%+v", result.Warnings)
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
