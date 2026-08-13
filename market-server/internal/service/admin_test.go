// 本文件是 Step 18-D 管理员引导（运维指南 §6.5）的业务行为测试。
//
// 市场刚部署完时库里一个用户都没有，而 blocked 只有管理员能标（007 §6.3）。
// 所以服务启动时要按 ADMIN_USERNAME / ADMIN_PASSWORD 把管理员准备好。
package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
	"github.com/brickkit/market-server/internal/service"
)

func TestEnsureAdminCreatesAdminOnFirstStart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.EnsureAdmin(ctx, "root", "admin-password"))

	token, err := f.svc.Login(ctx, "root", "admin-password")
	require.NoError(t, err, "引导出来的管理员必须能直接登录")

	id, err := f.svc.Authenticate(ctx, token.Token)
	require.NoError(t, err)
	assert.True(t, id.IsAdmin, "引导出来的账号必须带管理员权限")
}

// 重启会再跑一次引导，不能因为账号已存在就启动失败。
func TestEnsureAdminIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.svc.EnsureAdmin(ctx, "root", "admin-password"))

	require.NoError(t, f.svc.EnsureAdmin(ctx, "root", "admin-password"))

	_, err := f.svc.Login(ctx, "root", "admin-password")
	assert.NoError(t, err, "重复引导不能把原账号弄坏")
}

// 已经普通注册过的同名用户，引导时要补上管理员权限，
// 否则运维会拿着 .env 里的账号却发现自己不是管理员。
func TestEnsureAdminPromotesExistingUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.svc.Register(ctx, service.RegisterRequest{Username: "root", Password: "their-own-password"})
	require.NoError(t, err)

	require.NoError(t, f.svc.EnsureAdmin(ctx, "root", "admin-password"))

	// 口令不被覆盖：引导只负责权限，不负责改密码
	token, err := f.svc.Login(ctx, "root", "their-own-password")
	require.NoError(t, err, "引导不该改掉已有用户的口令")

	id, err := f.svc.Authenticate(ctx, token.Token)
	require.NoError(t, err)
	assert.True(t, id.IsAdmin)
}

// 没配管理员时（比如本地跑内存版）跳过即可，不是错误。
func TestEnsureAdminSkipsWhenNotConfigured(t *testing.T) {
	f := newFixture(t)

	require.NoError(t, f.svc.EnsureAdmin(context.Background(), "", ""))

	_, err := f.repo.GetUserByUsername(context.Background(), "")
	assert.Error(t, err, "不该建出一个空用户名的账号")
}

// ============================================================
// 管理员口令重置（运维指南 §9 Q5：忘记管理员密码）
// ============================================================

// 引导不覆盖口令（D118），所以"忘记密码"必须有一条显式的重置路径，
// 否则运维只能去库里改哈希。
func TestResetAdminPasswordChangesCredentials(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.svc.EnsureAdmin(ctx, "root", "old-password"))

	require.NoError(t, f.svc.ResetAdminPassword(ctx, "root", "new-password"))

	_, err := f.svc.Login(ctx, "root", "new-password")
	assert.NoError(t, err, "新口令必须可用")

	_, err = f.svc.Login(ctx, "root", "old-password")
	assert.Error(t, err, "旧口令必须失效")
}

// 重置口令的场景就是"凭据可能已经泄漏"，所以已签发的令牌必须一并作废——
// 否则拿到旧 Token 的人照样能用市场。
func TestResetAdminPasswordRevokesExistingTokens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.svc.EnsureAdmin(ctx, "root", "old-password"))

	stolen, err := f.svc.Login(ctx, "root", "old-password")
	require.NoError(t, err)
	_, err = f.svc.Authenticate(ctx, stolen.Token)
	require.NoError(t, err, "重置前该令牌是有效的")

	require.NoError(t, f.svc.ResetAdminPassword(ctx, "root", "new-password"))

	_, err = f.svc.Authenticate(ctx, stolen.Token)
	assert.Error(t, err, "重置口令后旧令牌必须失效")
}

// 重置的对象如果本来不是管理员，重置后也要是管理员：
// 这条路径的存在意义就是"把管理员账号救回来"。
func TestResetAdminPasswordEnsuresAdminFlag(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.svc.Register(ctx, service.RegisterRequest{Username: "root", Password: "old-password"})
	require.NoError(t, err)

	require.NoError(t, f.svc.ResetAdminPassword(ctx, "root", "new-password"))

	token, err := f.svc.Login(ctx, "root", "new-password")
	require.NoError(t, err)
	id, err := f.svc.Authenticate(ctx, token.Token)
	require.NoError(t, err)
	assert.True(t, id.IsAdmin)
}

// 账号不存在时要说清楚，而不是静默成功让运维以为改好了。
func TestResetAdminPasswordFailsForUnknownUser(t *testing.T) {
	f := newFixture(t)

	err := f.svc.ResetAdminPassword(context.Background(), "nobody", "new-password")

	require.Error(t, err)
	assert.Equal(t, model.CodeNotFound, apiErrorOf(t, err).Code)
}

// 弱口令不能通过重置这条路溜进来。
func TestResetAdminPasswordRejectsWeakPassword(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.svc.EnsureAdmin(ctx, "root", "old-password"))

	err := f.svc.ResetAdminPassword(ctx, "root", "123")

	require.Error(t, err)
	assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)

	_, loginErr := f.svc.Login(ctx, "root", "old-password")
	assert.NoError(t, loginErr, "校验失败时不该动原口令")
}

// 引导管理员这件事本身要留痕（008 §审计：权限变更必须可追溯）。
func TestEnsureAdminIsAudited(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.EnsureAdmin(ctx, "root", "admin-password"))

	entries, err := f.repo.ListAudit(ctx, repo.AuditQuery{Action: model.ActionUserRegistered})
	require.NoError(t, err)

	found := false
	for _, e := range entries {
		if e.Action == model.ActionUserRegistered && e.Operator == "root" {
			found = true
		}
	}
	assert.True(t, found, "管理员引导应留下注册审计，实际：%v", entries)
}
