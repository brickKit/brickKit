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
	assert.True(t, plan.Agents.AgentsCreated)
	assert.True(t, plan.Agents.ClaudeCreated)

	for _, name := range []string{"brickkit.yaml", "deploy.yaml", "config/vars.yaml", "config/.gitkeep", "shell/.gitkeep", ".gitignore", "AGENTS.md", "CLAUDE.md"} {
		assert.FileExists(t, filepath.Join(root, name))
	}
	assert.NoFileExists(t, filepath.Join(root, "BRICKKIT.md"), "项目不再有项目级的 BRICKKIT.md：组件表在 AGENTS.md 末尾")
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(agents), "# my-shop\n"))
	assert.Contains(t, string(agents), "## Overview")
	assert.Contains(t, string(agents), "<!-- brickkit:managed:begin lang=")
	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, "@AGENTS.md\n", string(claude))
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

// 组件仓库兼作工作台：BRICKKIT.md 是组件自己的文档，补全不替作者写它；AGENTS.md 是组件的那五节，
// 标题是组件 ID，维护区放组件规则加工作台的组件表。
func TestPlanCompleteInComponentRepoLeavesBrickkitMd(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("metadata:\n  id: erp/backend\n  version: 1.0.0\n"), 0o644))

	complete(t, root, "erp-backend")
	assert.NoFileExists(t, filepath.Join(root, "BRICKKIT.md"))
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(agents), "# erp/backend\n"))
	assert.Contains(t, string(agents), "## Code map")
	assert.Contains(t, string(agents), "reserved name")
	assert.Contains(t, string(agents), "## Components")
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

// 组件目录的本地联调工作台：brickkit.yaml 继承给定的安装源，不建 components/、shell/
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
	raw, err := os.ReadFile(l.DeclPath())
	require.NoError(t, err)
	assert.Contains(t, string(raw), "inherited from the enclosing project", "安装源确实是继承来的：要说出来")
}

// 组件仓库里的补全式 init与 add --local --init 是同一件事：只补 brickkit.yaml、
// deploy.yaml、config/，不建项目的 components/ 与 shell/，也不替它声明那两个本地源。
func TestPlanCompleteComponentRepoIsAWorkbench(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "BRICKKIT.md"), []byte("# erp/api\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("bin/\n"), 0o644))
	l := project.NewLayout(root)

	plan, err := project.PlanComplete(l, "erp-api")
	require.NoError(t, err)
	assert.False(t, plan.ObsoleteMap, "组件仓库里的 BRICKKIT.md 本来就是组件自己的文档")
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
	// 这份文件是 init 在组件仓库里建的，不是 add --local --init，也没有继承谁的安装源
	assert.NotContains(t, string(raw), "--init")
	assert.NotContains(t, string(raw), "inherited")
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
