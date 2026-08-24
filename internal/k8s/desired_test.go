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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
)

// refsInFiles 把生成的每一份清单解析出来，取出各自的 `<小写类型>/<名字>`。
//
// 一个文件里可能有多份文档（secrets/resource-secrets.yaml 就是），全都要算上。
func refsInFiles(t *testing.T, result *k8s.Result) []string {
	t.Helper()

	var out []string
	for _, file := range result.Files {
		decoder := yaml.NewDecoder(strings.NewReader(string(file.YAML)))
		for {
			var doc map[string]any
			err := decoder.Decode(&doc)
			if err != nil {
				break
			}
			if doc == nil {
				continue
			}
			kind, _ := doc["kind"].(string)
			metadata, _ := doc["metadata"].(map[string]any)
			name, _ := metadata["name"].(string)
			require.NotEmpty(t, kind, "每份清单都必须有 kind：%s", file.Path)
			require.NotEmpty(t, name, "每份清单都必须有 metadata.name：%s", file.Path)
			out = append(out, strings.ToLower(kind)+"/"+name)
		}
	}
	return out
}

// fullFeatured 造一个把**所有**条件生成的资源都打开的项目：
// 对外暴露、多副本、网络策略、独立 SA、数据库密码、数据库迁移。
func fullFeatured(t *testing.T) *k8s.Result {
	t.Helper()

	three := 3
	portal := withDatabase(simple("portal/web", "1.0.0", 8080))
	portal.Migration = &manifest.Migration{Command: []string{"/app/portal", "migrate"}}

	b := newBuilder(t)
	b.cfg.Deploy.NetworkPolicy = &config.NetworkPolicy{
		Enabled: true,
		IngressController: &config.IngressControllerSource{Namespace: "ingress-nginx"},
	}
	b.cfg.Deploy.ServiceAccount = &config.ServiceAccount{Enabled: true}

	return b.
		component(portal, config.Component{
			Expose: true, Hostname: "portal.example.com", Replicas: &three,
		}).
		resource(config.Resource{
			Kind: "database", Engine: "postgresql", ID: "pg-main",
			Host: "postgres.infra", Port: 5432, Username: "portal",
			Password: "${POSTGRES_PASSWORD}",
			Bindings: []config.Binding{{ComponentID: "portal/web", Database: "portal"}},
		}).
		generate()
}

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
		"secret/pg-main-secret",
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
		component(simple("portal/web", "1.0.0", 8080), config.Component{}).
		generate()

	assert.ElementsMatch(t, []string{
		"namespace/brickkit-my-erp",
		"deployment/portal-web-1-0-0",
		"service/portal-web-1-0-0",
	}, result.Desired, "没打开的开关不该在期望集合里留下任何东西")
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
