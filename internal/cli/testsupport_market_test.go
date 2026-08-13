// 本文件为 Step 19 的测试提供一个假市场。
//
// 它按真实市场（market-server，18-D 落地）的路径与信封形状应答，并把收到的
// 每一次调用记录下来——发布的正确性就是"按什么顺序、发了哪些请求"。
package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// marketCall 是假市场收到的一次调用。
type marketCall struct {
	Method string
	Path   string
	Query  string
	Token  string
	Body   []byte
}

// fakeMarket 是一个最小可用的市场服务端。
type fakeMarket struct {
	server *httptest.Server

	mu    sync.Mutex
	calls []marketCall

	// 可注入的行为
	username  string
	password  string
	token     string
	expiresAt time.Time
	// artifacts 是 draft 发布后市场返回的产物列表。
	artifacts []map[string]any
	// failLogin 为 true 时登录一律失败。
	failLogin bool
	// status 覆盖特定路径前缀的响应（用于构造错误路径）。
	overrides map[string]marketResponse
}

type marketResponse struct {
	status int
	body   string
}

func newFakeMarket(t *testing.T) *fakeMarket {
	t.Helper()

	m := &fakeMarket{
		username:  "zhangsan",
		password:  "correct-horse-battery",
		token:     "market-token-abc",
		expiresAt: time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second),
		overrides: map[string]marketResponse{},
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	return m
}

// url 是写进 brickkit.yaml 的市场地址。
func (m *fakeMarket) url() string { return m.server.URL + "/api/v1" }

func (m *fakeMarket) record(c marketCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, c)
}

// requests 返回收到的调用序列，形如 "POST /components/people/basic/versions"。
func (m *fakeMarket) requests() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]string, 0, len(m.calls))
	for _, c := range m.calls {
		out = append(out, c.Method+" "+strings.TrimPrefix(c.Path, "/api/v1"))
	}
	return out
}

// find 返回第一条匹配的调用。
func (m *fakeMarket) find(t *testing.T, method, pathSuffix string) marketCall {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.calls {
		if c.Method == method && strings.HasSuffix(c.Path, pathSuffix) {
			return c
		}
	}
	require.Failf(t, "假市场没有收到期望的调用", "%s ...%s，实际收到：%v", method, pathSuffix, m.calls)
	return marketCall{}
}

func (m *fakeMarket) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m.record(marketCall{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		Token: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
		Body:  body,
	})

	for prefix, resp := range m.overrides {
		if strings.Contains(r.URL.Path, prefix) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.status)
			_, _ = io.WriteString(w, resp.body)
			return
		}
	}

	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/auth/login"):
		m.handleLogin(w, body)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/versions"):
		writeOK(w, http.StatusCreated, map[string]any{"componentId": "x", "version": "1.0.0", "status": "draft"})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/artifacts"):
		writeOK(w, http.StatusOK, m.artifacts)
	case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/upload"):
		writeOK(w, http.StatusOK, map[string]any{"uploaded": true})
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/visibility"):
		writeOK(w, http.StatusOK, map[string]any{"visibility": "public"})
	case r.Method == http.MethodPut:
		writeOK(w, http.StatusOK, map[string]any{"status": "stable"})
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"success":false,"error":{"code":"NOT_FOUND","message":"接口不存在"}}`)
	}
}

func (m *fakeMarket) handleLogin(w http.ResponseWriter, body []byte) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	_ = json.Unmarshal(body, &req)

	if m.failLogin || req.Username != m.username || req.Password != m.password {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w,
			`{"success":false,"error":{"code":"UNAUTHORIZED","message":"用户名或密码错误"}}`)
		return
	}

	writeOK(w, http.StatusOK, map[string]any{
		"token":     m.token,
		"username":  req.Username,
		"expiresAt": m.expiresAt.Format(time.RFC3339),
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	})
}

func writeOK(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

// marketSourceFragment 返回可写进 brickkit.yaml 的市场安装源片段。
func marketSourceFragment(id, url, authToken string) string {
	fragment := "  - id: " + id + "\n    type: market\n    url: " + url + "\n"
	if authToken != "" {
		fragment += "    authToken: " + authToken + "\n"
	}
	return fragment
}
