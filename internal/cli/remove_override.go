package cli

// 本文件让 brickkit remove 在真的把一个组件 ID 的最后一个版本从 brickkit.yaml 里删掉
// 之后，把 override.yaml 也带上（override.yaml 设计书 §9）。留着一条指向不存在组件的
// 条目，下一次任何命令读 override.yaml 都会被 CheckAgainst 的悬空引用检查拦下
// （AGENTS.md §7.1）——remove 自己造成的问题不该留给下一条命令去发现。
//
// 这里做的是最小的、定向的编辑，不像 add 那样整个重新生成：override.yaml 里可能还有
// 别的组件带着真实自定义值，一次 remove 不该有牵连它们的风险（设计书 §9 把这条跟 add
// 的整体重生成分开论证过）。

import (
	"os"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
)

// removeFromOverride 在 id 的最后一个版本被移出 brickkit.yaml 之后调用。返回 changed
// 表示文件是否被真的改写过（remove.go 用它决定要不要打印一句确认）。
func removeFromOverride(layout config.Layout, id string) (bool, error) {
	ov, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil {
		return false, err
	}
	if ov == nil {
		return false, nil
	}

	changed, next := removeComponentFromOverride(ov.Components, id)
	if !changed {
		return false, nil
	}
	ov.Components = next

	data, err := renderOverride(ov)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(layout.OverridePath(), data, 0o644); err != nil {
		return false, clierr.New(clierr.CodeInternal, i18n.T(msgid.CliOverrideWriteFailed)).
			WithDetail(i18n.T(msgid.LabelPath), layout.OverridePath()).WithCause(err)
	}
	return true, nil
}

// removeComponentFromOverride 从顶层条目列表里删掉 id 对应的那一条（若有），外壳类
// 条目的成员原样提升成顶层；也会到每个外壳的 Members 里找一遍，删掉嵌套的那份。
// id 到底嵌在哪一层，remove 自己不需要关心 brickkit.yaml 当时是怎么声明 servedBy
// 的——override.yaml 里现在长什么样才是唯一要看的事实。
func removeComponentFromOverride(
	entries []override.ComponentOverride, id string,
) (bool, []override.ComponentOverride) {
	changed := false
	out := make([]override.ComponentOverride, 0, len(entries))
	for _, c := range entries {
		if c.ID == id {
			changed = true
			for _, m := range c.Members {
				out = append(out, promoteMember(m))
			}
			continue
		}
		var kept []override.MemberOverride
		for _, m := range c.Members {
			if m.ID == id {
				changed = true
				continue
			}
			kept = append(kept, m)
		}
		c.Members = kept
		out = append(out, c)
	}
	return changed, out
}

// promoteMember 把一个嵌套成员条目转成顶层条目——两个类型字段完全一样，只是
// ComponentOverride 多一个 Members（schemagen 的反射生成器不支持自引用递归类型，
// 这是它们从一开始就是两个类型的原因，见 internal/override/override.go）。
func promoteMember(m override.MemberOverride) override.ComponentOverride {
	return override.ComponentOverride{ID: m.ID, Mode: m.Mode, LocalPort: m.LocalPort, Baseline: m.Baseline}
}
