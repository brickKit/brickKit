// 本文件是 Step 10 排序逻辑的代码层单测：图不完整、环检测细节与排序稳定性。
package resolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 依赖指向图外的组件（弱依赖缺失后手工拼图等情形）时跳过该边，不影响排序。
func TestOrderIgnoresDependenciesOutsideGraph(t *testing.T) {
	inside := Ref{"a/one", "1.0.0"}
	outside := Ref{"missing/dep", "1.0.0"}
	g := &Graph{Nodes: []*Node{
		{Ref: inside, Requires: []Ref{outside}, Optional: []Ref{outside}},
	}}

	plan, err := Order(g)
	require.NoError(t, err)
	require.Len(t, plan.Steps, 1)
	assert.Equal(t, inside, plan.Steps[0].Ref)
	assert.Empty(t, plan.Steps[0].Requires, "图外的依赖不参与排序")
	assert.Empty(t, plan.Optional, "图外的弱依赖也不列入可跳过")
}

// 环之外还挂着一条尾巴时，只打印环本身。
func TestCycleAmongReportsOnlyTheCycle(t *testing.T) {
	a := Ref{"a/one", "1.0.0"}
	b := Ref{"b/two", "1.0.0"}
	c := Ref{"c/three", "1.0.0"}
	// c → a → b → a：c 不在环上
	g := &Graph{Nodes: []*Node{
		{Ref: a, Requires: []Ref{b}},
		{Ref: b, Requires: []Ref{a}},
		{Ref: c, Requires: []Ref{a}},
	}}

	_, err := Order(g)
	require.Error(t, err)
	out := clierr.As(err).Format()
	assert.Contains(t, out, "a/one@1.0.0 → b/two@1.0.0 → a/one@1.0.0")
	assert.NotContains(t, out, "c/three@1.0.0 → a/one@1.0.0")
}

// 自环同样被拦下。
func TestOrderDetectsSelfCycle(t *testing.T) {
	a := Ref{"a/one", "1.0.0"}
	g := &Graph{Nodes: []*Node{{Ref: a, Requires: []Ref{a}}}}

	_, err := Order(g)
	require.Error(t, err)
	assert.Equal(t, clierr.CodeDependencyCycle, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Format(), "a/one@1.0.0 → a/one@1.0.0")
}

// 两个互不相干的环：报出其中一个即可，但不能漏报。
func TestOrderDetectsOneOfSeveralCycles(t *testing.T) {
	g := &Graph{Nodes: []*Node{
		{Ref: Ref{"a/one", "1.0.0"}, Requires: []Ref{{"a/two", "1.0.0"}}},
		{Ref: Ref{"a/two", "1.0.0"}, Requires: []Ref{{"a/one", "1.0.0"}}},
		{Ref: Ref{"b/one", "1.0.0"}, Requires: []Ref{{"b/two", "1.0.0"}}},
		{Ref: Ref{"b/two", "1.0.0"}, Requires: []Ref{{"b/one", "1.0.0"}}},
	}}

	_, err := Order(g)
	require.Error(t, err)
	assert.Equal(t, clierr.CodeDependencyCycle, clierr.As(err).Code)
}

// 同 ID 的多个版本在同一层时按版本号数字序排列。
func TestSortRefsOrdersByIDThenVersion(t *testing.T) {
	refs := []Ref{
		{"people/basic", "10.0.0"},
		{"people/basic", "2.0.0"},
		{"department/tree", "1.0.0"},
	}
	sortRefs(refs)

	assert.Equal(t, []Ref{
		{"department/tree", "1.0.0"},
		{"people/basic", "2.0.0"},
		{"people/basic", "10.0.0"},
	}, refs)
}

// 没有强依赖的一组组件全部可独立启动，且序号连续。
func TestOrderAllIndependent(t *testing.T) {
	g := &Graph{Nodes: []*Node{
		{Ref: Ref{"b/two", "1.0.0"}},
		{Ref: Ref{"a/one", "1.0.0"}},
	}}

	plan, err := Order(g)
	require.NoError(t, err)
	assert.Equal(t, []string{"a/one@1.0.0", "b/two@1.0.0"}, refsInOrder(plan))
	assert.Len(t, plan.Independent(), 2)
	assert.Equal(t, "b/two@1.0.0", plan.Last().Ref.String())
}
