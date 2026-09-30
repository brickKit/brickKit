package cli

import (
	"os"
	"path/filepath"
	"regexp"
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

// 写进去会让个人文件通不过校验的焦点（k8s 上）：报错，文件一个字节都不动——
// 不能留下一份之后每条命令都读不了的 deploy.local.yaml。清掉焦点永远可以。
func TestUpFocusThatWouldBreakTheFileWritesNothing(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "local", "on")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	editFile(t, in(dir, "deploy.local.yaml"), "target: docker", "target: k8s")
	before := readFile(t, in(dir, "deploy.local.yaml"))

	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/api")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.ConfigFocusK8sUnsupported))
	assert.NotContains(t, r.stdout, i18n.T(msgid.CliFocusSet, "erp/api", "deploy.local.yaml"))
	assert.Equal(t, before, readFile(t, in(dir, "deploy.local.yaml")))
}

// 本地模式还没开、团队文件是 k8s：焦点设不上，那就什么都不做——不复制个人文件、不打开本地模式。
func TestUpFocusRefusedBeforeAnySideEffect(t *testing.T) {
	dir := focusFixture(t)
	editFile(t, in(dir, "deploy.yaml"), "target: docker", "target: k8s")

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/api")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.ConfigFocusK8sUnsupported))
	assert.NoFileExists(t, in(dir, "deploy.local.yaml"))
	assert.NoFileExists(t, in(dir, ".brickkit", "local-mode"))
}

// 在组件目录里 up、项目在 k8s 上：焦点是目录给的，不是使用者要的——提示先说怎么不带焦点跑整个项目。
func TestImplicitFocusOnK8sPointsAtAll(t *testing.T) {
	dir := focusFixture(t)
	editFile(t, in(dir, "deploy.yaml"), "target: docker", "target: k8s")

	r := runWithEngine(t, newFakeEngine(), in(dir, "components", "erp", "portal"), "up", "--dry-run")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.CliUpHintImplicitFocus, "erp/portal"))

	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
	assert.NotContains(t, r.stdout+r.stderr, i18n.T(msgid.CliUpHintImplicitFocus, "erp/portal"), "--focus was asked for explicitly")
}

// 真实的工作台（add --local --init 建的）：它继承的本地源 ../.. 正好提供工作台自己这个组件。
// 在工作台里，"我所在的组件"就是项目本身，不是焦点——up、deps、build 不带参数照旧作用于工作台（Final review #1）。
func TestARealWorkbenchIsNotItsOwnFocus(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "add", "--local", "--init", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	bench := in(dir, "components", "erp", "portal")
	require.FileExists(t, in(bench, "brickkit.yaml"))

	r = runWithEngine(t, newFakeEngine(), bench, "up", "--dry-run")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, i18n.T(msgid.CliFocusSet, "erp/portal", "deploy.local.yaml"))
	assert.NoFileExists(t, in(bench, "deploy.local.yaml"))

	r = runWithEngine(t, newFakeEngine(), bench, "deps")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 焦点是外壳：外壳带着它的成员跑，不是一个空壳（Final review #2）。
func TestFocusOnAShellRunsItsMembers(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/shell")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	for _, id := range []string{"erp/api", "erp/worker"} {
		assert.NotRegexp(t, id+`@1\.0\.0 +`+regexp.QuoteMeta(i18n.T(msgid.CascadeReasonOutsideFocus)), r.stdout, id)
	}
	assert.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonHostedBy, "erp/shell"))
}

// 焦点需要的组件是外壳的成员：它照项目声明的那样在外壳里跑，外壳跟着启动——
// 不回落成独立容器，也不冒出一句"把外壳重新打开"的误导警告（Final review #2）。
func TestFocusNeedingAMemberStartsItsShell(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotRegexp(t, `erp/shell@1\.0\.0 +`+regexp.QuoteMeta(i18n.T(msgid.CascadeReasonOutsideFocus)), r.stdout)
	assert.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonHosts, "erp/api"))
	assert.Regexp(t, `erp/worker@1\.0\.0 +`+regexp.QuoteMeta(i18n.T(msgid.CascadeReasonOutsideFocus)), r.stdout,
		"only the member the focus needs is reached; the shell hosts whichever members run")
}

// 焦点下 up 不说"sync 能把焦点之外的源码收起来"：sync 不看焦点，它什么都不会收（Final review #3）。
func TestUpUnderAFocusDoesNotPromiseWhatSyncWontDo(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/api")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonOutsideFocus))
	assert.NotContains(t, r.stdout, "brickkit sync")

	// 焦点之外、而没有焦点时也不跑的组件，照样提示：那是 sync 真会收起来的
	editFile(t, in(dir, "deploy.local.yaml"), "  - id: erp/portal\n", "  - id: erp/portal\n    mode: disable\n")
	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "brickkit sync")
}

// 焦点在跨文件的检查上通不过（条目写着 mode: disable、没有源码可跑）：报错，并且什么都没变——
// 个人文件没写上焦点、本地模式没被打开，之后的命令照常能用（Final review #5）。
func TestAFocusThatFailsTheProjectChecksChangesNothing(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"mode: disable": func(t *testing.T, dir string) {
			editFile(t, in(dir, "deploy.yaml"), "  - id: erp/portal\n", "  - id: erp/portal\n    mode: disable\n")
		},
		"no local source": func(t *testing.T, dir string) {
			require.NoError(t, os.RemoveAll(in(dir, "components", "erp", "portal")))
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			dir := focusFixture(t)
			breakIt(t, dir)
			r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
			assert.NotEqual(t, clierr.ExitOK, r.code)
			assert.NotContains(t, r.stdout, i18n.T(msgid.CliFocusSet, "erp/portal", "deploy.local.yaml"))
			assert.NoFileExists(t, in(dir, "deploy.local.yaml"))
			assert.NoFileExists(t, in(dir, ".brickkit", "local-mode"))

			r = runWithEngine(t, newFakeEngine(), dir, "status")
			assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
		})
	}
}
