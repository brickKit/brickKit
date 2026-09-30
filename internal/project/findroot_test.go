package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 向上找最近的 brickkit.yaml：像 git 找 .git，但不在嵌套的 .git 处停下——components/ 下的组件
// 往往自己就是一个 git 仓库。
func TestFindRootWalksUpToTheNearestProject(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, FileDecl), []byte("project: shop\n"), 0o644))
	deep := filepath.Join(root, "components", "erp", "api", "internal")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "components", "erp", "api", ".git"), 0o755))
	require.NoError(t, os.MkdirAll(deep, 0o755))

	got, found, err := FindRoot(deep)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, root, got)
}

// 就近原则：工作台（组件目录里自己的 brickkit.yaml）就是项目，不再往上找。
func TestFindRootStopsAtAWorkbench(t *testing.T) {
	root := t.TempDir()
	bench := filepath.Join(root, "components", "erp", "api")
	require.NoError(t, os.MkdirAll(bench, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, FileDecl), []byte("project: shop\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(bench, FileDecl), []byte("project: erp-api\n"), 0o644))

	got, found, err := FindRoot(bench)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, bench, got)
}

func TestFindRootReportsNothingOutsideAProject(t *testing.T) {
	_, found, err := FindRoot(t.TempDir())
	require.NoError(t, err)
	assert.False(t, found)
}
