package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doJSON(t *testing.T, srv *server, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("响应不是合法 JSON：%s", rec.Body.String())
		}
	}
	return rec.Code, body
}

// 002 §9.1：/healthz 必须返回 200。
// 002 §9.4：只检查本进程存活，禁止检查数据库、依赖组件或任何外部系统。
func TestHealthz(t *testing.T) {
	srv := &server{componentID: "demo/hello", version: "1.0.0"}

	code, body := doJSON(t, srv, "/healthz")

	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", code)
	}
	if body["status"] != "ok" {
		t.Fatalf("期望 status=ok，实际 %v", body["status"])
	}
}

func TestHelloUsesInjectedConfig(t *testing.T) {
	srv := &server{componentID: "demo/hello", version: "2.0.0", greeting: "早上好"}

	code, body := doJSON(t, srv, "/api/v1/hello")

	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", code)
	}
	if body["component"] != "demo/hello" || body["version"] != "2.0.0" {
		t.Fatalf("组件标识不对：%v", body)
	}
	if body["greeting"] != "早上好" {
		t.Fatalf("配置项 greeting 未生效：%v", body["greeting"])
	}
}

// /api/v1/env 回显平台注入的环境变量，供 CLI 的注入引擎验证。
func TestEnvEndpointEchoesPlatformVariables(t *testing.T) {
	t.Setenv("COMPONENT_ID", "demo/hello")
	t.Setenv("COMPONENT_VERSION", "1.0.0")
	t.Setenv("GREETING", "你好")
	srv := newServerFromEnv()

	code, body := doJSON(t, srv, "/api/v1/env")

	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", code)
	}
	env, ok := body["env"].(map[string]any)
	if !ok {
		t.Fatalf("期望 env 是对象：%v", body)
	}
	if env["COMPONENT_ID"] != "demo/hello" || env["GREETING"] != "你好" {
		t.Fatalf("环境变量回显不完整：%v", env)
	}
}

// 002 §1.4：配置只从环境变量读取，不硬编码；缺省值要合理。
func TestNewServerFromEnvDefaults(t *testing.T) {
	t.Setenv("COMPONENT_ID", "")
	t.Setenv("COMPONENT_VERSION", "")
	t.Setenv("GREETING", "")

	srv := newServerFromEnv()

	if srv.componentID != "demo/hello" {
		t.Fatalf("默认组件 ID 不对：%s", srv.componentID)
	}
	if srv.greeting == "" {
		t.Fatalf("greeting 应有默认值")
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	srv := &server{}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("期望 404，实际 %d", rec.Code)
	}
}
