// 本文件是 Step 19 市场客户端的代码级测试。
//
// 业务行为（登录、发布的完整流程）在 internal/cli 的测试里；
// 这里只管协议细节：信封解析、错误码翻译、异常响应。
package market_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/market"
)

// ============================================================
// 错误信封
// ============================================================

func TestDecodeErrorReadsEnvelope(t *testing.T) {
	apiErr := market.DecodeError(403, []byte(
		`{"success":false,"error":{"code":"COMPONENT_BLOCKED","message":"已下架",`+
			`"details":{"componentId":"people/basic"}}}`))

	assert.Equal(t, "COMPONENT_BLOCKED", apiErr.Code)
	assert.Equal(t, "已下架", apiErr.Message)
	assert.Equal(t, 403, apiErr.Status)
	assert.Equal(t, "people/basic", apiErr.Details["componentId"])
	assert.Contains(t, apiErr.Error(), "COMPONENT_BLOCKED")
}

// 市场前面可能挡着网关，返回的不一定是信封。这时也要给出能看的信息，
// 而不是一个空错误。
func TestDecodeErrorFallsBackForNonEnvelopeBody(t *testing.T) {
	apiErr := market.DecodeError(502, []byte("<html>502 Bad Gateway</html>"))

	assert.Empty(t, apiErr.Code)
	assert.Contains(t, apiErr.Message, "502")
	assert.Contains(t, apiErr.Message, "Bad Gateway")
	assert.Equal(t, 502, apiErr.Status)
	assert.Equal(t, apiErr.Message, apiErr.Error(), "没有错误码时 Error() 就是消息本身")
}

func TestDecodeErrorFallsBackForEmptyBody(t *testing.T) {
	apiErr := market.DecodeError(500, nil)

	assert.Contains(t, apiErr.Message, "500")
}

// 网关返回的长篇 HTML 不能整页糊到终端上。
func TestDecodeErrorTruncatesLongBody(t *testing.T) {
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'x'
	}

	apiErr := market.DecodeError(500, long)

	assert.Less(t, len(apiErr.Message), 300)
	assert.Contains(t, apiErr.Message, "…")
}

// ============================================================
// 错误码 → 使用者建议
// ============================================================

// 同样是 4xx，不同错误码该给完全不同的建议。
func TestAsCLIErrorMapsCodesToDistinctAdvice(t *testing.T) {
	cases := []struct {
		code       string
		status     int
		wantCode   clierr.Code
		wantHint   string
		unwantHint string
	}{
		{"COMPONENT_BLOCKED", 403, clierr.CodeComponentBlocked, "市场管理员", "brickkit login"},
		{"UNAUTHORIZED", 401, clierr.CodeAuthRequired, "brickkit login", ""},
		{"FORBIDDEN", 403, clierr.CodeAuthFailed, "所有者", ""},
		{"VERSION_ALREADY_EXISTS", 409, clierr.CodeConfigConflict, "版本号", ""},
		{"NOT_FOUND", 404, clierr.CodeComponentNotFound, "组件 ID", ""},
	}

	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			err := market.AsCLIError("发布组件", &market.APIError{
				Code: c.code, Message: "市场说的原因", Status: c.status,
			})

			assert.Equal(t, c.wantCode, err.Code)
			rendered := err.Format()
			assert.Contains(t, rendered, "市场说的原因", "必须把市场给的原因说出来")
			assert.Contains(t, rendered, c.wantHint)
			if c.unwantHint != "" {
				assert.NotContains(t, rendered, c.unwantHint)
			}
		})
	}
}

// 认不出的错误码要带上动作，让人知道是哪一步失败了。
func TestAsCLIErrorFallsBackWithAction(t *testing.T) {
	err := market.AsCLIError("上传产物 openapi.json", &market.APIError{
		Code: "SOMETHING_NEW", Message: "市场拒绝了", Status: 400,
	})

	assert.Equal(t, clierr.CodeNetworkUnreachable, err.Code)
	assert.Contains(t, err.Format(), "上传产物 openapi.json")
	assert.Contains(t, err.Format(), "市场拒绝了")
}

// 5xx 是服务端的问题，建议要跟 4xx 不一样。
func TestAsCLIErrorSuggestsRetryOnServerError(t *testing.T) {
	err := market.AsCLIError("发布组件", &market.APIError{Status: 503, Message: "服务不可用"})

	assert.Contains(t, err.Format(), "稍后重试")
}

// 市场没给 message 时也要有一句能看的说明。
func TestAsCLIErrorUsesFallbackMessage(t *testing.T) {
	err := market.AsCLIError("发布组件", &market.APIError{Code: "UNAUTHORIZED", Status: 401})

	assert.Contains(t, err.Format(), "市场认证失败")
}

// details 是校验类错误里唯一有价值的部分，必须显示出来。
func TestWithDetailsRendersNestedDetails(t *testing.T) {
	apiErr := &market.APIError{
		Code: "CONFIG_SCHEMA_RESERVED_VARIABLE_CONFLICT", Message: "配置项冲突", Status: 400,
		Details: map[string]any{
			"conflicts": []any{
				map[string]any{"configKey": "databaseUrl", "envVarName": "DATABASE_URL"},
			},
			"cause": "内部堆栈，不该给使用者看",
		},
	}

	rendered := market.WithDetails(market.AsCLIError("发布组件", apiErr), apiErr).Format()

	assert.Contains(t, rendered, "DATABASE_URL")
	assert.Contains(t, rendered, "databaseUrl")
	assert.NotContains(t, rendered, "内部堆栈", "details.cause 是服务端排障用的，不该外泄给使用者")
}

// ============================================================
// 客户端
// ============================================================

func TestLoginReturnsToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/auth/login", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"token": "tok-123", "username": "zhangsan",
				"expiresAt": "2026-09-08T10:00:00Z",
			},
		})
	}))
	defer server.Close()

	result, err := market.New(server.URL, "").Login(context.Background(), "zhangsan", "pw")

	require.NoError(t, err)
	assert.Equal(t, "tok-123", result.Token)
	assert.Equal(t, "zhangsan", result.Username)
	assert.Equal(t, 2026, result.ExpiresAt.Year())
}

// 市场答了 200 却没给令牌：这不是"登录成功"，必须报错。
func TestLoginWithoutTokenIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"username":"zhangsan"}}`))
	}))
	defer server.Close()

	_, err := market.New(server.URL, "").Login(context.Background(), "zhangsan", "pw")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "访问令牌")
}

// 地址指错了地方（比如指到一个返回 HTML 的站点）要说清楚。
func TestLoginAgainstNonMarketEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>hello</html>"))
	}))
	defer server.Close()

	_, err := market.New(server.URL, "").Login(context.Background(), "zhangsan", "pw")

	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "/api/v1")
}

// 带上 Token 的请求必须发 Authorization 头。
func TestClientSendsBearerToken(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		writeEmptyOK(w)
	}))
	defer server.Close()

	require.NoError(t, market.New(server.URL, "tok-abc").
		SetVersionStatus(context.Background(), "people/basic", "1.0.0", "stable"))

	assert.Equal(t, "Bearer tok-abc", got)
}

// 组件 ID 里的斜杠是路径的一部分，不转义（007 §4.5）。
func TestClientKeepsComponentIDSlashInPath(t *testing.T) {
	var path, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		writeEmptyOK(w)
	}))
	defer server.Close()

	require.NoError(t, market.New(server.URL, "tok").UploadArtifact(
		context.Background(), "people/basic", "1.0.0", "art-0", "proto/people.proto", []byte("x")))

	assert.Equal(t, "/components/people/basic/versions/1.0.0/artifacts/art-0/upload", path)
	assert.Equal(t, "file=proto%2Fpeople.proto", query)
}

// 市场地址末尾多写一个斜杠不该拼出双斜杠。
func TestClientNormalizesBaseURL(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeEmptyOK(w)
	}))
	defer server.Close()

	require.NoError(t, market.New(server.URL+"/  ", "tok").
		SetVisibility(context.Background(), "people/basic", "private"))

	assert.Equal(t, "/components/people/basic/visibility", path)
}

func TestListArtifactsDecodesEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":[
			{"id":"art-0","type":"api-docs","format":"openapi","files":["openapi.json"]}]}`))
	}))
	defer server.Close()

	got, err := market.New(server.URL, "tok").
		ListArtifacts(context.Background(), "people/basic", "1.0.0")

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "art-0", got[0].ID)
	assert.Equal(t, []string{"openapi.json"}, got[0].Files)
}

// 没有信封、直接返回数组的实现也能读（兼容性）。
func TestListArtifactsAcceptsBareArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"art-0","type":"api-docs","files":["openapi.json"]}]`))
	}))
	defer server.Close()

	got, err := market.New(server.URL, "tok").
		ListArtifacts(context.Background(), "people/basic", "1.0.0")

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "art-0", got[0].ID)
}

func TestListArtifactsRejectsUnparsableBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":"这不是数组"}`))
	}))
	defer server.Close()

	_, err := market.New(server.URL, "tok").
		ListArtifacts(context.Background(), "people/basic", "1.0.0")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "格式不符")
}

// 市场不可达时要说"不可达"，并去掉 net/http 那串冗长的 URL 前缀。
func TestUnreachableMarket(t *testing.T) {
	_, err := market.New("http://127.0.0.1:1/api/v1", "").
		Login(context.Background(), "zhangsan", "pw")

	require.Error(t, err)
	e := clierr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, clierr.CodeNetworkUnreachable, e.Code)
	assert.Contains(t, e.Format(), "市场不可达")
	assert.NotContains(t, e.Format(), "原因：Post \"http", "错误里不该重复整条 URL")
}

func TestInvalidMarketURL(t *testing.T) {
	err := market.New("://not a url", "tok").
		SetVisibility(context.Background(), "people/basic", "public")

	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
}

func writeEmptyOK(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
}
