package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 文件权限：配置与备份 0644，目录 0755。凭据文件由 login 单独用 0600 写。
const (
	filePerm = 0o644
	dirPerm  = 0o755
)

// InitResult 是 InitProject 的执行结果，供命令层渲染输出。
type InitResult struct {
	// ProjectName 是写入配置的项目名称。
	ProjectName string
	// ConfigName 是配置文件名（如 brickkit.yaml）。
	ConfigName string
	// GitignoreUpdated 表示是否向 .gitignore 追加了内容。
	GitignoreUpdated bool
}

// InitProject 初始化一个 BrickKit 项目（004 §3.2）：
//
//  1. 校验项目名称
//  2. 确认目录尚未初始化
//  3. 创建 .brickkit/{manifests,artifacts,generated} 与 components/.archived
//  4. 写入配置骨架
//  5. 追加 .gitignore 规则
func InitProject(l Layout, project string) (*InitResult, error) {
	if err := ValidateProjectName(project); err != nil {
		return nil, err
	}
	if err := checkNotInitialized(l); err != nil {
		return nil, err
	}

	for _, dir := range l.ManagedDirs() {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return nil, wrapIOError(i18n.T(msgid.ActionMkdir), dir, err)
		}
	}

	content := Skeleton(project, l.ConfigName())
	if err := writeNewFile(l.ConfigPath(), content); err != nil {
		return nil, err
	}

	updated, err := EnsureGitignore(l.GitignorePath())
	if err != nil {
		return nil, err
	}

	return &InitResult{
		ProjectName:      project,
		ConfigName:       l.ConfigName(),
		GitignoreUpdated: updated,
	}, nil
}

// checkNotInitialized 确认目录中还没有 BrickKit 项目。
// 已有配置文件或 .brickkit 目录时报错，绝不覆盖用户已有配置。
func checkNotInitialized(l Layout) error {
	var existing []string
	if _, err := os.Stat(l.ConfigPath()); err == nil {
		existing = append(existing, l.ConfigName())
	}
	if _, err := os.Stat(l.BrickkitDir()); err == nil {
		existing = append(existing, DirBrickkit+"/")
	}
	if len(existing) == 0 {
		return nil
	}

	root, err := filepath.Abs(l.Root)
	if err != nil {
		root = l.Root
	}
	return clierr.New(clierr.CodeProjectExists, i18n.T(msgid.ConfigProjectExists)).
		WithDetail(i18n.T(msgid.LabelDir), root).
		WithDetail(i18n.T(msgid.ConfigLabelExisting), strings.Join(existing, i18n.T(msgid.ListSeparator))).
		WithHint(i18n.T(msgid.ConfigHintReinit, l.ConfigName(), DirBrickkit))
}

// writeNewFile 写入文件，已存在时报错（O_EXCL），避免覆盖用户数据。
func writeNewFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return wrapIOError(i18n.T(msgid.ActionWriteFile), path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(content); err != nil {
		return wrapIOError(i18n.T(msgid.ActionWriteFile), path, err)
	}
	return nil
}

func wrapIOError(action, path string, cause error) error {
	return clierr.New(clierr.CodeInternal, i18n.T(msgid.IOFailed, action)).
		WithDetail(i18n.T(msgid.LabelPath), path).
		WithDetail(i18n.T(msgid.LabelReason), cause.Error()).
		WithHint(i18n.T(msgid.HintCheckDiskAccess)).
		WithCause(cause)
}

// gitignoreSection 是 .gitignore 中的一段（一条注释 + 若干规则）。
type gitignoreSection struct {
	comment string
	rules   []string
}

// gitignoreSections 是 003 §11 建议的 .gitignore 内容。
//
// 是函数而不是包级变量：注释文字要跟着语言变，包初始化时语言还没确定。
// 判断"这一段已经在了没有"只看规则行（见 missingBlock），不看注释，
// 所以换语言重跑不会重复追加。
func gitignoreSections() []gitignoreSection {
	return []gitignoreSection{
		{"# " + i18n.T(msgid.ConfigGitignoreGenerated), []string{".brickkit/generated/"}},
		{"# " + i18n.T(msgid.ConfigGitignoreCredentials), []string{".brickkit/credentials"}},
		{"# " + i18n.T(msgid.ConfigGitignoreEnvFile), []string{".env"}},
		{"# " + i18n.T(msgid.ConfigGitignoreComponents), []string{"components/"}},
		// 这两条**默认是注释掉的**：契约与 Manifest 缓存默认跟着项目一起提交，
		// 团队共享同一份。取消注释才会忽略它们。
		{"# " + i18n.T(msgid.ConfigGitignoreCaches),
			[]string{"# .brickkit/artifacts/", "# .brickkit/manifests/"}},
	}
}

// EnsureGitignore 确保 .gitignore 含有 BrickKit 需要的忽略规则。
// 文件不存在时创建；已存在时只追加缺失的规则，不覆盖、不重复。
// 返回是否发生了写入。
func EnsureGitignore(path string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, wrapIOError(i18n.T(msgid.ConfigActionReadFile), path, err)
	}

	present := make(map[string]bool)
	for _, line := range strings.Split(string(existing), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			present[trimmed] = true
		}
	}

	block := missingBlock(present)
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
	b.WriteString(strings.Join(block, "\n"))
	b.WriteString("\n")

	if err := os.WriteFile(path, []byte(b.String()), filePerm); err != nil {
		return false, wrapIOError(i18n.T(msgid.ActionWriteFile), path, err)
	}
	return true, nil
}

// missingBlock 生成需要追加的行。已存在的规则被跳过；
// 某段的规则全部已存在时，整段（含注释）都不追加，避免留下孤立注释。
func missingBlock(present map[string]bool) []string {
	var lines []string
	for _, section := range gitignoreSections() {
		var missing []string
		for _, rule := range section.rules {
			if !present[rule] {
				missing = append(missing, rule)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, section.comment)
		lines = append(lines, missing...)
	}
	return lines
}
