// Package workspace 管理 components/ 下的组件源码工作区。
//
// 设计依据：003 §1.4 目录结构、004 §3.3（--repo / --repo-all）、§3.4（remove 删除源码目录）。
//
// 约定：一个组件 ID 只有一个源码目录 `components/<scope>/<name>/`，与版本无关。
// 因此同 ID 的多个版本共用一份源码目录，只有最后一个版本被移除时才删除它。
package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// SourceDir 返回组件源码目录的完整路径。
func SourceDir(l config.Layout, componentID string) string {
	return filepath.Join(l.ComponentsDir(), filepath.FromSlash(componentID))
}

// DisplayDir 返回用于输出的相对路径，如 components/people/basic/。
func DisplayDir(componentID string) string {
	return config.DirComponents + "/" + componentID + "/"
}

// Exists 判断组件源码目录是否已存在。
func Exists(l config.Layout, componentID string) bool {
	info, err := os.Stat(SourceDir(l, componentID))
	return err == nil && info.IsDir()
}

// Clone 把开源组件的完整 Git 仓库 clone 到 components/<scope>/<name>/（004 §3.3）。
//
// 目标目录已存在时报错阻断：那里可能是使用者正在开发的源码，绝不能覆盖。
func Clone(ctx context.Context, l config.Layout, componentID, ref, gitURL string) (string, error) {
	target := SourceDir(l, componentID)
	if Exists(l, componentID) {
		return "", clierr.New(clierr.CodeCloneFailed, "clone 失败：目录已存在").
			WithDetail("组件", ref).
			WithDetail("目录", DisplayDir(componentID)).
			WithDetail("原因", "该目录已存在，可能包含你正在开发的组件源码").
			WithHint(
				"如果是误操作，请先删除或重命名该目录",
				"如果已有源码，无需再次 clone",
			)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", clierr.New(clierr.CodeCloneFailed, "错误：无法创建源码目录").
			WithDetail("目录", DisplayDir(componentID)).
			WithDetail("原因", err.Error()).
			WithCause(err)
	}

	// 完整 clone（不加 --depth）：使用者要能在这份仓库里改代码、提交、推送。
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", gitURL, target)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(target)
		return "", clierr.New(clierr.CodeCloneFailed, "错误：clone 失败").
			WithDetail("组件", ref).
			WithDetail("仓库", gitURL).
			WithDetail("原因", firstLine(string(out), err)).
			WithHint(
				"检查网络连接与仓库地址是否正确",
				"确认对该仓库有访问权限（私有仓库需配置 Git 凭据）",
			).WithCause(err)
	}
	return target, nil
}

// RemoveSource 删除组件源码目录。目录不存在时返回 false（不是错误）。
func RemoveSource(l config.Layout, componentID string) (bool, error) {
	if !Exists(l, componentID) {
		return false, nil
	}
	target := SourceDir(l, componentID)
	if err := os.RemoveAll(target); err != nil {
		return false, clierr.New(clierr.CodeConfigInvalid, "错误：删除源码目录失败").
			WithDetail("目录", DisplayDir(componentID)).
			WithDetail("原因", err.Error()).
			WithHint("检查目录权限，或手工删除后重试").
			WithCause(err)
	}
	// scope 目录空了就一并收走，避免 components/ 下堆积空壳
	pruneEmptyParent(l, componentID)
	return true, nil
}

// pruneEmptyParent 在 scope 目录为空时删除它。
func pruneEmptyParent(l config.Layout, componentID string) {
	scope, _, ok := strings.Cut(componentID, "/")
	if !ok || scope == "" {
		return
	}
	dir := filepath.Join(l.ComponentsDir(), scope)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	_ = os.Remove(dir)
}

// firstLine 取命令输出的首行作为原因；没有输出时回落到 error 本身。
func firstLine(out string, err error) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}
