// Package config 读取市场服务端的启动配置。
//
// 配置项、默认值与必填性来自《市场部署与运维指南》§5.1。所有密钥只走
// 环境变量，不落配置文件（006 §6、008 §4）。
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/brickkit/market-server/internal/storage"
)

// 环境变量名（运维指南 §5.1）。
const (
	EnvDatabaseHost     = "DATABASE_HOST"
	EnvDatabasePort     = "DATABASE_PORT"
	EnvDatabaseName     = "DATABASE_NAME"
	EnvDatabaseUser     = "DATABASE_USER"
	EnvDatabasePassword = "DATABASE_PASSWORD"
	EnvDatabaseSSLMode  = "DATABASE_SSLMODE"
	EnvTokenExpiryHours = "JWT_EXPIRY_HOURS"
	EnvAdminUsername    = "ADMIN_USERNAME"
	EnvAdminPassword    = "ADMIN_PASSWORD"
	EnvPort             = "PORT"
)

// 默认值（运维指南 §5.1）。
const (
	DefaultPort         = 8080
	DefaultDatabasePort = 5432
	// DefaultSSLMode 对应 compose 内网直连：那里没有证书，开着 SSL 反而连不上。
	DefaultSSLMode = "disable"
	// DefaultTokenExpiryHours 是 30 天。
	DefaultTokenExpiryHours = 720
	// MinAdminPasswordLength 是管理员口令的最短长度。
	// 管理员是市场里权限最大的账号（007 §6.3），不接受弱口令。
	MinAdminPasswordLength = 8
)

// Config 是市场服务端的启动配置。
type Config struct {
	// Port 是 API 监听端口。
	Port int
	// DatabaseURL 是 PostgreSQL 连接串。
	DatabaseURL string
	// Storage 是对象存储（RustFS）连接配置。
	Storage storage.Config
	// TokenTTL 是登录令牌的有效期。
	TokenTTL time.Duration
	// AdminUsername / AdminPassword 是首次启动要引导出来的管理员账号。
	AdminUsername string
	AdminPassword string
	// Version 是构建版本，健康检查会回显它。
	Version string
}

// String 返回可安全写进日志的配置摘要：只有地址与端口，没有任何口令。
func (c Config) String() string {
	db := "（未配置）"
	if u, err := url.Parse(c.DatabaseURL); err == nil {
		db = u.Host + u.Path
	}
	return fmt.Sprintf(
		"port=%d database=%s storage=%s bucket=%s tokenTTL=%s admin=%s",
		c.Port, db, c.Storage.Endpoint, c.Storage.Bucket, c.TokenTTL, c.AdminUsername)
}

// FromEnv 从环境变量读取配置。
//
// 缺失的必填项一次全部报出：让运维改一遍 .env 就能起来，
// 而不是"改一项、重启、再看下一项缺什么"。
func FromEnv(lookup func(string) string) (Config, error) {
	get := func(key string) string { return strings.TrimSpace(lookup(key)) }

	var missing []string
	required := func(key string) string {
		value := get(key)
		if value == "" {
			missing = append(missing, key)
		}
		return value
	}

	host := required(EnvDatabaseHost)
	name := required(EnvDatabaseName)
	user := required(EnvDatabaseUser)
	password := required(EnvDatabasePassword)
	adminUser := required(EnvAdminUsername)
	adminPassword := required(EnvAdminPassword)

	store := storage.Config{
		Endpoint:  get(storage.EnvEndpoint),
		AccessKey: get(storage.EnvAccessKey),
		SecretKey: get(storage.EnvSecretKey),
		Bucket:    get(storage.EnvBucket),
		Region:    get(storage.EnvRegion),
	}
	for key, value := range map[string]string{
		storage.EnvEndpoint:  store.Endpoint,
		storage.EnvAccessKey: store.AccessKey,
		storage.EnvSecretKey: store.SecretKey,
	} {
		if value == "" {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("缺少必填环境变量：%s", strings.Join(sorted(missing), ", "))
	}

	port, err := intOrDefault(get(EnvPort), DefaultPort, EnvPort)
	if err != nil {
		return Config{}, err
	}
	if port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("%s 必须在 1-65535 之间（当前是 %d）", EnvPort, port)
	}

	dbPort, err := intOrDefault(get(EnvDatabasePort), DefaultDatabasePort, EnvDatabasePort)
	if err != nil {
		return Config{}, err
	}

	hours, err := intOrDefault(get(EnvTokenExpiryHours), DefaultTokenExpiryHours, EnvTokenExpiryHours)
	if err != nil {
		return Config{}, err
	}
	if hours <= 0 {
		return Config{}, fmt.Errorf("%s 必须大于 0（当前是 %d）", EnvTokenExpiryHours, hours)
	}

	if len(adminPassword) < MinAdminPasswordLength {
		return Config{}, fmt.Errorf("%s 至少需要 %d 个字符：管理员是市场里权限最大的账号",
			EnvAdminPassword, MinAdminPasswordLength)
	}

	// storage.Config.Validate 会检查 endpoint 是否带 scheme——
	// 少了 scheme 时 S3 客户端的报错完全看不出根因（运维指南 §8 故障排查）。
	if err := store.Validate(); err != nil {
		return Config{}, err
	}
	if store.Bucket == "" {
		store.Bucket = storage.DefaultBucket
	}
	if store.Region == "" {
		store.Region = storage.DefaultRegion
	}

	sslMode := get(EnvDatabaseSSLMode)
	if sslMode == "" {
		sslMode = DefaultSSLMode
	}

	return Config{
		Port:          port,
		DatabaseURL:   databaseURL(host, dbPort, name, user, password, sslMode),
		Storage:       store,
		TokenTTL:      time.Duration(hours) * time.Hour,
		AdminUsername: adminUser,
		AdminPassword: adminPassword,
	}, nil
}

// databaseURL 拼出 PostgreSQL 连接串。
//
// 用 url.URL 而不是字符串拼接：强口令里常见的 @ : / ? 会把手工拼出来的
// DSN 拆到错误的主机上，而 url.Userinfo 会正确转义。
func databaseURL(host string, port int, name, user, password, sslMode string) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     host + ":" + strconv.Itoa(port),
		Path:     "/" + name,
		RawQuery: url.Values{"sslmode": []string{sslMode}}.Encode(),
	}
	return u.String()
}

// intOrDefault 解析整数环境变量。写错了当场报错——
// 默默退回默认值会让运维对着一个"起来了但访问不到"的服务查半天。
func intOrDefault(value string, fallback int, name string) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s 必须是整数（当前是 %q）", name, value)
	}
	return parsed, nil
}

// sorted 返回排好序的副本，让缺失项的报错顺序稳定。
func sorted(items []string) []string {
	out := append([]string(nil), items...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
