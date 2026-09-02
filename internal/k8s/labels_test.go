// 本文件覆盖 `labels` 透传在 K8s 侧的行为（002 §4.7、003 §4.11、012 §2.23）。
//
// K8s 下它落在 **annotations** 而不是 labels，而这个选择正是要守住的东西：
// 平台的 `app: <版本化服务名>` 是 Deployment 选择器与 NetworkPolicy 的匹配依据，
// 而 label 的**值**只许 [A-Za-z0-9._-]——Traefik 规则里那些反引号与括号
// 写进 labels 会被 API Server 整份拒绝。
package k8s_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 平台自己用的每一个键，ReservedLabelKey 都必须认得。
//
// 这条守的是**两份真相分叉**：保留键的清单写在 manifest 包里（config 也要用它，
// 而 k8s 依赖 manifest，倒过来 import 会成环），平台真正往清单里写的键是
// k8s 包的常量。谁改了一边忘了另一边，症状是"使用者能覆盖掉平台的键"——
// 覆盖成功之后 Deployment 选择器选空，而 apply 一路绿灯。
func TestPlatformOwnedKeysAreReserved(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc("deployments/people-basic-1-0-0.yaml")
	owned := map[string]bool{}
	for key := range dig(t, doc, "metadata", "labels").(map[string]any) {
		owned[key] = true
	}
	for key := range dig(t, doc, "metadata", "annotations").(map[string]any) {
		owned[key] = true
	}
	require.NotEmpty(t, owned, "一个平台键都没提取到——结论不可信")

	for key := range owned {
		assert.NotEmpty(t, manifest.ReservedLabelKey(key),
			"平台自己往清单里写了 %q，但 ReservedLabelKey 放它过——"+
				"使用者能把它覆盖掉，而覆盖成功之后 apply 不报错", key)
	}
	// 项目标签常量也点名一次：它在别的清单里出现，上面那份 Deployment 也有，
	// 但改名之后这条断言比"从 map 里捞"更容易读懂失败原因
	assert.NotEmpty(t, manifest.ReservedLabelKey(k8s.LabelProject))
}

// 透传键落在 Deployment 与 Pod 的 annotations 上。
func TestLabelsRenderedAsAnnotations(t *testing.T) {
	m := simple("erp/sales", "1.0.0", 8080)
	m.Deployment.Labels = map[string]string{"prometheus.io/port": "9090"}

	b := newBuilder(t)
	b.component(m, config.Component{Labels: map[string]string{
		"prometheus.io/scrape":                "true",
		"traefik.http.routers.erp-sales.rule": "PathPrefix(`/erp/sales`)",
	}})

	doc := b.doc("deployments/erp-sales-1-0-0.yaml")

	for _, where := range []struct {
		what string
		path []string
	}{
		{"Deployment", []string{"metadata", "annotations"}},
		// Pod 必须也带：prometheus.io/* 这一类抓的是 Pod，
		// 只写在 Deployment 上等于没写
		{"Pod 模板", []string{"spec", "template", "metadata", "annotations"}},
	} {
		annotations := dig(t, doc, where.path...).(map[string]any)
		assert.Equal(t, "true", annotations["prometheus.io/scrape"], where.what)
		assert.Equal(t, "9090", annotations["prometheus.io/port"],
			"%s：逐键合并，作者写的 port 不该被丢掉", where.what)
		assert.Equal(t, "PathPrefix(`/erp/sales`)",
			annotations["traefik.http.routers.erp-sales.rule"],
			"%s：反引号与斜杠原样透传——写进 labels 会被 API Server 拒掉", where.what)
		assert.Equal(t, "erp/sales", annotations["brickkit.io/component-id"],
			"%s：平台自己的注解不能被透传挤掉", where.what)
	}
}

// 透传键**不进** labels：那里的每一个键平台都在用，而且值的字符集受限。
func TestLabelsNotRenderedAsK8sLabels(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("erp/sales", "1.0.0", 8080), config.Component{
		Labels: map[string]string{"prometheus.io/scrape": "true"},
	})

	doc := b.doc("deployments/erp-sales-1-0-0.yaml")
	for _, path := range [][]string{
		{"metadata", "labels"},
		{"spec", "template", "metadata", "labels"},
	} {
		labels := dig(t, doc, path...).(map[string]any)
		assert.NotContains(t, labels, "prometheus.io/scrape")
		assert.Equal(t, "erp-sales-1-0-0", labels["app"],
			"平台的选择器标签必须原样在那里")
	}
}

// Service / Ingress / 迁移 Job 都不带透传键。
//
// Ingress 已经有自己的注解口（deploy.ingressAnnotations，003 §3），两个口子
// 写同一处会互相覆盖而且没人说得清谁赢；迁移 Job 跑完就退出，被 Traefik
// 当成路由目标、被 Prometheus 当成抓取目标都是 bug。
func TestLabelsNotOnServiceIngressOrJob(t *testing.T) {
	m := withDatabase(simple("erp/sales", "1.0.0", 8080))
	m.Migration = &manifest.Migration{Command: []string{"./migrate"}}

	b := newBuilder(t)
	b.resource(config.Resource{
		Kind: "database", Engine: "postgresql", ID: "pg",
		Host: "pg.internal", Port: 5432, Username: "dev", Password: "x",
		Bindings: []config.Binding{{ComponentID: "erp/sales", Database: "sales"}},
	})
	b.component(m, config.Component{
		Expose: true, Hostname: "sales.example.com",
		Labels: map[string]string{"prometheus.io/scrape": "true"},
	})

	for _, path := range []string{
		"services/erp-sales-1-0-0.yaml",
		"ingress/erp-sales-1-0-0.yaml",
		"migrations/erp-sales-1-0-0-migration.yaml",
	} {
		annotations, ok := dig(t, b.doc(path), "metadata", "annotations").(map[string]any)
		require.True(t, ok, "%s 的 annotations 段读不出来", path)
		assert.NotContains(t, annotations, "prometheus.io/scrape", path)
	}
}

// 没写 labels 时，annotations 段只有平台自己那一条——不该多出空键。
func TestAnnotationsUnchangedWhenNoLabels(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("erp/sales", "1.0.0", 8080), config.Component{})

	doc := b.doc("deployments/erp-sales-1-0-0.yaml")
	assert.Equal(t, map[string]any{"brickkit.io/component-id": "erp/sales"},
		dig(t, doc, "metadata", "annotations"))
}
