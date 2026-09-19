package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// resources[].existingSecret 是真实的字段，能被解析、能被读出来。
func TestResourceExistingSecretIsParsed(t *testing.T) {
	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: k8s
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    existingSecret: acme-db-vault-synced
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "acme-db-vault-synced", c.Resources[0].ExistingSecret)
	assert.Empty(t, c.Resources[0].Password, "没写 password 是合法的——密码在外部已建好的 Secret 里")
}

// existingSecret 与 password 同时写是矛盾意图：到底信哪个？必须报错，不能悄悄选一个。
func TestResourceExistingSecretConflictsWithPassword(t *testing.T) {
	_, err := ParseConfig([]byte(`project: demo
deploy:
  target: k8s
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    password: ${PG_PASSWORD}
    existingSecret: acme-db-vault-synced
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "existingSecret")
	assert.Contains(t, clierr.As(err).Format(), "password")
}
