package cli

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

func focusOnPortal(t *testing.T, dir string) {
	t.Helper()
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// sync 不看焦点：焦点之外、项目本来要跑的组件，源码照样留在活跃目录（设计 §4.6）。
func TestSyncIgnoresTheFocus(t *testing.T) {
	dir := focusFixture(t)
	focusOnPortal(t, dir)
	r := runWithEngine(t, newFakeEngine(), dir, "sync")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.DirExists(t, in(dir, "components", "erp", "worker"))
	assert.NoDirExists(t, in(dir, "components", ".archived", "erp", "worker"))
}

// local refresh 把焦点当成一处本地修改列出来。
func TestLocalRefreshListsTheFocus(t *testing.T) {
	dir := focusFixture(t)
	focusOnPortal(t, dir)
	r := runIn(t, dir, "local", "refresh")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliLocalChangeUnset, deployfile.ScopeDeploy, "focus", "erp/portal"))
}

// lint 检查焦点：个人文件改成 k8s 时报焦点不能用。
func TestLintChecksTheFocus(t *testing.T) {
	dir := focusFixture(t)
	focusOnPortal(t, dir)
	editFile(t, in(dir, "deploy.local.yaml"), "target: docker", "target: k8s")
	r := runIn(t, dir, "lint")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.ConfigFocusK8sUnsupported))
}

// graph 读 deploy.yaml，不受本地模式与焦点影响：设焦点前后输出完全一样。
func TestGraphIgnoresTheFocus(t *testing.T) {
	dir := focusFixture(t)
	before := runIn(t, dir, "graph")
	require.Equal(t, clierr.ExitOK, before.code, before.stdout+before.stderr)
	focusOnPortal(t, dir)
	after := runIn(t, dir, "graph")
	require.Equal(t, clierr.ExitOK, after.code, after.stdout+after.stderr)
	assert.Equal(t, before.stdout, after.stdout)
}

// lint 也查焦点组件有没有本地源码可跑（up 会拦的，lint 提前说）。
func TestLintReportsAFocusWithoutLocalSource(t *testing.T) {
	dir := focusFixture(t)
	focusOnPortal(t, dir)
	require.NoError(t, os.RemoveAll(in(dir, "components", "erp", "portal")))
	r := runIn(t, dir, "lint")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.CliUpNoLocalSourceFor, "erp/portal"))
}
