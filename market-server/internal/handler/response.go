package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/brickkit/market-server/internal/model"
)

// envelope 是市场的统一响应信封（007 §4.2）。
//
// 成功：{"success": true, "data": ...}
// 失败：{"success": false, "error": {"code": ..., "message": ..., "details": ...}}
//
// CLI 侧的 internal/source/market.go 就是按这个形状解析的（D47/D48），
// 改动信封等于改动客户端契约。
type envelope struct {
	Success bool            `json:"success"`
	Data    any             `json:"data,omitempty"`
	Error   *model.APIError `json:"error,omitempty"`
}

// writeJSON 写出一个成功响应。
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// 响应头已经发出去了，这里再出错也没法改状态码，只能放弃这次响应
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: data})
}

// writeError 把错误按错误码映射成状态码后写出。
func writeError(w http.ResponseWriter, err error) {
	apiErr := asAPIError(err)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusOf(apiErr))
	_ = json.NewEncoder(w).Encode(envelope{Success: false, Error: apiErr})
}

// asAPIError 把任意错误规整成 APIError。
//
// 非 APIError 说明是没被服务层包装的意外错误：对外只说"市场内部错误"，
// 具体原因留在服务端日志里（008 §5：错误信息不泄漏内部结构）。
func asAPIError(err error) *model.APIError {
	var apiErr *model.APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return model.Errorf(model.CodeInternal, "市场内部错误")
}

// statusOf 决定 HTTP 状态码。
//
// 状态码是对外契约的一部分：CLI 靠 404 判断"这个源没有该组件"从而继续
// 尝试下一个安装源（D40），靠 401/403 提示登录。映射错了会让整条安装链跑偏。
func statusOf(err *model.APIError) int {
	if err.Status != 0 {
		return err.Status
	}
	switch err.Code {
	case model.CodeUnauthorized:
		return http.StatusUnauthorized
	case model.CodeForbidden, model.CodeComponentBlocked:
		return http.StatusForbidden
	case model.CodeNotFound:
		return http.StatusNotFound
	case model.CodeConflict, model.CodeVersionExists:
		return http.StatusConflict
	case model.CodeInternal:
		return http.StatusInternalServerError
	default:
		// 校验类错误（MANIFEST_INVALID、保留变量冲突、闭源缺契约……）都是请求本身的问题
		return http.StatusBadRequest
	}
}

// decodeBody 解析 JSON 请求体。
func decodeBody(r *http.Request, target any) error {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		return model.Errorf(model.CodeInvalidRequest, "请求体不是合法的 JSON").
			WithDetail("cause", err.Error())
	}
	return nil
}
