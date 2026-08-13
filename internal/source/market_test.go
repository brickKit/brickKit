package source

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// ============================================================
// manifestFromBody：市场响应 → component.yaml 文本
// ============================================================

func TestManifestFromBodyJSONEnvelope(t *testing.T) {
	body := []byte(`{"success":true,"data":{"manifest":{"metadata":{"id":"people/basic"}}}}`)

	out, err := manifestFromBody(body, "brickkit-market")
	require.NoError(t, err)
	assert.Contains(t, string(out), "id: people/basic")
}

// data 直接就是 Manifest（不套一层 manifest 键）时同样接受。
func TestManifestFromBodyDataIsManifest(t *testing.T) {
	body := []byte(`{"success":true,"data":{"metadata":{"id":"people/basic"}}}`)

	out, err := manifestFromBody(body, "brickkit-market")
	require.NoError(t, err)
	assert.Contains(t, string(out), "id: people/basic")
}

// 裸 JSON Manifest（无信封）也接受。
func TestManifestFromBodyBareJSON(t *testing.T) {
	body := []byte(`{"metadata":{"id":"people/basic"}}`)

	out, err := manifestFromBody(body, "brickkit-market")
	require.NoError(t, err)
	assert.Contains(t, string(out), "id: people/basic")
}

// YAML 正文原样透传（不做任何改写，缓存的就是市场给的那份）。
func TestManifestFromBodyYAMLPassthrough(t *testing.T) {
	body := []byte("metadata:\n  id: people/basic\n")

	out, err := manifestFromBody(body, "brickkit-market")
	require.NoError(t, err)
	assert.Equal(t, body, out)
}

// 以 { 开头但不是合法 JSON：交给 Manifest 解析器去报带行号的语法错误。
func TestManifestFromBodyBrokenJSONFallsBackToYAML(t *testing.T) {
	body := []byte(`{ this is not json`)

	out, err := manifestFromBody(body, "brickkit-market")
	require.NoError(t, err)
	assert.Equal(t, body, out)
}

func TestManifestFromBodySuccessFalse(t *testing.T) {
	body := []byte(`{"success":false,"error":{"code":"NOT_FOUND","message":"组件版本不存在"}}`)

	_, err := manifestFromBody(body, "brickkit-market")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeComponentNotFound, e.Code)
	assert.Contains(t, e.Format(), "组件版本不存在")
}

func TestManifestFromBodyNullData(t *testing.T) {
	body := []byte(`{"success":true,"data":null}`)

	_, err := manifestFromBody(body, "brickkit-market")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeManifestInvalid, clierr.As(err).Code)
}

func TestEnvelopeError(t *testing.T) {
	assert.Equal(t, "组件不存在", envelopeError(map[string]any{
		"error": map[string]any{"message": "组件不存在"},
	}))
	assert.Equal(t, "顶层消息", envelopeError(map[string]any{"message": "顶层消息"}))
	assert.Equal(t, "市场未说明原因", envelopeError(map[string]any{}))
	assert.Equal(t, "市场未说明原因", envelopeError(map[string]any{
		"error": map[string]any{"code": "X"},
	}), "只有 code 没有 message 时也要有兜底文案")
}

// ============================================================
// 产物列表解析与匹配
// ============================================================

func TestDecodeArtifactListEnvelope(t *testing.T) {
	list, err := decodeArtifactList([]byte(
		`{"success":true,"data":[{"id":"art-0","type":"api-contract","format":"protobuf","files":["a.proto"]}]}`),
		"brickkit-market")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "art-0", list[0].ID)
	assert.Equal(t, []string{"a.proto"}, list[0].Files)
}

func TestDecodeArtifactListBareArray(t *testing.T) {
	list, err := decodeArtifactList([]byte(`[{"id":"art-0","type":"api-docs","files":["openapi.json"]}]`),
		"brickkit-market")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "api-docs", list[0].Type)
}

func TestDecodeArtifactListGarbage(t *testing.T) {
	_, err := decodeArtifactList([]byte("<html>502</html>"), "brickkit-market")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeNetworkUnreachable, clierr.As(err).Code)
}

func TestFindArtifact(t *testing.T) {
	list := []marketArtifact{
		{ID: "art-0", Type: "api-contract", Format: "protobuf", Files: []string{"a.proto"}},
		{ID: "art-1", Type: "api-docs", Format: "openapi", Files: []string{"openapi.json"}},
		{ID: "art-2", Type: "api-contract", Format: "graphql", Files: []string{"schema.graphql"}},
	}

	got, ok := findArtifact(list, manifest.Artifact{Type: "api-docs", Format: "openapi"}, "openapi.json")
	require.True(t, ok)
	assert.Equal(t, "art-1", got.ID)

	// format 不同则不匹配（同一 type 下可以有多种格式的契约）
	_, ok = findArtifact(list, manifest.Artifact{Type: "api-contract", Format: "graphql"}, "a.proto")
	assert.False(t, ok)

	// Manifest 未写 format 时只按 type + 文件名匹配
	got, ok = findArtifact(list, manifest.Artifact{Type: "api-contract"}, "schema.graphql")
	require.True(t, ok)
	assert.Equal(t, "art-2", got.ID)

	_, ok = findArtifact(list, manifest.Artifact{Type: "sdk"}, "a.proto")
	assert.False(t, ok)
}

// 市场列表里没有该文件时是"没有"，走产物警告而不是致命错误。
func TestMarketArtifactFileNotListed(t *testing.T) {
	spec := componentSpec{
		ID: "people/basic", Version: "1.0.0",
		Artifacts: []artifactSpec{{Type: "api-docs", Format: "openapi", Files: []string{"openapi.json"}}},
		Files:     map[string]string{"openapi.json": "{}"},
	}
	mock := newMarketMock(t, spec)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)

	// 构造一个市场并未登记的产物文件
	got.Manifest.Artifacts = []manifest.Artifact{
		{Type: "api-docs", Format: "openapi", Files: []string{"missing.json"}},
	}
	res, err := c.DownloadArtifacts(context.Background(), got.Manifest)
	require.NoError(t, err)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0].Format(), "所有安装源中都没有该产物文件")
}

// ============================================================
// 请求与 Token
// ============================================================

// 市场返回 500 等异常状态：报错但不当成"组件不存在"。
func TestMarketUnexpectedStatus(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mock.failManifest = true

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeNetworkUnreachable, e.Code)
	assert.Contains(t, e.Format(), "500")
}

// url 非法时给出配置错误，而不是把 net/url 的报错直接抛给用户。
func TestMarketInvalidURL(t *testing.T) {
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: "://not a url",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "brickkit-market")
}

// 凭据属于另一个市场时不使用，回落到 brickkit.yaml 的 authToken。
func TestMarketIgnoresCredentialsOfOtherMarket(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mock.token = "tok-from-yaml"

	layout := newProject(t)
	writeFile(t, layout.CredentialsPath(),
		`{"marketUrl":"https://another-market.example.com/api/v1","token":"tok-other","expiresAt":"2099-01-01T00:00:00Z"}`)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket,
		URL: mock.URL(), AuthToken: "tok-from-yaml",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok-from-yaml", mock.recorded()[0].Auth)
}

// 凭据文件损坏时报错，且不会退化成"匿名请求"。
func TestMarketBrokenCredentials(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	layout := newProject(t)
	writeFile(t, layout.CredentialsPath(), "{{{")

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeAuthFailed, clierr.As(err).Code)
	assert.Empty(t, mock.recorded(), "Token 解析失败时不应发出请求")
}

// 无凭据、无 authToken 时不带 Authorization 头（public 组件可匿名安装）。
func TestMarketAnonymousRequest(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Empty(t, mock.recorded()[0].Auth)
}

// Token 过期的判定用注入的时钟，不依赖真实时间。
func TestMarketTokenExpiryUsesInjectedClock(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	layout := newProject(t)
	writeFile(t, layout.CredentialsPath(),
		`{"marketUrl":"`+mock.URL()+`","token":"tok","expiresAt":"2026-09-08T10:00:00Z"}`)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{Now: at("2026-09-08T10:00:01Z")})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeTokenExpired, e.Code)
	assert.Contains(t, e.Format(), "2026-09-08T10:00:00Z")
}

func TestNetworkReason(t *testing.T) {
	inner := errors.New("connection refused")
	assert.Equal(t, "connection refused", networkReason(&url.Error{
		Op: "Get", URL: "http://x", Err: inner,
	}))
	assert.Equal(t, "boom", networkReason(errors.New("boom")))
}

// ============================================================
// 市场错误信封（P18 回填）
// ============================================================

// 市场把版本下架时返回 403 + COMPONENT_BLOCKED。
//
// 这条用例来自 18-D 的真实端到端验证：当时 CLI 只看状态码，把 403 一律说成
// "市场认证失败，请执行 brickkit login"——而登录并不能让被下架的组件变回可安装，
// 使用者会一直在错误的方向上折腾。
func TestMarketBlockedVersionIsNotReportedAsAuthFailure(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mock.blocked = true

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")

	require.Error(t, err)
	e := clierr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, clierr.CodeComponentBlocked, e.Code)

	rendered := e.Format()
	assert.Contains(t, rendered, "已被市场下架", "要把市场给的原因说出来：%s", rendered)
	assert.NotContains(t, rendered, "brickkit login", "下架与登录无关，别把人引到错误的方向")
}

// 真正的认证失败仍然要提示登录。
func TestMarketUnauthorizedStillAsksToLogin(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mock.token = "the-right-token"

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")

	require.Error(t, err)
	e := clierr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, clierr.CodeAuthRequired, e.Code)
	assert.Contains(t, e.Format(), "brickkit login")
}
