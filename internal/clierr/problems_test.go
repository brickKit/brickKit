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
		WithHint("参考 002 §2.2", "参考附录 B.1")
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
		"   文件：components/people/basic/component.yaml\n" +
		"   metadata.id：缺失（必填字段）\n" +
		"   deployment.type：必须是 container\n" +
		"   deployment.port：必须在 1~65535 之间（当前是 0）\n" +
		"   建议：\n" +
		"   1. 参考 002 §2.2\n" +
		"   2. 参考附录 B.1\n"
	assert.Equal(t, want, e.Format())
}

// 不设置 source 时不渲染来源行。
func TestProblemSetWithoutSource(t *testing.T) {
	p := NewProblemSet(CodeConfigInvalid, "错误：校验失败")
	p.Add("project", "缺失")

	assert.Equal(t, "❌ 错误：校验失败\n   project：缺失\n", As(p.Err()).Format())
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
	assert.Contains(t, As(p.Err()).Format(), "   建议：只有一条建议\n")
}
