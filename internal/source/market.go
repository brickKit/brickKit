package source

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/market"
	"github.com/brickkit/brickkit/internal/security"
)

// marketTimeout 是单次市场 API 请求的超时时间。
const marketTimeout = 30 * time.Second

// marketSource 是远程市场安装源（003 §6.2、007 §9.1）。
//
// 使用的端点：
//
//	GET {url}/components/{id}/versions/{ver}/manifest
//	GET {url}/components/{id}/versions/{ver}/artifacts
//	GET {url}/components/{id}/versions/{ver}/artifacts/{artifactId}/download?file={路径}
type marketSource struct {
	sourceID string
	baseURL  string
	// authToken 是 brickkit.yaml 中配置的 Token（登录态优先，见 tokenOnce）。
	authToken string
	// credentialsPath 是 .brickkit/credentials 的路径。
	credentialsPath string
	client          *http.Client
	now             func() time.Time

	tokenOnce sync.Once
	token     string
	tokenErr  error

	mu sync.Mutex
	// artifactIndex 缓存每个 <id>@<version> 的产物列表，避免逐个文件重复请求。
	artifactIndex map[string][]marketArtifact
	// signatures 记下每个 <id>@<version> 随 Manifest 一起返回的签名（008 §8.3）。
	//
	// 签名在取 Manifest 时顺手拿到，不另发一次请求：CLI 只调用 007 §4.5 的
	// manifest 端点，签名就在那个信封里。
	signatures map[string]*security.Signature
}

// marketArtifact 是产物列表端点返回的一条记录。
type marketArtifact struct {
	ID     string   `json:"id"`
	Type   string   `json:"type"`
	Format string   `json:"format"`
	Files  []string `json:"files"`
}

func (s *marketSource) id() string   { return s.sourceID }
func (s *marketSource) kind() string { return "market" }
func (s *marketSource) close() error { return nil }

func (s *marketSource) manifestBytes(ctx context.Context, componentID, version string) ([]byte, error) {
	body, err := s.get(ctx, s.versionPath(componentID, version)+"/manifest", nil)
	if err != nil {
		return nil, err
	}
	s.rememberSignature(componentID, version, signatureFromBody(body))
	return manifestFromBody(body, s.sourceID)
}

// signatureFor 返回上一次取 Manifest 时拿到的签名（signedFetcher）。
func (s *marketSource) signatureFor(componentID, version string) *security.Signature {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.signatures[componentID+"@"+version]
}

func (s *marketSource) rememberSignature(componentID, version string, sig *security.Signature) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.signatures == nil {
		s.signatures = map[string]*security.Signature{}
	}
	s.signatures[componentID+"@"+version] = sig
}

// signatureFromBody 从 Manifest 响应信封里取签名。
//
// 取不到就是"没有签名"，绝不报错：市场可能根本没实现这个字段，而"该不该
// 因为没签名而阻断"是使用者的策略（installer.requireSignature），不是解析层
// 能替他决定的事。
func signatureFromBody(body []byte) *security.Signature {
	var envelope struct {
		Data struct {
			Signature *security.Signature `json:"signature"`
		} `json:"data"`
		Signature *security.Signature `json:"signature"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}

	sig := envelope.Data.Signature
	if sig == nil {
		sig = envelope.Signature // 没有 data 信封时直接放在顶层
	}
	if sig == nil || sig.Empty() {
		return nil
	}
	return sig
}

func (s *marketSource) artifactFile(ctx context.Context, componentID, version string, art manifest.Artifact, file string) ([]byte, error) {
	list, err := s.artifacts(ctx, componentID, version)
	if err != nil {
		return nil, err
	}
	entry, ok := findArtifact(list, art, file)
	if !ok {
		return nil, errNotFound
	}
	return s.get(ctx, s.versionPath(componentID, version)+"/artifacts/"+entry.ID+"/download",
		url.Values{"file": []string{file}})
}

// origin 读取该版本的来源信息（开源 git / 闭源 registry，007 §11）。
//
// 它总是直接问市场，不走 Manifest 缓存：缓存里存的是 component.yaml 本身，
// 不含 sourceType / gitUrl。--repo 是低频操作，多一次请求换取信息准确。
func (s *marketSource) origin(ctx context.Context, componentID, version string) (*Origin, error) {
	body, err := s.get(ctx, s.versionPath(componentID, version)+"/manifest", nil)
	if err != nil {
		return nil, err
	}
	return originFromBody(body, s.sourceID), nil
}

// originFromBody 从 JSON 信封里取 sourceType / gitUrl；不是 JSON 或字段缺失时返回未知来源。
func originFromBody(body []byte, sourceID string) *Origin {
	o := &Origin{SourceID: sourceID}

	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return o
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		data = envelope
	}
	if v, ok := data["sourceType"].(string); ok {
		o.Type = v
	}
	if v, ok := data["gitUrl"].(string); ok {
		o.GitURL = v
	}
	return o
}

// artifacts 获取（并在本次运行内缓存）某个版本的产物列表。
func (s *marketSource) artifacts(ctx context.Context, componentID, version string) ([]marketArtifact, error) {
	key := componentID + "@" + version
	s.mu.Lock()
	cached, ok := s.artifactIndex[key]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}

	body, err := s.get(ctx, s.versionPath(componentID, version)+"/artifacts", nil)
	if err != nil {
		return nil, err
	}
	list, err := decodeArtifactList(body, s.sourceID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.artifactIndex == nil {
		s.artifactIndex = map[string][]marketArtifact{}
	}
	s.artifactIndex[key] = list
	s.mu.Unlock()
	return list, nil
}

func (s *marketSource) versionPath(componentID, version string) string {
	// 组件 ID 中的 `/` 是路径分隔符的一部分（007 §4.5），不做转义。
	return "/components/" + componentID + "/versions/" + version
}

// get 发起一次 GET 请求，返回响应体。
func (s *marketSource) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	endpoint := normalizeURL(s.baseURL) + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：市场安装源地址不合法").
			WithDetail("安装源", s.sourceID).
			WithDetail("地址", endpoint).
			WithHint("检查 brickkit.yaml → sources 中该安装源的 url").
			WithCause(err)
	}
	token, err := s.resolveToken()
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, clierr.New(clierr.CodeNetworkUnreachable, "错误：市场不可达").
			WithDetail("安装源", s.sourceID).
			WithDetail("地址", endpoint).
			WithDetail("原因", networkReason(err)).
			WithHint(
				"检查网络连接与市场地址是否正确",
				"或改用本地安装源（sources 中 type: local）离线安装",
			).WithCause(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errNotFound
	case resp.StatusCode != http.StatusOK:
		// 状态码只说明"哪一类问题"，真正的原因在响应信封的 error.code 里。
		// 只看状态码会把"版本已被下架"（403）说成"认证失败，请登录"，
		// 把使用者引到完全错误的方向上去。
		apiErr := market.DecodeError(resp.StatusCode, body)
		return nil, market.AsCLIError("访问市场", apiErr).
			WithDetail("安装源", s.sourceID).
			WithDetail("地址", endpoint)
	case readErr != nil:
		return nil, clierr.New(clierr.CodeNetworkUnreachable, "错误：读取市场响应失败").
			WithDetail("安装源", s.sourceID).
			WithDetail("地址", endpoint).
			WithDetail("原因", readErr.Error()).
			WithHint("检查网络连接后重试").
			WithCause(readErr)
	}
	return body, nil
}

// resolveToken 按 004 §5.3 的优先级解析 Token：
// .brickkit/credentials（登录态）> brickkit.yaml 的 sources.authToken。
func (s *marketSource) resolveToken() (string, error) {
	s.tokenOnce.Do(func() {
		creds, err := LoadCredentials(s.credentialsPath)
		if err != nil {
			s.tokenErr = err
			return
		}
		if creds != nil && creds.Token != "" && creds.MatchesMarket(s.baseURL) {
			if creds.Expired(s.now()) {
				s.tokenErr = clierr.New(clierr.CodeTokenExpired, "错误：Token 已过期").
					WithDetail("过期时间", creds.ExpiresAt.Format(time.RFC3339)).
					WithHint("重新执行 brickkit login 登录市场")
				return
			}
			s.token = creds.Token
			return
		}
		s.token = s.authToken
	})
	return s.token, s.tokenErr
}

// manifestFromBody 把市场响应还原成 component.yaml 文本。
//
// 兼容两种响应：
//  1. JSON 信封 {"success": true, "data": {"manifest": {...}}}（也接受 data 直接是 Manifest）
//  2. 直接返回 component.yaml 正文（YAML）
//
// 信封里的 sourceType / gitUrl 由 origin 单独读取（供 --repo 判断开源/闭源），
// 不进入 Manifest 本身。
func manifestFromBody(body []byte, sourceID string) ([]byte, error) {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "{") {
		return body, nil
	}

	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		// 不是合法 JSON，就按 YAML 正文处理，让 Manifest 解析器给出带行号的错误。
		return body, nil
	}
	if success, ok := envelope["success"].(bool); ok && !success {
		return nil, clierr.New(clierr.CodeComponentNotFound, "错误：市场返回失败").
			WithDetail("安装源", sourceID).
			WithDetail("原因", envelopeError(envelope)).
			WithHint("确认组件 ID 与版本号是否正确")
	}

	doc := any(envelope)
	if data, ok := envelope["data"]; ok {
		doc = data
	}
	if m, ok := doc.(map[string]any); ok {
		if inner, ok := m["manifest"]; ok {
			doc = inner
		}
	}
	if doc == nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误：市场返回的 Manifest 为空").
			WithDetail("安装源", sourceID).
			WithHint("确认市场服务是否正常")
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误：市场返回的 Manifest 无法解析").
			WithDetail("安装源", sourceID).
			WithDetail("原因", err.Error()).
			WithCause(err)
	}
	return out, nil
}

// decodeArtifactList 解析产物列表响应（JSON 信封或裸数组）。
func decodeArtifactList(body []byte, sourceID string) ([]marketArtifact, error) {
	var direct []marketArtifact
	if err := json.Unmarshal(body, &direct); err == nil {
		return direct, nil
	}

	var envelope struct {
		Success *bool            `json:"success"`
		Data    []marketArtifact `json:"data"`
		Error   json.RawMessage  `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, clierr.New(clierr.CodeNetworkUnreachable, "错误：市场返回的产物列表无法解析").
			WithDetail("安装源", sourceID).
			WithDetail("原因", err.Error()).
			WithHint("确认市场服务版本是否兼容").
			WithCause(err)
	}
	return envelope.Data, nil
}

// findArtifact 在产物列表中定位声明了该文件的条目。
func findArtifact(list []marketArtifact, art manifest.Artifact, file string) (marketArtifact, bool) {
	for _, entry := range list {
		if entry.Type != art.Type {
			continue
		}
		if art.Format != "" && entry.Format != "" && entry.Format != art.Format {
			continue
		}
		for _, f := range entry.Files {
			if f == file {
				return entry, true
			}
		}
	}
	return marketArtifact{}, false
}

func envelopeError(envelope map[string]any) string {
	if e, ok := envelope["error"].(map[string]any); ok {
		if msg, ok := e["message"].(string); ok && msg != "" {
			return msg
		}
	}
	if msg, ok := envelope["message"].(string); ok && msg != "" {
		return msg
	}
	return "市场未说明原因"
}

// networkReason 去掉 http.Client 错误里冗长的 URL 前缀，只保留根因。
func networkReason(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}
