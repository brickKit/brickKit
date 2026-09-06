// 本文件是 Step 9 中组件源码工作区（components/）的单元测试。
package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
)

func newLayout(t *testing.T) config.Layout {
	t.Helper()
	layout := config.NewLayout(t.TempDir(), "")
	require.NoError(t, os.MkdirAll(layout.ComponentsDir(), 0o755))
	return layout
}

// newRepo 建一个可 clone 的本地 git 仓库。
func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=T", "commit", "-q", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
}

// ============================================================
// 路径
// ============================================================

func TestSourceDirAndDisplayDir(t *testing.T) {
	layout := config.NewLayout("/projects/erp", "")

	assert.Equal(t, filepath.FromSlash("/projects/erp/components/people/basic"),
		SourceDir(layout, "people/basic"))
	assert.Equal(t, "components/people/basic/", DisplayDir("people/basic"))
}

func TestExists(t *testing.T) {
	layout := newLayout(t)
	assert.False(t, Exists(layout, "people/basic"))

	require.NoError(t, os.MkdirAll(SourceDir(layout, "people/basic"), 0o755))
	assert.True(t, Exists(layout, "people/basic"))

	// 同名普通文件不算源码目录
	require.NoError(t, os.MkdirAll(filepath.Join(layout.ComponentsDir(), "erp"), 0o755))
	require.NoError(t, os.WriteFile(SourceDir(layout, "erp/backend"), []byte("x"), 0o644))
	assert.False(t, Exists(layout, "erp/backend"))
}

// ============================================================
// Clone
// ============================================================

// clone 出来的必须是完整仓库（带 .git），使用者要能在里面改代码并提交。
func TestCloneCreatesFullRepository(t *testing.T) {
	layout := newLayout(t)
	repo := newRepo(t, map[string]string{
		"component.yaml": "metadata:\n  id: people/basic\n",
		"README.md":      "# people/basic\n",
	})

	target, err := Clone(context.Background(), layout, "people/basic", "people/basic@1.0.0", repo)
	require.NoError(t, err)

	assert.Equal(t, SourceDir(layout, "people/basic"), target)
	assert.DirExists(t, filepath.Join(target, ".git"))
	assert.FileExists(t, filepath.Join(target, "component.yaml"))
	assert.FileExists(t, filepath.Join(target, "README.md"))
	assert.True(t, Exists(layout, "people/basic"))
}

// 目标目录已存在时报错，且不碰已有内容（004 §3.3 输出样例）。
func TestCloneRefusesExistingDirectory(t *testing.T) {
	layout := newLayout(t)
	target := SourceDir(layout, "people/basic")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "mine.txt"), []byte("我的源码"), 0o644))

	_, err := Clone(context.Background(), layout, "people/basic", "people/basic@1.0.0", "irrelevant")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeCloneFailed, e.Code)
	out := e.Format()
	assert.Contains(t, out, "clone 失败：目录已存在")
	assert.Contains(t, out, "components/people/basic/")
	assert.Contains(t, out, "people/basic@1.0.0")

	data, err := os.ReadFile(filepath.Join(target, "mine.txt"))
	require.NoError(t, err)
	assert.Equal(t, "我的源码", string(data))
}

// 源码被 sync 归档着时，绝不在活跃目录再 clone 一份。
//
// 这条守的是 004 §8.1 的不变量：一个组件 ID 只有一个源码目录。
// 从前只查活跃目录，于是"归档 → 再 add --repo"会造出两份，
// 而下一次 sync 卡死在"目标目录已存在，无法移动组件源码"上。
func TestCloneRefusesArchivedSource(t *testing.T) {
	layout := newLayout(t)
	archived := ArchivedDir(layout, "people/basic")
	require.NoError(t, os.MkdirAll(archived, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(archived, "mine.txt"), []byte("我的源码"), 0o644))

	_, err := Clone(context.Background(), layout, "people/basic", "people/basic@1.0.0", "irrelevant")
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "只是被归档着", "要说清源码没丢")
	assert.Contains(t, out, "components/.archived/people/basic", "要指出它在哪")
	assert.Contains(t, out, "brickkit sync", "要给出把它移回来的办法")

	// 活跃目录一个字节都不该被创建出来
	assert.NoDirExists(t, SourceDir(layout, "people/basic"))
	data, err := os.ReadFile(filepath.Join(archived, "mine.txt"))
	require.NoError(t, err)
	assert.Equal(t, "我的源码", string(data))
}

// Locate 三态：两处都没有 / 在活跃目录 / 在归档目录。
func TestLocate(t *testing.T) {
	layout := newLayout(t)
	assert.Equal(t, StateMissing, Locate(layout, "people/basic"))

	require.NoError(t, os.MkdirAll(ArchivedDir(layout, "people/basic"), 0o755))
	assert.Equal(t, StateArchived, Locate(layout, "people/basic"))
	assert.True(t, IsArchived(layout, "people/basic"))
	assert.False(t, Exists(layout, "people/basic"))

	// 两处都有时以活跃目录为准——那是 sync 会撞上并明确报错的状态，
	// 不该由这里替它下结论
	require.NoError(t, os.MkdirAll(SourceDir(layout, "people/basic"), 0o755))
	assert.Equal(t, StateActive, Locate(layout, "people/basic"))
}

// 仓库地址不可达时报错，并且不留下半个目录。
func TestCloneUnreachableRepository(t *testing.T) {
	layout := newLayout(t)
	missing := filepath.Join(t.TempDir(), "no-such-repo.git")

	_, err := Clone(context.Background(), layout, "people/basic", "people/basic@1.0.0", missing)
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeCloneFailed, e.Code)
	assert.Contains(t, e.Format(), "no-such-repo.git")
	assert.False(t, Exists(layout, "people/basic"), "失败后不得留下空目录")
}

// components/ 的父目录被文件占住时报错。
func TestCloneCannotCreateParent(t *testing.T) {
	layout := config.NewLayout(t.TempDir(), "")
	require.NoError(t, os.WriteFile(layout.ComponentsDir(), []byte("占位"), 0o644))

	_, err := Clone(context.Background(), layout, "people/basic", "people/basic@1.0.0", "irrelevant")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "无法创建源码目录")
}

// ============================================================
// RemoveSource
// ============================================================

func TestRemoveSource(t *testing.T) {
	layout := newLayout(t)
	target := SourceDir(layout, "people/basic")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "sub", "a.go"), []byte("x"), 0o644))

	removed, err := RemoveSource(layout, "people/basic", nil)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NoDirExists(t, target)
	assert.NoDirExists(t, filepath.Join(layout.ComponentsDir(), "people"), "空的 scope 目录一并收走")
}

func TestRemoveSourceMissingIsNotAnError(t *testing.T) {
	layout := newLayout(t)

	removed, err := RemoveSource(layout, "people/basic", nil)
	require.NoError(t, err)
	assert.False(t, removed)
}

// 同 scope 下还有别的组件时，scope 目录必须保留。
func TestRemoveSourceKeepsNonEmptyScope(t *testing.T) {
	layout := newLayout(t)
	require.NoError(t, os.MkdirAll(SourceDir(layout, "people/basic"), 0o755))
	require.NoError(t, os.MkdirAll(SourceDir(layout, "people/advanced"), 0o755))

	removed, err := RemoveSource(layout, "people/basic", nil)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.DirExists(t, filepath.Join(layout.ComponentsDir(), "people"))
}

// 组件 ID 不含 scope（异常输入）时，父目录就是根目录本身，空了也绝不能删。
func TestPruneEmptyScopeNeverRemovesRoot(t *testing.T) {
	t.Run("components 根目录", func(t *testing.T) {
		layout := newLayout(t)
		require.NoError(t, os.MkdirAll(layout.ComponentsDir(), 0o755))

		activeLoc(layout, "solo").pruneEmptyScope()
		assert.DirExists(t, layout.ComponentsDir())
	})

	t.Run("归档根目录", func(t *testing.T) {
		layout := newLayout(t)
		require.NoError(t, os.MkdirAll(layout.ArchivedDir(), 0o755))

		archivedLoc(layout, "solo").pruneEmptyScope()
		assert.DirExists(t, layout.ArchivedDir())
	})
}

// RemoveArchived 与 RemoveSource 对称：删归档目录，并收走空掉的 scope 目录。
func TestRemoveArchived(t *testing.T) {
	layout := newLayout(t)
	target := ArchivedDir(layout, "people/basic")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "sub", "a.go"), []byte("x"), 0o644))

	removed, err := RemoveArchived(layout, "people/basic", nil)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NoDirExists(t, target)
	assert.NoDirExists(t, filepath.Join(layout.ArchivedDir(), "people"), "空的 scope 目录一并收走")
}

func TestRemoveArchivedMissingIsNotAnError(t *testing.T) {
	layout := newLayout(t)

	removed, err := RemoveArchived(layout, "people/basic", nil)
	require.NoError(t, err)
	assert.False(t, removed)
}

// RemoveArchived 只动归档目录，活跃源码一根汗毛都不能碰。
func TestRemoveArchivedLeavesActiveSourceAlone(t *testing.T) {
	layout := newLayout(t)
	require.NoError(t, os.MkdirAll(SourceDir(layout, "people/basic"), 0o755))
	require.NoError(t, os.MkdirAll(ArchivedDir(layout, "people/basic"), 0o755))

	removed, err := RemoveArchived(layout, "people/basic", nil)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.DirExists(t, SourceDir(layout, "people/basic"))
}

func TestRemoveSourceUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限位不生效")
	}
	layout := newLayout(t)
	target := SourceDir(layout, "people/basic")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "sub"), 0o755))
	require.NoError(t, os.Chmod(target, 0o500)) // 只读：无法删除子目录
	t.Cleanup(func() { _ = os.Chmod(target, 0o755) })

	_, err := RemoveSource(layout, "people/basic", nil)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "删除源码目录失败")
}

func TestFirstLine(t *testing.T) {
	assert.Equal(t, "fatal: repository not found",
		firstLine("\n fatal: repository not found\n更多\n", assert.AnError))
	assert.Equal(t, assert.AnError.Error(), firstLine("  \n\n", assert.AnError))
}

func TestInBothPlacesAnswersWhatLocateCannot(t *testing.T) {
	dir := t.TempDir()
	l := config.NewLayout(dir, "")
	const id = "demo/hello"

	require.NoError(t, os.MkdirAll(SourceDir(l, id), 0o755))
	assert.False(t, InBothPlaces(l, id))

	require.NoError(t, os.MkdirAll(ArchivedDir(l, id), 0o755))
	assert.True(t, InBothPlaces(l, id))
	assert.Equal(t, StateActive, Locate(l, id), "Locate 活跃优先，答不出这一种")
}

// ============================================================
// submodule 阻断（2026-09-06 gap report §2.2 / §2.3 的安全版修复）
//
// Archive/Activate（move）与 RemoveSource/RemoveArchived（removeDir）在真正
// 动文件系统之前，先问一句"目标是不是已登记的 submodule"——是就阻断并报错，
// 不静默地把 .gitmodules 记的 path 和实际位置弄得对不上，也不留下需要人工
// 善后的 git 状态。
// ============================================================

// newGitLayout 造一个本身是 git 仓库的项目布局：submodule 阻断要靠
// gitrepo.Open 才能生效，plain newLayout(t) 不是 git 仓库。
func newGitLayout(t *testing.T) config.Layout {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	layout := config.NewLayout(dir, "")
	require.NoError(t, os.MkdirAll(layout.ComponentsDir(), 0o755))
	return layout
}

// registerSubmodule 手写一条 .gitmodules 登记记录。不需要真的跑
// git submodule add——阻断逻辑只看"这个路径有没有登记"，不看子模块内部
// 结构是否完整（那部分已经由 internal/gitrepo 自己的测试覆盖）。
func registerSubmodule(t *testing.T, layout config.Layout, name, rel string) *gitrepo.Repo {
	t.Helper()
	git(t, layout.Root, "config", "-f", ".gitmodules", "submodule."+name+".path", rel)
	git(t, layout.Root, "config", "-f", ".gitmodules", "submodule."+name+".url", "git@example.com:x.git")
	repo, err := gitrepo.Open(layout.Root)
	require.NoError(t, err)
	return repo
}

func TestArchiveBlocksRegisteredSubmodule(t *testing.T) {
	layout := newGitLayout(t)
	dir := SourceDir(layout, "mdm/customer")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	repo := registerSubmodule(t, layout, "mdm/customer", "components/mdm/customer")

	err := Archive(layout, "mdm/customer", repo)
	require.Error(t, err)
	assert.DirExists(t, dir, "阻断之前不该移动任何东西")
	assert.NoDirExists(t, ArchivedDir(layout, "mdm/customer"))

	// 真跑一遍才发现的：git mv 不会自动建目标的父目录——第一次归档某个
	// scope 时 components/.archived/mdm/ 还不存在，裸给 "git mv 源 目标"
	// 会当场 fatal: renaming ... failed: No such file or directory。
	// 建议里必须先有 mkdir -p，不然照抄的人会撞墙。
	assert.Contains(t, clierr.As(err).Format(), "mkdir -p components/.archived/mdm")
}

func TestActivateBlocksRegisteredSubmodule(t *testing.T) {
	layout := newGitLayout(t)
	dir := ArchivedDir(layout, "mdm/customer")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	repo := registerSubmodule(t, layout, "mdm/customer", "components/.archived/mdm/customer")

	err := Activate(layout, "mdm/customer", repo)
	require.Error(t, err)
	assert.DirExists(t, dir, "阻断之前不该移动任何东西")
	assert.NoDirExists(t, SourceDir(layout, "mdm/customer"))
}

func TestRemoveSourceBlocksRegisteredSubmodule(t *testing.T) {
	layout := newGitLayout(t)
	dir := SourceDir(layout, "mdm/customer")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	repo := registerSubmodule(t, layout, "mdm/customer", "components/mdm/customer")

	removed, err := RemoveSource(layout, "mdm/customer", repo)
	require.Error(t, err)
	assert.False(t, removed)
	assert.DirExists(t, dir, "阻断之前不该删除任何东西")
}

func TestRemoveArchivedBlocksRegisteredSubmodule(t *testing.T) {
	layout := newGitLayout(t)
	dir := ArchivedDir(layout, "mdm/customer")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	repo := registerSubmodule(t, layout, "mdm/customer", "components/.archived/mdm/customer")

	removed, err := RemoveArchived(layout, "mdm/customer", repo)
	require.Error(t, err)
	assert.False(t, removed)
	assert.DirExists(t, dir, "阻断之前不该删除任何东西")
}

// 没有 .gitmodules 登记时（意外死 gitlink、或压根没有 submodule 概念），
// 现有行为必须完全不变——这条修复只该拦"已登记"的情形。
func TestArchiveIgnoresUnregisteredGitlink(t *testing.T) {
	layout := newGitLayout(t)
	dir := SourceDir(layout, "mdm/customer")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	repo, err := gitrepo.Open(layout.Root)
	require.NoError(t, err)

	require.NoError(t, Archive(layout, "mdm/customer", repo))
	assert.NoDirExists(t, dir)
	assert.DirExists(t, ArchivedDir(layout, "mdm/customer"))
}

// repo 为 nil（不在任何 git 仓库里，up/sync 在没有 git 的项目里也得能用）时
// 也必须完全不受影响。
func TestArchiveWorksOutsideGitRepo(t *testing.T) {
	layout := newLayout(t)
	dir := SourceDir(layout, "mdm/customer")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	require.NoError(t, Archive(layout, "mdm/customer", nil))
	assert.NoDirExists(t, dir)
	assert.DirExists(t, ArchivedDir(layout, "mdm/customer"))
}
