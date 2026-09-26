// Package project 把一个三层文件项目（brickkit.yaml + deploy*.yaml + config/）装载成
// 一个 Project：之后所有命令只认这一个对象，谁也不再自己去读分层文件。
package project

import "path/filepath"

// 项目目录里的固定名字（提案 §4.1）。
const (
	FileDecl              = "brickkit.yaml"
	FileDeploy            = "deploy.yaml"
	FileDeployLocal       = "deploy.local.yaml"
	FileDeployLocalBackup = "deploy.local.yaml.bak"
	FileGitignore         = ".gitignore"
	FileProjectDoc        = "BRICKKIT.md"
	DirConfig             = "config"
	FileVars              = "vars.yaml"
	DirConfigArchive      = ".archive"
	DirBrickkit           = ".brickkit"
	DirManifests          = "manifests"
	DirArtifacts          = "artifacts"
	DirGenerated          = "generated"
	DirComponents         = "components"
	DirShell              = "shell"
	DirArchived           = ".archived"
	FileCredentials       = "credentials"
	FileSkillsLock        = "skills.lock"
	FileSessionLock       = "session.lock"
	// FileLocalMode 存在即"本地模式已开启"（附录 A16）：local off 只删它，
	// 不删 deploy.local.yaml，再开时本地改动还在。
	FileLocalMode = "local-mode"
)

// Layout 描述项目目录布局。所有路径都由 Root 推导，不依赖进程当前目录。
type Layout struct {
	Root string
}

// NewLayout 构建布局；root 为空时用当前目录。
func NewLayout(root string) Layout {
	if root == "" {
		root = "."
	}
	return Layout{Root: root}
}

func (l Layout) path(parts ...string) string {
	return filepath.Join(append([]string{l.Root}, parts...)...)
}

// Resolve 把使用者给的路径（如 -f deploy.prod.yaml）按项目根解析；绝对路径原样返回。
func (l Layout) Resolve(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return l.path(p)
}

func (l Layout) DeclPath() string              { return l.path(FileDecl) }
func (l Layout) DeployPath() string            { return l.path(FileDeploy) }
func (l Layout) DeployLocalPath() string       { return l.path(FileDeployLocal) }
func (l Layout) DeployLocalBackupPath() string { return l.path(FileDeployLocalBackup) }
func (l Layout) GitignorePath() string         { return l.path(FileGitignore) }
func (l Layout) ProjectDocPath() string        { return l.path(FileProjectDoc) }
func (l Layout) ConfigDir() string             { return l.path(DirConfig) }
func (l Layout) VarsPath() string              { return l.path(DirConfig, FileVars) }
func (l Layout) ConfigArchiveDir() string      { return l.path(DirConfig, DirConfigArchive) }
func (l Layout) BrickkitDir() string           { return l.path(DirBrickkit) }
func (l Layout) ManifestsDir() string          { return l.path(DirBrickkit, DirManifests) }
func (l Layout) ArtifactsDir() string          { return l.path(DirBrickkit, DirArtifacts) }
func (l Layout) GeneratedDir() string          { return l.path(DirBrickkit, DirGenerated) }
func (l Layout) CredentialsPath() string       { return l.path(DirBrickkit, FileCredentials) }
func (l Layout) SkillsLockPath() string        { return l.path(DirBrickkit, FileSkillsLock) }
func (l Layout) SessionLockPath() string       { return l.path(DirBrickkit, FileSessionLock) }
func (l Layout) LocalModePath() string         { return l.path(DirBrickkit, FileLocalMode) }
func (l Layout) ComponentsDir() string         { return l.path(DirComponents) }
func (l Layout) ShellDir() string              { return l.path(DirShell) }
func (l Layout) ArchivedDir() string           { return l.path(DirComponents, DirArchived) }
