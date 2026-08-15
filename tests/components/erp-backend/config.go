package main

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const (
	// listenAddr 是主端口，与 component.yaml 的 deployment.port 一致。
	listenAddr = ":8080"
	// readHeaderTimeout 防住慢速请求头攻击。
	readHeaderTimeout = 10 * time.Second
	// defaultSessionTTL 是会话默认时长，对应 configSchema.sessionTtlSeconds
	// 的 default（3600）。使用者可在 brickkit.yaml 的 config 里覆盖（003 §5.4）。
	defaultSessionTTL = time.Hour
)

// config 是组件的全部配置。**只来自环境变量**（002 §1.4、006 §5.1）：
// 组件不知道也不该知道自己被部署在哪。
type config struct {
	ComponentID string
	Version     string
	LogLevel    string
	// PeopleEndpoint 由平台按强依赖注入（003 §4.5：PEOPLE_BASIC_ENDPOINT）。
	PeopleEndpoint string
	// AuthEndpoint 是 auth/password-login 的 HTTP 地址（强依赖）。
	AuthEndpoint string
	// AuthorizationEndpoint 是 authorization/rbac 的地址（强依赖，gRPC 走主端口）。
	AuthorizationEndpoint string
	// PeopleGRPCEndpoint 是 people/basic 的 **gRPC** 地址。
	//
	// 注意它来自 PEOPLE_BASIC_GRPC_ENDPOINT 而不是 PEOPLE_BASIC_ENDPOINT：
	// people/basic 是 Python 组件，grpcio 无法与 HTTP 共用端口，因此在
	// Manifest 里声明了 extraPorts（9090），平台据此额外注入这个变量（003 §4.5）。
	PeopleGRPCEndpoint string
	// EventBusEndpoint 是 infra/redis-event-bus 的地址（**弱依赖**）。
	//
	// 弱依赖缺席时平台完全不注入这个变量，这里就是空串——组件据此降级。
	// 它绝不能出现在"缺少必需配置"的校验里。
	EventBusEndpoint string
	// SessionTTL 来自 configSchema 的 sessionTtlSeconds（开发计划 25.5）。
	SessionTTL time.Duration
}

// String 返回可安全写进日志的摘要：有地址与库名，**没有口令、没有签名密钥**。
func (c config) String() string {
	bus := "（未启用，弱依赖缺席）"
	if c.EventBusEndpoint != "" {
		bus = c.EventBusEndpoint
	}
	return fmt.Sprintf(
		"component=%s@%s auth=%s rbac=%s people-grpc=%s eventBus=%s sessionTTL=%s logLevel=%s",
		c.ComponentID, c.Version, c.AuthEndpoint, c.AuthorizationEndpoint,
		c.PeopleGRPCEndpoint, bus, c.SessionTTL, c.LogLevel)
}

// configFromEnv 从环境变量读配置。
//
// 缺失项一次全部报出，且**绝不退化到默认值**——尤其是 JWT_SECRET：
// 一个内置默认密钥意味着所有装了这个组件的人共用同一把钥匙，
// 任何人都能给任何部署签出管理员令牌，而且看起来一切正常。
func configFromEnv(lookup func(string) string) (config, error) {
	get := func(key string) string { return strings.TrimSpace(lookup(key)) }

	cfg := config{
		ComponentID:           valueOr(get("COMPONENT_ID"), "erp/backend"),
		Version:               valueOr(get("COMPONENT_VERSION"), "1.0.0"),
		LogLevel:              valueOr(get("LOG_LEVEL"), "info"),
		AuthEndpoint:          get("AUTH_PASSWORD_LOGIN_ENDPOINT"),
		AuthorizationEndpoint: get("AUTHORIZATION_RBAC_ENDPOINT"),
		PeopleGRPCEndpoint:    get("PEOPLE_BASIC_GRPC_ENDPOINT"),
		EventBusEndpoint:      get("INFRA_REDIS_EVENT_BUS_ENDPOINT"),
		SessionTTL:            defaultSessionTTL,
	}

	// 三个**强依赖**的地址由平台注入。缺失说明 brickkit.yaml 里没装对应组件，
	// 或者这个组件被手工跑起来了——两种情况都该当场说清楚。
	//
	// **INFRA_REDIS_EVENT_BUS_ENDPOINT 不在这里**：它是弱依赖，
	// 缺席是正常状态（003 §4.3）。把它列进来就等于把弱依赖变成了强依赖。
	var missing []string
	for name, value := range map[string]string{
		"AUTH_PASSWORD_LOGIN_ENDPOINT": cfg.AuthEndpoint,
		"AUTHORIZATION_RBAC_ENDPOINT":  cfg.AuthorizationEndpoint,
		"PEOPLE_BASIC_GRPC_ENDPOINT":   cfg.PeopleGRPCEndpoint,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sortStrings(missing)
		return config{}, fmt.Errorf(
			"缺少必需的配置：%s（这些地址由平台按 Manifest 中的强依赖注入，见 003 §4.5；"+
				"PEOPLE_BASIC_GRPC_ENDPOINT 来自 people/basic 声明的 extraPorts）",
			strings.Join(missing, ", "))
	}

	if raw := get("SESSION_TTL_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 1 {
			return config{}, fmt.Errorf("SESSION_TTL_SECONDS 必须是正整数（当前是 %q）", raw)
		}
		cfg.SessionTTL = time.Duration(seconds) * time.Second
	}

	return cfg, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// ============================================================
// 日志（002 §11）
// ============================================================

// 敏感字段名：这些键的值一律不写进日志（002 §11.3）。
//
// 这里不把 "key" 列为敏感词：本组件的日志里会出现缓存键（cacheKey），
// 那是排障时最有用的信息之一，而且不含任何秘密。
var sensitiveKeys = []string{"password", "token", "secret", "dsn", "credential"}

// newLogger 创建 JSON 日志器，每条都带 componentId（002 §11.3）。
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

// redactSensitive 把口令一类的字段替换成掩码。
//
// 靠"写日志的人记得别写口令"是不可靠的——在认证组件里更是如此。所以在出口
// 处统一挡掉：哪怕某天有人顺手加了一行 logger.Info("登录请求", "password", pw)，
// 落到日志里的也是 ***。
func redactSensitive(_ []string, attr slog.Attr) slog.Attr {
	name := strings.ToLower(attr.Key)
	for _, key := range sensitiveKeys {
		if strings.Contains(name, key) {
			return slog.String(attr.Key, "***")
		}
	}
	return attr
}
