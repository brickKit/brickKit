package cli

// 本文件算"这次升级到底会改变什么"（004 §3.5.1 的升级变更摘要，开发计划 38.18–38.22）。
//
// # 为什么值得算
//
// 使用者按下 `--dry-run` 就是想在真动手之前知道这个。原来的摘要只说了
// 版本号和有没有迁移——其余四项（依赖、配置项、产物、资源配额）一律没有，
// 等于让他自己去 diff 两份 Manifest，而他多半不知道旧的那份就在
// `.brickkit/manifests/` 里。
//
// # 取不到旧 Manifest 时说"未知"，不说"无"
//
// 旧 Manifest 是从缓存读的，缓存可能被清过、文件可能坏了。
// 这时候唯一不能做的事是输出"无变化"——那是一句**看起来正常的假话**，
// 使用者会据此认为可以放心升级。说"未知（旧版本 Manifest 读不到）"
// 至少让他知道这一项没算出来。

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// unknownDiff 是算不出差异时的说法。
const unknownDiff = "未知（旧版本 Manifest 读不到）"

// cachedManifest 读缓存里某个版本的 Manifest；读不到返回 nil。
func cachedManifest(layout config.Layout, id, version string) *manifest.Manifest {
	name := strings.ReplaceAll(id, "/", "-") + "-" + version + ".yaml"
	m, err := manifest.ParseFile(filepath.Join(layout.ManifestsDir(), name))
	if err != nil {
		return nil // 缓存被清过或文件坏了；调用方会把这一项报成"未知"
	}
	return m
}

// describeUpgradeDiff 把新旧 Manifest 的差异填进 upgradeInfo。
func describeUpgradeDiff(u *upgradeInfo, oldM, newM *manifest.Manifest) {
	if oldM == nil || newM == nil {
		u.Deps, u.AddedConfig, u.RemovedConfig = unknownDiff, unknownDiff, unknownDiff
		u.Artifacts, u.Quota = unknownDiff, unknownDiff
		return
	}

	u.Deps = diffList(dependencyNames(oldM), dependencyNames(newM))
	u.AddedConfig = addedConfigText(oldM, newM)
	u.RemovedConfig = joinOrEmpty(removed(configKeys(oldM), configKeys(newM)))
	u.Artifacts = diffList(artifactFiles(oldM), artifactFiles(newM))
	u.Quota = quotaText(oldM, newM)
}

// ============================================================
// 依赖（38.18）
// ============================================================

// dependencyNames 收集组件依赖与资源依赖的名字。
//
// 资源依赖也算进来：新版本开始要一个数据库，对使用者的影响
// 不比多依赖一个组件小——他得去 brickkit.yaml 里绑定它。
func dependencyNames(m *manifest.Manifest) []string {
	if m.Dependencies == nil {
		return nil
	}
	var out []string
	for _, c := range m.Dependencies.Components {
		out = append(out, c.ID+"@"+c.Version)
	}
	for _, r := range m.Dependencies.Resources {
		out = append(out, r.Kind+"（"+r.Engine+"）")
	}
	return out
}

// ============================================================
// 配置项（38.19）
// ============================================================

func configKeys(m *manifest.Manifest) []string {
	if m.ConfigSchema == nil {
		return nil
	}
	out := make([]string, 0, len(m.ConfigSchema.Properties))
	for name := range m.ConfigSchema.Properties {
		out = append(out, name)
	}
	sort.Strings(out) // map 遍历顺序随机，不排序的话同一次升级两次输出会不一样
	return out
}

// addedConfigText 列出新增的配置项，并带上各自的默认值。
//
// 带默认值是因为新增项走的就是它（38.13）——使用者要判断这个值对不对，
// 而不只是知道"多了个配置项"。
func addedConfigText(oldM, newM *manifest.Manifest) string {
	added := removed(configKeys(newM), configKeys(oldM)) // 新有旧无
	if len(added) == 0 {
		return ""
	}

	items := make([]string, 0, len(added))
	for _, name := range added {
		prop := newM.ConfigSchema.Properties[name]
		if prop.Default == nil {
			items = append(items, name+"（无默认值）")
			continue
		}
		items = append(items, fmt.Sprintf("%s（默认 %v）", name, prop.Default))
	}
	return strings.Join(items, "、")
}

// ============================================================
// 产物（38.21）
// ============================================================

func artifactFiles(m *manifest.Manifest) []string {
	var out []string
	for _, a := range m.Artifacts {
		out = append(out, a.Files...)
	}
	sort.Strings(out)
	return out
}

// ============================================================
// 资源配额（38.22）
// ============================================================

// quotaText 描述 deployment.resources 的变化。
//
// 配额变了意味着这次升级可能因为节点资源不够而起不来，
// 是少数几个"升级失败但与组件代码无关"的原因之一，所以要给出**新值**——
// 只说"变了"的话，使用者还得自己去翻 Manifest。
func quotaText(oldM, newM *manifest.Manifest) string {
	before, after := quotaSpec(oldM), quotaSpec(newM)
	if before == after {
		return ""
	}
	return valueOrNone(before) + " → " + valueOrNone(after)
}

// quotaSpec 把配额压成一行可比较的文本。
func quotaSpec(m *manifest.Manifest) string {
	if m.Deployment.Resources == nil {
		return ""
	}
	var parts []string
	for _, s := range []struct {
		label string
		spec  *manifest.ResourceSpec
	}{
		{"requests", m.Deployment.Resources.Requests},
		{"limits", m.Deployment.Resources.Limits},
	} {
		if s.spec == nil {
			continue
		}
		var fields []string
		if s.spec.CPU != "" {
			fields = append(fields, "cpu "+s.spec.CPU)
		}
		if s.spec.Memory != "" {
			fields = append(fields, "内存 "+s.spec.Memory)
		}
		if len(fields) > 0 {
			parts = append(parts, s.label+" "+strings.Join(fields, "/"))
		}
	}
	return strings.Join(parts, "，")
}

// ============================================================
// 通用
// ============================================================

// diffList 把两份名单的增删描述成一句话。
func diffList(before, after []string) string {
	added, gone := removed(after, before), removed(before, after)

	var parts []string
	if len(added) > 0 {
		parts = append(parts, "新增 "+strings.Join(added, "、"))
	}
	if len(gone) > 0 {
		parts = append(parts, "移除 "+strings.Join(gone, "、"))
	}
	return strings.Join(parts, "；")
}

// removed 返回在 a 里、不在 b 里的元素（顺序保持 a 的顺序）。
func removed(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, item := range b {
		inB[item] = true
	}
	var out []string
	for _, item := range a {
		if !inB[item] {
			out = append(out, item)
		}
	}
	return out
}

func joinOrEmpty(items []string) string { return strings.Join(items, "、") }
