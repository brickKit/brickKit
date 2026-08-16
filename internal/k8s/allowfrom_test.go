package k8s_test

// 本文件是 P36「NetworkPolicy 挡掉了依赖图之外的合法访问方」的业务行为测试。
//
// 缺陷本身：生成的策略只放行依赖图里的组件，而 Prometheus 不在那张图上。
// 组件就算在 Manifest 里声明了 `observability.metrics: true`，抓取照样被挡——
// **现象是指标悄悄停了**，服务本身完全正常，没有任何报错。
//
// 这与"不生成 Egress"性质不同：后者是平台推导不出正确规则（有意的边界），
// 前者是漏了。补法遵循平台已有的分工线——能推导的由平台推导（依赖图），
// 推导不出的由使用者声明**意图**（`allowFrom`），而不是让他贴一段 NetworkPolicy。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// withAllowFrom 在已开启网络策略的基础上追加图外来源。
func withAllowFrom(b *builder, sources ...config.AllowFromSource) *builder {
	withNetworkPolicy(b)
	b.cfg.Deploy.NetworkPolicy.AllowFrom = sources
	return b
}

// prometheusSource 是最典型的那一个：监控抓 /metrics。
func prometheusSource() config.AllowFromSource {
	return config.AllowFromSource{
		Name:        "prometheus",
		Namespace:   "monitoring",
		PodSelector: map[string]string{"app.kubernetes.io/name": "prometheus"},
	}
}

// namespaceSourcesOf 收集所有 ingress 规则里按命名空间放行的来源。
func namespaceSourcesOf(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, rule := range ingressRules(t, doc) {
		froms, ok := dig(t, rule, "from").([]any)
		require.True(t, ok)
		for _, from := range froms {
			source, ok := from.(map[string]any)
			require.True(t, ok)
			if _, ok := source["namespaceSelector"]; ok {
				out = append(out, source)
			}
		}
	}
	return out
}

// ============================================================
// 默认不变
// ============================================================

// 不写 allowFrom 时，生成物与之前**完全一致**。
//
// 这条是回归保护：P36 是给已有能力打补丁，不能顺手改了别人的行为。
func TestNoAllowFromKeepsPolicyUnchanged(t *testing.T) {
	b := withNetworkPolicy(newBuilder(t))
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc(npPath("people-basic-1-0-0"))

	assert.Equal(t, []any{}, dig(t, doc, "spec", "ingress"),
		"P36：没写 allowFrom 就不该多出任何规则")
}

// ============================================================
// 核心行为
// ============================================================

// 图外来源要出现在**每一个**组件的策略里。
//
// 监控要抓的是全部组件，不是某一个。漏掉任何一个的表现都是
// "那个组件的指标没了"，而它本身好好的。
func TestAllowFromAppliesToEveryComponent(t *testing.T) {
	b := withAllowFrom(newBuilder(t), prometheusSource())
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{})

	for _, service := range []string{"people-basic-1-0-0", "department-tree-1-0-0"} {
		sources := namespaceSourcesOf(t, b.doc(npPath(service)))
		require.Len(t, sources, 1, "P36：%s 少了图外来源", service)
		assert.Equal(t, map[string]any{
			"matchLabels": map[string]any{"kubernetes.io/metadata.name": "monitoring"},
		}, sources[0]["namespaceSelector"], "P36：%s", service)
	}
}

// namespaceSelector 与 podSelector 必须在**同一个 from 元素**里。
//
// 与 ingressController 那条踩的是同一个坑（D384）：同一元素内是 AND，
// 拆成两个元素就变成"该命名空间的所有 Pod + 所有命名空间里符合标签的 Pod"。
// 写错了照样 apply 成功、照样通，只是放行范围大得多，且没有任何迹象。
func TestAllowFromCombinesSelectorsWithAnd(t *testing.T) {
	b := withAllowFrom(newBuilder(t), prometheusSource())
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	sources := namespaceSourcesOf(t, b.doc(npPath("people-basic-1-0-0")))
	require.Len(t, sources, 1)

	assert.Equal(t, map[string]any{
		"namespaceSelector": map[string]any{
			"matchLabels": map[string]any{"kubernetes.io/metadata.name": "monitoring"},
		},
		"podSelector": map[string]any{
			"matchLabels": map[string]any{"app.kubernetes.io/name": "prometheus"},
		},
	}, sources[0], "P36：两个 selector 必须是 AND")
}

// 不写 podSelector 就只按命名空间放行。
func TestAllowFromWithoutPodSelector(t *testing.T) {
	b := withAllowFrom(newBuilder(t),
		config.AllowFromSource{Name: "backup", Namespace: "ops"})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	sources := namespaceSourcesOf(t, b.doc(npPath("people-basic-1-0-0")))
	require.Len(t, sources, 1)

	assert.NotContains(t, sources[0], "podSelector",
		"P36：没写 podSelector 就只按命名空间放行")
}

// 不写 ports 时放行组件**声明过**的全部端口。
//
// 使用者多半不知道每个组件的端口是几——那本来就是组件自己声明的，
// 逼他把每个端口抄一遍既啰嗦又容易过期（组件加了 extraPorts 就得回来改）。
func TestAllowFromDefaultsToAllDeclaredPorts(t *testing.T) {
	people := simple("people/basic", "1.0.0", 8080)
	people.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	b := withAllowFrom(newBuilder(t), prometheusSource())
	b.component(people, config.Component{})

	rules := ingressRules(t, b.doc(npPath("people-basic-1-0-0")))
	require.Len(t, rules, 1)

	assert.Equal(t, []any{
		map[string]any{"protocol": "TCP", "port": 8080},
		map[string]any{"protocol": "TCP", "port": 9090},
	}, dig(t, rules[0], "ports"), "P36：默认放行组件声明过的全部端口")
}

// 写了 ports 就只放这些。
func TestAllowFromRestrictsToDeclaredPorts(t *testing.T) {
	people := simple("people/basic", "1.0.0", 8080)
	people.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	source := prometheusSource()
	source.Ports = []int{8080}

	b := withAllowFrom(newBuilder(t), source)
	b.component(people, config.Component{})

	rules := ingressRules(t, b.doc(npPath("people-basic-1-0-0")))
	require.Len(t, rules, 1)

	assert.Equal(t, []any{map[string]any{"protocol": "TCP", "port": 8080}},
		dig(t, rules[0], "ports"), "P36：写了就只放这些")
}

// ============================================================
// 与已有规则共存
// ============================================================

// 依赖方、ingress controller、图外来源三种规则要同时在。
//
// 新加一类规则最容易的错法是把前面的顶掉——而顶掉的表现是
// "打开监控之后，组件之间反而不通了"。
func TestAllowFromCoexistsWithOtherRules(t *testing.T) {
	b := withAllowFrom(newBuilder(t), prometheusSource())
	b.component(simple("portal/user-frontend", "1.0.0", 8080),
		config.Component{Expose: true, Hostname: "portal.example.com"})
	b.component(
		dependsOn(simple("erp/backend", "1.0.0", 8080), "portal/user-frontend", "1.0.0"),
		config.Component{})

	doc := b.doc(npPath("portal-user-frontend-1-0-0"))

	assert.True(t, allowedFrom(t, doc)["erp-backend-1-0-0"], "P36：依赖方那条还要在")
	require.Len(t, namespaceSourcesOf(t, doc), 2,
		"P36：ingress controller 与图外来源两条都要在")
	assert.Len(t, ingressRules(t, doc), 3, "P36：三类规则各占一条")
}

// 生成的策略上要能看出这些额外口子是为谁开的。
//
// 半年后有人 `kubectl get networkpolicy -o yaml`，看到一条放行 monitoring
// 命名空间的规则，得能立刻知道它是干什么的——否则只能在"不敢删"里躺着。
func TestAllowFromIsAnnotated(t *testing.T) {
	b := withAllowFrom(newBuilder(t), prometheusSource(),
		config.AllowFromSource{Name: "backup", Namespace: "ops"})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	annotations := dig(t, b.doc(npPath("people-basic-1-0-0")), "metadata", "annotations")

	assert.Equal(t, "prometheus,backup", dig(t, annotations, "brickkit.io/allow-from"),
		"P36：注解里要写清额外放行了谁")
}

// ============================================================
// 校验
// ============================================================

// 缺 namespace → 阻断，且点名是哪一条。
//
// 空命名空间会生成 `kubernetes.io/metadata.name: ""`——一条谁也匹配不上的规则。
// 策略照样 apply 成功，而监控照样抓不到，等于什么都没做。
func TestAllowFromRequiresNamespace(t *testing.T) {
	_, err := config.ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    allowFrom:
      - name: prometheus
        podSelector:
          app.kubernetes.io/name: prometheus
`), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace", "%v", err)
	assert.Contains(t, err.Error(), "prometheus", "要点名是哪一条：%v", err)
}

// 缺 name → 阻断。
//
// name 不是装饰：它会写进生成策略的注解，是半年后那个人判断
// "这条口子还要不要留"的唯一线索。
func TestAllowFromRequiresName(t *testing.T) {
	_, err := config.ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    allowFrom:
      - namespace: monitoring
`), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "name", "%v", err)
}
