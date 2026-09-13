package clierr

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 004 §10.2：错误块由 ❌ + 标题 + 明细行 + 建议组成。
func TestFormatFullBlock(t *testing.T) {
	err := New(CodeDependencyMissing, "错误：强依赖缺失").
		WithDetail("组件", "erp/backend@1.0.0").
		WithDetail("缺失依赖", "authorization/rbac@1.0.0").
		WithDetail("原因", "该组件在所有安装源中均未找到").
		WithHint("检查安装源配置（brickkit.yaml → sources）", "确认组件是否已发布到市场")

	got := err.Format()
	want := "❌ 错误：强依赖缺失\n" +
		"   组件：erp/backend@1.0.0\n" +
		"   缺失依赖：authorization/rbac@1.0.0\n" +
		"   原因：该组件在所有安装源中均未找到\n" +
		"   建议：\n" +
		"   1. 检查安装源配置（brickkit.yaml → sources）\n" +
		"   2. 确认组件是否已发布到市场\n"
	assert.Equal(t, want, got)
}

func TestFormatSingleHintIsInline(t *testing.T) {
	err := New(CodePortConflict, "错误：expose 端口冲突").
		WithHint("在 brickkit.yaml 中为其中一个组件添加 exposePort 字段")
	assert.Contains(t, err.Format(), "   建议：在 brickkit.yaml 中为其中一个组件添加 exposePort 字段\n")
	assert.NotContains(t, err.Format(), "   1. ")
}

func TestFormatTips(t *testing.T) {
	err := New(CodeImageUnauthorized, "错误：镜像拉取失败（未授权）").
		WithDetail("镜像", "registry.brickkit.io/people-basic:1.0.0").
		WithTip("请先登录镜像仓库：docker login registry.brickkit.io")
	assert.Contains(t, err.Format(), "   💡 请先登录镜像仓库：docker login registry.brickkit.io\n")
}

func TestFormatNoDetailsOrHints(t *testing.T) {
	assert.Equal(t, "❌ 请指定项目名称\n", New(CodeInvalidArgument, "请指定项目名称").Format())
}

// 开发计划 33.14 / 33.15：错误退出码非 0，警告退出码为 0。
func TestExitCodes(t *testing.T) {
	assert.Equal(t, ExitError, New(CodeInternal, "x").ExitCode())
	assert.Equal(t, ExitUsage, New(CodeInvalidArgument, "x").WithExit(ExitUsage).ExitCode())
	assert.Equal(t, ExitOK, Warn(CodeConfigConflict, "x").ExitCode())
}

// 004 §10.2 保留变量冲突是警告，用 ⚠️ 渲染。
func TestWarningRendersWithWarnSymbol(t *testing.T) {
	w := Warn(CodeConfigConflict, "配置冲突（警告，不阻断）：").
		WithDetail("配置项", "departmentTreeEndpoint").
		WithDetail("转换为环境变量", "DEPARTMENT_TREE_ENDPOINT")
	out := w.Format()
	assert.True(t, strings.HasPrefix(out, "⚠️"), "警告应以 ⚠️ 开头，实际：%q", out)
	assert.NotContains(t, out, "❌")
}

func TestErrorStringIsSingleLine(t *testing.T) {
	err := New(CodeDependencyCycle, "错误：循环依赖").WithDetail("路径", "a → b → a")
	assert.Equal(t, "DEPENDENCY_CYCLE: 错误：循环依赖; 路径=a → b → a", err.Error())
	assert.NotContains(t, err.Error(), "\n")
}

func TestUnwrapAndErrorsIs(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	err := New(CodeNetworkUnreachable, "错误：市场不可达").WithCause(cause)
	assert.ErrorIs(t, err, cause)

	var target *Error
	require.True(t, errors.As(error(err), &target))
	assert.Equal(t, CodeNetworkUnreachable, target.Code)
}

func TestAsWrapsPlainError(t *testing.T) {
	e := As(errors.New("boom"))
	require.NotNil(t, e)
	assert.Equal(t, CodeInternal, e.Code)
	assert.Contains(t, e.Message, "boom")

	assert.Nil(t, As(nil))
}

func TestAsKeepsCLIError(t *testing.T) {
	orig := New(CodeTokenExpired, "错误：Token 已过期")
	assert.Same(t, orig, As(error(orig)))
}

func TestRenderWritesAndReturnsExitCode(t *testing.T) {
	var buf bytes.Buffer
	code := Render(&buf, New(CodeAuthRequired, "错误：未登录市场").
		WithHint("执行 brickkit login 登录市场"))
	assert.Equal(t, ExitError, code)
	assert.Contains(t, buf.String(), "❌ 错误：未登录市场")
	assert.Contains(t, buf.String(), "建议：")

	buf.Reset()
	assert.Equal(t, ExitOK, Render(&buf, nil))
	assert.Empty(t, buf.String())
}

func TestNotImplementedCarriesStep(t *testing.T) {
	e := NotImplemented("brickkit up", 15)
	assert.Equal(t, CodeNotImplemented, e.Code)
	assert.Contains(t, e.Format(), "brickkit up 尚未实现")
	assert.Contains(t, e.Format(), "开发计划 Step 15")
	assert.Equal(t, ExitError, e.ExitCode())
}

func TestNewfAndWithDetailf(t *testing.T) {
	e := Newf(CodeVersionAmbiguous, "%s 存在多个版本", "people/basic").
		WithDetailf("版本列表", "%s", "1.0.0, 2.0.0")
	assert.Contains(t, e.Format(), "❌ people/basic 存在多个版本")
	assert.Contains(t, e.Format(), "   版本列表：1.0.0, 2.0.0")
}
