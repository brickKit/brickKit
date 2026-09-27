package project

// 本文件是 brickkit init 的补全原语（提案 §11.5）：对照完整项目的文件清单，缺的创建、
// 有的跳过。创建式只是"在一个新建的空目录里补全"。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
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
	// LocalShellSourceName 是外壳目录（shell/，提案 §9.6 外壳的目录约定）对应的本地源。
	LocalShellSourceName = "local-shells"

	initFilePerm = 0o644
	initDirPerm  = 0o755
)

// CompletePlan 是一次补全要做的事（提案 §11.5）：缺什么补什么，已有的不动。
// 创建式（init <name>）与补全式（init）、add --local --init 的子工作台都走它。
type CompletePlan struct {
	Name string
	// Create 与 Skip 是项目根下的相对路径（斜杠分隔），按固定顺序列出。
	Create, Skip []string
	// GitignoreCreate：.gitignore 不存在，整份写出。
	GitignoreCreate bool
	// GitignoreMissing：.gitignore 已存在但缺的必需条目——绝不替使用者改，只大声警告。
	GitignoreMissing []string
	// ProjectDoc：要生成项目 BRICKKIT.md（目录里是组件仓库时不生成，§16.1.1）。
	ProjectDoc bool
	// ProjectDocUnmanaged：已有的 BRICKKIT.md 没有 CLI 维护区，组件表不会自动更新。
	ProjectDocUnmanaged bool

	// sources 非 nil 时是组件目录里的本地联调工作台（PlanWorkbench）：brickkit.yaml 用这些安装源，
	// 不建 components/ 与 shell/——那是项目的目录约定，组件仓库里用不上。
	sources []projfile.Source
}

// projectFile 是补全清单里的一个文件：rel 是项目根下的相对路径，content 在 Apply 时才生成。
type projectFile struct {
	rel     string
	content func(name string) string
}

func projectFiles(workbench bool) []projectFile {
	files := []projectFile{
		{FileDecl, declSkeleton},
		{FileDeploy, func(string) string { return deploySkeleton() }},
		{DirConfig + "/" + FileVars, func(string) string {
			return yamlcomment.Block("", i18n.T(msgid.ProjectSkeletonVarsHeader))
		}},
		{DirConfig + "/" + FileGitkeep, func(string) string { return "" }},
	}
	if !workbench {
		files = append(files, projectFile{DirShell + "/" + FileGitkeep, func(string) string { return "" }})
	}
	return files
}

// PlanComplete 对照完整项目的文件清单，算出 l.Root 下缺哪些、有哪些。不写任何文件。
func PlanComplete(l Layout, name string) (*CompletePlan, error) {
	return planComplete(l, name, nil)
}

// PlanWorkbench 是组件目录里的本地联调工作台（提案 §9.6.1、§16.1.1）：与补全相同，
// 只是 brickkit.yaml 的安装源取 sources（add --local --init 从顶层项目继承、改写好路径的那一份）。
func PlanWorkbench(l Layout, name string, sources []projfile.Source) (*CompletePlan, error) {
	if sources == nil {
		sources = []projfile.Source{}
	}
	return planComplete(l, name, sources)
}

func planComplete(l Layout, name string, sources []projfile.Source) (*CompletePlan, error) {
	if err := projfile.ValidateProjectName(name); err != nil {
		return nil, err
	}
	plan := &CompletePlan{Name: name, sources: sources}
	for _, f := range projectFiles(plan.workbench()) {
		if exists(l.path(filepath.FromSlash(f.rel))) {
			plan.Skip = append(plan.Skip, f.rel)
		} else {
			plan.Create = append(plan.Create, f.rel)
		}
	}

	existing, err := os.ReadFile(l.GitignorePath())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		plan.GitignoreCreate = true
	case err != nil:
		return nil, ioError(i18n.T(msgid.ConfigActionReadFile), l.GitignorePath(), err)
	default:
		plan.GitignoreMissing = missingGitignore(existing)
	}

	switch {
	case exists(l.ProjectDocPath()):
		if data, err := os.ReadFile(l.ProjectDocPath()); err == nil && !hasManagedBlock(string(data)) {
			plan.ProjectDocUnmanaged = true
		}
	case !exists(filepath.Join(l.Root, manifest.FileName)):
		plan.ProjectDoc = true
	}
	return plan, nil
}

func (p *CompletePlan) workbench() bool { return p.sources != nil }

// Apply 按计划写文件，并建好 CLI 自己的工作目录（mkdir -p，已有的不动）。
func (p *CompletePlan) Apply(l Layout) error {
	dirs := []string{l.ManifestsDir(), l.ArtifactsDir(), l.GeneratedDir(), l.ConfigDir()}
	if !p.workbench() {
		dirs = append(dirs, l.ComponentsDir(), l.ShellDir())
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, initDirPerm); err != nil {
			return ioError(i18n.T(msgid.ActionMkdir), dir, err)
		}
	}
	for _, f := range projectFiles(p.workbench()) {
		if !slices.Contains(p.Create, f.rel) {
			continue
		}
		content := f.content(p.Name)
		if f.rel == FileDecl && p.workbench() {
			decl, err := workbenchDecl(p.Name, p.sources)
			if err != nil {
				return err
			}
			content = decl
		}
		if err := writeNewFile(l.path(filepath.FromSlash(f.rel)), content); err != nil {
			return err
		}
	}
	if p.GitignoreCreate {
		if err := writeNewFile(l.GitignorePath(), gitignoreContent()); err != nil {
			return err
		}
	}
	if p.ProjectDoc {
		if err := writeNewFile(l.ProjectDocPath(), projectDocHead(p.Name)+managedBlock("")); err != nil {
			return err
		}
	}
	return nil
}

// ProjectNameFor 决定补全式的项目名：--name > 已有 brickkit.yaml 的 project > 目录名（规整成合法名字）。
func ProjectNameFor(l Layout, flag string) (string, error) {
	if flag != "" {
		return flag, projfile.ValidateProjectName(flag)
	}
	if decl, err := projfile.ParseFile(l.DeclPath()); err == nil && decl.Project != "" {
		return decl.Project, nil
	}
	root, err := filepath.Abs(l.Root)
	if err != nil {
		root = l.Root
	}
	base := filepath.Base(root)
	if projfile.ProjectNameProblem(base) == "" {
		return base, nil
	}
	if s := projfile.SuggestProjectName(base); s != "" && projfile.ProjectNameProblem(s) == "" {
		return s, nil
	}
	return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.ProjectNameFromDirInvalid, base)).
		WithDetail(i18n.T(msgid.ConfigLabelNamingRule), projfile.ProjectNameRule()).
		WithHint(i18n.T(msgid.ProjectHintPassName)).
		WithExit(clierr.ExitUsage)
}

// DirIsEmpty 报告目录里是否除了 .git 之外什么都没有（补全前要不要先确认）。
func DirIsEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Name() != ".git" {
			return false, nil
		}
	}
	return true, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
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
		"  - name: " + LocalShellSourceName + "\n" +
		"    type: local\n" +
		"    path: ./" + DirShell + "           # " + i18n.T(msgid.ProjectSkeletonShellDirNote) + "\n" +
		yamlcomment.Block("  ", i18n.T(msgid.ProjectSkeletonMoreSources)) +
		"  # - name: company-git\n" +
		"  #   type: git\n" +
		"  #   baseUrl: https://git.example.com/components\n" +
		"  # - name: market\n" +
		"  #   type: market\n" +
		"  #   url: https://market.example.com/api/v1\n\n" +
		"components: []\n"
}

// workbenchDecl 是组件工作台的 brickkit.yaml：继承来的安装源，组件列表由之后的 add 填。
func workbenchDecl(name string, sources []projfile.Source) (string, error) {
	data, err := yaml.Marshal(struct {
		Sources []projfile.Source `yaml:"sources"`
	}{sources})
	if err != nil {
		return "", err
	}
	return yamlcomment.Block("", i18n.T(msgid.ProjectSkeletonWorkbenchHeader)) +
		"project: " + name + "\n\n" + string(data) + "\ncomponents: []\n", nil
}

// deploySkeleton 是 deploy.yaml 的骨架。
func deploySkeleton() string {
	return yamlcomment.Block("", i18n.T(msgid.ProjectSkeletonDeployHeader)) +
		"target: docker          # " + i18n.T(msgid.ProjectSkeletonDeployTarget) + "\n\n" +
		"components: []\n"
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
// 漏了任何一条，个人文件或密钥就会被提交。补全式 init 拿它做校验。
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

// gitignoreContent 是新项目的整份 .gitignore，每组规则带说明注释。
func gitignoreContent() string {
	var block []string
	for _, s := range gitignoreSections() {
		if len(block) > 0 {
			block = append(block, "")
		}
		block = append(block, "# "+s.comment)
		block = append(block, s.rules...)
	}
	return strings.Join(block, "\n") + "\n"
}

// missingGitignore 列出已有 .gitignore 里缺的必需条目（逐行精确匹配，忽略首尾空白）。
func missingGitignore(existing []byte) []string {
	present := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			present[trimmed] = true
		}
	}
	var missing []string
	for _, rule := range RequiredGitignore() {
		if !present[rule] {
			missing = append(missing, rule)
		}
	}
	return missing
}
