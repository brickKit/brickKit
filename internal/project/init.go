package project

// 本文件是 brickkit init 的创建原语：在一个空目录里写出能直接 `up` 的三层骨架。
// P7 在它之上加补全式（缺什么补什么）与收尾 lint。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

const (
	// DirSecrets 放 config 里 file:// 引用的密钥文件；永远不进 git。
	DirSecrets = ".secrets"
	// FileGitkeep 让空的 config/ 目录能被提交。
	FileGitkeep = ".gitkeep"
	// FileDotEnv 是 ${VAR} 的第二取值来源（进程环境之后）。
	FileDotEnv = ".env"
	// LocalSourceName 是 init 写进 brickkit.yaml 的默认本地源名字。
	LocalSourceName = "local-dev"

	initFilePerm = 0o644
	initDirPerm  = 0o755
)

// InitResult 是 Init 做了什么。
type InitResult struct {
	ProjectName      string
	GitignoreUpdated bool
}

// Init 在 l.Root 下创建新项目：brickkit.yaml、deploy.yaml、config/vars.yaml、
// .gitignore 的必需条目，以及 .brickkit/ 与 components/ 目录。
//
// 任何一个项目文件已经存在就整体拒绝、一个文件都不写：半套骨架比没有更难收拾。
// 不生成 deploy.local.yaml——那是 brickkit local on 按需复制出来的个人文件。
func Init(l Layout, name string) (*InitResult, error) {
	if err := projfile.ValidateProjectName(name); err != nil {
		return nil, err
	}
	if err := checkNotInitialized(l); err != nil {
		return nil, err
	}

	for _, dir := range []string{
		l.ManifestsDir(), l.ArtifactsDir(), l.GeneratedDir(), l.ComponentsDir(), l.ConfigDir(),
	} {
		if err := os.MkdirAll(dir, initDirPerm); err != nil {
			return nil, ioError(i18n.T(msgid.ActionMkdir), dir, err)
		}
	}
	files := []struct {
		path    string
		content string
	}{
		{l.DeclPath(), declSkeleton(name)},
		{l.DeployPath(), deploySkeleton()},
		{l.VarsPath(), yamlcomment.Block("", i18n.T(msgid.ProjectSkeletonVarsHeader))},
		{filepath.Join(l.ConfigDir(), FileGitkeep), ""},
	}
	for _, f := range files {
		if err := writeNewFile(f.path, f.content); err != nil {
			return nil, err
		}
	}

	updated, err := EnsureGitignore(l.GitignorePath())
	if err != nil {
		return nil, err
	}
	return &InitResult{ProjectName: name, GitignoreUpdated: updated}, nil
}

// declSkeleton 是 brickkit.yaml 的骨架。
func declSkeleton(name string) string {
	return yamlcomment.Block("", i18n.T(msgid.ProjectSkeletonDeclHeader)) +
		"project: " + name + "\n\n" +
		yamlcomment.Block("", i18n.T(msgid.ProjectSkeletonSources)) +
		"sources:\n" +
		"  - name: " + LocalSourceName + "\n" +
		"    type: local\n" +
		"    path: ./" + DirComponents + "      # " + i18n.T(msgid.ProjectSkeletonLocalDirNote) + "\n" +
		yamlcomment.Block("  ", i18n.T(msgid.ProjectSkeletonMoreSources)) +
		"  # - name: company-git\n" +
		"  #   type: git\n" +
		"  #   baseUrl: https://git.example.com/components\n" +
		"  # - name: market\n" +
		"  #   type: market\n" +
		"  #   url: https://market.example.com/api/v1\n\n" +
		"components: []\n"
}

// deploySkeleton 是 deploy.yaml 的骨架。
func deploySkeleton() string {
	return yamlcomment.Block("", i18n.T(msgid.ProjectSkeletonDeployHeader)) +
		"target: docker          # " + i18n.T(msgid.ProjectSkeletonDeployTarget) + "\n\n" +
		"components: []\n"
}

// checkNotInitialized 拒绝在已有项目文件的目录里再 init。
func checkNotInitialized(l Layout) error {
	var existing []string
	for _, name := range []string{FileDecl, FileDeploy, DirConfig, DirBrickkit} {
		if _, err := os.Stat(l.path(name)); err == nil {
			existing = append(existing, name)
		}
	}
	if len(existing) == 0 {
		return nil
	}
	root, err := filepath.Abs(l.Root)
	if err != nil {
		root = l.Root
	}
	list := strings.Join(existing, i18n.T(msgid.ListSeparator))
	return clierr.New(clierr.CodeProjectExists, i18n.T(msgid.ConfigProjectExists)).
		WithDetail(i18n.T(msgid.LabelDir), root).
		WithDetail(i18n.T(msgid.ConfigLabelExisting), list).
		WithHint(i18n.T(msgid.ProjectHintReinit, list))
}

func writeNewFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, initFilePerm)
	if err != nil {
		return ioError(i18n.T(msgid.ActionWriteFile), path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		return ioError(i18n.T(msgid.ActionWriteFile), path, err)
	}
	return nil
}

func ioError(action, path string, cause error) error {
	return clierr.New(clierr.CodeInternal, i18n.T(msgid.IOFailed, action)).
		WithDetail(i18n.T(msgid.LabelPath), path).
		WithDetail(i18n.T(msgid.LabelReason), cause.Error()).
		WithHint(i18n.T(msgid.HintCheckDiskAccess)).
		WithCause(cause)
}

// gitignoreSection 是 .gitignore 里带说明注释的一组规则。
type gitignoreSection struct {
	comment string
	rules   []string
}

// RequiredGitignore 是一个项目的 .gitignore 必须有的条目（提案 §11.5）：
// 漏了任何一条，个人文件或密钥就会被提交。P7 的补全式 init 与 lint 拿它做校验。
func RequiredGitignore() []string {
	var rules []string
	for _, s := range gitignoreSections() {
		rules = append(rules, s.rules...)
	}
	return rules
}

func gitignoreSections() []gitignoreSection {
	return []gitignoreSection{
		{i18n.T(msgid.ProjectGitignoreBrickkit), []string{DirBrickkit + "/"}},
		{i18n.T(msgid.ProjectGitignoreLocalDeploy), []string{FileDeployLocal, FileDeployLocalBackup}},
		{i18n.T(msgid.ProjectGitignoreSecrets), []string{DirSecrets + "/", FileDotEnv}},
		{i18n.T(msgid.ProjectGitignoreConfigArchive), []string{DirConfig + "/" + DirConfigArchive + "/"}},
		{i18n.T(msgid.ProjectGitignoreComponents), []string{DirComponents + "/"}},
	}
}

// EnsureGitignore 把缺的必需条目追加到 .gitignore 末尾，已有的一条不动。
// 返回这次是否真的改了文件。
func EnsureGitignore(path string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, ioError(i18n.T(msgid.ConfigActionReadFile), path, err)
	}
	present := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			present[trimmed] = true
		}
	}

	var block []string
	for _, s := range gitignoreSections() {
		var missing []string
		for _, rule := range s.rules {
			if !present[rule] {
				missing = append(missing, rule)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if len(block) > 0 {
			block = append(block, "")
		}
		block = append(block, "# "+s.comment)
		block = append(block, missing...)
	}
	if len(block) == 0 {
		return false, nil
	}

	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(block, "\n") + "\n")
	if err := os.WriteFile(path, []byte(b.String()), initFilePerm); err != nil {
		return false, ioError(i18n.T(msgid.ActionWriteFile), path, err)
	}
	return true, nil
}
