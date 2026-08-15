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
	// defaultTokenTTL 是令牌默认有效期，可由 configSchema 的 tokenTtlSeconds 覆盖。
	defaultTokenTTL = 30 * time.Minute
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
	// PeopleEndpoint 由平台按强依赖注入（003 §4.5：PEOPLE_BASIC_ENDPOINT）。
	PeopleEndpoint string
	// JWTSecret 是令牌签名密钥，由使用者通过 .env / K8s Secret 提供。
	JWTSecret string
	// TokenTTL 来自 configSchema 的 tokenTtlSeconds。
	TokenTTL time.Duration
}

// String 返回可安全写进日志的摘要：有地址与库名，**没有口令、没有签名密钥**。
func (c config) String() string {
	return fmt.Sprintf("component=%s@%s database=%s:%d/%s user=%s people=%s tokenTTL=%s logLevel=%s",
		c.ComponentID, c.Version, c.Database.Host, c.Database.Port,
		c.Database.Name, c.Database.User, c.PeopleEndpoint, c.TokenTTL, c.LogLevel)
}

// configFromEnv 从环境变量读配置。
//
// 缺失项一次全部报出，且**绝不退化到默认值**——尤其是 JWT_SECRET：
// 一个内置默认密钥意味着所有装了这个组件的人共用同一把钥匙，
// 任何人都能给任何部署签出管理员令牌，而且看起来一切正常。
func configFromEnv(lookup func(string) string) (config, error) {
	get := func(key string) string { return strings.TrimSpace(lookup(key)) }

	cfg := config{
		ComponentID: valueOr(get("COMPONENT_ID"), "auth/password-login"),
		Version:     valueOr(get("COMPONENT_VERSION"), "1.0.0"),
		LogLevel:    valueOr(get("LOG_LEVEL"), "info"),
		Database: databaseConfig{
			Host:     get("DATABASE_HOST"),
			Name:     get("DATABASE_NAME"),
			User:     get("DATABASE_USER"),
			Password: get("DATABASE_PASSWORD"),
		},
		PeopleEndpoint: get("PEOPLE_BASIC_ENDPOINT"),
		JWTSecret:      get("JWT_SECRET"),
		TokenTTL:       defaultTokenTTL,
	}

	var missing []string
	for name, value := range map[string]string{
		"DATABASE_HOST": cfg.Database.Host,
		"DATABASE_NAME": cfg.Database.Name,
		"DATABASE_USER": cfg.Database.User,
		// 强依赖的地址由平台注入。它缺失说明 brickkit.yaml 里没装 people/basic，
		// 或者这个组件被手工跑起来了——两种情况都该当场说清楚
		"PEOPLE_BASIC_ENDPOINT": cfg.PeopleEndpoint,
		"JWT_SECRET":            cfg.JWTSecret,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sortStrings(missing)
		return config{}, fmt.Errorf(
			"缺少必需的配置：%s（DATABASE_* 由平台按资源绑定注入、"+
				"PEOPLE_BASIC_ENDPOINT 由平台按强依赖注入，见 006 §5 与 003 §4.5；"+
				"JWT_SECRET 需要你自己在 .env 或 K8s Secret 中提供）",
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

	if raw := get("TOKEN_TTL_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 1 {
			return config{}, fmt.Errorf("TOKEN_TTL_SECONDS 必须是正整数（当前是 %q）", raw)
		}
		cfg.TokenTTL = time.Duration(seconds) * time.Second
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
// 对一个认证组件，这条比别处更要紧：出错时最想打印的就是"收到的请求体"，
// 而那里面正好是明文口令。
var sensitiveKeys = []string{"password", "token", "secret", "dsn", "key", "hash", "credential"}

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
