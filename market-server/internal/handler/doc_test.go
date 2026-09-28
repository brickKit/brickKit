package handler_test

// 本文件测 BRICKKIT.md 端点：原样的 Markdown、没有文档时 404、可见性与 /manifest 完全一致。

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/market-server/internal/model"
)

const sampleDoc = "# people/basic\n\nHow to call it: `GET /people`.\n"

func (f *fixture) publishWithDoc(t *testing.T, token, componentID, version, doc string) {
	t.Helper()
	body := publishBody(t, componentID, version, nil)
	body["doc"] = doc
	resp := f.do(t, http.MethodPost, "/api/v1/components/"+componentID+"/versions", token, body)
	require.Equal(t, http.StatusCreated, resp.status, "发布失败：%s", resp.body)
	assert.NotContains(t, string(resp.body), "How to call it", "发布的响应不回传整份文档")
}

func TestDocEndpointServesMarkdown(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publishWithDoc(t, token, "people/basic", "1.0.0", sampleDoc)

	resp := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/doc", "", nil)
	require.Equal(t, http.StatusOK, resp.status, "响应：%s", resp.body)
	assert.Equal(t, "text/markdown; charset=utf-8", resp.header.Get("Content-Type"))
	assert.Equal(t, "nosniff", resp.header.Get("X-Content-Type-Options"),
		"正文是发布者写的，从市场的域名送出：浏览器不许把它猜成别的类型")
	assert.Equal(t, sampleDoc, string(resp.body), "原样，不包信封")
}

// 这一功能之前发布的版本、以及没有 BRICKKIT.md 的组件：404，错误照常是 JSON 信封。
func TestDocEndpoint404WithoutDoc(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.publish(t, token, "people/basic", "1.0.0")

	resp := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/doc", "", nil)
	require.Equal(t, http.StatusNotFound, resp.status, "响应：%s", resp.body)
	require.NotNil(t, resp.Error)
	assert.Equal(t, model.CodeNotFound, resp.Error.Code)
}

// 新端点不能成为 private 组件的旁路：对每一种调用者，/doc 与 /manifest 的回答一样。
func TestDocEndpointHonoursVisibility(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	f.publishWithDoc(t, owner, "people/basic", "1.0.0", sampleDoc)
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut,
		"/api/v1/components/people/basic/visibility", owner,
		map[string]any{"visibility": model.VisibilityPrivate}).status)
	guest := f.login(t, "bob")

	for name, token := range map[string]string{"anonymous": "", "guest": guest, "owner": owner} {
		manifest := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/manifest", token, nil)
		doc := f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/doc", token, nil)
		assert.Equal(t, manifest.status, doc.status, name)
		if manifest.Error != nil {
			require.NotNil(t, doc.Error, name)
			assert.Equal(t, manifest.Error.Code, doc.Error.Code, name)
			assert.NotContains(t, string(doc.body), "How to call it", name)
		}
	}
}

// draft 版本只有所有者看得到，文档也一样；下架的版本不给文档。
func TestDocEndpointFollowsVersionStatus(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	body := publishBody(t, "people/basic", "1.0.0", nil)
	body["doc"] = sampleDoc
	body["status"] = model.VersionDraft
	require.Equal(t, http.StatusCreated,
		f.do(t, http.MethodPost, "/api/v1/components/people/basic/versions", owner, body).status)

	assert.Equal(t, http.StatusNotFound,
		f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/doc", "", nil).status)
	assert.Equal(t, http.StatusOK,
		f.do(t, http.MethodGet, versionPath("people/basic", "1.0.0")+"/doc", owner, nil).status)
}
