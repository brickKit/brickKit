package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// State 是一份托管文件的当前状态。
type State string

const (
	// StateMissing 文件不存在。
	StateMissing State = "缺失"
	// StateCurrent 内容已与当前版本的资产一致。
	StateCurrent State = "最新"
	// StateOutdated 内容是我们上次写的，但资产已经变了。
	StateOutdated State = "待更新"
	// StateModified 内容与我们上次写的不一致——用户改过。
	StateModified State = "已手改"
	// StateUntracked 文件存在但 lock 里没有记录。
	StateUntracked State = "未托管"
)

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
}

// Status 计算全部资产的当前状态。只读，不写任何文件。
func (in Installer) Status() ([]FileStatus, error) {
	lock, err := LoadLock(in.LockPath)
	if err != nil {
		return nil, err
	}
	var out []FileStatus
	for _, a := range Assets() {
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
		return st, fmt.Errorf("读取内嵌资产 %s 失败：%w", a.Source, err)
	}
	disk, err := os.ReadFile(filepath.Join(in.Root, a.Target))
	if os.IsNotExist(err) {
		st.State = StateMissing
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("读取 %s 失败：%w", a.Target, err)
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
func (in Installer) Apply() (*ApplyResult, error) {
	lock, err := LoadLock(in.LockPath)
	if err != nil {
		return nil, err
	}
	res := &ApplyResult{}
	for _, a := range Assets() {
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
			return nil, fmt.Errorf("读取内嵌资产 %s 失败：%w", a.Source, err)
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
	if err := lock.Save(in.LockPath); err != nil {
		return nil, err
	}
	return res, nil
}

func (in Installer) write(target string, content []byte) error {
	path := filepath.Join(in.Root, target)
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("创建目录 %s 失败：%w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(path, content, filePerm); err != nil {
		return fmt.Errorf("写入 %s 失败：%w", target, err)
	}
	return nil
}
