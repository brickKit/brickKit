// 本文件测试 servedBy（外壳合并部署）在 K8s 目标下的渲染，覆盖 servedBy
// 设计书 §6-§9。local: true 在 K8s 下依旧照常拒绝，回归覆盖见
// TestLocalStillRejectedAlongsideServedBy。
package k8s_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/shell"
)

func servedByEntry(shellID, shellVersion string) config.Component {
	return config.Component{ServedBy: shellID + "@" + shellVersion}
}

// ---- 不生成 Deployment/Job，只生成一个指向外壳 Pod 的 Service ----

func TestServedByComponentGeneratesOnlyAService(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(migrating(simple("mdm/customer", "1.0.7", 8080)),
		servedByEntry("infra/shell-go-core", "1.0.0"))

	result := b.generate()
	assert.False(t, hasFile(result, "deployments/mdm-customer-1-0-7.yaml"),
		"servedBy 组件不该有自己的 Deployment")
	assert.True(t, hasFile(result, "services/mdm-customer-1-0-7.yaml"),
		"但要有一个 Service 让它自己的服务名能被解析")
	assert.False(t, hasFile(result, "migrations/mdm-customer-1-0-7-migration.yaml"),
		"也不该有自己的迁移 Job")
	assert.True(t, hasFile(result, "deployments/infra-shell-go-core-1-0-0.yaml"),
		"外壳自己照常生成 Deployment")
}

func TestServedByServiceSelectsShellPod(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	doc := b.doc("services/mdm-customer-1-0-7.yaml")
	assert.Equal(t, "infra-shell-go-core-1-0-0", dig(t, doc, "spec", "selector", "app"),
		"selector 要指向外壳的 Pod，不是它自己（它没有自己的 Pod）")

	ports, ok := dig(t, doc, "spec", "ports").([]any)
	require.True(t, ok)
	require.Len(t, ports, 1)
	assert.Equal(t, 8080, ports[0].(map[string]any)["targetPort"], "端口是它自己声明的端口")
}

// ---- 合并环境变量 + BRICKKIT_SERVED_MEMBERS ----

func TestShellDeploymentGetsMergedEndpointsAndServedMembers(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0"),
		servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("infra/database", "1.0.0", 5432), config.Component{})

	env := envOf(t, b.container("infra-shell-go-core-1-0-0"))
	assert.Equal(t, "http://infra-database-1-0-0:5432", env["INFRA_DATABASE_ENDPOINT"])
	assert.Equal(t, "mdm-customer-1-0-7", env[shell.EnvVarServedMembers])
}

// 一个组件声明了 servedBy，但它自己当前被 enabled: false 关掉——它压根
// 不出现在 states.Running() 里，shell.Resolve 因此不会为这个外壳产出任何
// Group。但外壳本身还在跑，BRICKKIT_SERVED_MEMBERS 依旧必须显式写成空
// 字符串，不能让整个变量消失：“空字符串”（零个成员激活）与“变量不存在”
// （不受平台管辖）语义相反，不能合并处理（servedBy 设计书 §7）。
func TestShellServedMembersIsEmptyStringWhenMemberNotRunning(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	disabled := false
	b.component(simple("mdm/customer", "1.0.7", 8080),
		config.Component{ServedBy: "infra/shell-go-core@1.0.0", Enabled: &disabled})

	env := envOf(t, b.container("infra-shell-go-core-1-0-0"))
	value, ok := env[shell.EnvVarServedMembers]
	require.True(t, ok, "外壳一直在跑，变量必须存在，即使是空字符串")
	assert.Equal(t, "", value)
}

// ---- 孤儿清理：member Service 出现在 Desired 里 ----

func TestServedByServiceIsInDesired(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	result := b.generate()
	assert.Contains(t, result.Desired, "service/mdm-customer-1-0-7",
		"P38 孤儿清理靠 Desired 判断该留还是该删——servedBy 撤销之后这条要能被识别成孤儿")
}

// ---- 迁移警告 ----

func TestServedByMigrationWarnsInK8s(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(migrating(simple("mdm/customer", "1.0.7", 8080)), servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build()
	require.NoError(t, err)
	// 注意：simple() 默认带一个 HTTP 健康检查，所以这个组件同时触发迁移
	// 警告与健康检查警告（两条各自独立，见 servedHealthCheckWarnings），
	// 这里只断言迁移那一条存在，与 compose 侧的 TestServedByMigrationWarns
	// 用的是同一个断言方式。
	found := false
	for _, w := range result.Warnings {
		if w.Code == clierr.CodeMigrationSkipped {
			found = true
		}
	}
	assert.True(t, found, "应该有一条关于迁移不会自动执行的警告：%+v", result.Warnings)
}

// ---- labels：成员自己的不参与合并，只有外壳自己的算数 ----

// 两个成员各自声明了同名不同值的标签（典型例子：prometheus.io/port，
// 值本该是各自的端口号）不该混进外壳自己的 Pod annotations——它没有
// 自己的 Pod（brickKit 反馈：servedBy 的 labels 合并漏了排除规则；
// 与 compose 侧的 TestServedByMemberLabelsDoNotAffectShellService 同理）。
func TestServedByMemberLabelsDoNotLeakIntoShellAnnotations(t *testing.T) {
	a := simple("mdm/customer", "1.0.7", 8080)
	a.Deployment.Labels = map[string]string{"prometheus.io/port": "8080"}
	c := simple("mdm/product", "1.0.9", 8082)
	c.Deployment.Labels = map[string]string{"prometheus.io/port": "8082"}

	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(a, servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(c, servedByEntry("infra/shell-go-core", "1.0.0"))

	doc := b.doc("deployments/infra-shell-go-core-1-0-0.yaml")
	annotations, _ := dig(t, doc, "spec", "template", "metadata", "annotations").(map[string]any)
	_, present := annotations["prometheus.io/port"]
	assert.False(t, present, "外壳自己没声明这个键，成员的不该被合并上来：%v", annotations)
}

// 成员声明了 labels 就该警告——它没有自己的 Pod，这些 labels 落不到任何地方。
func TestServedByLabelsWarnInK8s(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	member := simple("mdm/customer", "1.0.7", 8080)
	member.Deployment.Labels = map[string]string{"prometheus.io/port": "8080"}
	b.component(member, servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build()
	require.NoError(t, err)
	found := false
	for _, w := range result.Warnings {
		if w.Code == clierr.CodeConfigInvalid && strings.Contains(w.Format(), "labels") {
			found = true
		}
	}
	assert.True(t, found, "应该有一条关于 labels 不生效的警告：%+v", result.Warnings)
}

// ---- local: true 回归：K8s 下依旧照常拒绝 ----

func TestLocalStillRejectedAlongsideServedBy(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("erp/backend", "1.0.0", 8080), config.Component{Local: true})

	_, err := b.build()
	require.Error(t, err, "local: true 在 K8s 下必须依旧被拒绝，不受 servedBy 存在与否影响")
	assert.Contains(t, err.Error(), "local: true can only be used with deploy.target: docker")
}

// ---- servedBy + NetworkPolicy：外壳要为被收编成员的依赖方放行入站 ----

func TestServedByMemberDependentsGetIngressRule(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(dependsOn(simple("erp/caller", "1.0.0", 8080), "mdm/customer", "1.0.7"), config.Component{})

	doc := b.doc(npPath("infra-shell-go-core-1-0-0"))
	allowed := allowedFrom(t, doc)
	assert.True(t, allowed["erp-caller-1-0-0"],
		"依赖被收编成员的调用方，也必须出现在外壳自己的 NetworkPolicy 入站白名单里——实际放行的是 %v", allowed)

	foundMemberPort := false
	for _, rule := range ingressRules(t, doc) {
		ports, _ := dig(t, rule, "ports").([]any)
		for _, p := range ports {
			entry, _ := p.(map[string]any)
			if entry["port"] == 8080 {
				foundMemberPort = true
			}
		}
	}
	assert.True(t, foundMemberPort,
		"必须有一条规则放行成员自己声明的端口 8080，不能只放行外壳自己的端口")
}

// ---- servedBy + egress：调用方要放行到外壳的出站，端口是成员自己的 ----

func TestServedByMemberDependencyGetsEgressRule(t *testing.T) {
	b := withEgress(newBuilder(t))
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(dependsOn(simple("erp/caller", "1.0.0", 8080), "mdm/customer", "1.0.7"), config.Component{})

	rule := ruleWithPort(t, b.doc(npPath("erp-caller-1-0-0")), 8080)

	assert.Equal(t, []any{map[string]any{
		"podSelector": map[string]any{"matchLabels": map[string]any{"app": "infra-shell-go-core-1-0-0"}},
	}}, rule["to"], "依赖 servedBy 成员时，出站目标要指向外壳的 Pod，不是成员自己（它没有 Pod）")
}

// servedBy 成员声明了 secret: true 的配置项：Secret 仍归成员，
// 外壳的 Deployment 里带前缀的那个变量是指向成员 Secret 的 secretKeyRef。
func TestServedByMemberSecretConfigStaysWithMemberSecret(t *testing.T) {
	member := simple("mdm/customer", "1.0.7", 8080)
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"apiKey": {Type: "string", Secret: true},
	}}
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Config = map[string]any{"apiKey": "${CUSTOMER_KEY}"}

	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), config.Component{})
	b.component(member, entry)
	b.env["CUSTOMER_KEY"] = "sk-customer"

	env := envOf(t, b.container("infra-shell-go-core-1-0-0"))
	secret := b.doc("secrets/config-secrets.yaml")
	deployment := string(b.file("deployments/infra-shell-go-core-1-0-0.yaml").YAML)

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "mdm-customer-1-0-7-config-secret", "key": "API_KEY",
	}}, env["MDM_CUSTOMER_API_KEY"], "外壳里带前缀的变量指向成员自己的 Secret")
	assert.Equal(t, "mdm-customer-1-0-7-config-secret", dig(t, secret, "metadata", "name"))
	assert.Equal(t, "sk-customer", dig(t, secret, "stringData", "API_KEY"))
	assert.NotContains(t, deployment, "sk-customer")
}
