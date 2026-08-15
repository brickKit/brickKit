package source

// 本文件是 P12「git 安装源指定分支 / tag / commit」的业务行为测试。
//
// 用真的 Git 仓库跑：ref 这件事的全部难点都在 git 自己的行为上
// （`clone --branch` 认分支也认 tag，**但不认 commit SHA**），Mock 掉就什么都验不到。

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// twoVersionRepo 造一个仓库：main 上是 1.0.0，分支 next 与 tag v2 上是 2.0.0。
// 返回仓库路径与 1.0.0 那次提交的 SHA。
func twoVersionRepo(t *testing.T) (repo, firstCommit string) {
	t.Helper()

	repo = t.TempDir()
	writeComponent(t, repo, componentSpec{ID: "demo/hello", Version: "1.0.0"})
	newGitRepo(t, repo)
	firstCommit = gitOutput(t, repo, "rev-parse", "HEAD")

	git(t, repo, "checkout", "-q", "-b", "next")
	writeComponent(t, repo, componentSpec{ID: "demo/hello", Version: "2.0.0"})
	git(t, repo, "add", "-A")
	git(t, repo, "-c", "user.email=t@e.com", "-c", "user.name=T", "commit", "-q", "-m", "2.0.0")
	git(t, repo, "tag", "v2")
	// 回到 main：默认分支上仍然是 1.0.0
	git(t, repo, "checkout", "-q", "main")

	return repo, firstCommit
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// versionAt 用指定的 ref 建源，取出 Manifest 里的版本号。
func versionAt(t *testing.T, url, ref, version string) (string, error) {
	t.Helper()

	c := newClient(t, newProject(t), cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url, Ref: ref,
	}), Options{})

	got, err := c.Manifest(context.Background(), "demo/hello", version)
	if err != nil {
		return "", err
	}
	return got.Manifest.Metadata.Version, nil
}

// 不写 ref 时拉默认分支——原来的行为不能变。
func TestGitSourceWithoutRefUsesDefaultBranch(t *testing.T) {
	repo, _ := twoVersionRepo(t)

	version, err := versionAt(t, repo, "", "1.0.0")

	require.NoError(t, err)
	assert.Equal(t, "1.0.0", version)
}

func TestGitSourceRefBranch(t *testing.T) {
	repo, _ := twoVersionRepo(t)

	version, err := versionAt(t, repo, "next", "2.0.0")

	require.NoError(t, err)
	assert.Equal(t, "2.0.0", version, "应当拉到 next 分支上的那一份")
}

func TestGitSourceRefTag(t *testing.T) {
	repo, _ := twoVersionRepo(t)

	version, err := versionAt(t, repo, "v2", "2.0.0")

	require.NoError(t, err)
	assert.Equal(t, "2.0.0", version)
}

// commit SHA：`git clone --branch <sha>` 是**不认**的，必须另走一条路。
//
// 这是 ref 唯一真正的难点——只测分支和 tag 会以为一次 clone 就够了。
func TestGitSourceRefCommit(t *testing.T) {
	repo, first := twoVersionRepo(t)

	version, err := versionAt(t, repo, first, "1.0.0")

	require.NoError(t, err)
	assert.Equal(t, "1.0.0", version, "应当停在那个提交上")
}

// ref 不存在时要说清楚是哪个源、哪个 ref。
func TestGitSourceUnknownRef(t *testing.T) {
	repo, _ := twoVersionRepo(t)

	_, err := versionAt(t, repo, "没有这个分支", "1.0.0")

	require.Error(t, err)
	assert.Equal(t, clierr.CodeCloneFailed, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "没有这个分支", "要指出是哪个 ref")
	assert.Contains(t, err.Error(), "my-git", "以及哪个安装源")
}
