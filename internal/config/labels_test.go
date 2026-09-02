// 本文件覆盖 brickkit.yaml 侧的 `labels` 透传口（003 §4.11）。
//
// 与 component.yaml 侧共用同一条校验（manifest.ValidateLabels）与同一条
// 值形状检查（yamlcheck.CheckStringValues）——所以这里测的是"接线接上了"，
// 各条规则本身在 internal/manifest/labels_test.go 里测。
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComponentLabelsParsed(t *testing.T) {
	c, err := ParseConfig([]byte(baseConfig+`
components:
  - id: erp/sales
    version: 1.0.0
    labels:
      traefik.enable: "true"
      traefik.http.routers.erp-sales.rule: "PathPrefix(`+"`"+`/erp/sales`+"`"+`)"
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"traefik.enable":                      "true",
		"traefik.http.routers.erp-sales.rule": "PathPrefix(`/erp/sales`)",
	}, c.Components[0].Labels)
}

func TestComponentReservedLabelRejected(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
components:
  - id: erp/sales
    version: 1.0.0
    labels:
      app: my-shell
`), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].labels.app")
	assert.Contains(t, err.Error(), "NetworkPolicy")
}

// 少了引号：`traefik.enable: true` 是照着 Traefik 文档敲时最常犯的错。
func TestComponentUnquotedLabelValueRejected(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
components:
  - id: erp/sales
    version: 1.0.0
    labels:
      traefik.enable: true
`), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].labels.traefik.enable")
	assert.Contains(t, err.Error(), `加上引号写成 "true"`)
}

func TestComponentLabelsMustBeMapping(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
components:
  - id: erp/sales
    version: 1.0.0
    labels:
      - traefik.enable
`), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].labels")
	assert.Contains(t, err.Error(), "必须是映射")
}
