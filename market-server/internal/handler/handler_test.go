// 本文件是 Step 18-D「HTTP 层」的业务行为测试。
//
// 覆盖开发计划 18.1–18.5、18.15、18.16、18.23，以及 007 §9（API 设计）
// 定义的路径与响应形状。
//
// 另外回填 P11：CLI 侧的市场安装源（internal/source/market.go）在 Step 6
// 只对着 Mock 验证过 D47（Manifest 信封）与 D48（产物列表 + ?file= 下载）。
// 本文件把那两条契约写成服务端的验收测试——响应形状一旦漂移，这里就红。
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/brickkit/market-server/internal/handler"
	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
	"github.com/brickkit/market-server/internal/service"
	"github.com/brickkit/market-server/internal/storage"
)

// ============================================================
// 测试夹具
// ============================================================

const testPassword = "correct-horse-battery"

type fixture struct {
	server *httptest.Server
	svc    *service.Service
	repo   repo.Repository
	store  *storage.Memory
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	r := repo.NewMemory()
	store := storage.NewMemory()
	svc := service.New(r, store, service.Options{BcryptCost: bcrypt.MinCost})
	// 访问日志走 t.Logf：只有失败的用例才会把它打出来
	srv := httptest.NewServer(handler.New(svc, handler.Options{Version: "test", Logf: t.Logf}))
	t.Cleanup(srv.Close)

	return &fixture{server: srv, svc: svc, repo: r, store: store}
}

// login 注册并登录一个用户，返回 Bearer Token。
func (f *fixture) login(t *testing.T, username string) string {
	t.Helper()
	ctx := context.Background()

	_, err := f.svc.Register(ctx, service.RegisterRequest{Username: username, Password: testPassword})
	require.NoError(t, err)
	token, err := f.svc.Login(ctx, username, testPassword)
	require.NoError(t, err)
	return token.Token
}

// loginAdmin 走管理员引导（运维指南 §6.5）造一个管理员并返回其 Token。
func (f *fixture) loginAdmin(t *testing.T, username string) string {
	t.Helper()

	require.NoError(t, f.svc.EnsureAdmin(context.Background(), username, testPassword))
	token, err := f.svc.Login(context.Background(), username, testPassword)
	require.NoError(t, err)
	return token.Token
}

// response 是一次 HTTP 调用的结果。
type response struct {
	status  int
	header  http.Header
	body    []byte
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *model.APIError `json:"error"`
}

// do 发起一次请求。token 为空表示匿名调用。
func (f *fixture) do(t *testing.T, method, path, token string, body any) *response {
	t.Helper()

	var reader io.Reader
	contentType := ""
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
		contentType = "application/octet-stream"
	case []byte:
		reader = bytes.NewReader(b)
		contentType = "application/octet-stream"
	default:
		raw, err := json.Marshal(b)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
		contentType = "application/json"
	}

	req, err := http.NewRequestWithContext(context.Background(), method, f.server.URL+path, reader)
	require.NoError(t, err)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	out := &response{status: resp.StatusCode, header: resp.Header, body: raw}
	// 产物下载返回的是文件本身，不是 JSON 信封，解析失败不算错误
	_ = json.Unmarshal(raw, out)
	return out
}

// decode 把 data 解成目标结构。
func (r *response) decode(t *testing.T, target any) {
	t.Helper()
	require.NotEmpty(t, r.Data, "响应里没有 data：%s", r.body)
	require.NoError(t, json.Unmarshal(r.Data, target))
}

func manifestOf(t *testing.T, componentID, version string, artifacts []any) map[string]any {
	t.Helper()
	doc := map[string]any{
		"apiVersion": "brickkit/v1",
		"kind":       "Component",
		"metadata": map[string]any{
			"id": componentID, "name": "组件 " + componentID,
			"version": version, "description": "描述",
		},
		"tags": []string{"demo"},
		"deployment": map[string]any{
			"type": "container", "image": "registry.example.com/x:" + version, "port": 8080,
		},
		"healthCheck": map[string]any{"type": "http", "path": "/healthz"},
	}
	if artifacts != nil {
		doc["artifacts"] = artifacts
	}
	return doc
}

func publishBody(t *testing.T, componentID, version string, artifacts []any) map[string]any {
	t.Helper()
	return map[string]any{
		"version":    version,
		"status":     model.VersionStable,
		"manifest":   manifestOf(t, componentID, version, artifacts),
		"sourceType": model.SourceTypeGit,
		"gitUrl":     "https://github.com/brickkit/demo.git",
	}
}

func versionPath(componentID, version string) string {
	return "/api/v1/components/" + componentID + "/versions/" + version
}

// publish 走 HTTP 发布一个版本（无文件产物，直接 stable）。
func (f *fixture) publish(t *testing.T, token, componentID, version string) {
	t.Helper()
	resp := f.do(t, http.MethodPost, "/api/v1/components/"+componentID+"/versions", token,
		publishBody(t, componentID, version, nil))
	require.Equal(t, http.StatusCreated, resp.status, "发布失败：%s", resp.body)
}

// ============================================================
// 18.23 健康检查
// ============================================================

// 健康检查是 compose 的 healthcheck 探针（运维指南 §4），必须匿名可访问。
func TestHealthEndpointIsPublic(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/health", "", nil)

	require.Equal(t, http.StatusOK, resp.status)
	assert.True(t, resp.Success)

	var data struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	resp.decode(t, &data)
	assert.Equal(t, "ok", data.Status)
	assert.Equal(t, "test", data.Version, "健康检查应回显构建版本，便于确认部署的是哪个镜像")
}

// ============================================================
// 18.1 组件发布
// ============================================================

func TestPublishReturns201(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	resp := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", token,
		publishBody(t, "people/basic", "1.0.0", nil))

	require.Equal(t, http.StatusCreated, resp.status, "响应：%s", resp.body)
	assert.True(t, resp.Success)

	var v model.Version
	resp.decode(t, &v)
	assert.Equal(t, "people/basic", v.ComponentID)
	assert.Equal(t, "1.0.0", v.Version)
	assert.Equal(t, model.VersionStable, v.Status)
	assert.Equal(t, "alice", v.PublishedBy)
	assert.False(t, v.PublishedAt.IsZero())
}

// 发布必须认证（007 §9.6）。
func TestPublishWithoutTokenReturns401(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", "",
		publishBody(t, "people/basic", "1.0.0", nil))

	require.Equal(t, http.StatusUnauthorized, resp.status)
	require.False(t, resp.Success)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeUnauthorized, resp.Error.Code)
}

// 令牌无效与令牌缺失是两回事：前者要明确说"重新登录"。
func TestPublishWithBogusTokenReturns401(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", "not-a-real-token",
		publishBody(t, "people/basic", "1.0.0", nil))

	require.Equal(t, http.StatusUnauthorized, resp.status)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "令牌")
}

// URL 里的组件 ID 与 Manifest 里的必须一致，否则组件会被发布到错误的位置。
func TestPublishRejectsComponentIDMismatch(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	resp := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", token,
		publishBody(t, "other/thing", "1.0.0", nil))

	require.Equal(t, http.StatusBadRequest, resp.status, "响应：%s", resp.body)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeInvalidRequest, resp.Error.Code)
}

// 18.6 / 18.7 在服务层已覆盖；这里确认校验错误经 HTTP 出去时详情不丢。
func TestPublishReservedVariableConflictKeepsDetails(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	doc := manifestOf(t, "people/basic", "1.0.0", nil)
	doc["configSchema"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"databaseUrl": map[string]any{"type": "string", "description": "数据库地址"},
		},
	}
	body := publishBody(t, "people/basic", "1.0.0", nil)
	body["manifest"] = doc

	resp := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", token, body)

	require.Equal(t, http.StatusBadRequest, resp.status, "响应：%s", resp.body)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeReservedVariableConflict, resp.Error.Code)
	assert.Contains(t, string(resp.body), "conflicts", "冲突详情必须随响应返回（18.7）")
}

// 18.14 在 HTTP 层的表现：409，不是 500。
func TestPublishDuplicateVersionReturns409(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", token,
		publishBody(t, "people/basic", "1.0.0", nil))

	require.Equal(t, http.StatusConflict, resp.status, "响应：%s", resp.body)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeVersionExists, resp.Error.Code)
}

// 请求体不是合法 JSON 时要给 400 与人能看懂的原因，而不是 500。
func TestPublishRejectsMalformedJSON(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		f.server.URL+"/api/v1/components/people/basic/versions", strings.NewReader("{ this is not json"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ============================================================
// 18.2 版本列表
// ============================================================

func TestListVersionsReturnsNewestFirst(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")
	f.publish(t, token, "people/basic", "1.10.0")
	f.publish(t, token, "people/basic", "1.2.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components/people/basic/versions", "", nil)

	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)
	var versions []model.Version
	resp.decode(t, &versions)

	got := make([]string, 0, len(versions))
	for _, v := range versions {
		got = append(got, v.Version)
	}
	// 语义化版本排序：1.10.0 > 1.2.0，不是字符串序
	assert.Equal(t, []string{"1.10.0", "1.2.0", "1.0.0"}, got)
	assert.Equal(t, model.VersionStable, versions[0].Status)
}

func TestListVersionsOfUnknownComponentReturns404(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/components/nobody/here/versions", "", nil)

	require.Equal(t, http.StatusNotFound, resp.status)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeNotFound, resp.Error.Code)
}

// ============================================================
// 18.3 获取 Manifest —— 回填 P11 / D47
// ============================================================

// CLI 的 manifestFromBody 从 data.manifest 取 Manifest，
// 从 data.sourceType / data.gitUrl 判断开源还是闭源（--repo 依赖它）。
func TestManifestEndpointMatchesCLIEnvelopeContract(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.2.0")

	resp := f.do(t, http.MethodGet, versionPath("people/basic", "1.2.0")+"/manifest", "", nil)
	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)
	assert.Contains(t, resp.header.Get("Content-Type"), "application/json")

	// 契约按客户端读取的路径逐层断言
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			ComponentID string         `json:"componentId"`
			Version     string         `json:"version"`
			Status      string         `json:"status"`
			SourceType  string         `json:"sourceType"`
			GitURL      string         `json:"gitUrl"`
			Manifest    map[string]any `json:"manifest"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp.body, &envelope))

	assert.True(t, envelope.Success)
	assert.Equal(t, "people/basic", envelope.Data.ComponentID)
	assert.Equal(t, "1.2.0", envelope.Data.Version)
	assert.Equal(t, model.VersionStable, envelope.Data.Status)
	assert.Equal(t, model.SourceTypeGit, envelope.Data.SourceType)
	assert.Equal(t, "https://github.com/brickkit/demo.git", envelope.Data.GitURL)

	// data.manifest 必须是完整的 component.yaml 结构（CLI 会 yaml.Marshal 后交给 Manifest 解析器）
	require.NotEmpty(t, envelope.Data.Manifest)
	assert.Equal(t, "brickkit/v1", envelope.Data.Manifest["apiVersion"])
	assert.Equal(t, "Component", envelope.Data.Manifest["kind"])
	metadata, ok := envelope.Data.Manifest["metadata"].(map[string]any)
	require.True(t, ok, "manifest.metadata 缺失：%s", resp.body)
	assert.Equal(t, "people/basic", metadata["id"])
	assert.Equal(t, "1.2.0", metadata["version"])
}

// CLI 靠 404 判断"这个源没有该组件"，然后继续尝试下一个安装源（D40）。
// 任何别的状态码都会中断整条源链。
func TestManifestOfUnknownVersionReturns404(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet, versionPath("people/basic", "9.9.9")+"/manifest", "", nil)

	require.Equal(t, http.StatusNotFound, resp.status)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeNotFound, resp.Error.Code)
}

// ============================================================
// 18.4 / 18.5 产物上传与下载 —— 回填 P11 / D48
// ============================================================

// protoArtifact 是一份带文件的产物声明。
func protoArtifact() []any {
	return []any{
		map[string]any{
			"type": "api-contract", "format": "protobuf",
			"description": "gRPC 契约", "files": []string{"proto/people/v1/people.proto"},
		},
		map[string]any{
			"type": "api-docs", "format": "openapi",
			"description": "HTTP 文档", "files": []string{"openapi.json"},
		},
	}
}

const protoContent = "syntax = \"proto3\";\npackage people.v1;\n"

// publishWithArtifacts 发布一个带文件产物的版本（先 draft，上传后转 stable）。
func (f *fixture) publishWithArtifacts(t *testing.T, token, componentID, version string) []model.ArtifactRecord {
	t.Helper()

	body := publishBody(t, componentID, version, protoArtifact())
	body["status"] = model.VersionDraft
	resp := f.do(t, http.MethodPost, "/api/v1/components/"+componentID+"/versions", token, body)
	require.Equal(t, http.StatusCreated, resp.status, "发布 draft 失败：%s", resp.body)

	list := f.do(t, http.MethodGet, versionPath(componentID, version)+"/artifacts", token, nil)
	require.Equal(t, http.StatusOK, list.status, "响应：%s", list.body)
	var records []model.ArtifactRecord
	list.decode(t, &records)

	for _, rec := range records {
		for _, file := range rec.Files {
			content := protoContent
			if strings.HasSuffix(file, ".json") {
				content = `{"openapi":"3.0.0"}`
			}
			up := f.do(t, http.MethodPost,
				versionPath(componentID, version)+"/artifacts/"+rec.ArtifactID+"/upload?file="+url.QueryEscape(file),
				token, content)
			require.Equal(t, http.StatusOK, up.status, "上传 %s 失败：%s", file, up.body)
		}
	}

	promote := f.do(t, http.MethodPut, versionPath(componentID, version), token,
		map[string]any{"status": model.VersionStable})
	require.Equal(t, http.StatusOK, promote.status, "转 stable 失败：%s", promote.body)
	return records
}

// 18.4：上传的文件真的落到了对象存储里，键上带组件 ID 与版本。
func TestUploadArtifactStoresObjectUnderVersionPrefix(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	keys, err := f.store.List(context.Background(), storage.VersionPrefix("people/basic", "1.0.0"))
	require.NoError(t, err)
	assert.Contains(t, keys,
		storage.ObjectKey("people/basic", "1.0.0", "api-contract", "proto/people/v1/people.proto"))
	assert.Contains(t, keys,
		storage.ObjectKey("people/basic", "1.0.0", "api-docs", "openapi.json"))
}

// 上传只有所有者能做：别人不能往你的版本里塞文件。
func TestUploadArtifactByOtherUserReturns403(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	f.publishWithArtifacts(t, owner, "people/basic", "1.0.0")
	other := f.login(t, "bob")

	records, err := f.repo.ListArtifacts(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	require.NotEmpty(t, records)

	resp := f.do(t, http.MethodPost,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+records[0].ArtifactID+
			"/upload?file="+url.QueryEscape(records[0].Files[0]),
		other, "malicious")

	require.Equal(t, http.StatusForbidden, resp.status, "响应：%s", resp.body)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeForbidden, resp.Error.Code)
}

// 不带 ?file= 时无法知道传的是哪个文件，必须拒绝。
func TestUploadArtifactWithoutFileParamReturns400(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	records, err := f.repo.ListArtifacts(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)

	resp := f.do(t, http.MethodPost,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+records[0].ArtifactID+"/upload",
		token, "content")

	require.Equal(t, http.StatusBadRequest, resp.status, "响应：%s", resp.body)
}

// 18.5 + D48：下载端点返回文件正文本身，不是 JSON 信封。
func TestDownloadArtifactReturnsRawFileContent(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	records := f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	var contractID string
	for _, rec := range records {
		if rec.Type == "api-contract" {
			contractID = rec.ArtifactID
		}
	}
	require.NotEmpty(t, contractID)

	resp := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+contractID+
			"/download?file="+url.QueryEscape("proto/people/v1/people.proto"), "", nil)

	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)
	assert.Equal(t, protoContent, string(resp.body), "下载到的必须是文件原文，不能是信封")
	assert.Contains(t, resp.header.Get("Content-Disposition"), "people.proto",
		"应带文件名，便于浏览器与 curl -O 直接落盘")
}

// 同一个产物的不同文件靠 ?file= 区分（D48）。
func TestDownloadArtifactSelectsFileByQueryParam(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	records := f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	var docsID string
	for _, rec := range records {
		if rec.Type == "api-docs" {
			docsID = rec.ArtifactID
		}
	}
	require.NotEmpty(t, docsID)

	resp := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+docsID+"/download?file=openapi.json", "", nil)

	require.Equal(t, http.StatusOK, resp.status)
	assert.JSONEq(t, `{"openapi":"3.0.0"}`, string(resp.body))
}

// 产物没声明该文件时给 404，CLI 才能把它当"这个源没有"继续往下找。
func TestDownloadUndeclaredFileReturns404(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	records := f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet,
		versionPath("people/basic", "1.0.0")+"/artifacts/"+records[0].ArtifactID+
			"/download?file=../../../etc/passwd", "", nil)

	require.Equal(t, http.StatusNotFound, resp.status, "响应：%s", resp.body)
}

// D48：产物列表必须给出 id / type / format / files，CLI 用它把
// Manifest 里的产物声明映射到下载用的 artifactId。
func TestArtifactListMatchesCLIContract(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publishWithArtifacts(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/artifacts", "", nil)
	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)

	var items []struct {
		ID     string   `json:"id"`
		Type   string   `json:"type"`
		Format string   `json:"format"`
		Files  []string `json:"files"`
	}
	resp.decode(t, &items)

	require.Len(t, items, 2)
	byType := map[string]int{}
	for i, item := range items {
		assert.NotEmpty(t, item.ID, "第 %d 条产物缺少 id，CLI 无法拼下载地址", i)
		byType[item.Type] = i
	}

	contract := items[byType["api-contract"]]
	assert.Equal(t, "protobuf", contract.Format)
	assert.Equal(t, []string{"proto/people/v1/people.proto"}, contract.Files)

	docs := items[byType["api-docs"]]
	assert.Equal(t, "openapi", docs.Format)
	assert.Equal(t, []string{"openapi.json"}, docs.Files)
}

// ============================================================
// 18.15 组件搜索
// ============================================================

// 007 §4.2 的响应形状：data.items + data.total。
func TestSearchComponentsReturnsItemsAndTotal(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")
	f.publish(t, token, "department/tree", "1.0.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components", "", nil)
	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)

	var data struct {
		Items []model.Component `json:"items"`
		Total int               `json:"total"`
	}
	resp.decode(t, &data)

	assert.Equal(t, 2, data.Total)
	require.Len(t, data.Items, 2)
	assert.NotEmpty(t, data.Items[0].ComponentID)
	assert.NotEmpty(t, data.Items[0].Visibility)
}

func TestSearchComponentsFiltersByKeyword(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")
	f.publish(t, token, "department/tree", "1.0.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components?keyword=people", "", nil)
	require.Equal(t, http.StatusOK, resp.status)

	var data struct {
		Items []model.Component `json:"items"`
		Total int               `json:"total"`
	}
	resp.decode(t, &data)

	require.Len(t, data.Items, 1)
	assert.Equal(t, "people/basic", data.Items[0].ComponentID)
	assert.Equal(t, 1, data.Total)
}

func TestSearchComponentsFiltersByTags(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	hit := f.do(t, http.MethodGet, "/api/v1/components?tags=demo", "", nil)
	require.Equal(t, http.StatusOK, hit.status)
	var withTag struct {
		Items []model.Component `json:"items"`
	}
	hit.decode(t, &withTag)
	assert.Len(t, withTag.Items, 1)

	miss := f.do(t, http.MethodGet, "/api/v1/components?tags=nonexistent", "", nil)
	require.Equal(t, http.StatusOK, miss.status)
	var without struct {
		Items []model.Component `json:"items"`
		Total int               `json:"total"`
	}
	miss.decode(t, &without)
	assert.Empty(t, without.Items)
	assert.Equal(t, 0, without.Total)
}

// total 是"符合条件的总数"，不是"这一页的条数"——
// 否则前端永远算不出还有几页（007 §4.2）。
func TestSearchTotalCountsBeyondCurrentPage(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")
	f.publish(t, token, "people/extra", "1.0.0")
	f.publish(t, token, "people/more", "1.0.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components?pageSize=2&page=1", "", nil)
	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)

	var first struct {
		Items []model.Component `json:"items"`
		Total int               `json:"total"`
	}
	resp.decode(t, &first)
	assert.Len(t, first.Items, 2, "每页 2 条")
	assert.Equal(t, 3, first.Total, "total 必须是总数")

	second := f.do(t, http.MethodGet, "/api/v1/components?pageSize=2&page=2", "", nil)
	require.Equal(t, http.StatusOK, second.status)
	var page2 struct {
		Items []model.Component `json:"items"`
		Total int               `json:"total"`
	}
	second.decode(t, &page2)
	assert.Len(t, page2.Items, 1, "第二页剩 1 条")
	assert.Equal(t, 3, page2.Total)
	assert.NotEqual(t, first.Items[0].ComponentID, page2.Items[0].ComponentID, "翻页不能重复")
}

// 分页参数不合法时不该 500，按默认值处理即可。
func TestSearchComponentsIgnoresGarbagePaging(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components?page=abc&pageSize=-5", "", nil)

	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)
	var data struct {
		Items []model.Component `json:"items"`
	}
	resp.decode(t, &data)
	assert.Len(t, data.Items, 1)
}

// items 永远是数组：null 会让弱类型客户端崩在遍历上。
func TestSearchWithNoMatchesReturnsEmptyArrayNotNull(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/components?keyword=nothing-here", "", nil)

	require.Equal(t, http.StatusOK, resp.status)
	assert.Contains(t, string(resp.body), `"items":[]`, "响应：%s", resp.body)
}

// ============================================================
// 18.16 组件详情
// ============================================================

// 007 §4.3：详情页要一次给出组件元数据 + 版本列表 + 最新版本。
func TestComponentDetailReturnsMetadataAndVersions(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")
	f.publish(t, token, "people/basic", "1.2.0")

	resp := f.do(t, http.MethodGet, "/api/v1/components/people/basic", "", nil)
	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)

	var data struct {
		ComponentID   string   `json:"componentId"`
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		Visibility    string   `json:"visibility"`
		SourceType    string   `json:"sourceType"`
		GitURL        string   `json:"gitUrl"`
		Tags          []string `json:"tags"`
		LatestVersion string   `json:"latestVersion"`
		Versions      []string `json:"versions"`
		Downloads     int64    `json:"downloads"`
	}
	resp.decode(t, &data)

	assert.Equal(t, "people/basic", data.ComponentID)
	assert.Equal(t, "组件 people/basic", data.Name)
	assert.Equal(t, "描述", data.Description)
	assert.Equal(t, model.VisibilityPublic, data.Visibility)
	assert.Equal(t, model.SourceTypeGit, data.SourceType)
	assert.Equal(t, "https://github.com/brickkit/demo.git", data.GitURL)
	assert.Equal(t, []string{"demo"}, data.Tags)
	assert.Equal(t, "1.2.0", data.LatestVersion)
	assert.Equal(t, []string{"1.2.0", "1.0.0"}, data.Versions)
}

func TestComponentDetailOfUnknownComponentReturns404(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/components/nobody/here", "", nil)

	require.Equal(t, http.StatusNotFound, resp.status)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeNotFound, resp.Error.Code)
}

// ============================================================
// 18.19 / 18.20 注册与登录
// ============================================================

func TestRegisterEndpointReturns201WithoutPasswordHash(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodPost, "/api/v1/auth/register", "",
		map[string]any{"username": "alice", "password": testPassword, "email": "alice@example.com"})

	require.Equal(t, http.StatusCreated, resp.status, "响应：%s", resp.body)
	var user model.User
	resp.decode(t, &user)
	assert.Equal(t, "alice", user.Username)
	assert.NotEmpty(t, user.UserID)
	assert.NotContains(t, string(resp.body), "passwordHash", "响应绝不能带出密码哈希")
	assert.NotContains(t, string(resp.body), testPassword)
}

func TestRegisterWithWeakPasswordReturns400(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodPost, "/api/v1/auth/register", "",
		map[string]any{"username": "alice", "password": "123"})

	require.Equal(t, http.StatusBadRequest, resp.status)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeInvalidRequest, resp.Error.Code)
}

func TestRegisterDuplicateUsernameReturns409(t *testing.T) {
	f := newFixture(t)
	f.login(t, "alice")

	resp := f.do(t, http.MethodPost, "/api/v1/auth/register", "",
		map[string]any{"username": "alice", "password": testPassword})

	require.Equal(t, http.StatusConflict, resp.status, "响应：%s", resp.body)
	assert.Equal(t, model.CodeConflict, resp.Error.Code)
}

// 登录返回的 Token 与过期时间，就是 CLI 写进 .brickkit/credentials 的内容（004 §5.3）。
func TestLoginEndpointReturnsTokenAndExpiry(t *testing.T) {
	f := newFixture(t)
	f.login(t, "alice")

	resp := f.do(t, http.MethodPost, "/api/v1/auth/login", "",
		map[string]any{"username": "alice", "password": testPassword})

	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)
	var token model.Token
	resp.decode(t, &token)
	assert.NotEmpty(t, token.Token)
	assert.Equal(t, "alice", token.Username)
	assert.True(t, token.ExpiresAt.After(token.CreatedAt), "必须带未来的过期时间")

	// 拿到的 Token 立刻能用
	publish := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", token.Token,
		publishBody(t, "people/basic", "1.0.0", nil))
	assert.Equal(t, http.StatusCreated, publish.status, "响应：%s", publish.body)
}

func TestLoginWithWrongPasswordReturns401(t *testing.T) {
	f := newFixture(t)
	f.login(t, "alice")

	resp := f.do(t, http.MethodPost, "/api/v1/auth/login", "",
		map[string]any{"username": "alice", "password": "wrong-password"})

	require.Equal(t, http.StatusUnauthorized, resp.status)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeUnauthorized, resp.Error.Code)
}

// 用户不存在与密码错误返回同样的响应：登录接口不能当用户名枚举器用。
func TestLoginWithUnknownUserLooksIdenticalToWrongPassword(t *testing.T) {
	f := newFixture(t)
	f.login(t, "alice")

	wrong := f.do(t, http.MethodPost, "/api/v1/auth/login", "",
		map[string]any{"username": "alice", "password": "wrong-password"})
	unknown := f.do(t, http.MethodPost, "/api/v1/auth/login", "",
		map[string]any{"username": "nobody", "password": "wrong-password"})

	assert.Equal(t, wrong.status, unknown.status)
	assert.JSONEq(t, string(wrong.body), string(unknown.body))
}

func TestLogoutInvalidatesToken(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	out := f.do(t, http.MethodPost, "/api/v1/auth/logout", token, nil)
	require.Equal(t, http.StatusOK, out.status, "响应：%s", out.body)

	after := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", token,
		publishBody(t, "people/basic", "1.0.0", nil))
	assert.Equal(t, http.StatusUnauthorized, after.status, "注销后旧 Token 必须失效")
}

// ============================================================
// 18.21 未认证访问 private 组件
// ============================================================

func TestPrivateComponentIsInvisibleToAnonymous(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut,
		"/api/v1/components/people/basic/visibility", token,
		map[string]any{"visibility": model.VisibilityPrivate}).status)

	manifest := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/manifest", "", nil)
	assert.Equal(t, http.StatusForbidden, manifest.status, "响应：%s", manifest.body)
	assert.Equal(t, model.CodeForbidden, manifest.Error.Code)

	detail := f.do(t, http.MethodGet, "/api/v1/components/people/basic", "", nil)
	assert.Equal(t, http.StatusForbidden, detail.status)

	search := f.do(t, http.MethodGet, "/api/v1/components", "", nil)
	require.Equal(t, http.StatusOK, search.status)
	assert.NotContains(t, string(search.body), "people/basic", "private 组件不能出现在匿名搜索结果里")
}

// 被授权的用户能通过同样的端点访问 private 组件。
func TestPrivateComponentVisibleToGrantedUser(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	f.publish(t, owner, "people/basic", "1.0.0")
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut,
		"/api/v1/components/people/basic/visibility", owner,
		map[string]any{"visibility": model.VisibilityPrivate}).status)

	guest := f.login(t, "bob")
	guestID, err := f.svc.Authenticate(context.Background(), guest)
	require.NoError(t, err)

	denied := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/manifest", guest, nil)
	require.Equal(t, http.StatusForbidden, denied.status, "授权前应当被拒")

	grant := f.do(t, http.MethodPut, "/api/v1/components/people/basic/access", owner,
		map[string]any{"policies": []any{
			map[string]any{"targetType": model.TargetUser, "targetId": guestID.UserID, "permission": "read"},
		}})
	require.Equal(t, http.StatusOK, grant.status, "响应：%s", grant.body)

	allowed := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/manifest", guest, nil)
	assert.Equal(t, http.StatusOK, allowed.status, "响应：%s", allowed.body)
}

// 访问策略里有"谁能看我的私有组件"，只有所有者能查。
func TestAccessPolicyListRequiresOwner(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	f.publish(t, owner, "people/basic", "1.0.0")
	other := f.login(t, "bob")

	mine := f.do(t, http.MethodGet, "/api/v1/components/people/basic/access", owner, nil)
	assert.Equal(t, http.StatusOK, mine.status, "响应：%s", mine.body)

	theirs := f.do(t, http.MethodGet, "/api/v1/components/people/basic/access", other, nil)
	assert.Equal(t, http.StatusForbidden, theirs.status)
}

// ============================================================
// 18.17 / 18.18 状态与可见性变更
// ============================================================

func TestVersionStatusEndpointDeprecates(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodPut, versionPath("people/basic", "1.0.0"), token,
		map[string]any{"status": model.VersionDeprecated})
	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)

	manifest := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/manifest", "", nil)
	require.Equal(t, http.StatusOK, manifest.status, "deprecated 仍然可安装（007 §6.1）")

	var data struct {
		Status string `json:"status"`
	}
	manifest.decode(t, &data)
	assert.Equal(t, model.VersionDeprecated, data.Status)
}

// 007 §6.3：blocked 只有市场管理员能标记。
func TestBlockingVersionRequiresAdmin(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	f.publish(t, owner, "people/basic", "1.0.0")

	byOwner := f.do(t, http.MethodPut, versionPath("people/basic", "1.0.0"), owner,
		map[string]any{"status": model.VersionBlocked, "reason": "发现问题"})
	assert.Equal(t, http.StatusForbidden, byOwner.status, "响应：%s", byOwner.body)

	admin := f.loginAdmin(t, "root")
	byAdmin := f.do(t, http.MethodPut, versionPath("people/basic", "1.0.0"), admin,
		map[string]any{"status": model.VersionBlocked, "reason": "发现恶意行为"})
	assert.Equal(t, http.StatusOK, byAdmin.status, "响应：%s", byAdmin.body)
}

func TestVersionStatusRejectsUnknownStatus(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodPut, versionPath("people/basic", "1.0.0"), token,
		map[string]any{"status": "whatever"})

	require.Equal(t, http.StatusBadRequest, resp.status, "响应：%s", resp.body)
	assert.Equal(t, model.CodeInvalidRequest, resp.Error.Code)
}

func TestVisibilityEndpointRejectsUnknownVisibility(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodPut, "/api/v1/components/people/basic/visibility", token,
		map[string]any{"visibility": "semi-public"})

	require.Equal(t, http.StatusBadRequest, resp.status, "响应：%s", resp.body)
	assert.Equal(t, model.CodeInvalidRequest, resp.Error.Code)
}

func TestVisibilityChangeByOtherUserReturns403(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	f.publish(t, owner, "people/basic", "1.0.0")
	other := f.login(t, "bob")

	resp := f.do(t, http.MethodPut, "/api/v1/components/people/basic/visibility", other,
		map[string]any{"visibility": model.VisibilityPrivate})

	assert.Equal(t, http.StatusForbidden, resp.status, "响应：%s", resp.body)
}

// ============================================================
// 18.24 软删除 / 18.25 blocked 不可安装
// ============================================================

func TestDeleteVersionIsSoftAndVersionNumberStaysTaken(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	del := f.do(t, http.MethodDelete, versionPath("people/basic", "1.0.0"), token, nil)
	require.Equal(t, http.StatusOK, del.status, "响应：%s", del.body)

	// 对外视同不存在
	manifest := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/manifest", token, nil)
	assert.Equal(t, http.StatusNotFound, manifest.status)

	// 版本列表里也不再出现
	list := f.do(t, http.MethodGet, "/api/v1/components/people/basic/versions", token, nil)
	require.Equal(t, http.StatusOK, list.status)
	var versions []model.Version
	list.decode(t, &versions)
	assert.Empty(t, versions)

	// 但版本号不可回收：重新发布同一版本号仍然冲突
	again := f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", token,
		publishBody(t, "people/basic", "1.0.0", nil))
	assert.Equal(t, http.StatusConflict, again.status,
		"删掉的版本号必须继续占位，否则同一个 people/basic@1.0.0 会指向不同内容")
}

// 18.25：blocked 版本要给出明确的错误码，而不是含糊的 404。
func TestBlockedVersionIsNotInstallable(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	f.publish(t, owner, "people/basic", "1.0.0")
	admin := f.loginAdmin(t, "root")
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, versionPath("people/basic", "1.0.0"), admin,
		map[string]any{"status": model.VersionBlocked}).status)

	resp := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/manifest", "", nil)

	require.Equal(t, http.StatusForbidden, resp.status, "响应：%s", resp.body)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeComponentBlocked, resp.Error.Code)
}

// ============================================================
// 18.13 审计
// ============================================================

// 审计里有"谁下载了什么"，必须登录才能看。
func TestAuditEndpointRequiresAuthAndRecordsPublish(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	anon := f.do(t, http.MethodGet, "/api/v1/audit", "", nil)
	assert.Equal(t, http.StatusUnauthorized, anon.status)

	resp := f.do(t, http.MethodGet, "/api/v1/audit?componentId=people/basic", token, nil)
	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)

	var entries []model.AuditEntry
	resp.decode(t, &entries)
	require.NotEmpty(t, entries)

	actions := map[string]bool{}
	for _, e := range entries {
		actions[e.Action] = true
		assert.Equal(t, "alice", e.Operator)
	}
	assert.True(t, actions[model.ActionVersionPublished], "发布动作必须留痕，实际：%v", actions)
}

// ============================================================
// 协议层通用行为
// ============================================================

// 未知路径也要给 JSON：CLI 只会解析信封，Go 默认的 "404 page not found" 会让它报解析错误。
func TestUnknownRouteReturnsJSONEnvelope(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/nope", "", nil)

	require.Equal(t, http.StatusNotFound, resp.status)
	assert.Contains(t, resp.header.Get("Content-Type"), "application/json")
	require.False(t, resp.Success)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeNotFound, resp.Error.Code)
}

func TestWrongMethodReturns405(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodDelete, "/api/v1/components", "", nil)

	assert.Equal(t, http.StatusMethodNotAllowed, resp.status, "响应：%s", resp.body)
}

// 错误信封的形状是对外契约：success=false + error.code + error.message。
func TestErrorEnvelopeShape(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/components/nobody/here", "", nil)

	var envelope struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(resp.body, &envelope))
	assert.False(t, envelope.Success)
	assert.NotEmpty(t, envelope.Error.Code)
	assert.NotEmpty(t, envelope.Error.Message)
	assert.NotContains(t, string(resp.body), `"data"`, "失败响应不该带 data")
}

// 组件 ID 是两段式 scope/name（002 §10.3）：一段的路径不是有效组件 ID。
func TestSingleSegmentComponentIDIsNotFound(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, http.MethodGet, "/api/v1/components/basic", "", nil)

	assert.Equal(t, http.StatusNotFound, resp.status)
	assert.Contains(t, resp.header.Get("Content-Type"), "application/json")
}
