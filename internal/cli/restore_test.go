package cli

// 本文件测 brickkit restore：yaml 的 enabled 逐条还原 + 结构跟着走。
//
// 最要紧的两条断言是"它不该做什么"：不吃掉未提交的 add，判定失败时不留下
// "yaml 改了、结构没动"的半成品。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

func boolp(b bool) *bool { return &b }

func TestRestorePlanOnlyTouchesEntriesPresentInBothVersions(t *testing.T) {
	head := &config.Config{Components: []config.Component{
		{ID: "demo/hello", Version: "1.0.0"}, // 提交里没写 enabled
		{ID: "demo/caller", Version: "1.0.0", Enabled: boolp(true)},
		{ID: "gone/thing", Version: "1.0.0"}, // 本地已 remove
	}}
	work := &config.Config{Components: []config.Component{
		{ID: "demo/hello", Version: "1.0.0", Enabled: boolp(false)},  // 本地关掉了
		{ID: "demo/caller", Version: "1.0.0", Enabled: boolp(true)},  // 没变
		{ID: "brand/new", Version: "0.1.0", Enabled: boolp(false)},   // 本地新 add 的
		{ID: "demo/bumped", Version: "2.0.0", Enabled: boolp(false)}, // 本地改了版本号
	}}

	changes, untouched := restorePlan(work, head)

	require.Len(t, changes, 1, "只有 hello 需要还原")
	assert.Equal(t, "demo/hello", changes[0].id)
	assert.Nil(t, changes[0].to, "提交里没写 enabled → 删掉这个字段，不是写 false")
	require.NotNil(t, changes[0].from)
	assert.False(t, *changes[0].from)

	assert.ElementsMatch(t, []string{"brand/new@0.1.0", "demo/bumped@2.0.0"}, untouched,
		"提交里没有的条目一个字不动——这是不吃掉未提交 add 的解药")
}

func TestRestoreRestoresEnabledAndMovesSourceBack(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	f.assertArchived(t, "demo/hello")

	r := runIn(t, f.Dir, "restore")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "enabled: false")
	assert.Contains(t, r.stdout, "删除该字段")

	f.assertActive(t, "demo/hello")
	f.assertActive(t, "demo/caller")

	cfg := f.parsed(t)
	assert.Nil(t, cfg.Components[0].Enabled, "enabled 回到「不写」")
}

func TestRestoreKeepsUncommittedAddInTheConfig(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		{ID: "solo/thing", Version: "1.0.0"},
	}
	f := addedProject(t, comps)
	f.writeConfig(t, allEnabled)
	initGitRepo(t, filepath.Join(f.Dir, "components", "demo", "hello"))
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	// 提交之后才 add 的组件
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "solo/thing@1.0.0", "--yes").code)

	r := runIn(t, f.Dir, "restore")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	cfg := f.parsed(t)
	var ids []string
	for _, c := range cfg.Components {
		ids = append(ids, c.ID)
	}
	assert.Contains(t, ids, "solo/thing",
		"restore 只动 enabled，绝不像 git checkout 那样把未提交的 add 一起吃掉")
}

func TestRestoreRejectsStagedComponentChanges(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	// 在归档目录里改了代码并暂存（004 §3.9.3 明说允许在那儿改）
	require.NoError(t, os.WriteFile(
		filepath.Join(f.archived("demo/hello"), "component.yaml"), []byte("# 改了\n"), 0o644))
	gitDo(t, f.Dir, "add", "-A", "components")

	r := runIn(t, f.Dir, "restore")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "已暂存")
	f.assertArchived(t, "demo/hello")
}

func TestRestoreRejectsSourceInBothPlaces(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	// 手工造出"两处都有"
	require.NoError(t, os.MkdirAll(f.archived("demo/hello"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(f.archived("demo/hello"), "component.yaml"), []byte("# 另一份\n"), 0o644))

	r := runIn(t, f.Dir, "restore")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "两处")
	assert.Contains(t, r.stderr, "不替你决定")
}

func TestRestoreRequiresGitBaseline(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello")

	r := runIn(t, f.Dir, "restore")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "git")

	gitProject(t, f.Dir) // 有仓库了，但还没有任何提交
	r = runIn(t, f.Dir, "restore")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "提交")
}

func TestRestoreIsIdempotent(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	first := runIn(t, f.Dir, "restore")
	require.Equal(t, clierr.ExitOK, first.code, first.stdout+first.stderr)
	after := f.config(t)

	second := runIn(t, f.Dir, "restore")
	require.Equal(t, clierr.ExitOK, second.code, second.stdout+second.stderr)
	assert.Equal(t, after, f.config(t), "连跑两次的结果必须一样")
	f.assertActive(t, "demo/hello")
}

// TestRestoreLeavesConfigUntouchedWhenFocusFails 补的是 §5.3 顺序约束本身：
// 先算判定（syncFocus），算成功了才落盘 yaml。反过来会在判定失败时留下
// "yaml 改了、结构没动"的半成品——那正是提交前最不该撞上的状态。
//
// 用 solo/thing@9.9.9 制造判定失败：语法上合法（ParseConfig 认得），但本地
// 安装源里只有 1.0.0，resolver.ResolveConfig 解不出来，syncFocus 因此报错——
// 与 TestCheckWarnsWhenGraphResolutionFails 用的是同一个机制。
func TestRestoreLeavesConfigUntouchedWhenFocusFails(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "archive hello")

	// 工作区换成引用不存在版本的配置：语法合法，但 syncFocus 解不出来。
	f.writeConfig(t, helloDisabledWithUnresolvable)
	before := f.config(t)

	r := runIn(t, f.Dir, "restore")

	assert.Equal(t, clierr.ExitError, r.code, "判定失败必须非零退出：%s%s", r.stdout, r.stderr)
	assert.Equal(t, before, f.config(t), "判定失败时 yaml 必须一个字节都没被改动")
}
