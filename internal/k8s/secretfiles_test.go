package k8s_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

// allDocs 把一份多文档 YAML 拆成各个文档（secrets/config-secrets.yaml 里每个 Secret 一份）。
func allDocs(t *testing.T, content []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		if doc != nil {
			out = append(out, doc)
		}
	}
}

func fileItem() manifest.ConfigProperty {
	return manifest.ConfigProperty{Type: "string", Secret: true, Mount: manifest.MountFile}
}

// mount: file 的配置项：值进 Secret，但不经 secretKeyRef 进环境变量——Secret 的那个键挂成文件，
// 环境变量里是文件的路径（与 Docker / Podman 下同一个路径）。existingSecret 指向的外部 Secret 同样挂成文件，
// 和平台生成的那份落在同一个目录里（projected 卷）。迁移 Job 挂的与主容器一样。
func TestMountFileBecomesAProjectedSecretVolume(t *testing.T) {
	m := migrating(simple("erp/sales", "1.0.0", 8080))
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"JWT_PRIVATE_KEY_FILE": fileItem(),
		"TLS_KEY_FILE":         fileItem(),
		"DB_PASSWORD":          {Type: "string", Secret: true},
	}}
	b := newBuilder(t)
	b.component(m, projecttest.Entry{Config: map[string]any{
		"JWT_PRIVATE_KEY_FILE": "file://secrets/key.pem",
		"TLS_KEY_FILE":         map[string]any{"existingSecret": "app-tls", "key": "tls.key"},
		"DB_PASSWORD":          "pw",
	}})
	b.spec.Files["secrets/key.pem"] = pemText

	container := b.container("erp-sales-1-0-0")
	env := envOf(t, container)
	assert.Equal(t, "/run/brickkit/secrets/erp-sales-1-0-0/JWT_PRIVATE_KEY_FILE", env["JWT_PRIVATE_KEY_FILE"])
	assert.Equal(t, "/run/brickkit/secrets/erp-sales-1-0-0/TLS_KEY_FILE", env["TLS_KEY_FILE"])
	own := "erp-sales-1-0-0-config-secret"
	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{"name": own, "key": "DB_PASSWORD"}}, env["DB_PASSWORD"],
		"没写 mount 的密钥照旧走 secretKeyRef")

	mounts := []any{map[string]any{
		"name": "brickkit-secrets-0", "mountPath": "/run/brickkit/secrets/erp-sales-1-0-0", "readOnly": true,
	}}
	volumes := []any{map[string]any{
		"name": "brickkit-secrets-0",
		"projected": map[string]any{"sources": []any{
			map[string]any{"secret": map[string]any{"name": "app-tls",
				"items": []any{map[string]any{"key": "tls.key", "path": "TLS_KEY_FILE"}}}},
			map[string]any{"secret": map[string]any{"name": own,
				"items": []any{map[string]any{"key": "JWT_PRIVATE_KEY_FILE", "path": "JWT_PRIVATE_KEY_FILE"}}}},
		}},
	}}
	assert.Equal(t, mounts, container["volumeMounts"])
	assert.Equal(t, volumes, dig(t, b.doc("deployments/erp-sales-1-0-0.yaml"), "spec", "template", "spec", "volumes"))

	job := dig(t, b.doc("migrations/erp-sales-1-0-0-migration.yaml"), "spec", "template", "spec")
	assert.Equal(t, volumes, dig(t, job, "volumes"))
	containers, _ := dig(t, job, "containers").([]any)
	require.Len(t, containers, 1)
	assert.Equal(t, mounts, dig(t, containers[0], "volumeMounts"))

	secret := b.doc("secrets/config-secrets.yaml")
	assert.Equal(t, pemText, dig(t, secret, "stringData", "JWT_PRIVATE_KEY_FILE"), "逐字节保留")
	assert.Equal(t, "pw", dig(t, secret, "stringData", "DB_PASSWORD"))
	assert.NotContains(t, string(b.file("deployments/erp-sales-1-0-0.yaml").YAML), "BEGIN KEY")
}

// 没有以文件交付的项时，Pod 上没有卷。
func TestNoMountFileMeansNoVolumes(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("erp/sales", "1.0.0", 8080), projecttest.Entry{})

	assert.NotContains(t, b.container("erp-sales-1-0-0"), "volumeMounts")
	assert.NotContains(t, dig(t, b.doc("deployments/erp-sales-1-0-0.yaml"), "spec", "template", "spec"), "volumes")
}

// 被外壳承载的成员：文件挂进外壳的 Pod，路径与成员自己跑时一样；外壳的 JSON 里是路径。成员没有迁移、
// 也就没有自己的工作负载时，装着那个键的 Secret 照样要生成——外壳的 Pod 要挂它。
// existingSecret 的值 CLI 读不到，本来进不了 JSON；以文件交付时 JSON 里只需要路径，所以成员也能用。
func TestMountFileOfAHostedMemberIsMountedIntoTheShellPod(t *testing.T) {
	member := simple("mdm/customer", "1.0.7", 8080)
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"SIGNING_KEY_FILE": fileItem(),
		"TLS_KEY_FILE":     fileItem(),
	}}
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Config = map[string]any{
		"SIGNING_KEY_FILE": "file://secrets/key.pem",
		"TLS_KEY_FILE":     map[string]any{"existingSecret": "app-tls", "key": "tls.key"},
	}
	shellManifest := simple("infra/shell-go-core", "1.0.0", 9000)
	shellManifest.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"SHELL_KEY_FILE": fileItem(),
	}}
	b := newBuilder(t)
	b.component(shellManifest, projecttest.Entry{Config: map[string]any{"SHELL_KEY_FILE": "shell-secret"}})
	b.component(member, entry)
	b.spec.Files["secrets/key.pem"] = pemText

	container := b.container("infra-shell-go-core-1-0-0")
	assert.Equal(t, []any{
		map[string]any{"name": "brickkit-secrets-0", "mountPath": "/run/brickkit/secrets/infra-shell-go-core-1-0-0", "readOnly": true},
		map[string]any{"name": "brickkit-secrets-1", "mountPath": "/run/brickkit/secrets/mdm-customer-1-0-7", "readOnly": true},
	}, container["volumeMounts"])

	var memberSecret, shellSecret map[string]any
	for _, doc := range allDocs(t, b.file("secrets/config-secrets.yaml").YAML) {
		switch dig(t, doc, "metadata", "name") {
		case "mdm-customer-1-0-7-config-secret":
			memberSecret = doc
		case "infra-shell-go-core-1-0-0-config-secret":
			shellSecret = doc
		}
	}
	require.NotNil(t, memberSecret, "成员没有自己的工作负载，Secret 也要生成")
	assert.Equal(t, pemText, dig(t, memberSecret, "stringData", "SIGNING_KEY_FILE"))
	require.NotNil(t, shellSecret)
	json, _ := dig(t, shellSecret, "stringData", "BRICKKIT_SERVED_MEMBERS_CONFIG").(string)
	assert.Contains(t, json, `"SIGNING_KEY_FILE":"/run/brickkit/secrets/mdm-customer-1-0-7/SIGNING_KEY_FILE"`)
	assert.Contains(t, json, `"TLS_KEY_FILE":"/run/brickkit/secrets/mdm-customer-1-0-7/TLS_KEY_FILE"`)
	assert.NotContains(t, json, "BEGIN KEY")
}

// 文件里可以是二进制（密钥库、DER 证书）：环境变量装不了，文件可以。Secret 的 stringData 只能装文本，
// 所以不是合法 UTF-8 的内容进 data（base64），其余照旧是明文的 stringData。
func TestMountFileKeepsBinaryContent(t *testing.T) {
	binary := "\x30\x82\x01\x00\xff\xfe\x00keystore"
	m := simple("erp/sales", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"KEYSTORE_FILE": fileItem(),
		"TOKEN_FILE":    fileItem(),
	}}
	b := newBuilder(t)
	b.component(m, projecttest.Entry{Config: map[string]any{
		"KEYSTORE_FILE": "file://secrets/store.p12", "TOKEN_FILE": "plain-text",
	}})
	b.spec.Files["secrets/store.p12"] = binary

	secret := b.doc("secrets/config-secrets.yaml")
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(binary)), dig(t, secret, "data", "KEYSTORE_FILE"))
	assert.Equal(t, "plain-text", dig(t, secret, "stringData", "TOKEN_FILE"))
}
