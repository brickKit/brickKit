// 本文件测试 servedBy（外壳合并部署）在 K8s 目标下的渲染，覆盖 servedBy
// 设计书 §6-§9。mode: debug 在 K8s 下依旧照常拒绝，但那条检查已经不在
// k8s.Generate 这一层了，见下面 servedUnsupportedFieldWarnings 之前那段说明。
package k8s_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project/projecttest"
	"github.com/brickkit/brickkit/internal/shell"
)

func servedByEntry(shellID, shellVersion string) projecttest.Entry {
	return projecttest.Entry{ServedBy: shellID + "@" + shellVersion}
}

// ---- 外壳没跑时，成员回落到独立 Deployment ----

func TestServedByMemberFallsBackToStandaloneDeploymentWhenShellIsDisabled(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000),
		projecttest.Entry{ID: "infra/shell-go-core", Version: "1.0.0", Mode: deployfile.ModeDisable})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	result := b.generate()
	assert.True(t, hasFile(result, "deployments/mdm-customer-1-0-7.yaml"),
		"外壳没跑，这个成员该有自己的 Deployment")
	assert.False(t, hasFile(result, "deployments/infra-shell-go-core-1-0-0.yaml"),
		"关掉的外壳自己不该生成 Deployment")
}

// ---- 不生成 Deployment/Job，只生成一个指向外壳 Pod 的 Service ----

func TestServedByComponentGeneratesOnlyAService(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(migrating(simple("mdm/customer", "1.0.7", 8080)),
		servedByEntry("infra/shell-go-core", "1.0.0"))

	result := b.generate()
	assert.False(t, hasFile(result, "deployments/mdm-customer-1-0-7.yaml"),
		"servedBy 组件不该有自己的 Deployment")
	assert.True(t, hasFile(result, "services/mdm-customer-1-0-7.yaml"),
		"但要有一个 Service 让它自己的服务名能被解析")
	assert.True(t, hasFile(result, "migrations/mdm-customer-1-0-7-migration.yaml"),
		"迁移 Job 仍然单独跑（提案 §8.9.4）")
	assert.True(t, hasFile(result, "deployments/infra-shell-go-core-1-0-0.yaml"),
		"外壳自己照常生成 Deployment")
}

func TestServedByServiceSelectsShellPod(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
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

// 成员的依赖地址只经由 BRICKKIT_SERVED_MEMBERS_CONFIG 交给外壳，不摊进外壳自己的环境。
func TestShellDeploymentGetsServedMembersButNotMemberEndpoints(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0"),
		servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(simple("infra/database", "1.0.0", 5432), projecttest.Entry{})

	env := envOf(t, b.container("infra-shell-go-core-1-0-0"))
	assert.NotContains(t, env, "INFRA_DATABASE_ENDPOINT")
	assert.Equal(t, "mdm-customer-1-0-7", env[shell.EnvVarServedMembers])
}

// 一个组件声明了 servedBy，但它自己当前被 mode: disable 关掉——它压根
// 不出现在 states.Running() 里，shell.Resolve 因此不会为这个外壳产出任何
// Group。但外壳本身还在跑，BRICKKIT_SERVED_MEMBERS 依旧必须显式写成空
// 字符串，不能让整个变量消失：“空字符串”（零个成员激活）与“变量不存在”
// （不受平台管辖）语义相反，不能合并处理（servedBy 设计书 §7）。
func TestShellServedMembersIsEmptyStringWhenMemberNotRunning(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(simple("mdm/customer", "1.0.7", 8080),
		projecttest.Entry{ServedBy: "infra/shell-go-core@1.0.0", Mode: deployfile.ModeDisable})

	env := envOf(t, b.container("infra-shell-go-core-1-0-0"))
	value, ok := env[shell.EnvVarServedMembers]
	require.True(t, ok, "外壳一直在跑，变量必须存在，即使是空字符串")
	assert.Equal(t, "", value)
}

// ---- 孤儿清理：member Service 出现在 Desired 里 ----

func TestServedByServiceIsInDesired(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	result := b.generate()
	assert.Contains(t, result.Desired, "service/mdm-customer-1-0-7",
		"P38 孤儿清理靠 Desired 判断该留还是该删——servedBy 撤销之后这条要能被识别成孤儿")
}

// ---- 迁移警告 ----

func TestServedByFallbackWarnsInK8s(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000),
		projecttest.Entry{ID: "infra/shell-go-core", Version: "1.0.0", Mode: deployfile.ModeDisable})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build()
	require.NoError(t, err)
	found := false
	for _, w := range result.Warnings {
		if w.Code == clierr.CodeConfigInvalid &&
			strings.Contains(w.Format(), "mdm/customer") &&
			strings.Contains(w.Format(), "infra/shell-go-core") {
			found = true
		}
	}
	assert.True(t, found, "外壳没跑、成员回落独立部署，该有一句警告点名是哪个组件、哪个外壳：%+v", result.Warnings)
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
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(a, servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(c, servedByEntry("infra/shell-go-core", "1.0.0"))

	doc := b.doc("deployments/infra-shell-go-core-1-0-0.yaml")
	annotations, _ := dig(t, doc, "spec", "template", "metadata", "annotations").(map[string]any)
	_, present := annotations["prometheus.io/port"]
	assert.False(t, present, "外壳自己没声明这个键，成员的不该被合并上来：%v", annotations)
}

// 成员的 labels 是它自己跑时用的：外壳在跑时不警告（附录 A21）。
func TestHostedMemberLabelsNotWarnedInK8s(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	member := simple("mdm/customer", "1.0.7", 8080)
	member.Deployment.Labels = map[string]string{"prometheus.io/port": "8080"}
	b.component(member, servedByEntry("infra/shell-go-core", "1.0.0"))

	result, err := b.build()
	require.NoError(t, err)
	for _, w := range result.Warnings {
		assert.NotContains(t, w.Format(), "mdm/customer")
	}
}

// 这里原来有一条回归测试（TestLocalStillRejectedAlongsideServedBy）：验证
// "servedBy 存在时，mode: debug 在 K8s 下依旧照常被拒绝"，防的是"servedBy
// 那条处理路径不小心绕过了 local 拒绝检查"这一类历史 bug。mode 字段迁移把
// local/debug + k8s 的拒绝从 k8s.Generate 挪到了 internal/config/validate.go
// 的 validateComponentMode——那是对每个组件独立、无条件跑的校验，不经过
// servedBy 相关的任何代码路径，这一类"被 servedBy 绕过"的 bug 在新架构下
// 已经没有存在的空间，不需要专门测。见 TestModeDebugRejectedAtParseNotGeneration
// （k8s_test.go）与 internal/config 的 TestValidateComponentMode。

// ---- servedBy + NetworkPolicy：外壳要为被收编成员的依赖方放行入站 ----

func TestServedByMemberDependentsGetIngressRule(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(dependsOn(simple("erp/caller", "1.0.0", 8080), "mdm/customer", "1.0.7"), projecttest.Entry{})

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
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(simple("mdm/customer", "1.0.7", 8080), servedByEntry("infra/shell-go-core", "1.0.0"))
	b.component(dependsOn(simple("erp/caller", "1.0.0", 8080), "mdm/customer", "1.0.7"), projecttest.Entry{})

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
		"API_KEY": {Type: "string", Secret: true},
	}}
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Config = map[string]any{"API_KEY": "${CUSTOMER_KEY}"}

	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(member, entry)
	b.env["CUSTOMER_KEY"] = "sk-customer"

	env := envOf(t, b.container("infra-shell-go-core-1-0-0"))
	secret := b.doc("secrets/config-secrets.yaml")
	deployment := string(b.file("deployments/infra-shell-go-core-1-0-0.yaml").YAML)

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "infra-shell-go-core-1-0-0-config-secret", "key": shell.EnvVarServedMembersConfig,
	}}, env[shell.EnvVarServedMembersConfig], "成员的密钥装在外壳的 JSON 里，JSON 整体进外壳的 Secret")
	assert.NotContains(t, env, "MDM_CUSTOMER_API_KEY", "不再有带前缀的成员变量")
	assert.Contains(t, dig(t, secret, "stringData", shell.EnvVarServedMembersConfig), `"API_KEY":"sk-customer"`)
	assert.NotContains(t, deployment, "sk-customer")
}

// 被承载的成员仍然有自己的迁移 Job：成员自己的镜像与配置（提案 §8.1 规则 2、§8.9.4），
// 排进 MigrationGroups——命令层在应用任何 Deployment（包括外壳）之前等它跑完。
func TestK8sHostedMemberMigrationJob(t *testing.T) {
	member := migrating(simple("mdm/customer", "1.0.7", 8080))
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"TOKEN": {Type: "string", Secret: true},
	}}
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Config = map[string]any{"TOKEN": "sk-member"}
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(member, entry)

	result := b.generate()
	assert.Equal(t, [][]string{{"mdm-customer-1-0-7-migration"}}, result.MigrationGroups)
	job := b.doc("migrations/mdm-customer-1-0-7-migration.yaml")
	containers, _ := dig(t, job, "spec", "template", "spec", "containers").([]any)
	require.Len(t, containers, 1)
	container, _ := containers[0].(map[string]any)
	assert.Equal(t, manifest.ImageRef(member), container["image"])
	assert.Contains(t, string(b.file("migrations/mdm-customer-1-0-7-migration.yaml").YAML),
		"mdm-customer-1-0-7-config-secret", "成员的密钥进成员自己的 Secret，Job 引用它")
	for _, f := range result.Files {
		assert.NotContains(t, f.Path, "deployments/mdm-customer", "成员没有自己的 Deployment")
	}
}

// 外壳的 JSON 进外壳的 Secret，逐字节不变（多行 PEM 也一样）；Deployment 里只有 secretKeyRef。
func TestK8sShellJSONInSecret(t *testing.T) {
	member := simple("mdm/customer", "1.0.7", 8080)
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"CERT": {Type: "string", Secret: true},
	}}
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Config = map[string]any{"CERT": "file://secrets/ca.pem"}
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(member, entry)
	b.spec.Files["secrets/ca.pem"] = pemText

	b.generate()
	secret := b.doc("secrets/config-secrets.yaml")
	raw, _ := dig(t, secret, "stringData", shell.EnvVarServedMembersConfig).(string)
	var entries []struct {
		Config map[string]string `json:"config"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, pemText, entries[0].Config["CERT"])
	assert.NotContains(t, string(b.file("deployments/infra-shell-go-core-1-0-0.yaml").YAML), "BEGIN KEY")
}

// 迁移 Job 的镜像与 Deployment 同一条规则：image 不带 tag 时补上组件版本（提案 §9.10.4）。
func TestK8sMigrationUsesImageRef(t *testing.T) {
	m := migrating(simple("people/basic", "1.0.0", 8080))
	m.Deployment.Image = "registry.example.com/people-basic"
	b := newBuilder(t)
	b.component(m, projecttest.Entry{})

	b.generate()
	job := b.doc("migrations/people-basic-1-0-0-migration.yaml")
	containers, _ := dig(t, job, "spec", "template", "spec", "containers").([]any)
	container, _ := containers[0].(map[string]any)
	assert.Equal(t, "registry.example.com/people-basic:1.0.0", container["image"])
}

// ---- 外壳 Pod 是成员的物理宿主：成员的依赖，外壳 Pod 都得能连、对方都得放行 ----

// 成员依赖外壳外面的 erp/ext：流量是从外壳 Pod 发出的，外壳的出站要放行到 ext，
// ext 的入站要放行外壳。
func TestShellPodReachesMemberDependencies(t *testing.T) {
	b := withEgress(newBuilder(t))
	b.component(simple("erp/shell", "1.0.0", 8080), projecttest.Entry{})
	b.component(dependsOn(simple("erp/a", "1.0.0", 8081), "erp/ext", "1.0.0"), servedByEntry("erp/shell", "1.0.0"))
	b.component(simple("erp/ext", "1.0.0", 8090), projecttest.Entry{})

	rule := ruleWithPort(t, b.doc(npPath("erp-shell-1-0-0")), 8090)
	assert.Equal(t, []any{map[string]any{
		"podSelector": map[string]any{"matchLabels": map[string]any{"app": "erp-ext-1-0-0"}},
	}}, rule["to"])
	assert.True(t, allowedFrom(t, b.doc(npPath("erp-ext-1-0-0")))["erp-shell-1-0-0"],
		"ext 要放行外壳 Pod——依赖它的成员就跑在那里面")
}

// 打开 serviceAccount 时，成员的迁移 Job 引用的 SA 必须真的生成出来，否则 Job 建不出 Pod。
func TestMemberMigrationJobServiceAccountIsGenerated(t *testing.T) {
	b := newBuilder(t)
	b.spec.K8s.ServiceAccount = &deployfile.ServiceAccount{Enabled: true}
	b.component(simple("erp/shell", "1.0.0", 8080), projecttest.Entry{})
	b.component(migrating(simple("erp/a", "1.0.0", 8081)), servedByEntry("erp/shell", "1.0.0"))

	result := b.generate()
	job := string(b.file("migrations/erp-a-1-0-0-migration.yaml").YAML)
	require.Contains(t, job, "serviceAccountName: erp-a-1-0-0")
	assert.True(t, hasFile(result, saPath("erp-a-1-0-0")), "Job 引用的 SA 要生成")
}

// 声明成外壳、这次一个成员都没有：两个保留变量照样写。
func TestK8sShellWithoutMembersStillGetsReservedVariables(t *testing.T) {
	shellM := simple("erp/shell", "1.0.0", 8080)
	shellM.Shell = &manifest.Shell{Members: []string{"erp/a@1.0.0"}}
	b := newBuilder(t)
	b.component(shellM, projecttest.Entry{Shell: true})

	env := envOf(t, b.container("erp-shell-1-0-0"))
	value, ok := env[shell.EnvVarServedMembers]
	require.True(t, ok)
	assert.Equal(t, "", value)
	assert.Contains(t, env, shell.EnvVarServedMembersConfig)
}
