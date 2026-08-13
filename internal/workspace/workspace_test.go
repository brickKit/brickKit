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

	removed, err := RemoveSource(layout, "people/basic")
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NoDirExists(t, target)
	assert.NoDirExists(t, filepath.Join(layout.ComponentsDir(), "people"), "空的 scope 目录一并收走")
}

func TestRemoveSourceMissingIsNotAnError(t *testing.T) {
	layout := newLayout(t)

	removed, err := RemoveSource(layout, "people/basic")
	require.NoError(t, err)
	assert.False(t, removed)
}

// 同 scope 下还有别的组件时，scope 目录必须保留。
func TestRemoveSourceKeepsNonEmptyScope(t *testing.T) {
	layout := newLayout(t)
	require.NoError(t, os.MkdirAll(SourceDir(layout, "people/basic"), 0o755))
	require.NoError(t, os.MkdirAll(SourceDir(layout, "people/advanced"), 0o755))

	removed, err := RemoveSource(layout, "people/basic")
	require.NoError(t, err)
	assert.True(t, removed)
	assert.DirExists(t, filepath.Join(layout.ComponentsDir(), "people"))
}

// 组件 ID 不含 scope（异常输入）时不去删父目录。
func TestPruneEmptyParentIgnoresIDWithoutScope(t *testing.T) {
	layout := newLayout(t)
	require.NoError(t, os.MkdirAll(filepath.Join(layout.ComponentsDir(), "solo"), 0o755))

	pruneEmptyParent(layout, "solo")
	assert.DirExists(t, filepath.Join(layout.ComponentsDir(), "solo"))
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

	_, err := RemoveSource(layout, "people/basic")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "删除源码目录失败")
}

func TestFirstLine(t *testing.T) {
	assert.Equal(t, "fatal: repository not found",
		firstLine("\n fatal: repository not found\n更多\n", assert.AnError))
	assert.Equal(t, assert.AnError.Error(), firstLine("  \n\n", assert.AnError))
}
