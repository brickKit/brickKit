package cli

// 本文件测 pre-commit hook 的脚本与安装。
//
// hook 是唯一能在 git commit 那个时点上真的拦住人的东西，所以它自己绝不能
// 变成新的故障源：找不到 brickkit 要放行、别人的 hook 绝不覆盖、
// 多个项目要幂等追加。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/gitrepo"
)

func TestRenderHookIsPosixShAndListsProjects(t *testing.T) {
	script := renderHook("/opt/bin/brickkit", "v1.2.3", []hookProject{
		{Dir: ".", Config: "brickkit.yaml"},
		{Dir: "apps/erp", Config: "brickkit.prod.yaml"},
	})

	assert.True(t, strings.HasPrefix(script, "#!/bin/sh\n"), "必须是 sh，Windows 上 git 用自带 sh 跑 hook")
	assert.Contains(t, script, hookMarker+" v1.2.3")
	assert.Contains(t, script, "/opt/bin/brickkit")
	assert.Contains(t, script, ".|brickkit.yaml")
	assert.Contains(t, script, "apps/erp|brickkit.prod.yaml")
	assert.NotContains(t, script, "[[", "不用任何 bash 特性")
	assert.NotContains(t, script, "function ")
}

func TestRenderedHookRunsAndBlocks(t *testing.T) {
	dir := t.TempDir()
	// 假的 brickkit：--check 一律非零退出
	fake := filepath.Join(dir, "fake-brickkit")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o755))

	script := filepath.Join(dir, "pre-commit")
	require.NoError(t, os.WriteFile(script,
		[]byte(renderHook(fake, "v0", []hookProject{{Dir: ".", Config: "brickkit.yaml"}})), 0o755))

	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	assert.Error(t, err, "brickkit restore --check 非零时 hook 必须非零：%s", out)
}

func TestRenderedHookPassesWhenBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "pre-commit")
	require.NoError(t, os.WriteFile(script,
		[]byte(renderHook(filepath.Join(dir, "does-not-exist"), "v0",
			[]hookProject{{Dir: ".", Config: "brickkit.yaml"}})), 0o755))

	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH=/nonexistent")
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err, "找不到 brickkit 必须放行，否则新人 clone 下来就提交不了：%s", out)
	assert.Contains(t, string(out), "找不到 brickkit")
}

func TestRenderedHookSkipsMissingProjectDir(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-brickkit")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	script := filepath.Join(dir, "pre-commit")
	require.NoError(t, os.WriteFile(script,
		[]byte(renderHook(fake, "v0", []hookProject{{Dir: "gone", Config: "brickkit.yaml"}})), 0o755))

	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err, "项目目录已经不在了，不该因此堵死提交：%s", out)
}

func TestParseHookProjectsRoundTrips(t *testing.T) {
	want := []hookProject{
		{Dir: ".", Config: "brickkit.yaml"},
		{Dir: "apps/erp", Config: "brickkit.prod.yaml"},
	}
	assert.Equal(t, want, parseHookProjects(renderHook("/x/brickkit", "v1", want)))
}

func TestInstallHookWritesExecutableAndIsIdempotent(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := gitrepo.Open(dir)
	require.NoError(t, err)

	path, added, err := installHook(repo, hookProject{".", "brickkit.yaml"}, "/x/brickkit", "v1")
	require.NoError(t, err)
	assert.True(t, added)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o100, "hook 必须可执行")

	_, added, err = installHook(repo, hookProject{".", "brickkit.yaml"}, "/x/brickkit", "v1")
	require.NoError(t, err)
	assert.False(t, added, "同一个项目重复安装不该重复加")

	_, added, err = installHook(repo, hookProject{"apps/erp", "brickkit.yaml"}, "/x/brickkit", "v1")
	require.NoError(t, err)
	assert.True(t, added)

	script, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Len(t, parseHookProjects(string(script)), 2, "第二个项目要追加，不是覆盖")
}

func TestInstallHookNeverOverwritesForeignHook(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := gitrepo.Open(dir)
	require.NoError(t, err)
	hooks, err := repo.HooksDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	foreign := filepath.Join(hooks, "pre-commit")
	require.NoError(t, os.WriteFile(foreign, []byte("#!/bin/sh\n# husky\nexit 0\n"), 0o755))

	_, _, err = installHook(repo, hookProject{".", "brickkit.yaml"}, "/x/brickkit", "v1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "已存在")
	assert.Contains(t, err.Error(), "restore --check", "要把该插进去的那一行告诉他")

	got, readErr := os.ReadFile(foreign)
	require.NoError(t, readErr)
	assert.Contains(t, string(got), "# husky", "别人的 hook 一个字都不能改")
}

// newTestRepo 建一个空的 git 仓库。
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v：%s", args, out)
	}
	return dir
}
