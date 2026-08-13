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

// 可独立启动的是没有强依赖的组件；"必须最后启动"的是依赖最多的那个。
func TestPlanIndependentAndLast(t *testing.T) {
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

	last := plan.Last()
	require.NotNil(t, last)
	assert.Equal(t, "erp/backend", last.Ref.ID, "依赖最多的排最后")
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

	// 004 §3.8：弱依赖引入的组件要能单列出来（"可跳过（弱依赖）"）
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
	assert.Nil(t, plan.Last())
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
