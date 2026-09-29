package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func relGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// pushedRepo 在 dir 建一个已推送到 bare 远端 origin 的仓库，返回远端目录。
func pushedRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	relGit(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", origin)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	relGit(t, dir, "init", "-q", "-b", "main")
	writeTree(t, dir, files)
	relGit(t, dir, "add", "-A")
	relGit(t, dir, "commit", "-q", "-m", "init")
	relGit(t, dir, "remote", "add", "origin", origin)
	relGit(t, dir, "push", "-q", "-u", "origin", "main")
	return origin
}

func compYAML(id, version string) string {
	return comp{ID: id, Version: version}.yamlText()
}

func TestReleaseCommandTagsAndPushes(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "api")
	origin := pushedRepo(t, repo, map[string]string{"component.yaml": compYAML("erp/api", "1.1.0")})

	r := runIn(t, repo, "release")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "erp/api@1.1.0")
	assert.Contains(t, r.stdout, "1.1.0")
	assert.Equal(t, "1.1.0", relGit(t, origin, "tag", "--list"))

	r = runIn(t, repo, "release")
	assert.NotEqual(t, clierr.ExitOK, r.code, "同一个版本不能发布两次")
	assert.Contains(t, r.stderr, "already released")
}

// 组件目录里同时有 brickkit.yaml（作者的本地工作台）：release 只认 component.yaml（提案 §16.1.1）。
func TestReleaseIgnoresWorkbenchBrickkitYaml(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "api")
	origin := pushedRepo(t, repo, map[string]string{
		"component.yaml": compYAML("erp/api", "1.0.0"),
		"brickkit.yaml":  "this is: [not valid for a project",
	})
	r := runIn(t, t.TempDir(), "release", "--path", repo)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "1.0.0", relGit(t, origin, "tag", "--list"))
}

// monorepo 子目录里的组件：tag 带命名空间，git 安装源按 source.path 把它读回来。
func TestReleaseSubdirIsReadBackByGitSource(t *testing.T) {
	mono := filepath.Join(t.TempDir(), "mono")
	origin := pushedRepo(t, mono, map[string]string{"svc/api/component.yaml": compYAML("erp/api", "1.0.0")})
	r := runIn(t, mono, "release", "--path", "svc/api")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "erp-api/1.0.0", relGit(t, origin, "tag", "--list"))

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n    source:\n      type: git\n      repo: file://" + filepath.ToSlash(origin) + "\n      path: svc/api\n",
		"deploy.yaml":   "target: docker\ncomponents:\n  - id: erp/api\n",
	})
	up := runWith(t, func(o *Options) { o.Engine = newFakeEngine() }, dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, up.code, up.stdout+up.stderr)
}

func TestReleaseLocalAndPathAreExclusive(t *testing.T) {
	r := runIn(t, t.TempDir(), "release", "--local", "--path", "x")
	assert.Equal(t, clierr.ExitUsage, r.code)
}

// localReleaseProject：项目的本地源 components/ 下三个组件，各自一个已推送的仓库。
func localReleaseProject(t *testing.T) (string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents: []\n",
		"deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	origins := map[string]string{}
	for _, id := range []string{"erp/a", "erp/b", "erp/c"} {
		origins[id] = pushedRepo(t, filepath.Join(dir, "components", filepath.FromSlash(id)),
			map[string]string{"component.yaml": compYAML(id, "1.0.0")})
	}
	return dir, origins
}

// 先把所有组件都检查完再打第一个 tag：有一个不干净，一个都不发布。
func TestReleaseLocalChecksAllBeforeTagging(t *testing.T) {
	dir, origins := localReleaseProject(t)
	writeTree(t, filepath.Join(dir, "components", "erp", "c"), map[string]string{"main.go": "package main\n"})

	r := runIn(t, dir, "release", "--local")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "erp/c@1.0.0 has uncommitted changes")
	for id, origin := range origins {
		assert.Empty(t, relGit(t, origin, "tag", "--list"), "%s 不该被发布", id)
	}
}

// 已经发布过（tag 就在当前提交上）的跳过，其余照常发布。
func TestReleaseLocalSkipsAlreadyReleased(t *testing.T) {
	dir, origins := localReleaseProject(t)
	require.Equal(t, clierr.ExitOK, runIn(t, filepath.Join(dir, "components", "erp", "a"), "release").code)

	r := runIn(t, dir, "release", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "erp/a@1.0.0 is already released")
	for _, id := range []string{"erp/b", "erp/c"} {
		assert.Equal(t, "1.0.0", relGit(t, origins[id], "tag", "--list"))
	}
	assert.Contains(t, r.stdout, "2 components released")
}

// 第二个推送失败：第一个的 tag 留在远端，第二个本地回滚，第三个从没动过（提案 §9.6.2）。
func TestReleaseLocalStopsAtFirstPushFailure(t *testing.T) {
	dir, origins := localReleaseProject(t)
	hook := filepath.Join(origins["erp/b"], "hooks", "pre-receive")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho no tags here >&2\nexit 1\n"), 0o755))

	r := runIn(t, dir, "release", "--local")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "no tags here")
	assert.Contains(t, r.stderr, "erp/a@1.0.0")
	assert.Contains(t, r.stderr, "erp/c@1.0.0")
	assert.Equal(t, "1.0.0", relGit(t, origins["erp/a"], "tag", "--list"))
	assert.Empty(t, relGit(t, filepath.Join(dir, "components", "erp", "b"), "tag", "--list"))
	assert.Empty(t, relGit(t, filepath.Join(dir, "components", "erp", "c"), "tag", "--list"))
}

func TestReleaseLocalNeedsAProject(t *testing.T) {
	r := runIn(t, t.TempDir(), "release", "--local")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "brickkit.yaml")
}

func TestReleaseLocalNothingToRelease(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents: []\n",
		"deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "components"), 0o755))
	r := runIn(t, dir, "release", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "no local-source components")
}

// 两个本地源指向同一个目录：一个组件只发布一次。
func TestReleaseLocalOverlappingSourcesReleaseOnce(t *testing.T) {
	dir, origins := localReleaseProject(t)
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\n  - name: again\n    type: local\n    path: ./components/\ncomponents: []\n",
	})
	r := runIn(t, dir, "release", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, 3, strings.Count(r.stdout, "Released erp/"))
	assert.NotContains(t, r.stdout, "✅ ✅")
	for _, origin := range origins {
		assert.Equal(t, "1.0.0", relGit(t, origin, "tag", "--list"))
	}
}

// 报错里的组件目录按使用者给的写法显示（相对当前目录），与其它命令一致——不是一长串绝对路径。
func TestReleaseShowsTheDirectoryRelative(t *testing.T) {
	project := t.TempDir()
	repo := filepath.Join(project, "svc", "api")
	pushedRepo(t, repo, map[string]string{"component.yaml": compYAML("erp/api", "1.1.0")})
	writeTree(t, repo, map[string]string{"main.go": "package main\n"})

	r := runIn(t, project, "release", "--path", "svc/api")
	require.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "svc/api")
	assert.NotContains(t, r.stderr, project, "不打印绝对路径")
}
