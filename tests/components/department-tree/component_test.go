// 本文件验证组件对平台的承诺（开发计划 21.6–21.10）：
// 迁移、artifacts 声明、JSON 日志、环境变量配置、非 root 镜像。
//
// 这些不是业务功能，而是"能不能被平台装配"的前提。
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ============================================================
// 21.9 配置只来自环境变量
// ============================================================

// 006 §5.1：组件不得硬编码资源连接信息，一律从环境变量读。
func TestConfigComesFromEnvironment(t *testing.T) {
	lookup := map[string]string{
		"COMPONENT_ID":      "department/tree",
		"COMPONENT_VERSION": "2.0.0",
		"DATABASE_HOST":     "pg.internal",
		"DATABASE_PORT":     "6432",
		"DATABASE_NAME":     "department",
		"DATABASE_USER":     "dept",
		"DATABASE_PASSWORD": "s3cret",
		"LOG_LEVEL":         "debug",
	}

	cfg, err := configFromEnv(func(key string) string { return lookup[key] })
	if err != nil {
		t.Fatalf("读取配置失败：%v", err)
	}

	if cfg.ComponentID != "department/tree" || cfg.Version != "2.0.0" {
		t.Fatalf("平台变量没读对：%+v", cfg)
	}
	if cfg.Database.Host != "pg.internal" || cfg.Database.Port != 6432 {
		t.Fatalf("数据库连接没读对：%+v", cfg.Database)
	}
	if cfg.Database.Name != "department" || cfg.Database.User != "dept" {
		t.Fatalf("数据库连接没读对：%+v", cfg.Database)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("日志级别没读对：%q", cfg.LogLevel)
	}
}

// 数据库连接信息缺失时必须**明确报错**，不能退化到某个默认地址 ——
// 悄悄连到 localhost 会让人以为配好了，实际连的根本不是那个库。
func TestMissingDatabaseConfigIsAnError(t *testing.T) {
	_, err := configFromEnv(func(string) string { return "" })

	if err == nil {
		t.Fatal("缺少 DATABASE_* 时应报错")
	}
	for _, name := range []string{"DATABASE_HOST", "DATABASE_NAME", "DATABASE_USER"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("错误里应列出缺失的变量 %s，实际：%v", name, err)
		}
	}
}

// 端口写错时当场报错，而不是默默用 5432。
func TestInvalidDatabasePortIsAnError(t *testing.T) {
	lookup := map[string]string{
		"DATABASE_HOST": "pg", "DATABASE_NAME": "d", "DATABASE_USER": "u",
		"DATABASE_PASSWORD": "p", "DATABASE_PORT": "六千",
	}

	_, err := configFromEnv(func(key string) string { return lookup[key] })

	if err == nil || !strings.Contains(err.Error(), "DATABASE_PORT") {
		t.Fatalf("期望 DATABASE_PORT 相关的错误，实际：%v", err)
	}
}

// 口令不能出现在配置的字符串形式里（002 §11.3：日志不输出密码）。
func TestConfigStringHidesPassword(t *testing.T) {
	lookup := map[string]string{
		"DATABASE_HOST": "pg", "DATABASE_NAME": "d", "DATABASE_USER": "u",
		"DATABASE_PASSWORD": "super-secret-password",
	}

	cfg, err := configFromEnv(func(key string) string { return lookup[key] })
	if err != nil {
		t.Fatalf("读取配置失败：%v", err)
	}

	if strings.Contains(cfg.String(), "super-secret-password") {
		t.Fatalf("配置摘要泄漏了口令：%s", cfg.String())
	}
	if !strings.Contains(cfg.String(), "pg") {
		t.Fatalf("非敏感信息应保留，便于确认连的是哪台机器：%s", cfg.String())
	}
}

// ============================================================
// 21.8 JSON 日志（002 §11）
// ============================================================

func TestLogsAreJSONWithComponentID(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "info", "department/tree")

	logger.Info("部门树已加载", "count", 4)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("21.8 日志必须是 JSON：%s", buf.String())
	}
	if entry["componentId"] != "department/tree" {
		t.Fatalf("002 §11.3：日志必须带 componentId，实际：%v", entry)
	}
	if entry["msg"] != "部门树已加载" && entry["message"] != "部门树已加载" {
		t.Fatalf("日志内容不对：%v", entry)
	}
	if entry["level"] == nil && entry["severity"] == nil {
		t.Fatalf("日志要带级别：%v", entry)
	}
}

// 日志级别可调，debug 默认不输出。
func TestLogLevelIsConfigurable(t *testing.T) {
	var quiet, verbose bytes.Buffer

	newLogger(&quiet, "info", "department/tree").Debug("细节")
	newLogger(&verbose, "debug", "department/tree").Debug("细节")

	if quiet.Len() != 0 {
		t.Fatalf("info 级别不该输出 debug 日志：%s", quiet.String())
	}
	if verbose.Len() == 0 {
		t.Fatal("debug 级别应输出 debug 日志")
	}
}

// 口令绝不能进日志（002 §11.3）。
func TestLoggerRedactsPasswordLikeFields(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "info", "department/tree")

	logger.Info("连接数据库", "dsn", "postgres://u:super-secret@pg:5432/d", "password", "super-secret")

	if strings.Contains(buf.String(), "super-secret") {
		t.Fatalf("日志泄漏了口令：%s", buf.String())
	}
}

// 21.6 的迁移测试在 migrate_test.go：SQL 迁移只能对着真库测。

// ============================================================
// 21.7 artifacts 声明 / 21.10 非 root
// ============================================================

// 21.7：component.yaml 必须声明 proto 与 openapi 两类产物，
// 且声明的文件必须真的存在——市场发布时会按这个列表逐个上传。
func TestComponentYamlDeclaresArtifactsThatExist(t *testing.T) {
	raw, err := os.ReadFile("component.yaml")
	if err != nil {
		t.Fatalf("读取 component.yaml 失败：%v", err)
	}
	text := string(raw)

	for _, want := range []string{"api-contract", "protobuf", "api-docs", "openapi"} {
		if !strings.Contains(text, want) {
			t.Fatalf("21.7 component.yaml 应声明 %s", want)
		}
	}

	for _, file := range []string{"proto/department/v1/department.proto", "openapi.json"} {
		if !strings.Contains(text, file) {
			t.Fatalf("21.7 component.yaml 应声明产物文件 %s", file)
		}
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("21.7 声明的产物文件必须真的存在：%s（%v）", file, err)
		}
	}
}

// openapi.json 得是合法 JSON，并且描述的就是本组件的接口。
func TestOpenAPIDocumentIsValid(t *testing.T) {
	raw, err := os.ReadFile("openapi.json")
	if err != nil {
		t.Fatalf("读取 openapi.json 失败：%v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi.json 不是合法 JSON：%v", err)
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi.json 缺少 paths：%v", doc)
	}
	for _, path := range []string{"/api/v1/departments", "/healthz"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("openapi.json 应描述 %s，实际：%v", path, keysOf(paths))
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// 21.10 / 008：容器不得以 root 运行。
func TestDockerfileRunsAsNonRoot(t *testing.T) {
	raw, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("读取 Dockerfile 失败：%v", err)
	}
	text := string(raw)

	if !strings.Contains(text, "USER ") {
		t.Fatal("21.10 Dockerfile 必须切换到非 root 用户")
	}
	if strings.Contains(text, "USER root") {
		t.Fatal("21.10 不得以 root 运行")
	}
}
