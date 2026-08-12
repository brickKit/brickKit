package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func newTestLayout(t *testing.T) Layout {
	t.Helper()
	return NewLayout(t.TempDir(), DefaultConfigFile)
}

func TestInitProjectCreatesEverything(t *testing.T) {
	l := newTestLayout(t)

	result, err := InitProject(l, "my-project")
	require.NoError(t, err)

	assert.Equal(t, "my-project", result.ProjectName)
	assert.Equal(t, DefaultConfigFile, result.ConfigName)
	assert.Equal(t, l.InitialBackupPath(), result.BackupPath)
	assert.True(t, result.GitignoreUpdated)

	for _, dir := range l.ManagedDirs() {
		info, err := os.Stat(dir)
		require.NoError(t, err, dir)
		assert.True(t, info.IsDir(), dir)
	}

	config, err := os.ReadFile(l.ConfigPath())
	require.NoError(t, err)
	assert.Equal(t, string(Skeleton("my-project", DefaultConfigFile)), string(config))

	backup, err := os.ReadFile(l.InitialBackupPath())
	require.NoError(t, err)
	assert.Equal(t, config, backup)
}

func TestInitProjectFileMode(t *testing.T) {
	l := newTestLayout(t)
	_, err := InitProject(l, "my-project")
	require.NoError(t, err)

	info, err := os.Stat(l.ConfigPath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestInitProjectRejectsInvalidName(t *testing.T) {
	l := newTestLayout(t)

	_, err := InitProject(l, "My Project")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)

	assert.NoFileExists(t, l.ConfigPath())
	assert.NoDirExists(t, l.BrickkitDir())
}

func TestInitProjectRejectsExistingConfig(t *testing.T) {
	l := newTestLayout(t)
	require.NoError(t, os.WriteFile(l.ConfigPath(), []byte("project: old\n"), 0o644))

	_, err := InitProject(l, "my-project")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeProjectExists, e.Code)
	assert.Contains(t, e.Format(), "brickkit.yaml")
	assert.Contains(t, e.Format(), "brickkit reset")
}

// 只有 .brickkit 目录（配置被误删）也算已初始化，不能悄悄覆盖缓存与备份。
func TestInitProjectRejectsExistingBrickkitDir(t *testing.T) {
	l := newTestLayout(t)
	require.NoError(t, os.MkdirAll(l.BrickkitDir(), 0o755))

	_, err := InitProject(l, "my-project")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeProjectExists, e.Code)
	assert.Contains(t, e.Format(), ".brickkit/")
}

func TestInitProjectTwiceFails(t *testing.T) {
	l := newTestLayout(t)
	_, err := InitProject(l, "my-project")
	require.NoError(t, err)

	_, err = InitProject(l, "another")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeProjectExists, clierr.As(err).Code)

	content, err := os.ReadFile(l.ConfigPath())
	require.NoError(t, err)
	assert.Contains(t, string(content), "project: my-project", "已有配置不能被覆盖")
}

// 目录不可写时给出可读的 IO 错误（而不是裸 os 错误）。
func TestInitProjectReportsIOError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户会绕过目录权限")
	}
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o500))
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	_, err := InitProject(NewLayout(root, DefaultConfigFile), "my-project")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeInternal, e.Code)
	assert.Contains(t, e.Format(), "失败")
	assert.Contains(t, e.Format(), "检查目录权限与磁盘空间")
}

// --config 指向不可写位置时，报错但不留下半个配置文件。
func TestInitProjectReportsConfigWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户会绕过目录权限")
	}
	root := t.TempDir()
	readonly := filepath.Join(root, "readonly")
	require.NoError(t, os.MkdirAll(readonly, 0o500))
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o700) })

	l := NewLayout(root, filepath.Join(readonly, DefaultConfigFile))
	_, err := InitProject(l, "my-project")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeInternal, e.Code)
	assert.Contains(t, e.Format(), "写入文件失败")
	assert.NoFileExists(t, l.ConfigPath())
}

// .gitignore 被占用成目录时，init 报错（而不是 panic 或静默忽略）。
func TestInitProjectReportsGitignoreError(t *testing.T) {
	l := newTestLayout(t)
	require.NoError(t, os.MkdirAll(l.GitignorePath(), 0o755))

	_, err := InitProject(l, "my-project")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInternal, clierr.As(err).Code)
}

func TestEnsureGitignoreReportsWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户会绕过目录权限")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := EnsureGitignore(filepath.Join(dir, FileGitignore))
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInternal, clierr.As(err).Code)
}

func TestEnsureGitignoreCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileGitignore)

	updated, err := EnsureGitignore(path)
	require.NoError(t, err)
	assert.True(t, updated)

	content := readTestFile(t, path)
	// 003 §11 的全部条目
	for _, want := range []string{
		".brickkit/generated/", ".brickkit/backup/", ".brickkit/credentials",
		".env", "components/",
		"# .brickkit/artifacts/", "# .brickkit/manifests/",
	} {
		assert.Contains(t, content, want)
	}
	assert.True(t, strings.HasSuffix(content, "\n"))
}

func TestEnsureGitignoreIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileGitignore)

	updated, err := EnsureGitignore(path)
	require.NoError(t, err)
	require.True(t, updated)
	first := readTestFile(t, path)

	updated, err = EnsureGitignore(path)
	require.NoError(t, err)
	assert.False(t, updated, "第二次不应有任何写入")
	assert.Equal(t, first, readTestFile(t, path))
}

// 已有部分条目时只追加缺失的，并且整段都已存在时连注释一起跳过（不留孤立注释）。
func TestEnsureGitignoreAppendsOnlyMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileGitignore)
	require.NoError(t, os.WriteFile(path, []byte("*.log\ncomponents/\n.env\n"), 0o644))

	updated, err := EnsureGitignore(path)
	require.NoError(t, err)
	require.True(t, updated)

	content := readTestFile(t, path)
	assert.Contains(t, content, "*.log", "原有内容保留")
	assert.Contains(t, content, ".brickkit/credentials", "缺失条目被追加")
	assert.Equal(t, 1, countLines(content, "components/"))
	assert.Equal(t, 1, countLines(content, ".env"))
	assert.NotContains(t, content, "# 组件源码目录", "整段已存在时不应留下孤立注释")
	assert.NotContains(t, content, "# 环境变量文件")
}

// 已有文件没有结尾换行时，追加的内容不能和最后一行粘在一起。
func TestEnsureGitignoreHandlesMissingTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileGitignore)
	require.NoError(t, os.WriteFile(path, []byte("*.log"), 0o644))

	_, err := EnsureGitignore(path)
	require.NoError(t, err)

	content := readTestFile(t, path)
	assert.Equal(t, 1, countLines(content, "*.log"))
	assert.NotContains(t, content, "*.log#")
}

func TestEnsureGitignoreReportsReadError(t *testing.T) {
	// 传目录路径 → 读取失败（既不是 NotExist，也不能当空文件处理）
	dir := t.TempDir()
	_, err := EnsureGitignore(dir)
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInternal, clierr.As(err).Code)
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func countLines(content, want string) int {
	var n int
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == want {
			n++
		}
	}
	return n
}
