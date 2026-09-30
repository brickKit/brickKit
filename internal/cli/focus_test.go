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

// focusFixture：外壳夹具，每个组件（和外壳）都写上显式的启动命令，好让它们能从源码跑。
func focusFixture(t *testing.T) string {
	t.Helper()
	dir := copyFixture(t, "three-layer-shell")
	for _, rel := range []string{"components/erp/portal", "components/erp/api", "components/erp/worker", "shell/erp/shell"} {
		editFile(t, filepath.Join(dir, rel, "component.yaml"), "deployment:\n",
			"local:\n  runCommand: [\"sleep\", \"60\"]\ndeployment:\n")
	}
	return dir
}

func in(dir string, rel ...string) string { return filepath.Join(append([]string{dir}, rel...)...) }

// 在组件目录里 up：焦点设成这个组件，写进 deploy.local.yaml，本地模式随之打开；
// 只有它和它需要的组件启动。
func TestUpFromAComponentDirectoryFocusesIt(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), in(dir, "components", "erp", "portal"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Contains(t, readFile(t, in(dir, "deploy.local.yaml")), "\nfocus: erp/portal\n")
	assert.FileExists(t, in(dir, ".brickkit", "local-mode"))
	assert.Contains(t, r.stdout, i18n.T(msgid.CliFocusSet, "erp/portal", "deploy.local.yaml"))
	assert.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonFocus))
	assert.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonNeededBy, "erp/portal"))
	assert.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonOutsideFocus), "erp/worker 不在焦点之内")
	assert.Equal(t, readFile(t, in(dir, "deploy.yaml")), readFile(t, filepath.Join("testdata", "three-layer-shell", "deploy.yaml")),
		"团队文件一个字节都不动")
}

// shell/ 下的外壳目录一样（Review Focus 1）。
func TestUpFromShellDirectoryFocusesTheShell(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), in(dir, "shell", "erp", "shell"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, readFile(t, in(dir, "deploy.local.yaml")), "\nfocus: erp/shell\n")
}

// 组件目录里有自己的 brickkit.yaml（工作台）：跑的是工作台，外面项目的文件一个字节都不动（Review Focus 3）。
func TestUpInsideAWorkbenchRunsTheWorkbench(t *testing.T) {
	dir := focusFixture(t)
	bench := in(dir, "components", "erp", "portal")
	writeTree(t, bench, map[string]string{
		"brickkit.yaml": "project: erp-portal\ncomponents: []\n",
		"deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	r := runWithEngine(t, newFakeEngine(), bench, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoFileExists(t, in(dir, "deploy.local.yaml"))
	assert.NoFileExists(t, in(dir, ".brickkit", "local-mode"))
	assert.NotContains(t, r.stdout, "📁")
}

// --focus 在项目根也能用；--all 清掉焦点、全部照常启动；两个一起写、或配 -f / --no-local 是用法错误。
func TestUpFocusFlagAndAll(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/worker")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, readFile(t, in(dir, "deploy.local.yaml")), "\nfocus: erp/worker\n")

	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--all")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, readFile(t, in(dir, "deploy.local.yaml")), "focus:")
	assert.Contains(t, r.stdout, i18n.T(msgid.CliFocusCleared))
	assert.NotContains(t, r.stdout, i18n.T(msgid.CascadeReasonOutsideFocus))

	for _, args := range [][]string{
		{"up", "--dry-run", "--focus", "erp/api", "--all"},
		{"up", "--dry-run", "--focus", "erp/api", "-f", "deploy.yaml"},
		{"up", "--dry-run", "--focus", "erp/api", "--no-local"},
	} {
		r = runWithEngine(t, newFakeEngine(), dir, args...)
		assert.Equal(t, clierr.ExitUsage, r.code, "%v", args)
	}
}

// 本地文件里的个人修改与注释在写焦点后原样都在（Review Focus 2 的命令层一侧）。
func TestUpFocusKeepsPersonalEdits(t *testing.T) {
	dir := focusFixture(t)
	mustLocal(t, dir, "on")
	path := in(dir, "deploy.local.yaml")
	edited := readFile(t, path) + "# my own note\n"
	edited = strings.Replace(edited, "  - id: erp/portal\n", "  - id: erp/portal\n    localPort: 18090\n", 1)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	got := readFile(t, path)
	assert.Equal(t, edited, strings.Replace(got, "focus: erp/portal\n", "", 1))
}

// --focus 写了不存在的组件：报 COMPONENT_NOT_FOUND，而且什么都没写。
func TestUpFocusOnAnUnknownComponentWritesNothing(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/nope")
	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, i18n.T(msgid.ProjectFocusUnknown, "erp/nope"))
	assert.NoFileExists(t, in(dir, "deploy.local.yaml"))
}

// 焦点设着时，status / lint 也说一句焦点（与本地模式的提醒放在一起）。
func TestFocusIsAnnouncedByEveryCommandThatReadsIt(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	for _, args := range [][]string{{"status"}, {"lint"}} {
		r = runWithEngine(t, newFakeEngine(), dir, args...)
		assert.Contains(t, r.stdout, i18n.T(msgid.CliFocusLine, "erp/portal"), "%v", args)
	}
}

// 这次刚写进焦点：那一句"已写入"就是焦点的说明，不再紧跟一行同样的状态行；
// 焦点没变的下一次 up 才打状态行。
func TestUpSaysTheFocusOnce(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), in(dir, "components", "erp", "portal"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliFocusSet, "erp/portal", "deploy.local.yaml"))
	assert.NotContains(t, r.stdout, i18n.T(msgid.CliFocusLine, "erp/portal")+"\n")

	r = runWithEngine(t, newFakeEngine(), in(dir, "components", "erp", "portal"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, i18n.T(msgid.CliFocusSet, "erp/portal", "deploy.local.yaml"))
	assert.Equal(t, 1, strings.Count(r.stdout, i18n.T(msgid.CliFocusLine, "erp/portal")+"\n"))
}
