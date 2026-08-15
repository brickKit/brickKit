package service_test

// 本文件是 P24「组织管理」（007 §9.5）的业务行为测试。
//
// 这三个端点不只是"补几个接口"：它们是**组织成员关系的唯一入口**。
// 在它们存在之前，成员关系只能靠注册时自报 orgId——那等于没有门。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/service"
)

// reload 重新读一遍身份：加入组织之后 OrgID 变了，
// 而 Identity 是登录那一刻的快照。
func (f *fixture) reload(t *testing.T, id *service.Identity) *service.Identity {
	t.Helper()

	user, err := f.repo.GetUserByID(context.Background(), id.UserID)
	require.NoError(t, err)
	return &service.Identity{
		UserID: user.UserID, Username: user.Username, OrgID: user.OrgID, IsAdmin: user.IsAdmin,
	}
}

// ============================================================
// 越权：注册时自报组织
// ============================================================

// 注册时自报的 orgId 必须被忽略。
//
// 访问控制按 OrgID 放行 private 组件（service/auth.go 的 canRead）。
// 只要注册时能自己填 orgId，任何人都可以写上 "org-acme" 注册一个账号，
// 然后把该组织的**全部 private 组件**读个遍——详情、Manifest、产物，一样不落。
// 组织成员关系只能由组织所有者或管理员通过 AddMember 建立。
func TestRegisterIgnoresSelfClaimedOrg(t *testing.T) {
	f := newFixture(t)

	user, err := f.svc.Register(context.Background(), service.RegisterRequest{
		Username: "outsider", Password: "correct-horse-battery", OrgID: "org-acme",
	})

	require.NoError(t, err)
	assert.Empty(t, user.OrgID, "自报的组织不能生效")
}

// ============================================================
// 创建组织
// ============================================================

func TestCreateOrganization(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "alice")

	org, err := f.svc.CreateOrganization(context.Background(), owner,
		service.CreateOrganizationRequest{Name: "Acme 公司"})

	require.NoError(t, err)
	assert.NotEmpty(t, org.OrgID)
	assert.Equal(t, "Acme 公司", org.Name)
	assert.Equal(t, owner.UserID, org.OwnerID, "创建者就是所有者")
}

// 创建者自动成为成员：否则他建完组织自己还进不去，只能再给自己加一次。
func TestCreateOrganizationMakesCreatorAMember(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "alice")

	org, err := f.svc.CreateOrganization(context.Background(), owner,
		service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)

	user, err := f.repo.GetUserByID(context.Background(), owner.UserID)
	require.NoError(t, err)
	assert.Equal(t, org.OrgID, user.OrgID)
}

func TestCreateOrganizationRequiresLogin(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.CreateOrganization(context.Background(), service.Anonymous(),
		service.CreateOrganizationRequest{Name: "Acme"})

	require.Error(t, err)
	assert.Equal(t, model.CodeUnauthorized, apiErrorOf(t, err).Code)
}

func TestCreateOrganizationRequiresName(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "alice")

	_, err := f.svc.CreateOrganization(context.Background(), owner,
		service.CreateOrganizationRequest{Name: "  "})

	require.Error(t, err)
	assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)
}

// 一个人只能属于一个组织（users.org_id 是单值，007 §10 的数据模型如此）。
// 已在别的组织里的人再建一个，必须明确报错，不能悄悄把他挪走。
func TestCreateOrganizationWhenAlreadyInOne(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.registerUser(t, "alice")
	_, err := f.svc.CreateOrganization(ctx, alice, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)

	alice = f.reload(t, alice)
	_, err = f.svc.CreateOrganization(ctx, alice, service.CreateOrganizationRequest{Name: "另一家"})

	require.Error(t, err)
	assert.Equal(t, model.CodeConflict, apiErrorOf(t, err).Code)
}

// ============================================================
// 添加成员
// ============================================================

func TestAddMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.registerUser(t, "alice")
	bob := f.registerUser(t, "bob")

	org, err := f.svc.CreateOrganization(ctx, owner, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)

	err = f.svc.AddOrganizationMember(ctx, f.reload(t, owner), org.OrgID,
		service.AddMemberRequest{Username: "bob"})

	require.NoError(t, err)
	user, err := f.repo.GetUserByID(ctx, bob.UserID)
	require.NoError(t, err)
	assert.Equal(t, org.OrgID, user.OrgID)
}

// 只有所有者或管理员能加人。
//
// 不然任何人都能把自己加进任何组织——和自报 orgId 是同一个洞。
func TestAddMemberRequiresOwnerOrAdmin(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.registerUser(t, "alice")
	mallory := f.registerUser(t, "mallory")

	org, err := f.svc.CreateOrganization(ctx, owner, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)

	err = f.svc.AddOrganizationMember(ctx, mallory, org.OrgID,
		service.AddMemberRequest{Username: "mallory"})

	require.Error(t, err)
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)
}

func TestAdminCanAddMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.registerUser(t, "alice")
	f.registerUser(t, "bob")
	admin := f.promoteAdmin(t, owner)

	org, err := f.svc.CreateOrganization(ctx, owner, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)

	err = f.svc.AddOrganizationMember(ctx, admin, org.OrgID,
		service.AddMemberRequest{Username: "bob"})

	require.NoError(t, err)
}

func TestAddMemberUnknownOrganization(t *testing.T) {
	f := newFixture(t)
	owner := f.registerUser(t, "alice")

	err := f.svc.AddOrganizationMember(context.Background(), owner, "org-不存在",
		service.AddMemberRequest{Username: "alice"})

	require.Error(t, err)
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)
}

func TestAddMemberUnknownUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.registerUser(t, "alice")
	org, err := f.svc.CreateOrganization(ctx, owner, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)

	err = f.svc.AddOrganizationMember(ctx, f.reload(t, owner), org.OrgID,
		service.AddMemberRequest{Username: "查无此人"})

	require.Error(t, err)
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)
}

// 已在别的组织里的人，不能被悄悄挪过来。
func TestAddMemberAlreadyInAnotherOrganization(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.registerUser(t, "alice")
	bob := f.registerUser(t, "bob")

	acme, err := f.svc.CreateOrganization(ctx, alice, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)
	other, err := f.svc.CreateOrganization(ctx, bob, service.CreateOrganizationRequest{Name: "另一家"})
	require.NoError(t, err)
	require.NotEqual(t, acme.OrgID, other.OrgID)

	err = f.svc.AddOrganizationMember(ctx, f.reload(t, alice), acme.OrgID,
		service.AddMemberRequest{Username: "bob"})

	require.Error(t, err)
	assert.Equal(t, model.CodeConflict, apiErrorOf(t, err).Code)
}

// 重复添加同一个人不算错：幂等，运维脚本可以放心重跑。
func TestAddMemberIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.registerUser(t, "alice")
	f.registerUser(t, "bob")
	org, err := f.svc.CreateOrganization(ctx, owner, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)

	owner = f.reload(t, owner)
	require.NoError(t, f.svc.AddOrganizationMember(ctx, owner, org.OrgID,
		service.AddMemberRequest{Username: "bob"}))

	err = f.svc.AddOrganizationMember(ctx, owner, org.OrgID, service.AddMemberRequest{Username: "bob"})

	assert.NoError(t, err)
}

// ============================================================
// 查询组织列表
// ============================================================

// 普通用户只看得到自己所属的组织：组织名与成员关系本身就是信息。
func TestListOrganizationsShowsOwnOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.registerUser(t, "alice")
	bob := f.registerUser(t, "bob")

	acme, err := f.svc.CreateOrganization(ctx, alice, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)
	_, err = f.svc.CreateOrganization(ctx, bob, service.CreateOrganizationRequest{Name: "另一家"})
	require.NoError(t, err)

	orgs, err := f.svc.ListOrganizations(ctx, f.reload(t, alice))

	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, acme.OrgID, orgs[0].OrgID)
}

// 管理员看得到全部。
func TestListOrganizationsAsAdmin(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.registerUser(t, "alice")
	bob := f.registerUser(t, "bob")
	admin := f.promoteAdmin(t, alice)

	_, err := f.svc.CreateOrganization(ctx, alice, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)
	_, err = f.svc.CreateOrganization(ctx, bob, service.CreateOrganizationRequest{Name: "另一家"})
	require.NoError(t, err)

	orgs, err := f.svc.ListOrganizations(ctx, admin)

	require.NoError(t, err)
	assert.Len(t, orgs, 2)
}

func TestListOrganizationsRequiresLogin(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.ListOrganizations(context.Background(), service.Anonymous())

	require.Error(t, err)
	assert.Equal(t, model.CodeUnauthorized, apiErrorOf(t, err).Code)
}

// 不属于任何组织时返回空列表，不是错误。
func TestListOrganizationsWhenNotAMember(t *testing.T) {
	f := newFixture(t)
	alice := f.registerUser(t, "alice")

	orgs, err := f.svc.ListOrganizations(context.Background(), alice)

	require.NoError(t, err)
	assert.Empty(t, orgs)
}

// ============================================================
// 组织成员关系真的能换来访问权（与 private 组件打通）
// ============================================================

// 这是组织存在的全部意义：把 private 组件按组织授权出去。
func TestOrganizationMemberCanReadPrivateComponent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.registerUser(t, "alice")
	bob := f.registerUser(t, "bob")

	org, err := f.svc.CreateOrganization(ctx, owner, service.CreateOrganizationRequest{Name: "Acme"})
	require.NoError(t, err)
	owner = f.reload(t, owner)

	// owner 发布一个 private 组件，并授权给整个组织
	f.publish(t, owner, "acme/approval", "1.0.0")
	require.NoError(t, f.svc.SetVisibility(ctx, owner, "acme/approval", model.VisibilityPrivate))
	require.NoError(t, f.svc.SetAccessPolicies(ctx, owner, "acme/approval", []model.AccessPolicy{
		{TargetType: model.TargetOrganization, TargetID: org.OrgID, Permission: "read"},
	}))

	// bob 还不是成员：读不到
	_, err = f.svc.GetComponent(ctx, f.reload(t, bob), "acme/approval")
	require.Error(t, err, "还没入组织就不该读得到")

	// 加进组织之后：读得到
	require.NoError(t, f.svc.AddOrganizationMember(ctx, owner, org.OrgID,
		service.AddMemberRequest{Username: "bob"}))

	got, err := f.svc.GetComponent(ctx, f.reload(t, bob), "acme/approval")

	require.NoError(t, err)
	assert.Equal(t, "acme/approval", got.Component.ComponentID)
}
