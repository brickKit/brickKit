package main

import (
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"
)

const (
	// listenAddr 是主端口，与 component.yaml 的 deployment.port 一致。
	listenAddr = ":8080"
	// readHeaderTimeout 防住慢速请求头攻击。
	readHeaderTimeout = 10 * time.Second
	// cacheTTL 是探测结果的缓存时长。
	//
	// 每次刷新页面都去探七个组件的话，一个卡住的上游会让页面很慢，
	// 而组件的 API 文档几乎不会在几十秒内变。
	cacheTTL = 30 * time.Second
)

// aggregated 列出本组件会去聚合哪些组件，以及各自的地址来自哪个环境变量。
//
// 这份清单必须与 component.yaml 里的**弱依赖声明**一一对应：
// 声明了平台才会注入地址，这里才探得到。漏声明的表现是"那个组件永远显示未安装"。
var aggregated = []struct {
	ComponentID string
	EnvVar      string
}{
	{"department/tree", "DEPARTMENT_TREE_ENDPOINT"},
	{"people/basic", "PEOPLE_BASIC_ENDPOINT"},
	{"auth/password-login", "AUTH_PASSWORD_LOGIN_ENDPOINT"},
	{"authorization/rbac", "AUTHORIZATION_RBAC_ENDPOINT"},
	{"erp/backend", "ERP_BACKEND_ENDPOINT"},
	{"infra/redis-event-bus", "INFRA_REDIS_EVENT_BUS_ENDPOINT"},
}

// config 是组件的全部配置。**只来自环境变量**（002 §1.4、006 §5.1）。
type config struct {
	ComponentID string
	Version     string
	LogLevel    string
	// Targets 是要探测的组件。**没有任何一项是必需的**：
	// 全部都是弱依赖，一个都没装时本组件照样起来，页面上如实显示"都没装"
	Targets []Target
}

// String 返回可安全写进日志的摘要。
func (c config) String() string {
	installed := make([]string, 0, len(c.Targets))
	for _, t := range c.Targets {
		if t.Endpoint != "" {
			installed = append(installed, t.ComponentID)
		}
	}
	sort.Strings(installed)

	summary := strings.Join(installed, ", ")
	if summary == "" {
		summary = "（一个都没装）"
	}
	return fmt.Sprintf("component=%s@%s 已安装的聚合目标=%d/%d [%s] logLevel=%s",
		c.ComponentID, c.Version, len(installed), len(c.Targets), summary, c.LogLevel)
}

// configFromEnv 从环境变量读配置。
//
// 与其他组件的一个根本差别：**这里没有"缺少必需配置"的校验**。
// 本组件的依赖全是弱依赖（003 §4.3），缺席是常态——把任何一个列成必需，
// 就等于要求使用者必须把七个组件全装上才能看文档。
func configFromEnv(lookup func(string) string) (config, error) {
	get := func(key string) string { return strings.TrimSpace(lookup(key)) }

	targets := make([]Target, 0, len(aggregated))
	for _, item := range aggregated {
		targets = append(targets, Target{
			ComponentID: item.ComponentID,
			Endpoint:    get(item.EnvVar),
		})
	}

	return config{
		ComponentID: valueOr(get("COMPONENT_ID"), "infra/api-docs"),
		Version:     valueOr(get("COMPONENT_VERSION"), "1.0.0"),
		LogLevel:    valueOr(get("LOG_LEVEL"), "info"),
		Targets:     targets,
	}, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// ============================================================
// 日志（002 §11）
// ============================================================

var sensitiveKeys = []string{"password", "token", "secret", "dsn"}

func newLogger(w io.Writer, level, componentID string) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redactSensitive,
	})
	return slog.New(handler).With("componentId", componentID)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func redactSensitive(_ []string, attr slog.Attr) slog.Attr {
	name := strings.ToLower(attr.Key)
	for _, key := range sensitiveKeys {
		if strings.Contains(name, key) {
			return slog.String(attr.Key, "***")
		}
	}
	return attr
}
