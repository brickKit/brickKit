// 本文件覆盖 `labels` 透传口的校验（002 §4.7、003 §4.11）。
//
// 这个字段的姿态是"平台不解释键值，只透传"，所以测试的重点不是"校验得多严"，
// 而是**该放过的都放过、该拦的只拦那两类**：平台自己拥有的键，与少了引号的值。
package manifest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/manifest"
)

// 平台不解释键值：奇形怪状的键与值都该原样放过。
func TestLabelsPassThroughWithoutInterpretation(t *testing.T) {
	m, err := manifest.Parse([]byte(labelsManifest(`
    traefik.enable: "true"
    traefik.http.routers.erp-sales.rule: "PathPrefix(`+"`"+`/erp/sales`+"`"+`)"
    prometheus.io/scrape: "true"
    io.something/wholly-unknown: "随便写什么"`)), "t.yaml")
	require.NoError(t, err, "平台不该对透传键值有任何意见")

	assert.Equal(t, "true", m.Deployment.Labels["traefik.enable"])
	assert.Equal(t, "PathPrefix(`/erp/sales`)",
		m.Deployment.Labels["traefik.http.routers.erp-sales.rule"])
	assert.Equal(t, "随便写什么", m.Deployment.Labels["io.something/wholly-unknown"])
}

// 平台自己拥有的键必须**当场报错**，不能静默丢弃。
//
// 静默丢弃的症状是"labels 写了但没生效"，那是最难查的一类——
// 生成物看上去完全正常，人只能去翻 Traefik 的日志，而那里什么都没有。
func TestReservedLabelKeysRejected(t *testing.T) {
	cases := []struct {
		key    string
		expect string
	}{
		{"app", "NetworkPolicy"},
		{"brickkit.io/component", "平台自己的命名空间"},
		{"brickkit.io/project", "平台自己的命名空间"},
		{"brickkit.io/anything-at-all", "平台自己的命名空间"},
		{"com.docker.compose.project", "docker compose"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			_, err := manifest.Parse([]byte(labelsManifest(
				"    "+tc.key+`: "x"`)), "t.yaml")
			require.Error(t, err, "%s 归平台所有，必须拦下", tc.key)
			assert.Contains(t, err.Error(), tc.key)
			assert.Contains(t, err.Error(), tc.expect,
				"报错要说清**为什么这一个**不许写，而不是只说不许")
		})
	}
}

// 平台自己没用到的 brickkit 前缀写法不该被误伤。
func TestNonReservedLookalikeKeysAllowed(t *testing.T) {
	for _, key := range []string{"brickkit.domain", "brickkit-io/x", "mybrickkit.io/x", "app.kubernetes.io/name"} {
		_, err := manifest.Parse([]byte(labelsManifest("    "+key+`: "x"`)), "t.yaml")
		assert.NoError(t, err, "%s 不是平台的键，不该拦", key)
	}
}

// 少了引号的值：这是照着 Traefik 文档敲时最容易犯的错。
//
// 报错必须说"加上引号"，而不是把 yaml 库那句
// `cannot unmarshal !!bool into string` 抛给用户——后者说的是 Go 的类型。
func TestUnquotedLabelValueRejectedWithHint(t *testing.T) {
	for _, raw := range []string{"true", "8080", "1.5"} {
		_, err := manifest.Parse([]byte(labelsManifest("    traefik.enable: "+raw)), "t.yaml")
		require.Error(t, err, "%s 少了引号，必须拦", raw)
		assert.Contains(t, err.Error(), `加上引号写成 "`+raw+`"`)
		assert.NotContains(t, err.Error(), "unmarshal", "不能把 yaml 库的 Go 类型错误抛给用户")
	}
}

// 值写空也拦：`traefik.enable:` 后面什么都没有，落到底层就是个空标签。
func TestNullLabelValueRejected(t *testing.T) {
	_, err := manifest.Parse([]byte(labelsManifest("    traefik.enable:")), "t.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "值不能为空")
}

// labels 写成数组这类形状错误，要给出精确字段名而不是 yaml 库的原话。
func TestLabelsMustBeMapping(t *testing.T) {
	_, err := manifest.Parse([]byte(labelsManifest(`    - traefik.enable`)), "t.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deployment.labels")
	assert.Contains(t, err.Error(), "必须是映射")
}

// MergeLabels 逐键合并，全空返回 nil（渲染器据此决定"这一段要不要生成"）。
func TestMergeLabels(t *testing.T) {
	assert.Nil(t, manifest.MergeLabels(nil, nil), "全空必须是 nil，不是空 map")
	assert.Nil(t, manifest.MergeLabels(map[string]string{}, nil))

	got := manifest.MergeLabels(
		map[string]string{"a": "1", "b": "2"},
		map[string]string{"b": "9", "c": "3"},
	)
	assert.Equal(t, map[string]string{"a": "1", "b": "9", "c": "3"}, got,
		"后来的逐键覆盖先前的，没被点到的键留着")
}

// labelsManifest 造一份只有 deployment.labels 有变化的最小 Manifest。
func labelsManifest(labels string) string {
	return `apiVersion: brickkit/v1
kind: Component
metadata:
  id: erp/sales
  name: 销售
  version: 1.0.0
  description: d
deployment:
  type: container
  image: registry.example.com/erp-sales:1.0.0
  port: 8080
  labels:
` + labels + `
healthCheck:
  type: http
  path: /healthz
`
}
