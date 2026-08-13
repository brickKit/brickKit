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
		jsonBody(map[string]string{"username": username, "password": password}), "登录市场")
	if err != nil {
		return nil, err
	}

	var result LoginResult
	if err := decodeData(body, &result); err != nil {
		return nil, err
	}
	if result.Token == "" {
		return nil, clierr.New(clierr.CodeAuthFailed, "错误：市场没有返回访问令牌").
			WithHint("确认市场服务版本是否兼容")
	}
	return &result, nil
}

// PublishRequest 是发布一个版本的请求体（007 §3.7）。
type PublishRequest struct {
	Version    string          `json:"version"`
	Status     string          `json:"status"`
	Manifest   json.RawMessage `json:"manifest"`
	SourceType string          `json:"sourceType"`
	GitURL     string          `json:"gitUrl,omitempty"`
	Changelog  string          `json:"changelog,omitempty"`
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
	_, err := c.do(ctx, http.MethodPost, versionsPath(componentID), nil, jsonBody(req), "发布组件版本")
	return err
}

// ListArtifacts 取该版本已登记的产物，用来知道每个文件该往哪个 artifactId 上传。
func (c *Client) ListArtifacts(ctx context.Context, componentID, version string) ([]Artifact, error) {
	body, err := c.do(ctx, http.MethodGet,
		versionPath(componentID, version)+"/artifacts", nil, nil, "查询产物列表")
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
		bytes.NewReader(content), "上传产物 "+file)
	return err
}

// SetVersionStatus 变更版本状态（发布三步中的最后一步：draft → stable）。
func (c *Client) SetVersionStatus(ctx context.Context, componentID, version, status string) error {
	_, err := c.do(ctx, http.MethodPut, versionPath(componentID, version), nil,
		jsonBody(map[string]string{"status": status}), "设置版本状态")
	return err
}

// SetVisibility 设置组件可见性（007 §9.4）。
func (c *Client) SetVisibility(ctx context.Context, componentID, visibility string) error {
	_, err := c.do(ctx, http.MethodPut, "/components/"+componentID+"/visibility", nil,
		jsonBody(map[string]string{"visibility": visibility}), "设置可见性")
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
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：市场地址不合法").
			WithDetail("地址", endpoint).
			WithHint("检查 brickkit.yaml → sources 中的市场地址，或 --market 参数").
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
		return nil, clierr.New(clierr.CodeNetworkUnreachable, "错误：读取市场响应失败").
			WithDetail("地址", endpoint).
			WithDetail("原因", readErr.Error()).
			WithHint("检查网络连接后重试").
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
		return clierr.New(clierr.CodeNetworkUnreachable, "错误：市场返回的内容无法解析").
			WithDetail("原因", err.Error()).
			WithHint("确认市场地址是否指向 BrickKit Market 的 /api/v1").
			WithCause(err)
	}
	if len(envelope.Data) > 0 {
		payload = envelope.Data
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return clierr.New(clierr.CodeNetworkUnreachable, "错误：市场返回的内容格式不符").
			WithDetail("原因", err.Error()).
			WithHint("确认市场服务版本是否兼容").
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
