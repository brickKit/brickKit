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
    mode: disable
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

// 依赖图解析成功、级联算不出来（钉住的组件撞上被关掉的强依赖）：两个结果都必须是 nil。
// 这是 resolveTopology 与它取代的内联写法唯一有差别的路径：内联写法把结果直接赋给调用方
// 自己的字段（up 的 plan.graph、down / status 的 p.graph），级联失败时依赖图已经落在那里了；
// 共用函数保证出错时不交出任何半成品，哪个调用方将来忘了先看 err，也拿不到能用的东西。
func TestResolveTopologyFailsWhenCascadeCannotBeComputed(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		comp{ID: "demo/hello", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	f.writeConfig(t, `components:
  - id: demo/caller
    version: 1.0.0
    mode: enabled
  - id: demo/hello
    version: 1.0.0
    mode: disable
resources: []
`)

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.Error(t, err)
	assert.Nil(t, graph, "依赖图是解析成功了的，但出错时不交出半成品")
	assert.Nil(t, states)
	assert.Equal(t, clierr.CodeComponentDisabled, clierr.As(err).Code, "是级联报的错，不是依赖解析报的")
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
