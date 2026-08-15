package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 本文件覆盖 P32：组件在**运行时**也暴露自己的 OpenAPI 文档。
//
// 它同时是市场产物（发布时上传，调用方 add 之后就能拿到）。但产物是
// **发布那一刻**的快照，而 infra/api-docs 之类的工具要回答的是
// "**此刻跑着的**服务长什么样"——组件升级之后，只有运行时这一份是准的。

func TestOpenAPIEndpointServesSpec(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	handleOpenAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got == "" {
		t.Error("要带 Content-Type，否则浏览器不知道怎么渲染")
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("发出去的不是合法 JSON：%v", err)
	}
	if doc["openapi"] == nil {
		t.Error("缺少 openapi 版本字段")
	}
	if doc["paths"] == nil {
		t.Error("缺少 paths")
	}
}

// TestEmbeddedSpecMatchesFile：嵌进去的必须就是仓库里那一份。
//
// go:embed 在**编译时**读文件。忘了重新编译的话，容器里发出去的会是旧文档，
// 而文件本身早就改了——这种不一致最难发现。
func TestEmbeddedSpecMatchesFile(t *testing.T) {
	if len(openapiSpec) == 0 {
		t.Fatal("openapi.json 没有被嵌进二进制")
	}
	if !json.Valid(openapiSpec) {
		t.Fatal("嵌进去的不是合法 JSON")
	}
}
