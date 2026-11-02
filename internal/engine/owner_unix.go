//go:build !windows

package engine

import (
	"os"
	"strconv"
	"syscall"
)

// fileOwner 是一个文件属主的 uid。
func fileOwner(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return strconv.FormatUint(uint64(st.Uid), 10), true
}
