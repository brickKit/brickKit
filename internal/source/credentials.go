package source

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
)

// Credentials 是 .brickkit/credentials 的内容（004 §5.3）。
//
//	{
//	  "type": "password",
//	  "marketUrl": "https://market.brickkit.io/api/v1",
//	  "username": "zhangsan",
//	  "token": "eyJhbGciOiJIUzI1NiIs...",
//	  "expiresAt": "2026-09-08T10:00:00Z",
//	  "createdAt": "2026-08-08T10:00:00Z"
//	}
type Credentials struct {
	Type      string    `json:"type"`
	MarketURL string    `json:"marketUrl"`
	Username  string    `json:"username"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// LoadCredentials 读取登录凭据。文件不存在时返回 (nil, nil)——未登录不是错误。
func LoadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, clierr.New(clierr.CodeAuthFailed, "错误：读取登录凭据失败").
			WithDetail("路径", path).
			WithDetail("原因", err.Error()).
			WithHint("重新执行 brickkit login 登录市场").
			WithCause(err)
	}

	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, clierr.New(clierr.CodeAuthFailed, "错误：登录凭据格式不合法").
			WithDetail("路径", path).
			WithHint("删除该文件后重新执行 brickkit login 登录市场").
			WithCause(err)
	}
	return &c, nil
}

// Expired 判断凭据是否已过期。expiresAt 缺失（零值）时视为不过期。
func (c *Credentials) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}

// MatchesMarket 判断该凭据是否属于给定的市场地址。
//
// marketUrl 缺失时视为通配（兼容手工写入的凭据）；否则必须与安装源 url 一致。
// 这是安全边界（008）：绝不把 A 市场的 Token 发给 B 市场。
func (c *Credentials) MatchesMarket(url string) bool {
	if c.MarketURL == "" {
		return true
	}
	return normalizeURL(c.MarketURL) == normalizeURL(url)
}

func normalizeURL(u string) string { return strings.TrimRight(strings.TrimSpace(u), "/") }
