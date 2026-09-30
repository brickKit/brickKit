package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/release"
)

func manifestYAML(id, version string) string {
	return "apiVersion: brickkit/v1\nkind: Component\nmetadata:\n  id: " + id + "\n  name: x\n  version: " + version +
		"\n  description: x\ndeployment:\n  type: container\n  image: registry.example.com/x:" + version +
		"\n  port: 8080\nhealthCheck:\n  type: http\n  path: /healthz\n"
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// repo 是一个工作副本，origin 是它的 bare 远端，当前分支已推送、设了上游。
type repo struct {
	t            *testing.T
	work, origin string
}

func newRepo(t *testing.T, files map[string]string) *repo {
	t.Helper()
	root := t.TempDir()
	r := &repo{t: t, work: filepath.Join(root, "work"), origin: filepath.Join(root, "origin.git")}
	git(t, root, "init", "-q", "--bare", "-b", "main", r.origin)
	git(t, root, "init", "-q", "-b", "main", r.work)
	r.write(files)
	r.commit("init")
	git(t, r.work, "remote", "add", "origin", r.origin)
	git(t, r.work, "push", "-q", "-u", "origin", "main")
	return r
}

func (r *repo) write(files map[string]string) {
	for name, content := range files {
		path := filepath.Join(r.work, name)
		require.NoError(r.t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(r.t, os.WriteFile(path, []byte(content), 0o644))
	}
}

func (r *repo) commit(msg string) {
	git(r.t, r.work, "add", "-A")
	git(r.t, r.work, "commit", "-q", "-m", msg)
}

func (r *repo) remoteTags() string { return git(r.t, r.origin, "tag", "--list") }
func (r *repo) localTags() string  { return git(r.t, r.work, "tag", "--list") }

func prepare(t *testing.T, dir string) *release.Target {
	t.Helper()
	target, err := release.Prepare(dir)
	require.NoError(t, err)
	return target
}

func TestReleaseTagsAndPushes(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.1.0")})
	target := prepare(t, r.work)
	assert.Equal(t, "1.1.0", target.Tag)
	state, err := target.Check()
	require.NoError(t, err)
	assert.Equal(t, release.Unreleased, state)
	require.NoError(t, target.Publish())
	assert.Equal(t, "1.1.0", r.remoteTags())
	assert.Equal(t, "1.1.0", r.localTags())
	assert.Equal(t, git(t, r.work, "rev-parse", "HEAD"), git(t, r.origin, "rev-parse", "1.1.0^{commit}"))
}

// 组件目录不是仓库根时，tag 带命名空间 <scope>-<name>/<版本>。
func TestReleaseSubdirUsesNamespacedTag(t *testing.T) {
	r := newRepo(t, map[string]string{"svc/api/component.yaml": manifestYAML("erp/api", "1.0.0"), "README.md": "x"})
	target := prepare(t, filepath.Join(r.work, "svc", "api"))
	assert.Equal(t, "erp-api/1.0.0", target.Tag)
	assert.Equal(t, "svc/api", target.Subpath)
	_, err := target.Check()
	require.NoError(t, err)
	require.NoError(t, target.Publish())
	assert.Equal(t, "erp-api/1.0.0", r.remoteTags())
}

func TestReleaseRefusesDirtyComponentDir(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	r.write(map[string]string{"main.go": "package main\n"})
	_, err := prepare(t, r.work).Check()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "main.go")
	assert.Empty(t, r.localTags())
}

// monorepo 里旁边组件没提交的改动与这个组件无关：tag 装的是提交，不是工作区。
func TestReleaseIgnoresSiblingEdits(t *testing.T) {
	r := newRepo(t, map[string]string{
		"svc/api/component.yaml": manifestYAML("erp/api", "1.0.0"),
		"svc/web/component.yaml": manifestYAML("erp/web", "1.0.0"),
	})
	r.write(map[string]string{"svc/web/main.go": "package main\n"})
	target := prepare(t, filepath.Join(r.work, "svc", "api"))
	_, err := target.Check()
	require.NoError(t, err)

	_, err = prepare(t, filepath.Join(r.work, "svc", "web")).Check()
	require.Error(t, err, "改动在自己目录里就要拦")
}

func TestReleaseRefusesUnpushedCommits(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	r.write(map[string]string{"main.go": "package main\n"})
	r.commit("local only")
	_, err := prepare(t, r.work).Check()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "git push")
}

func TestReleaseRefusesNoUpstream(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	git(t, r.work, "checkout", "-q", "-b", "feature")
	_, err := prepare(t, r.work).Check()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "upstream")
	assert.Contains(t, clierr.As(err).Format(), "git push -u origin feature", "分支名已知：提示直接给出能敲的命令")
}

func TestReleaseRefusesExistingTag(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	require.NoError(t, prepare(t, r.work).Publish())

	state, err := prepare(t, r.work).Check()
	require.NoError(t, err)
	assert.Equal(t, release.Released, state, "tag 已在当前提交上：已发布过")

	r.write(map[string]string{"main.go": "package main\n"})
	r.commit("more")
	git(t, r.work, "push", "-q")
	_, err = prepare(t, r.work).Check()
	require.Error(t, err, "tag 在别的提交上：改了代码却没改版本号")
	assert.Contains(t, clierr.As(err).Format(), "metadata.version")
}

// 只有远端有这个 tag（别人发布过）也要拦下，不能等 push 时才撞上。
func TestReleaseSeesRemoteOnlyTag(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	git(t, r.work, "tag", "1.0.0")
	git(t, r.work, "push", "-q", "origin", "1.0.0")
	git(t, r.work, "tag", "-d", "1.0.0")
	r.write(map[string]string{"main.go": "package main\n"})
	r.commit("more")
	git(t, r.work, "push", "-q")

	_, err := prepare(t, r.work).Check()
	require.Error(t, err)
}

// 推送失败：本地 tag 回滚，就像从没执行过（发布是原子的）。
func TestReleaseRollsBackOnPushFailure(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	hook := filepath.Join(r.origin, "hooks", "pre-receive")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho rejected by policy >&2\nexit 1\n"), 0o755))

	err := prepare(t, r.work).Publish()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "rejected by policy")
	assert.Empty(t, r.localTags(), "本地 tag 已回滚")
	assert.Empty(t, r.remoteTags())
}

func TestReleaseInvalidManifestStopsBeforeTag(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": "metadata:\n  id: erp/api\n  version: 1.0\n"})
	_, err := release.Prepare(r.work)
	require.Error(t, err)
	assert.Empty(t, r.localTags())
}

func TestReleaseNotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.yaml"), []byte(manifestYAML("erp/api", "1.0.0")), 0o644))
	_, err := release.Prepare(dir)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "Git repository")
}

func TestReleaseMissingManifest(t *testing.T) {
	_, err := release.Prepare(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "component.yaml")
}

// 只在本地有的 tag 不是"已发布"：消费方取不到它。要么推上去、要么删掉——不能让人去改版本号。
func TestReleaseLocalOnlyTagIsNotReleased(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	git(t, r.work, "tag", "1.0.0")
	_, err := prepare(t, r.work).Check()
	require.Error(t, err)
	msg := clierr.As(err).Format()
	assert.Contains(t, msg, "only locally")
	assert.Contains(t, msg, "git push origin 1.0.0")
	assert.NotContains(t, msg, "metadata.version")
}

// 远端有、本地没有（别的机器发布的，还没 fetch 下来），而且就在当前提交上：已发布。
func TestReleaseRemoteTagAtHeadIsReleased(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	git(t, r.work, "tag", "1.0.0")
	git(t, r.work, "push", "-q", "origin", "1.0.0")
	git(t, r.work, "tag", "-d", "1.0.0")
	state, err := prepare(t, r.work).Check()
	require.NoError(t, err)
	assert.Equal(t, release.Released, state)
}

func TestReleasePushFailureNamesTheComponent(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	require.NoError(t, os.WriteFile(filepath.Join(r.origin, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	err := prepare(t, r.work).Publish()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Message, "erp/api@1.0.0")
}

// 游离 HEAD（CI 的检出常常是）：说清要在分支上发布，而不只是"没有上游"。
func TestReleaseDetachedHeadSaysUseABranch(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	git(t, r.work, "checkout", "-q", "--detach")
	_, err := prepare(t, r.work).Check()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "branch")
	assert.Contains(t, clierr.As(err).Format(), "git checkout")
}
