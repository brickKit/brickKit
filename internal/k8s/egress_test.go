package k8s_test

// 本文件是 P37「出站策略」的业务行为测试。
//
// # 出站与入站不是对称的，这里有个真陷阱
//
// NetworkPolicy 的语义是：一个 Pod 只要在 **Egress 方向**被任何策略选中，
// 该方向**未明确允许的一律拒绝**。也就是说，加上第一条出站规则的那一刻，
// 这个组件就从"想连谁连谁"翻转成了"只能连白名单"。
//
// 漏掉一项的后果取决于组件**什么时候建连**（都在 calico 集群上实测过）：
//
//	漏了 DNS      什么都不通
//	启动时建连    组件起不来，rollout 失败
//	首次请求建连  健康检查照过（/healthz 只查本进程，002 §9.4），业务请求失败
//
// 最阴险的一点也是实测的：**改策略不会杀掉已建立的连接**。
// 正在跑的组件照常工作，问题要等到下一次重启（节点排空、升级、扩缩容）
// 才暴露——那时离改配置可能已经过去几周。
//
// 所以出站这一块的设计重点不是"能不能生成"，而是**不让人漏**：
// DNS 由平台自动放行（谁都需要，且漏了必挂）；组件依赖从依赖图推导；
// 而资源（数据库 / 缓存）平台知道**谁要用哪个**，只是不知道它在集群哪儿——
// 声明不全就在生成阶段阻断，绝不生成一份会让组件连不上库的策略。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
)

// withEgress 打开出站策略并给定放行目标。
func withEgress(b *builder, targets ...config.AllowToTarget) *builder {
	withNetworkPolicy(b)
	b.cfg.Deploy.NetworkPolicy.Egress = &config.Egress{Enabled: true, AllowTo: targets}
	return b
}

// pgTarget 是"postgres 在 infra 命名空间"这条声明。
func pgTarget() config.AllowToTarget {
	return config.AllowToTarget{
		Name:        "pg",
		Resource:    "people-db",
		Namespace:   "infra",
		PodSelector: map[string]string{"app": "postgres"},
	}
}

// egressRules 取出 spec.egress。
func egressRules(t *testing.T, doc map[string]any) []any {
	t.Helper()

	rules, ok := dig(t, doc, "spec", "egress").([]any)
	require.True(t, ok, "spec.egress 必须是列表，实际：%#v", dig(t, doc, "spec", "egress"))
	return rules
}

// ruleTo 找出目标里含指定端口的那条规则。
func ruleWithPort(t *testing.T, doc map[string]any, port int) map[string]any {
	t.Helper()

	for _, rule := range egressRules(t, doc) {
		ports, _ := dig(t, rule, "ports").([]any)
		for _, p := range ports {
			entry, _ := p.(map[string]any)
			if entry["port"] == port {
				out, _ := rule.(map[string]any)
				return out
			}
		}
	}
	require.Failf(t, "找不到规则", "没有放行端口 %d 的规则：%#v", port, egressRules(t, doc))
	return nil
}

// ============================================================
// 默认不变
// ============================================================

// 不开出站时，策略里**没有** egress 段，policyTypes 也只有 Ingress。
//
// 这条是回归保护，更是安全底线：一旦冒出 egress 段，所有组件的出站
// 就在使用者不知情的情况下变成了默认拒绝。
func TestEgressNotGeneratedByDefault(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc(npPath("people-basic-1-0-0"))

	assert.Equal(t, []any{"Ingress"}, dig(t, doc, "spec", "policyTypes"),
		"P37：不开出站时 policyTypes 只有 Ingress")
	assert.NotContains(t, doc["spec"], "egress", "P37：不该有 egress 段")
}

// ============================================================
// 自动放行的两类
// ============================================================

func TestEgressAddsEgressPolicyType(t *testing.T) {
	b := withEgress(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.Equal(t, []any{"Ingress", "Egress"},
		dig(t, b.doc(npPath("people-basic-1-0-0")), "spec", "policyTypes"), "P37")
}

// DNS 由平台**自动**放行，不需要任何声明。
//
// 这是整块设计里最要紧的一条。漏了 DNS 的后果是组件连自己的库都解析不出来，
// 什么都不通；而 kube-dns 在哪个命名空间、什么标签，各集群不一样——
// 逼使用者去查一次再抄进配置，只会制造一个必踩的坑。
//
// 放行范围是**集群内任意命名空间的 53 端口**，而不是钉死 kube-system：
// 这样换了 CNI / DNS 方案也不会坏，而安全上的让步很小——
// namespaceSelector: {} 只匹配集群内的 Pod，出不了集群。
func TestEgressAlwaysAllowsDNS(t *testing.T) {
	b := withEgress(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	rule := ruleWithPort(t, b.doc(npPath("people-basic-1-0-0")), 53)

	assert.Equal(t, []any{map[string]any{"namespaceSelector": map[string]any{}}},
		rule["to"], "P37：DNS 放行集群内任意命名空间")
	assert.Equal(t, []any{
		map[string]any{"protocol": "UDP", "port": 53},
		map[string]any{"protocol": "TCP", "port": 53},
	}, rule["ports"], "P37：UDP 与 TCP 都要放——大响应会退回 TCP")
}

// 组件依赖从**依赖图**推导，不用声明。
//
// 这与入站方向是同一张图的两面：入站问"谁能连我"，出站问"我能连谁"。
// 让使用者再声明一遍，等于把平台已经知道的事推给他，而且必然会过期。
func TestEgressAllowsDependencies(t *testing.T) {
	people := simple("people/basic", "1.0.0", 8080)
	people.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	b := withEgress(newBuilder(t))
	b.component(people, config.Component{})
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})

	rule := ruleWithPort(t, b.doc(npPath("erp-backend-1-0-0")), 8080)

	assert.Equal(t, []any{map[string]any{
		"podSelector": map[string]any{"matchLabels": map[string]any{"app": "people-basic-1-0-0"}},
	}}, rule["to"], "P37：出站目标是被依赖方")
	assert.Equal(t, []any{
		map[string]any{"protocol": "TCP", "port": 8080},
		map[string]any{"protocol": "TCP", "port": 9090},
	}, rule["ports"], "P37：被依赖方声明过的端口都要放（gRPC 常在 extraPorts）")
}

// 弱依赖也要放行——与入站方向同一个道理（D381）。
func TestEgressAllowsOptionalDependencies(t *testing.T) {
	b := withEgress(newBuilder(t))
	b.component(simple("infra/redis-event-bus", "1.0.0", 8080),
		config.Component{Enabled: pinned()})
	b.component(
		dependsOnOptional(simple("erp/backend", "1.0.0", 8080), "infra/redis-event-bus", "1.0.0"),
		config.Component{})

	doc := b.doc(npPath("erp-backend-1-0-0"))

	var found bool
	for _, rule := range egressRules(t, doc) {
		tos, _ := dig(t, rule, "to").([]any)
		for _, to := range tos {
			entry, _ := to.(map[string]any)
			selector, ok := entry["podSelector"].(map[string]any)
			if !ok {
				continue
			}
			labels, _ := selector["matchLabels"].(map[string]any)
			if labels["app"] == "infra-redis-event-bus-1-0-0" {
				found = true
			}
		}
	}
	assert.True(t, found, "P37：弱依赖运行时照样会连，必须放行")
}

// ============================================================
// 资源：平台知道谁要用，不知道它在哪
// ============================================================

// 资源的位置由使用者声明，**端口由平台从 resources[].port 补**。
//
// 端口不让人再写一遍：那个值配置里已经有了，抄第二遍只会出现两处不一致，
// 而不一致的表现是"策略看着对，组件连不上库"。
func TestEgressAllowsDeclaredResource(t *testing.T) {
	b := withEgress(newBuilder(t), pgTarget())
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	rule := ruleWithPort(t, b.doc(npPath("people-basic-1-0-0")), 5432)

	assert.Equal(t, []any{map[string]any{
		"namespaceSelector": map[string]any{
			"matchLabels": map[string]any{"kubernetes.io/metadata.name": "infra"},
		},
		"podSelector": map[string]any{"matchLabels": map[string]any{"app": "postgres"}},
	}}, rule["to"], "P37：命名空间与标签同样是 AND")
}

// **漏声明资源就阻断**——这是整块设计的核心。
//
// 不阻断的话，生成的策略会把数据库挡在外面。后果取决于组件什么时候建连：
// 启动时建连的起不来，首次请求才建连的则健康检查照过、业务请求失败。
// 两种都没有任何一处提示你是刚打开的那个开关干的。
//
// 平台有能力拦住它：谁绑了哪个资源，`resources[].bindings` 里写着。
//
// **这一句由 k8s.CheckEgressCoverage 给出，生成器自己不再拦。** "该不该阻断"是
// 命令层的决定：真 up 拦下，--dry-run 降级成警告（004 §4.4，与资源绑定检查同一条
// 规则）。生成器内部硬拦的后果是，一个正在配 egress 的人连"看看会生成什么策略"
// 都做不到——而那恰恰是他最需要看的东西。
func TestEgressCoverageReportsUndeclaredResource(t *testing.T) {
	b := withEgress(newBuilder(t)) // ← 没给任何 allowTo
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	// 生成本身不再失败——策略会照常生成出来（只是那份策略会把库挡在外面）
	_, err := b.build()
	require.NoError(t, err, "拦不拦由命令层决定，生成器只负责生成")

	problem := k8s.CheckEgressCoverage(b.cfg, []string{"people/basic"})

	require.NotNil(t, problem, "P37：这个组合必须被报出来")
	out := problem.Format()
	assert.Contains(t, out, "people-db", "要点名缺的是哪个资源：%s", out)
	assert.Contains(t, out, "people/basic", "以及谁要用它：%s", out)
	assert.Contains(t, out, "allowTo", "以及该往哪儿补：%s", out)
}

// 组件没绑任何资源时，不该逼人去声明什么。
func TestEgressWithoutResourcesNeedsNoDeclaration(t *testing.T) {
	b := withEgress(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	_, err := b.build()
	assert.NoError(t, err)
	assert.Nil(t, k8s.CheckEgressCoverage(b.cfg, []string{"people/basic"}),
		"P37：没有资源就没什么要声明的")
}

// 集群外的目标用 CIDR。
func TestEgressAllowsCIDR(t *testing.T) {
	b := withEgress(newBuilder(t), config.AllowToTarget{
		Name: "stripe", CIDR: "34.0.0.0/8", Ports: []int{443},
	})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	rule := ruleWithPort(t, b.doc(npPath("people-basic-1-0-0")), 443)

	assert.Equal(t, []any{map[string]any{"ipBlock": map[string]any{"cidr": "34.0.0.0/8"}}},
		rule["to"], "P37：集群外目标走 ipBlock")
}

// ============================================================
// 配置校验
// ============================================================

func TestEgressTargetRequiresExactlyOneLocation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"两个都写", `
        - name: pg
          namespace: infra
          cidr: 10.0.0.0/8`, "只能写一个"},
		{"两个都不写", `
        - name: pg`, "namespace"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := config.ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    egress:
      enabled: true
      allowTo:`+c.yaml+"\n"), "brickkit.yaml")

			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want, "%v", err)
		})
	}
}

// 写了 resource 就不该再写 ports：端口从 resources[].port 来，
// 两处各写一份，早晚不一致，而不一致的表现是组件连不上库。
func TestEgressResourceTargetRejectsExplicitPorts(t *testing.T) {
	_, err := config.ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    egress:
      enabled: true
      allowTo:
        - name: pg
          resource: people-db
          namespace: infra
          ports: [5432]
`), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ports", "%v", err)
}
