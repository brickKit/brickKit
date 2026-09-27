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
	"slices"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
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

// detectUpgrades 比对 brickkit.yaml 与这个项目上一次运行的版本，找出版本变更。
//
// # 判据：上次跑的版本从配置里消失了、换成了一个上次没跑的版本
//
//	首次运行      没有上次 → 不是变更
//	加共存版本    上次的版本都还在 → 不是变更
//	升级 / 回退   旧版本不在了、新版本上次没跑 → 旧版本就是基线
//	连带移除      兼容版本不在了，但留下的版本上次就在跑 → 不是变更
//
// 基线取被换掉的那些里版本号最高的一个：连续升级时那正是上一个。基线来自上次运行记录
// （project.ReadLastRun），不来自 Manifest 缓存——缓存是永久的，fetch 过、被拒的 add、
// 连带移除的版本都在里面，只有差异描述要读缓存里旧版本的 Manifest。
//
// # 没有上次运行记录时检测不到，这是有意的
//
// 否则每次 `rm -rf .brickkit` 都会被当成一次全量升级。代价也小——跳过的只有
// 那份信息性的摘要，检查一项都不会漏（它们本来就在常规 up 路径上）。
func detectUpgrades(proj *project.Project) []upgradeInfo {
	previous, ok := project.ReadLastRun(proj.Layout)
	if !ok {
		return nil
	}
	configured := map[string]map[string]bool{}
	for _, c := range proj.Decl.Components {
		if configured[c.ID] == nil {
			configured[c.ID] = map[string]bool{}
		}
		configured[c.ID][c.Version] = true
	}

	var out []upgradeInfo
	for _, c := range proj.Decl.Components {
		if slices.Contains(previous[c.ID], c.Version) {
			continue // 上次就在跑
		}
		var replaced []string
		for _, v := range previous[c.ID] {
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
//	mode: disable 被阻断   常规路径只查会启动的组件（006 §4.4），这份拷贝无条件查
//
// 删掉之后两个 bug 一起消失，002 §7.7 那五项一项没少——只是由常规路径统一执行。
//
// 放在依赖图解析**之后**：新版本的 Manifest 已经在图里，不必再取一次。
func describeUpgrades(
	ctx context.Context, opts *Options, layout project.Layout,
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
			opts.Printf("%s\n", i18n.T(msgid.CliUpUpgradeArtifactDownloadForFailed, refText(target), clierr.As(err).Message))
		}
	}
}

// renderUpgradeBanner 说明这次检测到了哪些版本变更。
func renderUpgradeBanner(opts *Options, upgrades []upgradeInfo) {
	if len(upgrades) == 0 {
		return
	}
	opts.Printf("%s\n", i18n.T(msgid.CliUpUpgradeVersionChangeDetected))
	for _, u := range upgrades {
		opts.Printf("   %s: %s → %s\n", u.ID, u.From, u.To)
	}
	opts.Printf("\n")
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

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpUpgradeVersionChangeSummary))
	for _, u := range plan.upgrades {
		opts.Printf("   %s: %s → %s\n", u.ID, u.From, u.To)

		// 六项固定都出（004 §3.5.1）。没变化的写"无"而不是隐藏——
		// 藏起来会让人分不清"没有变化"和"平台没检查这一方面"。
		for _, row := range []struct{ label, value string }{
			{i18n.T(msgid.CliUpUpgradeDependencyChanges), u.Deps},
			{i18n.T(msgid.CliUpUpgradeAddedConfigItems), u.AddedConfig},
			{i18n.T(msgid.CliUpUpgradeRemovedConfigItems), u.RemovedConfig},
			{i18n.T(msgid.CliUpUpgradeDatabaseMigration), u.Migration},
			{i18n.T(msgid.CliUpUpgradeArtifactsChanges), u.Artifacts},
			{i18n.T(msgid.CliUpUpgradeResourceQuotaChanges), u.Quota},
		} {
			opts.Printf("%s\n", i18n.T(msgid.CliUpUpgradeMsg, row.label, valueOrNone(row.value)))
		}
		opts.Printf("%s\n", i18n.T(msgid.CliUpUpgradeOldVersionArtifactsKeptCallers))
	}
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return i18n.T(msgid.CliUpUpgradeNone)
	}
	return value
}
