// 本文件是 Step 18-D HTTP 层的代码级测试：协议细节与异常路径。
//
// 业务规则的测试在 handler_test.go；这里只管"协议层自己会不会出错"。
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
)

// Authorization 头的 scheme 按 RFC 7235 是大小写不敏感的。
func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		f.server.URL+"/api/v1/components/people/basic/versions",
		bytes.NewReader(mustJSON(t, publishBody(t, "people/basic", "1.0.0", nil))))
	require.NoError(t, err)
	req.Header.Set("Authorization", "bearer "+token)

	resp, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

// 不是 Bearer 的认证头当作没带令牌处理（匿名），而不是当成一个叫
// "Basic xxx" 的令牌去查库。
func TestNonBearerAuthorizationIsTreatedAsAnonymous(t *testing.T) {
	f := newFixture(t)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		f.server.URL+"/api/v1/components", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Basic YWxpY2U6cHc=")

	resp, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "public 组件的查询不需要认证")
}

// 只写了 "Bearer" 没写令牌，同样按匿名处理。
func TestEmptyBearerIsAnonymous(t *testing.T) {
	f := newFixture(t)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		f.server.URL+"/api/v1/components", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer")

	resp, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// 路径末尾多一个斜杠不该 404：探针与 curl 都很容易多打一个。
func TestTrailingSlashStillMatches(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/health/", "", nil)

	assert.Equal(t, http.StatusOK, resp.status)
}

// 405 要带上 Allow 头，告诉调用方这个地址支持什么方法。
func TestMethodNotAllowedCarriesAllowHeader(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodPatch, "/api/v1/components/people/basic/versions", "", nil)

	require.Equal(t, http.StatusMethodNotAllowed, resp.status)
	allow := resp.header.Get("Allow")
	assert.Contains(t, allow, http.MethodGet)
	assert.Contains(t, allow, http.MethodPost)
}

// 根路径不是接口，也要走 JSON 信封。
func TestRootPathReturnsJSONNotFound(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/", "", nil)

	require.Equal(t, http.StatusNotFound, resp.status)
	assert.Contains(t, resp.header.Get("Content-Type"), "application/json")
	assert.Equal(t, model.CodeNotFound, resp.Error.Code)
}

// 组件 ID 多于两段时不匹配任何路由（002 §10.3 只有 scope/name 两段）。
func TestThreeSegmentComponentIDIsNotFound(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/components/a/b/c", "", nil)

	assert.Equal(t, http.StatusNotFound, resp.status)
}

// 空请求体的 PUT 也要给 400，而不是把空对象当成合法请求。
func TestPutWithEmptyBodyReturns400(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut,
		f.server.URL+"/api/v1/components/people/basic/visibility", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// 产物记录存在但文件还没上传时是 404，不是 500：
// draft 版本正处在"已登记、待上传"的中间态。
func TestDownloadBeforeUploadReturns404(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	body := publishBody(t, "people/basic", "1.0.0", protoArtifact())
	body["status"] = model.VersionDraft
	created := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", token, body)
	require.Equal(t, http.StatusCreated, created.status, "响应：%s", created.body)

	records, err := f.repo.ListArtifacts(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	require.NotEmpty(t, records)

	resp := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+records[0].ArtifactID+
			"/download?file="+url.QueryEscape(records[0].Files[0]), token, nil)

	require.Equal(t, http.StatusNotFound, resp.status, "响应：%s", resp.body)
	assert.Equal(t, model.CodeNotFound, resp.Error.Code)
}

// 未知的 artifactId 是 404。
func TestDownloadUnknownArtifactReturns404(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/does-not-exist/download?file=x.json", "", nil)

	assert.Equal(t, http.StatusNotFound, resp.status)
}

// 下载不带 ?file= 时给 400 并说明原因。
func TestDownloadWithoutFileParamReturns400(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	records := f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+records[0].ArtifactID+"/download", "", nil)

	require.Equal(t, http.StatusBadRequest, resp.status)
	assert.Contains(t, resp.Error.Message, "file")
}

// 产物文件的 Content-Type 按扩展名给，别让浏览器把 JSON 当成下载流。
func TestDownloadSetsContentTypeByExtension(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	records := f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	byType := map[string]string{}
	for _, rec := range records {
		byType[rec.Type] = rec.ArtifactID
	}

	docs := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+byType["api-docs"]+"/download?file=openapi.json",
		"", nil)
	assert.Contains(t, docs.header.Get("Content-Type"), "application/json")

	proto := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+byType["api-contract"]+
			"/download?file="+url.QueryEscape("proto/people/v1/people.proto"), "", nil)
	assert.Contains(t, proto.header.Get("Content-Type"), "text/plain")
}

// 超过大小上限的上传要给 400 并说明是"文件太大"，
// 而不是把 "request body too large" 当成内部错误抛 500。
func TestUploadTooLargeReturns400(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	records := f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	huge := strings.Repeat("x", (64<<20)+1024)
	resp := f.do(t, http.MethodPost,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+records[0].ArtifactID+
			"/upload?file="+url.QueryEscape(records[0].Files[0]), token, huge)

	require.Equal(t, http.StatusBadRequest, resp.status, "响应：%s", resp.body)
	assert.Contains(t, resp.Error.Message, "上限")
}

// 搜索结果里不能带出别人的所有权信息以外的敏感字段——
// 这里锁住 items 的字段集合不要悄悄变宽。
func TestSearchItemsCarryNoSecrets(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components", "", nil)
	require.Equal(t, http.StatusOK, resp.status)

	assert.NotContains(t, string(resp.body), "passwordHash")
	assert.NotContains(t, string(resp.body), "manifest",
		"列表页不需要 Manifest 全文，带上只是白白撑大响应")
}

// 详情响应是平铺的（007 §4.3），不是 {"component": {...}} 的嵌套形状。
func TestComponentDetailIsFlat(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components/people/basic", "", nil)

	var data map[string]any
	resp.decode(t, &data)
	assert.NotContains(t, data, "component", "详情不该多包一层")
	assert.Contains(t, data, "componentId")
}

// 版本列表不带 Manifest 全文（列表页用不上，它是响应里最大的字段）。
func TestVersionListOmitsManifest(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components/people/basic/versions", "", nil)

	require.Equal(t, http.StatusOK, resp.status)
	assert.NotContains(t, string(resp.body), "apiVersion", "响应：%s", resp.body)
}

// 空的访问策略与空的审计结果都返回 []，不返回 null。
func TestEmptyListsAreArraysNotNull(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	access := f.do(t, http.MethodGet, "/api/v1/components/people/basic/access", token, nil)
	require.Equal(t, http.StatusOK, access.status)
	assert.Contains(t, string(access.body), `"data":[]`, "响应：%s", access.body)

	audit := f.do(t, http.MethodGet, "/api/v1/audit?componentId=nobody/here", token, nil)
	require.Equal(t, http.StatusOK, audit.status)
	assert.Contains(t, string(audit.body), `"data":[]`, "响应：%s", audit.body)
}

// 所有响应都是 UTF-8 JSON：市场的错误信息是中文的，
// 没声明字符集的话终端与浏览器都可能显示成乱码。
func TestResponsesDeclareUTF8(t *testing.T) {
	f := newFixture(t)

	ok := f.do(t, http.MethodGet, "/api/v1/health", "", nil)
	assert.Contains(t, ok.header.Get("Content-Type"), "charset=utf-8")

	bad := f.do(t, http.MethodGet, "/api/v1/components/nobody/here", "", nil)
	assert.Contains(t, bad.header.Get("Content-Type"), "charset=utf-8")
	assert.Contains(t, string(bad.body), "组件", "中文原样返回：%s", bad.body)
}

// 上传成功后再上传同一个文件是覆盖，不是报错：发布失败重试很常见。
func TestReUploadOverwrites(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	records := f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	var docsID string
	for _, rec := range records {
		if rec.Type == "api-docs" {
			docsID = rec.ArtifactID
		}
	}

	up := f.do(t, http.MethodPost,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+docsID+"/upload?file=openapi.json",
		token, `{"openapi":"3.1.0"}`)
	require.Equal(t, http.StatusOK, up.status, "响应：%s", up.body)

	got := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+docsID+"/download?file=openapi.json", "", nil)
	assert.JSONEq(t, `{"openapi":"3.1.0"}`, string(got.body))
}

// 注册请求体里多带的字段被忽略，不该因此失败。
func TestRegisterIgnoresUnknownFields(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username": "alice", "password": testPassword, "isAdmin": true, "nonsense": 42,
	})

	require.Equal(t, http.StatusCreated, resp.status, "响应：%s", resp.body)

	// 关键：客户端不能通过请求体给自己发管理员权限
	token, err := f.svc.Login(context.Background(), "alice", testPassword)
	require.NoError(t, err)
	id, err := f.svc.Authenticate(context.Background(), token.Token)
	require.NoError(t, err)
	assert.False(t, id.IsAdmin, "isAdmin 不是注册请求能设置的字段")
}

// 没带令牌时注销是空操作，不该报错。
func TestLogoutWithoutTokenIsNoop(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodPost, "/api/v1/auth/logout", "", nil)

	assert.Equal(t, http.StatusOK, resp.status)
}

// 令牌无效时每个端点都必须是 401。
//
// 最危险的写法是把"解析不出身份"当成匿名继续往下走：那样一个过期令牌会被
// 悄悄降级成匿名调用，使用者看到的是"组件不存在"而不是"请重新登录"。
func TestInvalidTokenIsRejectedByEveryEndpoint(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	f.publish(t, owner, "people/basic", "1.0.0")

	const bogus = "expired-or-forged-token"
	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"搜索", http.MethodGet, "/api/v1/components", nil},
		{"详情", http.MethodGet, "/api/v1/components/people/basic", nil},
		{"版本列表", http.MethodGet, "/api/v1/components/people/basic/versions", nil},
		{"Manifest", http.MethodGet, versionPath("people/basic", "1.0.0") + "/manifest", nil},
		{"产物列表", http.MethodGet, versionPath("people/basic", "1.0.0") + "/artifacts", nil},
		{"下载", http.MethodGet, versionPath("people/basic", "1.0.0") + "/artifacts/art-0/download?file=x", nil},
		{"发布", http.MethodPost, "/api/v1/components/people/basic/versions",
			publishBody(t, "people/basic", "2.0.0", nil)},
		{"上传", http.MethodPost, versionPath("people/basic", "1.0.0") + "/artifacts/art-0/upload?file=x", "data"},
		{"改状态", http.MethodPut, versionPath("people/basic", "1.0.0"), map[string]any{"status": "deprecated"}},
		{"删除版本", http.MethodDelete, versionPath("people/basic", "1.0.0"), nil},
		{"改可见性", http.MethodPut, "/api/v1/components/people/basic/visibility",
			map[string]any{"visibility": model.VisibilityPrivate}},
		{"查访问策略", http.MethodGet, "/api/v1/components/people/basic/access", nil},
		{"改访问策略", http.MethodPut, "/api/v1/components/people/basic/access",
			map[string]any{"policies": []any{}}},
		{"审计", http.MethodGet, "/api/v1/audit", nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := f.do(t, c.method, c.path, bogus, c.body)

			require.Equal(t, http.StatusUnauthorized, resp.status, "响应：%s", resp.body)
			require.NotNil(t, resp.Error)
			assert.Equal(t, model.CodeUnauthorized, resp.Error.Code)
		})
	}
}

// 请求体不是 JSON 时，所有写接口都要给 400 且说明原因。
func TestMalformedBodyReturns400OnEveryWriteEndpoint(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"发布", http.MethodPost, "/api/v1/components/people/basic/versions"},
		{"改状态", http.MethodPut, versionPath("people/basic", "1.0.0")},
		{"改可见性", http.MethodPut, "/api/v1/components/people/basic/visibility"},
		{"改访问策略", http.MethodPut, "/api/v1/components/people/basic/access"},
		{"注册", http.MethodPost, "/api/v1/auth/register"},
		{"登录", http.MethodPost, "/api/v1/auth/login"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), c.method,
				f.server.URL+c.path, strings.NewReader("{ 半截 json"))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			resp, err := f.server.Client().Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}
