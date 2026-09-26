package project_test

import (
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
