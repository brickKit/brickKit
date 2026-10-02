package install

import (
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// EntryBefore 是 brickkit.yaml 与部署文件里组件条目的顺序：add / remove / upgrade 每次写这两种文件，
// 都把 components（和外壳下面的 members）按它排好——组件一多，按名字找得到比按加入的先后找得到有用。
//
//   - 先按组件 ID（<scope>/<name>）的字典序，同一个 scope 的自然挨在一起
//   - 同一个 ID：默认版本在前——brickkit.yaml 里是不带 requiredBy 的那一行，部署文件里是裸 ID 的条目
//   - 其余的兼容版本按版本号从低到高
//
// 不能直接比 id 字段的原文：部署文件的 id@version 里 "@" 排在 "-" 之后，erp/api@1.0.0 会被
// erp/api-gateway 隔开。
func EntryBefore(a, b yamlfile.EntryKey) bool {
	aID, aVersion, aCompat := orderKey(a)
	bID, bVersion, bCompat := orderKey(b)
	if aID != bID {
		return aID < bID
	}
	if aCompat != bCompat {
		return !aCompat
	}
	return manifest.CompareVersions(aVersion, bVersion) < 0
}

// orderKey 把一个条目拆成组件 ID、版本、是不是兼容版本。
func orderKey(k yamlfile.EntryKey) (id, version string, compat bool) {
	id, version, _ = manifest.SplitRef(k.ID)
	if k.Version != "" {
		return id, k.Version, k.RequiredBy
	}
	return id, version, version != ""
}
