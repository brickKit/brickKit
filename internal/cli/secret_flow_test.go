package cli

// 本文件钉住"密钥值写到哪"：
//
//	Docker  compose 文件里永远没有密钥，占位符留给 docker compose 启动时求值
//	K8s     生成时求值，资源密码进 Secret、Deployment 里只有 secretKeyRef
//
// 以前变量在**进程环境**里（CI 里最常见）时，compose 文件里会出现明文，
// 变量在 .env 里时却不会——同一份配置、两种结果。
//
// existingSecret 的端到端用例也在这份文件里。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// secretFlowComp 是带一个配置项的组件。
var secretFlowComp = comp{
	ID: "demo/hello", Version: "1.0.0",
	ConfigSchema: []string{"apiToken:"},
}

const secretFlowEntry = `    config:
      apiToken: ${HELLO_TOKEN}
`

// dockerSecretFlowProject 造一个 deploy.target: docker 的项目（写法照 k8sProjectWith）。
func dockerSecretFlowProject(t *testing.T) *projectFixture {
	t.Helper()

	f := addedProject(t, []comp{secretFlowComp}, secretFlowComp.ref())

	var b strings.Builder
	b.WriteString(configHeader)
	b.WriteString("\nsources:\n")
	for _, s := range f.Sources {
		b.WriteString(s)
	}
	b.WriteString("\ncomponents:\n  - id: demo/hello\n    version: 1.0.0\n")
	b.WriteString(secretFlowEntry)
	f.rewrite(t, b.String())
	return f
}

// 变量在进程环境里时，compose 文件里也不能出现明文。
func TestComposeNeverHoldsSecretValuesEvenWhenProcessEnvHasThem(t *testing.T) {
	t.Setenv("HELLO_TOKEN", "sk-live-TOKEN-VALUE")
	f := dockerSecretFlowProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)

	raw, err := os.ReadFile(filepath.Join(f.Layout.GeneratedDir(), composeFileName))
	require.NoError(t, err)
	text := string(raw)

	assert.NotContains(t, text, "sk-live-TOKEN-VALUE", "compose 文件会被人打开看、进 CI 产物，明文进去就是泄露")
	assert.Contains(t, text, "${HELLO_TOKEN}", "占位符留给 docker compose 启动时求值")
}

// 组件声明了 secret: true：K8s 下配置密钥进 Secret，Deployment 里没有明文。
func TestK8sConfigSecretGoesToSecretNotDeployment(t *testing.T) {
	t.Setenv("HELLO_TOKEN", "sk-live-TOKEN-VALUE")
	declared := secretFlowComp
	declared.SecretConfig = []string{"apiToken"}
	f := k8sProjectWith(t, declared, secretFlowEntry, "")

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)

	secrets, err := os.ReadFile(filepath.Join(k8sDir(f), "secrets", "config-secrets.yaml"))
	require.NoError(t, err)
	deployment, err := os.ReadFile(filepath.Join(k8sDir(f), "deployments", "demo-hello-1-0-0.yaml"))
	require.NoError(t, err)

	assert.Contains(t, string(secrets), "sk-live-TOKEN-VALUE")
	assert.NotContains(t, string(deployment), "sk-live-TOKEN-VALUE", "配置密钥不再明文进 Deployment")
}

// existingSecret 形状但配置项没有声明 secret: true：警告，且不能把值糊成
// map[...] 字符串塞给组件——inject 层已经确保它不被注入，这里确认使用者能看懂为什么。
func TestConfigExistingSecretWithoutDeclaredSecretWarns(t *testing.T) {
	declared := secretFlowComp
	declared.SecretConfig = nil // apiToken 没有声明 secret: true
	f := k8sProjectWith(t, declared, `    config:
      apiToken:
        existingSecret: acme-hello-vault-synced
        key: api-key
`, "")

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "是警告不是错误：%s%s", r.stdout, r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "API_TOKEN")
	assert.Contains(t, out, "existingSecret")
	assert.Contains(t, out, "secret: true")
}

// existingSecret 形状但目标是 docker：警告，且不出现在生成的 compose 里。
func TestConfigExistingSecretUnderDockerWarns(t *testing.T) {
	declared := secretFlowComp
	declared.SecretConfig = []string{"apiToken"}
	f := addedProject(t, []comp{declared}, declared.ref())

	var b strings.Builder
	b.WriteString(configHeader)
	b.WriteString("\nsources:\n")
	for _, s := range f.Sources {
		b.WriteString(s)
	}
	b.WriteString("\ncomponents:\n  - id: demo/hello\n    version: 1.0.0\n    config:\n" +
		"      apiToken:\n        existingSecret: acme-hello-vault-synced\n        key: api-key\n")
	f.rewrite(t, b.String())

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "existingSecret")
	assert.Contains(t, out, "only works on K8s")
	assert.Contains(t, out, "K8s")
}
