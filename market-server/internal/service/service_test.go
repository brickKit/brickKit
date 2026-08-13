// 本文件是 Step 18-C「服务层与认证」的业务行为测试。
//
// 覆盖开发计划 18.12–18.14、18.17–18.21、18.24、18.25，
// 以及 007 §5（可见性与权限）、§6（版本状态）、§16（审计）的规则。
package service_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
	"github.com/brickkit/market-server/internal/service"
	"github.com/brickkit/market-server/internal/storage"
)

// ============================================================
// 测试夹具
// ============================================================

type fixture struct {
	svc   *service.Service
	repo  repo.Repository
	store *storage.Memory
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	r := repo.NewMemory()
	store := storage.NewMemory()
	svc := service.New(r, store, service.Options{BcryptCost: bcrypt.MinCost})
	return &fixture{svc: svc, repo: r, store: store}
}

// registerUser 注册并登录一个用户，返回其身份。
func (f *fixture) registerUser(t *testing.T, username string) *service.Identity {
	t.Helper()
	ctx := context.Background()

	_, err := f.svc.Register(ctx, service.RegisterRequest{
		Username: username, Password: "correct-horse-battery", Email: username + "@example.com",
	})
	require.NoError(t, err)

	token, err := f.svc.Login(ctx, username, "correct-horse-battery")
	require.NoError(t, err)

	identity, err := f.svc.Authenticate(ctx, token.Token)
	require.NoError(t, err)
	return identity
}

// promoteAdmin 把用户提升为管理员（市场管理员由运维直接在库里设置）。
func (f *fixture) promoteAdmin(t *testing.T, id *service.Identity) *service.Identity {
	t.Helper()
	user, err := f.repo.GetUserByID(context.Background(), id.UserID)
	require.NoError(t, err)
	user.IsAdmin = true
	require.NoError(t, f.repo.CreateUser(context.Background(), &model.User{
		UserID: user.UserID + "-admin", Username: user.Username + "-admin", IsAdmin: true,
	}))
	return &service.Identity{UserID: user.UserID + "-admin", Username: user.Username + "-admin", IsAdmin: true}
}

// manifestOf 生成一份合法 Manifest。
func manifestOf(t *testing.T, componentID, version string, artifacts []any) json.RawMessage {
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
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	return raw
}

func publishRequest(t *testing.T, componentID, version string, artifacts []any) model.PublishRequest {
	t.Helper()
	return model.PublishRequest{
		Version:    version,
		Status:     model.VersionStable,
		Manifest:   manifestOf(t, componentID, version, artifacts),
		SourceType: model.SourceTypeGit,
		GitURL:     "https://github.com/brickkit/demo.git",
	}
}

// publish 发布一个版本（默认无文件产物，直接 stable）。
func (f *fixture) publish(t *testing.T, id *service.Identity, componentID, version string) *model.Version {
	t.Helper()
	v, err := f.svc.Publish(context.Background(), id, componentID, publishRequest(t, componentID, version, nil))
	require.NoError(t, err)
	return v
}

func apiErrorOf(t *testing.T, err error) *model.APIError {
	t.Helper()
	require.Error(t, err)
	e, ok := err.(*model.APIError)
	require.True(t, ok, "应返回 *model.APIError，实际 %T：%v", err, err)
	return e
}

// ============================================================
// 18.19 / 18.20 注册与登录
// ============================================================

func TestRegisterAndLogin(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	user, err := f.svc.Register(ctx, service.RegisterRequest{
		Username: "zhangsan", Password: "correct-horse-battery", Email: "z@example.com",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, user.UserID)
	assert.Equal(t, "zhangsan", user.Username)
	assert.Empty(t, user.PasswordHash, "对外返回的用户不得带密码哈希")

	// 密码必须是哈希存储，绝不能明文落库
	stored, err := f.repo.GetUserByUsername(ctx, "zhangsan")
	require.NoError(t, err)
	assert.NotEmpty(t, stored.PasswordHash)
	assert.NotContains(t, stored.PasswordHash, "correct-horse-battery")

	token, err := f.svc.Login(ctx, "zhangsan", "correct-horse-battery")
	require.NoError(t, err)
	assert.NotEmpty(t, token.Token)
	assert.Equal(t, "zhangsan", token.Username)
	assert.True(t, token.ExpiresAt.After(time.Now()), "令牌要有有效期")

	identity, err := f.svc.Authenticate(ctx, token.Token)
	require.NoError(t, err)
	assert.Equal(t, "zhangsan", identity.Username)
	assert.False(t, identity.Anonymous)
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	f := newFixture(t)
	f.registerUser(t, "zhangsan")

	_, err := f.svc.Register(context.Background(), service.RegisterRequest{
		Username: "zhangsan", Password: "another-password",
	})
	assert.Equal(t, model.CodeConflict, apiErrorOf(t, err).Code)
}

func TestRegisterValidatesInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	cases := map[string]service.RegisterRequest{
		"用户名为空":  {Password: "correct-horse-battery"},
		"密码为空":   {Username: "zhangsan"},
		"密码太短":   {Username: "zhangsan", Password: "123"},
		"用户名含空格": {Username: "zhang san", Password: "correct-horse-battery"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.Register(ctx, req)
			assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)
		})
	}
}

func TestLoginRejectsWrongCredentials(t *testing.T) {
	f := newFixture(t)
	f.registerUser(t, "zhangsan")
	ctx := context.Background()

	_, err := f.svc.Login(ctx, "zhangsan", "wrong-password")
	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeUnauthorized, e.Code)
	assert.NotContains(t, e.Message, "wrong-password", "错误信息不得回显密码")

	_, err = f.svc.Login(ctx, "nobody", "correct-horse-battery")
	assert.Equal(t, model.CodeUnauthorized, apiErrorOf(t, err).Code,
		"用户不存在与密码错误返回同样的错误，避免枚举用户名")
}

// 未带令牌 → 匿名身份；令牌非法或过期 → 报错。
func TestAuthenticate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	anon, err := f.svc.Authenticate(ctx, "")
	require.NoError(t, err)
	assert.True(t, anon.Anonymous)

	_, err = f.svc.Authenticate(ctx, "tok-not-exist")
	assert.Equal(t, model.CodeUnauthorized, apiErrorOf(t, err).Code)
}

func TestAuthenticateRejectsExpiredToken(t *testing.T) {
	r := repo.NewMemory()
	svc := service.New(r, storage.NewMemory(), service.Options{TokenTTL: time.Hour, BcryptCost: bcrypt.MinCost})
	ctx := context.Background()

	_, err := svc.Register(ctx, service.RegisterRequest{Username: "zhangsan", Password: "correct-horse-battery"})
	require.NoError(t, err)
	token, err := svc.Login(ctx, "zhangsan", "correct-horse-battery")
	require.NoError(t, err)

	// 直接把令牌改成已过期
	stored, err := r.GetToken(ctx, token.Token)
	require.NoError(t, err)
	stored.ExpiresAt = time.Now().Add(-time.Minute)
	require.NoError(t, r.CreateToken(ctx, stored))

	_, err = svc.Authenticate(ctx, token.Token)
	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeUnauthorized, e.Code)
	assert.Contains(t, e.Message, "过期")
}

func TestLogout(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.registerUser(t, "zhangsan")

	token, err := f.svc.Login(ctx, "zhangsan", "correct-horse-battery")
	require.NoError(t, err)
	require.NoError(t, f.svc.Logout(ctx, token.Token))

	_, err = f.svc.Authenticate(ctx, token.Token)
	assert.Equal(t, model.CodeUnauthorized, apiErrorOf(t, err).Code)
}

// ============================================================
// 发布（18.14 版本不可重复）
// ============================================================

func TestPublishCreatesComponentAndVersion(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	v, err := f.svc.Publish(ctx, id, "people/basic", publishRequest(t, "people/basic", "1.0.0", nil))
	require.NoError(t, err)
	assert.Equal(t, "people/basic", v.ComponentID)
	assert.Equal(t, "1.0.0", v.Version)
	assert.Equal(t, "zhangsan", v.PublishedBy)

	// 首次发布自动建组件记录，发布者成为 owner
	c, err := f.repo.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, id.UserID, c.OwnerID)
	assert.Equal(t, model.VisibilityPublic, c.Visibility, "默认 public")
	assert.Equal(t, model.SourceTypeGit, c.SourceType)
	assert.Equal(t, []string{"demo"}, c.Tags, "标签取自 Manifest")
}

// 18.14 版本不可重复。
func TestPublishRejectsDuplicateVersion(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	f.publish(t, id, "people/basic", "1.0.0")

	_, err := f.svc.Publish(context.Background(), id, "people/basic",
		publishRequest(t, "people/basic", "1.0.0", nil))

	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeVersionExists, e.Code)
	assert.Equal(t, "people/basic", e.Details["componentId"])
	assert.Equal(t, "1.0.0", e.Details["version"])
}

// 发布必须认证（007 §9.6：组件发布操作必须认证）。
func TestPublishRequiresAuthentication(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.Publish(context.Background(), service.Anonymous(), "people/basic",
		publishRequest(t, "people/basic", "1.0.0", nil))

	assert.Equal(t, model.CodeUnauthorized, apiErrorOf(t, err).Code)
}

// 只有组件所有者（或管理员）能往该命名空间继续发布。
func TestPublishRejectsNonOwner(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	f.publish(t, owner, "people/basic", "1.0.0")

	other := f.registerUser(t, "lisi")
	_, err := f.svc.Publish(context.Background(), other, "people/basic",
		publishRequest(t, "people/basic", "2.0.0", nil))

	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeForbidden, e.Code)
	assert.Contains(t, e.Message, "所有者")
}

// 校验不通过的 Manifest 一律拒绝（18-A 的校验器接在这里）。
func TestPublishRunsValidation(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")

	req := publishRequest(t, "people/basic", "1.0.0", nil)
	req.Manifest = manifestOf(t, "people/basic", "1.0.0", []any{
		map[string]any{"format": "protobuf", "files": []string{"a.proto"}}, // 缺 type
	})

	_, err := f.svc.Publish(context.Background(), id, "people/basic", req)
	assert.Equal(t, model.CodeManifestInvalid, apiErrorOf(t, err).Code)
}

// 路径里的组件 ID 必须与 Manifest 中的一致。
func TestPublishRejectsComponentIDMismatch(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")

	_, err := f.svc.Publish(context.Background(), id, "department/tree",
		publishRequest(t, "people/basic", "1.0.0", nil))

	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeInvalidRequest, e.Code)
	assert.Contains(t, e.Message, "组件 ID")
}

// 发布时把 Manifest 里声明的 artifacts 落成产物记录（供上传与下载）。
func TestPublishRecordsArtifacts(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", []any{
		map[string]any{"type": "api-contract", "format": "protobuf", "files": []string{"proto/people.proto"}},
		map[string]any{"type": "container", "reference": "registry.example.com/people-basic:1.0.0"},
	})
	req.Status = model.VersionDraft

	_, err := f.svc.Publish(ctx, id, "people/basic", req)
	require.NoError(t, err)

	artifacts, err := f.svc.ListArtifacts(ctx, id, "people/basic", "1.0.0")
	require.NoError(t, err)
	require.Len(t, artifacts, 2)
	assert.Equal(t, "art-0", artifacts[0].ArtifactID)
	assert.Equal(t, model.ArtifactTypeAPIContract, artifacts[0].Type)
	assert.Equal(t, []string{"proto/people.proto"}, artifacts[0].Files)
	assert.Equal(t, "registry.example.com/people-basic:1.0.0", artifacts[1].Reference)
}

// ============================================================
// 产物上传与下载
// ============================================================

func TestUploadAndDownloadArtifact(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", []any{
		map[string]any{"type": "api-contract", "files": []string{"proto/people.proto"}},
	})
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, id, "people/basic", req)
	require.NoError(t, err)

	content := "syntax = \"proto3\";\n"
	require.NoError(t, f.svc.UploadArtifact(ctx, id, "people/basic", "1.0.0", "art-0",
		"proto/people.proto", strings.NewReader(content), int64(len(content))))

	// 上传后才能转 stable（007 §18.2：文件必须与 files 列表一致）
	require.NoError(t, f.svc.SetVersionStatus(ctx, id, "people/basic", "1.0.0", model.VersionStable))

	r, err := f.svc.DownloadArtifact(ctx, service.Anonymous(), "people/basic", "1.0.0", "art-0", "proto/people.proto")
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

// 上传的文件必须在 Manifest 声明的 files 列表里。
func TestUploadRejectsUndeclaredFile(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", []any{
		map[string]any{"type": "api-contract", "files": []string{"proto/people.proto"}},
	})
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, id, "people/basic", req)
	require.NoError(t, err)

	err = f.svc.UploadArtifact(ctx, id, "people/basic", "1.0.0", "art-0",
		"proto/不在列表里.proto", strings.NewReader("x"), 1)

	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeInvalidRequest, e.Code)
	assert.Contains(t, e.Message, "未在 Manifest 中声明")
}

// 文件没传齐就想转 stable → 拒绝。
func TestSetStableRequiresAllFilesUploaded(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", []any{
		map[string]any{"type": "api-contract", "files": []string{"a.proto", "b.proto"}},
	})
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, id, "people/basic", req)
	require.NoError(t, err)
	require.NoError(t, f.svc.UploadArtifact(ctx, id, "people/basic", "1.0.0", "art-0",
		"a.proto", strings.NewReader("x"), 1))

	err = f.svc.SetVersionStatus(ctx, id, "people/basic", "1.0.0", model.VersionStable)
	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeConflict, e.Code)
	assert.Contains(t, e.Message, "b.proto")
}

func TestUploadRequiresOwner(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	other := f.registerUser(t, "lisi")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", []any{
		map[string]any{"type": "api-contract", "files": []string{"a.proto"}},
	})
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, owner, "people/basic", req)
	require.NoError(t, err)

	err = f.svc.UploadArtifact(ctx, other, "people/basic", "1.0.0", "art-0",
		"a.proto", strings.NewReader("x"), 1)
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)
}

// 下载会累加下载计数并留下审计（007 §16.1）。
func TestDownloadRecordsAudit(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", []any{
		map[string]any{"type": "api-docs", "files": []string{"openapi.json"}},
	})
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, id, "people/basic", req)
	require.NoError(t, err)
	require.NoError(t, f.svc.UploadArtifact(ctx, id, "people/basic", "1.0.0", "art-0",
		"openapi.json", strings.NewReader("{}"), 2))

	r, err := f.svc.DownloadArtifact(ctx, id, "people/basic", "1.0.0", "art-0", "openapi.json")
	require.NoError(t, err)
	_ = r.Close()

	c, err := f.repo.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, int64(1), c.Downloads)

	entries, err := f.svc.ListAudit(ctx, id, repo.AuditQuery{Action: model.ActionArtifactDownload})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "people/basic", entries[0].ComponentID)
}

// ============================================================
// 18.12 / 18.21 可见性与访问控制
// ============================================================

// 18.21 未认证访问 private 组件被拒绝。
func TestPrivateComponentDeniedForAnonymous(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "mycompany/approval", "1.0.0")
	require.NoError(t, f.svc.SetVisibility(ctx, owner, "mycompany/approval", model.VisibilityPrivate))

	_, err := f.svc.GetManifest(ctx, service.Anonymous(), "mycompany/approval", "1.0.0")
	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeForbidden, e.Code)

	// 搜索与详情同样看不到
	found, err := f.svc.SearchComponents(ctx, service.Anonymous(), repo.ComponentQuery{})
	require.NoError(t, err)
	assert.Empty(t, found.Items)
	assert.Zero(t, found.Total)

	_, err = f.svc.GetComponent(ctx, service.Anonymous(), "mycompany/approval")
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)
}

// 18.12 private 组件：授权用户可访问，未授权用户不行。
func TestPrivateComponentAccessControl(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	authorized := f.registerUser(t, "lisi")
	outsider := f.registerUser(t, "wangwu")
	ctx := context.Background()

	f.publish(t, owner, "mycompany/approval", "1.0.0")
	require.NoError(t, f.svc.SetVisibility(ctx, owner, "mycompany/approval", model.VisibilityPrivate))
	require.NoError(t, f.svc.SetAccessPolicies(ctx, owner, "mycompany/approval", []model.AccessPolicy{
		{TargetType: model.TargetUser, TargetID: authorized.UserID, Permission: "read"},
	}))

	// 所有者
	_, err := f.svc.GetManifest(ctx, owner, "mycompany/approval", "1.0.0")
	assert.NoError(t, err)

	// 被授权用户
	_, err = f.svc.GetManifest(ctx, authorized, "mycompany/approval", "1.0.0")
	assert.NoError(t, err)

	// 未授权用户
	_, err = f.svc.GetManifest(ctx, outsider, "mycompany/approval", "1.0.0")
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)

	// 被授权用户能在搜索结果里看到它
	found, err := f.svc.SearchComponents(ctx, authorized, repo.ComponentQuery{})
	require.NoError(t, err)
	assert.Equal(t, []string{"mycompany/approval"}, componentIDs(found.Items))

	found, err = f.svc.SearchComponents(ctx, outsider, repo.ComponentQuery{})
	require.NoError(t, err)
	assert.Empty(t, found.Items)
}

// 组织级授权：同组织的成员可访问。
func TestPrivateComponentOrgAccess(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	member, err := f.svc.Register(ctx, service.RegisterRequest{
		Username: "lisi", Password: "correct-horse-battery", OrgID: "org-mycompany",
	})
	require.NoError(t, err)
	token, err := f.svc.Login(ctx, "lisi", "correct-horse-battery")
	require.NoError(t, err)
	memberIdentity, err := f.svc.Authenticate(ctx, token.Token)
	require.NoError(t, err)
	assert.Equal(t, "org-mycompany", memberIdentity.OrgID)
	_ = member

	f.publish(t, owner, "mycompany/approval", "1.0.0")
	require.NoError(t, f.svc.SetVisibility(ctx, owner, "mycompany/approval", model.VisibilityPrivate))
	require.NoError(t, f.svc.SetAccessPolicies(ctx, owner, "mycompany/approval", []model.AccessPolicy{
		{TargetType: model.TargetOrganization, TargetID: "org-mycompany", Permission: "read"},
	}))

	_, err = f.svc.GetManifest(ctx, memberIdentity, "mycompany/approval", "1.0.0")
	assert.NoError(t, err)
}

// 18.18 可见性变更：只有所有者或管理员能改。
func TestSetVisibilityRequiresOwner(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	other := f.registerUser(t, "lisi")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")

	err := f.svc.SetVisibility(ctx, other, "people/basic", model.VisibilityPrivate)
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)

	require.NoError(t, f.svc.SetVisibility(ctx, owner, "people/basic", model.VisibilityPrivate))
	c, err := f.repo.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, model.VisibilityPrivate, c.Visibility)

	err = f.svc.SetVisibility(ctx, owner, "people/basic", "secret")
	assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)
}

// ============================================================
// 18.17 / 18.24 / 18.25 版本状态
// ============================================================

// 18.17 版本状态变更。
func TestSetVersionStatus(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")

	require.NoError(t, f.svc.SetVersionStatus(ctx, owner, "people/basic", "1.0.0", model.VersionDeprecated))
	v, err := f.repo.GetVersion(ctx, "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, model.VersionDeprecated, v.Status)

	// deprecated 仍可安装（007 §6：可以安装，但提示风险）
	view, err := f.svc.GetManifest(ctx, service.Anonymous(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, model.VersionDeprecated, view.Status)
}

// 007 §6.3：标记 blocked 只有市场管理员。
func TestOnlyAdminCanBlockVersion(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")

	err := f.svc.SetVersionStatus(ctx, owner, "people/basic", "1.0.0", model.VersionBlocked)
	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeForbidden, e.Code)
	assert.Contains(t, e.Message, "管理员")

	admin := f.promoteAdmin(t, owner)
	require.NoError(t, f.svc.SetVersionStatus(ctx, admin, "people/basic", "1.0.0", model.VersionBlocked))
}

// 18.25 blocked 组件/版本不可安装。
func TestBlockedVersionCannotBeInstalled(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")
	admin := f.promoteAdmin(t, owner)
	require.NoError(t, f.svc.SetVersionStatus(ctx, admin, "people/basic", "1.0.0", model.VersionBlocked))

	_, err := f.svc.GetManifest(ctx, service.Anonymous(), "people/basic", "1.0.0")
	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeComponentBlocked, e.Code)
	assert.Contains(t, e.Message, "不能安装")

	_, err = f.svc.ListArtifacts(ctx, service.Anonymous(), "people/basic", "1.0.0")
	assert.Equal(t, model.CodeComponentBlocked, apiErrorOf(t, err).Code)
}

// 整个组件被下架时，它的所有版本都不可安装。
func TestBlockedComponentCannotBeInstalled(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "evil/component", "1.0.0")
	admin := f.promoteAdmin(t, owner)

	require.NoError(t, f.svc.SetComponentStatus(ctx, admin, "evil/component", model.ComponentBlocked))

	_, err := f.svc.GetManifest(ctx, service.Anonymous(), "evil/component", "1.0.0")
	assert.Equal(t, model.CodeComponentBlocked, apiErrorOf(t, err).Code)

	err = f.svc.SetComponentStatus(ctx, owner, "evil/component", model.ComponentActive)
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code, "解除下架同样只有管理员能做")
}

// 18.24 已发布版本不可物理删除，只能软删除。
func TestDeleteVersionIsSoftDelete(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")

	require.NoError(t, f.svc.DeleteVersion(ctx, owner, "people/basic", "1.0.0"))

	// 记录仍在库里（只是状态变了），历史可追溯
	v, err := f.repo.GetVersion(ctx, "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, model.VersionDeleted, v.Status)

	// 但对外等同于不存在，不可安装
	_, err = f.svc.GetManifest(ctx, service.Anonymous(), "people/basic", "1.0.0")
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)

	// 已删除的版本号不能再次发布（版本号不可回收）
	_, err = f.svc.Publish(ctx, owner, "people/basic", publishRequest(t, "people/basic", "1.0.0", nil))
	assert.Equal(t, model.CodeVersionExists, apiErrorOf(t, err).Code)
}

func TestDeleteVersionRequiresOwner(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	other := f.registerUser(t, "lisi")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")

	err := f.svc.DeleteVersion(ctx, other, "people/basic", "1.0.0")
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)
}

// ============================================================
// 查询（18.2 / 18.3 / 18.15 / 18.16）
// ============================================================

func TestGetManifestReturnsSourceInfo(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")

	view, err := f.svc.GetManifest(ctx, service.Anonymous(), "people/basic", "1.0.0")
	require.NoError(t, err)

	assert.Equal(t, "people/basic", view.ComponentID)
	assert.Equal(t, "1.0.0", view.Version)
	assert.Equal(t, model.VersionStable, view.Status)
	assert.Equal(t, model.SourceTypeGit, view.SourceType, "CLI 的 --repo 要靠它判断开源/闭源")
	assert.Equal(t, "https://github.com/brickkit/demo.git", view.GitURL)

	var manifest map[string]any
	require.NoError(t, json.Unmarshal(view.Manifest, &manifest))
	assert.Equal(t, "brickkit/v1", manifest["apiVersion"])
}

func TestGetManifestNotFound(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")

	_, err := f.svc.GetManifest(ctx, service.Anonymous(), "people/basic", "9.9.9")
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)

	_, err = f.svc.GetManifest(ctx, service.Anonymous(), "nope/missing", "1.0.0")
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)
}

// draft 版本还没传完产物，不对外可见。
func TestDraftVersionIsNotInstallable(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", nil)
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, owner, "people/basic", req)
	require.NoError(t, err)

	_, err = f.svc.GetManifest(ctx, service.Anonymous(), "people/basic", "1.0.0")
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)

	// 所有者自己能看到（要能检查自己发了什么）
	_, err = f.svc.GetManifest(ctx, owner, "people/basic", "1.0.0")
	assert.NoError(t, err)
}

// 18.2 版本列表：默认不含已删除版本，按版本号倒序。
func TestListVersions(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	for _, v := range []string{"1.0.0", "1.2.0", "2.0.0"} {
		f.publish(t, owner, "people/basic", v)
	}
	require.NoError(t, f.svc.DeleteVersion(ctx, owner, "people/basic", "1.2.0"))

	versions, err := f.svc.ListVersions(ctx, service.Anonymous(), "people/basic")
	require.NoError(t, err)
	require.Len(t, versions, 2)
	assert.Equal(t, "2.0.0", versions[0].Version)
	assert.Equal(t, "1.0.0", versions[1].Version)
}

// 18.16 组件详情：带版本列表与最新版本。
func TestGetComponentDetail(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")
	f.publish(t, owner, "people/basic", "1.2.0")

	detail, err := f.svc.GetComponent(ctx, service.Anonymous(), "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", detail.Component.ComponentID)
	assert.Equal(t, "1.2.0", detail.LatestVersion)
	assert.Equal(t, []string{"1.2.0", "1.0.0"}, detail.Versions)
}

// 18.15 组件搜索。
func TestSearchComponents(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")
	f.publish(t, owner, "department/tree", "1.0.0")

	found, err := f.svc.SearchComponents(ctx, service.Anonymous(), repo.ComponentQuery{Keyword: "people"})
	require.NoError(t, err)
	assert.Equal(t, []string{"people/basic"}, componentIDs(found.Items))
	assert.Equal(t, 1, found.Total)

	found, err = f.svc.SearchComponents(ctx, service.Anonymous(), repo.ComponentQuery{})
	require.NoError(t, err)
	assert.Len(t, found.Items, 2)
	assert.Equal(t, 2, found.Total)
}

// ============================================================
// 18.13 审计
// ============================================================

func TestAuditRecordsPublishAndChanges(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	f.publish(t, owner, "people/basic", "1.0.0")
	require.NoError(t, f.svc.SetVisibility(ctx, owner, "people/basic", model.VisibilityPrivate))
	require.NoError(t, f.svc.SetVersionStatus(ctx, owner, "people/basic", "1.0.0", model.VersionDeprecated))
	require.NoError(t, f.svc.DeleteVersion(ctx, owner, "people/basic", "1.0.0"))

	entries, err := f.svc.ListAudit(ctx, owner, repo.AuditQuery{ComponentID: "people/basic"})
	require.NoError(t, err)

	actions := make([]string, 0, len(entries))
	for _, e := range entries {
		actions = append(actions, e.Action)
		assert.Equal(t, "zhangsan", e.Operator, "审计要记下是谁干的")
		assert.Equal(t, model.ResultSuccess, e.Result)
	}
	assert.Contains(t, actions, model.ActionVersionPublished)
	assert.Contains(t, actions, model.ActionVisibilityChanged)
	assert.Contains(t, actions, model.ActionVersionStatus)
	assert.Contains(t, actions, model.ActionVersionDeleted)

	// 发布记录要带版本号
	published, err := f.svc.ListAudit(ctx, owner, repo.AuditQuery{Action: model.ActionVersionPublished})
	require.NoError(t, err)
	require.Len(t, published, 1)
	assert.Equal(t, "1.0.0", published[0].Version)
}

func TestAuditRecordsRegistrationAndLogin(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	entries, err := f.svc.ListAudit(ctx, id, repo.AuditQuery{})
	require.NoError(t, err)

	actions := map[string]bool{}
	for _, e := range entries {
		actions[e.Action] = true
	}
	assert.True(t, actions[model.ActionUserRegistered])
	assert.True(t, actions[model.ActionUserLogin])
}

// 审计日志只有管理员能查（里面有谁下载了什么，属于敏感信息）。
func TestAuditQueryRequiresAuthentication(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.ListAudit(context.Background(), service.Anonymous(), repo.AuditQuery{})
	assert.Equal(t, model.CodeUnauthorized, apiErrorOf(t, err).Code)
}

func componentIDs(components []model.Component) []string {
	out := make([]string, 0, len(components))
	for _, c := range components {
		out = append(out, c.ComponentID)
	}
	return out
}
