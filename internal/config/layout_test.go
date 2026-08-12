package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLayoutPaths(t *testing.T) {
	l := NewLayout("/tmp/demo", DefaultConfigFile)

	assert.Equal(t, "/tmp/demo/brickkit.yaml", l.ConfigPath())
	assert.Equal(t, "brickkit.yaml", l.ConfigName())
	assert.Equal(t, "/tmp/demo/.brickkit", l.BrickkitDir())
	assert.Equal(t, "/tmp/demo/.brickkit/backup", l.BackupDir())
	assert.Equal(t, "/tmp/demo/.brickkit/manifests", l.ManifestsDir())
	assert.Equal(t, "/tmp/demo/.brickkit/artifacts", l.ArtifactsDir())
	assert.Equal(t, "/tmp/demo/.brickkit/generated", l.GeneratedDir())
	assert.Equal(t, "/tmp/demo/.brickkit/credentials", l.CredentialsPath())
	assert.Equal(t, "/tmp/demo/components", l.ComponentsDir())
	assert.Equal(t, "/tmp/demo/components/.archived", l.ArchivedDir())
	assert.Equal(t, "/tmp/demo/.gitignore", l.GitignorePath())
	// 003 §7.3：备份文件名 = 配置文件名 + .initial / .last
	assert.Equal(t, "/tmp/demo/.brickkit/backup/brickkit.yaml.initial", l.InitialBackupPath())
	assert.Equal(t, "/tmp/demo/.brickkit/backup/brickkit.yaml.last", l.LastBackupPath())
}

func TestNewLayoutDefaults(t *testing.T) {
	l := NewLayout("", "")
	assert.Equal(t, ".", l.Root)
	assert.Equal(t, DefaultConfigFile, l.ConfigFile)
	assert.Equal(t, DefaultConfigFile, l.ConfigPath())
}

// 多环境配置：--config 可以是别的文件名，备份名随之变化。
func TestLayoutCustomConfigFile(t *testing.T) {
	l := NewLayout("/tmp/demo", "brickkit.prod.yaml")
	assert.Equal(t, "/tmp/demo/brickkit.prod.yaml", l.ConfigPath())
	assert.Equal(t, "brickkit.prod.yaml", l.ConfigName())
	assert.Equal(t, "/tmp/demo/.brickkit/backup/brickkit.prod.yaml.initial", l.InitialBackupPath())
}

// --config 传绝对路径时原样使用，备份仍写在项目的 .brickkit 下（004 §3.5）。
func TestLayoutAbsoluteConfigFile(t *testing.T) {
	l := NewLayout("/tmp/demo", "/etc/brickkit/staging.yaml")
	assert.Equal(t, "/etc/brickkit/staging.yaml", l.ConfigPath())
	assert.Equal(t, "staging.yaml", l.ConfigName())
	assert.Equal(t, "/tmp/demo/.brickkit/backup/staging.yaml.initial", l.InitialBackupPath())
}

func TestManagedDirs(t *testing.T) {
	l := NewLayout("root", DefaultConfigFile)
	assert.Equal(t, []string{
		filepath.Join("root", ".brickkit"),
		filepath.Join("root", ".brickkit", "backup"),
		filepath.Join("root", ".brickkit", "manifests"),
		filepath.Join("root", ".brickkit", "artifacts"),
		filepath.Join("root", ".brickkit", "generated"),
		filepath.Join("root", "components"),
		filepath.Join("root", "components", ".archived"),
	}, l.ManagedDirs())
}
