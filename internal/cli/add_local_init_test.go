package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/projfile"
)

// workspaceProject：本地源 components/ 里 erp/backend 依赖 erp/db（也在本地源里），
// 外壳目录 shell/ 里有 erp/shell；另配一个 git 组织源，给子工作台继承。
func workspaceProject(t *testing.T) (*gitOrgProject, string) {
	t.Helper()
	g := newGitOrgProject(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\n" +
			"  - name: local-shells\n    type: local\n    path: ./shell\n" +
			"  - name: org\n    type: git\n    baseUrl: " + "file:///nowhere/" + "\ncomponents: []\n",
		"deploy.yaml":                           "target: docker\ncomponents: []\n",
		"components/erp/db/component.yaml":      comp{ID: "erp/db", Version: "1.0.0"}.yamlText(),
		"components/erp/backend/component.yaml": comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"erp/db@1.0.0"}}.yamlText(),
		"shell/erp/shell/component.yaml":        comp{ID: "erp/shell", Version: "1.0.0"}.yamlText(),
	})
	return g, dir
}

func TestAddLocalInitCreatesChildWorkbench(t *testing.T) {
	g, dir := workspaceProject(t)
	r := g.mustRun(dir, "add", "--local", "--init", "--yes")
	assert.Contains(t, r.stdout, "components/erp/backend")

	child := filepath.Join(dir, "components", "erp", "backend")
	decl, err := projfile.ParseFile(filepath.Join(child, "brickkit.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "erp-backend", decl.Project)
	var paths []string
	for _, s := range decl.Sources {
		paths = append(paths, s.Name+"="+s.Path+s.BaseURL)
	}
	assert.Equal(t, []string{"local-dev=../..", "local-shells=../../../shell", "org=file:///nowhere/"}, paths,
		"本地源的路径改写成相对子目录，看得到兄弟组件")
	require.Len(t, decl.Components, 1, "子工作台里是它自己的依赖")
	assert.Equal(t, "erp/db", decl.Components[0].ID)
	assert.Contains(t, readFile(t, filepath.Join(child, "deploy.yaml")), "- id: erp/db")
	assert.FileExists(t, filepath.Join(child, ".gitignore"))

	// 子工作台能独立装载
	sub := g.run(child, "lint")
	assert.Equal(t, clierr.ExitOK, sub.code, sub.stdout+sub.stderr)

	// 顶层项目照常 add --local
	top := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	for _, id := range []string{"erp/backend", "erp/db", "erp/shell"} {
		assert.Contains(t, top, "id: "+id)
	}
}

func TestAddLocalInitLeavesInitializedChild(t *testing.T) {
	g, dir := workspaceProject(t)
	mine := "project: mine\ncomponents: []\n"
	writeTree(t, dir, map[string]string{
		"components/erp/backend/brickkit.yaml": mine,
		"components/erp/backend/deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	g.mustRun(dir, "add", "--local", "--init", "--yes")
	assert.Equal(t, mine, readFile(t, filepath.Join(dir, "components", "erp", "backend", "brickkit.yaml")))
	assert.FileExists(t, filepath.Join(dir, "components", "erp", "db", "brickkit.yaml"), "别的子组件照样初始化")
}

func TestAddLocalInitCoversShellDir(t *testing.T) {
	g, dir := workspaceProject(t)
	g.mustRun(dir, "add", "--local", "--init", "--yes")
	assert.FileExists(t, filepath.Join(dir, "shell", "erp", "shell", "brickkit.yaml"))
}

// 某个子组件的依赖拉不到：大声失败，点名那个子组件；已经建好的子工作台留着，顶层项目不动。
func TestAddLocalInitStopsAtFirstFailure(t *testing.T) {
	g, dir := workspaceProject(t)
	writeTree(t, dir, map[string]string{
		"components/erp/backend/component.yaml": comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"erp/missing@1.0.0"}}.yamlText(),
	})
	r := g.run(dir, "add", "--local", "--init", "--yes")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "erp/missing")
	assert.Contains(t, r.stderr, filepath.Join("components", "erp", "backend"))
	assert.NotContains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "erp/backend", "顶层项目没被改")
}

func TestAddInitWithoutLocalIsUsageError(t *testing.T) {
	g, dir := workspaceProject(t)
	r := g.run(dir, "add", "erp/backend@1.0.0", "--init")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "--init")
}
