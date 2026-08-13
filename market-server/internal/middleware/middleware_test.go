// 本文件是 Step 18-D 中间件的测试。
package middleware_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/middleware"
)

// recorder 收集中间件写出的日志。
type recorder struct{ lines []string }

func (r *recorder) logf(format string, args ...any) {
	r.lines = append(r.lines, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// 单个请求 panic 不能把整个市场进程带走：别人的安装会一起失败。
func TestRecoverTurnsPanicIntoJSON500(t *testing.T) {
	logs := &recorder{}
	h := middleware.Recover(logs.logf)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("组件市场炸了")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/components", nil))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var envelope struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	assert.False(t, envelope.Success)
	assert.Equal(t, "INTERNAL", envelope.Error.Code)
	assert.NotContains(t, w.Body.String(), "组件市场炸了", "panic 内容只进日志，不进响应")

	require.Len(t, logs.lines, 1)
	assert.Contains(t, logs.lines[0], "组件市场炸了", "日志里要留下真正的原因")
	assert.Contains(t, logs.lines[0], "/api/v1/components")
}

// 正常请求不该被 Recover 影响。
func TestRecoverPassesThroughNormalResponses(t *testing.T) {
	logs := &recorder{}
	h := middleware.Recover(logs.logf)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "ok")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/components/a/b/versions", nil))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "ok", w.Body.String())
	assert.Empty(t, logs.lines)
}

func TestAccessLogRecordsMethodPathAndStatus(t *testing.T) {
	logs := &recorder{}
	h := middleware.AccessLog(logs.logf)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/components/nobody/here", nil))

	require.Len(t, logs.lines, 1)
	assert.Contains(t, logs.lines[0], "GET")
	assert.Contains(t, logs.lines[0], "/api/v1/components/nobody/here")
	assert.Contains(t, logs.lines[0], "404")
}

// 处理器没显式写状态码时，默认就是 200。
func TestAccessLogDefaultsToStatus200(t *testing.T) {
	logs := &recorder{}
	h := middleware.AccessLog(logs.logf)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{}")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	require.Len(t, logs.lines, 1)
	assert.Contains(t, logs.lines[0], "200")
}

// 凭据绝不进日志（008 §5）。
func TestAccessLogDoesNotLogAuthorizationHeaderOrQuery(t *testing.T) {
	logs := &recorder{}
	h := middleware.AccessLog(logs.logf)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/components?keyword=secret-project", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token")

	h.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, logs.lines, 1)
	assert.NotContains(t, logs.lines[0], "super-secret-token")
	assert.NotContains(t, logs.lines[0], "secret-project")
}
