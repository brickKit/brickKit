// Package market 是市场的写入侧客户端：登录与发布。
//
// 读取侧（安装组件时取 Manifest 与产物）在 internal/source —— 那里要处理
// 多安装源的优先级与回退，关注点不同。两边共用本包的响应信封解析。
package market

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
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

// IsVersionExists 判断这个错误是不是"该版本已存在"。
//
// publish 靠它决定要不要去看能不能续传（那个版本可能只是上次没发完的 draft）。
func IsVersionExists(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == CodeVersionExists
}

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
		return i18n.T(msgid.MarketStatusOnly, status)
	}
	return i18n.T(msgid.MarketStatusWithBody, status, text)
}

// AsCLIError 把市场错误翻译成面向使用者的 CLI 错误。
//
// 关键在于**不同的错误码要给不同的建议**：401 该去登录，而"组件被下架"
// 让人去登录只会白折腾——这正是 P18 记录的问题。
func AsCLIError(action string, apiErr *APIError) *clierr.Error {
	switch apiErr.Code {
	case CodeComponentBlocked:
		return clierr.New(clierr.CodeComponentBlocked, i18n.T(msgid.ErrorPrefix)+message(apiErr, i18n.T(msgid.MarketFallbackBlocked))).
			WithHint(
				i18n.T(msgid.MarketHintBlockedNoInstall),
				i18n.T(msgid.MarketHintBlockedPickAnother),
			)

	case CodeUnauthorized:
		return clierr.New(clierr.CodeAuthRequired, i18n.T(msgid.ErrorPrefix)+message(apiErr, i18n.T(msgid.MarketFallbackUnauthorized))).
			WithHint(i18n.T(msgid.MarketHintLogin), i18n.T(msgid.MarketHintSetAuthToken))

	case CodeForbidden:
		return clierr.New(clierr.CodeAuthFailed, i18n.T(msgid.ErrorPrefix)+message(apiErr, i18n.T(msgid.MarketFallbackForbidden))).
			WithHint(i18n.T(msgid.MarketHintCheckOwner), i18n.T(msgid.MarketHintPrivateNeedsGrant))

	case CodeVersionExists:
		return clierr.New(clierr.CodeConfigConflict, i18n.T(msgid.ErrorPrefix)+message(apiErr, i18n.T(msgid.MarketFallbackVersionExists))).
			WithHint(i18n.T(msgid.MarketHintVersionNotReusable))

	case CodeNotFound:
		return clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.ErrorPrefix)+message(apiErr, i18n.T(msgid.MarketFallbackNotFound))).
			WithHint(i18n.T(msgid.MarketHintCheckIDAndVersion))
	}

	err := clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.MarketActionFailed, action)).
		WithDetail(i18n.T(msgid.LabelReason), message(apiErr, i18n.T(msgid.MarketFallbackNoReason)))
	if apiErr.Status >= 500 {
		return err.WithHint(i18n.T(msgid.MarketHintServerFault))
	}
	return err.WithHint(i18n.T(msgid.MarketHintFixReasonAndRetry))
}

// WithDetails 把市场返回的 details 逐条挂到错误上。
//
// 保留变量冲突、Manifest 校验问题这类错误，真正有用的信息全在 details 里；
// 只显示一句 message 等于让人对着"校验失败"发呆。
func WithDetails(err *clierr.Error, apiErr *APIError) *clierr.Error {
	// 把原始的 APIError 挂上去：调用方要按**错误码**分流时（如 publish 撞上
	// VERSION_ALREADY_EXISTS 要去看能不能续传），只能靠它——翻译过的那层
	// 只剩面向使用者的文案，码丢了。
	err = err.WithCause(apiErr)
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
		return strings.Join(parts, i18n.T(msgid.MarketDescribeJoin))
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
	return clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.MarketUnreachable)).
		WithDetail(i18n.T(msgid.LabelAddress), endpoint).
		WithDetail(i18n.T(msgid.LabelReason), networkReason(cause)).
		WithHint(i18n.T(msgid.MarketHintCheckNetworkAndURL)).
		WithCause(cause)
}

// statusOK 判断是否是成功状态码。
func statusOK(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}
