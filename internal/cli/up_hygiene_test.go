package cli

// 本文件钉住"生成物与容器只属于这一次 up"：切换部署目标、离开 mode: debug、组件全部搬到
// 宿主机之后，上一次留下的东西（含密钥的文件、还在跑的容器）不能继续躺着。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 切到 k8s：Docker 侧 0600 的 env 文件（外壳 JSON、PEM）一并删掉；切回 docker：k8s 的 Secret 清单删掉。
func TestSwitchingTargetRemovesOtherTargetsSecrets(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	generated := filepath.Join(dir, ".brickkit", "generated")

	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run").code)
	require.FileExists(t, filepath.Join(generated, "env", "erp-shell-1-0-0.env"))

	r := runWithEngine(t, newK8sEngine(), dir, "up", "--dry-run", "-f", "deploy.k8s.yaml")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoDirExists(t, filepath.Join(generated, "env"), "切到 k8s 后 Docker 侧的密钥文件不能留着")
	require.DirExists(t, filepath.Join(generated, "k8s", "secrets"))

	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run").code)
	assert.NoDirExists(t, filepath.Join(generated, "k8s"), "切回 docker 后 k8s 的 Secret 清单不能留着")
}

// 组件离开 mode: debug：它的 local-debug.*.env（可能带密钥）随之删掉。
func TestLeavingDebugRemovesItsEnvFile(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	local := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/shell\n", "  - id: erp/shell\n    mode: debug\n    localPort: 18000\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(local), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".brickkit"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".brickkit", "local-mode"), []byte("on\n"), 0o644))
	envFile := filepath.Join(dir, ".brickkit", "generated", "local-debug.erp-shell-1-0-0.env")

	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run").code)
	require.FileExists(t, envFile)

	require.NoError(t, os.Remove(filepath.Join(dir, ".brickkit", "local-mode")))
	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run").code)
	assert.NoFileExists(t, envFile)
}

// 这次一个容器都不需要（全在宿主机上）：上一次留下的容器要停掉——它们可能还占着端口。
func TestAllOnHostStopsOldContainers(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, "components:\n  - id: demo/hello\n    version: 1.0.0\n    mode: debug\n    localPort: 9000\n")

	eng := newFakeEngine()
	r := runWithEngine(t, eng, f.Dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Empty(t, eng.ups)
	require.Len(t, eng.downs, 1, "上一次的容器要停掉")
}
