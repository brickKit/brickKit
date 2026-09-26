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
)

// init 生成的三层骨架必须能原样通过装载——否则第一条 up 就报错。
func TestInitCreatesLoadableThreeLayers(t *testing.T) {
	root := t.TempDir()

	res, err := project.Init(project.NewLayout(root), "my-shop")
	require.NoError(t, err)
	assert.Equal(t, "my-shop", res.ProjectName)

	for _, name := range []string{"brickkit.yaml", "deploy.yaml", "config/vars.yaml", "config/.gitkeep"} {
		assert.FileExists(t, filepath.Join(root, name))
	}
	assert.NoFileExists(t, filepath.Join(root, "deploy.local.yaml"), "本地文件由 local on 按需生成")
	for _, dir := range []string{".brickkit/manifests", ".brickkit/generated", "components"} {
		assert.DirExists(t, filepath.Join(root, dir))
	}

	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	assert.Equal(t, "my-shop", p.Decl.Project)
	assert.Equal(t, "docker", p.Deploy.Target)
	assert.Empty(t, p.Warnings)
}

// 这几条漏了，个人文件与密钥就会被提交进 git。
func TestInitGitignoreGuardsPersonalFiles(t *testing.T) {
	root := t.TempDir()
	_, err := project.Init(project.NewLayout(root), "my-shop")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	lines := strings.Split(string(data), "\n")
	for _, want := range []string{"deploy.local.yaml", "deploy.local.yaml.bak", ".secrets/", ".brickkit/", "config/.archive/", ".env"} {
		assert.Contains(t, lines, want)
	}
	assert.NotContains(t, lines, "override.yaml")
}

func TestInitRefusesExistingProject(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "deploy.yaml"), []byte("target: docker\n"), 0o644))

	_, err := project.Init(project.NewLayout(root), "my-shop")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeProjectExists, clierr.As(err).Code)
	assert.NoFileExists(t, filepath.Join(root, "brickkit.yaml"), "拒绝时一个文件都不写")
}

func TestInitRejectsBadName(t *testing.T) {
	_, err := project.Init(project.NewLayout(t.TempDir()), "My Shop")
	require.Error(t, err)
}
