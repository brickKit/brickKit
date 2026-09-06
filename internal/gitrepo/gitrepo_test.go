package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRepo 建一个真的 git 仓库（不含提交）。
//
// 用真仓库而不是打桩：这个包全部行为都是 git 的行为，桩只会证明桩自己对。
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--quiet")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v：%s", args, out)
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestOpenFindsRootFromSubdir(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "components/people/basic/main.go", "package main\n")

	r, err := Open(filepath.Join(dir, "components", "people"))
	require.NoError(t, err)

	rel, ok := r.Rel(filepath.Join(dir, "components", "people", "basic"))
	assert.True(t, ok)
	assert.Equal(t, "components/people/basic", rel, "必须是 / 分隔的相对路径")
}

func TestOpenRejectsNonRepo(t *testing.T) {
	_, err := Open(t.TempDir())
	assert.ErrorIs(t, err, ErrNotRepo)
}

func TestRelRejectsPathOutsideRepo(t *testing.T) {
	r, err := Open(newRepo(t))
	require.NoError(t, err)

	_, ok := r.Rel(filepath.Join(t.TempDir(), "brickkit.yaml"))
	assert.False(t, ok, "仓库外的路径必须报 false，而不是一串 ../..")
}

func TestHasHEADAndUnmerged(t *testing.T) {
	dir := newRepo(t)
	r, err := Open(dir)
	require.NoError(t, err)
	assert.False(t, r.HasHEAD(), "空仓库没有 HEAD")
	assert.False(t, r.Unmerged())

	write(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "init")
	assert.True(t, r.HasHEAD())
}

// Fix 2（复审）：TestHasHEADAndUnmerged 从前只在干净仓库里断言过
// assert.False(t, r.Unmerged())——一个永远返回 false 的实现能原样通过它。
// 而 Unmerged 判错会静默关掉 runRestoreCheck / restoreBaseline 的冲突守卫，
// git show :<path> 在真冲突里会直接 fatal。这里造一次真的合并冲突，
// 断言 Unmerged() 在冲突中必须是 true。
func TestUnmergedDuringRealConflict(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "k.txt", "base\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "base")

	git(t, dir, "checkout", "--quiet", "-b", "other")
	write(t, dir, "k.txt", "v1\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "v1")

	git(t, dir, "checkout", "--quiet", "-")
	write(t, dir, "k.txt", "v2\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "v2")

	// 制造冲突（合并会失败，这里刻意忽略返回值）
	cmd := exec.Command("git", "merge", "other")
	cmd.Dir = dir
	_ = cmd.Run()

	r, err := Open(dir)
	require.NoError(t, err)
	assert.True(t, r.Unmerged(), "冲突中 index 里必须有未合并条目")
}

func TestIndexBlobReadsStagedVersionNotHead(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "brickkit.yaml", "project: old\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "init")

	write(t, dir, "brickkit.yaml", "project: staged\n")
	git(t, dir, "add", "brickkit.yaml")
	write(t, dir, "brickkit.yaml", "project: worktree\n")

	r, err := Open(dir)
	require.NoError(t, err)

	staged, err := r.IndexBlob("brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "project: staged\n", string(staged), "index 版 = 即将提交的那一份")

	head, err := r.HeadBlob("brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "project: old\n", string(head))
}

func TestIndexEntriesSeesGitlinkWithoutTrailingSlash(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "components/people/basic/main.go", "package main\n")

	nested := filepath.Join(dir, "components", "erp", "backend")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	git(t, nested, "init", "--quiet")
	git(t, nested, "config", "user.email", "t@example.com")
	git(t, nested, "config", "user.name", "t")
	write(t, nested, "m.go", "package main\n")
	git(t, nested, "add", "-A")
	git(t, nested, "commit", "--quiet", "-m", "c1")

	git(t, dir, "add", "components")

	r, err := Open(dir)
	require.NoError(t, err)
	entries, err := r.IndexEntries("components")
	require.NoError(t, err)

	var gitlinks, files []string
	for _, e := range entries {
		if e.IsGitlink() {
			gitlinks = append(gitlinks, e.Path)
			continue
		}
		files = append(files, e.Path)
	}
	assert.Equal(t, []string{"components/erp/backend"}, gitlinks,
		"嵌套仓库是一条 160000 记录，路径没有尾斜杠")
	assert.Equal(t, []string{"components/people/basic/main.go"}, files)
}

func TestStagedUnderSeesStagedChangeOnly(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "components/people/basic/main.go", "package main\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "init")

	r, err := Open(dir)
	require.NoError(t, err)
	assert.False(t, r.StagedUnder("components"), "干净时不该有已暂存改动")

	write(t, dir, "components/people/basic/main.go", "package main // 改了\n")
	assert.False(t, r.StagedUnder("components"), "只改了工作区、没 add，不算已暂存")

	git(t, dir, "add", "components")
	assert.True(t, r.StagedUnder("components"))
}

func TestHooksDirFollowsCoreHooksPath(t *testing.T) {
	dir := newRepo(t)
	r, err := Open(dir)
	require.NoError(t, err)

	got, err := r.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(r.Root(), ".git", "hooks"), got)

	git(t, dir, "config", "core.hooksPath", ".githooks")
	got, err = r.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(r.Root(), ".githooks"), got,
		"husky / lefthook 会设 core.hooksPath，装错地方等于 hook 永不运行")
}

func TestHooksDirHonorsGloballySetHooksPath(t *testing.T) {
	dir := newRepo(t)
	// 把 core.hooksPath 设在**全局**配置里——husky / lefthook 就是这么设的。
	// 这是 HooksDir 唯一一处不屏蔽全局配置的理由：屏蔽掉就会装到 git 根本不看的
	// .git/hooks，hook 装了却永不运行，而且没有任何迹象。
	globalCfg := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(globalCfg,
		[]byte("[core]\n\thooksPath = .globalhooks\n"), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfg)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	r, err := Open(dir)
	require.NoError(t, err)

	got, err := r.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(r.Root(), ".globalhooks"), got,
		"全局设的 core.hooksPath 必须认——屏蔽掉它就会装到 git 不看的地方")
}

func TestStagedUnderBeforeFirstCommit(t *testing.T) {
	dir := newRepo(t)
	r, err := Open(dir)
	require.NoError(t, err)
	assert.False(t, r.StagedUnder("components"), "空仓库、什么都没 add：没有已暂存的东西")

	write(t, dir, "components/people/basic/main.go", "package main\n")
	git(t, dir, "add", "components")
	assert.True(t, r.StagedUnder("components"),
		"还没有任何提交时，index 里的每一条都算已暂存——diff --cached 在无 HEAD 时用不了")
}

func TestSubmodulesEmptyWithoutGitmodulesFile(t *testing.T) {
	r, err := Open(newRepo(t))
	require.NoError(t, err)
	assert.Empty(t, r.Submodules(), "没有 .gitmodules 时必须是空 map，不是 nil 之外的别的东西")
}

func TestSubmodulesParsesRegisteredEntry(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "config", "-f", ".gitmodules", "submodule.components/mdm/customer.path", "components/mdm/customer")
	git(t, dir, "config", "-f", ".gitmodules", "submodule.components/mdm/customer.url", "git@github.com:brickKit/mdm-customer.git")

	r, err := Open(dir)
	require.NoError(t, err)

	got := r.Submodules()
	require.Contains(t, got, "components/mdm/customer")
	assert.Equal(t, "git@github.com:brickKit/mdm-customer.git", got["components/mdm/customer"].URL)
	assert.Equal(t, "components/mdm/customer", got["components/mdm/customer"].Path)
}

func TestSubmodulesParsesMultipleEntries(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "config", "-f", ".gitmodules", "submodule.tools/be-ops.path", "tools/be-ops")
	git(t, dir, "config", "-f", ".gitmodules", "submodule.tools/be-ops.url", "git@github.com:brickKit/be-ops.git")
	git(t, dir, "config", "-f", ".gitmodules", "submodule.components/mdm/customer.path", "components/mdm/customer")
	git(t, dir, "config", "-f", ".gitmodules", "submodule.components/mdm/customer.url", "git@github.com:brickKit/mdm-customer.git")

	r, err := Open(dir)
	require.NoError(t, err)

	got := r.Submodules()
	assert.Len(t, got, 2)
	assert.Contains(t, got, "tools/be-ops")
	assert.Contains(t, got, "components/mdm/customer")
}

func TestSubmodulesKeyedByRegisteredPathNotSectionName(t *testing.T) {
	dir := newRepo(t)
	// 子模块的 section 名可以和 path 不一样（比如改名之后没跟着改 section）；
	// 判据要靠 path 字段本身，不是 section 名。
	git(t, dir, "config", "-f", ".gitmodules", "submodule.old-name.path", "components/mdm/customer")
	git(t, dir, "config", "-f", ".gitmodules", "submodule.old-name.url", "git@github.com:brickKit/mdm-customer.git")

	r, err := Open(dir)
	require.NoError(t, err)

	got := r.Submodules()
	assert.Contains(t, got, "components/mdm/customer")
}
