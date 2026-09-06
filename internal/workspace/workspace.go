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
	stdpath "path"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
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

// DeletionRisk 回答一个问题：**把这份源码删掉，还找得回来吗？**
//
// 找得回来时返回空串；否则返回一句"为什么找不回来"，由调用方决定是拦下还是放行。
//
// # 为什么要问这一句
//
// `brickkit remove` 会删掉组件的源码目录。012 §2.20 论证过这是对的——"remove
// 就是彻底移除"，"未提交的修改应该先 commit + push"。**但那句话的前提是有地方
// 可 push**，也就是源码来自 `--repo` clone。
//
// 而 `init` 生成的骨架把本地安装源指向 `./components`，试用指南 17 教的正是在
// 那儿手写自己的组件。那种源码没有远端，`remove` 一删就是**永久丢失**——
// 没有确认、没有 `--yes`、没有任何一句提示。这条路是默认约定自己铺出来的。
//
// 同一个仓库在别处的立场恰好相反：`Clone` 遇到已存在的目录**坚决不覆盖**
// （"活跃目录里多半是使用者自己的源码，平台不替他决定删不删"），`move` 遇到
// 已存在的目标同样拒绝。删除比覆盖更彻底，却是唯一不问的那个。
//
// # 判据只有一条：这些字节在别的地方还有没有
//
//	不是 Git 仓库          没有副本，删了就没了
//	有未提交的改动         那些改动只在这一份工作区里
//	有提交没推到任何远端   删掉 .git 就一起没了（没有远端时，所有提交都算）
//
// 干净、且全部推上去了的 clone → 删掉不丢任何东西，照常删。那正是 012 §2.20
// 写的那种情况，行为一点没变。
func DeletionRisk(dir string) string {
	if !isDir(dir) {
		return ""
	}
	if !gitOK(dir, "rev-parse", "--is-inside-work-tree") {
		return "它不是一个 Git 仓库——这些文件没有别的副本"
	}
	// 两条查询都要**限定到这个目录**（`-- .`）。不限定的话它们报的是整个仓库的
	// 状态——组件目录常常嵌在一个更大的仓库里（试用指南的 playground 就是），
	// 那时仓库别处的任何一点改动都会让这个组件被判成"删不得"。
	if out, ok := gitOut(dir, "status", "--porcelain", "--", "."); !ok ||
		strings.TrimSpace(out) != "" {
		return "有未提交的改动（含未跟踪的文件）"
	}
	// --branches --not --remotes：本地任何分支上、而任何远端分支上都没有的提交。
	// 没有配远端时它等于"全部提交"，正好也该拦——那时 .git 就是唯一的副本。
	if out, ok := gitOut(dir, "log", "--branches", "--not", "--remotes", "--oneline", "--", "."); !ok ||
		strings.TrimSpace(out) != "" {
		return "有提交还没推到任何远端"
	}
	return ""
}

// gitOut 在目录里跑一条 git 命令，返回输出与是否成功。
func gitOut(dir string, args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// 不读使用者的全局配置：别人的 alias / hook 不该影响这个判断
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	return string(out), err == nil
}

func gitOK(dir string, args ...string) bool {
	_, ok := gitOut(dir, args...)
	return ok
}

// RemoveSource 删除组件活跃源码目录。目录不存在时返回 false（不是错误）。
//
// repo 为 nil（不在 git 仓库里、或调用方查不清楚）时完全不受影响；repo 非
// nil 时，若这份源码已经是登记过的 git submodule，直接阻断（见 removeDir）。
func RemoveSource(l config.Layout, componentID string, repo *gitrepo.Repo) (bool, error) {
	return removeDir(repo, activeLoc(l, componentID), componentID)
}

// RemoveArchived 删除组件归档源码目录。目录不存在时返回 false（不是错误）。
//
// remove 必须连归档目录一起清：sync 把源码搬进 .archived/ 之后，组件又从
// brickkit.yaml 里被移除，sync 就再也不认识它了（planSync 只看配置里声明过的
// 组件）——不在这里删掉，那份源码就是永远没人回收的孤儿。
func RemoveArchived(l config.Layout, componentID string, repo *gitrepo.Repo) (bool, error) {
	return removeDir(repo, archivedLoc(l, componentID), componentID)
}

// registeredSubmodulePath 检查 path（绝对路径）是不是 repo 里已登记的 submodule，
// 是的话返回它的仓库相对路径。
//
// repo 为 nil、或 path 不在这个仓库里、或没有 .gitmodules 登记时一律返回
// false——查不清楚时当"没登记"处理，不能让这条判断反过来在没有 git、或
// git 状态异常的项目里制造新的阻断（2026-09-06 gap report §5.1/§5.3，与
// gitrepo.Submodules 同一个"漏查代价小于堵死一次"的立场）。
func registeredSubmodulePath(repo *gitrepo.Repo, path string) (string, bool) {
	if repo == nil {
		return "", false
	}
	rel, ok := repo.Rel(path)
	if !ok {
		return "", false
	}
	_, ok = repo.Submodules()[rel]
	return rel, ok
}

// SubmoduleRemoveGuard 检查 dir 是不是 repo 里已登记的 submodule；是的话
// 返回 RemoveSource/RemoveArchived 会给的那个阻断错误，不是就返回 nil。
//
// 导出是因为**这一问必须在改 brickkit.yaml 之前先问一遍**：`brickkit remove`
// 先写配置、再删源码，等 removeDir 自己查到时配置已经存盘——拦下也留下了
// "配置说组件没了、源码却还在"的现场（2026-09-06 gap report 之后发现的
// 时序问题，与 workspace.ExistingSourceError 必须在改配置前先查一遍是
// 同一个道理）。调用方在改配置之前先调这个函数，removeDir 自己再兜一次。
func SubmoduleRemoveGuard(repo *gitrepo.Repo, dir, componentID, display string) error {
	if _, ok := registeredSubmodulePath(repo, dir); !ok {
		return nil
	}
	return submoduleRemoveBlockedError(componentID, display)
}

// removeDir 删掉一份组件源码目录，并收走随之变空的 scope 目录。
func removeDir(repo *gitrepo.Repo, loc srcLoc, componentID string) (bool, error) {
	info, err := os.Stat(loc.path)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	if err := SubmoduleRemoveGuard(repo, loc.path, componentID, loc.display); err != nil {
		return false, err
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

// submoduleRemoveBlockedError 说清楚"为什么不直接删"以及等价的手工步骤。
//
// 直接 os.RemoveAll 只删工作目录：.gitmodules 里的 stanza、superproject 索引
// 里的 gitlink 记录、.git/modules/ 下的内部仓库数据都还留着——之后 git 状态
// 会"引用一个不存在的东西"，需要人工清理（gap report §2.3）。
func submoduleRemoveBlockedError(componentID, display string) error {
	return clierr.New(clierr.CodeSubmoduleGuard, "错误：无法删除组件源码——它是一个已登记的 git submodule").
		WithDetail("组件", componentID).
		WithDetail("路径", display).
		WithDetail("原因", "直接删除工作目录不会清理 .gitmodules、superproject 索引里的 gitlink 记录、"+
			"以及 .git/modules/ 下的内部仓库数据，git 状态会从此引用一个不存在的东西").
		WithHint(
			"手工执行：git submodule deinit -f -- "+display,
			"再执行：git rm -f "+display,
			"需要彻底清理时：rm -rf .git/modules/"+display,
		)
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

// InBothPlaces 报告一个组件的源码是不是活跃目录与归档目录**都有**。
//
// # 为什么 Locate 答不出这一问
//
// Locate 回答"在哪"时活跃优先——两处都有时它答 StateActive，于是 planSync 判它
// "已经在该在的位置"、什么都不做。而这种状态是**错的**：004 §8.1 立过
// "一个组件 ID 只有一个源码目录"。
//
// 不单独回答这一问，它就会和提交前的闸门形成死循环：闸门拦下提交、
// restore 说没事可做，两边都没错，人却没有任何出路。
func InBothPlaces(l config.Layout, componentID string) bool {
	return isDir(SourceDir(l, componentID)) && isDir(ArchivedDir(l, componentID))
}

// Archive 把组件源码从 components/ 移到 components/.archived/。
//
// repo 为 nil 时完全不受影响；非 nil 时，若源码是登记过的 git submodule，
// 阻断而不搬（见 move）。
func Archive(l config.Layout, componentID string, repo *gitrepo.Repo) error {
	return move(repo, activeLoc(l, componentID), archivedLoc(l, componentID), componentID)
}

// Activate 把组件源码从归档目录移回 components/。
func Activate(l config.Layout, componentID string, repo *gitrepo.Repo) error {
	return move(repo, archivedLoc(l, componentID), activeLoc(l, componentID), componentID)
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
//
// 搬之前先问一句 from 是不是已登记的 submodule：直接 os.Rename 不会跟着改
// .gitmodules 的 path 字段，也不会更新 superproject 索引，移动之后 git 会把
// 旧路径判成删除、新路径判成未跟踪——下一次 git add -A 就会把这个组件的
// 独立版本历史拍扁成普通文件，且没有任何报错（gap report §2.2 的最小复现）。
func move(repo *gitrepo.Repo, from, to srcLoc, componentID string) error {
	if _, ok := registeredSubmodulePath(repo, from.path); ok {
		return submoduleMoveBlockedError(componentID, from.display, to.display)
	}
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

// submoduleMoveBlockedError 说清楚"为什么不直接搬"以及等价的手工步骤。
//
// git mv 对 submodule 是安全的：它会同时更新 .gitmodules 的 path 字段、
// superproject 索引里的 gitlink 记录、以及 .git/modules/ 里的内部路径——
// os.Rename 三样都不管，纯属巧合地看起来像是搬成功了。
func submoduleMoveBlockedError(componentID, from, to string) error {
	// git mv 不会自动建目标的父目录（这一点对 submodule 和普通文件一样）——
	// 第一次归档某个 scope 时目标父目录还不存在，裸给一句 git mv 会让照抄的人
	// 当场撞上 "fatal: renaming ... failed: No such file or directory"。
	toParent := stdpath.Dir(strings.TrimSuffix(to, "/"))
	return clierr.New(clierr.CodeSubmoduleGuard, "错误：无法移动组件源码——它是一个已登记的 git submodule").
		WithDetail("组件", componentID).
		WithDetail("从", from).
		WithDetail("到", to).
		WithDetail("原因", "直接移动目录不会更新 .gitmodules 的 path 字段与 superproject 索引，"+
			"下一次 git add -A 会把这个组件的独立版本历史拍扁成普通文件，且没有任何报错").
		WithHint(
			"手工执行等价操作：mkdir -p "+toParent+" && git mv "+from+" "+to,
			"确认 .gitmodules 与 git status 都正常之后，再重跑一次 brickkit sync",
		)
}
