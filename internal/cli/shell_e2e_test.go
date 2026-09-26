package cli

// 本文件是外壳机制的端到端用例：一个真实形状的外壳项目（testdata/three-layer-shell/）
// 走完 up --dry-run，分别落到 Docker 与 K8s（提案 §8、附录 A3 / A7）。
//
//	erp/shell   外壳，shell.members 声明能承载 erp/api 与 erp/worker
//	erp/api     成员：带迁移、带一个 file:// 的 PEM 密钥（含 $ 与 "）
//	erp/worker  成员：依赖 erp/api
//	erp/portal  外壳外面的普通组件，依赖 erp/api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/envref"
)

// servedEntry 是 BRICKKIT_SERVED_MEMBERS_CONFIG 里的一个成员。
type servedEntry struct {
	ComponentID string            `json:"componentId"`
	Version     string            `json:"version"`
	HTTPPort    int               `json:"httpPort"`
	Config      map[string]string `json:"config"`
}

// shellJSONFromEnvFile 从外壳的 env 文件里取出 JSON：先按 compose 的双引号规则反转义，
// 再把 $$ 还原成 $——与 docker compose 读 env 文件时做的完全一样。
func shellJSONFromEnvFile(t *testing.T, content string) []servedEntry {
	t.Helper()
	const prefix = "BRICKKIT_SERVED_MEMBERS_CONFIG=\""
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		quoted := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\"")
		parsed := envref.ParseDotEnv("X=\"" + quoted + "\"\n")["X"]
		var entries []servedEntry
		require.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(parsed, "$$", "$")), &entries))
		return entries
	}
	require.Failf(t, "env 文件里没有 BRICKKIT_SERVED_MEMBERS_CONFIG", "%s", content)
	return nil
}

func TestShellDryRunDockerAndK8s(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	pem := readFile(t, filepath.Join(dir, ".secrets", "api.pem"))
	require.Contains(t, pem, "$", "夹具的 PEM 要带 $")
	require.Contains(t, pem, "\"", "夹具的 PEM 要带 \"")

	t.Run("docker", func(t *testing.T) {
		r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

		generated := filepath.Join(dir, ".brickkit", "generated")
		compose := readFile(t, filepath.Join(generated, composeFileName))
		assert.NotContains(t, compose, "BEGIN KEY", "PEM 不进 compose.yaml")
		assert.NotContains(t, compose, "BRICKKIT_SERVED_MEMBERS_CONFIG=", "外壳 JSON 不明文进 compose.yaml")

		var doc struct {
			Services map[string]struct {
				Image       string         `yaml:"image"`
				Environment []string       `yaml:"environment"`
				DependsOn   map[string]any `yaml:"depends_on"`
			} `yaml:"services"`
		}
		require.NoError(t, yaml.Unmarshal([]byte(compose), &doc))
		assert.NotContains(t, doc.Services, "erp-api-1-0-0", "成员没有自己的主容器")
		assert.NotContains(t, doc.Services, "erp-worker-1-0-0")
		migration, ok := doc.Services["erp-api-1-0-0-migration"]
		require.True(t, ok, "成员的迁移单独跑")
		assert.Equal(t, "registry.example.com/erp-api:1.0.0", migration.Image, "用成员自己的镜像")
		assert.Contains(t, doc.Services["erp-shell-1-0-0"].DependsOn, "erp-api-1-0-0-migration")
		assert.Contains(t, doc.Services["erp-portal-1-0-0"].Environment,
			"ERP_API_ENDPOINT=http://erp-shell-1-0-0:8081", "外壳外面的依赖方拿到外壳的地址")

		entries := shellJSONFromEnvFile(t, readFile(t, filepath.Join(generated, "env", "erp-shell-1-0-0.env")))
		require.Len(t, entries, 2)
		assert.Equal(t, "erp/api", entries[0].ComponentID)
		assert.Equal(t, pem, entries[0].Config["TLS_KEY"], "PEM 经过 env 文件与 JSON 两层编码后逐字节不变")
		assert.Equal(t, "erp/worker", entries[1].ComponentID)
		assert.Equal(t, "http://erp-shell-1-0-0:8081", entries[1].Config["ERP_API_ENDPOINT"],
			"外壳里的成员互相依赖时同样指向外壳")
	})

	t.Run("k8s", func(t *testing.T) {
		r := runWithEngine(t, newK8sEngine(), dir, "up", "--dry-run", "-f", "deploy.k8s.yaml")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

		k8sDir := filepath.Join(dir, ".brickkit", "generated", "k8s")
		assert.FileExists(t, filepath.Join(k8sDir, "migrations", "erp-api-1-0-0-migration.yaml"))
		_, err := os.Stat(filepath.Join(k8sDir, "deployments", "erp-api-1-0-0.yaml"))
		assert.True(t, os.IsNotExist(err), "成员没有自己的 Deployment")

		var secretJSON string
		for _, part := range strings.Split(readFile(t, filepath.Join(k8sDir, "secrets", "config-secrets.yaml")), "\n---\n") {
			var secret struct {
				StringData map[string]string `yaml:"stringData"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(part), &secret))
			if v, ok := secret.StringData["BRICKKIT_SERVED_MEMBERS_CONFIG"]; ok {
				secretJSON = v
			}
		}
		require.NotEmpty(t, secretJSON, "外壳 JSON 进 Secret")
		var entries []servedEntry
		require.NoError(t, json.Unmarshal([]byte(secretJSON), &entries))
		require.Len(t, entries, 2)
		assert.Equal(t, pem, entries[0].Config["TLS_KEY"])

		deployment := readFile(t, filepath.Join(k8sDir, "deployments", "erp-shell-1-0-0.yaml"))
		assert.NotContains(t, deployment, "BEGIN KEY")
		assert.Contains(t, deployment, "secretKeyRef")
	})
}

// 外壳以 mode: debug 跑在宿主机上（本地开发一开始就是这样）：成员跟着在宿主机上，
// 外壳的本地 env 文件里有 JSON（PEM 逐字节不变），成员的迁移在容器起来之后单独跑完，
// 外壳外面的容器经 extra_hosts 连到宿主机上的外壳。
func TestBareShellEndToEnd(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	pem := readFile(t, filepath.Join(dir, ".secrets", "api.pem"))
	local := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/shell\n", "  - id: erp/shell\n    mode: debug\n    localPort: 18000\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(local), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".brickkit"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".brickkit", "local-mode"), []byte("on\n"), 0o644))

	eng := newFakeEngine()
	r := runWithEngine(t, eng, dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	require.Len(t, eng.ups, 1)
	assert.Equal(t, []string{"erp-api-1-0-0-migration"}, eng.ups[0].RunAfter, "成员迁移在容器起来之后单独跑")
	assert.NotContains(t, eng.ups[0].Services, "erp-shell-1-0-0", "裸进程外壳没有容器")

	generated := filepath.Join(dir, ".brickkit", "generated")
	envFile := readFile(t, filepath.Join(generated, "local-debug.erp-shell-1-0-0.env"))
	env := map[string]string{}
	for k, v := range envref.ParseDotEnv(envFile) {
		env[k] = v
	}
	var entries []servedEntry
	require.NoError(t, json.Unmarshal([]byte(env["BRICKKIT_SERVED_MEMBERS_CONFIG"]), &entries))
	require.Len(t, entries, 2)
	assert.Equal(t, pem, entries[0].Config["TLS_KEY"])
	assert.Equal(t, "http://localhost:8081", entries[1].Config["ERP_API_ENDPOINT"],
		"外壳里的成员互相调用：都在宿主机上")

	compose := readFile(t, filepath.Join(generated, composeFileName))
	var doc struct {
		Services map[string]struct {
			ExtraHosts  []string `yaml:"extra_hosts"`
			Environment []string `yaml:"environment"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(compose), &doc))
	portal := doc.Services["erp-portal-1-0-0"]
	assert.Contains(t, portal.Environment, "ERP_API_ENDPOINT=http://erp-shell-1-0-0:8081")
	assert.Contains(t, portal.ExtraHosts, "erp-shell-1-0-0:host-gateway")
	assert.NotContains(t, compose, "BEGIN KEY")
}
