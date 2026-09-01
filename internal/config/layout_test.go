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
	assert.Equal(t, "/tmp/demo/.brickkit/manifests", l.ManifestsDir())
	assert.Equal(t, "/tmp/demo/.brickkit/artifacts", l.ArtifactsDir())
	assert.Equal(t, "/tmp/demo/.brickkit/generated", l.GeneratedDir())
	assert.Equal(t, "/tmp/demo/.brickkit/credentials", l.CredentialsPath())
	assert.Equal(t, "/tmp/demo/components", l.ComponentsDir())
	assert.Equal(t, "/tmp/demo/components/.archived", l.ArchivedDir())
	assert.Equal(t, "/tmp/demo/.gitignore", l.GitignorePath())
}

func TestNewLayoutDefaults(t *testing.T) {
	l := NewLayout("", "")
	assert.Equal(t, ".", l.Root)
	assert.Equal(t, DefaultConfigFile, l.ConfigFile)
	assert.Equal(t, DefaultConfigFile, l.ConfigPath())
}

// 多环境配置：--config 可以是别的文件名。
func TestLayoutCustomConfigFile(t *testing.T) {
	l := NewLayout("/tmp/demo", "brickkit.prod.yaml")
	assert.Equal(t, "/tmp/demo/brickkit.prod.yaml", l.ConfigPath())
	assert.Equal(t, "brickkit.prod.yaml", l.ConfigName())
}

// --config 传绝对路径时原样使用，工作目录仍是项目的 .brickkit（004 §3.5）。
func TestLayoutAbsoluteConfigFile(t *testing.T) {
	l := NewLayout("/tmp/demo", "/etc/brickkit/staging.yaml")
	assert.Equal(t, "/etc/brickkit/staging.yaml", l.ConfigPath())
	assert.Equal(t, "staging.yaml", l.ConfigName())
	assert.Equal(t, "/tmp/demo/.brickkit", l.BrickkitDir())
}

func TestManagedDirs(t *testing.T) {
	l := NewLayout("root", DefaultConfigFile)
	assert.Equal(t, []string{
		filepath.Join("root", ".brickkit"),
		filepath.Join("root", ".brickkit", "manifests"),
		filepath.Join("root", ".brickkit", "artifacts"),
		filepath.Join("root", ".brickkit", "generated"),
		filepath.Join("root", "components"),
		filepath.Join("root", "components", ".archived"),
	}, l.ManagedDirs())
}

func TestSkillsLockPath(t *testing.T) {
	l := NewLayout("/proj", DefaultConfigFile)
	assert.Equal(t, filepath.Join("/proj", DirBrickkit, FileSkillsLock),
		l.SkillsLockPath())
}
