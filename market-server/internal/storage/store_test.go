package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validConfig() Config {
	return Config{
		Endpoint:  "http://localhost:9000",
		AccessKey: "rustfsadmin",
		SecretKey: "rustfsadmin",
	}
}

func TestConfigValidate(t *testing.T) {
	require.NoError(t, validConfig().Validate())

	cases := map[string]struct {
		mutate   func(*Config)
		contains string
	}{
		"缺少 endpoint":       {func(c *Config) { c.Endpoint = "" }, EnvEndpoint},
		"缺少 accessKey":      {func(c *Config) { c.AccessKey = "" }, EnvAccessKey},
		"缺少 secretKey":      {func(c *Config) { c.SecretKey = "" }, EnvSecretKey},
		"endpoint 无 scheme": {func(c *Config) { c.Endpoint = "localhost:9000" }, "http://"},
		"endpoint 协议不支持":    {func(c *Config) { c.Endpoint = "ftp://localhost:9000" }, "http://"},
		"endpoint 缺主机":      {func(c *Config) { c.Endpoint = "http://" }, "缺少主机地址"},
		"endpoint 非法 URL":   {func(c *Config) { c.Endpoint = "http://[::1" }, EnvEndpoint},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig()
			c.mutate(&cfg)

			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.contains)
		})
	}
}

// 缺失项一次全部报出。
func TestConfigValidateReportsAllMissing(t *testing.T) {
	err := Config{}.Validate()
	require.Error(t, err)
	for _, want := range []string{EnvEndpoint, EnvAccessKey, EnvSecretKey} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{
		EnvEndpoint:  "http://localhost:9000",
		EnvAccessKey: "rustfsadmin",
		EnvSecretKey: "rustfsadmin",
	}
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	require.NoError(t, err)

	assert.Equal(t, "http://localhost:9000", cfg.Endpoint)
	assert.Equal(t, "rustfsadmin", cfg.AccessKey)
	assert.Equal(t, DefaultBucket, cfg.Bucket, "未设置时使用默认 bucket")
	assert.Equal(t, DefaultRegion, cfg.Region, "未设置时使用默认 region")
}

func TestConfigFromEnvOverrides(t *testing.T) {
	env := map[string]string{
		EnvEndpoint:  "https://storage.example.com",
		EnvAccessKey: "ak",
		EnvSecretKey: "sk",
		EnvBucket:    "my-bucket",
		EnvRegion:    "cn-north-1",
	}
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	require.NoError(t, err)
	assert.Equal(t, "my-bucket", cfg.Bucket)
	assert.Equal(t, "cn-north-1", cfg.Region)
}

func TestConfigFromEnvMissing(t *testing.T) {
	_, err := ConfigFromEnv(func(string) string { return "" })
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvEndpoint)
}

// RustFS 等自建 S3 服务必须用 path-style 寻址并指向自定义端点。
func TestNewS3ClientOptions(t *testing.T) {
	client, err := NewS3Client(validConfig())
	require.NoError(t, err)
	require.NotNil(t, client)

	opts := client.Options()
	require.NotNil(t, opts.BaseEndpoint)
	assert.Equal(t, "http://localhost:9000", *opts.BaseEndpoint)
	assert.True(t, opts.UsePathStyle, "自建 S3 服务需要 path-style 寻址")
	assert.Equal(t, DefaultRegion, opts.Region)

	creds, err := opts.Credentials.Retrieve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "rustfsadmin", creds.AccessKeyID)
	assert.Equal(t, "rustfsadmin", creds.SecretAccessKey)
}

func TestNewS3ClientRejectsInvalidConfig(t *testing.T) {
	_, err := NewS3Client(Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvEndpoint)
}
