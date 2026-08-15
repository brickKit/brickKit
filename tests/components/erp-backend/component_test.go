package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖 002 §1.4（配置只从环境变量读）、§11（JSON 日志）与
// "组件对平台的承诺"（Manifest 里的依赖声明与实现一致）。

func envOf(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func completeEnv() map[string]string {
	return map[string]string{
		"AUTH_PASSWORD_LOGIN_ENDPOINT": "http://auth-password-login-1-0-0:8080",
		"AUTHORIZATION_RBAC_ENDPOINT":  "http://authorization-rbac-1-0-0:8080",
		"PEOPLE_BASIC_GRPC_ENDPOINT":   "http://people-basic-1-0-0:9090",
	}
}

func TestConfigFromEnv(t *testing.T) {
	cfg, err := configFromEnv(envOf(completeEnv()))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}

	if cfg.SessionTTL != defaultSessionTTL {
		t.Errorf("会话时长缺省应为 %v，实际 %v", defaultSessionTTL, cfg.SessionTTL)
	}
	if cfg.EventBusEndpoint != "" {
		t.Errorf("没注入弱依赖地址时应当为空，实际 %q", cfg.EventBusEndpoint)
	}
}

// TestWeakDependencyIsNotRequired 是这个组件最要紧的一条配置规则。
//
// 弱依赖缺席时，平台**完全不注入** INFRA_REDIS_EVENT_BUS_ENDPOINT
// （003 §4.3、开发进度 D140）。若把它列进"缺少必需配置"的校验里，
// 一个从没装过事件总线的项目就永远启动不了这个组件——
// 弱依赖就此变成了事实上的强依赖。
func TestWeakDependencyIsNotRequired(t *testing.T) {
	_, err := configFromEnv(envOf(completeEnv()))
	if err != nil {
		t.Fatalf("没有弱依赖地址也该能启动：%v", err)
	}

	// 反过来：三个强依赖缺任何一个都必须失败
	for key := range completeEnv() {
		t.Run("缺 "+key, func(t *testing.T) {
			env := completeEnv()
			delete(env, key)
			if _, err := configFromEnv(envOf(env)); err == nil {
				t.Fatalf("缺少强依赖地址 %s 必须启动失败", key)
			}
		})
	}
}

// TestRequiredConfigReportedAtOnce：缺哪些一次说完，并说明谁负责注入。
func TestRequiredConfigReportedAtOnce(t *testing.T) {
	_, err := configFromEnv(envOf(map[string]string{}))
	if err == nil {
		t.Fatal("什么都没配还能启动？")
	}

	for name := range completeEnv() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("错误信息里没提到缺少 %s：%v", name, err)
		}
	}
	if !strings.Contains(err.Error(), "extraPorts") {
		t.Error("要说明 PEOPLE_BASIC_GRPC_ENDPOINT 来自 people/basic 的 extraPorts 声明")
	}
	if strings.Contains(err.Error(), "EVENT_BUS") {
		t.Errorf("弱依赖不该出现在必需配置里：%v", err)
	}
}

// TestSessionTTLOverride 是 25.5 在配置层的验证。
func TestSessionTTLOverride(t *testing.T) {
	env := completeEnv()
	env["SESSION_TTL_SECONDS"] = "7200"

	cfg, err := configFromEnv(envOf(env))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}
	if cfg.SessionTTL != 2*time.Hour {
		t.Errorf("SESSION_TTL_SECONDS=7200 应当得到 2 小时，实际 %v", cfg.SessionTTL)
	}
}

func TestConfigRejectsBadSessionTTL(t *testing.T) {
	for _, raw := range []string{"abc", "0", "-60"} {
		env := completeEnv()
		env["SESSION_TTL_SECONDS"] = raw

		if _, err := configFromEnv(envOf(env)); err == nil {
			t.Errorf("SESSION_TTL_SECONDS=%q 必须被拒绝", raw)
		}
	}
}

// TestConfigStringSaysWhenEventBusIsAbsent：弱依赖缺席时摘要要说出来。
//
// 否则"为什么没有事件"这个问题会查很久——而答案只是没装事件总线。
func TestConfigStringSaysWhenEventBusIsAbsent(t *testing.T) {
	cfg, _ := configFromEnv(envOf(completeEnv()))

	if !strings.Contains(cfg.String(), "弱依赖缺席") {
		t.Errorf("摘要要说明事件总线没启用：%s", cfg.String())
	}
}

// ============================================================
// grpcTarget：一个很容易漏的转换
// ============================================================

// TestGRPCTargetStripsScheme 挡一类症状与原因完全不搭的错。
//
// 平台注入的是带 scheme 的 URL，而 gRPC 要的是 host:port。忘了转换时，
// 报出来的是 "dns resolver: missing address" 之类跟业务毫无关系的话。
func TestGRPCTargetStripsScheme(t *testing.T) {
	cases := map[string]string{
		"http://people-basic-1-0-0:9090":  "people-basic-1-0-0:9090",
		"https://people-basic-1-0-0:9090": "people-basic-1-0-0:9090",
		"http://people-basic-1-0-0:9090/": "people-basic-1-0-0:9090",
		// 已经是 host:port 的原样返回
		"people-basic-1-0-0:9090": "people-basic-1-0-0:9090",
		"people-basic:9090/":      "people-basic:9090",
	}

	for input, want := range cases {
		if got := grpcTarget(input); got != want {
			t.Errorf("grpcTarget(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// ============================================================
// 日志
// ============================================================

func TestLoggerOutputsJSONWithComponentID(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "info", "erp/backend").Info("组件已就绪")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("日志不是 JSON：%s", buf.String())
	}
	if entry["componentId"] != "erp/backend" {
		t.Errorf("每条日志都要带 componentId：%v", entry)
	}
}

func TestLoggerRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "info", "erp/backend").Info("调用失败",
		"token", "eyJhbGciOi...",
		"password", "demo-password",
		"personId", "p-001", // 不是秘密，要留着
	)

	out := buf.String()
	for _, secret := range []string{"eyJhbGciOi", "demo-password"} {
		if strings.Contains(out, secret) {
			t.Errorf("日志里泄露了 %q：%s", secret, out)
		}
	}
	if !strings.Contains(out, "p-001") {
		t.Errorf("personId 不是秘密，不该被打码：%s", out)
	}
}

// ============================================================
// 组件对平台的承诺
// ============================================================

// TestComponentYamlDeclaresDependencies：四个依赖，强弱要标对。
//
// 弱依赖漏标 optional 就成了强依赖：平台会因为它取不到而阻断整个启动，
// 而这个组件明明能在没有它的情况下正常工作。
func TestComponentYamlDeclaresDependencies(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	manifest := string(raw)

	for _, want := range []string{
		"people/basic@1.0.0",
		"auth/password-login@1.0.0",
		"authorization/rbac@1.0.0",
		"infra/redis-event-bus@1.0.0",
		"optional: true",
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("component.yaml 缺少声明：%s", want)
		}
	}

	// 弱依赖必须紧跟 optional: true
	weak := strings.Index(manifest, "infra/redis-event-bus@1.0.0")
	opt := strings.Index(manifest, "optional: true")
	if weak < 0 || opt < weak {
		t.Error("infra/redis-event-bus 必须标记为 optional: true，否则它会变成强依赖")
	}
}

// TestComponentYamlHasNoResources：这个组件不绑定任何资源。
//
// 它是连接组件，不掌握主数据。声明一个用不上的 database 资源会让使用者
// 白建一个库，还得为它填口令。
func TestComponentYamlHasNoResources(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	manifest := string(raw)

	if strings.Contains(manifest, "kind: database") || strings.Contains(manifest, "kind: cache") {
		t.Error("erp/backend 不该绑定任何资源——它是连接组件，不掌握主数据")
	}
	// 没有资源就没有表，也就没有迁移
	if strings.Contains(manifest, "migration:") {
		t.Error("没有资源依赖的组件不该声明 migration")
	}
}

// TestComponentYamlDeclaresSessionTTL：25.5 要覆盖的那个配置项得先声明。
func TestComponentYamlDeclaresSessionTTL(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}

	if !strings.Contains(string(raw), "sessionTtlSeconds") {
		t.Error("configSchema 应当声明 sessionTtlSeconds（平台据此注入 SESSION_TTL_SECONDS）")
	}
}

func TestOpenAPIDocumentIsValid(t *testing.T) {
	raw, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("读取 openapi.json 失败：%v", err)
	}

	var doc struct {
		OpenAPI string                            `json:"openapi"`
		Paths   map[string]map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi.json 不是合法 JSON：%v", err)
	}
	for _, path := range []string{"/healthz", "/api/v1/login", "/api/v1/orders"} {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("openapi.json 缺少端点 %s", path)
		}
	}
}

func TestDockerfileRunsAsNonRoot(t *testing.T) {
	raw, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("读取 Dockerfile 失败：%v", err)
	}

	content := string(raw)
	if !strings.Contains(content, "USER 10001") {
		t.Error("Dockerfile 必须以非 root 用户运行（USER 10001）")
	}
	if strings.Contains(content, "USER root") {
		t.Error("Dockerfile 里出现了 USER root")
	}
}

func TestNoHardcodedEndpoints(t *testing.T) {
	for _, name := range []string{"main.go", "service.go", "clients.go", "config.go", "eventbus.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", name, err)
		}
		content := string(raw)

		for _, forbidden := range []string{"localhost:", "127.0.0.1", "http://people", "http://auth"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s 里出现了硬编码地址 %q", name, forbidden)
			}
		}
	}
}
