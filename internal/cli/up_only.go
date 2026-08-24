package cli

// 本文件是 `brickkit up --only` 的启动集合裁剪（004 §3.5）。
//
// 只启动被点名的那一个是起不来的——它的强依赖不在，容器起来也连不上。
// 所以这里做的是**强依赖闭包**，并把没被选中的组件标成"跳过"而不是悄悄丢掉。

import (
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/resolver"
)

// restrictToOnly 把启动集合收窄到 --only 指定的组件**及其强依赖**。
//
// 只启动被点名的那一个是起不来的：它的强依赖不在，容器起来也连不上
// （003 §4.3 的可行性判定同理）。所以这里取的是强依赖闭包。
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
					"移除该组件的 enabled: false，或改成 enabled: true",
					"确认这次确实要启动它——显式禁用通常是有意的",
				)
		}
	}

	opts.Printf("🎯 --only：只启动 %s 及其强依赖\n", strings.Join(only, "、"))

	// 重新算，而不是在已有结果上做交集。
	//
	// 交集是原来的做法，它把 --only 理解成"在会启动的那些里再挑几个"。
	// 于是点名一个**只被弱依赖引用**的组件时什么都不会启动——它本来就不在
	// 级联结果里（003 §4.3），而 004 §3.5 承诺的是"只启动指定组件及其依赖"。
	//
	// 级联回答的是"**你没说的时候**该跑什么"；命令行上点了名就是最明确的意图。
	// cascade.Focus 因此把点名的组件当种子（等同钉住），根组件不再自动启动。
	return cascade.Focus(plan.cfg, plan.graph, selected)
}

// addWithRequires 把该组件与它的强依赖递归加进集合。
func addWithRequires(graph *resolver.Graph, ref resolver.Ref, keep map[resolver.Ref]bool) {
	if keep[ref] {
		return
	}
	keep[ref] = true

	node := graph.Node(ref)
	if node == nil {
		return
	}
	for _, dep := range node.Requires {
		addWithRequires(graph, dep, keep)
	}
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
