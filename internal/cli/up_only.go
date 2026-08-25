package cli

// 本文件是 `brickkit up --only` 的启动集合裁剪（004 §3.5）。
//
// 只启动被点名的那一个是起不来的——它依赖的东西不在，容器起来也连不上。
// 所以这里做的是**依赖闭包**（强弱都算，见 cascade.Focus），
// 并把没被选中的组件标成"跳过"而不是悄悄丢掉。

import (
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/resolver"
)

// restrictToOnly 把启动集合收窄到 --only 指定的组件**及其依赖**。
//
// 闭包强弱都算：既然"跑不跑"上两者一视同仁（cascade 包开头），
// 收窄范围时也该一致——只取强依赖的话，点名一个组件会得到一个
// 异步功能是哑的它。
func restrictToOnly(opts *Options, plan *upPlan, only []string) (*cascade.Result, error) {
	selected, err := selectRefsIn(plan.cfg, only)
	if err != nil {
		return nil, err
	}

	// 15.9：点名一个被显式关掉的组件，两个意图直接冲突
	for _, ref := range selected {
		if stateOf(plan.states, ref) == cascade.StateDisabled {
			return nil, clierr.Newf(clierr.CodeComponentDisabled,
				"错误：--only 指定的组件被显式禁用").
				WithDetail("组件", refText(ref)).
				WithDetail("原因", "brickkit.yaml 中该组件是 enabled: false").
				WithHint(
					"移除该组件的 enabled: false",
					"确认这次确实要启动它——显式禁用通常是有意的",
				)
		}
	}

	opts.Printf("🎯 --only：只启动 %s 及其依赖\n", strings.Join(only, "、"))
	return cascade.Focus(plan.cfg, plan.graph, selected)
}

// selectRefsIn 解析 --only 的写法（004 §3.5）。
//
//	people/basic          该组件的**所有**版本（多版本默认共存，002 §3.6）
//	people/basic@1.0.0    只这一个版本
func selectRefsIn(cfg *config.Config, only []string) ([]resolver.Ref, error) {
	var out []resolver.Ref
	seen := map[resolver.Ref]bool{}

	for _, item := range only {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, version, hasVersion := strings.Cut(item, "@")

		matched := false
		for _, c := range cfg.Components {
			if c.ID != id || (hasVersion && c.Version != version) {
				continue
			}
			ref := resolver.Ref{ID: c.ID, Version: c.Version}
			if !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
			matched = true
		}
		if !matched {
			return nil, unknownComponentError(item, cfg)
		}
	}
	return out, nil
}

func stateOf(states *cascade.Result, ref resolver.Ref) cascade.State {
	for _, c := range states.Components {
		if c.Ref == ref {
			return c.State
		}
	}
	return cascade.StateSkipped
}
