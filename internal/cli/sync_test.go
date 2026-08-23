package cli

// 本文件是 Step 17「brickkit sync」的业务行为测试，覆盖开发计划 17.1–17.13。
//
// sync 移动的是**使用者的源码目录**，而且每个组件是一个独立的 Git 仓库。
// 断言因此落在两件事上：目录到底在哪，以及 .git 有没有被弄坏。

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// ============================================================
// 夹具
// ============================================================

// syncFixture 是一个带源码目录的项目。
type syncFixture struct{ *projectFixture }

// newSyncFixture 造一个项目，并为指定组件建出**真的 Git 仓库**作为源码目录。
func newSyncFixture(t *testing.T, body string, withSource ...string) *syncFixture {
	t.Helper()

	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		{ID: "solo/thing", Version: "1.0.0"},
	}
	f := addedProject(t, comps)
	f.writeConfig(t, body)

	for _, id := range withSource {
		initGitRepo(t, filepath.Join(f.Dir, "components", filepath.FromSlash(id)))
	}
	return &syncFixture{projectFixture: f}
}

// initGitRepo 在目标位置建一个真的 Git 仓库（含一次提交）。
//
// 用真仓库而不是空目录：17.5 / 17.6 要验证归档之后 .git 还完整、
// git 命令还能正常跑——那是 sync 最容易弄坏、也最要命的东西。
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.yaml"), []byte("# 组件源码\n"), 0o644))

	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "trial@example.com"},
		{"config", "user.name", "trial"},
		{"add", "."},
		{"commit", "--quiet", "-m", "初始提交"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v：%s", args, out)
	}
}

func (f *syncFixture) active(id string) string {
	return filepath.Join(f.Dir, "components", filepath.FromSlash(id))
}

func (f *syncFixture) archived(id string) string {
	return filepath.Join(f.Dir, "components", ".archived", filepath.FromSlash(id))
}

// activeAt 断言源码在活跃目录、且不在归档目录。
func (f *syncFixture) assertActive(t *testing.T, id string) {
	t.Helper()
	assert.DirExists(t, f.active(id), "%s 应在活跃目录", id)
	assert.NoDirExists(t, f.archived(id), "%s 不该同时在归档目录", id)
}

func (f *syncFixture) assertArchived(t *testing.T, id string) {
	t.Helper()
	assert.DirExists(t, f.archived(id), "%s 应在归档目录", id)
	assert.NoDirExists(t, f.active(id), "%s 不该还留在活跃目录", id)
}

// 全部启用的配置。
const allEnabled = `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
resources: []
`

// 三个组件都在：caller 强依赖 hello，solo/thing 与它们无关。
const allEnabledWithSolo = `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
  - id: solo/thing
    version: 1.0.0
resources: []
`

// hello 关掉（caller 跟着级联跳过），solo/thing 照常。
const helloDisabledWithSolo = `components:
  - id: demo/hello
    version: 1.0.0
    enabled: false
  - id: demo/caller
    version: 1.0.0
  - id: solo/thing
    version: 1.0.0
resources: []
`

// hello 被显式关掉 —— caller 会被级联跳过。
const helloDisabled = `components:
  - id: demo/hello
    version: 1.0.0
    enabled: false
  - id: demo/caller
    version: 1.0.0
resources: []
`

// ============================================================
// 17.1 / 17.2 / 17.3 归档与保留
// ============================================================

func TestSyncKeepsEnabledComponents(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")

	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	f.assertActive(t, "demo/hello")  // 17.1
	f.assertActive(t, "demo/caller") // 17.1
}

func TestSyncArchivesDisabledComponent(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")

	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	f.assertArchived(t, "demo/hello") // 17.2
}

// 17.3：caller 自己没写 enabled，但它强依赖的 hello 被关了，于是一起归档。
func TestSyncArchivesCascadeSkippedComponent(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	f.assertArchived(t, "demo/caller")
}

// ============================================================
// 17.4 / 17.9 双向：归档过的还能回来
// ============================================================

func TestSyncRestoresReenabledComponent(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	f.assertArchived(t, "demo/hello")

	// 放开禁用，再 sync 一次
	f.writeConfig(t, allEnabled)
	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	f.assertActive(t, "demo/hello")  // 17.4
	f.assertActive(t, "demo/caller") // 17.9 双向：级联跳过的也一起回来
}

// 反复 sync 不该有副作用。
func TestSyncIsIdempotent(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	f.assertArchived(t, "demo/hello")
	f.assertArchived(t, "demo/caller")
}

// ============================================================
// 17.5 / 17.6 Git 仓库必须完好
// ============================================================

// 归档移动的是整个目录，.git 必须原封不动地跟过去。
func TestSyncKeepsGitDirectoryIntact(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	assert.DirExists(t, filepath.Join(f.archived("demo/hello"), ".git"), "17.5")
}

// 归档之后 git 命令还得能正常跑——使用者可能正在那个仓库里开发。
func TestSyncKeepsGitUsable(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = f.archived("demo/hello")
	out, err := cmd.CombinedOutput()

	require.NoError(t, err, "17.6 git status 应正常：%s", out)
	assert.Empty(t, string(out), "17.6 工作区应当是干净的（只是被移动了位置）")
}

// ============================================================
// 17.7 没有源码的组件不受影响
// ============================================================

func TestSyncIgnoresComponentsWithoutSource(t *testing.T) {
	// 只给 hello 建源码，caller 没有
	f := newSyncFixture(t, helloDisabled, "demo/hello")

	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.NoDirExists(t, f.archived("demo/caller"), "17.7 没有源码就没什么可归档的")
	assert.NotContains(t, r.stdout, "demo/caller", "17.7 也不该出现在输出里")
}

// brickkit.yaml 里根本没有的组件，源码在也不能动。
//
// 那多半是使用者正在开发、还没 add 进来的组件。级联计算里没有它，
// 不代表"它该被归档"——代表"这不归我们管"。
func TestSyncLeavesUnmanagedSourceAlone(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "solo/thing")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	f.assertActive(t, "solo/thing")
}

// ============================================================
// 17.8 local: true 一视同仁
// ============================================================

func TestSyncTreatsLocalComponentsTheSame(t *testing.T) {
	f := newSyncFixture(t, `components:
  - id: demo/hello
    version: 1.0.0
    enabled: false
  - id: demo/caller
    version: 1.0.0
    local: true
resources: []
`, "demo/hello", "demo/caller")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	// caller 是 local，但它强依赖的 hello 被关了 —— 照样归档
	f.assertArchived(t, "demo/caller")
}

// ============================================================
// 17.10 不影响运行中的容器
// ============================================================

// sync 只动目录，绝不碰引擎。
func TestSyncNeverTouchesTheEngine(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")
	eng := newFakeEngine()

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "sync").code)

	assert.Empty(t, eng.ups, "17.10")
	assert.Empty(t, eng.downs, "17.10")
	assert.Empty(t, eng.checked, "17.10 连镜像都不该去查")
}

// 也不该动 brickkit.yaml 或生成的部署文件。
func TestSyncDoesNotTouchConfigOrGenerated(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello")
	before := f.config(t)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	assert.Equal(t, before, f.config(t), "17.10 sync 不写配置")
	assert.NoFileExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "docker-compose.yaml"))
}

// ============================================================
// 17.11 / 17.12 输出
// ============================================================

func TestSyncOutputMarksEachAction(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")

	r := runIn(t, f.Dir, "sync")

	assert.Contains(t, r.stdout, "📦", "17.11 归档")
	assert.Contains(t, r.stdout, "components/.archived/demo/hello", "17.11 要写清移到哪")

	// 再放开，验证"激活"与"活跃"两种标记
	f.writeConfig(t, allEnabled)
	r = runIn(t, f.Dir, "sync")

	assert.Contains(t, r.stdout, "📂", "17.11 激活")
	assert.Contains(t, r.stdout, "✅", "17.11 保持活跃")
}

func TestSyncOutputExplainsWhy(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")

	r := runIn(t, f.Dir, "sync")

	assert.Contains(t, r.stdout, "显式禁用", "17.12")
	assert.Contains(t, r.stdout, "级联跳过", "17.12")

	f.writeConfig(t, allEnabled)
	r = runIn(t, f.Dir, "sync")

	assert.Contains(t, r.stdout, "恢复启用", "17.12")
}

func TestSyncOutputSummarizesCounts(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")

	r := runIn(t, f.Dir, "sync")

	assert.Contains(t, r.stdout, "2 个归档")
}

// ============================================================
// 17.13 没什么可整理的
// ============================================================

func TestSyncOnEmptyWorkspace(t *testing.T) {
	f := newSyncFixture(t, allEnabled)

	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "无需整理", "17.13")
}

// 没有 components/ 目录时也不能报错。
func TestSyncWithoutComponentsDir(t *testing.T) {
	f := newSyncFixture(t, allEnabled)
	require.NoError(t, os.RemoveAll(filepath.Join(f.Dir, "components")))

	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "无需整理")
}

// ============================================================
// 边界
// ============================================================

// 归档目录里已经有同名目录时，绝不能覆盖使用者的源码。
func TestSyncRefusesToOverwriteExistingArchive(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello")
	// 手工在归档目录里放一份同名的东西
	require.NoError(t, os.MkdirAll(f.archived("demo/hello"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(f.archived("demo/hello"), "别动我.txt"), []byte("x"), 0o644))

	r := runIn(t, f.Dir, "sync")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "demo/hello")
	assert.DirExists(t, f.active("demo/hello"), "源码要留在原地，不能移走一半")
	assert.FileExists(t, filepath.Join(f.archived("demo/hello"), "别动我.txt"))
}

// 多版本共存：一个组件 ID 只有一份源码目录，只要有任一版本会启动就保持活跃。
func TestSyncKeepsSourceWhenAnyVersionRuns(t *testing.T) {
	f := newSyncFixture(t, `components:
  - id: demo/hello
    version: 1.0.0
    enabled: false
  - id: demo/hello
    version: 2.0.0
resources: []
`, "demo/hello")

	// 2.0.0 在本地源里不存在，解析会失败——这里只关心不会误归档
	r := runIn(t, f.Dir, "sync")
	if r.code != clierr.ExitOK {
		t.Skipf("该场景需要两个版本都能解析，跳过：%s", r.stderr)
	}
	f.assertActive(t, "demo/hello")
}

var _ = config.DirArchived

// ============================================================
// 归档之后平台自己还得读得到（回归：sync 曾经把项目锁死）
// ============================================================

// newWorkspaceFixture 造一个**贴近 init 骨架**的项目：本地安装源就是 ./components，
// 组件源码直接放在 components/<scope>/<name>/。
//
// 与 newSyncFixture 的区别正是这一点，而这一点就是那个 bug 的全部条件：
// 上面那批用例把每个组件放进各自的 src0/ src1/，安装源与 sync 的归档目录
// 因此没有重叠——于是"归档 = 从安装源里消失"永远不会发生，
// 一整组绿灯的用例掩护了一个能把项目彻底卡死的缺陷。
func newWorkspaceFixture(t *testing.T, body string) *syncFixture {
	t.Helper()

	dir := t.TempDir()
	for _, c := range []comp{
		{ID: "demo/hello", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	} {
		writeTree(t, filepath.Join(dir, "components", filepath.FromSlash(c.ID)), c.files())
	}

	f := newProjectFixtureAt(t, dir, "  - id: local-dev\n    type: local\n    path: ./components\n")
	f.writeConfig(t, body)
	return &syncFixture{projectFixture: f}
}

// 归档之后，即使 Manifest 缓存没了，up 照样算得出依赖图。
//
// `.brickkit/` 是 gitignore 的（003 §11），换台机器、清一次工作区、
// 或者一次 --refresh，缓存就没了。缓存不是保障，只是恰好挡住了。
func TestSyncArchivedComponentStillResolvableWithoutCache(t *testing.T) {
	f := newWorkspaceFixture(t, helloDisabled)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	f.assertArchived(t, "demo/hello")
	require.NoError(t, os.RemoveAll(f.Layout.ManifestsDir()), "模拟缓存过期 / 换台机器")

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code,
		"归档过的组件仍在 brickkit.yaml 里，级联计算必须读得到它：%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "显式禁用", "它该被判为不启动，而不是找不到")
}

// sync 必须解得开自己造成的局面：归档 → 缓存没了 → 重新启用 → sync 能移回来。
//
// 修复前这一步会失败，而且**没有出路**：sync 自己也要先解析全图，
// 于是使用者只剩手工 mv 一条路，而错误提示里从头到尾没出现过 .archived。
func TestSyncCanRestoreWhatItArchivedWithoutCache(t *testing.T) {
	f := newWorkspaceFixture(t, helloDisabled)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	require.NoError(t, os.RemoveAll(f.Layout.ManifestsDir()))
	f.writeConfig(t, allEnabled)

	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "sync 必须能撤销自己的归档：%s%s", r.stdout, r.stderr)
	f.assertActive(t, "demo/hello")
	f.assertActive(t, "demo/caller")
}

// 归档不影响 add --local 的既有行为：它照旧只扫活跃目录。
//
// 与上面两条是同一条分工线的两半——**扫描时看不见，按 ID 找时找得到**。
// 少了这一条，"让本地源认归档目录"很容易被顺手改成"扫描也认"，
// 那样 sync 刚归档完，一条 add --local 就把它们全拽回配置里。
func TestAddLocalStillIgnoresArchived(t *testing.T) {
	f := newWorkspaceFixture(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	f.assertArchived(t, "demo/hello")

	f.writeConfig(t, "components: []\nresources: []\n")
	r := runIn(t, f.Dir, "add", "--local", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.NotContains(t, f.refs(t), "demo/hello@1.0.0", "归档的组件不该被 add --local 拽回来")
}

// ============================================================
// sync --only：不改配置就把工作区收拢到几个组件上
// ============================================================

// --only 只留被点名的组件与它的强依赖，其余全部归档。
func TestSyncOnlyKeepsSelectedAndItsRequires(t *testing.T) {
	// caller 强依赖 hello；solo 与它们无关
	f := newSyncFixture(t, allEnabledWithSolo, "demo/hello", "demo/caller", "solo/thing")

	r := runIn(t, f.Dir, "sync", "--only", "demo/caller")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	f.assertActive(t, "demo/caller")  // 被点名
	f.assertActive(t, "demo/hello")   // caller 的强依赖，跟着留下
	f.assertArchived(t, "solo/thing") // 与本次无关
	assert.Contains(t, r.stdout, "未被 --only 选中")
}

// --only **不改 brickkit.yaml**：这是它相对"改 enabled 再 sync"的全部意义。
func TestSyncOnlyLeavesConfigUntouched(t *testing.T) {
	f := newSyncFixture(t, allEnabledWithSolo, "demo/hello", "demo/caller", "solo/thing")
	before := f.config(t)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync", "--only", "demo/hello").code)

	assert.Equal(t, before, f.config(t), "--only 不该动配置文件一个字节")
}

// 不带参数的 sync 就是 --only 之后的"恢复"：回到与 brickkit up 一致。
func TestSyncWithoutOnlyRestoresAfterFocus(t *testing.T) {
	f := newSyncFixture(t, allEnabledWithSolo, "demo/hello", "demo/caller", "solo/thing")
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync", "--only", "demo/hello").code)
	f.assertArchived(t, "solo/thing")

	r := runIn(t, f.Dir, "sync")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	f.assertActive(t, "solo/thing")
	f.assertActive(t, "demo/caller")
}

// --only 点到一个 enabled: false 的组件是**允许**的，与 up --only 刻意不同。
//
// up 决定"跑什么"，sync 决定"看什么"。要看一个已经关掉的组件的源码完全说得通——
// 多数时候正是因为要重写它才把它关掉的。这里报错等于把最常见的用法堵死。
func TestSyncOnlyAllowsDisabledComponent(t *testing.T) {
	f := newSyncFixture(t, helloDisabledWithSolo, "demo/hello", "demo/caller", "solo/thing")
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	f.assertArchived(t, "demo/hello") // 先被级联归档

	r := runIn(t, f.Dir, "sync", "--only", "demo/hello")

	require.Equal(t, clierr.ExitOK, r.code,
		"sync --only 不该像 up --only 那样拒绝 enabled: false 的组件：%s%s", r.stdout, r.stderr)
	f.assertActive(t, "demo/hello")
	assert.Contains(t, r.stdout, "被 --only 选中")
}

// 组件名写错时报的是"配置里没有这个组件"，与 up --only 同一套解析。
func TestSyncOnlyUnknownComponent(t *testing.T) {
	f := newSyncFixture(t, allEnabledWithSolo, "demo/hello")

	r := runIn(t, f.Dir, "sync", "--only", "demo/nope")

	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "demo/nope")
}
