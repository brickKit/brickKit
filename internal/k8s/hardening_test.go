// 本文件是 P26「NetworkPolicy / ServiceAccount」的业务行为测试。
//
// 这两样设计书原本没有（005 §5 只写到 Ingress 为止）。P26 当初记的理由是
// "三者都强依赖集群侧的策略约定，凭空生成一份多半是错的"——那个判断对 PDB
// 仍然成立（见 pdb_test.go 里的说明），但对 NetworkPolicy 不成立，
// 因为**BrickKit 手里有依赖图**：谁该连谁是组件声明出来的，不是猜的。
//
// 手写 NetworkPolicy 之所以又难又容易过期，正是因为人得自己维护这张图：
// 加一个依赖要记得去改策略，删一个依赖没人记得收回。生成器不会忘。
//
// 断言一律落在**最终 YAML 里有什么**：这些文件要交给 kubectl，
// 而 NetworkPolicy 写错的表现是"apply 成功、然后悄悄放行或悄悄阻断"。
package k8s_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
)

// ============================================================
// 夹具
// ============================================================

// dependsOnOptional 加一条**弱**依赖。
func dependsOnOptional(m *manifest.Manifest, id, version string) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Components = append(m.Dependencies.Components,
		manifest.ComponentDep{ID: id, Version: version, Optional: true})
	return m
}

// withNetworkPolicy 打开 NetworkPolicy 生成，并告诉它 ingress controller 在哪。
func withNetworkPolicy(b *builder) *builder {
	b.cfg.Deploy.NetworkPolicy = &config.NetworkPolicy{
		Enabled: true,
		IngressController: &config.IngressControllerSource{
			Namespace:   "ingress-nginx",
			PodSelector: map[string]string{"app.kubernetes.io/name": "ingress-nginx"},
		},
	}
	return b
}

// pinned 是 enabled: true（显式钉住，不被级联跳过）。
func pinned() *bool { yes := true; return &yes }

// npPath 是某个组件的 NetworkPolicy 文件路径。
func npPath(service string) string { return "networkpolicies/" + service + ".yaml" }

// saPath 是某个组件的 ServiceAccount 文件路径。
func saPath(service string) string { return "serviceaccounts/" + service + ".yaml" }

// ingressRules 取出 spec.ingress，并保证它是个列表。
func ingressRules(t *testing.T, doc map[string]any) []any {
	t.Helper()

	rules, ok := dig(t, doc, "spec", "ingress").([]any)
	require.True(t, ok, "spec.ingress 必须是列表，实际：%#v", dig(t, doc, "spec", "ingress"))
	return rules
}

// allowedFrom 把所有 ingress 规则里的来源 Pod 标签收集成一个集合。
//
// 只看 podSelector.matchLabels.app——组件之间互访靠的就是这个标签。
func allowedFrom(t *testing.T, doc map[string]any) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	for _, rule := range ingressRules(t, doc) {
		froms, ok := dig(t, rule, "from").([]any)
		require.True(t, ok, "每条 ingress 规则都要有 from")
		for _, from := range froms {
			source, ok := from.(map[string]any)
			require.True(t, ok)
			selector, ok := source["podSelector"].(map[string]any)
			if !ok {
				continue
			}
			labels, ok := selector["matchLabels"].(map[string]any)
			require.True(t, ok, "podSelector 必须有 matchLabels")
			if app, ok := labels["app"].(string); ok {
				out[app] = true
			}
		}
	}
	return out
}

// ============================================================
// NetworkPolicy：默认不生成
// ============================================================

// 不写 deploy.networkPolicy 时**什么都不生成**。
//
// 与 podSecurity 同一个道理（D246）：集群里可能压根没有能执行策略的 CNI，
// 也可能运维已经在命名空间级别铺了一套自己的策略。默默给每个组件套一层
// 默认拒绝，最好的情况是没人执行、白写；最坏的情况是把本来通的流量掐断。
func TestNetworkPolicyNotGeneratedByDefault(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	result := b.generate()

	assert.False(t, hasFile(result, npPath("people-basic-1-0-0")),
		"P26：不写 deploy.networkPolicy 就不该有 NetworkPolicy，实际生成了：%v", pathsOf(result))
}

// ============================================================
// NetworkPolicy：基本形状
// ============================================================

func TestNetworkPolicyBasics(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc(npPath("people-basic-1-0-0"))

	assert.Equal(t, "networking.k8s.io/v1", doc["apiVersion"], "P26")
	assert.Equal(t, "NetworkPolicy", doc["kind"], "P26")
	assert.Equal(t, "people-basic-1-0-0", dig(t, doc, "metadata", "name"))
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))

	assert.Equal(t, map[string]any{"matchLabels": map[string]any{"app": "people-basic-1-0-0"}},
		dig(t, doc, "spec", "podSelector"),
		"P26：策略作用在自己的 Pod 上，用的是 Service 认后端的同一个标签")
}

// 只生成 Ingress 方向，不生成 Egress。
//
// 这是一条**有意的**边界，不是漏做：出站方向 BrickKit 生成不出正确的规则。
//
//	DNS       得显式放行 kube-dns，而它在哪个命名空间、什么标签，各集群不一样
//	数据库    K8s 下基础资源由运维部署（005 §5.1），可能在别的命名空间、
//	          也可能是集群外一个托管实例的 IP——配置里只有一个 host 字符串，
//	          变不成 podSelector 也变不成 CIDR
//
// 生成一份"看起来收紧了、其实为了不误伤而放行了 0.0.0.0/0"的出站策略，
// 比不生成更糟：它会让人以为出站已经管住了。
func TestNetworkPolicyIsIngressOnly(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.doc(npPath("people-basic-1-0-0"))

	assert.Equal(t, []any{"Ingress"}, dig(t, doc, "spec", "policyTypes"),
		"P26：只管入站。出站方向生成不出正确规则，见本测试的注释")
	assert.NotContains(t, doc["spec"], "egress",
		"P26：不能生成 egress 段")
}

// ============================================================
// NetworkPolicy：谁能进来
// ============================================================

// 依赖方能进：erp/backend 依赖 people/basic，那 people/basic 就得放行它。
func TestNetworkPolicyAllowsDependents(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})

	allowed := allowedFrom(t, b.doc(npPath("people-basic-1-0-0")))

	assert.True(t, allowed["erp-backend-1-0-0"],
		"P26：声明了依赖就必须放行，否则装上就连不通——实际放行的是 %v", allowed)
}

// 没声明依赖的组件进不来。
//
// 这是整件事的意义所在：没有它，NetworkPolicy 只是一份写着"全部放行"的文件。
func TestNetworkPolicyDeniesNonDependents(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	// 谁也不依赖 people/basic 的第三方组件
	b.component(simple("infra/redis-event-bus", "1.0.0", 8080), config.Component{})

	allowed := allowedFrom(t, b.doc(npPath("people-basic-1-0-0")))

	assert.False(t, allowed["infra-redis-event-bus-1-0-0"],
		"P26：没声明依赖就不该被放行——实际放行的是 %v", allowed)
}

// 弱依赖也要放行。
//
// 弱依赖的语义是"有就用、没有就降级"（003 §4.3）——组件在的时候它是真的会去连的。
// 把弱依赖漏在策略外面，表现会非常迷惑：组件装了、起来了、健康检查也过，
// 只是那条"可选"的链路永远超时，看起来就像对方本来就没装。
func TestNetworkPolicyAllowsOptionalDependents(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	// enabled: true 是必需的：只被弱依赖引用的组件会被级联跳过（004 §4.5），
	// 要它真的跑起来就得钉住
	b.component(simple("infra/redis-event-bus", "1.0.0", 8080), config.Component{Enabled: pinned()})
	b.component(
		dependsOnOptional(simple("erp/backend", "1.0.0", 8080), "infra/redis-event-bus", "1.0.0"),
		config.Component{})

	allowed := allowedFrom(t, b.doc(npPath("infra-redis-event-bus-1-0-0")))

	assert.True(t, allowed["erp-backend-1-0-0"],
		"P26：弱依赖运行时照样会连，必须放行——实际放行的是 %v", allowed)
}

// 没人依赖的组件：生成一份**空** ingress 的策略，而不是不生成。
//
// 不生成等于"不管"，那个 Pod 就完全敞着；生成一份空规则才是"谁也不许进"。
// 这个区别在 NetworkPolicy 里很关键：一个 Pod 只要没被任何策略选中就是全放行。
func TestNetworkPolicyWithoutDependentsDeniesAll(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc(npPath("people-basic-1-0-0"))

	assert.Equal(t, []any{}, dig(t, doc, "spec", "ingress"),
		"P26：没人依赖它，就应该是一条规则都没有的空列表（拒绝一切入站）")
}

// 只放行组件**声明过**的端口，包括 extraPorts。
//
// people/basic 是 Python 组件，gRPC 不能与 HTTP 共用端口，走 extraPorts 的 9090；
// 依赖方用的正是那个端口。只放行主端口的话，HTTP 通、gRPC 不通——
// 而两者是同一个组件，排查时很容易往"组件挂了"的方向想。
func TestNetworkPolicyAllowsDeclaredPortsOnly(t *testing.T) {
	people := simple("people/basic", "1.0.0", 8080)
	people.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	b := withNetworkPolicy(newBuilder(t))
	b.component(people, config.Component{})
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})

	rules := ingressRules(t, b.doc(npPath("people-basic-1-0-0")))
	require.Len(t, rules, 1, "只有依赖方一条来源，就该只有一条规则")

	assert.Equal(t, []any{
		map[string]any{"protocol": "TCP", "port": 8080},
		map[string]any{"protocol": "TCP", "port": 9090},
	}, dig(t, rules[0], "ports"),
		"P26：主端口与 extraPorts 都要放行，且仅放行这些")
}

// ============================================================
// NetworkPolicy：对外暴露的组件
// ============================================================

// expose: true 的组件要额外放行 ingress controller。
//
// 最要紧的是 namespaceSelector 与 podSelector 必须写在**同一个** from 元素里：
// 同一个元素里是 AND（那个命名空间里符合标签的 Pod），拆成两个元素就是 OR
// （那个命名空间的**所有** Pod，加上**所有**命名空间里符合标签的 Pod）。
// 这是 NetworkPolicy 最经典的一个坑，写错了照样 apply 成功、照样通，
// 只是范围比你以为的大得多。
func TestNetworkPolicyAllowsIngressController(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("portal/user-frontend", "1.0.0", 8080),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	rules := ingressRules(t, b.doc(npPath("portal-user-frontend-1-0-0")))
	require.Len(t, rules, 1, "没人依赖它，只该有 ingress controller 这一条")

	froms, ok := dig(t, rules[0], "from").([]any)
	require.True(t, ok)
	require.Len(t, froms, 1, "命名空间与 Pod 两个条件必须在同一个 from 元素里（AND）")

	assert.Equal(t, map[string]any{
		"namespaceSelector": map[string]any{
			"matchLabels": map[string]any{"kubernetes.io/metadata.name": "ingress-nginx"},
		},
		"podSelector": map[string]any{
			"matchLabels": map[string]any{"app.kubernetes.io/name": "ingress-nginx"},
		},
	}, froms[0], "P26：AND 语义")

	assert.Equal(t, []any{map[string]any{"protocol": "TCP", "port": 8080}},
		dig(t, rules[0], "ports"),
		"P26：Ingress 只会打到主端口，没必要把 extraPorts 也对外放开")
}

// 不写 podSelector 时放行该命名空间的所有 Pod。
//
// 各家 ingress controller 的标签五花八门（ingress-nginx、traefik、higress……），
// 不该逼使用者非得写对；命名空间这一级已经收得够紧了。
func TestNetworkPolicyIngressControllerNamespaceOnly(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.NetworkPolicy = &config.NetworkPolicy{
		Enabled:           true,
		IngressController: &config.IngressControllerSource{Namespace: "ingress-nginx"},
	}
	b.component(simple("portal/user-frontend", "1.0.0", 8080),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	rules := ingressRules(t, b.doc(npPath("portal-user-frontend-1-0-0")))
	froms, _ := dig(t, rules[0], "from").([]any)
	require.Len(t, froms, 1)

	assert.Equal(t, map[string]any{
		"namespaceSelector": map[string]any{
			"matchLabels": map[string]any{"kubernetes.io/metadata.name": "ingress-nginx"},
		},
	}, froms[0], "P26：没写 podSelector 就只按命名空间放行")
}

// 有 expose: true 的组件却没说 ingress controller 在哪 → 阻断。
//
// 不阻断的话生成的策略会把 ingress controller 一起挡在外面，
// 结果是**部署全部成功、网站直接打不开**——而且现象是超时或 504，
// 一眼看去像是组件本身的问题，最不容易联想到是自己刚打开的那个开关。
func TestNetworkPolicyRequiresIngressControllerWhenExposed(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.NetworkPolicy = &config.NetworkPolicy{Enabled: true}
	b.component(simple("portal/user-frontend", "1.0.0", 8080),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	_, err := b.build()

	require.Error(t, err, "P26：这个组合必须阻断")
	assert.Contains(t, err.Error(), "ingressController",
		"错误要点出到底该补哪个字段：%v", err)
	assert.Contains(t, err.Error(), "portal/user-frontend",
		"错误要点名是哪个组件：%v", err)
}

// 没有任何 expose: true 的组件时，不配 ingressController 是合法的。
func TestNetworkPolicyWithoutExposedComponentsNeedsNoController(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.NetworkPolicy = &config.NetworkPolicy{Enabled: true}
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	_, err := b.build()

	require.NoError(t, err, "P26：全是内部组件时不该逼人去配 ingress controller")
}

// ============================================================
// ServiceAccount
// ============================================================

// 默认不生成，Deployment 里也不出现 serviceAccountName。
func TestServiceAccountNotGeneratedByDefault(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	result := b.generate()
	assert.False(t, hasFile(result, saPath("people-basic-1-0-0")),
		"P26：不写 deploy.serviceAccount 就不该生成，实际有：%v", pathsOf(result))

	spec := dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "template", "spec")
	assert.NotContains(t, spec, "serviceAccountName", "P26")
}

// 打开后每个组件一个 SA，且**不挂载**令牌。
//
// 不挂载才是重点。默认情况下每个 Pod 都会被塞进一个 default SA 的令牌
// （/var/run/secrets/kubernetes.io/serviceaccount/token），拿着它就能跟
// API Server 说话。业务组件没有一个需要它——003 里的组件模型里根本没有
// "访问 K8s API"这回事。关掉它是纯收益：任何一个组件被拿下，
// 攻击者也拿不到一张能问集群要东西的票。
func TestServiceAccountGenerated(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.ServiceAccount = &config.ServiceAccount{Enabled: true}
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc(saPath("people-basic-1-0-0"))

	assert.Equal(t, "v1", doc["apiVersion"], "P26")
	assert.Equal(t, "ServiceAccount", doc["kind"], "P26")
	assert.Equal(t, "people-basic-1-0-0", dig(t, doc, "metadata", "name"))
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))
	assert.Equal(t, false, doc["automountServiceAccountToken"],
		"P26：业务组件不需要跟 API Server 说话，令牌不该挂进去")
}

// Deployment 要真的用上它——SA 建了但没人引用是最容易漏的一步。
//
// automountServiceAccountToken 在 Pod 上再写一次不是冗余：
// Pod 级别的设置会覆盖 SA 级别的，写上它，就算以后有人手工把 SA 上那个
// 开关打开，这些 Pod 也还是不挂载。
func TestDeploymentUsesServiceAccount(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.ServiceAccount = &config.ServiceAccount{Enabled: true}
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	spec := dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "template", "spec")

	assert.Equal(t, "people-basic-1-0-0", dig(t, spec, "serviceAccountName"), "P26")
	assert.Equal(t, false, dig(t, spec, "automountServiceAccountToken"), "P26")
}

// 迁移 Job 用同一个 SA：它是同一个组件的同一个镜像，跑在同一个命名空间里。
func TestMigrationJobUsesServiceAccount(t *testing.T) {
	m := withDatabase(simple("people/basic", "1.0.0", 8080))
	m.Migration = &manifest.Migration{Command: []string{"/app/migrate", "up"}}

	b := newBuilder(t)
	b.cfg.Deploy.ServiceAccount = &config.ServiceAccount{Enabled: true}
	b.component(m, config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	spec := dig(t, b.doc("migrations/people-basic-1-0-0-migration.yaml"),
		"spec", "template", "spec")

	assert.Equal(t, "people-basic-1-0-0", dig(t, spec, "serviceAccountName"), "P26")
	assert.Equal(t, false, dig(t, spec, "automountServiceAccountToken"), "P26")
}

// 组件指定了已有的 SA 时：只引用，不生成。
//
// 这是 P26 当初"用哪个 SA"这个问号的正面回答。云上很常见——SA 上绑着
// IRSA / Workload Identity 的注解，由运维创建并授权，平台去覆盖它
// 就等于把那份授权抹掉，而且是安静地抹掉（apply 会成功）。
func TestExistingServiceAccountIsReferencedNotGenerated(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.ServiceAccount = &config.ServiceAccount{Enabled: true}
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{ServiceAccountName: "people-s3-reader"})

	result := b.generate()
	assert.False(t, hasFile(result, saPath("people-basic-1-0-0")),
		"P26：运维建的 SA 不能由平台重新生成一份盖掉，实际有：%v", pathsOf(result))

	spec := dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "template", "spec")
	assert.Equal(t, "people-s3-reader", dig(t, spec, "serviceAccountName"), "P26")
	assert.NotContains(t, spec, "automountServiceAccountToken",
		"P26：别人的 SA 由别人决定挂不挂令牌——它可能正是靠令牌去调 API 的")
}

// 只写 serviceAccountName、没开 deploy.serviceAccount 时照样生效。
//
// "用运维给的那个 SA"和"给每个组件生成一个 SA"是两件独立的事。
func TestServiceAccountNameWorksWithoutGlobalSwitch(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{ServiceAccountName: "people-s3-reader"})

	spec := dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "template", "spec")

	assert.Equal(t, "people-s3-reader", dig(t, spec, "serviceAccountName"), "P26")
}

// ============================================================
// PodDisruptionBudget：**只在多副本时生成**
// ============================================================

// 单副本时不生成 PDB，而且这是一条经过实测的决定。
//
// PDB 的作用是"排空节点时至少留几个副本活着"。单副本下无论怎么写都是死路：
//
//	minAvailable: 1      要留 1 个，总共就 1 个 → 一个也不许赶走
//	maxUnavailable: 0    同一件事换个说法
//	maxUnavailable: 1    允许全部赶走 → 等于没有这个 PDB
//
// 前两种的后果在 calico 集群上真跑过：`kubectl get pdb` 显示
// **ALLOWED DISRUPTIONS: 0**，`kubectl drain` 报
// "Cannot evict pod as it would violate the pod's disruption budget"
// 然后一直重试到超时——排空**永远**不可能成功。
//
// 要命的是代价落在谁身上：打开开关的是开发者，撞上的是几个月后升级集群的运维，
// 而那时现场只有一个排不空的节点，跟 brickkit.yaml 里某个开关联系不起来。
//
// **P35 已落地**：`replicas` 现在可配（005 §5.8），多副本时生成
// `maxUnavailable: 1` 的 PDB（见 pdb_test.go）。这条测试留下来守另一半——
// **副本数是 1 时坚决不生成**，那才是上面那段实测结论真正要钉住的东西。
func TestNoPodDisruptionBudgetGenerated(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.cfg.Deploy.ServiceAccount = &config.ServiceAccount{Enabled: true}
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	result := b.generate()

	for _, f := range result.Files {
		assert.NotContains(t, string(f.YAML), "PodDisruptionBudget",
			"单副本下的 PDB 会让节点永远排不空，见本测试的注释：%s", f.Path)
	}

	// 这条用例的前提：没写 replicas，因而副本数是 1
	assert.Equal(t, 1, dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "replicas"),
		"不写 replicas 时必须还是单副本，否则这条用例守的就不是它自称守的东西")
}

// ============================================================
// 生成物的完整性
// ============================================================

// 两个开关一起打开时，一个组件应该有它全套的五份清单。
//
// 漏生成不会报错，只会在集群里少一样东西——所以这里正面点名。
func TestHardenedProjectGeneratesFullSet(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.cfg.Deploy.ServiceAccount = &config.ServiceAccount{Enabled: true}
	b.component(simple("portal/user-frontend", "1.0.0", 8080),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	result := b.generate()

	for _, path := range []string{
		"deployments/portal-user-frontend-1-0-0.yaml",
		"services/portal-user-frontend-1-0-0.yaml",
		"ingress/portal-user-frontend-1-0-0.yaml",
		npPath("portal-user-frontend-1-0-0"),
		saPath("portal-user-frontend-1-0-0"),
	} {
		assert.True(t, hasFile(result, path), "P26：缺少 %s，实际有 %v", path, pathsOf(result))
	}
}

// 生成物的每个子目录都要在 kubectl 引擎的 apply / delete 列表里。
//
// 这是一条"接线"测试，而不是形状测试：生成器多产出一个子目录、
// 引擎那边忘了加，表现是**清单生成了、集群里却没有**——`brickkit up` 一切正常，
// 只有去 kubectl get 才发现少了东西。down 那边漏掉则更隐蔽：
// 删不干净，下次 up 撞上残留。
func TestAllGeneratedDirsAreKnownToEngine(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.cfg.Deploy.ServiceAccount = &config.ServiceAccount{Enabled: true}
	m := withDatabase(simple("people/basic", "1.0.0", 8080))
	m.Migration = &manifest.Migration{Command: []string{"/app/migrate", "up"}}
	b.component(m, config.Component{Expose: true, Hostname: "people.example.com"})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	known := map[string]bool{}
	for _, dir := range k8s.ManifestDirs() {
		known[dir] = true
	}

	for _, path := range pathsOf(b.generate()) {
		dir, _, nested := strings.Cut(path, "/")
		if !nested {
			continue // namespace.yaml 不在子目录里，引擎单独处理
		}
		assert.True(t, known[dir],
			"P26：生成了 %s，但 k8s.ManifestDirs() 里没有 %q——引擎不会 apply 也不会 delete 它",
			path, dir)
	}
}
