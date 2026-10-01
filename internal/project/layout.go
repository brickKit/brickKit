// Package project 把一个三层文件项目（brickkit.yaml + deploy*.yaml + config/）装载成
// 一个 Project：之后所有命令只认这一个对象，谁也不再自己去读分层文件。
package project

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/projfile"
)

// 项目目录里的固定名字。
const (
	FileDecl              = projfile.FileName
	FileDeploy            = deployfile.FileTeam
	FileDeployLocal       = deployfile.FileLocal
	FileDeployLocalBackup = deployfile.FileLocal + ".bak"
	FileGitignore         = ".gitignore"
	DirConfig             = "config"
	FileVars              = configdir.VarsFile
	DirConfigArchive      = configdir.ArchiveDir
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
	// FileLocalMode 存在即"本地模式已开启"：local off 只删它，
	// 不删 deploy.local.yaml，再开时本地改动还在。
	FileLocalMode = "local-mode"
	// FileLocalBase 是 deploy.local.yaml 上次从 deploy.yaml 复制时的那份团队文件（local on / refresh 写）。
	// refresh 拿它分辨"你在本地改了什么"：与它不同的值、它有而你删掉的字段；没有它时退回两方对比。
	FileLocalBase = "deploy.local.base.yaml"
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
func (l Layout) LocalBasePath() string         { return l.path(DirBrickkit, FileLocalBase) }
func (l Layout) ComponentsDir() string         { return l.path(DirComponents) }
func (l Layout) ShellDir() string              { return l.path(DirShell) }
func (l Layout) ArchivedDir() string           { return l.path(DirComponents, DirArchived) }

// Manifest 缓存里每个版本目录下的文件。
const (
	FileCachedManifest  = "component.yaml"
	FileCachedSignature = "signature.json"
	FileCachedDoc       = "BRICKKIT.md"
)

// CachedManifestDir 是一个组件版本的永久缓存目录：.brickkit/manifests/<scope>/<name>/<version>/。
// 精确版本不可变，缓存从不过期，也从不删除。
func (l Layout) CachedManifestDir(id, version string) string {
	return filepath.Join(l.ManifestsDir(), filepath.FromSlash(id), version)
}

// CachedManifestPath 是缓存的 component.yaml。
func (l Layout) CachedManifestPath(id, version string) string {
	return filepath.Join(l.CachedManifestDir(id, version), FileCachedManifest)
}

// CachedSignaturePath 是缓存旁边的来源与签名信封。
func (l Layout) CachedSignaturePath(id, version string) string {
	return filepath.Join(l.CachedManifestDir(id, version), FileCachedSignature)
}

// CachedDocPath 是缓存的组件文档 BRICKKIT.md（组件带着时才有）。
func (l Layout) CachedDocPath(id, version string) string {
	return filepath.Join(l.CachedManifestDir(id, version), FileCachedDoc)
}

// AgentsPath 是根目录的 AGENTS.md：项目（或组件）自己的 AI 导读，末尾一段由 CLI 维护。
func (l Layout) AgentsPath() string { return l.path(docspec.FileAgents) }

// CachedDocLangs 是缓存里这个组件版本带着的文档：有没有原文 BRICKKIT.md，以及各译本的语言（排好序）。
// 名字不是合法译本的文件（BRICKKIT.zh-CN.md）不算。
func (l Layout) CachedDocLangs(id, version string) (hasPrimary bool, langs []string) {
	entries, _ := os.ReadDir(l.CachedManifestDir(id, version))
	for _, e := range entries {
		base, lang, ok := docspec.SplitTranslation(e.Name())
		if !ok || base != docspec.FileBrickkit {
			continue
		}
		if lang == "" {
			hasPrimary = true
		} else {
			langs = append(langs, lang)
		}
	}
	sort.Strings(langs)
	return hasPrimary, langs
}

// CachedVersions 列出缓存里这个组件有 component.yaml 的精确版本，从低到高。
func (l Layout) CachedVersions(id string) []string {
	entries, err := os.ReadDir(filepath.Join(l.ManifestsDir(), filepath.FromSlash(id)))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !manifest.IsExactVersion(e.Name()) {
			continue
		}
		if _, err := os.Stat(l.CachedManifestPath(id, e.Name())); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Slice(out, func(i, j int) bool { return manifest.CompareVersions(out[i], out[j]) < 0 })
	return out
}
