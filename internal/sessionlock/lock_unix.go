//go:build unix

package sessionlock

import (
	"os"
	"syscall"
)

// tryLock 以不阻塞的方式拿独占锁；false 表示别人持有着。
// flock 锁的是"打开的文件描述"，所以同一个进程里再 open 一次、再 flock，同样会被拒绝。
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if err == syscall.EWOULDBLOCK {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
