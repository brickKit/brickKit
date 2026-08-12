// Package clierr 定义 BrickKit CLI 的统一错误类型与错误输出格式。
//
// 设计依据：004 §10 错误处理。
//
// 所有面向用户的错误都应该是 *clierr.Error，它包含四部分：
//
//	Code     机器可读的错误码，便于测试与文档交叉引用
//	Message  一句话错误标题（渲染为 "❌ <Message>"）
//	Details  有序的明细行（组件、镜像、退出码……）
//	Hints    建议（一条时单行，多条时自动编号）
//
// 渲染示例（004 §10.2）：
//
//	❌ 错误：强依赖缺失
//	   组件：erp/backend@1.0.0
//	   缺失依赖：authorization/rbac@1.0.0
//	   原因：该组件在所有安装源中均未找到
//	   建议：
//	   1. 检查安装源配置（brickkit.yaml → sources）
//	   2. 确认组件是否已发布到市场
//
// 约定：Message 由调用方给出完整文案（如 "错误：强依赖缺失"、"请指定项目名称"、
// "clone 失败：目录已存在"），clierr 只负责加 ❌ 前缀与缩进排版，不猜测措辞。
package clierr

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Code 是机器可读的错误码。分类依据 004 §10.1。
type Code string

// 错误码目录。新增错误场景时在此登记，便于测试与文档对照。
const (
	// 内部错误（未归类，通常是 bug）。
	CodeInternal Code = "INTERNAL"
	// 命令用法错误：参数缺失、参数非法、未知命令。
	CodeInvalidArgument Code = "INVALID_ARGUMENT"
	// 功能尚未实现（骨架阶段占位）。
	CodeNotImplemented Code = "NOT_IMPLEMENTED"

	// 配置错误（004 §10.1 配置错误）。
	CodeConfigInvalid  Code = "CONFIG_INVALID"
	CodeConfigConflict Code = "CONFIG_CONFLICT"
	CodeProjectExists  Code = "PROJECT_EXISTS"
	CodeProjectMissing Code = "PROJECT_MISSING"
	CodeBackupMissing  Code = "BACKUP_MISSING"

	// Manifest 与依赖错误。
	CodeManifestInvalid   Code = "MANIFEST_INVALID"
	CodeDependencyMissing Code = "DEPENDENCY_MISSING"
	CodeDependencyCycle   Code = "DEPENDENCY_CYCLE"
	CodeVersionAmbiguous  Code = "VERSION_AMBIGUOUS"
	CodeComponentDisabled Code = "COMPONENT_DISABLED"
	CodeComponentNotFound Code = "COMPONENT_NOT_FOUND"

	// 资源与端口。
	CodeResourceUnbound Code = "RESOURCE_UNBOUND"
	CodePortConflict    Code = "PORT_CONFLICT"

	// 迁移与引擎。
	CodeMigrationFailed Code = "MIGRATION_FAILED"
	CodeEngineFailed    Code = "ENGINE_FAILED"
	CodeEngineMissing   Code = "ENGINE_MISSING"

	// 网络、认证与镜像权限。
	CodeNetworkUnreachable Code = "NETWORK_UNREACHABLE"
	CodeAuthRequired       Code = "AUTH_REQUIRED"
	CodeAuthFailed         Code = "AUTH_FAILED"
	CodeTokenExpired       Code = "TOKEN_EXPIRED"
	CodeImageUnauthorized  Code = "IMAGE_UNAUTHORIZED"
	CodeSignatureInvalid   Code = "SIGNATURE_INVALID"

	// 源码工作区。
	CodeCloneFailed Code = "CLONE_FAILED"
)

// 退出码。004 未规定具体数值，此处约定：
// 用法错误 2，其余错误 1，警告不影响退出码（0，见开发计划 33.15）。
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Detail 是一行错误明细，渲染为 "   Key：Value"。
type Detail struct {
	Key   string
	Value string
}

// Error 是 CLI 的统一错误类型。
type Error struct {
	Code    Code
	Message string
	Details []Detail
	Hints   []string // 渲染在 "建议：" 之下
	Tips    []string // 渲染为 "💡 ..." 行
	Exit    int      // 0 表示使用默认退出码 ExitError
	Cause   error    // 底层错误，只进日志，不给用户看（004：错误信息不暴露内部实现细节）
	Warning bool     // true 表示这是警告（⚠️），不阻断、退出码 0
}

// New 创建一个错误。message 需要是完整的用户文案。
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Newf 与 New 相同，但支持格式化。
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Warn 创建一个警告（⚠️，退出码 0）。用于保留变量冲突等"警告但继续"场景。
func Warn(code Code, message string) *Error {
	return &Error{Code: code, Message: message, Warning: true, Exit: ExitOK}
}

// WithDetail 追加一行明细。
func (e *Error) WithDetail(key, value string) *Error {
	e.Details = append(e.Details, Detail{Key: key, Value: value})
	return e
}

// WithDetailf 追加一行明细（值支持格式化）。
func (e *Error) WithDetailf(key, format string, args ...any) *Error {
	return e.WithDetail(key, fmt.Sprintf(format, args...))
}

// WithHint 追加建议。
func (e *Error) WithHint(hints ...string) *Error {
	e.Hints = append(e.Hints, hints...)
	return e
}

// WithTip 追加 💡 提示行。
func (e *Error) WithTip(tips ...string) *Error {
	e.Tips = append(e.Tips, tips...)
	return e
}

// WithCause 记录底层错误（只进日志）。
func (e *Error) WithCause(err error) *Error {
	e.Cause = err
	return e
}

// WithExit 指定退出码。
func (e *Error) WithExit(code int) *Error {
	e.Exit = code
	return e
}

// Error 实现 error 接口，返回单行摘要（供日志与 %v 使用）。
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(string(e.Code))
	b.WriteString(": ")
	b.WriteString(e.Message)
	for _, d := range e.Details {
		b.WriteString("; ")
		b.WriteString(d.Key)
		b.WriteString("=")
		b.WriteString(d.Value)
	}
	return b.String()
}

// Unwrap 支持 errors.Is / errors.As 穿透到底层错误。
func (e *Error) Unwrap() error { return e.Cause }

// ExitCode 返回该错误对应的进程退出码。
func (e *Error) ExitCode() int {
	if e.Warning {
		return ExitOK
	}
	if e.Exit != 0 {
		return e.Exit
	}
	return ExitError
}

// Format 把错误渲染成用户可读的多行文本（不含结尾换行之外的额外空行）。
func (e *Error) Format() string {
	symbol := "❌"
	if e.Warning {
		symbol = "⚠️"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", symbol, e.Message)
	for _, d := range e.Details {
		fmt.Fprintf(&b, "   %s：%s\n", d.Key, d.Value)
	}
	switch len(e.Hints) {
	case 0:
	case 1:
		fmt.Fprintf(&b, "   建议：%s\n", e.Hints[0])
	default:
		b.WriteString("   建议：\n")
		for i, h := range e.Hints {
			fmt.Fprintf(&b, "   %d. %s\n", i+1, h)
		}
	}
	for _, t := range e.Tips {
		fmt.Fprintf(&b, "   💡 %s\n", t)
	}
	return b.String()
}

// As 把任意 error 转换为 *Error。非 *Error 会被包装为 CodeInternal，
// 保证输出格式统一，且不把内部错误细节当作标题暴露给用户之外的结构。
func As(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return New(CodeInternal, "错误："+err.Error()).WithCause(err)
}

// Render 把错误渲染到 w（通常是 stderr），返回建议的退出码。
func Render(w io.Writer, err error) int {
	e := As(err)
	if e == nil {
		return ExitOK
	}
	fmt.Fprint(w, e.Format())
	return e.ExitCode()
}

// NotImplemented 生成"该命令尚未实现"错误，用于骨架阶段的命令占位。
// step 是开发计划中实现该命令的 Step 编号。
func NotImplemented(command string, step int) *Error {
	return Newf(CodeNotImplemented, "错误：%s 尚未实现", command).
		WithDetail("实现计划", fmt.Sprintf("开发计划 Step %d", step)).
		WithHint("当前版本为开发中的骨架，可执行 brickkit version 查看版本信息")
}
