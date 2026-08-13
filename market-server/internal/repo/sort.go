package repo

import (
	"sort"
	"strconv"
	"strings"

	"github.com/brickkit/market-server/internal/model"
)

// sortComponents 按组件 ID 升序，保证列表输出稳定可预期。
func sortComponents(items []model.Component) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].ComponentID < items[j].ComponentID
	})
}

// sortVersionsDesc 按版本号倒序（新版本在前）。
//
// 必须按数字比较：字符串序会把 1.10.0 排在 1.2.0 前面，
// 展示给使用者的"最新版本"就错了。
func sortVersionsDesc(items []model.Version) {
	sort.Slice(items, func(i, j int) bool {
		return CompareVersions(items[i].Version, items[j].Version) > 0
	})
}

// CompareVersions 比较两个 major.minor.patch 版本，返回 -1 / 0 / 1。
func CompareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		if aerr != nil || berr != nil {
			return strings.Compare(a, b)
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return len(as) - len(bs)
}
