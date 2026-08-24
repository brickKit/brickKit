package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
)

// ============================================================
// 007 §14.2 命名空间归属
// ============================================================

// 官方命名空间只有市场管理员能首次创建。
//
// 不拦的话，任何注册用户都能发布 `brickkit/saga-orchestrator`——
// 而设计书把这个前缀写成"官方组件"，使用者据此判断可不可信。
// 那是一次冒名，且正好落在签名机制的盲区里（还没配公钥的项目验不了签）。
func TestReservedScopesRequireAdmin(t *testing.T) {
	f := newFixture(t)
	user := f.registerUser(t, "zhangsan")

	for _, id := range []string{"brickkit/saga-orchestrator", "infra/redis-event-bus"} {
		_, err := f.svc.Publish(context.Background(), user, id, publishRequest(t, id, "1.0.0", nil))

		require.Error(t, err, id)
		assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code, id)
		assert.Contains(t, err.Error(), "市场管理员", id)
	}
}

// 管理员可以在官方命名空间里发布。
func TestAdminCanPublishToReservedScope(t *testing.T) {
	f := newFixture(t)
	admin := f.promoteAdmin(t, f.registerUser(t, "ops"))

	v := f.publish(t, admin, "brickkit/saga-orchestrator", "1.0.0")

	assert.Equal(t, "1.0.0", v.Version)
}

// 其余命名空间先到先得——与 npm 的 unscoped 包名同一个模型。
//
// 刻意不做"组织名/ 必须是该组织成员"那一套：它需要一张命名空间注册表
// （申请、审批、转让、争议处理），而抢注一个名字**并不能劫持别人的组件**——
// 首次发布者成为 owner，之后每个版本都要过 requireOwner。
func TestOtherScopesAreFirstComeFirstServed(t *testing.T) {
	f := newFixture(t)
	zhangsan := f.registerUser(t, "zhangsan")

	v := f.publish(t, zhangsan, "mycompany/approval", "1.0.0")
	assert.Equal(t, "1.0.0", v.Version)

	// 但别人抢不走它：第二个版本要 owner
	lisi := f.registerUser(t, "lisi")
	_, err := f.svc.Publish(context.Background(), lisi, "mycompany/approval",
		publishRequest(t, "mycompany/approval", "1.1.0", nil))

	require.Error(t, err)
	assert.Equal(t, model.CodeForbidden, apiErrorOf(t, err).Code)
}
