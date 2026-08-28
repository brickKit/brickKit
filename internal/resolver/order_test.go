// 本文件是 Step 10「拓扑排序」的业务行为测试，覆盖开发计划 10.1、10.2、10.4–10.6。
// 命令层的输出格式（10.3、10.7）见 internal/cli/order_test.go。
package resolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// positions 返回排序结果中每个组件的启动序号。
func positions(t *testing.T, plan *Plan) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, s := range plan.Steps {
		out[s.Ref.String()] = s.Position
	}
	return out
}

// refsInOrder 返回排序后的组件引用序列。
func refsInOrder(plan *Plan) []string {
	out := make([]string, 0, len(plan.Steps))
	for _, s := range plan.Steps {
		out = append(out, s.Ref.String())
	}
	return out
}

// ============================================================
// 10.1 / 10.5 正确的拓扑排序
// ============================================================

// 10.1 用 004 §4.3 的 ERP 依赖链验证排序结果。
func TestOrderERPChain(t *testing.T) {
	f := newFixture(t,
		comp{ID: "portal/user-frontend", Version: "1.0.0", Requires: []string{"erp/backend@1.0.0"}},
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{
			"people/basic@1.0.0", "department/tree@1.0.0", "authorization/rbac@1.0.0",
		}},
		comp{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "authorization/rbac", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "department/tree", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"portal/user-frontend", "1.0.0"})
	require.NoError(t, err)

	plan, err := Order(g)
	require.NoError(t, err)
	require.Len(t, plan.Steps, 5)

	pos := positions(t, plan)
	// 004 §4.3：被依赖的排在前面，依赖方排在后面
	assert.Less(t, pos["department/tree@1.0.0"], pos["people/basic@1.0.0"])
	assert.Less(t, pos["department/tree@1.0.0"], pos["authorization/rbac@1.0.0"])
	assert.Less(t, pos["people/basic@1.0.0"], pos["erp/backend@1.0.0"])
	assert.Less(t, pos["authorization/rbac@1.0.0"], pos["erp/backend@1.0.0"])
	assert.Less(t, pos["erp/backend@1.0.0"], pos["portal/user-frontend@1.0.0"])

	// 10.5 无依赖的组件排第一
	assert.Equal(t, 1, pos["department/tree@1.0.0"])
	assert.Equal(t, "department-tree-1-0-0", plan.Steps[0].Service, "输出用版本化服务名")

	// 序号从 1 连续编号
	for i, s := range plan.Steps {
		assert.Equal(t, i+1, s.Position)
	}
}

// 每一步都记录了自己直接强依赖的序号（004 §3.8 的 "← 依赖 1, 2"）。
func TestOrderRecordsDependencyPositions(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{
			"people/basic@1.0.0", "department/tree@1.0.0",
		}},
		comp{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "department/tree", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.NoError(t, err)
	plan, err := Order(g)
	require.NoError(t, err)

	steps := map[string]PlanStep{}
	for _, s := range plan.Steps {
		steps[s.Ref.ID] = s
	}
	assert.Empty(t, steps["department/tree"].RequirePositions)
	assert.Equal(t, []int{1}, steps["people/basic"].RequirePositions)
	assert.Equal(t, []int{1, 2}, steps["erp/backend"].RequirePositions, "序号升序排列")
}

// 可独立启动的是没有强依赖的组件；最长链是那条最深的强依赖路径。
func TestPlanIndependentAndChain(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{
			"people/basic@1.0.0", "department/tree@1.0.0",
		}},
		comp{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "department/tree", Version: "1.0.0"},
		comp{ID: "infra/cache", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(),
		Ref{"erp/backend", "1.0.0"}, Ref{"infra/cache", "1.0.0"})
	require.NoError(t, err)
	plan, err := Order(g)
	require.NoError(t, err)

	var independent []string
	for _, s := range plan.Independent() {
		independent = append(independent, s.Ref.ID)
	}
	assert.ElementsMatch(t, []string{"department/tree", "infra/cache"}, independent)

	var chain []string
	for _, ref := range plan.Chain {
		chain = append(chain, ref.ID)
	}
	assert.Equal(t, []string{"department/tree", "people/basic", "erp/backend"}, chain,
		"最深的那条路径；infra/cache 谁都不依赖，不在链上")
}

// ============================================================
// 10.2 弱依赖不参与排序约束
// ============================================================

// 弱依赖不产生排序边：它可能根本不启动，让它约束顺序会把可选变成必选。
func TestOptionalDependencyDoesNotConstrainOrder(t *testing.T) {
	f := newFixture(t,
		// ID 特意让弱依赖排在字母序后面：如果弱依赖参与约束，它会被排到前面去
		comp{ID: "aaa/app", Version: "1.0.0", Optional: []string{"zzz/bus@1.0.0"}},
		comp{ID: "zzz/bus", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"aaa/app", "1.0.0"})
	require.NoError(t, err)
	plan, err := Order(g)
	require.NoError(t, err)

	assert.Equal(t, []string{"aaa/app@1.0.0", "zzz/bus@1.0.0"}, refsInOrder(plan),
		"弱依赖不应把 zzz/bus 拉到 aaa/app 前面")

	pos := positions(t, plan)
	assert.Equal(t, 1, pos["aaa/app@1.0.0"], "只有弱依赖的组件本身也算可独立启动")
	assert.Empty(t, plan.Steps[0].RequirePositions)

	// 004 §3.8：弱依赖引入的组件要能单列出来（order 单列一行说明它默认不启动）
	assert.Equal(t, []Ref{{"zzz/bus", "1.0.0"}}, plan.Optional)
}

// 同时被强依赖和弱依赖引用的组件按"强"算，仍然参与排序约束。
func TestDependencyRequiredBySomeoneIsNotOptional(t *testing.T) {
	f := newFixture(t,
		comp{ID: "aaa/app", Version: "1.0.0", Optional: []string{"zzz/bus@1.0.0"}},
		comp{ID: "bbb/worker", Version: "1.0.0", Requires: []string{"zzz/bus@1.0.0"}},
		comp{ID: "zzz/bus", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(),
		Ref{"aaa/app", "1.0.0"}, Ref{"bbb/worker", "1.0.0"})
	require.NoError(t, err)
	plan, err := Order(g)
	require.NoError(t, err)

	assert.Empty(t, plan.Optional, "有人强依赖它，就不再是可跳过的")
	pos := positions(t, plan)
	assert.Less(t, pos["zzz/bus@1.0.0"], pos["bbb/worker@1.0.0"])
}

// ============================================================
// 10.4 循环依赖
// ============================================================

// 排序阶段的环检测是最后一道防线：Manifest 校验与递归解析都放过的环，这里必须拦住。
func TestOrderDetectsCycle(t *testing.T) {
	a := Ref{"a/one", "1.0.0"}
	b := Ref{"b/two", "1.0.0"}
	g := &Graph{
		Nodes: []*Node{
			{Ref: a, Requires: []Ref{b}},
			{Ref: b, Requires: []Ref{a}},
		},
	}

	_, err := Order(g)
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDependencyCycle, e.Code)
	out := e.Format()
	assert.Contains(t, out, "循环依赖")
	assert.Contains(t, out, "a/one@1.0.0")
	assert.Contains(t, out, "b/two@1.0.0")
}

// ============================================================
// 10.6 多版本各自独立排序
// ============================================================

func TestOrderMultipleVersionsIndependently(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/a", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		comp{ID: "erp/b", Version: "1.0.0", Requires: []string{"people/basic@2.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(),
		Ref{"erp/a", "1.0.0"}, Ref{"erp/b", "1.0.0"})
	require.NoError(t, err)
	plan, err := Order(g)
	require.NoError(t, err)

	require.Len(t, plan.Steps, 4)
	pos := positions(t, plan)
	assert.Less(t, pos["people/basic@1.0.0"], pos["erp/a@1.0.0"])
	assert.Less(t, pos["people/basic@2.0.0"], pos["erp/b@1.0.0"])

	services := map[string]bool{}
	for _, s := range plan.Steps {
		services[s.Service] = true
	}
	assert.True(t, services["people-basic-1-0-0"])
	assert.True(t, services["people-basic-2-0-0"], "两个版本各自一个服务名，互不影响")
}

// ============================================================
// 其他
// ============================================================

func TestOrderEmptyGraph(t *testing.T) {
	plan, err := Order(&Graph{})
	require.NoError(t, err)
	assert.Empty(t, plan.Steps)
	assert.Empty(t, plan.Optional)
	assert.Empty(t, plan.Independent())
	assert.Empty(t, plan.Chain)
}

func TestOrderNilGraph(t *testing.T) {
	plan, err := Order(nil)
	require.NoError(t, err)
	assert.Empty(t, plan.Steps)
}

// 同一个依赖图排两次必须得到完全相同的顺序（部署文件要可复现）。
func TestOrderIsDeterministic(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{
			"people/basic@1.0.0", "department/tree@1.0.0", "authorization/rbac@1.0.0",
		}},
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "department/tree", Version: "1.0.0"},
		comp{ID: "authorization/rbac", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.NoError(t, err)

	first, err := Order(g)
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		again, err := Order(g)
		require.NoError(t, err)
		assert.Equal(t, refsInOrder(first), refsInOrder(again))
	}
	// 同层内按组件 ID 排序，结果稳定可预期
	assert.Equal(t, []string{
		"authorization/rbac@1.0.0", "department/tree@1.0.0",
		"people/basic@1.0.0", "erp/backend@1.0.0",
	}, refsInOrder(first))
}

// ============================================================
// 最长依赖链
// ============================================================

// 最长链只沿**强依赖**边走，且与图里别的分支无关。
//
// 这是替掉"必须最后启动：X（需等前 N 个组件就绪）"的那个数。旧说法拿的是
// 拓扑序号，而序号是一条把整张图压平的直线——它把毫不相干的另一条链上的组件
// 也算进了"要等的前 N 个"。使用者据此以为整个 up 是串行的，
// 而它下面那张依赖图恰好在打脸。
func TestPlanLongestChain(t *testing.T) {
	f := newFixture(t,
		// 一条三层链
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "department/tree", Version: "1.0.0"},
		// 一条与它完全无关的两层链
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		comp{ID: "demo/hello", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(),
		Ref{"erp/backend", "1.0.0"}, Ref{"demo/caller", "1.0.0"})
	require.NoError(t, err)
	plan, err := Order(g)
	require.NoError(t, err)

	var chain []string
	for _, ref := range plan.Chain {
		chain = append(chain, ref.ID)
	}
	assert.Equal(t, []string{"department/tree", "people/basic", "erp/backend"}, chain,
		"从最底层排到最上层；demo 那条链更短，不该混进来")
}

// 弱依赖不进链：它不约束启动顺序（弱依赖可能根本不启动）。
//
// 图的形状是刻意挑的：**只有跨过那条弱边才够得到最深的那个节点**。
//
//	erp/backend --强--> people/basic                      （强链深 2）
//	erp/backend --弱--> infra/bus --强--> infra/mid --强--> infra/deep
//
// 弱边算数的话，erp/backend 的深度是 4、链尾就是它；只认强边时最长的是
// infra 那条（深 3），erp/backend 根本不在链上。断言"链里没有 erp/backend"
// 才真的证明了弱边没被算进去——两条链一样长的时候，这个断言证明不了任何事。
func TestLongestChainIgnoresOptional(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/backend", Version: "1.0.0",
			Requires: []string{"people/basic@1.0.0"},
			Optional: []string{"infra/bus@1.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "infra/bus", Version: "1.0.0", Requires: []string{"infra/mid@1.0.0"}},
		comp{ID: "infra/mid", Version: "1.0.0", Requires: []string{"infra/deep@1.0.0"}},
		comp{ID: "infra/deep", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.NoError(t, err)
	plan, err := Order(g)
	require.NoError(t, err)

	var chain []string
	for _, ref := range plan.Chain {
		chain = append(chain, ref.ID)
	}
	assert.Equal(t, []string{"infra/deep", "infra/mid", "infra/bus"}, chain,
		"最长的强依赖链是 infra 那条")
	assert.NotContains(t, chain, "erp/backend",
		"跨过弱边才够得到 infra/deep——弱依赖可能根本不启动，不能算进关键路径")
}

// 全是独立组件时没有链可言：不该报一个假的"深度 2"。
func TestLongestChainIsSingleWhenNothingDepends(t *testing.T) {
	f := newFixture(t,
		comp{ID: "demo/one", Version: "1.0.0"},
		comp{ID: "demo/two", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(),
		Ref{"demo/one", "1.0.0"}, Ref{"demo/two", "1.0.0"})
	require.NoError(t, err)
	plan, err := Order(g)
	require.NoError(t, err)

	assert.Len(t, plan.Chain, 1, "没有任何强依赖边时，最长链就是一个组件")
}

// 同样长的两条链要给出稳定的那一条：同一份配置连跑两次必须输出一致，
// 否则使用者会以为哪里在飘。
func TestLongestChainIsDeterministic(t *testing.T) {
	f := newFixture(t,
		comp{ID: "a/top", Version: "1.0.0", Requires: []string{"a/leaf@1.0.0"}},
		comp{ID: "a/leaf", Version: "1.0.0"},
		comp{ID: "b/top", Version: "1.0.0", Requires: []string{"b/leaf@1.0.0"}},
		comp{ID: "b/leaf", Version: "1.0.0"},
	)

	var first []Ref
	for i := 0; i < 5; i++ {
		g, err := f.Resolver.Resolve(context.Background(),
			Ref{"a/top", "1.0.0"}, Ref{"b/top", "1.0.0"})
		require.NoError(t, err)
		plan, err := Order(g)
		require.NoError(t, err)
		if i == 0 {
			first = plan.Chain
			continue
		}
		assert.Equal(t, first, plan.Chain, "两条一样长的链，每次都要挑同一条")
	}
}
