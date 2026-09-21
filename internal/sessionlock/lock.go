// Package sessionlock 是项目目录级的会话锁：同一时刻只允许一个前台会话监管本地进程，
// 并且让别的终端里的 status / down 能看见"这个项目现在有个会话在跑"。
//
// 锁靠操作系统的文件锁（Unix 的 flock、Windows 的 LockFileEx）实现，持有者进程一死，
// 内核就自动释放——所以判断"有没有会话"看的是锁本身，不是文件里写的 PID。
// PID 文件那套的毛病正是会陈旧：进程被 kill -9 之后文件还在，还得再去猜那个 PID 是不是被别的进程复用了。
// 文件里的内容（持有者的 PID 与启动时间）只是拿来给人看的提示。
package sessionlock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Info 是持有者写在锁文件里的提示信息。
type Info struct {
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
}

// HeldError 表示锁已经被另一个活着的会话持有。Info 是那个会话留下的提示，
// 读不出来时（刚好赶上它在写）是零值。
type HeldError struct{ Info Info }

func (e *HeldError) Error() string {
	return fmt.Sprintf("session lock is held by pid %d", e.Info.PID)
}

// Lock 是一把已经拿到手的锁。
type Lock struct {
	f *os.File
}

// Acquire 在 path 上拿锁；path 所在目录不存在会被创建。
// 已经被别的会话持有时返回 *HeldError。
//
// 锁文件永远不会被删除：删除会让"正在等着 open 同一个路径的另一个进程"拿到一个已经脱离目录的
// inode，两边各自以为自己持有锁。空文件留在原地没有任何代价。
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	locked, err := tryLock(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !locked {
		_ = f.Close()
		info, _ := readInfo(path)
		return nil, &HeldError{Info: info}
	}

	l := &Lock{f: f}
	if err := l.writeInfo(); err != nil {
		_ = l.Release()
		return nil, err
	}
	return l, nil
}

func (l *Lock) writeInfo() error {
	b, err := json.Marshal(Info{PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return err
	}
	if err := l.f.Truncate(0); err != nil {
		return err
	}
	_, err = l.f.WriteAt(b, 0)
	return err
}

// Release 释放锁。进程退出时不调用也行——内核会替你释放——但正常收尾时应该调用，
// 它会顺手清掉文件里的提示信息。
func (l *Lock) Release() error {
	_ = l.f.Truncate(0)
	return errors.Join(unlock(l.f), l.f.Close())
}

// Inspect 看 path 上现在有没有活着的持有者：held 为 true 时 info 是它留下的提示。
// 文件不存在、或者没人持有，都返回 held == false。
//
// 探测的办法是试着拿一下锁再立刻放掉，所以它和 Acquire 之间有一个微秒级的窗口：
// 恰好在这一瞬间调用 Acquire 的会话会被误判成"已被持有"。代价是重试一次，不值得为它引入别的机制。
func Inspect(path string) (info Info, held bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, false, nil
	}
	if err != nil {
		return Info{}, false, err
	}
	defer func() { _ = f.Close() }()

	locked, err := tryLock(f)
	if err != nil {
		return Info{}, false, err
	}
	if locked {
		return Info{}, false, unlock(f)
	}
	info, _ = readInfo(path)
	return info, true, nil
}

func readInfo(path string) (Info, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	err = json.Unmarshal(b, &info)
	return info, err
}
