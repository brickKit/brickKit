// 本文件用随机图验证 Order（拓扑排序）的几条性质。
//
// # 为什么要有它
//
// order_test.go / order_edge_test.go 是示例式的：每个用例一张手画的图。排序有几条
// **对任何图都必须成立**的性质，手画几张图证明不了它们——而生成的部署文件、迁移
// 顺序、`up` 的启动顺序都建立在这几条性质上：
//
//	依赖先于依赖方   每条强依赖边，被依赖方的序号都更小
//	每个组件恰好一次 序号是 1..n 的排列
//	弱依赖不约束顺序 删掉全部弱依赖边，顺序一个字都不变（弱依赖可能根本不启动，
//	                 让它约束顺序等于把"可选"偷偷变成"必选"）
//	顺序是确定的     打乱输入组件的顺序，得到的启动顺序完全相同
//	                 （生成的部署文件要可复现，不能因为 map 遍历顺序而变）
//	关键路径         Chain 的长度等于最长强依赖链，且相邻两个确实是强依赖边
//
// 性质来自 Order 的文档注释。随机图只生成合法输入：强依赖只指向编号更大的组件
// （强依赖环是另一种错误，由 cycleAmong 的示例测试管），弱依赖可以指向任何方向。
// 规模压小、种子固定，失败信息里带着种子，原样可复现。
package resolver

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

const orderPropertyCases = 2000

func propertyRef(i int) Ref { return Ref{ID: fmt.Sprintf("c%d/x", i), Version: "1.0.0"} }

// randomGraph 造一张随机图：强依赖只指向编号更大的组件，弱依赖可以指向任何方向。
func randomGraph(rng *rand.Rand) *Graph {
	n := 1 + rng.Intn(8)
	nodes := make([]*Node, n)
	for i := range nodes {
		nodes[i] = &Node{Ref: propertyRef(i)}
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j || rng.Float64() >= 0.3 {
				continue
			}
			if j > i && rng.Intn(2) == 0 {
				nodes[i].Requires = append(nodes[i].Requires, nodes[j].Ref)
			} else {
				nodes[i].Optional = append(nodes[i].Optional, nodes[j].Ref)
			}
		}
	}
	return &Graph{Nodes: nodes}
}

// describeGraph 把图写成一行，失败信息里用。
func describeGraph(g *Graph) string {
	out := ""
	for _, n := range g.Nodes {
		out += fmt.Sprintf("%s[强:%v 弱:%v] ", n.Ref.ID, refIDs(n.Requires), refIDs(n.Optional))
	}
	return out
}

func refIDs(refs []Ref) []string {
	ids := make([]string, len(refs))
	for i, r := range refs {
		ids[i] = r.ID
	}
	return ids
}

func stepRefs(p *Plan) []Ref {
	out := make([]Ref, len(p.Steps))
	for i, s := range p.Steps {
		out[i] = s.Ref
	}
	return out
}

// longestRequiredChain 是最长强依赖链的长度，用一趟独立的动态规划算——
// 强依赖只指向编号更大的组件，所以从大到小算每个组件的深度即可。
func longestRequiredChain(g *Graph) int {
	depth := map[Ref]int{}
	best := 0
	for i := len(g.Nodes) - 1; i >= 0; i-- {
		d := 1
		for _, dep := range g.Nodes[i].Requires {
			if depth[dep]+1 > d {
				d = depth[dep] + 1
			}
		}
		depth[g.Nodes[i].Ref] = d
		if d > best {
			best = d
		}
	}
	return best
}

func TestOrderPropertiesHoldOnRandomGraphs(t *testing.T) {
	withRequires := 0

	for seed := int64(1); seed <= orderPropertyCases; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g := randomGraph(rng)
		context := fmt.Sprintf("种子 %d\n%s", seed, describeGraph(g))

		plan, err := Order(g)
		require.NoError(t, err, "强依赖无环的图必须能排出顺序（弱依赖成环不算环）\n%s", context)
		require.Len(t, plan.Steps, len(g.Nodes), "每个组件必须恰好出现一次\n%s", context)

		position := map[Ref]int{}
		for i, step := range plan.Steps {
			require.Equal(t, i+1, step.Position, "序号必须是 1..n 连续\n%s", context)
			_, dup := position[step.Ref]
			require.False(t, dup, "%s 出现了两次\n%s", step.Ref, context)
			position[step.Ref] = step.Position
		}

		// 依赖先于依赖方。
		for _, n := range g.Nodes {
			for _, dep := range n.Requires {
				withRequires++
				require.Less(t, position[dep], position[n.Ref],
					"%s 强依赖 %s，后者必须排在前面\n%s", n.Ref.ID, dep.ID, context)
			}
		}

		// 弱依赖不约束顺序：删掉全部弱依赖边，顺序与关键路径都不变。
		strong := &Graph{Nodes: make([]*Node, len(g.Nodes))}
		for i, n := range g.Nodes {
			strong.Nodes[i] = &Node{Ref: n.Ref, Requires: n.Requires}
		}
		strongPlan, err := Order(strong)
		require.NoError(t, err, context)
		require.Equal(t, stepRefs(plan), stepRefs(strongPlan), "弱依赖改变了启动顺序\n%s", context)
		require.Equal(t, plan.Chain, strongPlan.Chain, "弱依赖改变了关键路径\n%s", context)

		// 顺序是确定的：打乱输入组件的顺序，启动顺序与关键路径完全相同。
		shuffled := &Graph{Nodes: make([]*Node, len(g.Nodes))}
		for to, from := range rng.Perm(len(g.Nodes)) {
			shuffled.Nodes[to] = g.Nodes[from]
		}
		shuffledPlan, err := Order(shuffled)
		require.NoError(t, err, context)
		require.Equal(t, stepRefs(plan), stepRefs(shuffledPlan), "打乱输入顺序后启动顺序变了\n%s", context)
		require.Equal(t, plan.Chain, shuffledPlan.Chain, "打乱输入顺序后关键路径变了\n%s", context)

		// 关键路径：长度等于最长强依赖链，相邻两个确实是强依赖边（依赖在前，依赖方在后）。
		require.Len(t, plan.Chain, longestRequiredChain(g), "关键路径不是最长的强依赖链\n%s", context)
		for k := 1; k < len(plan.Chain); k++ {
			upper := shuffled.Nodes[0] // 占位，下面按 Ref 找
			for _, n := range g.Nodes {
				if n.Ref == plan.Chain[k] {
					upper = n
				}
			}
			require.Contains(t, upper.Requires, plan.Chain[k-1],
				"关键路径里 %s 排在 %s 后面，但前者并不强依赖后者\n%s", plan.Chain[k].ID, plan.Chain[k-1].ID, context)
		}
	}

	// 覆盖率自检：随机图里要真的有强依赖边，否则"依赖先于依赖方"是空真。
	require.Greater(t, withRequires, orderPropertyCases, "随机图里强依赖边太少（%d），生成器没覆盖到", withRequires)
}
