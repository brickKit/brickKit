package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// -f 只在本地模式真的开着时才说"本次忽略本地模式"：关着时这句话是在描述一件没发生的事。
func TestExplicitDeployFileMentionsLocalModeOnlyWhenOn(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")

	r := runWithEngine(t, newK8sEngine(), dir, "up", "--dry-run", "-f", "deploy.k8s.yaml")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliUpUsingDeployFile, "deploy.k8s.yaml"))
	assert.NotContains(t, r.stdout, i18n.T(msgid.CliUpUsingExplicitDeployFile, "deploy.k8s.yaml"),
		"本地模式关着，不该说忽略了它")

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".brickkit"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".brickkit", "local-mode"), []byte("on\n"), 0o644))
	r = runWithEngine(t, newK8sEngine(), dir, "up", "--dry-run", "-f", "deploy.k8s.yaml")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliUpUsingExplicitDeployFile, "deploy.k8s.yaml"))
}

// mode: debug 的提示不能把人引向只监听 127.0.0.1：容器经 host-gateway 过来，连不到只听回环地址的进程。
func TestDebugHintSaysListenOnAllInterfaces(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	local := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/shell\n", "  - id: erp/shell\n    mode: debug\n    localPort: 18000\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(local), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".brickkit"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".brickkit", "local-mode"), []byte("on\n"), 0o644))

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "0.0.0.0")
	assert.NotContains(t, r.stdout, "localhost:18000")
	env := readFile(t, filepath.Join(dir, ".brickkit", "generated", "local-debug.erp-shell-1-0-0.env"))
	assert.Contains(t, env, "0.0.0.0")
}
