// 本文件测试 Graph.Subgraph（Step 11：先算级联、再对启动集合排序）。
package resolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/manifest"
)

// 子图只保留给定组件，并把指向图外组件的依赖边裁掉 ——
// 留着这些边会让拓扑排序等一个永远不会启动的组件。
func TestSubgraphKeepsOnlyGivenRefsAndPrunesEdges(t *testing.T) {
	provider := newFakeProvider().
		add("erp/backend", "1.0.0",
			manifest.ComponentDep{ID: "people/basic", Version: "1.0.0"},
			manifest.ComponentDep{ID: "infra/redis-event-bus", Version: "1.0.0", Optional: true}).
		add("people/basic", "1.0.0").
		add("infra/redis-event-bus", "1.0.0")

	full, err := New(provider).Resolve(context.Background(),
		Ref{ID: "erp/backend", Version: "1.0.0"})
	require.NoError(t, err)

	backend := Ref{ID: "erp/backend", Version: "1.0.0"}
	sub := full.Subgraph([]Ref{backend})

	require.Len(t, sub.Nodes, 1)
	assert.Equal(t, backend, sub.Nodes[0].Ref)
	assert.Empty(t, sub.Nodes[0].Requires, "指向图外的强依赖边要裁掉")
	assert.Empty(t, sub.Nodes[0].Optional, "弱依赖边同样裁掉")
	assert.True(t, sub.Has(backend))
	assert.False(t, sub.Has(Ref{ID: "people/basic", Version: "1.0.0"}))
}

// 保留下来的组件之间的依赖边要留着：排序依赖它。
func TestSubgraphKeepsInternalEdges(t *testing.T) {
	provider := newFakeProvider().
		add("erp/backend", "1.0.0", manifest.ComponentDep{ID: "people/basic", Version: "1.0.0"}).
		add("people/basic", "1.0.0")

	full, err := New(provider).Resolve(context.Background(),
		Ref{ID: "erp/backend", Version: "1.0.0"})
	require.NoError(t, err)

	sub := full.Subgraph(full.Refs())
	plan, err := Order(sub)
	require.NoError(t, err)

	require.Len(t, plan.Steps, 2)
	assert.Equal(t, "people/basic", plan.Steps[0].Ref.ID, "被依赖的排前面")
	assert.Equal(t, "erp/backend", plan.Steps[1].Ref.ID)
}

// 空集合得到空图，不 panic。
func TestSubgraphWithNoRefs(t *testing.T) {
	provider := newFakeProvider().add("people/basic", "1.0.0")
	full, err := New(provider).Resolve(context.Background(),
		Ref{ID: "people/basic", Version: "1.0.0"})
	require.NoError(t, err)

	sub := full.Subgraph(nil)

	assert.Empty(t, sub.Nodes)
	assert.Empty(t, sub.Roots)
}
