package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// newTopologyClient 为一个测试项目构造安装源客户端。
func newTopologyClient(t *testing.T, f *projectFixture, cfg *config.Config) *source.Client {
	t.Helper()
	client, err := source.New(f.Layout, cfg, source.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestResolveTopologyReturnsGraphAndCascade(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		comp{ID: "demo/hello", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "--local").code)

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)

	assert.Len(t, graph.Nodes, 2)
	assert.ElementsMatch(t,
		[]resolver.Ref{{ID: "demo/caller", Version: "1.0.0"}, {ID: "demo/hello", Version: "1.0.0"}},
		states.Running())
}

func TestResolveTopologyHonoursDisabledTopLevel(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		comp{ID: "demo/hello", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	f.writeConfig(t, `components:
  - id: demo/caller
    version: 1.0.0
    enabled: false
resources: []
`)

	cfg := f.parsed(t)
	_, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)
	assert.True(t, states.Empty(), "顶层被关掉，它带来的依赖也不跑")
}

func TestResolveTopologyFailsWhenComponentMissing(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	f.writeConfig(t, `components:
  - id: demo/ghost
    version: 1.0.0
resources: []
`)

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.Error(t, err)
	assert.Nil(t, graph)
	assert.Nil(t, states)
	assert.NotEmpty(t, clierr.As(err).Code, "是结构化错误，带稳定的错误码")
}

func TestClearServedBy(t *testing.T) {
	cfg := &config.Config{Components: []config.Component{
		{ID: "demo/a", Version: "1.0.0", ServedBy: "demo/shell@1.0.0"},
		{ID: "demo/shell", Version: "1.0.0"},
		{ID: "demo/b", Version: "1.0.0", ServedBy: "demo/shell@1.0.0"},
	}}
	clearServedBy(cfg)
	for _, c := range cfg.Components {
		assert.Empty(t, c.ServedBy, c.ID)
	}
	assert.Len(t, cfg.Components, 3, "只清 servedBy，条目本身不动")
}
