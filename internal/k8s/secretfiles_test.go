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

func podAnnotations(t *testing.T, b *builder, service string) map[string]any {
	t.Helper()
	annotations, _ := dig(t, b.doc("deployments/"+service+".yaml"), "spec", "template", "metadata", "annotations").(map[string]any)
	return annotations
}

// 密钥进环境变量是在容器启动的那一刻。只改一个密钥的值，Deployment 的清单一个字都不变（它只写着 secretKeyRef），
// kubectl apply 就不会滚动更新：Secret 是新的，Pod 手里还是旧的——而 Docker 下同一次改动会重建容器。
// 所以 Pod 模板上带一个密钥摘要：值变了它就变，滚动更新照常发生。只在 Pod 模板上，不在 Deployment 自己身上。
func TestSecretDigestRollsThePodWhenASecretChanges(t *testing.T) {
	build := func(token, greeting string) *builder {
		m := simple("erp/sales", "1.0.0", 8080)
		m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
			"API_TOKEN": {Type: "string", Secret: true},
			"GREETING":  {Type: "string"},
		}}
		b := newBuilder(t)
		b.component(m, projecttest.Entry{Config: map[string]any{"API_TOKEN": token, "GREETING": greeting}})
		return b
	}
	const key = "brickkit.io/secret-digest"

	one := podAnnotations(t, build("token-one", "hello"), "erp-sales-1-0-0")[key]
	require.NotEmpty(t, one)
	assert.Len(t, one, 16)
	assert.Equal(t, one, podAnnotations(t, build("token-one", "hello"), "erp-sales-1-0-0")[key], "同样的值，同样的摘要：不平白滚动")
	assert.NotEqual(t, one, podAnnotations(t, build("token-two", "hello"), "erp-sales-1-0-0")[key], "密钥变了，模板跟着变")
	assert.Equal(t, one, podAnnotations(t, build("token-one", "hi"), "erp-sales-1-0-0")[key],
		"明文的值不算进去：它本来就写在清单里，变了自然会滚动")

	b := build("token-one", "hello")
	assert.NotContains(t, dig(t, b.doc("deployments/erp-sales-1-0-0.yaml"), "metadata", "annotations"), key)
	assert.NotContains(t, string(b.file("deployments/erp-sales-1-0-0.yaml").YAML), "token-one")
}

// 不算进摘要的两类：以文件交付的（有意不重启，组件自己重新读）和 existingSecret（平台读不到它的值）。
// 一项经环境变量交付的平台密钥都没有时，根本没有这条注解。
func TestSecretDigestLeavesOutFilesAndExistingSecrets(t *testing.T) {
	build := func(key string) *builder {
		m := simple("erp/sales", "1.0.0", 8080)
		m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
			"SIGNING_KEY_FILE": fileItem(),
			"DB_PASSWORD":      {Type: "string", Secret: true},
		}}
		b := newBuilder(t)
		b.component(m, projecttest.Entry{Config: map[string]any{
			"SIGNING_KEY_FILE": key,
			"DB_PASSWORD":      map[string]any{"existingSecret": "db", "key": "password"},
		}})
		return b
	}
	assert.NotContains(t, podAnnotations(t, build("key-one"), "erp-sales-1-0-0"), "brickkit.io/secret-digest")
	assert.Equal(t,
		string(build("key-one").file("deployments/erp-sales-1-0-0.yaml").YAML),
		string(build("key-two").file("deployments/erp-sales-1-0-0.yaml").YAML),
		"以文件交付的值变了，Deployment 逐字节不变：不滚动")
}

// 外壳的成员 JSON 在外壳的环境变量里：成员的配置变了，外壳要重启才读得到。
func TestSecretDigestCoversTheShellsMemberJSON(t *testing.T) {
	build := func(mode string) *builder {
		member := simple("mdm/customer", "1.0.7", 8080)
		member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
			"MODE": {Type: "string"},
		}}
		entry := servedByEntry("infra/shell-go-core", "1.0.0")
		entry.Config = map[string]any{"MODE": mode}
		b := newBuilder(t)
		b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
		b.component(member, entry)
		return b
	}
	const key = "brickkit.io/secret-digest"
	strict := podAnnotations(t, build("strict"), "infra-shell-go-core-1-0-0")[key]
	require.NotEmpty(t, strict)
	assert.NotEqual(t, strict, podAnnotations(t, build("relaxed"), "infra-shell-go-core-1-0-0")[key])
}
