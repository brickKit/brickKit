// Package config 负责 BrickKit 项目配置（brickkit.yaml）的目录布局、骨架生成、
// 解析与校验。
//
// 设计依据：003 项目配置规范、004 §8 项目目录结构。
package config

import (
	"path/filepath"
)

// 项目目录布局中的固定名称（003 §1.4、004 §8.1）。
const (
	// DefaultConfigFile 是默认的项目配置文件名。
	DefaultConfigFile = "brickkit.yaml"
	// DirBrickkit 是 CLI 工作目录。
	DirBrickkit = ".brickkit"
	// DirBackup 存放配置备份（003 §7.3）。
	DirBackup = "backup"
	// DirManifests 存放 Manifest 缓存。
	DirManifests = "manifests"
	// DirArtifacts 存放 API 契约及其他产物缓存。
	DirArtifacts = "artifacts"
	// DirGenerated 存放 CLI 生成的部署文件（勿手动编辑）。
	DirGenerated = "generated"
	// DirComponents 是组件源码工作区。
	DirComponents = "components"
	// DirArchived 是组件源码归档目录（brickkit sync 管理）。
	DirArchived = ".archived"
	// FileCredentials 是登录凭据（brickkit login 生成）。
	FileCredentials = "credentials"
	// FileGitignore 是项目的 .gitignore。
	FileGitignore = ".gitignore"

	// SuffixInitialBackup 是 init 时的初始备份后缀（供 brickkit reset 使用）。
	SuffixInitialBackup = ".initial"
	// SuffixLastBackup 是 add / remove 前的备份后缀。
	SuffixLastBackup = ".last"
)

// Layout 描述一个 BrickKit 项目的目录布局。
//
// 所有路径都由 Root 与配置文件名推导，不依赖进程当前目录，
// 因此同一进程内可以安全地操作多个项目（测试与嵌套调用都需要）。
type Layout struct {
	// Root 是项目根目录。
	Root string
	// ConfigFile 是配置文件路径。相对路径按 Root 解析，绝对路径原样使用。
	ConfigFile string
}

// NewLayout 构建布局。root 为空时用当前目录（"."），
// configFile 为空时用默认的 brickkit.yaml。
func NewLayout(root, configFile string) Layout {
	if root == "" {
		root = "."
	}
	if configFile == "" {
		configFile = DefaultConfigFile
	}
	return Layout{Root: root, ConfigFile: configFile}
}

// path 把相对项目根的若干段拼成路径。
func (l Layout) path(parts ...string) string {
	return filepath.Join(append([]string{l.Root}, parts...)...)
}

// ConfigPath 返回项目配置文件的完整路径。
func (l Layout) ConfigPath() string {
	if filepath.IsAbs(l.ConfigFile) {
		return l.ConfigFile
	}
	return l.path(l.ConfigFile)
}

// ConfigName 返回配置文件名（不含目录），用于备份命名与输出展示。
func (l Layout) ConfigName() string { return filepath.Base(l.ConfigFile) }

// BrickkitDir 返回 .brickkit 目录。
func (l Layout) BrickkitDir() string { return l.path(DirBrickkit) }

// BackupDir 返回配置备份目录。
func (l Layout) BackupDir() string { return l.path(DirBrickkit, DirBackup) }

// ManifestsDir 返回 Manifest 缓存目录。
func (l Layout) ManifestsDir() string { return l.path(DirBrickkit, DirManifests) }

// ArtifactsDir 返回产物缓存目录。
func (l Layout) ArtifactsDir() string { return l.path(DirBrickkit, DirArtifacts) }

// GeneratedDir 返回生成的部署文件目录。
func (l Layout) GeneratedDir() string { return l.path(DirBrickkit, DirGenerated) }

// CredentialsPath 返回登录凭据文件路径。
func (l Layout) CredentialsPath() string { return l.path(DirBrickkit, FileCredentials) }

// ComponentsDir 返回组件源码工作区目录。
func (l Layout) ComponentsDir() string { return l.path(DirComponents) }

// ArchivedDir 返回组件源码归档目录。
func (l Layout) ArchivedDir() string { return l.path(DirComponents, DirArchived) }

// GitignorePath 返回项目 .gitignore 路径。
func (l Layout) GitignorePath() string { return l.path(FileGitignore) }

// InitialBackupPath 返回初始备份路径，如 .brickkit/backup/brickkit.yaml.initial。
func (l Layout) InitialBackupPath() string {
	return filepath.Join(l.BackupDir(), l.ConfigName()+SuffixInitialBackup)
}

// LastBackupPath 返回上一次操作前的备份路径。
func (l Layout) LastBackupPath() string {
	return filepath.Join(l.BackupDir(), l.ConfigName()+SuffixLastBackup)
}

// ManagedDirs 返回 brickkit init 需要创建的全部目录（按创建顺序）。
func (l Layout) ManagedDirs() []string {
	return []string{
		l.BrickkitDir(),
		l.BackupDir(),
		l.ManifestsDir(),
		l.ArtifactsDir(),
		l.GeneratedDir(),
		l.ComponentsDir(),
		l.ArchivedDir(),
	}
}
