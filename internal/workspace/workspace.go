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

// State 是一个组件的源码目录当前在哪。
type State int

const (
	// StateMissing 表示两处都没有这个组件的源码。
	StateMissing State = iota
	// StateActive 表示源码在 components/<scope>/<name>/。
	StateActive
	// StateArchived 表示源码被 brickkit sync 收进了 components/.archived/。
	StateArchived
)

// Locate 回答"这个组件的源码在哪"。
//
// # 为什么要有一个统一的入口，而不是两个各查一半的布尔
//
// 004 §8.1 立了一条不变量：**一个组件 ID 在 components/ 下只能有一个源码目录**。
// 从前守这条的两处（Clone 与命令层的预检）都只 stat 活跃目录，归档目录在
// 它们眼里根本不存在——于是 `sync` 归档之后再 `add --repo`，会往活跃目录
// 再 clone 一份，报"✅ 已 clone"，而下一次 `sync` 就卡在
// "目标目录已存在，无法移动组件源码"上，只剩手工 rm 一条路。
//
// 根子上是 004 §3.9 那句"**归档只改变看不看得见，不改变取不取得到**"
// 只被执行了一半：安装源按组件 ID 查找时会回落到归档目录（否则归档过的
// 组件会让 up 与 sync 双双失败），而"有没有源码"这一问没有回落。
// 同一个概念，两条路各理解了一半。
//
// 收成一个函数之后，那条不变量有了唯一的落点。
func Locate(l config.Layout, componentID string) State {
	switch {
	case isDir(SourceDir(l, componentID)):
		return StateActive
	case isDir(ArchivedDir(l, componentID)):
		return StateArchived
	default:
		return StateMissing
	}
}

// Exists 判断组件源码是否在**活跃**目录里。
func Exists(l config.Layout, componentID string) bool {
	return Locate(l, componentID) == StateActive
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ExistingSourceError 在组件已经有源码时给出该说的那句话，两处都没有时返回 nil。
//
// 导出是因为**同一个判断要在两个时点各做一次**：命令层在改 brickkit.yaml
// **之前**先做一遍（失败时不留下"配置写了一半"的现场），Clone 自己再兜一次。
// 两处必须给出同一句话，所以只写一遍。
//
// 两种已存在的说法刻意不同——它们要的下一步动作不一样：
//
//	活跃目录里有   多半是使用者自己的源码。平台不替他决定删不删
//	归档目录里有   源码没丢，只是被 sync 收起来了（004 §3.9）。
//	               他要的其实是 `brickkit sync`，不是再 clone 一份
//
// 从前只查活跃目录，于是第二种一路绿灯：归档之后再 add --repo 会在活跃目录
// 再 clone 一份，报"✅ 已 clone"，而下一次 sync 卡死在"目标目录已存在"上。
func ExistingSourceError(l config.Layout, componentID, ref string) error {
	switch Locate(l, componentID) {
	case StateActive:
		return clierr.New(clierr.CodeCloneFailed, "clone 失败：目录已存在").
			WithDetail("组件", ref).
			WithDetail("目录", DisplayDir(componentID)).
			WithDetail("原因", "该目录已存在，可能包含你正在开发的组件源码").
			WithHint(
				"如果是误操作，请先删除或重命名该目录",
				"如果已有源码，无需再次 clone",
			)
	case StateArchived:
		return clierr.New(clierr.CodeCloneFailed, "clone 失败：源码已经在了，只是被归档着").
			WithDetail("组件", ref).
			WithDetail("位置", DisplayArchivedDir(componentID)).
			WithDetail("原因",
				"brickkit sync 把这次不启动的组件源码收进了归档目录，"+
					"它没有丢，只是不在活跃目录里（004 §3.9）").
			WithHint(
				"brickkit sync —— 让它跟着启停判定回到 "+DisplayDir(componentID),
				"或直接进 "+DisplayArchivedDir(componentID)+"/ 编辑，git 命令与 IDE 都照常",
			)
	default:
		return nil
	}
}

// Clone 把开源组件的完整 Git 仓库 clone 到 components/<scope>/<name>/（004 §3.3）。
//
// 源码**两处任一处**已存在时报错阻断：活跃目录里可能是使用者正在开发的源码，
// 归档目录里那份也是他自己的（只是被 sync 收起来了）。绝不覆盖，也绝不
// 在活跃目录再造一份——那会打破"一个组件 ID 只有一个源码目录"（004 §8.1）。
func Clone(ctx context.Context, l config.Layout, componentID, ref, gitURL string) (string, error) {
	target := SourceDir(l, componentID)
	if err := ExistingSourceError(l, componentID, ref); err != nil {
		return "", err
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

// DisplayArchivedRoot 返回用于输出的归档根目录相对路径。
func DisplayArchivedRoot() string {
	return config.DirComponents + "/" + config.DirArchived
}

// DisplayArchivedDir 返回用于输出的归档相对路径。
func DisplayArchivedDir(componentID string) string {
	return config.DirComponents + "/" + config.DirArchived + "/" + componentID
}

// IsArchived 判断组件源码是否在归档目录里。
func IsArchived(l config.Layout, componentID string) bool {
	return Locate(l, componentID) == StateArchived
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
