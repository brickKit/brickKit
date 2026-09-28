// 本文件是依赖解析的代码层单测：图查询、错误压缩、版本比较、资源匹配等内部行为。
package resolver

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// ============================================================
// Ref / Graph
// ============================================================

func TestRefString(t *testing.T) {
	assert.Equal(t, "people/basic@1.0.0", Ref{"people/basic", "1.0.0"}.String())
}

func TestGraphQueriesOnEmptyGraph(t *testing.T) {
	f := newFixture(t)
	g, err := f.Resolver.Resolve(context.Background())
	require.NoError(t, err)

	assert.Nil(t, g.Node(Ref{"people/basic", "1.0.0"}))
	assert.False(t, g.Has(Ref{"people/basic", "1.0.0"}))
	assert.Empty(t, g.Versions("people/basic"))
	assert.Empty(t, g.Refs())
}

// 版本查询按数字比较排序，"10.0.0" 不能排在 "2.0.0" 前面。
func TestGraphVersionsSortedNumerically(t *testing.T) {
	p := newFakeProvider().
		add("x/a", "1.0.0", dep("people/basic@10.0.0"), dep("people/basic@2.0.0")).
		add("people/basic", "10.0.0").
		add("people/basic", "2.0.0")

	g, err := New(p).Resolve(context.Background(), Ref{"x/a", "1.0.0"})
	require.NoError(t, err)
	assert.Equal(t, []string{"2.0.0", "10.0.0"}, g.Versions("people/basic"))
}

func TestResolveProjectWithNilProject(t *testing.T) {
	f := newFixture(t)

	g, err := f.Resolver.ResolveProject(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, g.Nodes)
}

// 同一个弱依赖被两个组件依赖且缺失时，每个受影响的组件各得一条警告。
func TestMissingOptionalWarnsPerDependent(t *testing.T) {
	f := newFixture(t,
		comp{ID: "erp/a", Version: "1.0.0", Optional: []string{"infra/redis-event-bus@1.0.0"}},
		comp{ID: "erp/b", Version: "1.0.0", Optional: []string{"infra/redis-event-bus@1.0.0"}},
	)

	g, err := f.Resolver.Resolve(context.Background(), Ref{"erp/a", "1.0.0"}, Ref{"erp/b", "1.0.0"})
	require.NoError(t, err)
	require.Len(t, g.Warnings, 2)
	assert.Contains(t, g.Warnings[0].Format(), "erp/a@1.0.0")
	assert.Contains(t, g.Warnings[1].Format(), "erp/b@1.0.0")
}

// ============================================================
// fetchError / reasonOf
// ============================================================

func TestFetchErrorWrapsCause(t *testing.T) {
	cause := errors.New("boom")
	fe := &fetchError{ref: Ref{"people/basic", "1.0.0"}, err: cause}

	assert.Equal(t, "people/basic@1.0.0: boom", fe.Error())
	assert.ErrorIs(t, fe, cause)
	assert.Equal(t, cause, unwrapFetch(fe))
	assert.Equal(t, cause, unwrapFetch(cause), "非 fetchError 原样返回")
}

func TestReasonOf(t *testing.T) {
	assert.Equal(t, "", reasonOf(nil))
	assert.Equal(t, "the Market is unreachable", reasonOf(
		clierr.New(clierr.CodeNetworkUnreachable, "Error: the Market is unreachable")))
	assert.Equal(t, "connection refused", reasonOf(
		clierr.New(clierr.CodeNetworkUnreachable, "Error: the Market is unreachable").
			WithDetail("Reason", "connection refused")))
	assert.Equal(t, "boom", reasonOf(errors.New("boom")))
}

// 强依赖因为"市场不可达"而取不到时，原因要如实透传，而不是一律说"未找到"。
func TestMissingDependencyKeepsUnderlyingReason(t *testing.T) {
	p := newFakeProvider().add("erp/backend", "1.0.0", dep("people/basic@1.0.0"))
	p.errs["people/basic@1.0.0"] = clierr.New(clierr.CodeNetworkUnreachable, "错误：市场不可达").
		WithDetail("原因", "连接被拒绝")

	_, err := New(p).Resolve(context.Background(), Ref{"erp/backend", "1.0.0"})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeDependencyMissing, e.Code)
	assert.Contains(t, e.Format(), "连接被拒绝")
}

// ============================================================
// 循环路径的裁剪
// ============================================================

// 环不从根开始时，只打印环本身，不把前置链条也算进去。
func TestCyclePathStartsAtRepeatedNode(t *testing.T) {
	p := newFakeProvider().
		add("x/root", "1.0.0", dep("x/a@1.0.0")).
		add("x/a", "1.0.0", dep("x/b@1.0.0")).
		add("x/b", "1.0.0", dep("x/a@1.0.0"))

	_, err := New(p).Resolve(context.Background(), Ref{"x/root", "1.0.0"})
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "x/a@1.0.0 → x/b@1.0.0 → x/a@1.0.0")
	assert.NotContains(t, out, "x/root@1.0.0 → x/a@1.0.0", "x/root 不在环上")
}

// ============================================================
// 资源绑定检查
// ============================================================

// ============================================================
// 版本比较
// ============================================================

func TestCompareVersions(t *testing.T) {
	assert.Equal(t, 0, manifest.CompareVersions("1.0.0", "1.0.0"))
	assert.Negative(t, manifest.CompareVersions("1.0.0", "1.0.1"))
	assert.Negative(t, manifest.CompareVersions("2.0.0", "10.0.0"), "数字比较，不是字符串比较")
	assert.Positive(t, manifest.CompareVersions("1.2.0", "1.1.9"))
	assert.Negative(t, manifest.CompareVersions("1.0", "1.0.0"), "段数少的排前面")
	// 版本号的合法性由 Manifest 校验保证；万一混进非数字，退化为字符串比较且不 panic
	assert.NotPanics(t, func() { manifest.CompareVersions("abc", "1.0.0") })
	assert.Positive(t, manifest.CompareVersions("abc", "1.0.0"))
}

func TestContainsRef(t *testing.T) {
	refs := []Ref{{"a/b", "1.0.0"}}
	assert.True(t, containsRef(refs, Ref{"a/b", "1.0.0"}))
	assert.False(t, containsRef(refs, Ref{"a/b", "2.0.0"}))
	assert.False(t, containsRef(nil, Ref{"a/b", "1.0.0"}))
}
