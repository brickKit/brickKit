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
// "组件对平台的承诺"（Manifest 声明的产物真的在、镜像不以 root 运行）。

func envOf(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func completeEnv() map[string]string {
	return map[string]string{
		"DATABASE_HOST":         "postgres",
		"DATABASE_NAME":         "rbac",
		"DATABASE_USER":         "rbac_user",
		"DATABASE_PASSWORD":     "s3cr3t-p@ss/word",
		"PEOPLE_BASIC_ENDPOINT": "http://people-basic-1-0-0:8080",
		"REDIS_HOST":            "redis",
		"REDIS_PASSWORD":        "redis-secret",
	}
}

func TestConfigFromEnv(t *testing.T) {
	cfg, err := configFromEnv(envOf(completeEnv()))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}

	if cfg.Database.Port != 5432 {
		t.Errorf("DATABASE_PORT 缺省应为 5432，实际 %d", cfg.Database.Port)
	}
	if !cfg.Cache.Enabled() || cfg.Cache.Addr() != "redis:6379" {
		t.Errorf("REDIS_PORT 缺省应为 6379，实际 %q", cfg.Cache.Addr())
	}
	if cfg.Cache.TTL != defaultCacheTTL {
		t.Errorf("缓存 TTL 缺省应为 %v，实际 %v", defaultCacheTTL, cfg.Cache.TTL)
	}
}

// TestCacheIsOptional 是这个组件与前两个最大的不同。
//
// **Redis 是加速器，不是数据源。** 没绑定 cache 资源时组件照样要能起来，
// 只是每次都回源。若在这里报错，一个可选的基础设施就变成了硬依赖——
// 而 006 把 cache 定义为一种普通资源，绑不绑是使用者的决定。
func TestCacheIsOptional(t *testing.T) {
	env := completeEnv()
	delete(env, "REDIS_HOST")
	delete(env, "REDIS_PASSWORD")

	cfg, err := configFromEnv(envOf(env))
	if err != nil {
		t.Fatalf("没绑定 cache 资源也该能启动：%v", err)
	}
	if cfg.Cache.Enabled() {
		t.Error("没有 REDIS_HOST 时 Enabled() 应当为 false")
	}
}

// TestRequiredConfigReportedAtOnce：真正必需的那几项，缺了要一次说完。
func TestRequiredConfigReportedAtOnce(t *testing.T) {
	_, err := configFromEnv(envOf(map[string]string{}))
	if err == nil {
		t.Fatal("什么都没配还能启动？")
	}

	for _, name := range []string{
		"DATABASE_HOST", "DATABASE_NAME", "DATABASE_USER", "PEOPLE_BASIC_ENDPOINT",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("错误信息里没提到缺少 %s：%v", name, err)
		}
	}
	// Redis 是可选的，不该出现在"缺少必需配置"里
	if strings.Contains(err.Error(), "REDIS") {
		t.Errorf("Redis 是可选资源，不该被当成必需项：%v", err)
	}
}

func TestConfigRejectsBadNumbers(t *testing.T) {
	for name, pair := range map[string][2]string{
		"数据库端口不是数字":    {"DATABASE_PORT", "abc"},
		"Redis 端口不是数字": {"REDIS_PORT", "abc"},
		"缓存 TTL 不是数字":  {"CACHE_TTL_SECONDS", "five-minutes"},
		"缓存 TTL 为 0":   {"CACHE_TTL_SECONDS", "0"},
	} {
		t.Run(name, func(t *testing.T) {
			env := completeEnv()
			env[pair[0]] = pair[1]

			if _, err := configFromEnv(envOf(env)); err == nil {
				t.Fatalf("%s=%q 必须被拒绝", pair[0], pair[1])
			}
		})
	}
}

func TestCacheTTLOverride(t *testing.T) {
	env := completeEnv()
	env["CACHE_TTL_SECONDS"] = "60"

	cfg, err := configFromEnv(envOf(env))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}
	if cfg.Cache.TTL != time.Minute {
		t.Errorf("TTL 应为 1 分钟，实际 %v", cfg.Cache.TTL)
	}
}

// TestConfigStringHasNoSecrets：配置摘要会被打进日志。
func TestConfigStringHasNoSecrets(t *testing.T) {
	cfg, err := configFromEnv(envOf(completeEnv()))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}

	summary := cfg.String()
	for _, secret := range []string{"s3cr3t-p@ss/word", "redis-secret"} {
		if strings.Contains(summary, secret) {
			t.Errorf("配置摘要里出现了口令 %q：%s", secret, summary)
		}
	}
	for _, want := range []string{"postgres", "people-basic", "redis:6379"} {
		if !strings.Contains(summary, want) {
			t.Errorf("配置摘要里缺少 %q：%s", want, summary)
		}
	}
}

// TestConfigStringSaysWhenCacheIsAbsent：没绑缓存时摘要要说出来。
//
// 否则"为什么这么慢"这个问题会查很久——而答案只是没绑 cache 资源。
func TestConfigStringSaysWhenCacheIsAbsent(t *testing.T) {
	env := completeEnv()
	delete(env, "REDIS_HOST")

	cfg, _ := configFromEnv(envOf(env))
	if !strings.Contains(cfg.String(), "未绑定") {
		t.Errorf("没绑定 cache 时摘要要说明：%s", cfg.String())
	}
}

// ============================================================
// 日志
// ============================================================

func TestLoggerOutputsJSONWithComponentID(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "info", "authorization/rbac").Info("组件已就绪")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("日志不是 JSON：%s", buf.String())
	}
	if entry["componentId"] != "authorization/rbac" {
		t.Errorf("每条日志都要带 componentId：%v", entry)
	}
}

func TestLoggerRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "info", "authorization/rbac").Info("连接失败",
		"redis_password", "redis-secret",
		"dsn", "postgres://u:p@h/db",
		"personId", "p-001", // 不是秘密，要留着
	)

	out := buf.String()
	for _, secret := range []string{"redis-secret", "postgres://u:p@h/db"} {
		if strings.Contains(out, secret) {
			t.Errorf("日志里泄露了 %q：%s", secret, out)
		}
	}
	if !strings.Contains(out, "p-001") {
		t.Errorf("personId 不是秘密，不该被打码：%s", out)
	}
}

// ============================================================
// 参数解析（002 §8.5.1）
// ============================================================

func TestParseArgsRejectsUnknown(t *testing.T) {
	if _, _, err := parseArgs([]string{"migreate"}); err == nil {
		t.Fatal("拼错的参数必须报错，绝不能回落到启动服务")
	}

	mode, _, err := parseArgs(nil)
	if err != nil || mode != modeServe {
		t.Errorf("不带参数应当启动服务，实际 mode=%q err=%v", mode, err)
	}
}

// ============================================================
// 组件对平台的承诺
// ============================================================

func TestComponentYamlDeclaresArtifactsThatExist(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	manifest := string(raw)

	for _, file := range []string{
		"proto/authorization/v1/authorization.proto",
		"openapi.json",
	} {
		if !strings.Contains(manifest, file) {
			t.Errorf("component.yaml 应当声明产物 %s", file)
		}
		if _, err := os.Stat(file); err != nil {
			t.Errorf("component.yaml 声明了 %s，但文件不存在：%v", file, err)
		}
	}
}

// TestComponentYamlDeclaresDependencies：强依赖与资源都要写在 Manifest 里。
//
// cache 资源必须声明，否则平台不会注入 REDIS_*，组件就永远走"没绑缓存"的路径
// ——而它照样能跑，只是慢，谁也不会发现声明漏了。
func TestComponentYamlDeclaresDependencies(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	manifest := string(raw)

	for _, want := range []string{
		"people/basic@1.0.0", "engine: postgresql", "kind: cache", "engine: redis",
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("component.yaml 缺少声明：%s", want)
		}
	}
}

func TestMigrationCommandMatchesBinaryPath(t *testing.T) {
	manifest, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	dockerfile, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("读取 Dockerfile 失败：%v", err)
	}

	const binaryPath = "/app/authorization-rbac"
	if !strings.Contains(string(manifest), binaryPath) {
		t.Errorf("migration.command 应当指向 %s", binaryPath)
	}
	if !strings.Contains(string(dockerfile), binaryPath) {
		t.Errorf("Dockerfile 应当把二进制放在 %s", binaryPath)
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
	if doc.OpenAPI == "" {
		t.Error("缺少 openapi 版本字段")
	}
	for _, path := range []string{"/healthz", "/api/v1/permissions", "/api/v1/check"} {
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
	for _, name := range []string{"main.go", "service.go", "people.go", "config.go", "cache.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", name, err)
		}
		content := string(raw)

		for _, forbidden := range []string{"localhost:", "127.0.0.1", "http://people"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s 里出现了硬编码地址 %q", name, forbidden)
			}
		}
	}
}
