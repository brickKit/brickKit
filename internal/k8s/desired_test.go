// 本文件守着 Result.Desired 与生成物之间的那条契约。
//
// 孤儿清理的判据是"集群里有、Desired 里没有 → 删"，所以这份名单**两个方向
// 都错不起**：
//
//	漏报一项   引擎把一个正在服务的资源当成孤儿删掉——比留个孤儿严重得多
//	多报一项   真正的孤儿被当成"该留的"，永远清不掉（这正是 expose 那个缺陷）
//
// 因此这里的核心用例不是"某个字段对不对"，而是**逐份清单比对**：
// 生成了什么，Desired 里就该一字不差地有什么。
package k8s_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

// 生成了什么，Desired 里就有什么——一字不差，不多不少。
//
// 这是整条清理链路的地基。它之所以能成立，是因为 Desired 由 emitAll 从
// **文档本身**取（kind + metadata.name），而不是各个调用点再报一遍——
// 后者是第二份真相，漏一处就会让引擎删掉一个正在服务的资源。
func TestDesiredMatchesGeneratedFilesExactly(t *testing.T) {
	result := fullFeatured(t)

	assert.ElementsMatch(t, refsInFiles(t, result), result.Desired,
		"Desired 必须与生成的清单逐份对应：漏报会让引擎删掉正在服务的资源，"+
			"多报会让真正的孤儿永远清不掉")
}

// 全开的项目里，八类资源一个不少。
//
// 上一条是结构性的（两边一致就行），这条钉住**具体有哪些**：
// 万一哪天生成器整类漏掉了（比如再也不生成 Ingress 了），
// 上一条仍然会通过，而这条不会。
func TestDesiredCoversEveryConditionalKind(t *testing.T) {
	result := fullFeatured(t)

	assert.ElementsMatch(t, []string{
		"namespace/brickkit-my-erp",
		"secret/portal-web-1-0-0-config-secret",
		"serviceaccount/portal-web-1-0-0",
		"networkpolicy/portal-web-1-0-0",
		"deployment/portal-web-1-0-0",
		"service/portal-web-1-0-0",
		"poddisruptionbudget/portal-web-1-0-0",
		"ingress/portal-web-1-0-0",
		"job/portal-web-1-0-0-migration",
	}, result.Desired)
}

// 开关关掉之后，对应的条目从 Desired 里消失——引擎据此把集群里那份删掉。
//
// 这是 expose 那个缺陷的生成侧一半：只要 Desired 如实反映"本次没生成 Ingress"，
// 引擎那边的判据（集群里有、Desired 里没有 → 删）就会做对的事。
func TestDesiredDropsConditionalKindsWhenTurnedOff(t *testing.T) {
	// 同一个组件，什么开关都不开
	result := newBuilder(t).
		component(simple("portal/web", "1.0.0", 8080), projecttest.Entry{}).
		generate()

	assert.ElementsMatch(t, []string{
		"namespace/brickkit-my-erp",
		"deployment/portal-web-1-0-0",
		"service/portal-web-1-0-0",
	}, result.Desired, "没打开的开关不该在期望集合里留下任何东西")
}

// 声明了 secret: true 的配置项：它自己的 Secret 也要出现在 Desired 里，
// 否则关掉这个配置项之后，孤儿清理找不到理由去删它。
func TestDesiredCoversConfigSecret(t *testing.T) {
	m := simple("acme/hello", "0.1.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"API_KEY": {Type: "string", Secret: true},
	}}

	b := newBuilder(t)
	b.component(m, projecttest.Entry{Config: map[string]any{"API_KEY": "${THIRD_PARTY_KEY}"}})
	b.env["THIRD_PARTY_KEY"] = "sk-live-SECRET123"

	assert.Contains(t, b.generate().Desired, "secret/acme-hello-0-1-0-config-secret")
}

// Desired 是排序的：同一份配置两次生成给出同样的顺序。
func TestDesiredIsSorted(t *testing.T) {
	result := fullFeatured(t)

	sorted := append([]string(nil), result.Desired...)
	assert.Equal(t, sorted, result.Desired)
	for i := 1; i < len(result.Desired); i++ {
		assert.Less(t, result.Desired[i-1], result.Desired[i], "Desired 必须有序")
	}
}
