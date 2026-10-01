package cli

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/source/gittest"
)

// gitOrgProject 是一个从 git 组织拉组件的项目：org 下每个组件一个仓库（<scope>-<name>.git），
// 每个版本一个 tag；cache 是用户级 bare 仓库缓存（同一个 gitOrgProject 的各个项目共用）。
type gitOrgProject struct {
	t       *testing.T
	org     string
	cache   string
	remotes map[string]*gittest.Remote
}

func newGitOrgProject(t *testing.T) *gitOrgProject {
	return &gitOrgProject{t: t, org: t.TempDir(), cache: t.TempDir(), remotes: map[string]*gittest.Remote{}}
}

// release 在组件仓库里发布一个版本（打 tag），extra 是 component.yaml 之外的文件。
func (g *gitOrgProject) release(c comp, extra ...map[string]string) {
	g.t.Helper()
	r, ok := g.remotes[c.ID]
	if !ok {
		r = gittest.NewRemoteIn(g.t, g.org, strings.ReplaceAll(c.ID, "/", "-"))
		g.remotes[c.ID] = r
	}
	files := map[string]string{"component.yaml": c.yamlText()}
	for _, m := range extra {
		for k, v := range m {
			files[k] = v
		}
	}
	r.Tag(c.Version, files)
}

// releaseRaw 发布一份原样的 component.yaml（造不合法的组件用）。
func (g *gitOrgProject) releaseRaw(id, version, yamlText string) {
	g.t.Helper()
	r, ok := g.remotes[id]
	if !ok {
		r = gittest.NewRemoteIn(g.t, g.org, strings.ReplaceAll(id, "/", "-"))
		g.remotes[id] = r
	}
	r.Tag(version, map[string]string{"component.yaml": yamlText})
}

// project 建一个空项目：只配了这个 git 组织作安装源。
func (g *gitOrgProject) project() string {
	g.t.Helper()
	dir := g.t.TempDir()
	writeTree(g.t, dir, map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: org\n    type: git\n    baseUrl: " + gittest.BaseURL(g.org) + "\ncomponents: []\n",
		"deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	return dir
}

func (g *gitOrgProject) run(dir string, args ...string) result {
	g.t.Helper()
	return g.runStdin(dir, "", args...)
}

func (g *gitOrgProject) runStdin(dir, stdin string, args ...string) result {
	g.t.Helper()
	return runWith(g.t, func(o *Options) {
		o.Engine = newFakeEngine()
		o.RepoCacheDir = g.cache
		if stdin != "" {
			o.Stdin = strings.NewReader(stdin)
		}
	}, dir, args...)
}

func (g *gitOrgProject) mustRun(dir string, args ...string) result {
	g.t.Helper()
	r := g.run(dir, args...)
	require.Equal(g.t, clierr.ExitOK, r.code, "%v\n%s", args, r.stdout+r.stderr)
	return r
}

func TestAddFromGitWritesThreeLayers(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0", ConfigSchema: []string{"DB_HOST:localhost"}})
	g.release(comp{ID: "erp/portal", Version: "1.0.0", Requires: []string{"erp/api@1.0.0"}, Port: 8090})
	dir := g.project()

	r := g.mustRun(dir, "add", "erp/portal@1.0.0")
	assert.Contains(t, r.stdout, "erp/api@1.0.0")

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/api\n    version: 1.0.0")
	assert.Contains(t, decl, "id: erp/portal\n    version: 1.0.0")
	deploy := readFile(t, filepath.Join(dir, "deploy.yaml"))
	assert.Contains(t, deploy, "- id: erp/api\n")
	assert.Contains(t, deploy, "- id: erp/portal\n")
	assert.Contains(t, readFile(t, filepath.Join(dir, "config", "erp-api.yaml")), "# Component: erp/api@1.0.0")
	assert.NoFileExists(t, filepath.Join(dir, "config", "erp-portal.yaml"), "没有 configSchema 不生成")

	g.mustRun(dir, "up", "--dry-run")
}

// 不写版本时取仓库里最高的精确版本 tag，钉进 brickkit.yaml。
func TestAddLatestFromTags(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/api", Version: "1.2.0"})
	dir := g.project()

	r := g.mustRun(dir, "add", "erp/api")
	assert.Contains(t, r.stdout, "1.2.0")
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "version: 1.2.0")
}

// 依赖要的是另一个版本——多版本自动共存，requiredBy 写上依赖方。
func TestAddDependencyOtherVersionCoexists(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/db", Version: "1.0.0", ConfigSchema: []string{"DB_HOST:localhost"}})
	g.release(comp{ID: "erp/db", Version: "2.0.0", ConfigSchema: []string{"DB_HOST:localhost"}})
	g.release(comp{ID: "erp/api", Version: "1.0.0", Requires: []string{"erp/db@1.0.0"}, Port: 8081})
	dir := g.project()

	g.mustRun(dir, "add", "erp/db@2.0.0")
	g.mustRun(dir, "add", "erp/api@1.0.0")

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "version: 1.0.0\n    requiredBy: [erp/api]")
	assert.Contains(t, readFile(t, filepath.Join(dir, "deploy.yaml")), "- id: erp/db@1.0.0\n")
	assert.FileExists(t, filepath.Join(dir, "config", "erp-db@1.0.0.yaml"))
	assert.FileExists(t, filepath.Join(dir, "config", "erp-db.yaml"))
	g.mustRun(dir, "up", "--dry-run")
}

// add 外壳时按它编进的成员版本写三份文件，P3f 的版本核对直接通过。
func TestAddShellWithMembers(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "1.0.0", Port: 8082})
	g.release(comp{ID: "erp/b", Version: "2.0.0", Port: 8082})
	g.release(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0", "erp/b@1.0.0"}})
	dir := g.project()

	g.mustRun(dir, "add", "erp/b@2.0.0")
	g.mustRun(dir, "add", "erp/shell@1.0.0")

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/shell\n    version: 1.0.0\n    kind: shell")
	assert.Contains(t, decl, "id: erp/b\n    version: 1.0.0\n    requiredBy: [erp/shell]")
	deploy := readFile(t, filepath.Join(dir, "deploy.yaml"))
	assert.Contains(t, deploy, "  - id: erp/shell\n    members:\n      - id: erp/a\n      - id: erp/b@1.0.0\n")
	assert.Contains(t, deploy, "  - id: erp/b\n", "默认版本留在顶层独立运行")
	g.mustRun(dir, "up", "--dry-run")
}

// add 写 deploy.yaml，也写 deploy.local.yaml（它存在时）；-f 用的别的部署文件不碰，说一声。
func TestAddWritesDeployLocalToo(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	writeTree(t, dir, map[string]string{
		"deploy.local.yaml": "target: docker\ncomponents: []\n",
		"deploy.prod.yaml":  "target: k8s\ncomponents: []\n",
	})

	r := g.mustRun(dir, "add", "erp/api@1.0.0")
	assert.Contains(t, readFile(t, filepath.Join(dir, "deploy.local.yaml")), "- id: erp/api\n")
	assert.Equal(t, "target: k8s\ncomponents: []\n", readFile(t, filepath.Join(dir, "deploy.prod.yaml")))
	assert.Contains(t, r.stdout, "deploy.prod.yaml")
}

// config/vars.yaml 里有同名变量——问要不要引用；--yes 引用，没有输入不引用。
func TestAddVarPrompt(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0", ConfigSchema: []string{"DB_HOST:localhost"}})

	for name, tc := range map[string]struct {
		stdin string
		args  []string
		want  string
	}{
		"answer y":  {"y\n", nil, "DB_HOST: $var:DB_HOST"},
		"--yes":     {"", []string{"--yes"}, "DB_HOST: $var:DB_HOST"},
		"no answer": {"", nil, "# DB_HOST: localhost"},
	} {
		dir := g.project()
		writeTree(t, dir, map[string]string{"config/vars.yaml": "DB_HOST: pg.internal\n"})
		r := g.runStdin(dir, tc.stdin, append([]string{"add", "erp/api@1.0.0"}, tc.args...)...)
		require.Equal(t, clierr.ExitOK, r.code, "%s\n%s", name, r.stdout+r.stderr)
		assert.Contains(t, readFile(t, filepath.Join(dir, "config", "erp-api.yaml")), tc.want, name)
	}
}

// 改完三份文件后项目装载不了：全部还原，一个字节都不留（add 绝不留下 up 读不了的项目）。
func TestAddRollsBackWhenProjectWouldNotLoad(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0", ConfigSchema: []string{"DB_HOST:localhost"}})
	dir := g.project()
	// 一份孤立的带版本号配置：erp/api 1.0.0 一旦成为默认版本，默认版本就有了两份配置文件
	writeTree(t, dir, map[string]string{"config/erp-api@1.0.0.yaml": "DB_HOST: x\n"})
	before := map[string]string{
		"brickkit.yaml": readFile(t, filepath.Join(dir, "brickkit.yaml")),
		"deploy.yaml":   readFile(t, filepath.Join(dir, "deploy.yaml")),
	}

	r := g.run(dir, "add", "erp/api@1.0.0")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	for name, content := range before {
		assert.Equal(t, content, readFile(t, filepath.Join(dir, name)), name)
	}
	assert.NoFileExists(t, filepath.Join(dir, "config", "erp-api.yaml"))
}

// 拉过一次之后，远端没了，同一台机器上的另一个项目照样能 add、能 up。
func TestAddOfflineAfterFirstFetch(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.mustRun(g.project(), "add", "erp/api@1.0.0")

	require.NoError(t, os.RemoveAll(g.remotes["erp/api"].Dir()))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.mustRun(dir, "up", "--dry-run")
}

// --repo 克隆源码后检出这个版本的 tag——本地仓库就是默认版本。
func TestAddRepoChecksOutTag(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/api", Version: "1.1.0"})
	dir := g.project()

	g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")
	assert.Contains(t, readFile(t, filepath.Join(dir, "components", "erp", "api", "component.yaml")), "version: 1.0.0")
}

// --local 把本地安装源里的组件按 component.yaml 的真实版本一次全加进来。
func TestAddLocalAddsAllAtRealVersions(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml":                   "project: shop\nsources:\n  - name: dev\n    type: local\n    path: ./components\ncomponents: []\n",
		"deploy.yaml":                     "target: docker\ncomponents: []\n",
		"components/erp/a/component.yaml": comp{ID: "erp/a", Version: "1.2.0", Requires: []string{"erp/b@1.0.0"}}.yamlText(),
		"components/erp/b/component.yaml": comp{ID: "erp/b", Version: "1.0.0", Port: 8081}.yamlText(),
	})
	r := runWithEngine(t, newFakeEngine(), dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/a\n    version: 1.2.0")
	assert.Contains(t, decl, "id: erp/b\n    version: 1.0.0")
	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 用户裁定（2026-09-27）：直接 add 另一个版本报错，指向 upgrade。
func TestAddOtherVersionPointsToUpgrade(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/api", Version: "2.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")

	r := g.run(dir, "add", "erp/api@2.0.0")
	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "brickkit upgrade erp/api@2.0.0")
}

func TestAddFlagErrors(t *testing.T) {
	g := newGitOrgProject(t)
	dir := g.project()
	for name, args := range map[string][]string{
		"no component":        {"add"},
		"--local with an id":  {"add", "--local", "erp/api"},
		"--local with --repo": {"add", "--local", "--repo"},
	} {
		r := g.run(dir, args...)
		assert.Equal(t, clierr.ExitUsage, r.code, name)
	}
	r := g.run(dir, "add", "--local")
	assert.Equal(t, clierr.ExitError, r.code, "没有本地安装源")
	assert.Contains(t, r.stderr, "type: local")
}

// 同一个组件再 add 一次：三份文件都不用改，照实说。
func TestAddSameComponentTwice(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	r := g.mustRun(dir, "add", "erp/api@1.0.0")
	assert.Contains(t, r.stdout, "already in the project")
}

// add --local 不改已有组件的版本：本地仓库与默认版本对不上时说一声；全都在了就说全都在了。
func TestAddLocalVersionDiffersAndNothingLeft(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml":                   "project: shop\nsources:\n  - name: dev\n    type: local\n    path: ./components\ncomponents: []\n",
		"deploy.yaml":                     "target: docker\ncomponents: []\n",
		"components/erp/a/component.yaml": comp{ID: "erp/a", Version: "1.0.0"}.yamlText(),
	})
	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), dir, "add", "--local").code)
	r := runWithEngine(t, newFakeEngine(), dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "already in the project")

	writeTree(t, dir, map[string]string{"components/erp/a/component.yaml": comp{ID: "erp/a", Version: "1.1.0"}.yamlText()})
	r = runWithEngine(t, newFakeEngine(), dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "1.1.0")
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "version: 1.0.0", "版本不动")
}

// --repo-all：这次加进来的每个开源组件都克隆（默认版本）；产物照常下载。
func TestAddRepoAllAndArtifacts(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
		map[string]string{"openapi.json": `{"openapi":"3.0.0"}`})
	g.release(comp{ID: "erp/portal", Version: "1.0.0", Requires: []string{"erp/api@1.0.0"}, Port: 8090})
	dir := g.project()

	r := g.mustRun(dir, "add", "erp/portal@1.0.0", "--repo-all")
	assert.Contains(t, r.stdout, "Artifacts")
	assert.FileExists(t, filepath.Join(dir, "components", "erp", "api", "component.yaml"))
	assert.FileExists(t, filepath.Join(dir, "components", "erp", "portal", "component.yaml"))

	// 源码目录已经在：克隆前就拦下，三份文件一个字都没写
	g.release(comp{ID: "erp/web", Version: "1.0.0"})
	writeTree(t, dir, map[string]string{"components/erp/web/README.md": "mine\n"})
	before := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	r = g.run(dir, "add", "erp/web@1.0.0", "--repo")
	require.Equal(t, clierr.ExitError, r.code)
	assert.Equal(t, before, readFile(t, filepath.Join(dir, "brickkit.yaml")))
}

// 组件已经在项目里，事后才想要源码：add --repo 照样克隆（三份文件不用改）。
func TestAddRepoOnAnAlreadyAddedComponent(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")
	assert.FileExists(t, filepath.Join(dir, "components", "erp", "api", "component.yaml"))
}

// 三份文件写好之后克隆失败：说清楚文件已经改好了，修好之后再 add --repo 只会补克隆。
func TestAddCloneFailureSaysFilesAreWritten(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	// components/erp 是个文件：资格检查放行（组件目录不存在），真克隆时建不了目录
	writeTree(t, dir, map[string]string{"components/erp": "not a directory\n"})

	r := g.run(dir, "add", "erp/api@1.0.0", "--repo")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "erp/api")
	assert.Contains(t, r.stderr, "brickkit add erp/api@1.0.0 --repo")
}

// --repo 从本机的仓库缓存克隆（离线也行），origin 指回真正的远端：推送照常去原仓库。
func TestAddRepoClonesFromTheCacheAndPointsOriginAtTheRemote(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	url := gittest.BaseURL(g.org) + "erp-api" // brickkit.yaml 推导出的地址
	g.mustRun(g.project(), "fetch", "erp/api@1.0.0")
	require.NoError(t, os.Rename(g.remotes["erp/api"].Dir(), g.remotes["erp/api"].Dir()+".offline"))

	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")
	repo := filepath.Join(dir, "components", "erp", "api")
	out, err := exec.Command("git", "-C", repo, "remote", "get-url", "origin").CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, url, strings.TrimSpace(string(out)))
}

// 还原要彻底：这次新建的目录（config/）一并删掉，原有文件的权限保持原样。
func TestAddRollbackRemovesCreatedDirsAndKeepsModes(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0", ConfigSchema: []string{"DB_HOST:localhost"}})
	dir := g.project()
	// 本地部署文件里多一个 brickkit.yaml 没有的条目：add 写完之后的核对会拦下
	writeTree(t, dir, map[string]string{"deploy.local.yaml": "target: docker\ncomponents:\n  - id: erp/ghost\n"})
	require.NoError(t, os.Chmod(filepath.Join(dir, "deploy.yaml"), 0o600))
	require.NoDirExists(t, filepath.Join(dir, "config"))

	r := g.run(dir, "add", "erp/api@1.0.0")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.NoDirExists(t, filepath.Join(dir, "config"))
	info, err := os.Stat(filepath.Join(dir, "deploy.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// remove 归档的配置，重新 add 时迁移回来——使用者写过的值回来了。
func TestAddRestoresArchivedConfig(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "DB_HOST:x"))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	cfg := filepath.Join(dir, "config", "erp-api.yaml")
	editFile(t, cfg, "# DB_HOST: ", "DB_HOST: pg.internal  # ")
	g.mustRun(dir, "remove", "erp/api")
	require.NoFileExists(t, cfg)

	r := g.mustRun(dir, "add", "erp/api@1.0.0")
	assert.Contains(t, readFile(t, cfg), "DB_HOST: pg.internal")
	assert.Contains(t, r.stdout, "config/.archive/erp-api@1.0.0.yaml")
}

// 配置要从归档恢复时，归档里是使用者当初写的配置，以它为准：不再问要不要引用公共变量，
// 更不能宣称"已引用"——从前这里先问、再被恢复的文件整个盖掉，输出与文件对不上。
func TestAddVarPromptSkipsConfigRestoredFromArchive(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "DB_HOST:x"))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	cfg := filepath.Join(dir, "config", "erp-api.yaml")
	editFile(t, cfg, "# DB_HOST: ", "DB_HOST: pg.internal  # ")
	g.mustRun(dir, "remove", "erp/api")
	writeTree(t, dir, map[string]string{"config/vars.yaml": "DB_HOST: shared.host\n"})

	r := g.mustRun(dir, "add", "erp/api@1.0.0", "--yes")
	assert.Contains(t, readFile(t, cfg), "DB_HOST: pg.internal", "恢复的是使用者当初写的值")
	assert.NotContains(t, r.stdout, "references the variable of the same name", "没有引用的事不能说引用了")
	assert.NotContains(t, r.stdout, "Config skeletons: config/erp-api.yaml", "恢复出来的不是新骨架")
}

// 归档里是旧版本（1.0.0），这次 add 的是 2.0.0：按迁移算法迁移，新版本没有了的键报出来。
func TestAddRestoresFromOlderArchivedVersion(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "DB_HOST:x", "OLD_KEY:y"))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	cfg := filepath.Join(dir, "config", "erp-api.yaml")
	editFile(t, cfg, "# DB_HOST: ", "DB_HOST: pg.internal  # ")
	editFile(t, cfg, "# OLD_KEY: ", "OLD_KEY: legacy  # ")
	g.mustRun(dir, "remove", "erp/api")
	g.release(withConfig(comp{ID: "erp/api", Version: "2.0.0"}, "DB_HOST:x", "NEW_KEY:z"))

	r := g.mustRun(dir, "add", "erp/api@2.0.0")
	text := readFile(t, cfg)
	assert.Contains(t, text, "DB_HOST: pg.internal")
	assert.Contains(t, text, "# Component: erp/api@2.0.0")
	assert.NotContains(t, text, "OLD_KEY")
	assert.Contains(t, r.stdout, "OLD_KEY")
}

// 从归档恢复遇到冲突（使用者改过、默认值也变了）：--yes 写成冲突块，up 在解决之前拒绝启动。
func TestAddArchiveRestoreConflictUsesYes(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "LOG_LEVEL:info"))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	cfg := filepath.Join(dir, "config", "erp-api.yaml")
	editFile(t, cfg, "# LOG_LEVEL: ", "LOG_LEVEL: debug  # ")
	g.mustRun(dir, "remove", "erp/api")
	g.release(withConfig(comp{ID: "erp/api", Version: "2.0.0"}, "LOG_LEVEL:warn"))

	r := g.run(dir, "add", "erp/api@2.0.0", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, 2, strings.Count(readFile(t, cfg), "\nLOG_LEVEL: "))
	assert.Equal(t, clierr.ExitError, g.run(dir, "up", "--dry-run").code)
}

// 归档文件名只按 FileBase 匹配会撞（a-b/c 与 a/b-c 都是 a-b-c）：还要核对文件头里的组件 ID。
func TestAddRestoreChecksTheArchivedComponentID(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "a/b-c", Version: "1.0.0"}, "DB_HOST:x"))
	dir := g.project()
	writeTree(t, dir, map[string]string{"config/.archive/a-b-c@1.0.0.yaml": "# Component: a-b/c@1.0.0\nDB_HOST: someone-else\n"})
	g.mustRun(dir, "add", "a/b-c@1.0.0")
	assert.NotContains(t, readFile(t, filepath.Join(dir, "config", "a-b-c.yaml")), "someone-else")
}

// 闭源组件（市场里 sourceType: registry）没有源码仓库：--repo 照常装上组件，
// 只是点名说明它没被克隆、为什么，而不是一声不吭——也不是整条 add 失败。
func TestAddRepoClosedSourceIsSaidAndSkipped(t *testing.T) {
	market := newMockMarket(t, &mockComponent{
		Spec: comp{ID: "vendor/engine", Version: "1.0.0"}, SourceType: "registry",
	})
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "vendor/engine@1.0.0", "--repo", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "vendor/engine@1.0.0 has no source repository")
	assert.Contains(t, r.stdout, "closed source")
	assert.NoDirExists(t, filepath.Join(f.Dir, "components", "vendor", "engine"))
	assert.Contains(t, f.refs(t), "vendor/engine@1.0.0", "组件照常装上")
}

// add 把组件声明的产物（契约、文档）下载进 .brickkit/artifacts/<服务名>/，内容与组件仓库里的一致。
func TestAddDownloadsArtifacts(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0", Artifacts: []string{"api-contract:api/openapi.yaml"}},
		map[string]string{"api/openapi.yaml": "openapi: 3.0.3\n"})
	dir := g.project()

	g.mustRun(dir, "add", "erp/api@1.0.0")

	var found []string
	root := filepath.Join(project.NewLayout(dir).ArtifactsDir(), "erp-api-1-0-0")
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = append(found, path)
		}
		return err
	}))
	require.Len(t, found, 1, "声明了一个产物文件，就下载一个")
	assert.Equal(t, "openapi: 3.0.3\n", readFile(t, found[0]))
}

// 写到一半失败（这里是 config/ 不可写）：已经写过的 brickkit.yaml / deploy.yaml 要回到 add 之前，
// 不能留下一份"组件登记了、配置却没有"的半成品。
func TestAddWriteFailureLeavesNoHalfWrittenProject(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限限制，造不出写失败")
	}
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0", ConfigSchema: []string{"DB_HOST:localhost"}})
	dir := g.project()
	configDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.Chmod(configDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(configDir, 0o755) })
	before := map[string]string{
		"brickkit.yaml": readFile(t, filepath.Join(dir, "brickkit.yaml")),
		"deploy.yaml":   readFile(t, filepath.Join(dir, "deploy.yaml")),
	}

	r := g.run(dir, "add", "erp/api@1.0.0")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	for name, content := range before {
		assert.Equal(t, content, readFile(t, filepath.Join(dir, name)), "%s 回到 add 之前", name)
	}
}

// remove 会删组件的源码目录——只在删掉之后还找得回来时才删。找不回来的三种情况都要拦下，
// 并且拦下时三份文件一个字都不改；--force 才照删。
func TestRemoveRefusesToDeleteUnrecoverableSource(t *testing.T) {
	commit := func(t *testing.T, dir string, args ...string) {
		t.Helper()
		gitCmd(t, dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	}
	refused := func(t *testing.T, g *gitOrgProject, dir, id, src string) {
		t.Helper()
		r := g.run(dir, "remove", id)
		require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
		assert.DirExists(t, src)
		assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), id, "拦下时三份文件都不动")
		g.mustRun(dir, "remove", id, "--force")
		assert.NoDirExists(t, src)
	}

	t.Run("hand-written component, not a git repository", func(t *testing.T) {
		g := newGitOrgProject(t)
		dir := g.project()
		replaceInFile(t, filepath.Join(dir, "brickkit.yaml"), "sources:\n",
			"sources:\n  - name: local-dev\n    type: local\n    path: ./components\n")
		hand := comp{ID: "demo/hand", Version: "1.0.0"}
		src := filepath.Join(dir, "components", "demo", "hand")
		writeTree(t, src, hand.files())
		g.mustRun(dir, "add", "demo/hand@1.0.0")
		refused(t, g, dir, "demo/hand", src)
	})

	t.Run("commit on a branch that was never pushed", func(t *testing.T) {
		g := newGitOrgProject(t)
		g.release(comp{ID: "erp/api", Version: "1.0.0"})
		dir := g.project()
		g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")
		src := filepath.Join(dir, "components", "erp", "api")
		gitCmd(t, src, "switch", "--quiet", "-c", "fix")
		require.NoError(t, os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644))
		gitCmd(t, src, "add", "main.go")
		commit(t, src, "commit", "--quiet", "-m", "local work")
		refused(t, g, dir, "erp/api", src)
	})

	t.Run("commit on the detached tag checkout", func(t *testing.T) {
		g := newGitOrgProject(t)
		g.release(comp{ID: "erp/api", Version: "1.0.0"})
		dir := g.project()
		g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")
		src := filepath.Join(dir, "components", "erp", "api")
		require.NoError(t, os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644))
		gitCmd(t, src, "add", "main.go")
		commit(t, src, "commit", "--quiet", "-m", "local work on the tag")
		refused(t, g, dir, "erp/api", src)
	})
}

// 缓存建好之后远端又发了新版本：从缓存 clone 出来的源码目录一个字没改，
// remove 就必须照常删——不能因为缓存里的分支还停在第一次克隆时，
// 就把新版本的提交说成"没推到任何远端"。
func TestRemoveAcceptsPristineCloneOfLaterRelease(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	first := g.project()
	g.mustRun(first, "add", "erp/api@1.0.0") // 缓存在这时建好

	g.release(comp{ID: "erp/api", Version: "1.1.0"}) // 远端 main 前进了
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.1.0", "--repo")
	src := filepath.Join(dir, "components", "erp", "api")
	require.DirExists(t, src)

	r := g.run(dir, "remove", "erp/api")
	require.Equal(t, clierr.ExitOK, r.code, "一份没改过的 clone 不该被拦：%s", r.stdout+r.stderr)
	assert.NoDirExists(t, src)
}

// 新骨架里只有真有"必填且没有默认值"的键时才提示去填，而且点名是哪个文件、哪几个键；
// 全是可选项的骨架不能说"必填项要自己填上"——使用者会去找一个并不存在的必填项。
func TestAddNamesTheRequiredKeysToFill(t *testing.T) {
	dir := t.TempDir()
	optional := strings.Replace(schemaComp("erp/web"), "  required: [DB_HOST]\n", "", 1)
	writeTree(t, dir, map[string]string{
		"brickkit.yaml":                     "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents: []\n",
		"deploy.yaml":                       "target: docker\ncomponents: []\n",
		"components/erp/api/component.yaml": schemaComp("erp/api"),
		"components/erp/web/component.yaml": optional,
	})
	r := runIn(t, dir, "add", "--local", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Config skeletons: config/erp-api.yaml, config/erp-web.yaml\n")
	assert.Contains(t, r.stdout, "Fill in the required keys in config/erp-api.yaml: DB_HOST")
	assert.NotContains(t, r.stdout, "config/erp-web.yaml: ")
}

// releaseWithNotes 发布一个带发版说明的版本（brickkit release --notes 打的那种 tag）。
func (g *gitOrgProject) releaseWithNotes(c comp, notes string) {
	g.t.Helper()
	r, ok := g.remotes[c.ID]
	if !ok {
		r = gittest.NewRemoteIn(g.t, g.org, strings.ReplaceAll(c.ID, "/", "-"))
		g.remotes[c.ID] = r
	}
	r.TagWithNotes(c.Version, notes, map[string]string{"component.yaml": c.yamlText()})
}

// upgrade 先列出从现在的版本（不含）到目标版本（含）之间每个写了说明的版本的说明——
// --dry-run 也列，这样升级之前就看得到改了什么；没写说明的版本不出现。
func TestUpgradeShowsReleaseNotesOfEveryVersionInBetween(t *testing.T) {
	g := newGitOrgProject(t)
	g.releaseWithNotes(comp{ID: "erp/api", Version: "1.0.0"}, "the first")
	g.release(comp{ID: "erp/api", Version: "1.1.0"})
	g.releaseWithNotes(comp{ID: "erp/api", Version: "1.2.0"}, "## Added\n\n- an export endpoint")
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")

	r := g.mustRun(dir, "upgrade", "erp/api", "--dry-run")
	assert.Contains(t, r.stdout, "Release notes of erp/api, 1.0.0 → 1.2.0")
	assert.Contains(t, r.stdout, "── 1.2.0 ──")
	assert.Contains(t, r.stdout, "      ## Added")
	assert.Contains(t, r.stdout, "      - an export endpoint")
	assert.NotContains(t, r.stdout, "the first", "现在的版本自己的说明不列")
	assert.NotContains(t, r.stdout, "── 1.1.0 ──", "没写说明的版本不出现")

	r = g.mustRun(dir, "upgrade", "erp/api")
	assert.Contains(t, r.stdout, "- an export endpoint")
}
