//go:build windows

package yamlfile

import "os"

// keepOwner 在 Windows 上什么也不做：文件的归属由 ACL 表达，改名覆盖时随目录继承。
func keepOwner(string, os.FileInfo) {}
