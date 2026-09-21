//go:build windows

package sessionlock

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockOffsetHigh 让被锁的那一个字节落在文件内容之外很远的地方。
// Windows 的文件锁是强制的：被锁住的区间别的进程读不了，而持有者的提示信息就写在文件开头，
// 得让别的终端里的 status / down 读得到。锁一个不存放内容的字节，两件事就互不妨碍。
const lockOffsetHigh = 0x7fffffff

func lockRange(f *os.File, flags uint32) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{OffsetHigh: lockOffsetHigh})
}

// tryLock 以不阻塞的方式拿独占锁；false 表示别人持有着。
func tryLock(f *os.File) (bool, error) {
	err := lockRange(f, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err == nil {
		return true, nil
	}
	if err == windows.ERROR_LOCK_VIOLATION {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{OffsetHigh: lockOffsetHigh})
}
