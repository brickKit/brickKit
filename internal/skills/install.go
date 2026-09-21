package skills

import (
	"fmt"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"os"
	"path/filepath"
)

// State 是一份托管文件的当前状态。
type State string

// 取值是语言中立的标识（比较、分流用），从不直接显示；要给人看走 Label()。
const (
	// StateMissing 文件不存在。
	StateMissing State = "missing"
	// StateCurrent 内容已与当前版本的资产一致。
	StateCurrent State = "current"
	// StateOutdated 内容是我们上次写的，但资产已经变了。
	StateOutdated State = "outdated"
	// StateModified 内容与我们上次写的不一致——用户改过。
	StateModified State = "modified"
	// StateUntracked 文件存在但 lock 里没有记录。
	StateUntracked State = "untracked"
)

// Label 是给人看的状态名，跟着当前语言走。
func (s State) Label() string {
	switch s {
	case StateMissing:
		return i18n.T(msgid.SkillsStateMissing)
	case StateCurrent:
		return i18n.T(msgid.SkillsStateCurrent)
	case StateOutdated:
		return i18n.T(msgid.SkillsStateOutdated)
	case StateModified:
		return i18n.T(msgid.SkillsStateModified)
	case StateUntracked:
		return i18n.T(msgid.SkillsStateUntracked)
	}
	return string(s)
}

// writable 判断这个状态下是否允许写入。
// 只有这两种状态可写，其余一律不动——尤其是「已手改」与「未托管」。
func (s State) writable() bool {
	return s == StateMissing || s == StateOutdated
}

// FileStatus 是一份托管文件的状态。
type FileStatus struct {
	// Target 是项目内相对路径。
	Target string
	// State 是当前状态。
	State State
	// FromVersion 仅在 StateOutdated 时有值：lock 里记的旧版本。
	FromVersion string
}

// Installer 把内嵌资产装进一个项目。
type Installer struct {
	// Root 是项目根目录。
	Root string
	// LockPath 是 skills.lock 的路径。
	LockPath string
	// Version 是当前 CLI 版本，写进 lock。
	Version string
	// Scope 是管理哪一部分资产，零值是完整的项目那一套。
	Scope Scope
	// Lang 显式指定这次要用哪种语言的资产（"" 表示不指定，走 resolveLang 的
	// 优先级）。brickkit init 与 brickkit skills update --lang 会显式传它；
	// 裸的 status / update 留空，沿用项目已经在用的语言。
	Lang i18n.Lang
}

// resolveLang 决定这次实际使用的语言，按优先级：
//
//  1. Installer.Lang——调用方显式指定（init 传当前 CLI 语言；update --lang 传参数值）
//  2. lock.Lang——项目已经记录过的语言，稳定，不随运行 CLI 的语言变
//  3. lock 里有旧条目、但没有 Lang 字段——早于这个字段存在的项目，
//     那批资产历史上只有中文，回退到 zh 而不是当场改语言
//  4. 全新项目（既没指定、lock 也是空的）——用当前 CLI 语言，等价于"就当
//     现在装一份"
func (in Installer) resolveLang(lock *Lock) i18n.Lang {
	if in.Lang != "" {
		return in.Lang
	}
	if l, ok := i18n.ParseLang(lock.Lang); ok {
		return l
	}
	if len(lock.Entries) > 0 {
		return i18n.ZH
	}
	return i18n.Current()
}

// ResolvedLang 返回这次实际会用的语言（不落盘，只读），供 CLI 在 `skills status`
// 里告诉用户"这个项目现在用的是哪种语言"——语言本身跟着 resolveLang 的优先级走，
// 与 Status()/Apply() 用的是同一套判断。
func (in Installer) ResolvedLang() (i18n.Lang, error) {
	lock, err := LoadLock(in.LockPath)
	if err != nil {
		return "", err
	}
	return in.resolveLang(lock), nil
}

// Status 计算全部资产的当前状态。只读，不写任何文件——语言解析的结果不落盘，
// 用什么语言只有 Apply 才会记进 lock。
func (in Installer) Status() ([]FileStatus, error) {
	lock, err := LoadLock(in.LockPath)
	if err != nil {
		return nil, err
	}
	lang := in.resolveLang(lock)
	var out []FileStatus
	for _, a := range AssetsFor(in.Scope, lang) {
		st, err := in.stateOf(a, lock)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// stateOf 判定单个资产的状态。
//
// 判定顺序本身是规格：
//
//	文件不存在                → 缺失
//	磁盘指纹 == 资产指纹       → 最新
//	lock 里没有这条            → 未托管
//	磁盘指纹 != lock 记录      → 已手改
//	其余                      → 待更新
//
// 「最新」刻意排在 lock 查询之前：一个内容恰好与资产一致的文件，判成「最新」
// 并补登记，比判成「未托管」永远跳过要好——否则它下个版本还是「未托管」。
func (in Installer) stateOf(a Asset, lock *Lock) (FileStatus, error) {
	st := FileStatus{Target: a.Target}

	want, err := a.Content()
	if err != nil {
		return st, fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToReadTheEmbedded, a.Source), err)
	}
	disk, err := os.ReadFile(filepath.Join(in.Root, a.Target))
	if os.IsNotExist(err) {
		st.State = StateMissing
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToRead, a.Target), err)
	}

	if Sum(disk) == Sum(want) {
		st.State = StateCurrent
		return st, nil
	}
	entry, ok := lock.Get(a.Target)
	if !ok {
		st.State = StateUntracked
		return st, nil
	}
	if entry.Sum != Sum(disk) {
		st.State = StateModified
		return st, nil
	}
	st.State = StateOutdated
	st.FromVersion = entry.Version
	return st, nil
}

// ApplyResult 是一次 Apply 的结果。
type ApplyResult struct {
	// Written 是本次写入的项目内相对路径。
	Written []string
	// Skipped 是刻意没碰的文件（已手改 / 未托管）。
	Skipped []FileStatus
}

// Apply 按状态写入资产：缺失与待更新写入，已手改与未托管跳过，最新只补登记 lock。
//
// 全程不删任何文件，也不动跳过的那些在 lock 里的记录——抹掉的话，
// 「已手改」下次就会退化成「未托管」，而两者给用户的提示是不一样的。
//
// # 语言切换是怎么"免费"生效的
//
// resolveLang 解出的语言一定会写回 lock.Lang，即便这次没有任何文件被判定为
// 需要改动——项目从此记住了这个选择。切换语言（比如 skills update --lang zh）
// 不需要任何特殊逻辑：lock.Lang 一变，AssetsFor 取的就是另一种语言的内容，
// 而磁盘上的文件还是旧语言、指纹与旧 lock 记录一致——stateOf 天然把它判成
// 「待更新」（内容变了、没被手改），可写；真被用户手改过的文件则天然判成
// 「已手改」，照样跳过。状态机本身完全不用知道"语言"这回事。
func (in Installer) Apply() (*ApplyResult, error) {
	lock, err := LoadLock(in.LockPath)
	if err != nil {
		return nil, err
	}
	lang := in.resolveLang(lock)
	res := &ApplyResult{}
	for _, a := range AssetsFor(in.Scope, lang) {
		st, err := in.stateOf(a, lock)
		if err != nil {
			return nil, err
		}
		if st.State != StateCurrent && !st.State.writable() {
			res.Skipped = append(res.Skipped, st)
			continue
		}
		content, err := a.Content()
		if err != nil {
			return nil, fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToReadTheEmbedded, a.Source), err)
		}
		if st.State.writable() {
			if err := in.write(a.Target, content); err != nil {
				return nil, err
			}
			res.Written = append(res.Written, a.Target)
		}
		// 「最新」也要登记：内容一致但 lock 里没有记录时补上，
		// 否则它下个版本会被判成「未托管」而永远升不上去。
		lock.Set(LockEntry{Path: a.Target, Version: in.Version, Sum: Sum(content)})
	}
	lock.Lang = string(lang)
	if err := lock.Save(in.LockPath); err != nil {
		return nil, err
	}
	return res, nil
}

func (in Installer) write(target string, content []byte) error {
	path := filepath.Join(in.Root, target)
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToCreateTheDirectory, filepath.Dir(target)), err)
	}
	if err := os.WriteFile(path, content, filePerm); err != nil {
		return fmt.Errorf("%s%w", i18n.T(msgid.SkillsInstallFailedToWrite, target), err)
	}
	return nil
}
