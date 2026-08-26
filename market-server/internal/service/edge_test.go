// 本文件是 Step 18-C 的代码层单测：找不到、参数非法、底层故障等边界路径。
package service_test

import (
	"context"
	"errors"
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
// 找不到
// ============================================================

func TestOperationsOnMissingComponent(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	cases := map[string]func() error{
		"上传产物": func() error {
			return f.svc.UploadArtifact(ctx, id, "nope/missing", "1.0.0", "art-0", "a.proto",
				strings.NewReader("x"), 1)
		},
		"变更版本状态": func() error {
			return f.svc.SetVersionStatus(ctx, id, "nope/missing", "1.0.0", model.VersionDeprecated, "")
		},
		"删除版本":   func() error { return f.svc.DeleteVersion(ctx, id, "nope/missing", "1.0.0") },
		"变更可见性":  func() error { return f.svc.SetVisibility(ctx, id, "nope/missing", model.VisibilityPrivate) },
		"变更访问策略": func() error { return f.svc.SetAccessPolicies(ctx, id, "nope/missing", nil) },
		"查询访问策略": func() error { _, err := f.svc.ListAccessPolicies(ctx, id, "nope/missing"); return err },
		"查询详情":   func() error { _, err := f.svc.GetComponent(ctx, id, "nope/missing"); return err },
		"查询版本列表": func() error { _, err := f.svc.ListVersions(ctx, id, "nope/missing"); return err },
		"列出产物":   func() error { _, err := f.svc.ListArtifacts(ctx, id, "nope/missing", "1.0.0"); return err },
		"下载产物": func() error {
			_, err := f.svc.DownloadArtifact(ctx, id, "nope/missing", "1.0.0", "art-0", "a.proto")
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, model.CodeNotFound, apiErrorOf(t, call()).Code)
		})
	}
}

func TestOperationsOnMissingVersion(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, id, "people/basic", "1.0.0")

	err := f.svc.SetVersionStatus(ctx, id, "people/basic", "9.9.9", model.VersionDeprecated, "")
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)

	err = f.svc.DeleteVersion(ctx, id, "people/basic", "9.9.9")
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)

	err = f.svc.UploadArtifact(ctx, id, "people/basic", "9.9.9", "art-0", "a.proto",
		strings.NewReader("x"), 1)
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)
}

func TestArtifactNotFound(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", []any{
		map[string]any{"type": "api-docs", "files": []string{"openapi.json"}},
	})
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, id, "people/basic", req)
	require.NoError(t, err)

	// 产物 ID 不存在
	err = f.svc.UploadArtifact(ctx, id, "people/basic", "1.0.0", "art-9", "openapi.json",
		strings.NewReader("{}"), 2)
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)

	_, err = f.svc.DownloadArtifact(ctx, id, "people/basic", "1.0.0", "art-9", "openapi.json")
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)

	// 产物存在但文件名不在列表里
	_, err = f.svc.DownloadArtifact(ctx, id, "people/basic", "1.0.0", "art-0", "别的文件.json")
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)

	// 文件已声明但还没上传
	_, err = f.svc.DownloadArtifact(ctx, id, "people/basic", "1.0.0", "art-0", "openapi.json")
	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeNotFound, e.Code)
	assert.Contains(t, e.Message, "尚未上传")
}

// ============================================================
// 参数校验
// ============================================================

func TestInvalidStatusValues(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, id, "people/basic", "1.0.0")

	err := f.svc.SetVersionStatus(ctx, id, "people/basic", "1.0.0", "随便什么状态", "")
	assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)

	admin := f.promoteAdmin(t, id)
	err = f.svc.SetComponentStatus(ctx, admin, "people/basic", "随便什么状态")
	assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)
}

func TestAccessPolicyValidation(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, id, "mycompany/approval", "1.0.0")

	err := f.svc.SetAccessPolicies(ctx, id, "mycompany/approval", []model.AccessPolicy{
		{TargetType: "robot", TargetID: "x"},
	})
	assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)

	err = f.svc.SetAccessPolicies(ctx, id, "mycompany/approval", []model.AccessPolicy{
		{TargetType: model.TargetUser},
	})
	assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)
}

func TestListAccessPolicies(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	other := f.registerUser(t, "lisi")
	ctx := context.Background()
	f.publish(t, owner, "mycompany/approval", "1.0.0")

	require.NoError(t, f.svc.SetAccessPolicies(ctx, owner, "mycompany/approval", []model.AccessPolicy{
		{TargetType: model.TargetUser, TargetID: other.UserID, Permission: "read"},
	}))

	policies, err := f.svc.ListAccessPolicies(ctx, owner, "mycompany/approval")
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, other.UserID, policies[0].TargetID)

	// 非所有者不能查看策略（策略本身也是敏感信息）
	_, err = f.svc.ListAccessPolicies(ctx, other, "mycompany/approval")
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)
}

// ============================================================
// 管理员与所有者
// ============================================================

// 管理员可以代替所有者发布、改可见性、删版本（运维兜底能力）。
func TestAdminActsOnBehalfOfOwner(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "people/basic", "1.0.0")
	admin := f.promoteAdmin(t, owner)

	_, err := f.svc.Publish(ctx, admin, "people/basic", publishRequest(t, "people/basic", "2.0.0", nil))
	require.NoError(t, err)

	require.NoError(t, f.svc.SetVisibility(ctx, admin, "people/basic", model.VisibilityPrivate))
	require.NoError(t, f.svc.DeleteVersion(ctx, admin, "people/basic", "2.0.0"))

	// owner 字段不因管理员操作而改变
	c, err := f.repo.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, owner.UserID, c.OwnerID)
}

// 发布新版本不会悄悄改变已有组件的可见性与所有者。
func TestPublishKeepsExistingVisibility(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()
	f.publish(t, owner, "mycompany/approval", "1.0.0")
	require.NoError(t, f.svc.SetVisibility(ctx, owner, "mycompany/approval", model.VisibilityPrivate))

	f.publish(t, owner, "mycompany/approval", "2.0.0")

	c, err := f.repo.GetComponent(ctx, "mycompany/approval")
	require.NoError(t, err)
	assert.Equal(t, model.VisibilityPrivate, c.Visibility)
}

// 首次发布可以直接指定 private（发布即私有，不留空窗期）。
func TestPublishWithVisibility(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "mycompany/approval", "1.0.0", nil)
	req.Visibility = model.VisibilityPrivate
	_, err := f.svc.Publish(ctx, owner, "mycompany/approval", req)
	require.NoError(t, err)

	c, err := f.repo.GetComponent(ctx, "mycompany/approval")
	require.NoError(t, err)
	assert.Equal(t, model.VisibilityPrivate, c.Visibility)

	_, err = f.svc.GetManifest(ctx, service.Anonymous(), "mycompany/approval", "1.0.0")
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)
}

// 没有文件产物的版本可以直接转 stable（只有 container 引用时无须上传）。
func TestSetStableWithoutFileArtifacts(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "zhangsan")
	ctx := context.Background()

	req := publishRequest(t, "people/basic", "1.0.0", []any{
		map[string]any{"type": "container", "reference": "registry.example.com/x:1.0.0"},
	})
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, owner, "people/basic", req)
	require.NoError(t, err)

	assert.NoError(t, f.svc.SetVersionStatus(ctx, owner, "people/basic", "1.0.0", model.VersionStable, ""))
}

// ============================================================
// 底层故障
// ============================================================

// failingRepo 在指定操作上注入故障，用于验证"内部错误不泄漏实现细节"。
type failingRepo struct {
	repo.Repository
	failOn string
	err    error
}

func (f *failingRepo) UpsertComponent(ctx context.Context, c *model.Component) error {
	if f.failOn == "UpsertComponent" {
		return f.err
	}
	return f.Repository.UpsertComponent(ctx, c)
}

func (f *failingRepo) GetComponent(ctx context.Context, id string) (*model.Component, error) {
	if f.failOn == "GetComponent" {
		return nil, f.err
	}
	return f.Repository.GetComponent(ctx, id)
}

func (f *failingRepo) ListComponents(ctx context.Context, q repo.ComponentQuery) ([]model.Component, error) {
	if f.failOn == "ListComponents" {
		return nil, f.err
	}
	return f.Repository.ListComponents(ctx, q)
}

func (f *failingRepo) CreateUser(ctx context.Context, u *model.User) error {
	if f.failOn == "CreateUser" {
		return f.err
	}
	return f.Repository.CreateUser(ctx, u)
}

func (f *failingRepo) GetUserByUsername(ctx context.Context, name string) (*model.User, error) {
	if f.failOn == "GetUserByUsername" {
		return nil, f.err
	}
	return f.Repository.GetUserByUsername(ctx, name)
}

func (f *failingRepo) GetToken(ctx context.Context, token string) (*model.Token, error) {
	if f.failOn == "GetToken" {
		return nil, f.err
	}
	return f.Repository.GetToken(ctx, token)
}

func (f *failingRepo) DeleteToken(ctx context.Context, token string) error {
	if f.failOn == "DeleteToken" {
		return f.err
	}
	return f.Repository.DeleteToken(ctx, token)
}

func newFailingService(t *testing.T, failOn string) *service.Service {
	t.Helper()
	return service.New(
		&failingRepo{Repository: repo.NewMemory(), failOn: failOn, err: errors.New("数据库连接断了")},
		storage.NewMemory(),
		service.Options{BcryptCost: bcrypt.MinCost},
	)
}

// 底层故障一律转成 INTERNAL，且不把 SQL / 连接串之类的细节写进 message。
func TestInternalErrorsAreWrapped(t *testing.T) {
	ctx := context.Background()

	cases := map[string]func(*service.Service) error{
		"注册": func(s *service.Service) error {
			_, err := s.Register(ctx, service.RegisterRequest{Username: "u", Password: "correct-horse-battery"})
			return err
		},
		"登录": func(s *service.Service) error {
			_, err := s.Login(ctx, "u", "correct-horse-battery")
			return err
		},
		"注销": func(s *service.Service) error { return s.Logout(ctx, "tok") },
		"鉴权": func(s *service.Service) error {
			_, err := s.Authenticate(ctx, "tok")
			return err
		},
	}
	failOn := map[string]string{
		"注册": "CreateUser", "登录": "GetUserByUsername", "注销": "DeleteToken", "鉴权": "GetToken",
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call(newFailingService(t, failOn[name]))
			e := apiErrorOf(t, err)
			assert.Equal(t, model.CodeInternal, e.Code)
			assert.Equal(t, "市场内部错误", e.Message, "对外只说内部错误，细节放 details 供服务端日志用")
			assert.Contains(t, e.Details["cause"], "数据库连接断了")
		})
	}
}

func TestSearchInternalError(t *testing.T) {
	svc := newFailingService(t, "ListComponents")
	_, err := svc.SearchComponents(context.Background(), service.Anonymous(), repo.ComponentQuery{})
	assert.Equal(t, model.CodeInternal, apiErrorOf(t, err).Code)
}

func TestPublishInternalError(t *testing.T) {
	ctx := context.Background()
	svc := newFailingService(t, "GetComponent")
	_, err := svc.Register(ctx, service.RegisterRequest{Username: "u", Password: "correct-horse-battery"})
	require.NoError(t, err)
	token, err := svc.Login(ctx, "u", "correct-horse-battery")
	require.NoError(t, err)
	id, err := svc.Authenticate(ctx, token.Token)
	require.NoError(t, err)

	_, err = svc.Publish(ctx, id, "people/basic", publishRequest(t, "people/basic", "1.0.0", nil))
	assert.Equal(t, model.CodeInternal, apiErrorOf(t, err).Code)
}

// 令牌对应的用户被删掉后，令牌立即失效。
func TestAuthenticateWithOrphanToken(t *testing.T) {
	r := repo.NewMemory()
	svc := service.New(r, storage.NewMemory(), service.Options{BcryptCost: bcrypt.MinCost})
	ctx := context.Background()

	require.NoError(t, r.CreateToken(ctx, &model.Token{
		Token: "tok-orphan", UserID: "user-gone", Username: "gone",
		ExpiresAt: time.Now().Add(time.Hour),
	}))

	_, err := svc.Authenticate(ctx, "tok-orphan")
	e := apiErrorOf(t, err)
	assert.Equal(t, model.CodeUnauthorized, e.Code)
	assert.Contains(t, e.Message, "用户已不存在")
}

// 令牌两端的空白不影响认证（HTTP 头里常见）。
func TestAuthenticateTrimsWhitespace(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.registerUser(t, "zhangsan")
	token, err := f.svc.Login(ctx, "zhangsan", "correct-horse-battery")
	require.NoError(t, err)

	id, err := f.svc.Authenticate(ctx, "  "+token.Token+"  ")
	require.NoError(t, err)
	assert.Equal(t, "zhangsan", id.Username)

	anon, err := f.svc.Authenticate(ctx, "   ")
	require.NoError(t, err)
	assert.True(t, anon.Anonymous)
}
