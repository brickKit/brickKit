package cli

// 本文件是三层文件模型的端到端用例：一个真实形状的项目（testdata/three-layer/）
// 从 brickkit.yaml + deploy*.yaml + config/ 出发，走完 up --dry-run 的整条流水线，
// 分别落到 Docker 与 K8s。
//
// 它钉的是"值最后落在哪里"（附录 A6 / A7）——这是整个重构里最容易悄悄出错的地方：
//
//	Docker  密钥与 file:// 读出的内容进 0600 的 env 文件，按 compose 的规则转义，
//	        字面量里的 $ 写成 $$；compose.yaml 里只有 env_file 引用，一个字的密钥都没有
//	K8s     同样的值进生成的 Secret（逐字节不变），Deployment 里只有 secretKeyRef

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
)

// e2ePEM 是 testdata/three-layer/.secrets/tls.pem 的原文：带 $ 与 "，
// 正是会被 compose 的变量替换与引号规则咬坏的两种字符。
const e2ePEM = "-----BEGIN KEY-----\nab$c\"d\n-----END KEY-----\n"

// copyFixture 把 testdata 下的项目复制进临时目录（测试会往里写生成物）。
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", name)
	dst := t.TempDir()
	require.NoError(t, filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}))
	return dst
}

func TestThreeLayerDryRunDockerAndK8s(t *testing.T) {
	clearAmbientEnvForTest(t, "E2E_WEBHOOK")
	dir := copyFixture(t, "three-layer")
	pem, err := os.ReadFile(filepath.Join(dir, ".secrets", "tls.pem"))
	require.NoError(t, err)
	require.Equal(t, e2ePEM, string(pem), "夹具里的 PEM 被改过了")

	t.Run("docker", func(t *testing.T) {
		r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
		assert.NotContains(t, r.stdout, "deploy.local.yaml", "本地模式没开：deploy.local.yaml 存在也不读")

		generated := filepath.Join(dir, ".brickkit", "generated")
		compose := readFile(t, filepath.Join(generated, composeFileName))
		for _, secret := range []string{"sk-live-e2e", "BEGIN KEY", "ab$c"} {
			assert.NotContains(t, compose, secret, "compose.yaml 里不能有密钥或 PEM 的任何部分")
		}
		assert.Contains(t, compose, "price: $$5", "非密钥的字面量留在 compose.yaml 里，$ 写成 $$ 才不会被当成变量")
		assert.Contains(t, compose, "${E2E_WEBHOOK}", "${VAR} 模板原样留给 docker compose 启动时求值")
		assert.Contains(t, compose, "DB_HOST=db.internal", "$var: 引用在生成时就解开了")

		type envFileRef struct {
			Path string `yaml:"path"`
		}
		var doc struct {
			Services map[string]struct {
				EnvFile []envFileRef `yaml:"env_file"`
			} `yaml:"services"`
		}
		require.NoError(t, yaml.Unmarshal([]byte(compose), &doc))
		envRef := envFileRef{Path: ".brickkit/generated/env/demo-api-1-0-0.env"}
		assert.Equal(t, []envFileRef{envRef}, doc.Services["demo-api-1-0-0"].EnvFile)
		assert.Equal(t, []envFileRef{envRef}, doc.Services["demo-api-1-0-0-migration"].EnvFile,
			"迁移服务与主服务读同一份配置——包括密钥")
		assert.Empty(t, doc.Services["demo-worker-1-0-0"].EnvFile, "没有密钥的组件不需要 env 文件")

		envPath := filepath.Join(generated, "env", "demo-api-1-0-0.env")
		info, err := os.Stat(envPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		assert.Equal(t,
			"API_KEY=\"sk-live-e2e$$x\"\n"+
				"TLS_KEY=\"-----BEGIN KEY-----\\nab$$c\\\"d\\n-----END KEY-----\\n\"\n",
			readFile(t, envPath),
			"双引号包住整值；\\ \" 换行按 compose 的规则转义；字面量里的 $ 写成 $$")
	})

	t.Run("k8s", func(t *testing.T) {
		r := runWithEngine(t, newK8sEngine(), dir, "up", "--dry-run", "-f", "deploy.k8s.yaml")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

		k8sDir := filepath.Join(dir, ".brickkit", "generated", "k8s")
		var secret struct {
			Kind       string            `yaml:"kind"`
			StringData map[string]string `yaml:"stringData"`
			Data       map[string]string `yaml:"data"`
		}
		raw := readFile(t, filepath.Join(k8sDir, "secrets", "config-secrets.yaml"))
		found := false
		for _, part := range strings.Split(raw, "\n---\n") {
			secret.StringData, secret.Data = nil, nil
			require.NoError(t, yaml.Unmarshal([]byte(part), &secret))
			if v, ok := secret.StringData["TLS_KEY"]; ok {
				assert.Equal(t, e2ePEM, v, "PEM 逐字节进 Secret：K8s 不做 $ 替换，也不需要转义")
				assert.Equal(t, "sk-live-e2e$x", secret.StringData["API_KEY"])
				found = true
			}
		}
		require.True(t, found, "生成的 Secret 里没有 TLS_KEY：\n%s", raw)

		deployment := readFile(t, filepath.Join(k8sDir, "deployments", "demo-api-1-0-0.yaml"))
		for _, leaked := range []string{"BEGIN KEY", "sk-live-e2e"} {
			assert.NotContains(t, deployment, leaked, "Deployment 里只能有 secretKeyRef，不能有值")
		}
		assert.Contains(t, deployment, "secretKeyRef")
	})
}
