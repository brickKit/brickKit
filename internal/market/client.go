package market

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/security"
)

// requestTimeout 是单次市场请求的超时时间。产物上传可能有几十 MB，给宽一点。
const requestTimeout = 2 * time.Minute

// Client 是市场的写入侧客户端。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New 创建客户端。token 为空表示匿名（只有 login 用得到）。
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   token,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// LoginResult 是登录成功后市场返回的令牌信息。
type LoginResult struct {
	Token     string    `json:"token"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// Login 用用户名密码换取访问令牌（007 §9.6）。
func (c *Client) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	body, err := c.do(ctx, http.MethodPost, "/auth/login", nil,
		jsonBody(map[string]string{"username": username, "password": password}), i18n.T(msgid.MarketActionLogin))
	if err != nil {
		return nil, err
	}

	var result LoginResult
	if err := decodeData(body, &result); err != nil {
		return nil, err
	}
	if result.Token == "" {
		return nil, clierr.New(clierr.CodeAuthFailed, i18n.T(msgid.MarketNoToken)).
			WithHint(i18n.T(msgid.MarketHintCheckMarketVersion))
	}
	return &result, nil
}

// Logout 作废服务端那一侧的令牌（007 §9.5）。
//
// 重复注销是幂等的（市场侧保证），所以本地凭据已经删了、再调一次也没关系。
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/logout", nil, nil, i18n.T(msgid.MarketActionLogout))
	return err
}

// PublishRequest 是发布一个版本的请求体（007 §3.7）。
type PublishRequest struct {
	Version    string          `json:"version"`
	Status     string          `json:"status"`
	Manifest   json.RawMessage `json:"manifest"`
	SourceType string          `json:"sourceType"`
	GitURL     string          `json:"gitUrl,omitempty"`
	Changelog  string          `json:"changelog,omitempty"`
	// Signature 是对 Manifest 规范化载荷的签名（008 §8.3），未签名时为 nil。
	Signature *security.Signature `json:"signature,omitempty"`
}

// Artifact 是市场返回的产物条目。
type Artifact struct {
	ID     string   `json:"id"`
	Type   string   `json:"type"`
	Format string   `json:"format"`
	Files  []string `json:"files"`
}

// CreateVersion 建一个版本（发布三步中的第一步）。
func (c *Client) CreateVersion(ctx context.Context, componentID string, req PublishRequest) error {
	_, err := c.do(ctx, http.MethodPost, versionsPath(componentID), nil, jsonBody(req), i18n.T(msgid.MarketActionPublishVersion))
	return err
}

// VersionInfo 是市场上一个版本的状态（007 §9.2 的版本列表）。
type VersionInfo struct {
	Version string `json:"version"`
	Status  string `json:"status"`
}

// FindVersion 查这个版本在市场上的状态；不存在时返回 nil。
//
// publish 撞上 VERSION_EXISTS 时靠它分辨两种完全不同的情况：上一次没发完留下的
// draft（可以续传），还是真的已经发布过了（只能换版本号）。
func (c *Client) FindVersion(ctx context.Context, componentID, version string) (*VersionInfo, error) {
	body, err := c.do(ctx, http.MethodGet, versionsPath(componentID), nil, nil, i18n.T(msgid.MarketActionListVersions))
	if err != nil {
		return nil, err
	}
	var versions []VersionInfo
	if err := decodeData(body, &versions); err != nil {
		return nil, err
	}
	for _, v := range versions {
		if v.Version == version {
			found := v
			return &found, nil
		}
	}
	return nil, nil
}

// FetchManifest 取市场上登记的那份 Manifest（原始 JSON）。
//
// 续传之前要拿它与本地逐字节比对：draft 里登记的是**上一次**那份 Manifest，
// 组件改过之后闷头续传，会把旧 Manifest 配上新产物发出去。
func (c *Client) FetchManifest(ctx context.Context, componentID, version string) (json.RawMessage, error) {
	body, err := c.do(ctx, http.MethodGet,
		versionPath(componentID, version)+"/manifest", nil, nil, i18n.T(msgid.MarketActionFetchManifest))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Manifest json.RawMessage `json:"manifest"`
	}
	if err := decodeData(body, &envelope); err != nil {
		return nil, err
	}
	return envelope.Manifest, nil
}

// ListArtifacts 取该版本已登记的产物，用来知道每个文件该往哪个 artifactId 上传。
func (c *Client) ListArtifacts(ctx context.Context, componentID, version string) ([]Artifact, error) {
	body, err := c.do(ctx, http.MethodGet,
		versionPath(componentID, version)+"/artifacts", nil, nil, i18n.T(msgid.MarketActionListArtifacts))
	if err != nil {
		return nil, err
	}

	var artifacts []Artifact
	if err := decodeData(body, &artifacts); err != nil {
		return nil, err
	}
	return artifacts, nil
}

// UploadArtifact 上传一个产物文件。
func (c *Client) UploadArtifact(
	ctx context.Context, componentID, version, artifactID, file string, content []byte,
) error {
	_, err := c.do(ctx, http.MethodPost,
		versionPath(componentID, version)+"/artifacts/"+artifactID+"/upload",
		url.Values{"file": []string{file}},
		bytes.NewReader(content), i18n.T(msgid.MarketActionUploadArtifact, file))
	return err
}

// SetVersionStatus 变更版本状态（发布三步中的最后一步：draft → stable）。
func (c *Client) SetVersionStatus(ctx context.Context, componentID, version, status string) error {
	_, err := c.do(ctx, http.MethodPut, versionPath(componentID, version), nil,
		jsonBody(map[string]string{"status": status}), i18n.T(msgid.MarketActionSetStatus))
	return err
}

// SetVisibility 设置组件可见性（007 §9.4）。
func (c *Client) SetVisibility(ctx context.Context, componentID, visibility string) error {
	_, err := c.do(ctx, http.MethodPut, "/components/"+componentID+"/visibility", nil,
		jsonBody(map[string]string{"visibility": visibility}), i18n.T(msgid.MarketActionSetVisibility))
	return err
}

// 组件 ID 中的 `/` 是路径的一部分（007 §4.5），不做转义。
func versionsPath(componentID string) string { return "/components/" + componentID + "/versions" }

func versionPath(componentID, version string) string {
	return versionsPath(componentID) + "/" + version
}

// do 发起一次请求并返回响应体。
func (c *Client) do(
	ctx context.Context, method, path string, query url.Values, body io.Reader, action string,
) ([]byte, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.MarketBadURL)).
			WithDetail(i18n.T(msgid.LabelAddress), endpoint).
			WithHint(i18n.T(msgid.MarketHintCheckMarketURL)).
			WithCause(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, unreachable(endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, readErr := io.ReadAll(resp.Body)
	if !statusOK(resp.StatusCode) {
		apiErr := DecodeError(resp.StatusCode, raw)
		return nil, WithDetails(AsCLIError(action, apiErr), apiErr)
	}
	if readErr != nil {
		return nil, clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.MarketReadResponseFailed)).
			WithDetail(i18n.T(msgid.LabelAddress), endpoint).
			WithDetail(i18n.T(msgid.LabelReason), readErr.Error()).
			WithHint(i18n.T(msgid.MarketHintCheckNetworkRetry)).
			WithCause(readErr)
	}
	return raw, nil
}

// decodeData 解开 {"success":true,"data":...} 信封，把 data 解到 target。
func decodeData(body []byte, target any) error {
	var envelope struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	payload := body
	if err := json.Unmarshal(body, &envelope); err != nil {
		// 不是信封形状。可能是裸数据（如直接返回数组），先按裸数据试一次；
		// 再不行才说明这个地址根本不是市场 API。
		if json.Unmarshal(body, target) == nil {
			return nil
		}
		return clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.MarketBodyUnparseable)).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.MarketHintCheckAPIBase)).
			WithCause(err)
	}
	if len(envelope.Data) > 0 {
		payload = envelope.Data
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.MarketBodyShapeMismatch)).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.MarketHintCheckMarketVersion)).
			WithCause(err)
	}
	return nil
}

func jsonBody(v any) io.Reader {
	raw, err := json.Marshal(v)
	if err != nil {
		// 这里的入参都是本包构造的普通结构，编码失败只可能是编程错误
		return strings.NewReader("{}")
	}
	return bytes.NewReader(raw)
}

// networkReason 去掉 http.Client 错误里冗长的 URL 前缀，只留根因。
func networkReason(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}
