package cli

// 本文件钉住"密钥值写到哪"（Spec 2026-09-19 §3.1）：
//
//	Docker  compose 文件里永远没有密钥，占位符留给 docker compose 启动时求值
//	K8s     生成时求值，资源密码进 Secret、Deployment 里只有 secretKeyRef
//
// 以前变量在**进程环境**里（CI 里最常见）时，compose 文件里会出现明文，
// 变量在 .env 里时却不会——同一份配置、两种结果。
//
// Task 4 会在这份文件里继续加 existingSecret 的端到端用例。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// secretFlowComp 是带一个配置项、一个数据库依赖的组件。
var secretFlowComp = comp{
	ID: "demo/hello", Version: "1.0.0",
	ConfigSchema: []string{"apiToken:"},
	ResourceDeps: []string{"database:postgresql"},
}

const secretFlowEntry = `    config:
      apiToken: ${HELLO_TOKEN}
`

const secretFlowResources = `
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.internal
    port: 5432
    username: app
    password: ${HELLO_DB_PASSWORD}
    bindings:
      - componentId: demo/hello
        database: hello
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
	b.WriteString(secretFlowResources)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(b.String()), 0o644))
	return f
}

// 变量在进程环境里时，compose 文件里也不能出现明文。
func TestComposeNeverHoldsSecretValuesEvenWhenProcessEnvHasThem(t *testing.T) {
	t.Setenv("HELLO_TOKEN", "sk-live-TOKEN-VALUE")
	t.Setenv("HELLO_DB_PASSWORD", "pw-DB-VALUE")
	f := dockerSecretFlowProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)

	raw, err := os.ReadFile(filepath.Join(f.Layout.GeneratedDir(), composeFileName))
	require.NoError(t, err)
	text := string(raw)

	assert.NotContains(t, text, "sk-live-TOKEN-VALUE", "compose 文件会被人打开看、进 CI 产物，明文进去就是泄露")
	assert.NotContains(t, text, "pw-DB-VALUE")
	assert.Contains(t, text, "${HELLO_TOKEN}", "占位符留给 docker compose 启动时求值")
	assert.Contains(t, text, "${HELLO_DB_PASSWORD}")
}

// K8s 目标：变量在进程环境里，生成时求值——资源密码进 Secret，Deployment 里没有它。
func TestK8sResourcePasswordStaysInSecretNotDeployment(t *testing.T) {
	t.Setenv("HELLO_TOKEN", "sk-live-TOKEN-VALUE")
	t.Setenv("HELLO_DB_PASSWORD", "pw-DB-VALUE")
	f := k8sProjectWith(t, secretFlowComp, secretFlowEntry, secretFlowResources)

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)

	secrets, err := os.ReadFile(filepath.Join(k8sDir(f), "secrets", "resource-secrets.yaml"))
	require.NoError(t, err)
	deployment, err := os.ReadFile(filepath.Join(k8sDir(f), "deployments", "demo-hello-1-0-0.yaml"))
	require.NoError(t, err)

	assert.Contains(t, string(secrets), "pw-DB-VALUE", "K8s 没有变量替换，Secret 必须在生成时求值")
	assert.NotContains(t, string(deployment), "pw-DB-VALUE")
}
