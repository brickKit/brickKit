package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func (g *gitOrgProject) upWith(dir string, eng *fakeEngine, images *fakeImages, args ...string) result {
	g.t.Helper()
	return runWith(g.t, func(o *Options) {
		o.Engine = eng
		o.Images = images
		o.RepoCacheDir = g.cache
	}, dir, append([]string{"up"}, args...)...)
}

// 提案 §9.10.1/9.10.3：up 从不构建。只有 build 的组件镜像不在本机：启动前就失败，提示 build；
// --dry-run 不看镜像；构建之后照常启动，用的是推出来的镜像名。
func TestUpBuildOnlyImageMissingPointsToBuild(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(buildOnly(comp{ID: "erp/api", Version: "1.0.0"}), map[string]string{"Dockerfile": "FROM scratch\n"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")

	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, g.upWith(dir, eng, newFakeImages(), "--dry-run").code)
	r := g.upWith(dir, eng, newFakeImages())
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "brickkit build erp/api@1.0.0")
	assert.Empty(t, eng.ups, "镜像不齐不启动任何东西")
	assert.NotContains(t, eng.checked, "", "不会拿空镜像名去查")

	r = g.upWith(dir, eng, newFakeImages("erp-api:1.0.0"))
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Len(t, eng.ups, 1)
}

// 本地安装源的组件写了 image 也不从 registry 拉：本地源是正在开发的代码，拉来的镜像是另一份代码。
func TestUpLocalSourceNeverPullsInstead(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml":                     "project: shop\nsources:\n  - {name: dev, type: local, path: ./components}\ncomponents:\n  - {id: erp/api, version: 1.0.0}\n",
		"deploy.yaml":                       "target: docker\ncomponents:\n  - id: erp/api\n",
		"components/erp/api/component.yaml": comp{ID: "erp/api", Version: "1.0.0"}.yamlText(),
	})
	eng := newFakeEngine()
	r := runWith(t, func(o *Options) { o.Engine = eng; o.Images = newFakeImages() }, dir, "up")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "brickkit build erp/api@1.0.0")
	assert.Empty(t, eng.ups)
}

func TestUpListsEveryMissingImage(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(buildOnly(comp{ID: "erp/a", Version: "1.0.0", Port: 8081}), map[string]string{"Dockerfile": "FROM scratch\n"})
	g.release(buildOnly(comp{ID: "erp/b", Version: "1.0.0", Port: 8082}), map[string]string{"Dockerfile": "FROM scratch\n"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/a@1.0.0")
	g.mustRun(dir, "add", "erp/b@1.0.0")

	r := g.upWith(dir, newFakeEngine(), newFakeImages())
	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "erp-a:1.0.0")
	assert.Contains(t, r.stderr, "erp-b:1.0.0")
}

// shellFixture：外壳（只有 build）编进 erp/a@1.0.0。
func shellImageFixture(t *testing.T) (*gitOrgProject, string) {
	t.Helper()
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(buildOnly(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0"}}), map[string]string{"Dockerfile": "FROM scratch\n"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/shell@1.0.0")
	return g, dir
}

// 附录 A24：本机构建的外壳镜像记下的成员版本与 component.yaml 对不上（改了成员版本没重建）——报错，提示 build --force。
func TestUpShellImageLabelMismatch(t *testing.T) {
	g, dir := shellImageFixture(t)
	images := newFakeImages()
	images.present["erp-shell:1.0.0"] = map[string]string{labelShellMembers: "erp/a@0.9.0"}

	r := g.upWith(dir, newFakeEngine(), images)
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "erp/a@0.9.0")
	assert.Contains(t, r.stderr, "erp/a@1.0.0")
	assert.Contains(t, r.stderr, "brickkit build erp/shell@1.0.0 --force")

	images.present["erp-shell:1.0.0"] = map[string]string{labelShellMembers: "erp/a@1.0.0"}
	r = g.upWith(dir, newFakeEngine(), images)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 镜像没有这个标签（手工构建、第三方镜像）：只警告"无法确认"。
func TestUpShellImageWithoutLabelWarns(t *testing.T) {
	g, dir := shellImageFixture(t)
	r := g.upWith(dir, newFakeEngine(), newFakeImages("erp-shell:1.0.0"))
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr, "erp/shell@1.0.0")
	assert.Contains(t, r.stdout+r.stderr, "cannot be confirmed")
}

// 外壳镜像只在 registry 里（发布出去的版本）：tag、component.yaml、镜像三者天然一致，不核对。
func TestUpShellImageOnlyInRegistryNotChecked(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/shell@1.0.0")

	r := g.upWith(dir, newFakeEngine(), newFakeImages())
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "cannot be confirmed")
}

// §8.5 规则 3：外壳成员必须有自己的镜像（image 或 build）——没有的组件连 Manifest 校验都过不了，add 当场失败。
func TestAddShellMemberWithoutImageFails(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0"}})
	g.releaseRaw("erp/a", "1.0.0", "apiVersion: brickkit/v1\nkind: Component\nmetadata: {id: erp/a, name: a, version: 1.0.0, description: x}\ndeployment: {type: container, port: 8081}\nhealthCheck: {type: none}\n")
	dir := g.project()

	r := g.run(dir, "add", "erp/shell@1.0.0")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "erp/a")
	assert.NotContains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "erp/shell")
}
