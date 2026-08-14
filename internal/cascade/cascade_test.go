// 本文件是 Step 11 级联启停计算（延后项 P1 / P14）的业务行为测试。
//
// 规则来自 003 §4.3：enabled 是三态字段，CLI 据此算出"这次到底跑哪些组件"。
// 这里的每个用例都对应设计书里写死的一条判定。
package cascade_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// ============================================================
// 夹具
// ============================================================

// spec 描述一个组件及其依赖。
type spec struct {
	id       string
	requires []string
	optional []string
}

func ref(id string) resolver.Ref { return resolver.Ref{ID: id, Version: "1.0.0"} }

func refs(ids ...string) []resolver.Ref {
	out := make([]resolver.Ref, 0, len(ids))
	for _, id := range ids {
		out = append(out, ref(id))
	}
	return out
}

// stubProvider 按 spec 提供 Manifest，让测试走真实的解析器建图。
type stubProvider map[string]*manifest.Manifest

func (p stubProvider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	m, ok := p[id+"@"+version]
	if !ok {
		return nil, clierr.New(clierr.CodeComponentNotFound, "错误：测试夹具里没有 "+id+"@"+version)
	}
	return m, nil
}

// newGraph 用真实解析器造一张依赖图（Dependents 由解析器回填）。
func newGraph(t *testing.T, specs ...spec) *resolver.Graph {
	t.Helper()

	provider := stubProvider{}
	roots := make([]resolver.Ref, 0, len(specs))
	for _, s := range specs {
		m := &manifest.Manifest{
			Metadata: manifest.Metadata{ID: s.id, Name: s.id, Version: "1.0.0"},
		}
		if len(s.requires)+len(s.optional) > 0 {
			m.Dependencies = &manifest.Dependencies{}
		}
		for _, dep := range s.requires {
			m.Dependencies.Components = append(m.Dependencies.Components,
				manifest.ComponentDep{ID: dep, Version: "1.0.0"})
		}
		for _, dep := range s.optional {
			m.Dependencies.Components = append(m.Dependencies.Components,
				manifest.ComponentDep{ID: dep, Version: "1.0.0", Optional: true})
		}
		provider[s.id+"@1.0.0"] = m
		roots = append(roots, ref(s.id))
	}

	graph, err := resolver.New(provider).Resolve(context.Background(), roots...)
	require.NoError(t, err)
	return graph
}

// cfgOf 造一份 brickkit.yaml 配置。enabled 用 "" / "true" / "false" 表示三态。
func cfgOf(entries ...[2]string) *config.Config {
	cfg := &config.Config{}
	for _, e := range entries {
		c := config.Component{ID: e[0], Version: "1.0.0"}
		switch e[1] {
		case "true":
			on := true
			c.Enabled = &on
		case "false":
			off := false
			c.Enabled = &off
		}
		cfg.Components = append(cfg.Components, c)
	}
	return cfg
}

func entry(id, enabled string) [2]string { return [2]string{id, enabled} }

// runningIDs 返回实际启动的组件 ID。
func runningIDs(r *cascade.Result) []string {
	out := make([]string, 0, len(r.Components))
	for _, c := range r.Components {
		if c.State == cascade.StateRunning {
			out = append(out, c.Ref.ID)
		}
	}
	return out
}

// reasonOf 返回某个组件的判定理由。
func reasonOf(t *testing.T, r *cascade.Result, id string) (cascade.State, string) {
	t.Helper()
	for _, c := range r.Components {
		if c.Ref.ID == id {
			return c.State, c.Reason
		}
	}
	require.Failf(t, "结果里没有该组件", "%s，实际：%v", id, r.Components)
	return "", ""
}

// ============================================================
// 003 §4.3 的完整示例
// ============================================================

// 设计书里那张表是这个算法的验收标准，逐行对齐。
func TestCascadeMatchesDesignExample(t *testing.T) {
	graph := newGraph(t,
		spec{id: "portal/user-frontend", requires: []string{"erp/backend"}},
		spec{id: "erp/backend", requires: []string{"people/basic", "authorization/rbac", "department/tree"}},
		spec{id: "people/basic", requires: []string{"department/tree"}},
		spec{id: "authorization/rbac", requires: []string{"department/tree"}},
		spec{id: "department/tree"},
	)
	cfg := cfgOf(
		entry("department/tree", ""),
		entry("people/basic", ""),
		entry("authorization/rbac", "true"),
		entry("erp/backend", "false"),
		entry("portal/user-frontend", ""),
	)

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"authorization/rbac", "department/tree"}, runningIDs(result))

	state, reason := reasonOf(t, result, "erp/backend")
	assert.Equal(t, cascade.StateDisabled, state)
	assert.Contains(t, reason, "显式禁用")

	state, reason = reasonOf(t, result, "portal/user-frontend")
	assert.Equal(t, cascade.StateSkipped, state)
	assert.Contains(t, reason, "erp/backend", "要说清是被哪个组件拖下来的：%s", reason)

	state, reason = reasonOf(t, result, "people/basic")
	assert.Equal(t, cascade.StateSkipped, state)
	assert.Contains(t, reason, "没有启用中的组件依赖它")

	_, reason = reasonOf(t, result, "authorization/rbac")
	assert.Contains(t, reason, "钉住")

	_, reason = reasonOf(t, result, "department/tree")
	assert.Contains(t, reason, "authorization/rbac", "要说清是被谁拉起来的：%s", reason)
}

// ============================================================
// 三态的基本行为
// ============================================================

// 没写 enabled 的根组件默认启动（003 §4.3）。
func TestRootWithoutEnabledRuns(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
	)

	result, err := cascade.Compute(cfg2(t, "erp/backend", "people/basic"), graph)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"erp/backend", "people/basic"}, runningIDs(result))
}

func cfg2(t *testing.T, ids ...string) *config.Config {
	t.Helper()
	entries := make([][2]string, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, entry(id, ""))
	}
	return cfgOf(entries...)
}

// enabled: false 一定不启动，哪怕它是根组件。
func TestExplicitlyDisabledNeverRuns(t *testing.T) {
	graph := newGraph(t, spec{id: "people/basic"})

	result, err := cascade.Compute(cfgOf(entry("people/basic", "false")), graph)
	require.NoError(t, err)

	assert.Empty(t, runningIDs(result))
	state, _ := reasonOf(t, result, "people/basic")
	assert.Equal(t, cascade.StateDisabled, state)
}

// enabled: true 是"钉住"：没有任何组件依赖它也要跑。
func TestPinnedComponentRunsWithoutDependents(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
		spec{id: "infra/redis-event-bus"},
	)
	cfg := cfgOf(
		entry("erp/backend", "false"),
		entry("people/basic", ""),
		entry("infra/redis-event-bus", "true"),
	)

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.Equal(t, []string{"infra/redis-event-bus"}, runningIDs(result))
}

// 被启动中的组件强依赖 → 跟着启动。
func TestStrongDependencyIsPulledIn(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic", requires: []string{"department/tree"}},
		spec{id: "department/tree"},
	)

	result, err := cascade.Compute(cfg2(t, "erp/backend", "people/basic", "department/tree"), graph)
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{"erp/backend", "people/basic", "department/tree"}, runningIDs(result))
}

// 弱依赖不会被级联拉起：它本来就是"有就用、没有就降级"的东西。
// 要让它跑就显式 enabled: true。
func TestWeakDependencyIsNotPulledIn(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", optional: []string{"infra/redis-event-bus"}},
		spec{id: "infra/redis-event-bus"},
	)

	result, err := cascade.Compute(cfg2(t, "erp/backend", "infra/redis-event-bus"), graph)
	require.NoError(t, err)

	assert.Equal(t, []string{"erp/backend"}, runningIDs(result))
	state, reason := reasonOf(t, result, "infra/redis-event-bus")
	assert.Equal(t, cascade.StateSkipped, state)
	assert.Contains(t, reason, "弱依赖")
}

// 弱依赖被钉住时照常启动。
func TestPinnedWeakDependencyRuns(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", optional: []string{"infra/redis-event-bus"}},
		spec{id: "infra/redis-event-bus"},
	)
	cfg := cfgOf(entry("erp/backend", ""), entry("infra/redis-event-bus", "true"))

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"erp/backend", "infra/redis-event-bus"}, runningIDs(result))
}

// ============================================================
// P14：强依赖被禁用（004 §10.3）
// ============================================================

// 钉住的组件的强依赖被禁用 → 这是矛盾的意图，必须报错，
// 而不是偷偷启动一个必然崩溃的组件。
func TestPinnedComponentWithDisabledStrongDependencyIsAnError(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"authorization/rbac"}},
		spec{id: "authorization/rbac"},
	)
	cfg := cfgOf(entry("erp/backend", "true"), entry("authorization/rbac", "false"))

	_, err := cascade.Compute(cfg, graph)

	require.Error(t, err)
	e := clierr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, clierr.CodeComponentDisabled, e.Code)

	rendered := e.Format()
	assert.Contains(t, rendered, "authorization/rbac")
	assert.Contains(t, rendered, "erp/backend")
	assert.Contains(t, rendered, "enabled: false", "要告诉使用者去哪儿改：%s", rendered)
}

// 间接强依赖被禁用同样要报错，并且把链路指出来。
func TestPinnedComponentWithTransitivelyDisabledDependencyIsAnError(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic", requires: []string{"department/tree"}},
		spec{id: "department/tree"},
	)
	cfg := cfgOf(
		entry("erp/backend", "true"),
		entry("people/basic", ""),
		entry("department/tree", "false"),
	)

	_, err := cascade.Compute(cfg, graph)

	require.Error(t, err)
	rendered := clierr.As(err).Format()
	assert.Contains(t, rendered, "department/tree")
	assert.Contains(t, rendered, "erp/backend → people/basic → department/tree",
		"要打印完整依赖链：%s", rendered)
}

// 没被钉住的组件遇到同样的情况不报错，只是级联跳过 ——
// 这是"用户关掉了一整条链"的正常操作。
func TestUnpinnedComponentWithDisabledDependencyIsSkippedNotAnError(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"authorization/rbac"}},
		spec{id: "authorization/rbac"},
	)
	cfg := cfgOf(entry("erp/backend", ""), entry("authorization/rbac", "false"))

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.Empty(t, runningIDs(result))
	state, reason := reasonOf(t, result, "erp/backend")
	assert.Equal(t, cascade.StateSkipped, state)
	assert.Contains(t, reason, "authorization/rbac")
}

// 弱依赖被禁用不影响依赖方启动（这正是弱依赖存在的意义）。
func TestDisabledWeakDependencyDoesNotBlockDependent(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", optional: []string{"infra/redis-event-bus"}},
		spec{id: "infra/redis-event-bus"},
	)
	cfg := cfgOf(entry("erp/backend", ""), entry("infra/redis-event-bus", "false"))

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.Equal(t, []string{"erp/backend"}, runningIDs(result))
}

// ============================================================
// 边界
// ============================================================

// 多版本共存时按"组件ID@版本"分别判定，不能互相牵连。
func TestVersionsAreJudgedIndependently(t *testing.T) {
	provider := stubProvider{}
	var roots []resolver.Ref
	for _, v := range []string{"1.0.0", "2.0.0"} {
		provider["people/basic@"+v] = &manifest.Manifest{
			Metadata: manifest.Metadata{ID: "people/basic", Name: "people", Version: v},
		}
		roots = append(roots, resolver.Ref{ID: "people/basic", Version: v})
	}
	g, err := resolver.New(provider).Resolve(context.Background(), roots...)
	require.NoError(t, err)

	on, off := true, false
	cfg := &config.Config{Components: []config.Component{
		{ID: "people/basic", Version: "1.0.0", Enabled: &on},
		{ID: "people/basic", Version: "2.0.0", Enabled: &off},
	}}

	result, err := cascade.Compute(cfg, g)
	require.NoError(t, err)

	require.Len(t, result.Components, 2)
	for _, c := range result.Components {
		if c.Ref.Version == "1.0.0" {
			assert.Equal(t, cascade.StateRunning, c.State)
		} else {
			assert.Equal(t, cascade.StateDisabled, c.State)
		}
	}
}

// 全部被禁用时不报错：这是合法的"先都停下来"的状态。
func TestAllDisabledIsNotAnError(t *testing.T) {
	graph := newGraph(t, spec{id: "people/basic"})

	result, err := cascade.Compute(cfgOf(entry("people/basic", "false")), graph)

	require.NoError(t, err)
	assert.Empty(t, runningIDs(result))
	assert.True(t, result.Empty())
}

// 依赖图里有、但 brickkit.yaml 里没写的组件（手工编辑过配置），
// 按"未钉住、未禁用"处理，可以被依赖拉起。
func TestComponentAbsentFromConfigCanStillBePulledIn(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
	)

	result, err := cascade.Compute(cfgOf(entry("erp/backend", "")), graph)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"erp/backend", "people/basic"}, runningIDs(result))
}

// IsRunning 是给注入引擎与 order 用的查询入口。
func TestIsRunningLookup(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
	)
	cfg := cfgOf(entry("erp/backend", "false"), entry("people/basic", ""))

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.False(t, result.IsRunning(ref("erp/backend")))
	assert.False(t, result.IsRunning(ref("people/basic")), "唯一的依赖方停了，它也不该跑")
	assert.False(t, result.IsRunning(resolver.Ref{ID: "nobody/here", Version: "1.0.0"}))
}

// ============================================================
// Restrict（`brickkit up --only`，004 §3.5）
// ============================================================

// 收窄之后，不在集合里的组件从"启动"变成"跳过"，并带上调用方给的理由。
func TestRestrictNarrowsRunningSet(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
	)
	result, err := cascade.Compute(cfgOf(entry("erp/backend", ""), entry("people/basic", "")), graph)
	require.NoError(t, err)
	require.ElementsMatch(t, refs("erp/backend", "people/basic"), result.Running())

	narrowed := result.Restrict(map[resolver.Ref]bool{ref("people/basic"): true}, "未被 --only 选中")

	assert.Equal(t, refs("people/basic"), narrowed.Running())
	assert.False(t, narrowed.IsRunning(ref("erp/backend")))
	assert.True(t, narrowed.IsRunning(ref("people/basic")))

	for _, c := range narrowed.Components {
		if c.Ref == ref("erp/backend") {
			assert.Equal(t, cascade.StateSkipped, c.State)
			assert.Equal(t, "未被 --only 选中", c.Reason)
		}
	}
}

// 收窄不会把已经"显式禁用"的组件改写成别的理由——
// 那条理由比"未被选中"更能说明问题。
func TestRestrictKeepsDisabledReason(t *testing.T) {
	graph := newGraph(t, spec{id: "people/basic"}, spec{id: "infra/bus"})
	result, err := cascade.Compute(
		cfgOf(entry("people/basic", ""), entry("infra/bus", "false")), graph)
	require.NoError(t, err)

	narrowed := result.Restrict(map[resolver.Ref]bool{ref("people/basic"): true}, "未被 --only 选中")

	for _, c := range narrowed.Components {
		if c.Ref == ref("infra/bus") {
			assert.Equal(t, cascade.StateDisabled, c.State)
			assert.Contains(t, c.Reason, "显式禁用")
		}
	}
}

// 收窄到空集合是合法的（--only 指了个本来就不会启动的组件）。
func TestRestrictToNothing(t *testing.T) {
	graph := newGraph(t, spec{id: "people/basic"})
	result, err := cascade.Compute(cfgOf(entry("people/basic", "")), graph)
	require.NoError(t, err)

	narrowed := result.Restrict(map[resolver.Ref]bool{}, "未被 --only 选中")

	assert.True(t, narrowed.Empty())
	assert.Len(t, narrowed.Components, 1, "组件本身仍要出现在清单里，只是不启动")
}
