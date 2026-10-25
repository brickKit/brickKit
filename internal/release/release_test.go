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

// publish 是只关心成败的 Publish。
func publish(target *release.Target) error {
	_, err := target.Publish()
	return err
}

func TestReleaseTagsAndPushes(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.1.0")})
	target := prepare(t, r.work)
	assert.Equal(t, "1.1.0", target.Tag)
	state, err := target.Check()
	require.NoError(t, err)
	assert.Equal(t, release.Unreleased, state)
	require.NoError(t, publish(target))
	assert.Equal(t, "1.1.0", r.remoteTags())
	assert.Equal(t, "1.1.0", r.localTags())
	assert.Equal(t, git(t, r.work, "rev-parse", "HEAD"), git(t, r.origin, "rev-parse", "1.1.0^{commit}"))
}

// 带说明发版：说明原样写进带注释的 tag（# 开头的 Markdown 标题不被当成注释删掉），推上去的就是它。
func TestReleaseWithNotesWritesThemIntoTheTag(t *testing.T) {
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.1.0")})
	target := prepare(t, r.work)
	target.Notes = "## Added\n\n- an export endpoint\n\n# Breaking\n"
	_, err := target.Check()
	require.NoError(t, err)
	require.NoError(t, publish(target))

	assert.Equal(t, "tag", git(t, r.origin, "cat-file", "-t", "1.1.0"))
	assert.Equal(t, "## Added\n\n- an export endpoint\n\n# Breaking", git(t, r.origin, "for-each-ref", "refs/tags/1.1.0", "--format=%(contents)"))
}

// 不写说明照样发：和从前一样是轻量 tag。
func TestReleaseWithoutNotesStaysALightweightTag(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.1.0")})
	target := prepare(t, r.work)
	target.Notes = "  \n"
	_, err := target.Check()
	require.NoError(t, err)
	require.NoError(t, publish(target))
	assert.Equal(t, "commit", git(t, r.origin, "cat-file", "-t", "1.1.0"))
}

// 组件目录不是仓库根时，tag 带命名空间 <scope>-<name>/<版本>。
func TestReleaseSubdirUsesNamespacedTag(t *testing.T) {
	r := newRepo(t, map[string]string{"svc/api/component.yaml": manifestYAML("erp/api", "1.0.0"), "README.md": "x"})
	target := prepare(t, filepath.Join(r.work, "svc", "api"))
	assert.Equal(t, "erp-api/1.0.0", target.Tag)
	assert.Equal(t, "svc/api", target.Subpath)
	_, err := target.Check()
	require.NoError(t, err)
	require.NoError(t, publish(target))
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

// 组件自己的检查（release.checks）在 Check 之后才跑，它可能改写文件——打 tag 之前再核对一次：
// 检查时用的文件必须就是要打 tag 的那个提交。
func TestVerifyUnchangedRefusesFilesChangedSinceCheck(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0"), "go.mod": "module x\n"})
	target := prepare(t, r.work)
	_, err := target.Check()
	require.NoError(t, err)
	require.NoError(t, target.VerifyUnchanged(), "什么都没动")

	r.write(map[string]string{"go.mod": "module x\n\nrequire y v1.0.0\n"})
	err = target.VerifyUnchanged()
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeReleaseBlocked, e.Code)
	assert.Contains(t, e.Format(), "the release checks left uncommitted changes in erp/api@1.0.0")
	assert.Contains(t, e.Format(), "M go.mod")
}

// 检查命令自己提交了东西：工作区是干净的，但检查过的提交已经不是当前提交。
func TestVerifyUnchangedRefusesAMovedHead(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0")})
	target := prepare(t, r.work)
	_, err := target.Check()
	require.NoError(t, err)
	checked := git(t, r.work, "rev-parse", "--short", "HEAD")

	r.write(map[string]string{"generated.go": "package main\n"})
	r.commit("generated by a check")
	err = target.VerifyUnchanged()
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeReleaseBlocked, e.Code)
	assert.Contains(t, e.Format(), "the current commit changed while the release checks of erp/api@1.0.0 ran")
	assert.Contains(t, e.Format(), checked)
	assert.Contains(t, e.Format(), git(t, r.work, "rev-parse", "--short", "HEAD"))
}

// 子目录组件只看自己的目录，和 Check 一样。
func TestVerifyUnchangedIgnoresSiblingEdits(t *testing.T) {
	r := newRepo(t, map[string]string{
		"svc/api/component.yaml": manifestYAML("erp/api", "1.0.0"),
		"svc/web/component.yaml": manifestYAML("erp/web", "1.0.0"),
	})
	target := prepare(t, filepath.Join(r.work, "svc", "api"))
	_, err := target.Check()
	require.NoError(t, err)
	r.write(map[string]string{"svc/web/main.go": "package main\n"})
	require.NoError(t, target.VerifyUnchanged())
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
	require.NoError(t, publish(prepare(t, r.work)))

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

	err := publish(prepare(t, r.work))
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
	err := publish(prepare(t, r.work))
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

// goManifestYAML 是写了 release.goModule 的组件。
func goManifestYAML(id, version string) string {
	return manifestYAML(id, version) + "release:\n  goModule: true\n"
}

// release.goModule：同一个提交上再打 Go 的 v 标签，两个一起推上去，带同一份说明。
func TestReleaseGoModuleTagsBothWithTheSameNotes(t *testing.T) {
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	r := newRepo(t, map[string]string{
		"component.yaml": goManifestYAML("erp/api", "3.0.7"),
		"go.mod":         "module example.com/erp/api/v3\n\ngo 1.22\n",
	})
	target := prepare(t, r.work)
	assert.Equal(t, "v3.0.7", target.GoTag)
	assert.Equal(t, []string{"3.0.7", "v3.0.7"}, target.Tags())
	target.Notes = "# 3.0.7\n\n- fixed the export\n"
	_, err := target.Check()
	require.NoError(t, err)
	pushed, err := target.Publish()
	require.NoError(t, err)
	assert.Equal(t, []string{"3.0.7", "v3.0.7"}, pushed)

	assert.Equal(t, "3.0.7\nv3.0.7", r.remoteTags())
	head := git(t, r.work, "rev-parse", "HEAD")
	for _, tag := range pushed {
		assert.Equal(t, head, git(t, r.origin, "rev-parse", tag+"^{commit}"))
		assert.Equal(t, "# 3.0.7\n\n- fixed the export", git(t, r.origin, "for-each-ref", "refs/tags/"+tag, "--format=%(contents)"),
			"两个 tag 的说明一字不差，# 开头的标题也在")
	}
	state, err := prepare(t, r.work).Check()
	require.NoError(t, err)
	assert.Equal(t, release.Released, state)
}

// 子目录里的模块：Go 的 tag 按模块所在的目录起名（svc/api/v1.2.0），平台的按组件 ID（erp-api/1.2.0）。
func TestReleaseGoModuleInASubdirectoryIsTaggedByItsPath(t *testing.T) {
	r := newRepo(t, map[string]string{
		"svc/api/component.yaml": goManifestYAML("erp/api", "1.2.0"),
		"svc/api/go.mod":         "module example.com/mono/svc/api\n",
	})
	target := prepare(t, filepath.Join(r.work, "svc", "api"))
	assert.Equal(t, "svc/api/v1.2.0", target.GoTag)
	require.NoError(t, publish(target))
	assert.Equal(t, "erp-api/1.2.0\nsvc/api/v1.2.0", r.remoteTags())
}

// 没写 release.goModule：有 go.mod 也只打平台的 tag。
func TestReleaseWithoutGoModuleTagsOnlyThePlatformTag(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "2.0.0"), "go.mod": "module example.com/api\n"})
	target := prepare(t, r.work)
	assert.Empty(t, target.GoTag)
	require.NoError(t, publish(target))
	assert.Equal(t, "2.0.0", r.remoteTags())
}

// 模块路径与主版本对不上：Go 不认这个 tag，而推出去就收不回来——Prepare 就停下，一个 tag 都不打。
func TestReleaseGoModuleRefusesAPathWithoutTheMajorSuffix(t *testing.T) {
	r := newRepo(t, map[string]string{
		"component.yaml": goManifestYAML("erp/api", "3.0.0"),
		"go.mod":         "module example.com/erp/api\n",
	})
	_, err := release.Prepare(r.work)
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
	assert.False(t, e.Warning)
	msg := e.Format()
	assert.Contains(t, msg, "Go won't accept the tag v3.0.0 for erp/api")
	assert.Contains(t, msg, "/v3")
	assert.Contains(t, msg, "example.com/erp/api")
	assert.Empty(t, r.localTags())
}

func TestReleaseGoModuleNeedsAGoMod(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": goManifestYAML("erp/api", "1.0.0")})
	_, err := release.Prepare(r.work)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "there is no go.mod in its directory")
}

// 平台的 tag 已经在当前提交上、只缺 Go 的：release 只补上 Go 的，不动已有的。
// 写 goModule 之前发布的版本补不了：加字段是新提交，平台的 tag 在旧提交上，照"版本不可变"拦下。
func TestReleaseGoModuleAddsTheMissingGoTag(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": manifestYAML("erp/api", "1.0.0"), "go.mod": "module example.com/api\n"})
	require.NoError(t, publish(prepare(t, r.work)))
	r.write(map[string]string{"component.yaml": goManifestYAML("erp/api", "1.0.0")})
	r.commit("tag as a go module")
	git(t, r.work, "push", "-q")
	// 平台的 tag 在上一个提交上：版本不可变，这里要拦
	_, err := prepare(t, r.work).Check()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "the tag 1.0.0 already exists on another commit")

	// 同一个提交上：只缺 Go 的
	r2 := newRepo(t, map[string]string{"component.yaml": goManifestYAML("erp/api", "1.0.0"), "go.mod": "module example.com/api\n"})
	git(t, r2.work, "tag", "1.0.0")
	git(t, r2.work, "push", "-q", "origin", "1.0.0")
	target := prepare(t, r2.work)
	state, err := target.Check()
	require.NoError(t, err)
	assert.Equal(t, release.Unreleased, state)
	pushed, err := target.Publish()
	require.NoError(t, err)
	assert.Equal(t, []string{"v1.0.0"}, pushed)
	assert.Equal(t, "1.0.0\nv1.0.0", r2.remoteTags())
}

// Go 的 tag 在别的提交上（手打错了地方）：和平台的 tag 一样拦下。
func TestReleaseGoModuleRefusesAGoTagElsewhere(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": goManifestYAML("erp/api", "1.0.0"), "go.mod": "module example.com/api\n"})
	git(t, r.work, "tag", "v1.0.0")
	git(t, r.work, "push", "-q", "origin", "v1.0.0")
	r.write(map[string]string{"main.go": "package main\n"})
	r.commit("more")
	git(t, r.work, "push", "-q")
	_, err := prepare(t, r.work).Check()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "the tag v1.0.0 already exists on another commit")
	assert.Equal(t, "v1.0.0", r.localTags(), "没有打平台的 tag")
}

// 推送失败：两个本地 tag 都回滚，远端一个都没有（--atomic）。
func TestReleaseGoModuleRollsBackBothTags(t *testing.T) {
	r := newRepo(t, map[string]string{"component.yaml": goManifestYAML("erp/api", "1.0.0"), "go.mod": "module example.com/api\n"})
	// update 钩子按引用逐个判断、只拒绝 Go 的 tag：不是原子推送的话，平台的那个会单独上去
	hook := "#!/bin/sh\ncase \"$1\" in refs/tags/v*) echo no go tags >&2; exit 1;; esac\n"
	require.NoError(t, os.WriteFile(filepath.Join(r.origin, "hooks", "update"), []byte(hook), 0o755))
	err := publish(prepare(t, r.work))
	require.Error(t, err)
	msg := clierr.As(err).Format()
	assert.Contains(t, msg, "pushing the tags 1.0.0, v1.0.0 of erp/api@1.0.0")
	assert.Contains(t, msg, "the local tags 1.0.0, v1.0.0 were deleted again")
	assert.Empty(t, r.localTags())
	assert.Empty(t, r.remoteTags())
}
