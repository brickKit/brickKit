package clierr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProblemSetEmptyReturnsNil(t *testing.T) {
	p := NewProblemSet(CodeConfigInvalid, "错误：校验失败")
	assert.Equal(t, 0, p.Len())
	assert.Empty(t, p.Items())
	assert.NoError(t, p.Err())
}

func TestProblemSetRendersAllProblems(t *testing.T) {
	p := NewProblemSet(CodeManifestInvalid, "错误：component.yaml 校验失败").
		WithSource("文件", "components/people/basic/component.yaml").
		WithHint("参考 component.yaml 字段说明", "参考完整字段表")
	p.Missing("metadata.id")
	p.Add("deployment.type", "必须是 container")
	p.Addf("deployment.port", "必须在 %d~%d 之间（当前是 %d）", 1, 65535, 0)

	require.Equal(t, 3, p.Len())

	err := p.Err()
	require.Error(t, err)

	e := As(err)
	assert.Equal(t, CodeManifestInvalid, e.Code)
	assert.Equal(t, ExitError, e.ExitCode())

	want := "❌ 错误：component.yaml 校验失败\n" +
		"   文件: components/people/basic/component.yaml\n" +
		"   metadata.id: missing (required field)\n" +
		"   deployment.type: 必须是 container\n" +
		"   deployment.port: 必须在 1~65535 之间（当前是 0）\n" +
		"   Suggestions:\n" +
		"   1. 参考 component.yaml 字段说明\n" +
		"   2. 参考完整字段表\n"
	assert.Equal(t, want, e.Format())
}

// 不设置 source 时不渲染来源行。
func TestProblemSetWithoutSource(t *testing.T) {
	p := NewProblemSet(CodeConfigInvalid, "错误：校验失败")
	p.Add("project", "缺失")

	assert.Equal(t, "❌ 错误：校验失败\n   project: 缺失\n", As(p.Err()).Format())
}

// 问题按加入顺序渲染（便于对照配置文件从上到下修改）。
func TestProblemSetPreservesOrder(t *testing.T) {
	p := NewProblemSet(CodeConfigInvalid, "m")
	for _, f := range []string{"a", "b", "c"} {
		p.Add(f, "r")
	}

	items := p.Items()
	require.Len(t, items, 3)
	assert.Equal(t, "a", items[0].Field)
	assert.Equal(t, "b", items[1].Field)
	assert.Equal(t, "c", items[2].Field)
}

func TestProblemSetSingleHintInline(t *testing.T) {
	p := NewProblemSet(CodeConfigInvalid, "m").WithHint("只有一条建议")
	p.Add("f", "r")
	assert.Contains(t, As(p.Err()).Format(), "   Suggestion: 只有一条建议\n")
}

// 市场把每一条问题原样映射成自己的 {field, reason}：渲染成明细行之后结构不能丢。
func TestProblemSetErrCarriesProblems(t *testing.T) {
	p := NewProblemSet(CodeManifestInvalid, "invalid").WithSource("File", "component.yaml")
	p.Add("metadata.id", "missing")
	p.Add("deployment.port", "out of range")

	e := As(p.Err())
	assert.Equal(t, []Problem{
		{Field: "metadata.id", Reason: "missing"},
		{Field: "deployment.port", Reason: "out of range"},
	}, e.Problems, "来源行不是问题，不进列表")
}
