package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 密钥候选值里的 ${VAR} 必须原样进入 Config，即使变量此刻就在进程环境里。
//
// 提前展开的后果：变量在进程环境里（CI 里最常见）时，明文在解析阶段就进了配置结构，
// docker compose 渲染器想保留占位符也保留不了；变量在 .env 里时却不会——
// 同一份配置、两种结果，取决于变量放在哪。
func TestSecretBearingFieldsKeepTheirReferences(t *testing.T) {
	t.Setenv("PG_PASSWORD", "pw-from-process-env")
	t.Setenv("API_TOKEN", "token-from-process-env")
	t.Setenv("PG_HOST", "db.internal")

	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
    config:
      apiToken: ${API_TOKEN}
      region: eu-west-1
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: ${PG_HOST}
    port: 5432
    password: ${PG_PASSWORD}
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.Equal(t, "${PG_PASSWORD}", c.Resources[0].Password, "密码里的引用留给渲染器求值")
	assert.Equal(t, "${API_TOKEN}", c.Components[0].Config["apiToken"], "config 里的引用留给渲染器求值")
	assert.Equal(t, "eu-west-1", c.Components[0].Config["region"], "不是引用的值照旧")
	assert.Equal(t, "db.internal", c.Resources[0].Host, "CLI 自己要用的字段照常在解析时展开")
}

// 路径里的 `*` 是数组下标通配：第二个组件、第二个资源同样适用。
func TestDeferredRefsApplyToEveryArrayItem(t *testing.T) {
	t.Setenv("SECOND_TOKEN", "must-not-appear")
	t.Setenv("SECOND_PASSWORD", "must-not-appear")

	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
  - id: people/other
    version: 1.0.0
    config:
      apiToken: ${SECOND_TOKEN}
resources:
  - kind: cache
    engine: redis
    id: first
    host: redis-a.example.com
    port: 6379
    bindings:
      - componentId: people/basic
  - kind: cache
    engine: redis
    id: second
    host: redis-b.example.com
    port: 6379
    password: ${SECOND_PASSWORD}
    bindings:
      - componentId: people/other
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.Equal(t, "${SECOND_TOKEN}", c.Components[1].Config["apiToken"])
	assert.Equal(t, "${SECOND_PASSWORD}", c.Resources[1].Password)
}

// 变量根本没配时，同样保留占位符（渲染器负责报"哪个变量没定义"）。
func TestUnsetReferenceStaysAsWritten(t *testing.T) {
	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    password: ${PG_PASSWORD_NOT_SET}
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.Equal(t, "${PG_PASSWORD_NOT_SET}", c.Resources[0].Password)
}
