package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖开发计划 28.1（Swagger UI 展示）、28.2（gRPC 文档）、
// **28.3（弱依赖组件不可用时不崩溃）**——最后一条是这个组件存在的理由。

func quietLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// fakeComponent 是一个会返回 OpenAPI 的假组件。
func fakeComponent(t *testing.T, spec string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, spec)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// noDocsComponent 是一个在线、但两条发现路径都不提供的组件。
func noDocsComponent(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestService(t *testing.T, targets []Target) *service {
	t.Helper()

	cfg := config{ComponentID: "infra/api-docs", Version: "1.0.0", Targets: targets}
	return newService(NewDiscoverer(), cfg, quietLogger(), t.TempDir())
}

func fetchSources(t *testing.T, svc *service) []map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Sources []map[string]any `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%s", rec.Body.String())
	}
	return body.Sources
}

func sourceOf(t *testing.T, sources []map[string]any, componentID string) map[string]any {
	t.Helper()

	for _, s := range sources {
		if s["componentId"] == componentID {
			return s
		}
	}
	t.Fatalf("聚合结果里没有 %s：%v", componentID, sources)
	return nil
}

const sampleSpec = `{"openapi":"3.0.3","info":{"title":"people/basic","version":"1.0.0"},"paths":{}}`

// ============================================================
// 28.1 抓到并展示 OpenAPI
// ============================================================

func TestDiscoversOpenAPI(t *testing.T) {
	backend := fakeComponent(t, sampleSpec)
	svc := newTestService(t, []Target{{ComponentID: "people/basic", Endpoint: backend.URL}})

	source := sourceOf(t, fetchSources(t, svc), "people/basic")
	if source["status"] != statusOK {
		t.Fatalf("期望 %s，实际 %v（%v）", statusOK, source["status"], source["reason"])
	}
	if source["specUrl"] != "/api/v1/openapi/people/basic" {
		t.Errorf("要给出可直接加载的 specUrl，实际 %v", source["specUrl"])
	}
}

// TestOpenAPIIsProxiedThroughThisComponent 说明为什么要代理。
//
// 那些组件默认不暴露端口（008 §5.2），浏览器根本连不上；就算连得上也会撞跨域。
// 由本组件代理是唯一走得通的路。
func TestOpenAPIIsProxiedThroughThisComponent(t *testing.T) {
	backend := fakeComponent(t, sampleSpec)
	svc := newTestService(t, []Target{{ComponentID: "people/basic", Endpoint: backend.URL}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi/people/basic", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("代理出去的不是合法 JSON：%s", rec.Body.String())
	}
	if spec["openapi"] != "3.0.3" {
		t.Errorf("文档内容被改动了：%v", spec)
	}
}

// TestEndpointsAreNotLeaked：内部地址不能出现在响应里。
//
// 聚合状态是给浏览器看的，把 http://people-basic-1-0-0:8080 这种内部拓扑
// 发出去，等于把内网结构告诉了任何能打开这个页面的人。
func TestEndpointsAreNotLeaked(t *testing.T) {
	backend := fakeComponent(t, sampleSpec)
	svc := newTestService(t, []Target{{ComponentID: "people/basic", Endpoint: backend.URL}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), backend.URL) {
		t.Errorf("响应里泄露了组件的内部地址：%s", rec.Body.String())
	}
}

// ============================================================
// 28.3 弱依赖不可用时不崩溃
// ============================================================

// TestAbsentComponentIsNotAnError 是这个组件最核心的一条。
//
// 弱依赖缺席时平台**完全不注入**那个地址变量（003 §4.3、开发进度 D140）。
// 这是**正常状态**，不是故障——把它当成错误，等于要求使用者必须把七个组件
// 全装上才能看文档。
func TestAbsentComponentIsNotAnError(t *testing.T) {
	svc := newTestService(t, []Target{
		{ComponentID: "people/basic", Endpoint: ""},
		{ComponentID: "erp/backend", Endpoint: ""},
	})

	sources := fetchSources(t, svc)
	for _, id := range []string{"people/basic", "erp/backend"} {
		source := sourceOf(t, sources, id)
		if source["status"] != statusAbsent {
			t.Errorf("%s 应当是 %s，实际 %v", id, statusAbsent, source["status"])
		}
		if source["reason"] == "" {
			t.Errorf("%s 要说明为什么没有文档，不能让页面空着让人猜", id)
		}
	}
}

// TestNothingInstalledStillServes：一个组件都没装，页面也要打得开。
//
// 这不是容错做得好，而是这个组件的本来面目——文档入口不该因为
// 某个业务组件没装就打不开。
func TestNothingInstalledStillServes(t *testing.T) {
	svc := newTestService(t, []Target{
		{ComponentID: "people/basic", Endpoint: ""},
		{ComponentID: "erp/backend", Endpoint: ""},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("一个组件都没装时也该正常响应，实际 %d", rec.Code)
	}
}

// TestUnreachableComponentDoesNotAffectOthers 是 28.3 的实质。
//
// 一个组件挂了，其余的文档必须照常展示。做不到的话，七个组件里任意一个
// 抖动都会让整个文档中心变成白页——而它本身完全正常。
func TestUnreachableComponentDoesNotAffectOthers(t *testing.T) {
	good := fakeComponent(t, sampleSpec)

	// 造一个"地址在、但连不上"的目标：起一个 server 再立刻关掉
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	svc := newTestService(t, []Target{
		{ComponentID: "people/basic", Endpoint: good.URL},
		{ComponentID: "erp/backend", Endpoint: deadURL},
	})

	sources := fetchSources(t, svc)

	if got := sourceOf(t, sources, "people/basic")["status"]; got != statusOK {
		t.Errorf("正常的组件不该被挂掉的那个连累：%v", got)
	}
	if got := sourceOf(t, sources, "erp/backend")["status"]; got != statusUnreachable {
		t.Errorf("挂掉的组件应当标为 %s，实际 %v", statusUnreachable, got)
	}
}

// TestOnlineButNoDocsIsDistinguished：三种"没文档"要分清楚。
//
//	absent       组件没装      → 装上它
//	unreachable  组件挂了      → 去看那个组件
//	no-docs      组件在线没文档 → 让它提供 /openapi.json 或开 Reflection
//
// 混成一种的话，使用者看到空页面完全不知道该做什么。
func TestOnlineButNoDocsIsDistinguished(t *testing.T) {
	silent := noDocsComponent(t)
	svc := newTestService(t, []Target{{ComponentID: "auth/password-login", Endpoint: silent.URL}})

	source := sourceOf(t, fetchSources(t, svc), "auth/password-login")
	if source["status"] != statusNoDocs {
		t.Fatalf("期望 %s，实际 %v", statusNoDocs, source["status"])
	}
	reason, _ := source["reason"].(string)
	if !strings.Contains(reason, "openapi.json") || !strings.Contains(reason, "Reflection") {
		t.Errorf("要说清楚组件该怎么做才能出现在这里：%q", reason)
	}
}

// ============================================================
// 健康检查
// ============================================================

// TestHealthzDoesNotProbeComponents：健康检查不去探那六个组件。
//
// 它们全是弱依赖，全挂了这个页面也该打得开——而且那时候正是最需要看文档的时候。
// 去探的话，任意一个组件抖动都会让文档中心被杀掉重启。
func TestHealthzDoesNotProbeComponents(t *testing.T) {
	var probed bool
	watcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probed = true
		http.NotFound(w, nil)
	}))
	defer watcher.Close()

	svc := newTestService(t, []Target{{ComponentID: "people/basic", Endpoint: watcher.URL}})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	if probed {
		t.Error("healthz 去探了组件")
	}
}

// ============================================================
// 缓存
// ============================================================

// TestSourcesAreCached：不能每次刷新页面都去探一遍。
//
// 一个卡住的上游会让页面很慢，而组件的 API 文档几乎不会在几十秒内变。
func TestSourcesAreCached(t *testing.T) {
	var hits int
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			hits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, sampleSpec)
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	svc := newTestService(t, []Target{{ComponentID: "people/basic", Endpoint: backend.URL}})

	fetchSources(t, svc)
	afterFirst := hits
	fetchSources(t, svc)

	if hits != afterFirst {
		t.Errorf("第二次请求不该重新探测：%d → %d", afterFirst, hits)
	}
}

// TestCacheExpires：缓存要会过期，否则组件上线了也永远看不到。
func TestCacheExpires(t *testing.T) {
	var hits int
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			hits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, sampleSpec)
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	svc := newTestService(t, []Target{{ComponentID: "people/basic", Endpoint: backend.URL}})

	now := time.Now()
	svc.now = func() time.Time { return now }
	fetchSources(t, svc)
	afterFirst := hits

	now = now.Add(cacheTTL + time.Second)
	fetchSources(t, svc)

	if hits == afterFirst {
		t.Error("缓存过期后应当重新探测，否则新上线的组件永远看不到")
	}
}

// ============================================================
// 目标清单
// ============================================================

// TestConfigCoversEveryAggregatedComponent：清单与 Manifest 必须对得上。
//
// 漏一条的表现是"那个组件在页面上永远显示未安装"——因为平台根本不会
// 注入它的地址，而不是它真的没装。
func TestConfigCoversEveryAggregatedComponent(t *testing.T) {
	cfg, err := configFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}

	if len(cfg.Targets) != len(aggregated) {
		t.Fatalf("目标数量对不上：%d vs %d", len(cfg.Targets), len(aggregated))
	}
	for _, target := range cfg.Targets {
		if target.ComponentID == "" {
			t.Error("目标缺少组件 ID")
		}
	}
}

// TestNoDependencyIsRequired：这个组件没有任何必需配置。
//
// 依赖全是弱依赖，缺席是常态。把任何一个列成必需，就等于要求使用者
// 必须把七个组件全装上才能看文档。
func TestNoDependencyIsRequired(t *testing.T) {
	if _, err := configFromEnv(func(string) string { return "" }); err != nil {
		t.Fatalf("一个环境变量都没有时也该能启动：%v", err)
	}
}

func TestGRPCTargetStripsScheme(t *testing.T) {
	cases := map[string]string{
		"http://department-tree-1-0-0:8080":  "department-tree-1-0-0:8080",
		"https://department-tree-1-0-0:8080": "department-tree-1-0-0:8080",
		"department-tree-1-0-0:8080":         "department-tree-1-0-0:8080",
	}

	for input, want := range cases {
		if got := grpcTarget(input); got != want {
			t.Errorf("grpcTarget(%q) = %q，期望 %q", input, got, want)
		}
	}
}
