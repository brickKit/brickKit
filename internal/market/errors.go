// Package market 是市场的写入侧客户端：登录与发布。
//
// 读取侧（安装组件时取 Manifest 与产物）在 internal/source —— 那里要处理
// 多安装源的优先级与回退，关注点不同。两边共用本包的响应信封解析。
package market

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
)

// APIError 是市场错误信封里的 error 对象（007 §9）。
//
//	{"success":false,"error":{"code":"...","message":"...","details":{...}}}
type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	// Status 是 HTTP 状态码。
	Status int `json:"-"`
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// 市场的错误码（与 market-server/internal/model 保持一致）。
const (
	CodeUnauthorized     = "UNAUTHORIZED"
	CodeForbidden        = "FORBIDDEN"
	CodeNotFound         = "NOT_FOUND"
	CodeComponentBlocked = "COMPONENT_BLOCKED"
	CodeVersionExists    = "VERSION_ALREADY_EXISTS"
)

// DecodeError 从响应里解出市场给的错误。
//
// 市场不一定总能返回信封（比如中间挡了一层网关），所以解不出来时
// 也要给一个能看的兜底描述，而不是把空的 APIError 交出去。
func DecodeError(status int, body []byte) *APIError {
	var envelope struct {
		Error *APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error != nil &&
		(envelope.Error.Code != "" || envelope.Error.Message != "") {
		envelope.Error.Status = status
		return envelope.Error
	}

	return &APIError{
		Status:  status,
		Message: fallbackMessage(status, body),
	}
}

// fallbackMessage 在没有信封时，尽量给出有用的信息。
func fallbackMessage(status int, body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	if text == "" {
		return fmt.Sprintf("市场返回状态码 %d", status)
	}
	return fmt.Sprintf("市场返回状态码 %d：%s", status, text)
}

// AsCLIError 把市场错误翻译成面向使用者的 CLI 错误。
//
// 关键在于**不同的错误码要给不同的建议**：401 该去登录，而"组件被下架"
// 让人去登录只会白折腾——这正是 P18 记录的问题。
func AsCLIError(action string, apiErr *APIError) *clierr.Error {
	switch apiErr.Code {
	case CodeComponentBlocked:
		return clierr.New(clierr.CodeComponentBlocked, "错误："+message(apiErr, "该组件版本已被市场下架")).
			WithHint(
				"该版本已被市场管理员下架，不能再安装",
				"请改用其他版本，或联系市场管理员了解原因",
			)

	case CodeUnauthorized:
		return clierr.New(clierr.CodeAuthRequired, "错误："+message(apiErr, "市场认证失败")).
			WithHint("执行 brickkit login 登录市场", "或在 brickkit.yaml 中配置 sources.authToken")

	case CodeForbidden:
		return clierr.New(clierr.CodeAuthFailed, "错误："+message(apiErr, "无权执行该操作")).
			WithHint("确认当前账号是否是该组件的所有者", "私有组件需要所有者授权后才能访问")

	case CodeVersionExists:
		return clierr.New(clierr.CodeConfigConflict, "错误："+message(apiErr, "该版本已存在")).
			WithHint("版本号一旦发布就不可重用，请改用新的版本号")

	case CodeNotFound:
		return clierr.New(clierr.CodeComponentNotFound, "错误："+message(apiErr, "市场上没有该资源")).
			WithHint("确认组件 ID 与版本号是否正确")
	}

	err := clierr.New(clierr.CodeNetworkUnreachable, "错误："+action+"失败").
		WithDetail("原因", message(apiErr, "市场未说明原因"))
	if apiErr.Status >= 500 {
		return err.WithHint("市场服务端异常，稍后重试或联系市场管理员")
	}
	return err.WithHint("请按上面的原因调整后重试")
}

// WithDetails 把市场返回的 details 逐条挂到错误上。
//
// 保留变量冲突、Manifest 校验问题这类错误，真正有用的信息全在 details 里；
// 只显示一句 message 等于让人对着"校验失败"发呆。
func WithDetails(err *clierr.Error, apiErr *APIError) *clierr.Error {
	for _, key := range sortedKeys(apiErr.Details) {
		if key == "cause" {
			// 服务端内部原因，对使用者没有意义
			continue
		}
		err = err.WithDetail(key, describe(apiErr.Details[key]))
	}
	return err
}

func message(apiErr *APIError, fallback string) string {
	if strings.TrimSpace(apiErr.Message) != "" {
		return apiErr.Message
	}
	return fallback
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// describe 把 details 里的值渲染成一行人能读的文字。
func describe(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, describe(item))
		}
		return strings.Join(parts, "；")
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, key := range sortedKeys(v) {
			parts = append(parts, key+"="+describe(v[key]))
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(value)
	}
}

// unreachable 构造"市场不可达"错误。
func unreachable(endpoint string, cause error) *clierr.Error {
	return clierr.New(clierr.CodeNetworkUnreachable, "错误：市场不可达").
		WithDetail("地址", endpoint).
		WithDetail("原因", networkReason(cause)).
		WithHint("检查网络连接与市场地址是否正确").
		WithCause(cause)
}

// statusOK 判断是否是成功状态码。
func statusOK(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}
