// Package configdir 负责 config/ 目录——三层文件的"业务配置"层：每个组件一份环境变量文件、
// 一份项目级公共变量 vars.yaml，以及两者之间显式的 $var: 引用（提案 §7）。
package configdir

import (
	"strings"

	"github.com/brickkit/brickkit/internal/manifest"
)

const (
	// VarsFile 是公共变量文件名。
	VarsFile = "vars.yaml"
	// ArchiveDir 是归档目录名（config/.archive/，不进 Git）。
	ArchiveDir = ".archive"
	ext        = ".yaml"
)

// FileBase 把组件 ID 变成文件名主干：erp/backend → erp-backend。
func FileBase(id string) string { return strings.ReplaceAll(id, "/", "-") }

// FileName 返回组件的配置文件名；version 为空时是跟随当前版本的无版本文件。
func FileName(id, version string) string {
	if version == "" {
		return FileBase(id) + ext
	}
	return FileBase(id) + "@" + version + ext
}

// ParseFileName 反解 config/ 下的文件名。vars.yaml、非 .yaml 文件、版本不是精确版本的都不算。
func ParseFileName(name string) (base, version string, ok bool) {
	if name == VarsFile || !strings.HasSuffix(name, ext) {
		return "", "", false
	}
	stem := strings.TrimSuffix(name, ext)
	if i := strings.LastIndex(stem, "@"); i >= 0 {
		base, version = stem[:i], stem[i+1:]
		if base == "" || !manifest.IsExactVersion(version) {
			return "", "", false
		}
		return base, version, true
	}
	if stem == "" {
		return "", "", false
	}
	return stem, "", true
}
