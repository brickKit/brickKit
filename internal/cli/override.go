package cli

import (
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
	"github.com/brickkit/brickkit/internal/shell"
	"gopkg.in/yaml.v3"
)

func newOverrideCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:     "override",
		Short:   i18n.T(msgid.CliOverrideShort),
		GroupID: groupProject,
		Long:    i18n.T(msgid.CliOverrideLong),
		Example: i18n.T(msgid.CliOverrideExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverride(opts)
		},
	}
}

// runOverride 创建/刷新 override.yaml（override.yaml 设计书 §3）：首次运行时按
// brickkit.yaml 当前的组件列表生成穷举式的骨架，之后每次运行都是"刷新"——
// 已有的覆盖原样保留，新组件补一条裸 `- id:`，被删掉的组件那一行也跟着消失，
// 同时把漂移提示（Task 3 的 Drift）打印出来。这就是重置/修复操作本身，没有
// 单独的第二个命令（设计书 §3）。
func runOverride(opts *Options) error {
	if !isDefaultConfigFile(opts.ConfigPath) {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliOverrideRefusesNonDefaultConfig, opts.ConfigPath)).
			WithDetail(i18n.T(msgid.LabelPath), opts.ConfigPath)
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	existing, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil {
		return err
	}

	fresh := generateOverride(cfg, existing)

	// 刷新场景没有理由是唯一绕过 override.CheckAgainst 的地方：既有
	// override.yaml 里若带着非法升级（target: k8s 而 brickkit.yaml 自己是
	// docker/podman），generateOverride 会原样保留（它只管 Mode/LocalPort/
	// Baseline，不管 target 合不合法），写回去、报成功，直到下一次 up/sync/
	// status/down 才会被发现——那时使用者早忘了自己刚"刷新成功"过。跟
	// up/sync/status/down 走同一条校验，在这里就把它挡住。
	if err := override.CheckAgainst(cfg, fresh); err != nil {
		return err
	}

	data, err := renderOverride(fresh)
	if err != nil {
		return err
	}
	if err := os.WriteFile(layout.OverridePath(), data, 0o644); err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliOverrideWriteFailed)).
			WithDetail(i18n.T(msgid.LabelPath), layout.OverridePath()).WithCause(err)
	}

	if updated, err := config.EnsureGitignore(layout.GitignorePath()); err == nil && updated {
		opts.Printf("%s\n", i18n.T(msgid.CliOverrideGitignoreUpdated))
	}

	opts.Printf("%s\n", i18n.T(msgid.CliOverrideWritten, layout.OverridePath()))

	for _, note := range override.Drift(cfg, fresh) {
		opts.Printf("%s\n", i18n.T(msgid.CliOverrideDriftNote, note.Field, note.Message))
	}

	return nil
}

// savedOverride 是从已有 override.yaml 的一个条目里只取出、生成新文件时要保留的
// 三个字段——ComponentOverride 与 MemberOverride 是两个不同的 Go 类型
// （internal/override 的类型定义：schemagen 的反射生成器不支持自引用递归类型），
// 展平成一份索引表时只需要两者共有的这三个值。
type savedOverride struct {
	Mode      string
	LocalPort int
	Baseline  string
}

// flattenExisting 把已有 override.yaml 的顶层条目与它们嵌套的 members 展平成
// 一份按组件 ID 索引的表——generateOverride 只关心"这个 ID 之前有没有被覆盖过"，
// 不关心它当时嵌在哪个外壳下面（那层嵌套每次都重新计算，见 generateOverride 的
// 说明）。
func flattenExisting(entries []override.ComponentOverride, out map[string]savedOverride) {
	for _, c := range entries {
		out[c.ID] = savedOverride{Mode: c.Mode, LocalPort: c.LocalPort, Baseline: c.Baseline}
		for _, m := range c.Members {
			out[m.ID] = savedOverride{Mode: m.Mode, LocalPort: m.LocalPort, Baseline: m.Baseline}
		}
	}
}

// generateOverride 按 brickkit.yaml 的当前状态重建覆盖树：已有条目（按组件 ID
// 匹配）原样保留它的 Mode/LocalPort/Baseline，新组件补一条裸 id，不在
// brickkit.yaml 里的旧条目被丢弃。外壳分组每次都重新计算，而不是从旧文件
// 继承——servedBy 关系随时可能变，旧的嵌套结构不该假设还对。
//
// **一个组件 ID 只生成一条覆盖条目，与版本无关**：override.yaml 按 ID 索引，
// 没有版本号（设计书 §8"没有版本号，见 §6.1"），applyOverride 本身也是按 ID
// 匹配、对同 ID 的每个版本一视同仁。多版本共存时 cfg.Components 里同一个 ID
// 会出现好几次——原先这里照原样各生成一条，写出的文件带着重复 ID，
// 下一次任何命令读它都会被 Validate() 的"重复组件 ID"规则拒绝，等于生成
// 命令把自己刚写的文件写坏了（brickKit 反馈：多版本项目 brickkit override
// 生成的文件读不回去）。
func generateOverride(cfg *config.Config, existing *override.Override) *override.Override {
	saved := map[string]savedOverride{}
	if existing != nil {
		flattenExisting(existing.Components, saved)
	}

	ids := uniqueComponentIDs(cfg.Components)
	knownIDs := map[string]bool{}
	for _, id := range ids {
		knownIDs[id] = true
	}

	shellFor := placementShellFor(cfg.Components)

	// 外壳本身没在 brickkit.yaml 里出现（被删掉了，或者从没添加过——悬空的
	// servedBy 引用）时，嵌在它下面的成员没有地方可嵌：按 standalone 处理，
	// 而不是连同不存在的外壳一起从生成结果里消失。这与 Plan 1 的"外壳没跑，
	// 成员按普通组件独立部署"是同一个道理，只是这里连"外壳是否存在"都不成立。
	membersByShell := map[string][]string{}
	var standalone []string
	for _, id := range ids {
		if shellID, ok := shellFor[id]; ok && knownIDs[shellID] {
			membersByShell[shellID] = append(membersByShell[shellID], id)
			continue
		}
		standalone = append(standalone, id)
	}

	var out []override.ComponentOverride
	for _, id := range standalone {
		entry := componentEntryFor(id, saved)
		if group := membersByShell[id]; len(group) > 0 {
			sorted := append([]string(nil), group...)
			sort.Strings(sorted)
			for _, m := range sorted {
				entry.Members = append(entry.Members, memberEntryFor(m, saved))
			}
		}
		out = append(out, entry)
	}

	fresh := &override.Override{Components: out}
	if existing != nil {
		fresh.Target = existing.Target
		fresh.TargetBaseline = existing.TargetBaseline
	}
	return fresh
}

// uniqueComponentIDs 返回 cfg.Components 里出现过的组件 ID，按第一次出现的顺序去重。
func uniqueComponentIDs(components []config.Component) []string {
	seen := map[string]bool{}
	var ids []string
	for _, c := range components {
		if !seen[c.ID] {
			seen[c.ID] = true
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// placementShellFor 决定每个组件 ID 该嵌进哪个外壳的 members 下面（不嵌的 ID
// 不出现在返回值里，按 standalone 处理）。
//
// 只要这个 ID 有任何一个版本是 standalone（没有 servedBy，或 servedBy 写的
// 形状解析不出来），整个 ID 就按 standalone 处理，不嵌进任何外壳——嵌进去会
// 造成"这个组件只属于这个外壳"的错误印象，而它明明还有版本是独立部署的。
// 真实场景：同一个 mdm/customer，1.0.7 被 infra/shell-go-core 收编，2.0.0
// 独立部署（resolver 的版本级 servedBy 回落规则，AGENTS.md §5.1 /
// resolver_edge_test.go 的 TestServingShellIDMatchesExactVersionOnly）。
func placementShellFor(components []config.Component) map[string]string {
	standaloneIDs := map[string]bool{}
	shellFor := map[string]string{}
	for _, c := range components {
		if c.ServedBy == "" {
			standaloneIDs[c.ID] = true
			continue
		}
		ref, ok := shell.ParseRef(c.ServedBy)
		if !ok {
			standaloneIDs[c.ID] = true
			continue
		}
		if _, has := shellFor[c.ID]; !has {
			shellFor[c.ID] = ref.ID
		}
	}
	for id := range standaloneIDs {
		delete(shellFor, id)
	}
	return shellFor
}

func componentEntryFor(id string, saved map[string]savedOverride) override.ComponentOverride {
	v := saved[id]
	return override.ComponentOverride{ID: id, Mode: v.Mode, LocalPort: v.LocalPort, Baseline: v.Baseline}
}

func memberEntryFor(id string, saved map[string]savedOverride) override.MemberOverride {
	v := saved[id]
	return override.MemberOverride{ID: id, Mode: v.Mode, LocalPort: v.LocalPort, Baseline: v.Baseline}
}

// renderOverride 序列化成 override.yaml——固定的说明性头注释 + yaml.Marshal 的
// 结构体输出。不像 config.Edit 那样做节点级手术保留原有格式/注释：override.yaml
// 是 gitignore、本地生成的文件，不进 code review，每次刷新整份重写没有代价
// （跟 brickkit.yaml 的场景完全不同，那边保留格式是因为它要进 git diff）。
func renderOverride(o *override.Override) ([]byte, error) {
	const header = `# override.yaml — local deployment overrides on top of brickkit.yaml.
# Generated/refreshed by ` + "`brickkit override`" + `. Not authoritative — brickkit.yaml
# stays the source of truth for everything not listed here.

`
	body, err := yaml.Marshal(o)
	if err != nil {
		return nil, clierr.New(clierr.CodeInternal, i18n.T(msgid.CliOverrideWriteFailed)).WithCause(err)
	}
	return append([]byte(header), body...), nil
}
