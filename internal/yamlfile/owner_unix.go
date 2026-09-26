//go:build !windows

package yamlfile

import (
	"os"
	"syscall"
)

// keepOwner 让改名后的新文件沿用原文件的属主（sudo 编辑别人的文件时不至于把它变成 root 的）。
// 改不了（不是 root 又不是同一个人）就算了：权限已经照原样设好。
func keepOwner(name string, original os.FileInfo) {
	if st, ok := original.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(name, int(st.Uid), int(st.Gid))
	}
}
