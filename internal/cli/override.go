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
	if opts.ConfigPath != "" && opts.ConfigPath != DefaultConfigFile {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliOverrideRefusesNonDefaultConfig)).
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
func generateOverride(cfg *config.Config, existing *override.Override) *override.Override {
	saved := map[string]savedOverride{}
	if existing != nil {
		flattenExisting(existing.Components, saved)
	}

	members := map[string][]config.Component{}
	var standalone []config.Component
	for _, c := range cfg.Components {
		if c.ServedBy != "" {
			if ref, ok := shell.ParseRef(c.ServedBy); ok {
				members[ref.ID] = append(members[ref.ID], c)
				continue
			}
		}
		standalone = append(standalone, c)
	}

	var out []override.ComponentOverride
	for _, c := range standalone {
		entry := componentEntryFor(c.ID, saved)
		if group, ok := members[c.ID]; ok {
			sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })
			for _, m := range group {
				entry.Members = append(entry.Members, memberEntryFor(m.ID, saved))
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
