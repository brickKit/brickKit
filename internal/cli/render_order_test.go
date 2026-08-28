// 本文件覆盖启动顺序与依赖图的渲染（开发计划 10.1–10.7）。
//
// 从前这些是 brickkit order 的测试；order 已删除（它逐字输出的就是
// up --dry-run 的中段），因此改由 up --dry-run 驱动同一批渲染函数。
package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/resolver"
)

// indexOf 返回子串在输出中的位置，用于断言先后顺序。
func indexOf(t *testing.T, out, sub string) int {
	t.Helper()
	i := strings.Index(out, sub)
	require.GreaterOrEqual(t, i, 0, "输出中应包含 %q：\n%s", sub, out)
	return i
}

// orderProject 建一个 add 过 ERP 那套组件的项目。
func orderProject(t *testing.T) *projectFixture {
	t.Helper()
	comps := []comp{
		{ID: "portal/user-frontend", Version: "1.0.0", Requires: []string{"erp/backend@1.0.0"}},
		{ID: "erp/backend", Version: "1.0.0",
			Requires: []string{"people/basic@1.0.0", "authorization/rbac@1.0.0"},
			Optional: []string{"infra/redis-event-bus@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		{ID: "authorization/rbac", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		{ID: "department/tree", Version: "1.0.0"},
		{ID: "infra/redis-event-bus", Version: "1.0.0"},
	}
	return addedProject(t, comps, "portal/user-frontend@1.0.0")
}

// ============================================================
// 10.3 输出格式
// ============================================================

// 10.3 输出包含编号、箭头与依赖图（004 §3.8）。
func TestDryRunOrderOutputFormat(t *testing.T) {
	f := orderProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	out := r.stdout
	assert.Contains(t, out, "📋 启动顺序（拓扑排序）：")
	// 编号 + 版本化服务名
	assert.Contains(t, out, "1. department-tree-1-0-0")
	assert.Contains(t, out, "无依赖")
	// 箭头 + 依赖编号
	assert.Regexp(t, `\d+\. people-basic-1-0-0\s+← 依赖 1`, out)
	assert.Contains(t, out, "portal-user-frontend-1-0-0")

	assert.Contains(t, out, "可独立启动：")
	// 报的是最长的那条**强依赖链**，用版本化服务名（多版本时不带版本会歧义）。
	//
	// 这个夹具正好说明旧那句话错在哪：六个组件，旧输出会说
	// "必须最后启动：portal/user-frontend（需等前 5 个组件就绪）"，
	// 而真实的关键路径只有 4 层——infra/redis-event-bus 是弱依赖，
	// 根本不在链上，它与整条链并行启动。
	assert.Contains(t, out,
		"最长依赖链（4 层）：department-tree-1-0-0 → people-basic-1-0-0 → "+
			"erp-backend-1-0-0 → portal-user-frontend-1-0-0")
	assert.Contains(t, out, "不在这条链上的组件与它并行启动")
	assert.Contains(t, out, "依赖图：")
	assert.Contains(t, out, "→")
}

// 10.1 顺序本身正确：被依赖的组件出现在依赖方之前。
func TestDryRunOrderPutsDependenciesFirst(t *testing.T) {
	f := orderProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.Less(t,
		indexOf(t, r.stdout, "department-tree-1-0-0"),
		indexOf(t, r.stdout, "people-basic-1-0-0"))
	assert.Less(t,
		indexOf(t, r.stdout, "erp-backend-1-0-0"),
		indexOf(t, r.stdout, "portal-user-frontend-1-0-0"))
}

// 10.2 弱依赖照常启动，但不约束启动顺序，且单独列出来告诉使用者它可以关。
func TestDryRunOrderListsOptionalDependencies(t *testing.T) {
	f := orderProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	// 它跟着上层一起启动（003 §4.3），只是不约束顺序——
	// 那一行"只被弱依赖引用"回答的是"哪些是可以关掉的"。
	assert.Contains(t, r.stdout, "infra/redis-event-bus@1.0.0")
	assert.Contains(t, startupSection(r.stdout), "infra-redis-event-bus-1-0-0",
		"弱依赖也要启动")
	assert.Contains(t, r.stdout, "只被弱依赖引用")
	assert.Contains(t, r.stdout, "enabled: false", "要给出关掉它的办法")
	assert.Contains(t, r.stdout, "（弱）", "依赖图里要标出弱依赖")
}

// 10.6 多版本各自独立出现在顺序里。
func TestDryRunOrderShowsEachVersionSeparately(t *testing.T) {
	comps := []comp{
		{ID: "erp/a", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "erp/b", Version: "1.0.0", Requires: []string{"people/basic@2.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "erp/a@1.0.0", "erp/b@1.0.0")

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.Contains(t, r.stdout, "people-basic-1-0-0")
	assert.Contains(t, r.stdout, "people-basic-2-0-0")
	assert.Contains(t, r.stdout, "4. ", "四个组件各占一行")
}

// ============================================================
// 10.7 空项目
// ============================================================

func TestDryRunOrderOnEmptyProject(t *testing.T) {
	f := newProjectFixture(t)

	r := runIn(t, f.Dir, "up", "--dry-run")
	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "当前项目没有组件")
	assert.Contains(t, r.stdout, "brickkit add")
	assert.NotContains(t, r.stdout, "启动顺序")
}

// ============================================================
// 10.4 循环依赖
// ============================================================

func TestDryRunOrderReportsCycle(t *testing.T) {
	comps := []comp{
		{ID: "a/one", Version: "1.0.0", Requires: []string{"b/two@1.0.0"}},
		{ID: "b/two", Version: "1.0.0", Requires: []string{"a/one@1.0.0"}},
	}
	dir := t.TempDir()
	sources := localSource(t, dir, comps...)
	f := newProjectFixtureAt(t, dir, sources...)
	// 直接写入配置：add 会在解析阶段就拒绝这套组件
	f.writeConfig(t, "components:\n  - id: a/one\n    version: 1.0.0\n  - id: b/two\n    version: 1.0.0\n")

	r := runIn(t, f.Dir, "up", "--dry-run")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "循环依赖")
	assert.Contains(t, r.stderr, "a/one@1.0.0")
}

// ============================================================
// 用法
// ============================================================

func TestDryRunOrderInUninitializedDir(t *testing.T) {
	r := runIn(t, t.TempDir(), "up", "--dry-run")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "❌")
}

// Manifest 缓存缺失且安装源不可用时，order 报错说明原因（而不是给出错误的顺序）。
func TestDryRunOrderWithUnavailableManifest(t *testing.T) {
	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")
	f.Sources = nil
	f.writeConfig(t, "components:\n  - id: people/basic\n    version: 1.0.0\n")
	require.NoError(t, os.RemoveAll(f.Layout.ManifestsDir()))

	r := runIn(t, f.Dir, "up", "--dry-run")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "❌")
}

// ============================================================
// 渲染细节（代码层）
// ============================================================

func TestDependencyNote(t *testing.T) {
	assert.Equal(t, "无依赖", dependencyNote(resolver.PlanStep{}))
	assert.Equal(t, "← 依赖 1", dependencyNote(resolver.PlanStep{RequirePositions: []int{1}}))
	assert.Equal(t, "← 依赖 1, 2, 3",
		dependencyNote(resolver.PlanStep{RequirePositions: []int{1, 2, 3}}))
}

func TestPad(t *testing.T) {
	assert.Equal(t, "abc  ", pad("abc", 5))
	assert.Equal(t, "abc", pad("abc", 3))
	assert.Equal(t, "abcdef", pad("abcdef", 3), "超长时不截断")
}

// 依赖图里没有任何依赖关系时不打印这一节。
func TestRenderDependencyGraphSkippedWhenNoEdges(t *testing.T) {
	var out bytes.Buffer
	opts := &Options{Stdout: &out}
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	plan := &resolver.Plan{Steps: []resolver.PlanStep{{Position: 1, Ref: ref, Service: "people-basic-1-0-0"}}}
	graph := &resolver.Graph{Nodes: []*resolver.Node{{Ref: ref}}}

	renderDependencyGraph(opts, plan, graph)
	assert.Empty(t, out.String())
}

// 排序结果里的组件在图里查不到时跳过，不 panic（防御性分支）。
func TestRenderDependencyGraphSkipsUnknownNode(t *testing.T) {
	var out bytes.Buffer
	opts := &Options{Stdout: &out}
	plan := &resolver.Plan{Steps: []resolver.PlanStep{{
		Position: 1,
		Ref:      resolver.Ref{ID: "ghost/one", Version: "1.0.0"},
		Service:  "ghost-one-1-0-0",
	}}}

	renderDependencyGraph(opts, plan, &resolver.Graph{})
	assert.Empty(t, out.String())
}

// 只有一个组件时不打印最长链：那时"链"这个词解释不了任何事。
func TestDryRunOrderSingleComponentOutput(t *testing.T) {
	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "1. people-basic-1-0-0")
	assert.Contains(t, r.stdout, "可独立启动：people-basic-1-0-0（无依赖）")
	assert.NotContains(t, r.stdout, "最长依赖链")
	assert.NotContains(t, r.stdout, "依赖图：")
}

// 弱依赖缺失时，order 也要把警告打出来，并在依赖图里标注"未安装"。
func TestDryRunOrderShowsMissingOptionalDependency(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Optional: []string{"infra/redis-event-bus@1.0.0"}},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "⚠️")
	assert.Contains(t, r.stdout, "（弱，未安装）")
	assert.NotContains(t, r.stdout, "只被弱依赖引用", "没装进图里的弱依赖不该出现在这一行")
}

// ⚠️ 现状锁定：order 目前**不做级联计算**，enabled: false 的组件也会出现在顺序里。
//
// 级联启停是 Step 11 的职责（延后清单 P17）。Step 11 实现后，本用例应改为
// 断言被禁用的组件不出现在启动顺序中——它失败正是提醒回来改这里。
// P17 回填：order 按级联结果过滤（003 §4.3）。
// 被显式禁用的组件不该出现在启动顺序里。
func TestDryRunOrderExcludesDisabledComponent(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "department/tree", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "department/tree@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: department/tree
    version: 1.0.0
    enabled: false
`)

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	startup := startupSection(r.stdout)
	assert.NotContains(t, startup, "department-tree-1-0-0", "被禁用的组件不该排进启动顺序")
	assert.Contains(t, startup, "people-basic-1-0-0")

	// 但要在状态一览里说清它为什么不跑
	assert.Contains(t, r.stdout, "📋 组件状态计算：")
	assert.Contains(t, r.stdout, "department/tree@1.0.0")
	assert.Contains(t, r.stdout, "显式禁用")
}

// 上层关掉之后，下层跟着不排进启动顺序，并说清是跟着谁走的。
func TestDryRunOrderExcludesComponentsWhoseParentsAreOff(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: erp/backend
    version: 1.0.0
    enabled: false
  - id: people/basic
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.Contains(t, r.stdout, "上层都不启动")
	assert.NotContains(t, startupSection(r.stdout), "people-basic-1-0-0",
		"唯一的上层停了，它也不该启动")
}

// 全部组件都不启动时，明确说清楚，而不是打印一张空表。
func TestDryRunOrderWithNothingRunning(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    enabled: false
`)

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "本次没有组件会启动")
}

// P14 回填：钉住的组件依赖了被禁用的组件 → 报错（004 §10.3）。
func TestDryRunOrderReportsDisabledStrongDependency(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"authorization/rbac@1.0.0"}},
		{ID: "authorization/rbac", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: erp/backend
    version: 1.0.0
    enabled: true
  - id: authorization/rbac
    version: 1.0.0
    enabled: false
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "强依赖 authorization/rbac 被禁用")
	assert.Contains(t, r.stderr, "erp/backend")
}

// startupSection 截取"启动顺序"那一段，避免与状态一览里的名字混淆。
func startupSection(out string) string {
	start := strings.Index(out, "📋 启动顺序")
	if start < 0 {
		return ""
	}
	return out[start:]
}
