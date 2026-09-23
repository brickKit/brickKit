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

// planOverrideRemoval 解析 override.yaml 并算出去掉 id 之后的新内容——**只读、
// 纯计算，从不写文件**，所以必须在 brickkit.yaml 的 edit.Save() 之前调用。
//
// 这是 checkSourceDeletable 那几行注释早就讲过的同一条纪律："拦下时不该留下
// 配置改了一半、源码还在的现场"——旧版本在 Save() 之后才解析 override.yaml，
// 解析失败时 brickkit.yaml 已经少了这一条、源码目录还在，组件"半删"在那儿，
// 重跑 remove 只会得到"不在配置里"，用户没有退路（评审真机复现，Important #2）。
// 把唯一可能失败的一步（解析）挪到 Save() 之前，才能保证失败时 brickkit.yaml
// 一个字节都没动。
//
// changed 为 false 时（文件不存在，或压根没有这个 id 的条目）返回的 *override.Override
// 是 nil，调用方不需要再调 writeOverrideRemoval。
func planOverrideRemoval(layout config.Layout, id string) (*override.Override, bool, error) {
	ov, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil {
		return nil, false, err
	}
	if ov == nil {
		return nil, false, nil
	}

	changed, next := removeComponentFromOverride(ov.Components, id)
	if !changed {
		return nil, false, nil
	}
	ov.Components = next
	return ov, true, nil
}

// writeOverrideRemoval 把 planOverrideRemoval 已经算好的新内容写盘——只在
// brickkit.yaml 的 edit.Save() **成功之后**调用，这一步唯一还可能失败的原因
// 是文件系统本身（磁盘满、权限），跟 edit.Save() 自己面对的风险是同一级别，
// 不是这里的新增负担。
func writeOverrideRemoval(layout config.Layout, ov *override.Override) error {
	data, err := renderOverride(ov)
	if err != nil {
		return err
	}
	if err := os.WriteFile(layout.OverridePath(), data, 0o644); err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliOverrideWriteFailed)).
			WithDetail(i18n.T(msgid.LabelPath), layout.OverridePath()).WithCause(err)
	}
	return nil
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
