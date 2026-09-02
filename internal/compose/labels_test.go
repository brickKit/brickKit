// 本文件覆盖 `labels` 透传在 Docker 侧的行为（002 §4.7、003 §4.11、012 §2.23）。
//
// 断言全部落在**最终 YAML 里有什么**：这个字段存在的全部意义就是让容器上
// 真的出现那几行标签，让 Traefik / Prometheus 的 Docker Provider 读得到。
package compose_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// labelsOf 取一个 service 的 labels 段。
func labelsOf(t *testing.T, svc map[string]any) map[string]string {
	t.Helper()

	raw, ok := svc["labels"]
	if !ok {
		return nil
	}
	mapping, ok := raw.(map[string]any)
	require.True(t, ok, "labels 必须渲染成映射，实际是 %T：%v", raw, raw)

	out := map[string]string{}
	for key, value := range mapping {
		str, ok := value.(string)
		require.True(t, ok, "labels[%s] 必须是字符串，实际是 %T", key, value)
		out[key] = str
	}
	return out
}

// 项目级 labels 出现在组件 service 上。
func TestProjectLabelsRenderedOnService(t *testing.T) {
	doc := newBuilder(t).component(simple("erp/sales", "1.0.0", 8080), config.Component{
		Labels: map[string]string{
			"traefik.enable":                          "true",
			"traefik.http.routers.erp-sales.rule":     "PathPrefix(`/erp/sales`)",
			"traefik.http.services.erp-sales.lb.port": "8080",
		},
	}).parsed()

	labels := labelsOf(t, serviceOf(t, doc, "erp-sales-1-0-0"))
	assert.Equal(t, "true", labels["traefik.enable"])
	assert.Equal(t, "PathPrefix(`/erp/sales`)", labels["traefik.http.routers.erp-sales.rule"],
		"反引号与斜杠必须原样透传——Traefik 的规则语法全靠它们")
	assert.Equal(t, "8080", labels["traefik.http.services.erp-sales.lb.port"])
}

// 组件作者的推荐值也会落地，brickkit.yaml 逐键覆盖它。
func TestComponentLabelsMergeWithProjectLabels(t *testing.T) {
	m := simple("erp/sales", "1.0.0", 8080)
	m.Deployment.Labels = map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   "9090",
	}

	doc := newBuilder(t).component(m, config.Component{
		Labels: map[string]string{
			"prometheus.io/scrape": "false", // 覆盖作者的推荐值
			"traefik.enable":       "true",  // 新增
		},
	}).parsed()

	labels := labelsOf(t, serviceOf(t, doc, "erp-sales-1-0-0"))
	assert.Equal(t, "false", labels["prometheus.io/scrape"], "brickkit.yaml 必须赢")
	assert.Equal(t, "9090", labels["prometheus.io/port"],
		"逐键合并：只覆盖 scrape 不该把作者写的 port 一起丢掉")
	assert.Equal(t, "true", labels["traefik.enable"])
}

// 没写 labels 时，生成物里连这一段都不该出现。
func TestNoLabelsSectionWhenUnset(t *testing.T) {
	doc := newBuilder(t).component(simple("erp/sales", "1.0.0", 8080), config.Component{}).parsed()

	_, present := serviceOf(t, doc, "erp-sales-1-0-0")["labels"]
	assert.False(t, present, "一个 label 都没写时不该多出一个空的 labels 段")
}

// 迁移容器**不带** labels：它跑完就退出，被当成路由目标或抓取目标都是 bug。
func TestMigrationServiceHasNoLabels(t *testing.T) {
	m := withDatabase(simple("erp/sales", "1.0.0", 8080))
	m.Migration = &manifest.Migration{Command: []string{"./migrate"}}

	doc := newBuilder(t).
		resource(config.Resource{
			Kind: "database", Engine: "postgresql", ID: "pg",
			Host: "127.0.0.1", Port: 5432, Username: "dev", Password: "x",
			Bindings: []config.Binding{{ComponentID: "erp/sales", Database: "sales"}},
		}).
		component(m, config.Component{Labels: map[string]string{"traefik.enable": "true"}}).
		parsed()

	assert.NotNil(t, labelsOf(t, serviceOf(t, doc, "erp-sales-1-0-0")),
		"主容器上必须有")
	assert.Nil(t, labelsOf(t, serviceOf(t, doc, "erp-sales-1-0-0-migration")),
		"迁移容器上必须没有：Traefik 会把这个跑完就退出的容器当成路由目标")
}

// labels 的渲染顺序必须稳定，否则每次 up 的 diff 里都是噪音。
func TestLabelsRenderDeterministically(t *testing.T) {
	entry := config.Component{Labels: map[string]string{
		"zeta.key": "1", "alpha.key": "2", "middle.key": "3",
	}}

	first := string(newBuilder(t).component(simple("erp/sales", "1.0.0", 8080), entry).generate().YAML)
	for i := 0; i < 5; i++ {
		again := string(newBuilder(t).component(simple("erp/sales", "1.0.0", 8080), entry).generate().YAML)
		require.Equal(t, first, again, "同一份配置连跑两次必须逐字节相同")
	}
	assert.Less(t, strings.Index(first, "alpha.key"), strings.Index(first, "middle.key"))
	assert.Less(t, strings.Index(first, "middle.key"), strings.Index(first, "zeta.key"))
}

// local: true 的组件上写了 labels → 警告（写了、没生效、而且没有任何征兆）。
func TestLocalComponentLabelsWarn(t *testing.T) {
	result := newBuilder(t).
		component(simple("erp/sales", "1.0.0", 8080), config.Component{
			Local: true, LocalPort: 8080,
			Labels: map[string]string{"traefik.enable": "true"},
		}).
		generate()

	var found string
	for _, w := range result.Warnings {
		if strings.Contains(w.Format(), "labels 本次不生效") {
			found = w.Format()
		}
	}
	require.NotEmpty(t, found, "local 组件上的 labels 挂不上，必须说出来：%v", result.Warnings)
	assert.Contains(t, found, "erp/sales")
	assert.Contains(t, found, "traefik.enable", "得点名那几个键，否则人不知道说的是哪一行")
}
