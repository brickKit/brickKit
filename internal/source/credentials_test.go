package source

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 未登录不是错误：凭据文件不存在时返回 (nil, nil)。
func TestLoadCredentialsMissingFile(t *testing.T) {
	c, err := LoadCredentials(filepath.Join(t.TempDir(), "credentials"))
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestLoadCredentialsFullFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	writeFile(t, path, `{
  "type": "password",
  "marketUrl": "https://market.brickkit.io/api/v1",
  "username": "zhangsan",
  "token": "eyJhbGciOiJIUzI1NiIs",
  "expiresAt": "2026-09-08T10:00:00Z",
  "createdAt": "2026-08-08T10:00:00Z"
}`)

	c, err := LoadCredentials(path)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "password", c.Type)
	assert.Equal(t, "https://market.brickkit.io/api/v1", c.MarketURL)
	assert.Equal(t, "zhangsan", c.Username)
	assert.Equal(t, "eyJhbGciOiJIUzI1NiIs", c.Token)
	assert.Equal(t, 2026, c.ExpiresAt.Year())
	assert.Equal(t, 2026, c.CreatedAt.Year())
}

func TestLoadCredentialsMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	writeFile(t, path, "not json at all")

	_, err := LoadCredentials(path)
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeAuthFailed, e.Code)
	assert.Contains(t, e.Format(), "brickkit login")
}

// 目录占位在凭据文件的位置上：读取失败要报出来，而不是当成"未登录"。
func TestLoadCredentialsUnreadable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	_, err := LoadCredentials(dir)
	require.Error(t, err)
	assert.Equal(t, clierr.CodeAuthFailed, clierr.As(err).Code)
}

func TestCredentialsExpired(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)

	assert.False(t, (&Credentials{}).Expired(now), "缺少 expiresAt 时视为不过期")
	assert.False(t, (&Credentials{ExpiresAt: now.Add(time.Hour)}).Expired(now))
	assert.True(t, (&Credentials{ExpiresAt: now.Add(-time.Hour)}).Expired(now))
	assert.False(t, (&Credentials{ExpiresAt: now}).Expired(now), "恰好到期的瞬间还不算过期")
}

// 008 安全边界：绝不把 A 市场的 Token 发给 B 市场。
func TestCredentialsMatchesMarket(t *testing.T) {
	const url = "https://market.brickkit.io/api/v1"

	assert.True(t, (&Credentials{}).MatchesMarket(url), "marketUrl 缺失时视为通配")
	assert.True(t, (&Credentials{MarketURL: url}).MatchesMarket(url))
	assert.True(t, (&Credentials{MarketURL: url + "/"}).MatchesMarket(url), "结尾斜杠不影响判定")
	assert.True(t, (&Credentials{MarketURL: " " + url + " "}).MatchesMarket(url))
	assert.False(t, (&Credentials{MarketURL: "https://evil.example.com/api/v1"}).MatchesMarket(url))
}
