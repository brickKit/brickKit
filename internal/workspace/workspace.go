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

// RemoveSource 删除组件活跃源码目录。目录不存在时返回 false（不是错误）。
func RemoveSource(l config.Layout, componentID string) (bool, error) {
	return removeDir(activeLoc(l, componentID))
}

// RemoveArchived 删除组件归档源码目录。目录不存在时返回 false（不是错误）。
//
// remove 必须连归档目录一起清：sync 把源码搬进 .archived/ 之后，组件又从
// brickkit.yaml 里被移除，sync 就再也不认识它了（planSync 只看配置里声明过的
// 组件）——不在这里删掉，那份源码就是永远没人回收的孤儿。
func RemoveArchived(l config.Layout, componentID string) (bool, error) {
	return removeDir(archivedLoc(l, componentID))
}

// removeDir 删掉一份组件源码目录，并收走随之变空的 scope 目录。
func removeDir(loc srcLoc) (bool, error) {
	info, err := os.Stat(loc.path)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	if err := os.RemoveAll(loc.path); err != nil {
		return false, clierr.New(clierr.CodeConfigInvalid, "错误：删除源码目录失败").
			WithDetail("目录", loc.display).
			WithDetail("原因", err.Error()).
			WithHint("检查目录权限，或手工删除后重试").
			WithCause(err)
	}
	loc.pruneEmptyScope()
	return true, nil
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

// ============================================================
// 归档 / 激活（004 §3.9，brickkit sync）
// ============================================================

// ArchivedDir 返回组件在归档目录中的路径。
func ArchivedDir(l config.Layout, componentID string) string {
	return filepath.Join(l.ArchivedDir(), filepath.FromSlash(componentID))
}

// DisplayArchivedDir 返回用于输出的归档相对路径。
func DisplayArchivedDir(componentID string) string {
	return config.DirComponents + "/" + config.DirArchived + "/" + componentID
}

// IsArchived 判断组件源码是否在归档目录里。
func IsArchived(l config.Layout, componentID string) bool {
	info, err := os.Stat(ArchivedDir(l, componentID))
	return err == nil && info.IsDir()
}

// Archive 把组件源码从 components/ 移到 components/.archived/。
func Archive(l config.Layout, componentID string) error {
	return move(activeLoc(l, componentID), archivedLoc(l, componentID), componentID)
}

// Activate 把组件源码从归档目录移回 components/。
func Activate(l config.Layout, componentID string) error {
	return move(archivedLoc(l, componentID), activeLoc(l, componentID), componentID)
}

// srcLoc 是一份组件源码目录的位置：活跃的或归档的。
//
// 带上 root 是为了 pruneEmptyScope——组件 ID 不含 scope 时，父目录就是 root
// 本身，components/ 和 .archived/ 空了也绝不能删。
type srcLoc struct {
	root    string
	path    string
	display string
}

func activeLoc(l config.Layout, componentID string) srcLoc {
	return srcLoc{l.ComponentsDir(), SourceDir(l, componentID), DisplayDir(componentID)}
}

func archivedLoc(l config.Layout, componentID string) srcLoc {
	return srcLoc{l.ArchivedDir(), ArchivedDir(l, componentID), DisplayArchivedDir(componentID)}
}

// pruneEmptyScope 删掉腾空了的 scope 目录（components/demo/ 里没东西了就别留着）。
func (loc srcLoc) pruneEmptyScope() {
	dir := filepath.Dir(loc.path)
	if dir == loc.root {
		return
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

// move 整目录搬家。
//
// 用 os.Rename 而不是复制：每个组件是一个独立的 Git 仓库，整目录移动才能
// 保住 .git（以及里面的 index、hooks、文件权限）。归档目录就在 components/
// 底下，同一个文件系统，Rename 不会跨设备失败。
func move(from, to srcLoc, componentID string) error {
	if _, err := os.Stat(to.path); err == nil {
		// 目标已存在：那里可能是使用者手工放的东西，绝不覆盖
		return clierr.New(clierr.CodeConfigInvalid, "错误：目标目录已存在，无法移动组件源码").
			WithDetail("组件", componentID).
			WithDetail("从", from.display).
			WithDetail("到", to.display).
			WithHint(
				"先检查目标目录里是什么，确认无用后删除或重命名它",
				"两处都有源码时，平台不替你决定保留哪一份",
			)
	}

	if err := os.MkdirAll(filepath.Dir(to.path), 0o755); err != nil {
		return moveError("创建目录", componentID, from.display, to.display, err)
	}
	if err := os.Rename(from.path, to.path); err != nil {
		return moveError("移动目录", componentID, from.display, to.display, err)
	}
	from.pruneEmptyScope()
	return nil
}

func moveError(action, componentID, from, to string, cause error) error {
	return clierr.Newf(clierr.CodeInternal, "错误：%s失败", action).
		WithDetail("组件", componentID).
		WithDetail("从", from).
		WithDetail("到", to).
		WithDetail("原因", cause.Error()).
		WithHint("检查目录权限与磁盘空间").
		WithCause(cause)
}
