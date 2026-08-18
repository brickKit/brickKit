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
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
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

// handleUpgrades 处理"使用者把版本号改了"这件事。
//
// 判据是**缓存里有这个组件的别的版本、却没有现在这个版本**：
//   - 首次安装 → 缓存里一个版本都没有，不是升级
//   - 缓存被清空 → 同上，不是升级（否则每次清缓存都会被当成全量升级）
//
// 检测到之后：兼容性检查（002 §7.7，阻断项在这里就报错）→ 拉产物。
// 都要在解析依赖图之前做完——不然新版本的依赖还没进图就先报"缺依赖"。
func handleUpgrades(
	ctx context.Context, opts *Options, layout config.Layout,
	cfg *config.Config, client *source.Client,
) ([]upgradeInfo, error) {
	upgrades := detectUpgrades(layout, cfg)
	if len(upgrades) == 0 {
		return nil, nil
	}

	opts.Printf("⬆️ 检测到版本变更（升级流程，004 §3.5.1）：\n")
	for _, u := range upgrades {
		opts.Printf("   %s: %s → %s\n", u.ID, u.From, u.To)
	}

	r := resolver.New(resolver.FromSource(client))
	for i, u := range upgrades {
		target := resolver.Ref{ID: u.ID, Version: u.To}

		// 002 §7.7：强依赖不可满足 / 资源未绑定 / 循环依赖 → 阻断
		report, err := r.CheckUpgrade(ctx, cfg, target)
		if err != nil {
			return nil, err
		}
		renderWarnings(opts, report.Warnings)

		node := report.Graph.Node(target)
		if node != nil && node.Manifest != nil {
			if node.Manifest.Migration != nil {
				upgrades[i].Migration = strings.Join(node.Manifest.Migration.Command, " ")
			}
			// 004 §3.5.1 的其余五项：拿缓存里的旧 Manifest 与新的比
			describeUpgradeDiff(&upgrades[i], cachedManifest(layout, u.ID, u.From), node.Manifest)
			// P10：新版本的产物要下载到新的版本化服务名目录下。
			// 旧版本的保留——调用方可能还指着旧版本（002 §7.8）
			if result, err := client.DownloadArtifacts(ctx, node.Manifest); err == nil {
				renderWarnings(opts, result.Warnings)
			} else {
				// 产物是开发时的辅助，取不到不该拦住启动（004 §10.1）
				opts.Printf("⚠️ %s 的产物下载失败：%s\n", refText(target), clierr.As(err).Message)
			}
		}
	}
	opts.Printf("\n")
	return upgrades, nil
}

// detectUpgrades 比对 brickkit.yaml 与 Manifest 缓存，找出版本变更。
func detectUpgrades(layout config.Layout, cfg *config.Config) []upgradeInfo {
	cached := cachedVersions(layout)

	var out []upgradeInfo
	for _, c := range cfg.Components {
		versions := cached[c.ID]
		if len(versions) == 0 {
			continue // 首次安装，或缓存被清空
		}
		if contains(versions, c.Version) {
			continue // 这个版本本来就装着
		}
		// 同一组件的多版本共存时，取字典序最前的那个当"从哪来"——
		// 只用于展示，不影响任何判定
		out = append(out, upgradeInfo{ID: c.ID, From: versions[0], To: c.Version})
	}
	return out
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

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// renderUpgradeSummary 输出 --dry-run 的升级变更摘要（004 §3.5.1）。
//
// 只是信息展示，不阻断任何操作。
func renderUpgradeSummary(opts *Options, plan *upPlan) {
	if len(plan.upgrades) == 0 {
		return
	}

	opts.Printf("\n📋 升级变更摘要：\n")
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
