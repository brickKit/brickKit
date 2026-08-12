package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
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
	// BackupPath 是初始备份的完整路径。
	BackupPath string
	// GitignoreUpdated 表示是否向 .gitignore 追加了内容。
	GitignoreUpdated bool
}

// InitProject 初始化一个 BrickKit 项目（004 §3.2）：
//
//  1. 校验项目名称
//  2. 确认目录尚未初始化
//  3. 创建 .brickkit/{backup,manifests,artifacts,generated} 与 components/.archived
//  4. 写入配置骨架
//  5. 写入初始备份（供 brickkit reset 使用）
//  6. 追加 .gitignore 规则
func InitProject(l Layout, project string) (*InitResult, error) {
	if err := ValidateProjectName(project); err != nil {
		return nil, err
	}
	if err := checkNotInitialized(l); err != nil {
		return nil, err
	}

	for _, dir := range l.ManagedDirs() {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return nil, wrapIOError("创建目录", dir, err)
		}
	}

	content := Skeleton(project, l.ConfigName())
	if err := writeNewFile(l.ConfigPath(), content); err != nil {
		return nil, err
	}
	if err := writeNewFile(l.InitialBackupPath(), content); err != nil {
		return nil, err
	}

	updated, err := EnsureGitignore(l.GitignorePath())
	if err != nil {
		return nil, err
	}

	return &InitResult{
		ProjectName:      project,
		ConfigName:       l.ConfigName(),
		BackupPath:       l.InitialBackupPath(),
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
	return clierr.New(clierr.CodeProjectExists, "错误：项目已初始化，无需重复执行 init").
		WithDetail("目录", root).
		WithDetail("已存在", strings.Join(existing, "、")).
		WithHint(
			fmt.Sprintf("如需重新初始化，请先删除 %s 与 %s/ 目录", l.ConfigName(), DirBrickkit),
			"如需恢复初始配置，执行 brickkit reset",
		)
}

// writeNewFile 写入文件，已存在时报错（O_EXCL），避免覆盖用户数据。
func writeNewFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return wrapIOError("写入文件", path, err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return wrapIOError("写入文件", path, err)
	}
	return nil
}

func wrapIOError(action, path string, cause error) error {
	return clierr.Newf(clierr.CodeInternal, "错误：%s失败", action).
		WithDetail("路径", path).
		WithDetail("原因", cause.Error()).
		WithHint("检查目录权限与磁盘空间").
		WithCause(cause)
}

// gitignoreSection 是 .gitignore 中的一段（一条注释 + 若干规则）。
type gitignoreSection struct {
	comment string
	rules   []string
}

// gitignoreSections 是 003 §11 建议的 .gitignore 内容。
var gitignoreSections = []gitignoreSection{
	{"# BrickKit CLI 生成的文件（不提交到 Git）", []string{".brickkit/generated/", ".brickkit/backup/"}},
	{"# 登录凭据（包含 Token）", []string{".brickkit/credentials"}},
	{"# 环境变量文件（包含密码）", []string{".env"}},
	{"# 组件源码目录（每个组件是独立的 Git 仓库，不提交到项目仓库）", []string{"components/"}},
	{"# API 契约缓存（可选，团队可共享则注释掉此行）", []string{"# .brickkit/artifacts/"}},
	{"# Manifest 缓存（可选，团队可共享则注释掉此行）", []string{"# .brickkit/manifests/"}},
}

// EnsureGitignore 确保 .gitignore 含有 BrickKit 需要的忽略规则。
// 文件不存在时创建；已存在时只追加缺失的规则，不覆盖、不重复。
// 返回是否发生了写入。
func EnsureGitignore(path string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, wrapIOError("读取文件", path, err)
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
		return false, wrapIOError("写入文件", path, err)
	}
	return true, nil
}

// missingBlock 生成需要追加的行。已存在的规则被跳过；
// 某段的规则全部已存在时，整段（含注释）都不追加，避免留下孤立注释。
func missingBlock(present map[string]bool) []string {
	var lines []string
	for _, section := range gitignoreSections {
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
