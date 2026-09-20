package userconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirRespectsEnvOverride(t *testing.T) {
	t.Setenv(EnvDirOverride, "/tmp/example-brickkit-config")
	dir, err := Dir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/example-brickkit-config", dir)
}

func TestLoadWithoutFileReturnsEmptyConfigNotError(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Lang)
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())

	require.NoError(t, Save(&Config{Lang: "zh"}))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "zh", cfg.Lang)
}

func TestSaveOverwritesPreviousValue(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())

	require.NoError(t, Save(&Config{Lang: "zh"}))
	require.NoError(t, Save(&Config{Lang: "en"}))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "en", cfg.Lang)
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDirOverride, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0o644))

	_, err := Load()
	assert.Error(t, err)
}

func TestSaveCreatesDirIfMissing(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "does", "not", "exist", "yet")
	t.Setenv(EnvDirOverride, nested)

	require.NoError(t, Save(&Config{Lang: "en"}))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "en", cfg.Lang)
}

func TestPathIsInsideDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDirOverride, dir)

	path, err := Path()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "config.json"), path)
}
