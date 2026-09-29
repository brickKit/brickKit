package shell_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// 外壳的工作负载真正要连的依赖 = 它自己的依赖 + 它这次承载的成员的依赖，
// 去掉它承载的成员本身（进程内调用，不走网络）与它自己。
func TestDependenciesIncludeHostedMembers(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		comp("erp/a", "1.0.0", "erp/shell@1.0.0"),
		comp("erp/b", "1.0.0", "erp/shell@1.0.0"),
		comp("erp/ext", "1.0.0", ""),
		comp("erp/weak", "1.0.0", ""),
		comp("erp/own", "1.0.0", ""),
	}}
	a := dependsOn(simple("erp/a", "1.0.0", 8081), "erp/ext", "1.0.0")
	a.Dependencies.Components = append(a.Dependencies.Components, manifest.ComponentDep{ID: "erp/weak", Version: "1.0.0", Optional: true})
	b := dependsOn(simple("erp/b", "1.0.0", 8082), "erp/a", "1.0.0")
	sh := dependsOn(simple("erp/shell", "1.0.0", 8080), "erp/own", "1.0.0")
	manifests := map[string]*manifest.Manifest{
		"erp/shell@1.0.0": sh, "erp/a@1.0.0": a, "erp/b@1.0.0": b,
		"erp/ext@1.0.0": simple("erp/ext", "1.0.0", 8090), "erp/weak@1.0.0": simple("erp/weak", "1.0.0", 8091),
		"erp/own@1.0.0": simple("erp/own", "1.0.0", 8092),
	}
	_, err := resolveFixture(t, cfg, manifests) // 补上外壳的能力声明
	require.NoError(t, err)

	p := projectFrom(t, cfg)
	var roots []resolver.Ref
	for _, c := range cfg.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	graph, err := resolver.New(stubProvider(manifests)).Resolve(context.Background(), roots...)
	require.NoError(t, err)
	states, err := cascade.Compute(p, graph)
	require.NoError(t, err)

	ref := func(id string) resolver.Ref { return resolver.Ref{ID: id, Version: "1.0.0"} }
	requires, optional := shell.Dependencies(p, graph, states, ref("erp/shell"))
	assert.Equal(t, []resolver.Ref{ref("erp/ext"), ref("erp/own")}, requires,
		"erp/a 是同一外壳里的成员，不算依赖")
	assert.Equal(t, []resolver.Ref{ref("erp/weak")}, optional)

	requires, _ = shell.Dependencies(p, graph, states, ref("erp/ext"))
	assert.Empty(t, requires, "不是外壳：只有它自己的依赖")
}

// Workloads 是这次真正要起的工作负载组成的依赖图：外壳承载的成员并进外壳（外壳继承它们的依赖），
// 依赖成员的组件改成依赖外壳。启动顺序按它排，才与生成文件里的 depends_on 一致。
func TestWorkloadsMergeHostedMembersIntoTheirShell(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		comp("erp/a", "1.0.0", "erp/shell@1.0.0"),
		comp("erp/db", "1.0.0", ""),
		comp("erp/caller", "1.0.0", ""),
	}}
	manifests := map[string]*manifest.Manifest{
		"erp/shell@1.0.0":  simple("erp/shell", "1.0.0", 8080),
		"erp/a@1.0.0":      dependsOn(simple("erp/a", "1.0.0", 8081), "erp/db", "1.0.0"),
		"erp/db@1.0.0":     simple("erp/db", "1.0.0", 5432),
		"erp/caller@1.0.0": dependsOn(simple("erp/caller", "1.0.0", 8090), "erp/a", "1.0.0"),
	}
	_, err := resolveFixture(t, cfg, manifests)
	require.NoError(t, err)
	p := projectFrom(t, cfg)
	var roots []resolver.Ref
	for _, c := range cfg.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	graph, err := resolver.New(stubProvider(manifests)).Resolve(context.Background(), roots...)
	require.NoError(t, err)
	states, err := cascade.Compute(p, graph)
	require.NoError(t, err)

	ref := func(id string) resolver.Ref { return resolver.Ref{ID: id, Version: "1.0.0"} }
	work := shell.Workloads(p, graph, states)
	assert.False(t, work.Has(ref("erp/a")), "成员并进外壳")
	assert.Equal(t, []resolver.Ref{ref("erp/db")}, work.Node(ref("erp/shell")).Requires, "外壳继承成员的依赖")
	assert.Equal(t, []resolver.Ref{ref("erp/shell")}, work.Node(ref("erp/caller")).Requires, "依赖成员 = 依赖外壳")

	plan, err := resolver.Order(work)
	require.NoError(t, err)
	position := map[resolver.Ref]int{}
	for _, s := range plan.Steps {
		position[s.Ref] = s.Position
	}
	assert.Less(t, position[ref("erp/db")], position[ref("erp/shell")])
	assert.Less(t, position[ref("erp/shell")], position[ref("erp/caller")])
}

// skipWaitFor 按条目各算各的：两个成员都依赖 erp/x，只有一个写了跳过——另一个没接受这个代价，
// 外壳照样等；两个都写了才不等。顶层组件跳过外壳里的成员，它就不等那个成员（也就是不等外壳）。
func TestWaitForHonorsEachEntrysOwnSkip(t *testing.T) {
	build := func(skipA, skipB, skipC []string) (*resolver.Graph, func(string) []resolver.Ref) {
		a := comp("erp/a", "1.0.0", "erp/shell@1.0.0")
		a.SkipWaitFor = skipA
		b := comp("erp/b", "1.0.0", "erp/shell@1.0.0")
		b.SkipWaitFor = skipB
		c := comp("erp/c", "1.0.0", "")
		c.SkipWaitFor = skipC
		cfg := &testCfg{Components: []testComp{comp("erp/shell", "1.0.0", ""), a, b, comp("erp/x", "1.0.0", ""), c}}
		manifests := map[string]*manifest.Manifest{
			"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080),
			"erp/a@1.0.0":     dependsOn(simple("erp/a", "1.0.0", 8081), "erp/x", "1.0.0"),
			"erp/b@1.0.0":     dependsOn(simple("erp/b", "1.0.0", 8082), "erp/x", "1.0.0"),
			"erp/x@1.0.0":     simple("erp/x", "1.0.0", 8090),
			"erp/c@1.0.0":     dependsOn(simple("erp/c", "1.0.0", 8091), "erp/a", "1.0.0"),
		}
		_, err := resolveFixture(t, cfg, manifests)
		require.NoError(t, err)
		p := projectFrom(t, cfg)
		var roots []resolver.Ref
		for _, cc := range cfg.Components {
			roots = append(roots, resolver.Ref{ID: cc.ID, Version: cc.Version})
		}
		graph, err := resolver.New(stubProvider(manifests)).Resolve(context.Background(), roots...)
		require.NoError(t, err)
		states, err := cascade.Compute(p, graph)
		require.NoError(t, err)
		return shell.Workloads(p, graph, states), func(id string) []resolver.Ref {
			return shell.WaitFor(p, graph, states, resolver.Ref{ID: id, Version: "1.0.0"})
		}
	}
	x := resolver.Ref{ID: "erp/x", Version: "1.0.0"}
	shellRef := resolver.Ref{ID: "erp/shell", Version: "1.0.0"}

	_, waitFor := build([]string{"erp/x"}, nil, nil)
	assert.Equal(t, []resolver.Ref{x}, waitFor("erp/shell"), "erp/b 没写跳过：外壳照样等 erp/x")

	_, waitFor = build([]string{"erp/x"}, []string{"erp/x"}, nil)
	assert.Empty(t, waitFor("erp/shell"), "两个成员都写了：外壳不等 erp/x")

	work, _ := build(nil, nil, nil)
	assert.Equal(t, []resolver.Ref{shellRef}, work.Node(resolver.Ref{ID: "erp/c", Version: "1.0.0"}).Requires)
	work, _ = build(nil, nil, []string{"erp/a"})
	assert.Empty(t, work.Node(resolver.Ref{ID: "erp/c", Version: "1.0.0"}).Requires, "erp/c 跳过 erp/a：不等外壳")
}

// 外壳里两个成员都依赖夹在中间的 erp/x：给出的 skipWaitFor 出路要把两个成员都点到——
// 只写一个，环还在。
func TestMergeCycleSuggestsEveryMemberThatMustSkip(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		comp("erp/a", "1.0.0", "erp/shell@1.0.0"),
		comp("erp/b", "1.0.0", "erp/shell@1.0.0"),
		comp("erp/d", "1.0.0", "erp/shell@1.0.0"),
		comp("erp/x", "1.0.0", ""),
	}}
	manifests := map[string]*manifest.Manifest{
		"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080),
		"erp/a@1.0.0":     dependsOn(simple("erp/a", "1.0.0", 8081), "erp/x", "1.0.0"),
		"erp/b@1.0.0":     dependsOn(simple("erp/b", "1.0.0", 8082), "erp/x", "1.0.0"),
		"erp/d@1.0.0":     simple("erp/d", "1.0.0", 8083),
		"erp/x@1.0.0":     dependsOn(simple("erp/x", "1.0.0", 8090), "erp/d", "1.0.0"),
	}
	_, err := resolveFixture(t, cfg, manifests)
	require.NoError(t, err)
	p := projectFrom(t, cfg)
	var roots []resolver.Ref
	for _, c := range cfg.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	graph, err := resolver.New(stubProvider(manifests)).Resolve(context.Background(), roots...)
	require.NoError(t, err)
	states, err := cascade.Compute(p, graph)
	require.NoError(t, err)

	cycle := shell.MergeCycleError(p, graph, states, shell.Workloads(p, graph, states))
	require.Error(t, cycle)
	assert.Contains(t, clierr.As(cycle).Hints, i18n.T(msgid.ShellHintMergeCycleSkipWait, "erp/x", "erp/a@1.0.0, erp/b@1.0.0"))
}

// 环上只有一个成员要跳过时，出路说"给这一个条目写上"，不说"各写上"。
func TestMergeCycleSuggestsTheOneMemberThatMustSkip(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		comp("erp/a", "1.0.0", "erp/shell@1.0.0"),
		comp("erp/d", "1.0.0", "erp/shell@1.0.0"),
		comp("erp/x", "1.0.0", ""),
	}}
	manifests := map[string]*manifest.Manifest{
		"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080),
		"erp/a@1.0.0":     dependsOn(simple("erp/a", "1.0.0", 8081), "erp/x", "1.0.0"),
		"erp/d@1.0.0":     simple("erp/d", "1.0.0", 8083),
		"erp/x@1.0.0":     dependsOn(simple("erp/x", "1.0.0", 8090), "erp/d", "1.0.0"),
	}
	_, err := resolveFixture(t, cfg, manifests)
	require.NoError(t, err)
	p := projectFrom(t, cfg)
	var roots []resolver.Ref
	for _, c := range cfg.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	graph, err := resolver.New(stubProvider(manifests)).Resolve(context.Background(), roots...)
	require.NoError(t, err)
	states, err := cascade.Compute(p, graph)
	require.NoError(t, err)

	cycle := shell.MergeCycleError(p, graph, states, shell.Workloads(p, graph, states))
	require.Error(t, cycle)
	assert.Contains(t, clierr.As(cycle).Hints, i18n.T(msgid.ShellHintMergeCycleSkipWaitOne, "erp/x", "erp/a@1.0.0"))
}
