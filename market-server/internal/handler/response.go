package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/brickkit/brickkit/market-server/internal/model"
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
	return model.Errorf(model.CodeInternal, "internal Market error")
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

// maxJSONBody 是 JSON 请求体的上限。注册是开放的：没有上限，谁都能让市场把任意大的
// 请求体读进内存。最大的正常请求是带着满额 BRICKKIT.md 的发布（manifest.MaxDocBytes），
// JSON 会把 <、>、& 转义成 6 个字节，所以留到它的几倍；产物文件走单独的上传端点，不经过这里。
const maxJSONBody = 8 << 20

// decodeBody 解析 JSON 请求体。
func decodeBody(r *http.Request, target any) error {
	// 写 nil 而不是 ResponseWriter：超限时不让 net/http 自己去关连接，由下面照常写出 400
	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxJSONBody)).Decode(target)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return model.Errorf(model.CodeInvalidRequest,
			"the request body is larger than "+strconv.Itoa(maxJSONBody>>20)+" MiB").
			WithDetail("limitBytes", maxJSONBody)
	case err != nil:
		return model.Errorf(model.CodeInvalidRequest, "the request body is not valid JSON").
			WithDetail("cause", err.Error())
	}
	return nil
}
