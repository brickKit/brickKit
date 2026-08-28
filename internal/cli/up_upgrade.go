package cli

// 本文件是 `brickkit up` 路上的升级处理（004 §3.5.1）。
//
// 触发条件不是某个开关，而是 brickkit.yaml 里的版本号与本地缓存对不上——
// 于是拉新版本 Manifest 与产物、做 002 §7.7 的兼容性检查，
// 阻断项在真正启动之前就报错。
//
// 升级前后的差异呈现（哪些环境变量变了）在 up_upgrade_diff.go。

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// upgradeInfo 是一次版本变更。
type upgradeInfo struct {
	ID   string
	From string
	To   string
	// Migration 是新版本声明的迁移命令，空表示新版本没有迁移。
	Migration string
	// Deps / AddedConfig / RemovedConfig / Artifacts / Quota 是新旧 Manifest 的
	// 差异描述（004 §3.5.1 规定的六项里的其余五项），空字符串表示"无变化"。
	Deps          string
	AddedConfig   string
	RemovedConfig string
	Artifacts     string
	Quota         string
}

// detectUpgrades 比对 brickkit.yaml 与 Manifest 缓存，找出版本变更。
//
// # 判据：缓存里有、而配置里已经没有的版本，才是"被换掉的"
//
// 从前的判据是"缓存里有这个组件的别的版本、却没有现在这个版本"，`From` 取
// os.ReadDir 顺序的第一个——那是**文件名的字典序**，与"上一个装的是哪个"
// 毫无关系。四个场景里错了三个：
//
//	1.0.0 → 2.0.0 → 3.0.0   缓存 {1,2}、配置 {3} → 报成 1.0.0 → 3.0.0
//	                        六项摘要也拿 1.0.0 当基线，把 2.0.0 早有的配置项报成"新增"
//	加一个共存版本           缓存 {1}、配置 {1,2} → 误报成升级，而使用者要的是两个一起跑
//	回退 2.0.0 → 1.0.0      目标就在缓存里 → 完全检测不到，摘要一个字都没有
//
// 换成"被换掉的 = 缓存有 ∖ 配置有"之后四个场景全对，规则反而更短：
//
//	首次安装      缓存空 → 没有被换掉的 → 不是变更
//	加共存版本    两个版本都还在配置里 → 没有被换掉的 → 不是变更
//	升级 / 回退   旧版本从配置里消失了 → 它就是基线
//
// 基线取被换掉的那些里**版本号最高**的一个：连续升级时那正是上一个。
//
// # 缓存被清空时检测不到，这是有意的
//
// 否则每次 `rm -rf .brickkit` 都会被当成一次全量升级。代价也小——跳过的只有
// 那份信息性的摘要，检查一项都不会漏（它们本来就在常规 up 路径上）。
func detectUpgrades(layout config.Layout, cfg *config.Config) []upgradeInfo {
	cached := cachedVersions(layout)

	configured := map[string]map[string]bool{}
	for _, c := range cfg.Components {
		if configured[c.ID] == nil {
			configured[c.ID] = map[string]bool{}
		}
		configured[c.ID][c.Version] = true
	}

	var out []upgradeInfo
	for _, c := range cfg.Components {
		// 该组件曾经在这个项目里出现过、如今配置里已经没有的版本
		var replaced []string
		for _, v := range cached[c.ID] {
			if !configured[c.ID][v] {
				replaced = append(replaced, v)
			}
		}
		if len(replaced) == 0 {
			continue
		}
		sort.Slice(replaced, func(i, j int) bool {
			return manifest.CompareVersions(replaced[i], replaced[j]) < 0
		})
		out = append(out, upgradeInfo{
			ID: c.ID, From: replaced[len(replaced)-1], To: c.Version,
		})
	}
	return out
}

// describeUpgrades 补齐每条变更的差异描述，并拉新版本的产物。
//
// # 为什么不在这里做兼容性检查
//
// 这里从前还跑一遍 `resolver.CheckUpgrade`（002 §7.7 的五项）。**那五项常规
// `up` 路径一项不落地全做了**——解析拿不到 Manifest 就报错、强依赖缺失报错、
// 弱依赖缺失警告、循环依赖报错、资源未绑定报错。它是同一套判断的第二份拷贝，
// 而且复制得不完整，于是升级路径上多出两个只有升级才会撞的 bug：
//
//	--dry-run 被阻断      常规路径把资源检查降级成警告（004 §4.4），这份拷贝没有
//	enabled: false 被阻断  常规路径只查会启动的组件（006 §4.4），这份拷贝无条件查
//
// 删掉之后两个 bug 一起消失，002 §7.7 那五项一项没少——只是由常规路径统一执行。
//
// 放在依赖图解析**之后**：新版本的 Manifest 已经在图里，不必再取一次。
func describeUpgrades(
	ctx context.Context, opts *Options, layout config.Layout,
	client *source.Client, graph *resolver.Graph, upgrades []upgradeInfo,
) {
	for i, u := range upgrades {
		target := resolver.Ref{ID: u.ID, Version: u.To}
		node := graph.Node(target)
		if node == nil || node.Manifest == nil {
			continue
		}

		if node.Manifest.Migration != nil {
			upgrades[i].Migration = strings.Join(node.Manifest.Migration.Command, " ")
		}
		// 004 §3.5.1 的其余五项：拿缓存里的旧 Manifest 与新的比
		describeUpgradeDiff(&upgrades[i], cachedManifest(layout, u.ID, u.From), node.Manifest)

		// P10：新版本的产物要下载到新的版本化服务名目录下。手改版本号时没跑过
		// `add`，这是唯一会拉它们的地方。旧版本的保留——调用方可能还指着
		// 旧版本（002 §7.8）
		if result, err := client.DownloadArtifacts(ctx, node.Manifest); err == nil {
			renderWarnings(opts, result.Warnings)
		} else {
			// 产物是开发时的辅助，取不到不该拦住启动（004 §10.1）
			opts.Printf("⚠️ %s 的产物下载失败：%s\n", refText(target), clierr.As(err).Message)
		}
	}
}

// renderUpgradeBanner 说明这次检测到了哪些版本变更。
func renderUpgradeBanner(opts *Options, upgrades []upgradeInfo) {
	if len(upgrades) == 0 {
		return
	}
	opts.Printf("⬆️ 检测到版本变更（004 §3.5.1）：\n")
	for _, u := range upgrades {
		opts.Printf("   %s: %s → %s\n", u.ID, u.From, u.To)
	}
	opts.Printf("\n")
}

// cachedVersions 读出 .brickkit/manifests/ 里每个组件已缓存的版本。
//
// 文件名形如 people-basic-1.0.0.yaml：组件 ID 里的 `/` 在文件名里是 `-`，
// 因此不能直接按 `-` 切——用配置里的组件 ID 反过来匹配前缀才可靠。
func cachedVersions(layout config.Layout) map[string][]string {
	entries, err := os.ReadDir(layout.ManifestsDir())
	if err != nil {
		return nil
	}

	out := map[string][]string{}
	for _, entry := range entries {
		// 只认 Manifest 本身。这个目录里还躺着签名缓存
		// （people-basic-1.0.0.sig.json）——不筛掉的话，去掉一层扩展名会得到
		// people-basic-1.0.0.sig，"版本号"就成了 1.0.0.sig，一路显示到升级摘要里。
		if filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		id, version, ok := splitCachedName(name)
		if !ok {
			continue
		}
		out[id] = append(out[id], version)
	}
	return out
}

// splitCachedName 把 `people-basic-1.0.0` 拆成 people/basic + 1.0.0。
//
// 版本号一定在最后一个 `-` 之后，且带点；前面的 `-` 分隔的是 scope 与 name。
func splitCachedName(name string) (id, version string, ok bool) {
	idx := strings.LastIndex(name, "-")
	if idx <= 0 {
		return "", "", false
	}
	version = name[idx+1:]
	if !strings.Contains(version, ".") {
		return "", "", false
	}

	prefix := name[:idx]
	slash := strings.Index(prefix, "-")
	if slash <= 0 {
		return "", "", false
	}
	return prefix[:slash] + "/" + prefix[slash+1:], version, true
}

// renderUpgradeSummary 输出 --dry-run 的版本变更摘要（004 §3.5.1）。
//
// 只是信息展示，不阻断任何操作。
//
// 叫"版本变更"而不是"升级"：判据换成"配置里没有了的那个版本"之后，
// 回退（2.0.0 → 1.0.0）同样会走到这里，而那不是升级。
func renderUpgradeSummary(opts *Options, plan *upPlan) {
	if len(plan.upgrades) == 0 {
		return
	}

	opts.Printf("\n📋 版本变更摘要：\n")
	for _, u := range plan.upgrades {
		opts.Printf("   %s: %s → %s\n", u.ID, u.From, u.To)

		// 六项固定都出（004 §3.5.1）。没变化的写"无"而不是隐藏——
		// 藏起来会让人分不清"没有变化"和"平台没检查这一方面"。
		for _, row := range []struct{ label, value string }{
			{"依赖变更", u.Deps},
			{"新增配置项", u.AddedConfig},
			{"删除配置项", u.RemovedConfig},
			{"数据库迁移", u.Migration},
			{"artifacts 变更", u.Artifacts},
			{"资源配额变更", u.Quota},
		} {
			opts.Printf("   ├── %s：%s\n", row.label, valueOrNone(row.value))
		}
		opts.Printf("   └── 旧版本产物：保留（调用方可能仍指向旧版本）\n")
	}
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "无"
	}
	return value
}
