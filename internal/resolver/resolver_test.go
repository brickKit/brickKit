// 本文件是 Step 7「依赖解析引擎」的业务行为测试，逐项覆盖开发计划 7.1–7.15。
package resolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// ============================================================
// 7.1 强依赖缺失
// ============================================================

// 7.1 强依赖缺失时报错阻断，并指出缺失的组件。
func TestStrongDependencyMissingBlocks(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"authorization/rbac@1.0.0"}},
	)

	_, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDependencyMissing, e.Code)
	out := e.Format()
	// 逐字对齐 004 §10.2 的强依赖缺失错误块
	assert.Contains(t, out, "强依赖缺失")
	assert.Contains(t, out, "erp/backend@1.0.0")
	assert.Contains(t, out, "authorization/rbac@1.0.0")
	assert.Contains(t, out, "所有安装源")
	assert.Contains(t, out, "检查安装源配置（brickkit.yaml → sources）")
}

// 传递依赖（第二层）缺失时，错误要指出**直接**依赖方，而不是根组件。
func TestTransitiveStrongDependencyMissingNamesDirectDependent(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
	)

	_, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "people/basic@1.0.0", "缺失依赖的直接依赖方是 people/basic")
	assert.Contains(t, out, "department/tree@1.0.0")
}

// 根组件本身取不到时，报的是"组件未找到"，不是"强依赖缺失"。
func TestRootComponentMissing(t *testing.T) {
	f := newFixture(t)

	_, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentNotFound, clierr.As(err).Code)
}

// ============================================================
// 7.2 / 7.6 弱依赖
// ============================================================

// 7.2 弱依赖缺失时警告但继续。
func TestOptionalDependencyMissingWarnsAndContinues(t *testing.T) {
	f := newFixture(t,
		comp{
			ID: "erp/backend", Version: "1.0.0",
			Optional: []string{"infra/redis-event-bus@1.0.0"},
		},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.NoError(t, err, "弱依赖缺失不得阻断")

	require.Len(t, g.Nodes, 1)
	node := g.Node(Ref{"erp/backend", "1.0.0"})
	require.NotNil(t, node)
	assert.Equal(t, []Ref{{"infra/redis-event-bus", "1.0.0"}}, node.MissingOptional)
	assert.Empty(t, node.Optional)

	require.Len(t, g.Warnings, 1)
	w := g.Warnings[0]
	assert.True(t, w.Warning, "必须渲染为 ⚠️ 且不影响退出码")
	assert.Equal(t, clierr.ExitOK, w.ExitCode())
	out := w.Format()
	// 004 §4.5 的弱依赖警告块
	assert.Contains(t, out, "infra/redis-event-bus@1.0.0")
	assert.Contains(t, out, "erp/backend@1.0.0")
	assert.Contains(t, out, "INFRA_REDIS_EVENT_BUS_ENDPOINT")
	assert.Contains(t, out, "不会被注入")
}

// 7.6 弱依赖 `optional: true` 正确识别：能取到时正常进入依赖图，且不产生警告。
func TestOptionalDependencyPresentIsResolved(t *testing.T) {
	f := newFixture(t,
		comp{
			ID: "erp/backend", Version: "1.0.0",
			Requires: []string{"people/basic@1.0.0"},
			Optional: []string{"infra/redis-event-bus@1.0.0"},
		},
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "infra/redis-event-bus", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.NoError(t, err)
	assert.Empty(t, g.Warnings)

	node := g.Node(Ref{"erp/backend", "1.0.0"})
	require.NotNil(t, node)
	assert.Equal(t, []Ref{{"people/basic", "1.0.0"}}, node.Requires)
	assert.Equal(t, []Ref{{"infra/redis-event-bus", "1.0.0"}}, node.Optional)
	assert.Empty(t, node.MissingOptional)
	assert.True(t, g.Has(Ref{"infra/redis-event-bus", "1.0.0"}))
}

// ============================================================
// 7.3 / 7.9 循环依赖
// ============================================================

// 7.3 循环依赖时报错，并指出循环路径。
func TestDependencyCycleIsReported(t *testing.T) {
	f := newFixture(t,
		comp{ID: "a/one", Version: "1.0.0", Requires: []string{"b/two@1.0.0"}},
		comp{ID: "b/two", Version: "1.0.0", Requires: []string{"c/three@1.0.0"}},
		comp{ID: "c/three", Version: "1.0.0", Requires: []string{"a/one@1.0.0"}},
	)

	_, err := f.Resolver.Resolve(context.Background(), Ref{"a/one", "1.0.0"})
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDependencyCycle, e.Code)
	out := e.Format()
	assert.Contains(t, out, "循环依赖")
	assert.Contains(t, out, "a/one@1.0.0 → b/two@1.0.0 → c/three@1.0.0 → a/one@1.0.0",
		"004 §4.3：要打印完整循环路径")
	assert.Contains(t, out, "Manifest")
}

// 弱依赖同样参与循环检测：环就是环，不因为"可选"而消失。
func TestCycleThroughOptionalDependency(t *testing.T) {
	f := newFixture(t,
		comp{ID: "a/one", Version: "1.0.0", Optional: []string{"b/two@1.0.0"}},
		comp{ID: "b/two", Version: "1.0.0", Requires: []string{"a/one@1.0.0"}},
	)

	_, err := f.Resolver.Resolve(context.Background(), Ref{"a/one", "1.0.0"})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeDependencyCycle, clierr.As(err).Code)
}

// 7.9 自依赖时报错。
//
// component.yaml 里写自依赖会被 Manifest 校验（002）直接拒掉，因此这里用构造好的
// Manifest 走解析器，确认解析器自身也防住了 A→A。
func TestSelfDependencyIsReported(t *testing.T) {
	p := newFakeProvider().add("a/one", "1.0.0", dep("a/one@1.0.0"))

	_, err := New(p).Resolve(context.Background(), Ref{"a/one", "1.0.0"})
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDependencyCycle, e.Code)
	assert.Contains(t, e.Format(), "a/one@1.0.0 → a/one@1.0.0")
}

// 组件依赖"自己的另一个版本"：Manifest 校验（002，见 D27）在更早的一层就拒绝了，
// 根本到不了解析器。这里锁定这条分层边界，避免以后有人误以为解析器该放行。
func TestDependingOnOwnOtherVersionIsRejectedByManifest(t *testing.T) {
	f := newFixture(t,
		comp{ID: "a/one", Version: "2.0.0", Requires: []string{"a/one@1.0.0"}},
		comp{ID: "a/one", Version: "1.0.0"},
	)

	_, err := f.Resolver.Resolve(context.Background(), Ref{"a/one", "2.0.0"})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
	assert.Contains(t, e.Format(), "组件不能依赖自己")
}

// 解析器本身按 ID + 版本去重：同一 ID 的不同版本是两个独立节点，不构成环。
func TestSameIDDifferentVersionsAreDistinctNodes(t *testing.T) {
	p := newFakeProvider().
		add("erp/a", "1.0.0", dep("people/basic@1.0.0")).
		add("erp/b", "1.0.0", dep("people/basic@2.0.0")).
		add("people/basic", "1.0.0").
		add("people/basic", "2.0.0")

	g, err := New(p).Resolve(context.Background(), Ref{"erp/a", "1.0.0"}, Ref{"erp/b", "1.0.0"})
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 4)
	assert.Equal(t, []string{"1.0.0", "2.0.0"}, g.Versions("people/basic"))
}

// ============================================================
// 7.4 / 7.8 去重
// ============================================================

// 7.4 多个组件依赖同一个组件时只解析一次。
func TestSharedDependencyResolvedOnce(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0", "authorization/rbac@1.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "authorization/rbac", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "department/tree", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.NoError(t, err)

	assert.Len(t, g.Nodes, 4, "去重后共 4 个组件")
	assert.Equal(t, 1, f.Provider.count("department/tree@1.0.0"), "department/tree 只应被获取一次")

	// 被依赖方记录了所有依赖它的组件，供卸载检查（002 §3.9）与错误提示复用
	dept := g.Node(Ref{"department/tree", "1.0.0"})
	require.NotNil(t, dept)
	assert.ElementsMatch(t, []Ref{
		{"people/basic", "1.0.0"},
		{"authorization/rbac", "1.0.0"},
	}, dept.Dependents)
}

// 7.8 菱形依赖 A→B、A→C、B→D、C→D：D 只解析一次。
func TestDiamondDependencyDeduplicates(t *testing.T) {
	f := newFixture(t,
		comp{ID: "x/a", Version: "1.0.0", Requires: []string{"x/b@1.0.0", "x/c@1.0.0"}},
		comp{ID: "x/b", Version: "1.0.0", Requires: []string{"x/d@1.0.0"}},
		comp{ID: "x/c", Version: "1.0.0", Requires: []string{"x/d@1.0.0"}},
		comp{ID: "x/d", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"x/a", "1.0.0"})
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 4)
	assert.Equal(t, 1, f.Provider.count("x/d@1.0.0"))
}

// 同一个组件作为多个根传入时也只解析一次。
func TestDuplicateRootsDeduplicate(t *testing.T) {
	f := newFixture(t, comp{ID: "people/basic", Version: "1.0.0"})

	g, err := f.Resolver.Resolve(context.Background(),
		Ref{"people/basic", "1.0.0"}, Ref{"people/basic", "1.0.0"})
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 1)
	assert.Equal(t, 1, f.Provider.count("people/basic@1.0.0"))
}

// ============================================================
// 7.5 多版本共存
// ============================================================

// 7.5 A 依赖 X@1.0.0、B 依赖 X@2.0.0：两个版本都进入依赖图（共存，不报错）。
func TestMultipleVersionsCoexist(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/a", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		comp{ID: "erp/b", Version: "1.0.0", Requires: []string{"people/basic@2.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(),
		Ref{"erp/a", "1.0.0"}, Ref{"erp/b", "1.0.0"})
	require.NoError(t, err, "多版本共存是默认行为，不报错不阻断")

	assert.Len(t, g.Nodes, 4)
	assert.True(t, g.Has(Ref{"people/basic", "1.0.0"}))
	assert.True(t, g.Has(Ref{"people/basic", "2.0.0"}))
	assert.Equal(t, []string{"1.0.0", "2.0.0"}, g.Versions("people/basic"),
		"同 ID 的多个版本可被查询出来，供 add 写入 brickkit.yaml（Step 9）")
}

// ============================================================
// 7.7 深层递归
// ============================================================

// 7.7 深层递归解析（A→B→C→D）。
func TestDeepRecursion(t *testing.T) {
	f := newFixture(t,
		comp{ID: "x/a", Version: "1.0.0", Requires: []string{"x/b@1.0.0"}},
		comp{ID: "x/b", Version: "1.0.0", Requires: []string{"x/c@1.0.0"}},
		comp{ID: "x/c", Version: "1.0.0", Requires: []string{"x/d@1.0.0"}},
		comp{ID: "x/d", Version: "1.0.0"},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"x/a", "1.0.0"})
	require.NoError(t, err)

	assert.Len(t, g.Nodes, 4)
	for _, id := range []string{"x/a", "x/b", "x/c", "x/d"} {
		assert.True(t, g.Has(Ref{id, "1.0.0"}), "%s 应被解析", id)
	}
	// 解析结果按"依赖先于依赖方"排列，Step 10 的拓扑排序与 up 的启动顺序据此推进
	assert.Equal(t, []Ref{
		{"x/d", "1.0.0"}, {"x/c", "1.0.0"}, {"x/b", "1.0.0"}, {"x/a", "1.0.0"},
	}, g.Refs())
}

// ============================================================
// 7.15 空依赖
// ============================================================

// 7.15 空依赖列表解析成功。
func TestEmptyDependencies(t *testing.T) {
	f := newFixture(t,
		comp{ID: "people/basic", Version: "1.0.0", EmptyDependencies: true},
		comp{ID: "department/tree", Version: "1.0.0"}, // 完全不写 dependencies
	)

	ctx := context.Background()
	for _, ref := range []Ref{{"people/basic", "1.0.0"}, {"department/tree", "1.0.0"}} {
		g, err := f.Resolver.Resolve(ctx, ref)
		require.NoError(t, err)
		require.Len(t, g.Nodes, 1)
		assert.Empty(t, g.Nodes[0].Requires)
		assert.Empty(t, g.Warnings)
	}
}

// 不传根组件时返回空图，不报错（供 up 处理空项目）。
func TestResolveWithoutRoots(t *testing.T) {
	f := newFixture(t)

	g, err := f.Resolver.Resolve(context.Background())
	require.NoError(t, err)
	assert.Empty(t, g.Nodes)
	assert.Empty(t, g.Warnings)
}

// 从 brickkit.yaml 的 components 列表整体解析（up / order 的入口）。
func TestResolveConfig(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0"},
	)
	f.Config.Components = []config.Component{
		{ID: "erp/backend", Version: "1.0.0"},
	}

	g, err := f.Resolver.ResolveConfig(context.Background(), f.Config)
	require.NoError(t, err)
	assert.Len(t, g.Nodes, 2)
	assert.Equal(t, []Ref{{"erp/backend", "1.0.0"}}, g.Roots)
}

// ============================================================
// 7.10–7.14 升级时的依赖兼容性检查（002 §7.7）
// ============================================================

// 7.10 升级时新版本 Manifest 不可获取 → 报错阻断。
func TestUpgradeNewVersionManifestUnavailable(t *testing.T) {
	f := newFixture(t, comp{ID: "people/basic", Version: "1.0.0"})
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}

	_, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeComponentNotFound, e.Code)
	assert.Contains(t, e.Format(), "people/basic@2.0.0")
}

// 7.11 升级时新版本的强依赖可满足 → 通过。
func TestUpgradeNewStrongDependencySatisfied(t *testing.T) {
	f := newFixture(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "department/tree", Version: "1.0.0"},
	)
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}

	report, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.NoError(t, err)
	assert.Empty(t, report.Warnings)
	assert.True(t, report.Graph.Has(Ref{"department/tree", "1.0.0"}))
	assert.Equal(t, []Ref{{"department/tree", "1.0.0"}}, report.NewDependencies,
		"新增的依赖要能列出来，供 up 提示使用者")
}

// 7.11 升级时新版本新增的强依赖无法获取 → 报错阻断。
func TestUpgradeNewStrongDependencyMissing(t *testing.T) {
	f := newFixture(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0", Requires: []string{"authorization/rbac@1.0.0"}},
	)
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}

	_, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDependencyMissing, e.Code)
	assert.Contains(t, e.Format(), "authorization/rbac@1.0.0")
}

// 7.12 升级时新版本的资源依赖已绑定 → 通过。
func TestUpgradeResourceDependencyBound(t *testing.T) {
	f := newFixture(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0", Resources: []string{"database:postgresql"}},
	)
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}
	f.Config.Resources = []config.Resource{{
		Kind: "database", Engine: "postgresql", ID: "postgres-main",
		Host: "localhost", Port: 5432,
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people"}},
	}}

	_, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.NoError(t, err)
}

// 7.12 新版本新增的资源依赖未声明 → 报错阻断。
func TestUpgradeResourceDependencyNotDeclared(t *testing.T) {
	f := newFixture(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0", Resources: []string{"cache:redis"}},
	)
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}

	_, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeResourceUnbound, e.Code)
	out := e.Format()
	assert.Contains(t, out, "people/basic@2.0.0")
	assert.Contains(t, out, "cache")
	assert.Contains(t, out, "redis")
	assert.Contains(t, out, "resources")
}

// 7.12 资源已声明但没绑定给该组件 → 同样报错。
func TestUpgradeResourceDeclaredButNotBound(t *testing.T) {
	f := newFixture(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0", Resources: []string{"database:postgresql"}},
	)
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}
	f.Config.Resources = []config.Resource{{
		Kind: "database", Engine: "postgresql", ID: "postgres-main",
		Host: "localhost", Port: 5432,
		Bindings: []config.Binding{{ComponentID: "department/tree", Database: "department"}},
	}}

	_, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeResourceUnbound, e.Code)
	assert.Contains(t, e.Format(), "未绑定")
}

// 7.13 升级时新版本的弱依赖不可获取 → 警告但继续。
func TestUpgradeOptionalDependencyMissingWarns(t *testing.T) {
	f := newFixture(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0", Optional: []string{"infra/redis-event-bus@1.0.0"}},
	)
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}

	report, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.NoError(t, err, "弱依赖缺失不阻断升级")
	require.Len(t, report.Warnings, 1)
	assert.Contains(t, report.Warnings[0].Format(), "infra/redis-event-bus@1.0.0")
}

// 7.14 升级时新版本引入循环依赖 → 报错阻断。
func TestUpgradeIntroducesCycle(t *testing.T) {
	f := newFixture(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0", Requires: []string{"department/tree@1.0.0"}},
		comp{ID: "department/tree", Version: "1.0.0", Requires: []string{"people/basic@2.0.0"}},
	)
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}

	_, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDependencyCycle, e.Code)
	assert.Contains(t, e.Format(), "people/basic@2.0.0 → department/tree@1.0.0 → people/basic@2.0.0")
}

// 升级到同一个版本（没变化）时不做无谓的报错。
func TestUpgradeToSameVersion(t *testing.T) {
	f := newFixture(t, comp{ID: "people/basic", Version: "1.0.0"})
	f.Config.Components = []config.Component{{ID: "people/basic", Version: "1.0.0"}}

	report, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "1.0.0"})
	require.NoError(t, err)
	assert.Empty(t, report.NewDependencies)
}

// 升级一个 brickkit.yaml 里根本没有的组件：属于用法错误。
func TestUpgradeUnknownComponent(t *testing.T) {
	f := newFixture(t, comp{ID: "people/basic", Version: "2.0.0"})

	_, err := f.Resolver.CheckUpgrade(context.Background(), f.Config, Ref{"people/basic", "2.0.0"})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeComponentNotFound, e.Code)
	assert.Contains(t, e.Format(), "brickkit.yaml")
}
