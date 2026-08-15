package handler_test

// 本文件是组织管理三个端点（007 §9.5）的 HTTP 层测试：
// 路由通不通、状态码对不对、未登录挡不挡得住。
// 业务规则本身由 service 层的用例盯住。

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
)

// createOrg 建一个组织并返回它的 ID。
func createOrg(t *testing.T, f *fixture, token, name string) string {
	t.Helper()

	r := f.do(t, http.MethodPost, "/api/v1/organizations", token, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, r.status, string(r.body))

	var org model.Organization
	require.NoError(t, json.Unmarshal(r.Data, &org))
	require.NotEmpty(t, org.OrgID)
	return org.OrgID
}

func TestCreateOrganizationEndpoint(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	r := f.do(t, http.MethodPost, "/api/v1/organizations", token, map[string]any{"name": "Acme"})

	require.Equal(t, http.StatusCreated, r.status, string(r.body))
	assert.True(t, r.Success)

	var org model.Organization
	require.NoError(t, json.Unmarshal(r.Data, &org))
	assert.Equal(t, "Acme", org.Name)
	assert.NotEmpty(t, org.OwnerID)
}

func TestCreateOrganizationRequiresAuth(t *testing.T) {
	f := newFixture(t)

	r := f.do(t, http.MethodPost, "/api/v1/organizations", "", map[string]any{"name": "Acme"})

	assert.Equal(t, http.StatusUnauthorized, r.status)
	require.NotNil(t, r.Error)
	assert.Equal(t, model.CodeUnauthorized, r.Error.Code)
}

func TestListOrganizationsEndpoint(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	orgID := createOrg(t, f, token, "Acme")

	r := f.do(t, http.MethodGet, "/api/v1/organizations", token, nil)

	require.Equal(t, http.StatusOK, r.status, string(r.body))
	var got struct {
		Organizations []model.Organization `json:"organizations"`
		Total         int                  `json:"total"`
	}
	require.NoError(t, json.Unmarshal(r.Data, &got))
	assert.Equal(t, 1, got.Total)
	assert.Equal(t, orgID, got.Organizations[0].OrgID)
}

func TestListOrganizationsRequiresAuthEndpoint(t *testing.T) {
	f := newFixture(t)

	r := f.do(t, http.MethodGet, "/api/v1/organizations", "", nil)

	assert.Equal(t, http.StatusUnauthorized, r.status)
}

func TestAddMemberEndpoint(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")
	f.login(t, "bob")
	orgID := createOrg(t, f, token, "Acme")

	// 组织 ID 带 `-`，路径参数要能原样取到
	r := f.do(t, http.MethodPost, "/api/v1/organizations/"+orgID+"/members", token,
		map[string]any{"username": "bob"})

	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.True(t, r.Success)
}

// 不是所有者的人来加人：403，而不是 500 或者悄悄成功。
func TestAddMemberForbiddenEndpoint(t *testing.T) {
	f := newFixture(t)
	owner := f.login(t, "alice")
	mallory := f.login(t, "mallory")
	orgID := createOrg(t, f, owner, "Acme")

	r := f.do(t, http.MethodPost, "/api/v1/organizations/"+orgID+"/members", mallory,
		map[string]any{"username": "mallory"})

	assert.Equal(t, http.StatusForbidden, r.status)
	require.NotNil(t, r.Error)
	assert.Equal(t, model.CodeForbidden, r.Error.Code)
}

func TestAddMemberUnknownOrganizationEndpoint(t *testing.T) {
	f := newFixture(t)
	token := f.login(t, "alice")

	r := f.do(t, http.MethodPost, "/api/v1/organizations/org-nope/members", token,
		map[string]any{"username": "alice"})

	assert.Equal(t, http.StatusNotFound, r.status)
}

// 注册接口即使收到 orgId 也必须忽略它（见 service 层的说明）。
func TestRegisterEndpointIgnoresOrgID(t *testing.T) {
	f := newFixture(t)

	r := f.do(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username": "outsider", "password": "correct-horse-battery", "orgId": "org-acme",
	})

	require.Equal(t, http.StatusCreated, r.status, string(r.body))
	var user model.User
	require.NoError(t, json.Unmarshal(r.Data, &user))
	assert.Empty(t, user.OrgID, "自报的组织不能生效")
}
