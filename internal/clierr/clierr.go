// Package clierr 定义 BrickKit CLI 的统一错误类型与错误输出格式。
//
// 一条错误由标题、明细、建议组成，渲染成使用者一眼能看懂的块。
//
// 所有面向用户的错误都应该是 *clierr.Error，它包含四部分：
//
//	Code     机器可读的错误码：对外契约，只增不改（CI 据此判断该重试还是该报警）
//	Message  一句话错误标题（渲染为 "❌ <Message>"）
//	Details  有序的明细行（组件、镜像、退出码……）
//	Hints    建议（一条时单行，多条时自动编号）
//
// 渲染示例：
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

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Code 是机器可读的错误码。
type Code string

// 错误码目录。新增错误场景时在此登记，并写进 docs/{en,zh}/06-architecture/09-error-codes.md
// ——tests/docfields 会拦住漏写；码一旦发布就不改名、不挪作他用、不删除。
const (
	// 内部错误（未归类，通常是 bug）。
	CodeInternal Code = "INTERNAL"
	// 命令用法错误：参数缺失、参数非法、未知命令。
	CodeInvalidArgument Code = "INVALID_ARGUMENT"
	// 功能尚未实现。目前没有任何命令会产生它；码已经发布过，按上面的规则保留。
	CodeNotImplemented Code = "NOT_IMPLEMENTED"

	// 配置错误。
	CodeConfigInvalid  Code = "CONFIG_INVALID"
	CodeConfigConflict Code = "CONFIG_CONFLICT"
	// CodeDeployInconsistent 是"部署文件（deploy.yaml / deploy.local.yaml / -f 指定的文件）
	// 与 brickkit.yaml 的组件集合对不上"：多了、少了都算。
	CodeDeployInconsistent Code = "DEPLOY_INCONSISTENT"
	CodeProjectExists      Code = "PROJECT_EXISTS"
	CodeProjectMissing     Code = "PROJECT_MISSING"

	// Manifest 与依赖错误。
	CodeManifestInvalid   Code = "MANIFEST_INVALID"
	CodeDependencyMissing Code = "DEPENDENCY_MISSING"
	CodeDependencyCycle   Code = "DEPENDENCY_CYCLE"
	CodeVersionAmbiguous  Code = "VERSION_AMBIGUOUS"
	CodeComponentDisabled Code = "COMPONENT_DISABLED"
	CodeComponentNotFound Code = "COMPONENT_NOT_FOUND"
	// CodeComponentBlocked 是"市场已下架该组件版本"。
	// 它与认证失败是两回事：去登录并不能让被下架的组件变回可安装。
	CodeComponentBlocked Code = "COMPONENT_BLOCKED"

	// 资源与端口。
	CodeResourceUnbound Code = "RESOURCE_UNBOUND"
	CodePortConflict    Code = "PORT_CONFLICT"

	// 迁移与引擎。
	CodeMigrationFailed Code = "MIGRATION_FAILED"
	// CodeMigrationSkipped 是"这次不由 CLI 代跑迁移"（如 mode: debug 的组件）。
	// 它只作警告使用，不阻断。
	CodeMigrationSkipped Code = "MIGRATION_SKIPPED"
	CodeEngineFailed     Code = "ENGINE_FAILED"
	CodeEngineMissing    Code = "ENGINE_MISSING"

	// 网络、认证与镜像权限。
	CodeNetworkUnreachable Code = "NETWORK_UNREACHABLE"
	CodeAuthRequired       Code = "AUTH_REQUIRED"
	CodeAuthFailed         Code = "AUTH_FAILED"
	CodeTokenExpired       Code = "TOKEN_EXPIRED"
	CodeImageUnauthorized  Code = "IMAGE_UNAUTHORIZED"
	// CodeImageMissing 是"这个镜像要在本机构建，还没有构建"（提案 §9.10：up 从不构建）。
	CodeImageMissing Code = "IMAGE_MISSING"
	// CodeImageStale 是"本机构建的外壳镜像编进的成员版本与 component.yaml 不一致"（附录 A24）：重建镜像。
	CodeImageStale Code = "IMAGE_STALE"
	// CodeImageUnverified 是"外壳镜像里编进的成员版本无法确认"（没有 brickkit build 的标签，附录 A24），只作警告。
	CodeImageUnverified  Code = "IMAGE_UNVERIFIED"
	CodeSignatureInvalid Code = "SIGNATURE_INVALID"

	// 源码工作区。
	CodeCloneFailed Code = "CLONE_FAILED"
	// CodeSubmoduleGuard 是"目标已登记为 git submodule，阻断 sync/remove 的
	// 直接文件系统操作"（一次外部实操反馈指出的）。os.Rename/os.RemoveAll
	// 不懂 .gitmodules，会把子模块的独立版本历史和 superproject 脱钩且不报错。
	CodeSubmoduleGuard Code = "SUBMODULE_GUARD"

	// 发布（brickkit release，提案 §10.2）。
	//
	// CodeReleaseBlocked 是"发布前的检查没过"（工作区不干净、有未推送的提交、没有上游、
	// tag 已存在）：什么都没写，改好再发。
	CodeReleaseBlocked Code = "RELEASE_BLOCKED"
	// CodeReleasePushFailed 是"tag 推不上去"：本地 tag 已回滚，远端的原因（网络、权限、
	// 服务端钩子）解决后可以原样重试。
	CodeReleasePushFailed Code = "RELEASE_PUSH_FAILED"

	// 结构检查（brickkit lint）。
	//
	// CodeLintFailed 是"lint 查出了问题"——逐条问题已经打印在 stdout，这个码只标记整条命令的结局。
	// 不复用 CONFIG_INVALID / MANIFEST_INVALID：一次 lint 可以两者兼有，汇总只能带一个码。
	// 所以 lint 里不合法的 brickkit.yaml 也以本码收尾，不是 CONFIG_INVALID；lint 自己有独立码的
	// 只有两种情形，都与文件内容无关：PROJECT_MISSING（无处可查）与 INVALID_ARGUMENT（命令行写错）。
	// CI 脚本据此认：本码 = "lint 跑了，并且查出了问题"。
	CodeLintFailed Code = "LINT_FAILED"
)

// 退出码约定：
// 用法错误 2，其余错误 1，警告不影响退出码（0）。
// 唯一的例外是 brickkit lint --strict：警告在那里也算失败，以 CodeLintFailed、退出码 1 收尾。
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
	// Problems 是 ProblemSet 收集的逐条问题，已经渲染在 Details 里；另存一份结构化的，
	// 给要按字段转交问题的调用方（市场把它们映射成自己响应里的 {field, reason}）。
	Problems []Problem
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
		// 日志要一行：多行的明细值（引擎的原始输出）用 " / " 接起来
		b.WriteString(strings.Join(valueLines(d.Value), " / "))
	}
	return b.String()
}

// valueLines 把一个明细值拆成行，去掉行尾空白与空行：空行在错误块里只会是一行孤零零的缩进。
func valueLines(v string) []string {
	var lines []string
	for _, line := range strings.Split(v, "\n") {
		if line = strings.TrimRight(line, " \t\r"); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
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

// MapText 返回一份副本，给人看的每段文字（标题、明细值、建议、提示）都经过 f。
// 命令层用它把路径改成按使用者所在目录的写法；原错误不变，日志里记的仍是它。
func (e *Error) MapText(f func(string) string) *Error {
	c := *e
	c.Message = f(e.Message)
	c.Details = make([]Detail, len(e.Details))
	for i, d := range e.Details {
		c.Details[i] = Detail{Key: d.Key, Value: f(d.Value)}
	}
	c.Hints = mapAll(e.Hints, f)
	c.Tips = mapAll(e.Tips, f)
	return &c
}

func mapAll(in []string, f func(string) string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = f(s)
	}
	return out
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
		// 多行的值（引擎的原始输出、Dockerfile 摘录、git 的原话）按行给出，续行比明细行多缩进三格：
		// 压成一行，摘录里的行号与 >>> 标记就读不出来了
		lines := valueLines(d.Value)
		first := ""
		if len(lines) > 0 {
			first = lines[0]
		}
		fmt.Fprintf(&b, "   %s\n", i18n.T(msgid.DetailLine, d.Key, first))
		for _, line := range lines[min(1, len(lines)):] {
			fmt.Fprintf(&b, "      %s\n", line)
		}
	}
	switch len(e.Hints) {
	case 0:
	case 1:
		fmt.Fprintf(&b, "   %s\n", i18n.T(msgid.HintLabelSingle, e.Hints[0]))
	default:
		b.WriteString("   " + i18n.T(msgid.HintLabelMulti) + "\n")
		for i, h := range e.Hints {
			fmt.Fprintf(&b, "   %d. %s\n", i+1, h)
		}
	}
	for _, t := range e.Tips {
		fmt.Fprintf(&b, "   💡 %s\n", t)
	}
	return b.String()
}

// Structured 报告 err 链上是否**本来就有**一个 *Error，有则返回它。
//
// 与 As 的区别在于它不合成：As 会把裸 error 包装成 CodeInternal，因此
// "是不是结构化错误"无法再从返回值上看出来。调用方若用 As 的结果去判断
// 这件事，就只能写成"Code != CodeInternal"——那会把我们自己抛出的、
// 货真价实的 CodeInternal（写文件失败等）一并当成裸错误处理。
func Structured(err error) (*Error, bool) {
	var e *Error
	if err != nil && errors.As(err, &e) {
		return e, true
	}
	return nil, false
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
	return New(CodeInternal, i18n.T(msgid.ErrorPrefix)+err.Error()).WithCause(err)
}

// Render 把错误渲染到 w（通常是 stderr），返回建议的退出码。
func Render(w io.Writer, err error) int {
	e := As(err)
	if e == nil {
		return ExitOK
	}
	_, _ = fmt.Fprint(w, e.Format())
	return e.ExitCode()
}
