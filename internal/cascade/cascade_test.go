// 本文件是启停判定的业务行为测试。
//
// 规则来自 003 §4.3，只有一句：**配置里有它、又没写 enabled: false，就启动**。
// 强依赖与弱依赖在这件事上一视同仁。这里的每个用例都对应设计书里写死的一条判定。
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

// cfgOf 造一份 brickkit.yaml 配置。enabled 用 "" / "true" / "false" 表示三种写法
// （前两种行为相同——不写与写 true 都是启动，这正是要锁住的事）。
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

// cfg2 造一份"全都没写 enabled"的配置。
func cfg2(t *testing.T, ids ...string) *config.Config {
	t.Helper()
	entries := make([][2]string, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, entry(id, ""))
	}
	return cfgOf(entries...)
}

// ============================================================
// 003 §4.3 的完整示例
// ============================================================

// 设计书里那张表是这个算法的验收标准，逐行对齐。
//
// 注意 erp/backend 被关掉之后 portal/user-frontend **不会**被顺手关掉——
// 它强依赖 erp/backend，所以这是一次报错（见下面的 TestDisabledRequirement…）。
// 这里把 portal 也关掉，测的是"其余组件照常启动"。
func TestDisablingOneComponentDoesNotTouchTheRest(t *testing.T) {
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
		entry("portal/user-frontend", "false"),
	)

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	// 关掉的只有那两个；people/basic 照常跑——没人再需要它不是关掉它的理由，
	// 那种"倒推"正是删掉的级联在做的事
	assert.ElementsMatch(t,
		[]string{"authorization/rbac", "department/tree", "people/basic"},
		runningIDs(result))

	state, reason := reasonOf(t, result, "erp/backend")
	assert.Equal(t, cascade.StateDisabled, state)
	assert.Contains(t, reason, "显式禁用")
}

// ============================================================
// enabled 的基本行为
// ============================================================

// 没写 enabled 就是启动（003 §4.3）。
func TestComponentWithoutEnabledRuns(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
	)

	result, err := cascade.Compute(cfg2(t, "erp/backend", "people/basic"), graph)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"erp/backend", "people/basic"}, runningIDs(result))
}

// enabled: true 与不写完全等价——它不是"钉住"，只是把默认值写了出来。
func TestEnabledTrueIsTheSameAsOmitting(t *testing.T) {
	graph := newGraph(t, spec{id: "people/basic"}, spec{id: "infra/bus"})

	explicit, err := cascade.Compute(
		cfgOf(entry("people/basic", "true"), entry("infra/bus", "true")), graph)
	require.NoError(t, err)
	omitted, err := cascade.Compute(cfg2(t, "people/basic", "infra/bus"), graph)
	require.NoError(t, err)

	assert.Equal(t, omitted.Running(), explicit.Running())
}

// enabled: false 一定不启动，哪怕没有任何人依赖它。
func TestExplicitlyDisabledNeverRuns(t *testing.T) {
	graph := newGraph(t, spec{id: "people/basic"})

	result, err := cascade.Compute(cfgOf(entry("people/basic", "false")), graph)
	require.NoError(t, err)

	assert.Empty(t, runningIDs(result))
	state, _ := reasonOf(t, result, "people/basic")
	assert.Equal(t, cascade.StateDisabled, state)
}

// 没人依赖的组件照常启动：配置里写了它，那就是声明（012 §2.3）。
func TestComponentNobodyDependsOnStillRuns(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
		spec{id: "infra/redis-event-bus"},
	)
	cfg := cfg2(t, "erp/backend", "people/basic", "infra/redis-event-bus")

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{"erp/backend", "people/basic", "infra/redis-event-bus"}, runningIDs(result))
}

// **这条是这次改动的核心**：只被弱依赖引用的组件照常启动。
//
// 从前它默认不启动，于是 `brickkit add` 把它写进配置、`up` 却不起它——
// "配置里有它、却不启动，是正常现象"这种要专门写一节解释的现象。
// 而使用者装了组件却发现一半功能是哑的，没有任何一处告诉他为什么。
func TestWeaklyReferencedComponentRuns(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", optional: []string{"infra/redis-event-bus"}},
		spec{id: "infra/redis-event-bus"},
	)

	result, err := cascade.Compute(cfg2(t, "erp/backend", "infra/redis-event-bus"), graph)
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{"erp/backend", "infra/redis-event-bus"}, runningIDs(result))
}

// 弱依赖被关掉不影响依赖方——这正是弱依赖存在的意义（002 §3.4）。
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
// 强依赖被显式关闭 → 报错
// ============================================================

// 关掉一个被强依赖的组件是矛盾的意图，必须报错并点名依赖方。
//
// 从前的行为取决于依赖方有没有写 enabled: true：钉住的报错、没钉住的静默跳过。
// "报不报错看你写没写 true"本身就说不通，而静默跳过是平台替使用者
// 做了一次隐式决定。
func TestDisabledRequirementIsAnError(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"authorization/rbac"}},
		spec{id: "authorization/rbac"},
	)
	cfg := cfgOf(entry("erp/backend", ""), entry("authorization/rbac", "false"))

	_, err := cascade.Compute(cfg, graph)

	require.Error(t, err)
	e := clierr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, clierr.CodeComponentDisabled, e.Code)

	rendered := e.Format()
	assert.Contains(t, rendered, "authorization/rbac", "要点出是谁被关掉了")
	assert.Contains(t, rendered, "erp/backend", "也要点出是谁在依赖它")
	assert.Contains(t, rendered, "enabled: false", "要告诉使用者去哪儿改：%s", rendered)
}

// 一次报出全部冲突：关掉一个底层组件往往同时影响好几个依赖方。
func TestAllDisabledRequirementsAreReportedAtOnce(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"department/tree"}},
		spec{id: "people/basic", requires: []string{"department/tree"}},
		spec{id: "department/tree"},
	)
	cfg := cfgOf(
		entry("erp/backend", ""), entry("people/basic", ""), entry("department/tree", "false"))

	_, err := cascade.Compute(cfg, graph)

	require.Error(t, err)
	rendered := clierr.As(err).Format()
	assert.Contains(t, rendered, "erp/backend")
	assert.Contains(t, rendered, "people/basic",
		"两个依赖方都要出现，不能只报第一个：%s", rendered)
}

// 依赖方自己也关掉了，就不再是冲突——这是"整条链一起关"的正常操作。
func TestDisabledRequirementIsFineWhenTheDependentIsAlsoDisabled(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"authorization/rbac"}},
		spec{id: "authorization/rbac"},
	)
	cfg := cfgOf(entry("erp/backend", "false"), entry("authorization/rbac", "false"))

	result, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.Empty(t, runningIDs(result))
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

// 依赖图里有、但 brickkit.yaml 里没写的组件（手工编辑过配置）按"未禁用"处理。
func TestComponentAbsentFromConfigStillRuns(t *testing.T) {
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
	assert.True(t, result.IsRunning(ref("people/basic")),
		"依赖方关了不影响它——被关掉的只有写了 enabled: false 的那一个")
	assert.False(t, result.IsRunning(resolver.Ref{ID: "nobody/here", Version: "1.0.0"}))
}

// ============================================================
// Focus（`brickkit up --only`，004 §3.5）
// ============================================================

// 点名的组件与它的依赖启动，其余全部跳过。
func TestFocusStartsSelectedAndItsDependencies(t *testing.T) {
	graph := newGraph(t,
		spec{id: "portal/web", requires: []string{"erp/backend"}},
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
	)
	cfg := cfg2(t, "portal/web", "erp/backend", "people/basic")

	result, err := cascade.Focus(cfg, graph, refs("erp/backend"))
	require.NoError(t, err)

	assert.ElementsMatch(t, refs("erp/backend", "people/basic"), result.Running())
	assert.False(t, result.IsRunning(ref("portal/web")), "没点名的不启动，那正是 --only 的意思")
	for _, c := range result.Components {
		if c.Ref == ref("portal/web") {
			assert.Equal(t, cascade.StateSkipped, c.State)
			assert.Equal(t, "未被 --only 选中", c.Reason)
		}
	}
}

// 闭包强弱都算：点名一个组件不该得到一个异步功能是哑的它。
func TestFocusClosureIncludesWeakDependencies(t *testing.T) {
	graph := newGraph(t,
		spec{id: "portal/web"},
		spec{id: "erp/backend", optional: []string{"infra/bus"}},
		spec{id: "infra/bus"},
	)
	cfg := cfg2(t, "portal/web", "erp/backend", "infra/bus")

	result, err := cascade.Focus(cfg, graph, refs("erp/backend"))
	require.NoError(t, err)

	assert.ElementsMatch(t, refs("erp/backend", "infra/bus"), result.Running())
}

// 点名一个只被弱依赖引用的组件：单独把它跑起来是合法用法。
func TestFocusOnAWeaklyReferencedComponent(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", optional: []string{"infra/bus"}},
		spec{id: "infra/bus"},
	)
	cfg := cfg2(t, "erp/backend", "infra/bus")

	result, err := cascade.Focus(cfg, graph, refs("infra/bus"))
	require.NoError(t, err)

	assert.Equal(t, refs("infra/bus"), result.Running())
}

// 显式禁用的组件即使落在闭包里也不启动，理由保持"显式禁用"——
// enabled: false 是长期声明，--only 是这一次的意图，前者更强。
func TestFocusKeepsDisabledOut(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", optional: []string{"infra/bus"}},
		spec{id: "infra/bus"},
	)
	cfg := cfgOf(entry("erp/backend", ""), entry("infra/bus", "false"))

	result, err := cascade.Focus(cfg, graph, refs("erp/backend"))
	require.NoError(t, err)

	assert.Equal(t, refs("erp/backend"), result.Running())
	for _, c := range result.Components {
		if c.Ref == ref("infra/bus") {
			assert.Equal(t, cascade.StateDisabled, c.State)
			assert.Contains(t, c.Reason, "显式禁用")
		}
	}
}

// 点名的组件的**强依赖**被关掉时报错——与不带 --only 时走同一条路。
func TestFocusErrorsWhenARequirementIsDisabled(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
	)
	cfg := cfgOf(entry("erp/backend", ""), entry("people/basic", "false"))

	_, err := cascade.Focus(cfg, graph, refs("erp/backend"))

	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "people/basic", "要点出是谁被关掉了")
	assert.Contains(t, text, "erp/backend", "也要点出是谁在依赖它")
}

// 没点名的组件即使强依赖了被关掉的组件，也不该报错——它本来就不跑。
func TestFocusIgnoresConflictsOutsideTheSelection(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}},
		spec{id: "people/basic"},
		spec{id: "demo/hello"},
	)
	cfg := cfgOf(
		entry("erp/backend", ""), entry("people/basic", "false"), entry("demo/hello", ""))

	result, err := cascade.Focus(cfg, graph, refs("demo/hello"))
	require.NoError(t, err)

	assert.Equal(t, refs("demo/hello"), result.Running())
}

// only 为空时退化成 Compute：调用方不必自己分支。
func TestFocusWithoutSelectionFallsBackToCompute(t *testing.T) {
	graph := newGraph(t, spec{id: "people/basic"})
	cfg := cfg2(t, "people/basic")

	focused, err := cascade.Focus(cfg, graph, nil)
	require.NoError(t, err)
	normal, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	assert.Equal(t, normal.Running(), focused.Running())
}

// 点名一个没有依赖的组件：只起它，组件清单仍然完整。
func TestFocusOnASingleComponent(t *testing.T) {
	graph := newGraph(t, spec{id: "people/basic"}, spec{id: "infra/bus"})
	cfg := cfg2(t, "people/basic", "infra/bus")

	result, err := cascade.Focus(cfg, graph, refs("people/basic"))
	require.NoError(t, err)

	assert.Equal(t, refs("people/basic"), result.Running())
	assert.Len(t, result.Components, 2, "没被选中的组件仍要出现在清单里")
}

// DependencyClosure 是 sync --only 与 up --only 共用的那份算法（导出即为此）。
func TestDependencyClosureCoversBothKinds(t *testing.T) {
	graph := newGraph(t,
		spec{id: "erp/backend", requires: []string{"people/basic"}, optional: []string{"infra/bus"}},
		spec{id: "people/basic", requires: []string{"department/tree"}},
		spec{id: "department/tree"},
		spec{id: "infra/bus"},
		spec{id: "portal/web"},
	)

	keep := cascade.DependencyClosure(graph, refs("erp/backend"))

	for _, id := range []string{"erp/backend", "people/basic", "department/tree", "infra/bus"} {
		assert.True(t, keep[ref(id)], "%s 应当在闭包里", id)
	}
	assert.False(t, keep[ref("portal/web")], "无关组件不该进闭包")
}
