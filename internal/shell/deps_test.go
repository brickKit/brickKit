package shell_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/manifest"
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
