package project_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
)

func complete(t *testing.T, root, name string) *project.CompletePlan {
	t.Helper()
	l := project.NewLayout(root)
	plan, err := project.PlanComplete(l, name)
	require.NoError(t, err)
	require.NoError(t, plan.Apply(l))
	return plan
}

// 空目录里补全出来的三层骨架必须能原样通过装载——否则第一条 up 就报错。
func TestPlanCompleteEmptyDirCreatesEverything(t *testing.T) {
	root := t.TempDir()
	plan := complete(t, root, "my-shop")
	assert.Empty(t, plan.Skip)
	assert.True(t, plan.GitignoreCreate)
	assert.True(t, plan.ProjectDoc)

	for _, name := range []string{"brickkit.yaml", "deploy.yaml", "config/vars.yaml", "config/.gitkeep", "shell/.gitkeep", ".gitignore", "BRICKKIT.md"} {
		assert.FileExists(t, filepath.Join(root, name))
	}
	assert.NoFileExists(t, filepath.Join(root, "deploy.local.yaml"), "本地文件由 local on 按需生成")
	for _, dir := range []string{".brickkit/manifests", ".brickkit/generated", "components", "shell"} {
		assert.DirExists(t, filepath.Join(root, dir))
	}

	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	assert.Equal(t, "my-shop", p.Decl.Project)
	assert.Equal(t, "docker", p.Deploy.Target)
	assert.Empty(t, p.Warnings)
	var local []string
	for _, s := range p.Decl.EnabledSources() {
		local = append(local, s.Name+"="+s.Path)
	}
	assert.Equal(t, []string{"local-dev=./components", "local-shells=./shell"}, local)

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	lines := strings.Split(string(data), "\n")
	for _, want := range []string{"deploy.local.yaml", "deploy.local.yaml.bak", ".secrets/", ".brickkit/", "config/.archive/", ".env"} {
		assert.Contains(t, lines, want)
	}
	assert.NotContains(t, lines, "shell/", "外壳是项目自己的代码，要进 git")
}

func TestPlanCompleteSkipsExistingFiles(t *testing.T) {
	root := t.TempDir()
	deploy := "target: k8s\ncomponents: []\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "deploy.yaml"), []byte(deploy), 0o644))

	plan := complete(t, root, "my-shop")
	assert.Contains(t, plan.Skip, "deploy.yaml")
	assert.NotContains(t, plan.Create, "deploy.yaml")
	assert.Contains(t, plan.Create, "brickkit.yaml")
	got, err := os.ReadFile(filepath.Join(root, "deploy.yaml"))
	require.NoError(t, err)
	assert.Equal(t, deploy, string(got), "已有的文件一个字节都不动")
}

func TestPlanCompleteNeverEditsExistingGitignore(t *testing.T) {
	root := t.TempDir()
	gitignore := "node_modules/\ndeploy.local.yaml\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644))

	plan := complete(t, root, "my-shop")
	assert.False(t, plan.GitignoreCreate)
	assert.Contains(t, plan.GitignoreMissing, ".brickkit/")
	assert.Contains(t, plan.GitignoreMissing, ".secrets/")
	assert.NotContains(t, plan.GitignoreMissing, "deploy.local.yaml")
	got, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, gitignore, string(got))
}

// 组件仓库兼作工作台（提案 §16.1.1）：BRICKKIT.md 是组件自己的文档，不能生成项目文档盖掉它，
// 也不能在它不在时替组件作者生成一份项目文档。
func TestPlanCompleteInComponentRepoLeavesBrickkitMd(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("apiVersion: brickkit/v1\n"), 0o644))

	plan := complete(t, root, "erp-backend")
	assert.False(t, plan.ProjectDoc)
	assert.NoFileExists(t, filepath.Join(root, "BRICKKIT.md"))
}

func TestPlanCompleteRejectsBadName(t *testing.T) {
	_, err := project.PlanComplete(project.NewLayout(t.TempDir()), "My Shop")
	require.Error(t, err)
}

func TestProjectNameForFallbacks(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "My_Shop")
	require.NoError(t, os.Mkdir(root, 0o755))
	l := project.NewLayout(root)

	name, err := project.ProjectNameFor(l, "given")
	require.NoError(t, err)
	assert.Equal(t, "given", name, "--name 优先")

	name, err = project.ProjectNameFor(l, "")
	require.NoError(t, err)
	assert.Equal(t, "my-shop", name, "目录名规整成合法项目名")

	require.NoError(t, os.WriteFile(l.DeclPath(), []byte("project: declared\ncomponents: []\n"), 0o644))
	name, err = project.ProjectNameFor(l, "")
	require.NoError(t, err)
	assert.Equal(t, "declared", name, "已有 brickkit.yaml 的 project 沿用")

	_, err = project.ProjectNameFor(l, "Bad Name")
	require.Error(t, err)

	odd := filepath.Join(parent, "___")
	require.NoError(t, os.Mkdir(odd, 0o755))
	_, err = project.ProjectNameFor(project.NewLayout(odd), "")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "--name")
}

func TestDirIsEmptyIgnoresGit(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	empty, err := project.DirIsEmpty(root)
	require.NoError(t, err)
	assert.True(t, empty)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), nil, 0o644))
	empty, err = project.DirIsEmpty(root)
	require.NoError(t, err)
	assert.False(t, empty)
}

// writeDocProject 写一个有两个组件的项目：erp/backend 的 BRICKKIT.md 与产物在缓存里，
// people/basic 什么都没缓存、但它在本地源里有源码。
func writeDocProject(t *testing.T) (string, *project.Project) {
	t.Helper()
	root := t.TempDir()
	complete(t, root, "my-shop")
	l := project.NewLayout(root)
	require.NoError(t, os.WriteFile(l.DeclPath(), []byte(`project: my-shop
sources:
  - name: local-dev
    type: local
    path: ./components
components:
  - id: erp/backend
    version: 2.0.0
  - id: people/basic
    version: 1.0.0
`), 0o644))
	require.NoError(t, os.WriteFile(l.DeployPath(), []byte(`target: docker
components:
  - id: erp/backend
  - id: people/basic
`), 0o644))
	require.NoError(t, os.MkdirAll(l.CachedManifestDir("erp/backend", "2.0.0"), 0o755))
	require.NoError(t, os.WriteFile(l.CachedDocPath("erp/backend", "2.0.0"), []byte("# erp/backend\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(l.ArtifactsDir(), "erp-backend-2-0-0"), 0o755))
	repo := filepath.Join(root, "components", "people", "basic")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "component.yaml"), []byte("metadata:\n  id: people/basic\n  version: 1.0.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "BRICKKIT.md"), []byte("# people/basic\n"), 0o644))

	p, err := project.Load(root, project.LoadOptions{NoLocal: true})
	require.NoError(t, err)
	return root, p
}

func TestRenderProjectDocTables(t *testing.T) {
	_, p := writeDocProject(t)
	doc := project.RenderProjectDoc(p)

	assert.Contains(t, doc, "<!-- brickkit:managed:begin -->")
	assert.Contains(t, doc, "<!-- brickkit:managed:end -->")
	assert.Contains(t, doc, "| erp/backend | 2.0.0 | `.brickkit/manifests/erp/backend/2.0.0/BRICKKIT.md` | `.brickkit/artifacts/erp-backend-2-0-0/` |")
	assert.Contains(t, doc, "| people/basic | 1.0.0 | `components/people/basic/BRICKKIT.md` | — |", "本地源组件指向正在改的那份；没下载的产物不写路径")
	assert.Contains(t, doc, "| people/basic | `components/people/basic/` | `components/people/basic/BRICKKIT.md` |")
}

func TestWriteProjectDocReplacesOnlyTheManagedBlock(t *testing.T) {
	root, p := writeDocProject(t)
	path := filepath.Join(root, "BRICKKIT.md")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	edited := strings.Replace(string(before), "# my-shop", "# my-shop\n\nNotes written by hand.", 1)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))

	written, err := project.WriteProjectDoc(p.Layout, p)
	require.NoError(t, err)
	assert.True(t, written)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "Notes written by hand.")
	assert.Contains(t, string(got), "| erp/backend | 2.0.0 |")
}

func TestWriteProjectDocLeavesFileWithoutMarkers(t *testing.T) {
	root, p := writeDocProject(t)
	path := filepath.Join(root, "BRICKKIT.md")
	mine := "# my own notes\n"
	require.NoError(t, os.WriteFile(path, []byte(mine), 0o644))

	written, err := project.WriteProjectDoc(p.Layout, p)
	require.NoError(t, err)
	assert.False(t, written)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, mine, string(got))
}

// 只有开始标记、没有结束标记：不算维护区，不动；文件不存在：不凭空生成（那是 init 的事）。
func TestWriteProjectDocNeedsBothMarkers(t *testing.T) {
	root, p := writeDocProject(t)
	path := filepath.Join(root, "BRICKKIT.md")
	half := "# x\n<!-- brickkit:managed:begin -->\n"
	require.NoError(t, os.WriteFile(path, []byte(half), 0o644))
	written, err := project.WriteProjectDoc(p.Layout, p)
	require.NoError(t, err)
	assert.False(t, written)

	require.NoError(t, os.Remove(path))
	written, err = project.WriteProjectDoc(p.Layout, p)
	require.NoError(t, err)
	assert.False(t, written)
	assert.NoFileExists(t, path)

	plan, err := project.PlanComplete(p.Layout, "my-shop")
	require.NoError(t, err)
	assert.True(t, plan.ProjectDoc, "文件没了，补全式会重新生成")
}

func TestPlanCompleteReportsUnmanagedDoc(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "BRICKKIT.md"), []byte("# mine\n"), 0o644))
	plan, err := project.PlanComplete(project.NewLayout(root), "my-shop")
	require.NoError(t, err)
	assert.False(t, plan.ProjectDoc)
	assert.True(t, plan.ProjectDocUnmanaged)
}

// 组件目录的本地联调工作台（提案 §9.6.1）：brickkit.yaml 继承给定的安装源，不建 components/、shell/
// ——那是项目的目录约定，组件仓库里用不上。
func TestPlanWorkbenchInheritsSources(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("x"), 0o644))
	l := project.NewLayout(root)
	sources := []projfile.Source{
		{Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "../.."},
		{Name: "org", Type: projfile.SourceTypeGit, BaseURL: "https://git.example.com/org/"},
	}
	plan, err := project.PlanWorkbench(l, "erp-backend", sources)
	require.NoError(t, err)
	require.NoError(t, plan.Apply(l))

	decl, err := projfile.ParseFile(l.DeclPath())
	require.NoError(t, err)
	assert.Equal(t, "erp-backend", decl.Project)
	assert.Equal(t, sources, decl.Sources)
	assert.NoDirExists(t, filepath.Join(root, "components"))
	assert.NoDirExists(t, filepath.Join(root, "shell"))
	assert.FileExists(t, filepath.Join(root, "deploy.yaml"))
	assert.FileExists(t, filepath.Join(root, "config", "vars.yaml"))
	assert.FileExists(t, filepath.Join(root, ".gitignore"))
	assert.NoFileExists(t, filepath.Join(root, "BRICKKIT.md"))
}

// 组件仓库里的补全式 init（提案 §16.1.1）与 add --local --init 是同一件事：只补 brickkit.yaml、
// deploy.yaml、config/，不建项目的 components/ 与 shell/，也不替它声明那两个本地源。
func TestPlanCompleteComponentRepoIsAWorkbench(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "BRICKKIT.md"), []byte("# erp/api\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("bin/\n"), 0o644))
	l := project.NewLayout(root)

	plan, err := project.PlanComplete(l, "erp-api")
	require.NoError(t, err)
	assert.False(t, plan.ProjectDocUnmanaged, "组件仓库里的 BRICKKIT.md 本来就是组件自己的文档")
	assert.NotContains(t, plan.GitignoreMissing, "components/", "组件仓库用不上 components/")
	assert.Contains(t, plan.GitignoreMissing, ".brickkit/")
	assert.NotContains(t, plan.Create, "shell/.gitkeep")
	require.NoError(t, plan.Apply(l))

	assert.NoDirExists(t, filepath.Join(root, "components"))
	assert.NoDirExists(t, filepath.Join(root, "shell"))
	decl, err := projfile.ParseFile(l.DeclPath())
	require.NoError(t, err)
	assert.Empty(t, decl.Sources, "安装源由作者自己加（骨架里有注释示例）")
	raw, err := os.ReadFile(l.DeclPath())
	require.NoError(t, err)
	assert.Contains(t, string(raw), "# - name: company-git")
}

// --name 与已有 brickkit.yaml 的 project 相矛盾：拒绝，而不是生成一份标题对不上的项目文档。
func TestProjectNameForRejectsContradictingFlag(t *testing.T) {
	root := t.TempDir()
	l := project.NewLayout(root)
	require.NoError(t, os.WriteFile(l.DeclPath(), []byte("project: declared\ncomponents: []\n"), 0o644))
	_, err := project.ProjectNameFor(l, "other")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "declared")
	name, err := project.ProjectNameFor(l, "declared")
	require.NoError(t, err)
	assert.Equal(t, "declared", name)
}

// 工作台的 brickkit.yaml 与其它骨架同样两格缩进。
func TestWorkbenchDeclUsesTwoSpaceIndent(t *testing.T) {
	root := t.TempDir()
	l := project.NewLayout(root)
	plan, err := project.PlanWorkbench(l, "x", []projfile.Source{{Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "../.."}})
	require.NoError(t, err)
	require.NoError(t, plan.Apply(l))
	raw, err := os.ReadFile(l.DeclPath())
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\nsources:\n  - name: local-dev\n")
}
