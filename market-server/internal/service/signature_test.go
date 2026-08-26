package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
)

// 本文件覆盖开发计划 20.6：签名信息存储在版本记录中。
//
// 市场对签名做的事只有两件：**存下来**，以及**挡住结构上就不可能有效的签名**。
// 它不做密码学校验——见 TestPublishDoesNotCryptographicallyVerify 的说明。

func testSignature() *model.Signature {
	return &model.Signature{
		Algorithm:    model.AlgorithmCosign,
		PublicKeyRef: "keys/people-basic-release.pub",
		// 合法 base64（内容是不是真签名，市场无从判断）
		Value:    "MEUCIQDhJ4pQ3H8xN0mS1vZk2bYc9eR6tW7uX5oP4qL3aB2cDwIgFg==",
		SignedAt: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		SignedBy: "release-bot@brickkit.io",
	}
}

// signedPublish 构造一个带签名的发布请求。
func signedPublish(t *testing.T, componentID, version string, sig *model.Signature) model.PublishRequest {
	t.Helper()

	req := publishRequest(t, componentID, version, nil)
	req.Signature = sig
	return req
}

// TestPublishStoresSignature 是 20.6 的主干：发布时带签名，查询版本时拿得到。
func TestPublishStoresSignature(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.registerUser(t, "release-bot")

	sig := testSignature()
	created, err := f.svc.Publish(ctx, id, "people/basic", signedPublish(t, "people/basic", "1.2.0", sig))
	require.NoError(t, err)

	require.NotNil(t, created.Signature, "发布返回的版本记录就该带上签名")
	assert.Equal(t, model.AlgorithmCosign, created.Signature.Algorithm)
	assert.Equal(t, "keys/people-basic-release.pub", created.Signature.PublicKeyRef)
	assert.Equal(t, sig.Value, created.Signature.Value)
	assert.Equal(t, "release-bot@brickkit.io", created.Signature.SignedBy)
	assert.Equal(t, sig.SignedAt.UTC(), created.Signature.SignedAt.UTC())

	// 真正要紧的是**存住了**：重新查一遍还在
	stored, err := f.repo.GetVersion(ctx, "people/basic", "1.2.0")
	require.NoError(t, err)
	require.NotNil(t, stored.Signature)
	assert.Equal(t, sig.Value, stored.Signature.Value)
}

// TestGetManifestReturnsSignature 决定 CLI 能不能验签。
//
// CLI 在 add 时只请求这一个端点（007 §4.5）。签名不跟着 Manifest 一起回来，
// 使用者就得再发一次请求去猜它在哪儿——或者干脆验不了。
func TestGetManifestReturnsSignature(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.registerUser(t, "release-bot")

	sig := testSignature()
	_, err := f.svc.Publish(ctx, id, "people/basic", signedPublish(t, "people/basic", "1.2.0", sig))
	require.NoError(t, err)

	view, err := f.svc.GetManifest(ctx, id, "people/basic", "1.2.0")
	require.NoError(t, err)

	require.NotNil(t, view.Signature)
	assert.Equal(t, sig.Value, view.Signature.Value)
	assert.Equal(t, sig.PublicKeyRef, view.Signature.PublicKeyRef)
}

// TestListVersionsShowsSignature：列表要能看出哪些版本签了、哪些没签。
//
// 列表响应会剥掉 Manifest（它是最大的字段），签名不能一起被剥掉——
// "这个组件的哪些版本有签名"正是使用者要在列表上看的。
func TestListVersionsShowsSignature(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.registerUser(t, "release-bot")

	_, err := f.svc.Publish(ctx, id, "people/basic", signedPublish(t, "people/basic", "1.2.0", testSignature()))
	require.NoError(t, err)
	f.publish(t, id, "people/basic", "1.1.0") // 未签名的旧版本

	versions, err := f.svc.ListVersions(ctx, id, "people/basic")
	require.NoError(t, err)
	require.Len(t, versions, 2)

	byVersion := map[string]*model.Signature{}
	for _, v := range versions {
		byVersion[v.Version] = v.Signature
	}
	assert.NotNil(t, byVersion["1.2.0"], "签过名的版本要能看出来")
	assert.Nil(t, byVersion["1.1.0"], "没签名的版本就是没签名，不能凭空造一个")
}

// TestPublishWithoutSignatureIsAllowed：市场不强制签名。
//
// 强制与否是**使用者**的策略（installer.requireSignature，008 §8.5）：
// 同一个市场同时服务着"本地开发随便装"和"生产必须验签"的项目。
// 市场在发布侧一刀切，等于替所有使用者做了决定。
func TestPublishWithoutSignatureIsAllowed(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "release-bot")

	v := f.publish(t, id, "people/basic", "1.0.0")
	assert.Nil(t, v.Signature)
}

// TestPublishRejectsMalformedSignature：结构上就不可能有效的签名当场退回。
//
// 市场验不了密码学，但"算法不认识""value 不是 base64""没写 publicKeyRef"
// 这三种一眼就是错的。放进库里只会让每一个使用者在 add 时各自撞一次墙，
// 而那时已经查不到是谁、什么时候传坏的了。
func TestPublishRejectsMalformedSignature(t *testing.T) {
	cases := map[string]*model.Signature{
		"算法不认识": {
			Algorithm: "rsa-pkcs1", PublicKeyRef: "keys/a.pub", Value: "MEUCIQ==",
		},
		"缺算法": {
			PublicKeyRef: "keys/a.pub", Value: "MEUCIQ==",
		},
		"缺 publicKeyRef": {
			Algorithm: model.AlgorithmCosign, Value: "MEUCIQ==",
		},
		"value 为空": {
			Algorithm: model.AlgorithmCosign, PublicKeyRef: "keys/a.pub",
		},
		"value 不是 base64": {
			Algorithm: model.AlgorithmCosign, PublicKeyRef: "keys/a.pub", Value: "这不是签名",
		},
	}

	for name, sig := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			id := f.registerUser(t, "release-bot")

			_, err := f.svc.Publish(context.Background(), id, "people/basic",
				signedPublish(t, "people/basic", "1.2.0", sig))
			require.Error(t, err)
			assert.Equal(t, model.CodeInvalidRequest, apiErrorOf(t, err).Code)

			// 退回的版本不能在库里留下痕迹
			_, err = f.repo.GetVersion(context.Background(), "people/basic", "1.2.0")
			assert.Error(t, err, "被拒绝的发布不该留下版本记录")
		})
	}
}

// TestPublishDoesNotCryptographicallyVerify 记录一处与设计书的**有意偏离**。
//
// 008 §8.2 的时序图里有一步 "Market->>Market: 校验签名（使用公钥）"。市场做不到，
// 而且不该假装做得到：它手里没有任何可信的公钥。若让发布者连公钥一起上传，
// 那就是自己给自己发证——攻击者拿到发布 Token 后，用自己的密钥对签名、连公钥
// 一起传，"校验"照样通过。这种校验比没有更糟，因为它会让人以为验过了。
//
// 真正的校验在 CLI 侧，公钥来自使用者的 installer.publicKeys（008 §8.4）。
// 市场若要做有意义的校验，前提是发布者公钥在账号下登记并有独立的变更审计——
// 那是另一件事，已登记为延后项。
func TestPublishDoesNotCryptographicallyVerify(t *testing.T) {
	f := newFixture(t)
	id := f.registerUser(t, "release-bot")

	// 结构完全合法、但内容是彻头彻尾的假签名
	sig := testSignature()
	sig.Value = "aGVsbG8gd29ybGQ=" // base64("hello world")

	v, err := f.svc.Publish(context.Background(), id, "people/basic",
		signedPublish(t, "people/basic", "1.2.0", sig))
	require.NoError(t, err, "市场只做结构校验，不做密码学校验")
	require.NotNil(t, v.Signature)
	assert.Equal(t, "aGVsbG8gd29ybGQ=", v.Signature.Value)
}

// TestSignatureSurvivesStatusChange：改版本状态不能把签名弄丢。
//
// publish 是三步走（建 draft → 传产物 → 转 stable），签名在第一步写入。
// 第三步若整条记录覆盖回去，签名就在使用者完全看不见的地方消失了。
func TestSignatureSurvivesStatusChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.registerUser(t, "release-bot")

	req := signedPublish(t, "people/basic", "1.2.0", testSignature())
	req.Status = model.VersionDraft
	_, err := f.svc.Publish(ctx, id, "people/basic", req)
	require.NoError(t, err)

	require.NoError(t, f.svc.SetVersionStatus(ctx, id, "people/basic", "1.2.0", model.VersionStable, ""), "")

	stored, err := f.repo.GetVersion(ctx, "people/basic", "1.2.0")
	require.NoError(t, err)
	require.NotNil(t, stored.Signature, "转 stable 之后签名必须还在")
	assert.Equal(t, model.VersionStable, stored.Status)
}
