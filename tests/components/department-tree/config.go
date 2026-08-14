package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// listenAddr 是主端口，与 component.yaml 的 deployment.port 一致。
	listenAddr = ":8080"
	// readHeaderTimeout 防住慢速请求头攻击。
	readHeaderTimeout = 10 * time.Second
	// shutdownTimeout 是收到停止信号后等待在途请求的时间。
	shutdownTimeout = 15 * time.Second
)

// databaseConfig 是平台注入的数据库连接（006 §5.2）。
type databaseConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
}

// DSN 拼出连接串。用 url.URL 而不是字符串拼接：
// 强口令里的 @ : / 会把手工拼出来的 DSN 拆到错误的主机上。
func (d databaseConfig) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password),
		Host:     d.Host + ":" + strconv.Itoa(d.Port),
		Path:     "/" + d.Name,
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

// config 是组件的全部配置。**只来自环境变量**（002 §1.4、006 §5.1）：
// 组件不知道也不该知道自己被部署在哪。
type config struct {
	ComponentID string
	Version     string
	LogLevel    string
	Database    databaseConfig
}

// String 返回可安全写进日志的摘要：有地址与库名，没有口令。
func (c config) String() string {
	return fmt.Sprintf("component=%s@%s database=%s:%d/%s user=%s logLevel=%s",
		c.ComponentID, c.Version, c.Database.Host, c.Database.Port,
		c.Database.Name, c.Database.User, c.LogLevel)
}

// configFromEnv 从环境变量读配置。
//
// 缺失项一次全部报出，且**绝不退化到默认地址**：悄悄连到 localhost
// 会让人以为配好了，实际连的根本不是那个库。
func configFromEnv(lookup func(string) string) (config, error) {
	get := func(key string) string { return strings.TrimSpace(lookup(key)) }

	cfg := config{
		ComponentID: valueOr(get("COMPONENT_ID"), "department/tree"),
		Version:     valueOr(get("COMPONENT_VERSION"), "1.0.0"),
		LogLevel:    valueOr(get("LOG_LEVEL"), "info"),
		Database: databaseConfig{
			Host:     get("DATABASE_HOST"),
			Name:     get("DATABASE_NAME"),
			User:     get("DATABASE_USER"),
			Password: get("DATABASE_PASSWORD"),
		},
	}

	var missing []string
	for name, value := range map[string]string{
		"DATABASE_HOST": cfg.Database.Host,
		"DATABASE_NAME": cfg.Database.Name,
		"DATABASE_USER": cfg.Database.User,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sortStrings(missing)
		return config{}, fmt.Errorf(
			"缺少数据库连接配置：%s（这些变量由平台按 brickkit.yaml 的资源绑定注入，见 006 §5）",
			strings.Join(missing, ", "))
	}

	port := 5432
	if raw := get("DATABASE_PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return config{}, fmt.Errorf("DATABASE_PORT 必须是整数（当前是 %q）", raw)
		}
		port = parsed
	}
	cfg.Database.Port = port

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
var sensitiveKeys = []string{"password", "token", "secret", "dsn", "key"}

// newLogger 创建 JSON 日志器。
//
// 每条日志都带 componentId：一个项目里跑着十几个组件，
// 没有这个字段就没法在聚合日志里把它们分开（002 §11.3）。
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
// 靠"写日志的人记得别写口令"是不可靠的：连接失败时最想打印的就是 DSN，
// 而 DSN 里正好带着口令。所以在出口处统一挡掉。
func redactSensitive(_ []string, attr slog.Attr) slog.Attr {
	name := strings.ToLower(attr.Key)
	for _, key := range sensitiveKeys {
		if strings.Contains(name, key) {
			return slog.String(attr.Key, "***")
		}
	}
	return attr
}
