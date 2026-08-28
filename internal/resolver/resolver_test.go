// 本文件是 Step 7「依赖解析引擎」的业务行为测试，逐项覆盖开发计划 7.1–7.15。
package resolver

import (
	"context"
	"fmt"
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

// 环上只要有一条**弱**依赖边就不是错误。
//
// 环之所以致命，是因为它让启动顺序无解；而启动顺序只由强依赖约束
// （Order 的拓扑排序压根不看 Optional）。所以带弱边的环不影响任何顺序。
//
// 这不是钻牛角尖：两个组件互相"有就用、没有就降级"（通知组件可选地调审计、
// 审计可选地调通知）是很现实的写法，早先会被一句"检测到循环依赖"整个拦下来。
func TestCycleThroughOptionalDependencyIsAllowed(t *testing.T) {
	f := newFixture(t,
		comp{ID: "a/one", Version: "1.0.0", Optional: []string{"b/two@1.0.0"}},
		comp{ID: "b/two", Version: "1.0.0", Requires: []string{"a/one@1.0.0"}},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"a/one", "1.0.0"})
	require.NoError(t, err)

	require.Len(t, g.Nodes, 2, "两个组件都要解析出来")
	assert.True(t, g.Has(Ref{"a/one", "1.0.0"}))
	assert.True(t, g.Has(Ref{"b/two", "1.0.0"}))

	// 顺序仍然排得出来：强边只有 b→a 一条
	plan, err := Order(g)
	require.NoError(t, err)
	require.Len(t, plan.Steps, 2)
	assert.Equal(t, "a/one", plan.Steps[0].Ref.ID, "b 强依赖 a，a 要先起")
}

// 两条边都是弱依赖的环同样合法。
func TestMutualOptionalCycleIsAllowed(t *testing.T) {
	f := newFixture(t,
		comp{ID: "infra/notifier", Version: "1.0.0", Optional: []string{"infra/audit@1.0.0"}},
		comp{ID: "infra/audit", Version: "1.0.0", Optional: []string{"infra/notifier@1.0.0"}},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"infra/notifier", "1.0.0"})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 2)

	plan, err := Order(g)
	require.NoError(t, err)
	assert.Len(t, plan.Steps, 2, "没有强边，两个都可以先起")
}

// 全是强依赖的环仍然报错——那才是真的解不开。
func TestStrongCycleIsStillRejected(t *testing.T) {
	f := newFixture(t,
		comp{ID: "a/one", Version: "1.0.0", Requires: []string{"b/two@1.0.0"}},
		comp{ID: "b/two", Version: "1.0.0", Requires: []string{"a/one@1.0.0"}},
	)

	_, err := f.Resolver.Resolve(context.Background(), Ref{"a/one", "1.0.0"})
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDependencyCycle, e.Code)
	assert.Contains(t, e.Format(), "optional: true",
		"要给出出路：其中一方改成弱依赖就不再是死结")
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

// 32.18 十层依赖链：确认没有隐藏的深度上限。
//
// 上面那条只到 4 层，证明了"递归能走通"；这条证明的是"走多深都不会断"。
// 两者不重复：深度上限这类限制往往不是有意加的，而是某个中间结构
// （固定长度的数组、递归改迭代时的栈）带进来的，
// 而它一旦存在，表现是**依赖链长到一定程度就莫名其妙解析失败**。
func TestTenLevelDependencyChain(t *testing.T) {
	const depth = 10

	comps := make([]comp, 0, depth)
	names := make([]string, 0, depth)
	for i := 0; i < depth; i++ {
		id := fmt.Sprintf("x/n%02d", i)
		names = append(names, id)
		c := comp{ID: id, Version: "1.0.0"}
		if i < depth-1 {
			c.Requires = []string{fmt.Sprintf("x/n%02d@1.0.0", i+1)}
		}
		comps = append(comps, c)
	}

	f := newFixture(t, comps...)
	g, err := f.Resolver.Resolve(context.Background(), Ref{"x/n00", "1.0.0"})
	require.NoError(t, err, "32.18：十层依赖链必须能解析")

	require.Len(t, g.Nodes, depth)
	for _, id := range names {
		assert.True(t, g.Has(Ref{id, "1.0.0"}), "32.18：%s 应被解析", id)
	}

	// 顺序仍要满足"依赖先于依赖方"——最深的排最前
	refs := g.Refs()
	require.Len(t, refs, depth)
	assert.Equal(t, "x/n09", refs[0].ID, "32.18：最深的那个要最先启动")
	assert.Equal(t, "x/n00", refs[depth-1].ID, "32.18：根组件最后启动")
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
// 002 §7.7 的五项检查在哪
// ============================================================
//
// 这里从前有 CheckUpgrade（外加 UpgradeReport / newDependencies）与 7.10–7.14
// 五条用例。**已删除**：那五项检查常规解析路径本来就全做了——Manifest 取不到
// 报错、强依赖缺失报错、弱依赖缺失警告、循环依赖报错，资源绑定由
// CheckResourceBindings 负责。CheckUpgrade 是同一套判断的第二份拷贝，
// 而且复制得不完整：它无条件阻断，既不认 --dry-run 的降级（004 §4.4），
// 也不过滤本次不启动的组件（006 §4.4），于是只有升级路径上会撞到两个 bug。
//
// 五项的用例改挂到真正在跑的那条路上，见 internal/cli/up_upgrade_test.go
// 的"002 §7.7 升级时的五项检查"一节：改版本号、up、看结果。
