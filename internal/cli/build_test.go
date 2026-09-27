package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func (g *gitOrgProject) runImages(dir string, images *fakeImages, args ...string) result {
	g.t.Helper()
	return runWith(g.t, func(o *Options) {
		o.Engine = newFakeEngine()
		o.Images = images
		o.RepoCacheDir = g.cache
	}, dir, args...)
}

// 提案 §11.2：本地源的组件、没有 image 的组件从源码构建；有 image 的 git 组件是拉的，不构建。
// tag 与 metadata.version 一致（§9.10.4）。
func TestBuildBuildsLocalAndBuildOnlyVersions(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(buildOnly(comp{ID: "erp/builtonly", Version: "1.0.0", Port: 8082}), map[string]string{"Dockerfile": "FROM scratch\n"})
	g.release(comp{ID: "erp/pulled", Version: "2.0.0", Port: 8083})
	dir := g.project()
	g.mustRun(dir, "add", "erp/builtonly@1.0.0")
	g.mustRun(dir, "add", "erp/pulled@2.0.0")
	writeTree(t, dir, map[string]string{
		"components/erp/local/component.yaml": comp{ID: "erp/local", Version: "1.0.0", Port: 8081}.yamlText(),
		"components/erp/local/Dockerfile":     "FROM scratch\n",
	})
	editFile(t, filepath.Join(dir, "brickkit.yaml"), "sources:\n", "sources:\n  - name: dev\n    type: local\n    path: ./components\n")
	g.mustRun(dir, "add", "erp/local@1.0.0")

	images := newFakeImages()
	r := g.runImages(dir, images, "build")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.ElementsMatch(t, []string{"erp-builtonly:1.0.0", "registry.example.com/erp-local:1.0.0"}, images.built())
	for _, b := range images.builds {
		assert.Equal(t, filepath.Join(b.Context, "Dockerfile"), b.Dockerfile)
		assert.True(t, images.hadDockerfile[b.Tag], "构建时 Dockerfile 在上下文里：%s", b.Tag)
		assert.NotEmpty(t, b.Labels["io.brickkit.component"])
	}
}

// buildOnly 去掉 image，只留 build：镜像名由组件 ID 与版本推出来。
func buildOnly(c comp) comp {
	c.Image = "-"
	return c
}

func TestBuildSkipsExistingUnlessForce(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(buildOnly(comp{ID: "erp/api", Version: "1.0.0"}), map[string]string{"Dockerfile": "FROM scratch\n"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")

	images := newFakeImages("erp-api:1.0.0")
	r := g.runImages(dir, images, "build")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Empty(t, images.built())
	assert.Contains(t, r.stdout, "--force")

	r = g.runImages(dir, images, "build", "erp/api", "--force")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"erp-api:1.0.0"}, images.built())
}

// 附录 A24：外壳镜像记下构建时编进去的成员版本。
func TestBuildShellRecordsMemberVersions(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "2.0.0", Port: 8082})
	g.release(buildOnly(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/b@2.0.0", "erp/a@1.0.0"}}),
		map[string]string{"Dockerfile": "FROM scratch\n"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/shell@1.0.0")

	images := newFakeImages()
	r := g.runImages(dir, images, "build", "erp/shell")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, images.builds, 1)
	assert.Equal(t, "erp/a@1.0.0,erp/b@2.0.0", images.builds[0].Labels["io.brickkit.shell.members"])
}

// 兼容版本不在本地仓库里：从它的 tag 导出源码构建，远端没了也照样从缓存里导出。
func TestBuildCompatibilityVersionFromTag(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(buildOnly(comp{ID: "erp/db", Version: "1.0.0", Port: 5432}), map[string]string{"Dockerfile": "FROM scratch\n", "VERSION": "one\n"})
	g.release(buildOnly(comp{ID: "erp/db", Version: "2.0.0", Port: 5432}), map[string]string{"Dockerfile": "FROM scratch\n", "VERSION": "two\n"})
	g.release(comp{ID: "erp/old", Version: "1.0.0", Requires: []string{"erp/db@1.0.0"}, Port: 8081})
	dir := g.project()
	g.mustRun(dir, "add", "erp/db@2.0.0")
	g.mustRun(dir, "add", "erp/old@1.0.0")
	require.NoError(t, os.RemoveAll(g.remotes["erp/db"].Dir()))

	images := &fakeImages{present: map[string]map[string]string{}, buildErr: map[string]error{}}
	var contexts = map[string]string{}
	r := runWith(t, func(o *Options) {
		o.Engine = newFakeEngine()
		o.RepoCacheDir = g.cache
		o.Images = recordingContext{images, contexts}
	}, dir, "build", "erp/db@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"erp-db:1.0.0"}, images.built())
	assert.Equal(t, "one\n", contexts["erp-db:1.0.0"], "构建上下文是 1.0.0 那个 tag 的源码")
}

// 点名一个不需要构建的组件（镜像是拉的）：说明原因，不报错。
func TestBuildNamedComponentThatNeedsNoBuild(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/pulled", Version: "2.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/pulled@2.0.0")
	images := newFakeImages()
	r := g.runImages(dir, images, "build", "erp/pulled")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Empty(t, images.built())
	assert.Contains(t, r.stdout, "registry.example.com/erp-pulled:2.0.0")

	r = g.runImages(dir, images, "build", "erp/nope")
	assert.Equal(t, clierr.ExitError, r.code)
}

// 一次构建失败就停下，点名是哪个组件；已经构建好的留着。
func TestBuildStopsAtFirstFailure(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(buildOnly(comp{ID: "erp/db", Version: "1.0.0", Port: 5432}), map[string]string{"Dockerfile": "FROM scratch\n"})
	g.release(buildOnly(comp{ID: "erp/api", Version: "1.0.0", Requires: []string{"erp/db@1.0.0"}, Port: 8081}), map[string]string{"Dockerfile": "FROM scratch\n"})
	g.release(buildOnly(comp{ID: "erp/web", Version: "1.0.0", Requires: []string{"erp/api@1.0.0"}, Port: 8082}), map[string]string{"Dockerfile": "FROM scratch\n"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/web@1.0.0")

	images := newFakeImages()
	images.buildErr["erp-api:1.0.0"] = errors.New("go build returned 1")
	r := g.runImages(dir, images, "build")
	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "erp/api@1.0.0")
	assert.Equal(t, []string{"erp-db:1.0.0"}, images.built(), "依赖顺序：db 先构建，api 失败，web 没开始")
}
