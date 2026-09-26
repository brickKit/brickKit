package k8s_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

const pemText = "-----BEGIN KEY-----\nabc\n-----END KEY-----\n"

// 每种值在 K8s 下的去处（附录 A6/A7）：明文字面量与模板进 env.value（模板生成时就展开），
// 密钥与 file:// 进生成的 Secret，existingSecret 直接引用外部 Secret。
func TestK8sPlacement(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"PLAIN":    {Type: "string"},
		"TEMPLATE": {Type: "string"},
		"TOKEN":    {Type: "string", Secret: true},
		"CERT":     {Type: "string"},
		"EXTERNAL": {Type: "string", Secret: true},
	}}
	b := newBuilder(t)
	b.component(m, projecttest.Entry{Config: map[string]any{
		"PLAIN":    "visible",
		"TEMPLATE": "pg://${PG_HOST}",
		"TOKEN":    "sk-live",
		"CERT":     "file://secrets/ca.pem",
		"EXTERNAL": map[string]any{"existingSecret": "vault-synced", "key": "token"},
	}})
	b.spec.Files["secrets/ca.pem"] = pemText
	b.env["PG_HOST"] = "pg.prod.svc"

	env := envOf(t, b.container("people-basic-1-0-0"))
	secret := b.doc("secrets/config-secrets.yaml")
	deployment := string(b.file("deployments/people-basic-1-0-0.yaml").YAML)

	assert.Equal(t, "visible", env["PLAIN"])
	assert.Equal(t, "pg://pg.prod.svc", env["TEMPLATE"])
	own := "people-basic-1-0-0-config-secret"
	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{"name": own, "key": "TOKEN"}}, env["TOKEN"])
	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{"name": own, "key": "CERT"}}, env["CERT"],
		"file:// 内容（证书、私钥）一律进 Secret")
	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{"name": "vault-synced", "key": "token"}}, env["EXTERNAL"])

	assert.Equal(t, "sk-live", dig(t, secret, "stringData", "TOKEN"))
	assert.Equal(t, pemText, dig(t, secret, "stringData", "CERT"), "逐字节保留")
	assert.NotContains(t, deployment, "sk-live")
	assert.NotContains(t, deployment, "BEGIN KEY")
}

func TestK8sNamespaceFromDeployFile(t *testing.T) {
	b := newBuilder(t)
	b.spec.K8s.Namespace = "shop-prod"
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	result := b.generate()
	assert.Equal(t, "shop-prod", result.Namespace)
	assert.Equal(t, "shop-prod", k8s.NamespaceOf(b.proj))
}
