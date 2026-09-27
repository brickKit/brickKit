package project_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/project"
)

func TestLayoutPaths(t *testing.T) {
	l := project.NewLayout("/p")
	assert.Equal(t, "/p/brickkit.yaml", l.DeclPath())
	assert.Equal(t, "/p/deploy.yaml", l.DeployPath())
	assert.Equal(t, "/p/deploy.local.yaml", l.DeployLocalPath())
	assert.Equal(t, "/p/deploy.local.yaml.bak", l.DeployLocalBackupPath())
	assert.Equal(t, "/p/config", l.ConfigDir())
	assert.Equal(t, "/p/config/vars.yaml", l.VarsPath())
	assert.Equal(t, "/p/config/.archive", l.ConfigArchiveDir())
	assert.Equal(t, "/p/.brickkit/local-mode", l.LocalModePath())
	assert.Equal(t, "/p/shell", l.ShellDir())
	assert.Equal(t, "/p/deploy.prod.yaml", l.Resolve("deploy.prod.yaml"))
	assert.Equal(t, "/abs/d.yaml", l.Resolve("/abs/d.yaml"))
	assert.Equal(t, ".", project.NewLayout("").Root)
}

func TestLocalModeToggle(t *testing.T) {
	l := project.NewLayout(t.TempDir())
	on, err := project.LocalModeOn(l)
	require.NoError(t, err)
	assert.False(t, on)

	require.NoError(t, project.SetLocalMode(l, true))
	on, err = project.LocalModeOn(l)
	require.NoError(t, err)
	assert.True(t, on)
	assert.FileExists(t, filepath.Join(l.Root, ".brickkit", "local-mode"))

	require.NoError(t, project.SetLocalMode(l, false))
	require.NoError(t, project.SetLocalMode(l, false)) // 关两次不报错
	on, err = project.LocalModeOn(l)
	require.NoError(t, err)
	assert.False(t, on)
}

// Manifest 缓存每个版本一个目录（提案 §9.4），CachedVersions 只认真有 component.yaml 的精确版本目录。
func TestManifestCacheLayout(t *testing.T) {
	l := project.NewLayout(t.TempDir())
	assert.Equal(t, filepath.Join(l.ManifestsDir(), "erp", "api", "1.0.0", "component.yaml"), l.CachedManifestPath("erp/api", "1.0.0"))
	assert.Equal(t, filepath.Join(l.ManifestsDir(), "erp", "api", "1.0.0", "signature.json"), l.CachedSignaturePath("erp/api", "1.0.0"))
	assert.Equal(t, filepath.Join(l.ManifestsDir(), "erp", "api", "1.0.0", "BRICKKIT.md"), l.CachedDocPath("erp/api", "1.0.0"))

	for _, v := range []string{"1.0.0", "1.10.0"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(l.CachedManifestPath("erp/api", v)), 0o755))
		require.NoError(t, os.WriteFile(l.CachedManifestPath("erp/api", v), []byte("x"), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(l.ManifestsDir(), "erp", "api", "latest"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(l.ManifestsDir(), "erp", "api", "2.0.0"), 0o755), "空目录不算")
	assert.Equal(t, []string{"1.0.0", "1.10.0"}, l.CachedVersions("erp/api"))
	assert.Empty(t, l.CachedVersions("erp/none"))
}
