package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, srv *server, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%s", rec.Body.String())
	}
	return rec.Code, body
}

// 002 §9.4：/healthz 不检查依赖组件——上游挂了，本组件的存活状态不变。
func TestHealthzDoesNotDependOnUpstream(t *testing.T) {
	srv := &server{componentID: "demo/caller", helloEndpoint: "http://不可达:8080"}

	code, body := get(t, srv, "/healthz")

	if code != http.StatusOK {
		t.Fatalf("上游不可达时 /healthz 仍应返回 200，实际 %d", code)
	}
	if body["status"] != "ok" {
		t.Fatalf("期望 status=ok，实际 %v", body["status"])
	}
}

// 002 §5.4：调用方从环境变量拿到依赖地址，直接发 HTTP 调用。
func TestCallUsesInjectedEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/hello" {
			t.Errorf("上游收到意外路径：%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"component":"demo/hello","version":"1.0.0","message":"你好"}`))
	}))
	defer upstream.Close()

	srv := &server{componentID: "demo/caller", version: "1.0.0", helloEndpoint: upstream.URL}

	code, body := get(t, srv, "/api/v1/call")

	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%v", code, body)
	}
	upstreamBody, ok := body["upstream"].(map[string]any)
	if !ok {
		t.Fatalf("期望回显上游响应：%v", body)
	}
	if upstreamBody["component"] != "demo/hello" {
		t.Fatalf("上游响应不对：%v", upstreamBody)
	}
}

// 强依赖地址没注入时要明确报错，而不是装作正常。
func TestCallWithoutEndpointFails(t *testing.T) {
	srv := &server{componentID: "demo/caller"}

	code, body := get(t, srv, "/api/v1/call")

	if code != http.StatusFailedDependency {
		t.Fatalf("期望 424，实际 %d", code)
	}
	if !strings.Contains(body["error"].(string), "DEMO_HELLO_ENDPOINT") {
		t.Fatalf("错误信息应点名缺失的环境变量：%v", body["error"])
	}
}

// 002 §3.4：弱依赖缺失时用安全方式读取并自行降级，不能崩。
func TestOptionalDependencyDegradesGracefully(t *testing.T) {
	srv := &server{componentID: "demo/caller"} // 没有 DEMO_BUS_ENDPOINT

	code, body := get(t, srv, "/api/v1/status")

	if code != http.StatusOK {
		t.Fatalf("弱依赖缺失不应影响本组件，实际 %d", code)
	}
	if body["eventBus"] != "degraded" {
		t.Fatalf("期望降级标记 degraded，实际 %v", body["eventBus"])
	}
}

func TestOptionalDependencyReportedWhenPresent(t *testing.T) {
	srv := &server{componentID: "demo/caller", busEndpoint: "http://demo-bus-1-0-0:8080"}

	_, body := get(t, srv, "/api/v1/status")

	if body["eventBus"] != "available" {
		t.Fatalf("期望 available，实际 %v", body["eventBus"])
	}
}

// /api/v1/env 回显平台注入的全部关键变量，供 CLI 的注入引擎验证。
func TestEnvEndpointEchoesInjectedVariables(t *testing.T) {
	t.Setenv("COMPONENT_ID", "demo/caller")
	t.Setenv("DEMO_HELLO_ENDPOINT", "http://demo-hello-1-0-0:8080")
	t.Setenv("DATABASE_HOST", "postgres-main")
	srv := newServerFromEnv()

	_, body := get(t, srv, "/api/v1/env")

	env, ok := body["env"].(map[string]any)
	if !ok {
		t.Fatalf("期望 env 是对象：%v", body)
	}
	for key, want := range map[string]string{
		"COMPONENT_ID":        "demo/caller",
		"DEMO_HELLO_ENDPOINT": "http://demo-hello-1-0-0:8080",
		"DATABASE_HOST":       "postgres-main",
	} {
		if env[key] != want {
			t.Fatalf("环境变量 %s 期望 %q，实际 %v", key, want, env[key])
		}
	}
	if _, ok := env["DEMO_BUS_ENDPOINT"]; !ok {
		t.Fatalf("弱依赖变量也要出现在回显里（值为空表示未注入）")
	}
}

// ============================================================
// migration（005：迁移命令失败必须以非 0 退出码告知平台）
// ============================================================

func TestMigrateSucceedsWithoutDatabase(t *testing.T) {
	t.Setenv("DATABASE_HOST", "")
	t.Setenv("MIGRATION_SHOULD_FAIL", "")

	if err := migrate(); err != nil {
		t.Fatalf("未配置数据库时迁移应跳过而不是失败：%v", err)
	}
}

// 平台要能验证"迁移失败会阻断主服务启动"，因此留一个可控的失败开关。
func TestMigrateFailsWhenSwitchSet(t *testing.T) {
	t.Setenv("MIGRATION_SHOULD_FAIL", "1")

	if err := migrate(); err == nil {
		t.Fatal("期望迁移失败")
	}
}

func TestMigrateFailsWhenDatabaseUnreachable(t *testing.T) {
	t.Setenv("MIGRATION_SHOULD_FAIL", "")
	t.Setenv("DATABASE_HOST", "127.0.0.1")
	t.Setenv("DATABASE_PORT", "1") // 不会有人监听

	if err := migrate(); err == nil {
		t.Fatal("数据库不可达时迁移应失败")
	}
}
