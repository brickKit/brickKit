// 本文件是 Step 18-D 服务端启动配置的业务行为测试。
//
// 配置项与默认值来自《市场部署与运维指南》§5.1。配置读错的代价很高——
// 服务要么起不来，要么连到错误的库上，所以这里逐条锁住。
package config_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/config"
)

// fullEnv 是一份最小可用的完整配置。
func fullEnv() map[string]string {
	return map[string]string{
		"DATABASE_HOST":     "postgres",
		"DATABASE_NAME":     "brickkit_market",
		"DATABASE_USER":     "brickkit",
		"DATABASE_PASSWORD": "s3cret",
		"RUSTFS_ENDPOINT":   "http://rustfs:9000",
		"RUSTFS_ACCESS_KEY": "ak",
		"RUSTFS_SECRET_KEY": "sk",
		"ADMIN_USERNAME":    "admin",
		"ADMIN_PASSWORD":    "admin-password",
	}
}

func lookupFrom(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

// 缺项要一次全部报出来：让运维改一次 .env 就能起来，而不是改一项重启一次。
func TestFromEnvReportsAllMissingRequiredVarsAtOnce(t *testing.T) {
	_, err := config.FromEnv(lookupFrom(map[string]string{
		"DATABASE_HOST": "postgres",
	}))

	require.Error(t, err)
	for _, name := range []string{
		"DATABASE_NAME", "DATABASE_USER", "DATABASE_PASSWORD",
		"RUSTFS_ENDPOINT", "RUSTFS_ACCESS_KEY", "RUSTFS_SECRET_KEY",
		"ADMIN_USERNAME", "ADMIN_PASSWORD",
	} {
		assert.Contains(t, err.Error(), name, "缺失项 %s 应出现在错误里：%v", name, err)
	}
	assert.NotContains(t, err.Error(), "DATABASE_HOST", "已给的项不该报缺失")
}

// 运维指南 §5.1 的默认值。
func TestFromEnvAppliesDocumentedDefaults(t *testing.T) {
	cfg, err := config.FromEnv(lookupFrom(fullEnv()))
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "brickkit-artifacts", cfg.Storage.Bucket)
	assert.Equal(t, "us-east-1", cfg.Storage.Region)
	assert.Equal(t, 720*time.Hour, cfg.TokenTTL, "默认 30 天")
	assert.Equal(t, "admin", cfg.AdminUsername)
	assert.Equal(t, "admin-password", cfg.AdminPassword)
}

func TestFromEnvOverridesDefaults(t *testing.T) {
	env := fullEnv()
	env["PORT"] = "9090"
	env["DATABASE_PORT"] = "6432"
	env["RUSTFS_BUCKET"] = "my-bucket"
	env["RUSTFS_REGION"] = "cn-north-1"
	env["JWT_EXPIRY_HOURS"] = "24"

	cfg, err := config.FromEnv(lookupFrom(env))
	require.NoError(t, err)

	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, "my-bucket", cfg.Storage.Bucket)
	assert.Equal(t, "cn-north-1", cfg.Storage.Region)
	assert.Equal(t, 24*time.Hour, cfg.TokenTTL)
	assert.Contains(t, cfg.DatabaseURL, ":6432/")
}

// 数据库口令里带 @ : / 这类字符很常见（强密码生成器就会给）。
// 直接拼字符串会拼出一个连到错误主机的 DSN，必须转义。
func TestDatabaseURLEscapesSpecialCharactersInPassword(t *testing.T) {
	env := fullEnv()
	env["DATABASE_PASSWORD"] = "p@ss:w/ord?x=1"

	cfg, err := config.FromEnv(lookupFrom(env))
	require.NoError(t, err)

	parsed, err := url.Parse(cfg.DatabaseURL)
	require.NoError(t, err, "DSN 必须是可解析的 URL：%s", cfg.DatabaseURL)
	assert.Equal(t, "postgres", parsed.Scheme)
	assert.Equal(t, "postgres:5432", parsed.Host, "口令里的 @ 不能把主机名带跑")
	assert.Equal(t, "brickkit", parsed.User.Username())
	password, _ := parsed.User.Password()
	assert.Equal(t, "p@ss:w/ord?x=1", password)
	assert.Equal(t, "/brickkit_market", parsed.Path)
}

// 默认 sslmode=disable：compose 内网直连，开着反而起不来。
func TestDatabaseURLDefaultsToSSLDisabled(t *testing.T) {
	cfg, err := config.FromEnv(lookupFrom(fullEnv()))
	require.NoError(t, err)

	assert.Contains(t, cfg.DatabaseURL, "sslmode=disable")
}

func TestDatabaseURLHonoursExplicitSSLMode(t *testing.T) {
	env := fullEnv()
	env["DATABASE_SSLMODE"] = "require"

	cfg, err := config.FromEnv(lookupFrom(env))
	require.NoError(t, err)

	assert.Contains(t, cfg.DatabaseURL, "sslmode=require")
	assert.NotContains(t, cfg.DatabaseURL, "sslmode=disable")
}

// 端口写错了要当场报错。默默用 8080 会让运维对着一个"起来了但访问不到"的服务查半天。
func TestFromEnvRejectsNonNumericPort(t *testing.T) {
	env := fullEnv()
	env["PORT"] = "eight-thousand"

	_, err := config.FromEnv(lookupFrom(env))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PORT")
}

func TestFromEnvRejectsOutOfRangePort(t *testing.T) {
	env := fullEnv()
	env["PORT"] = "70000"

	_, err := config.FromEnv(lookupFrom(env))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PORT")
}

func TestFromEnvRejectsInvalidTokenExpiry(t *testing.T) {
	env := fullEnv()
	env["JWT_EXPIRY_HOURS"] = "0"

	_, err := config.FromEnv(lookupFrom(env))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_EXPIRY_HOURS")
}

// 运维指南 §5.1 与故障排查都强调 RUSTFS_ENDPOINT 必须带 scheme。
func TestFromEnvRejectsEndpointWithoutScheme(t *testing.T) {
	env := fullEnv()
	env["RUSTFS_ENDPOINT"] = "rustfs:9000"

	_, err := config.FromEnv(lookupFrom(env))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "RUSTFS_ENDPOINT")
	assert.Contains(t, err.Error(), "http://")
}

// 管理员口令太弱时不该放过：它是市场里权限最大的账号（007 §6.3 blocked 只有它能标）。
func TestFromEnvRejectsWeakAdminPassword(t *testing.T) {
	env := fullEnv()
	env["ADMIN_PASSWORD"] = "123"

	_, err := config.FromEnv(lookupFrom(env))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ADMIN_PASSWORD")
}

// 口令绝不能出现在错误信息或日志里。
func TestConfigStringRedactsSecrets(t *testing.T) {
	env := fullEnv()
	env["DATABASE_PASSWORD"] = "super-secret-db"
	env["ADMIN_PASSWORD"] = "super-secret-admin"
	env["RUSTFS_SECRET_KEY"] = "super-secret-s3"

	cfg, err := config.FromEnv(lookupFrom(env))
	require.NoError(t, err)

	rendered := cfg.String()
	for _, secret := range []string{"super-secret-db", "super-secret-admin", "super-secret-s3"} {
		assert.NotContains(t, rendered, secret, "配置摘要泄漏了密钥：%s", rendered)
	}
	assert.Contains(t, rendered, "postgres:5432", "非敏感信息应保留，便于确认连的是哪台机器")
	assert.True(t, strings.Contains(rendered, "8080"))
}
