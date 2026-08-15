// 本文件是 Step 18-B 仓储层的**行为契约测试**。
//
// 同一份测试跑两个实现：内存实现（始终跑）与 PostgreSQL 实现
// （设置 MARKET_TEST_DATABASE_URL 时跑）。两边语义必须完全一致——
// 否则单测全绿、上了真库才出问题。
package repo_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
)

// EnvTestDatabaseURL 指向用于集成测试的 PostgreSQL。未设置时跳过 PG 用例。
const EnvTestDatabaseURL = "MARKET_TEST_DATABASE_URL"

// TestRepositoryContract 是仓储层的行为契约。
func TestRepositoryContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		runContract(t, func(t *testing.T) repo.Repository {
			t.Helper()
			return repo.NewMemory()
		})
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv(EnvTestDatabaseURL)
		if dsn == "" {
			t.Skipf("未设置 %s，跳过 PostgreSQL 契约测试", EnvTestDatabaseURL)
		}
		runContract(t, func(t *testing.T) repo.Repository {
			t.Helper()
			r, err := repo.NewPostgres(dsn)
			require.NoError(t, err)
			require.NoError(t, r.Migrate(context.Background()))
			require.NoError(t, r.TruncateAll(context.Background()), "每个用例从干净的库开始")
			t.Cleanup(func() { _ = r.Close() })
			return r
		})
	})
}

// runContract 在给定实现上跑完整套行为契约。
func runContract(t *testing.T, newRepo func(t *testing.T) repo.Repository) {
	t.Helper()

	tests := map[string]func(*testing.T, repo.Repository){
		"组件的创建与查询":             testComponentCRUD,
		"组件搜索":                 testComponentSearch,
		"可见性与状态变更":             testVisibilityAndStatus,
		"下载计数":                 testDownloads,
		"版本创建与重复拒绝":            testVersionCreate,
		"版本列表与状态变更":            testVersionListAndStatus,
		"版本签名的存取":              testVersionSignature,
		"产物按版本独立存储":            testArtifactsPerVersion,
		"产物上传标记":               testArtifactUploadMark,
		"访问策略":                 testAccessPolicies,
		"用户与令牌":                testUsersAndTokens,
		"组织与成员":                testOrganizations,
		"审计只追加":                testAudit,
		"不存在的记录返回 ErrNotFound": testNotFound,
	}

	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			fn(t, newRepo(t))
		})
	}
}

// ============================================================
// 组件
// ============================================================

func newComponent(id string) *model.Component {
	return &model.Component{
		ComponentID: id,
		Name:        "组件 " + id,
		Description: "描述 " + id,
		Vendor:      "brickkit-official",
		Visibility:  model.VisibilityPublic,
		SourceType:  model.SourceTypeGit,
		GitURL:      "https://github.com/brickkit/" + id + ".git",
		Status:      model.ComponentActive,
		OwnerID:     "user-1",
		Tags:        []string{"demo", "backend"},
	}
}

func testComponentCRUD(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	c := newComponent("people/basic")

	require.NoError(t, r.UpsertComponent(ctx, c))

	got, err := r.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", got.ComponentID)
	assert.Equal(t, "组件 people/basic", got.Name)
	assert.Equal(t, model.SourceTypeGit, got.SourceType)
	assert.Equal(t, "user-1", got.OwnerID)
	assert.ElementsMatch(t, []string{"demo", "backend"}, got.Tags)
	assert.False(t, got.CreatedAt.IsZero(), "创建时间要落库")

	// 再次 upsert 是更新而不是报错
	c.Name = "改过的名字"
	c.Tags = []string{"demo"}
	require.NoError(t, r.UpsertComponent(ctx, c))

	got, err = r.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "改过的名字", got.Name)
	assert.Equal(t, []string{"demo"}, got.Tags, "标签是覆盖写，不是追加")
}

// 返回的对象必须是副本：调用方改了它不能影响库里的数据。
func testComponentSearch(t *testing.T, r repo.Repository) {
	ctx := context.Background()

	people := newComponent("people/basic")
	people.Tags = []string{"people", "master-data"}
	dept := newComponent("department/tree")
	dept.Tags = []string{"department", "master-data"}
	secret := newComponent("mycompany/approval")
	secret.Visibility = model.VisibilityPrivate
	secret.Tags = nil
	blocked := newComponent("evil/component")
	blocked.Status = model.ComponentBlocked
	blocked.Tags = nil

	for _, c := range []*model.Component{people, dept, secret, blocked} {
		require.NoError(t, r.UpsertComponent(ctx, c))
	}

	all, err := r.ListComponents(ctx, repo.ComponentQuery{})
	require.NoError(t, err)
	assert.Equal(t, []string{"department/tree", "mycompany/approval", "people/basic"}, idsOf(all),
		"默认不含 blocked，且按组件 ID 排序")

	// 关键字匹配 ID / 名称 / 描述
	found, err := r.ListComponents(ctx, repo.ComponentQuery{Keyword: "people"})
	require.NoError(t, err)
	assert.Equal(t, []string{"people/basic"}, idsOf(found))

	// 标签是交集过滤
	found, err = r.ListComponents(ctx, repo.ComponentQuery{Tags: []string{"master-data"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"department/tree", "people/basic"}, idsOf(found))

	found, err = r.ListComponents(ctx, repo.ComponentQuery{Tags: []string{"master-data", "people"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"people/basic"}, idsOf(found))

	// 可见性过滤 + 额外放行（有权访问的 private 组件）
	found, err = r.ListComponents(ctx, repo.ComponentQuery{
		Visibilities: []string{model.VisibilityPublic},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"department/tree", "people/basic"}, idsOf(found))

	found, err = r.ListComponents(ctx, repo.ComponentQuery{
		Visibilities: []string{model.VisibilityPublic},
		AlsoInclude:  []string{"mycompany/approval"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"department/tree", "mycompany/approval", "people/basic"}, idsOf(found))

	// blocked 只有显式要求时才出现
	found, err = r.ListComponents(ctx, repo.ComponentQuery{IncludeBlocked: true})
	require.NoError(t, err)
	assert.Contains(t, idsOf(found), "evil/component")

	// 分页
	page1, err := r.ListComponents(ctx, repo.ComponentQuery{Page: 1, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"department/tree", "mycompany/approval"}, idsOf(page1))

	page2, err := r.ListComponents(ctx, repo.ComponentQuery{Page: 2, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"people/basic"}, idsOf(page2))

	// 计数忽略分页，但沿用其余过滤条件（007 §4.2 的 total）
	total, err := r.CountComponents(ctx, repo.ComponentQuery{Page: 1, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, total)

	total, err = r.CountComponents(ctx, repo.ComponentQuery{Tags: []string{"master-data"}})
	require.NoError(t, err)
	assert.Equal(t, 2, total)

	total, err = r.CountComponents(ctx, repo.ComponentQuery{Keyword: "nothing-matches-this"})
	require.NoError(t, err)
	assert.Equal(t, 0, total)
}

func testVisibilityAndStatus(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	require.NoError(t, r.UpsertComponent(ctx, newComponent("people/basic")))

	require.NoError(t, r.SetVisibility(ctx, "people/basic", model.VisibilityPrivate))
	got, err := r.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, model.VisibilityPrivate, got.Visibility)

	require.NoError(t, r.SetComponentStatus(ctx, "people/basic", model.ComponentBlocked))
	got, err = r.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, model.ComponentBlocked, got.Status)

	assert.ErrorIs(t, r.SetVisibility(ctx, "nope/missing", model.VisibilityPrivate), repo.ErrNotFound)
	assert.ErrorIs(t, r.SetComponentStatus(ctx, "nope/missing", model.ComponentBlocked), repo.ErrNotFound)
}

func testDownloads(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	require.NoError(t, r.UpsertComponent(ctx, newComponent("people/basic")))

	require.NoError(t, r.AddDownload(ctx, "people/basic", 1))
	require.NoError(t, r.AddDownload(ctx, "people/basic", 2))

	got, err := r.GetComponent(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, int64(3), got.Downloads)
}

// ============================================================
// 版本
// ============================================================

func newVersion(componentID, version string) *model.Version {
	return &model.Version{
		ComponentID: componentID,
		Version:     version,
		Status:      model.VersionStable,
		Manifest:    json.RawMessage(`{"metadata":{"id":"` + componentID + `","version":"` + version + `"}}`),
		Changelog:   "首次发布",
		PublishedBy: "user-1",
	}
}

// 18.14 版本不可重复。
func testVersionCreate(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	require.NoError(t, r.UpsertComponent(ctx, newComponent("people/basic")))

	require.NoError(t, r.CreateVersion(ctx, newVersion("people/basic", "1.0.0")))

	err := r.CreateVersion(ctx, newVersion("people/basic", "1.0.0"))
	assert.ErrorIs(t, err, repo.ErrConflict, "同 ID 同版本不得重复发布")

	// 不同版本可以继续发
	require.NoError(t, r.CreateVersion(ctx, newVersion("people/basic", "2.0.0")))

	got, err := r.GetVersion(ctx, "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, model.VersionStable, got.Status)
	assert.Equal(t, "user-1", got.PublishedBy)
	assert.False(t, got.PublishedAt.IsZero())
	assert.JSONEq(t, `{"metadata":{"id":"people/basic","version":"1.0.0"}}`, string(got.Manifest))
}

// testVersionSignature 覆盖 20.6：签名必须真的**存进去**、真的**取得回来**。
//
// 这条测试针对的是一类特别难发现的错：`component_versions.signature_json` 这一列
// 早就在建表脚本里预留好了，但只要 SQL 语句里不写它，签名就在两条 SQL 之间静默
// 消失——发布接口返回的是内存里那个结构体，看上去一切正常；等使用者 add 时才
// 发现签名没了，而那时已经无从判断是发布者没签、还是市场弄丢了。
// 内存实现天然不会犯这个错（它存的就是结构体本身），所以只有契约测试能抓住它。
func testVersionSignature(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	require.NoError(t, r.UpsertComponent(ctx, newComponent("people/basic")))

	signed := newVersion("people/basic", "1.2.0")
	signed.Signature = &model.Signature{
		Algorithm:    model.AlgorithmCosign,
		PublicKeyRef: "keys/people-basic-release.pub",
		Value:        "MEUCIQDhJ4pQ3H8xN0mS1vZk2bYc9eR6tW7uX5oP4qL3aB2cDwIgFg==",
		SignedAt:     time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		SignedBy:     "release-bot@brickkit.io",
	}
	require.NoError(t, r.CreateVersion(ctx, signed))

	// 未签名的版本：签名必须是 nil，不能是一个字段全空的结构体
	require.NoError(t, r.CreateVersion(ctx, newVersion("people/basic", "1.1.0")))

	got, err := r.GetVersion(ctx, "people/basic", "1.2.0")
	require.NoError(t, err)
	require.NotNil(t, got.Signature, "签名必须真的落库")
	assert.Equal(t, model.AlgorithmCosign, got.Signature.Algorithm)
	assert.Equal(t, "keys/people-basic-release.pub", got.Signature.PublicKeyRef)
	assert.Equal(t, signed.Signature.Value, got.Signature.Value)
	assert.Equal(t, "release-bot@brickkit.io", got.Signature.SignedBy)
	assert.True(t, signed.Signature.SignedAt.Equal(got.Signature.SignedAt),
		"签名时间要原样取回（时区可以不同，时刻必须一致）")

	unsigned, err := r.GetVersion(ctx, "people/basic", "1.1.0")
	require.NoError(t, err)
	assert.Nil(t, unsigned.Signature, "没签名就是 nil，不能凭空造一个空签名出来")

	// 列表也要带签名：使用者要在列表上看出哪些版本签了
	versions, err := r.ListVersions(ctx, "people/basic")
	require.NoError(t, err)
	bySignature := map[string]*model.Signature{}
	for _, v := range versions {
		bySignature[v.Version] = v.Signature
	}
	require.NotNil(t, bySignature["1.2.0"])
	assert.Equal(t, signed.Signature.Value, bySignature["1.2.0"].Value)
	assert.Nil(t, bySignature["1.1.0"])

	// 改状态不能把签名弄丢（publish 是三步走，转 stable 在最后一步）
	require.NoError(t, r.SetVersionStatus(ctx, "people/basic", "1.2.0", model.VersionDeprecated))
	got, err = r.GetVersion(ctx, "people/basic", "1.2.0")
	require.NoError(t, err)
	require.NotNil(t, got.Signature, "改状态之后签名必须还在")
	assert.Equal(t, signed.Signature.Value, got.Signature.Value)
}

func testVersionListAndStatus(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	require.NoError(t, r.UpsertComponent(ctx, newComponent("people/basic")))
	for _, v := range []string{"1.0.0", "1.10.0", "1.2.0", "2.0.0"} {
		require.NoError(t, r.CreateVersion(ctx, newVersion("people/basic", v)))
	}

	versions, err := r.ListVersions(ctx, "people/basic")
	require.NoError(t, err)
	assert.Equal(t, []string{"2.0.0", "1.10.0", "1.2.0", "1.0.0"}, versionsOf(versions),
		"按版本号倒序，且 1.10.0 要排在 1.2.0 之前（数字比较，不是字符串比较）")

	// 状态变更：deprecated / blocked / 软删除
	require.NoError(t, r.SetVersionStatus(ctx, "people/basic", "1.0.0", model.VersionDeprecated))
	got, err := r.GetVersion(ctx, "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, model.VersionDeprecated, got.Status)

	// 软删除后记录仍在（007 §9.2：删除版本是软删除）
	require.NoError(t, r.SetVersionStatus(ctx, "people/basic", "2.0.0", model.VersionDeleted))
	got, err = r.GetVersion(ctx, "people/basic", "2.0.0")
	require.NoError(t, err)
	assert.Equal(t, model.VersionDeleted, got.Status)
	assert.False(t, got.Installable())

	assert.ErrorIs(t,
		r.SetVersionStatus(ctx, "people/basic", "9.9.9", model.VersionBlocked), repo.ErrNotFound)
}

// ============================================================
// 产物
// ============================================================

func artifacts(version string) []model.ArtifactRecord {
	return []model.ArtifactRecord{
		{
			ArtifactID: "art-0", Type: model.ArtifactTypeAPIContract, Format: "protobuf",
			Files: []string{"proto/people/v1/people.proto"},
		},
		{
			ArtifactID: "art-1", Type: "api-docs", Format: "openapi",
			Files: []string{"openapi.json"}, Description: "版本 " + version,
		},
		{
			ArtifactID: "art-2", Type: model.ArtifactTypeContainer,
			Reference: "registry.brickkit.io/people-basic:" + version,
		},
	}
}

// 18.22 artifacts 按版本独立存储。
func testArtifactsPerVersion(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	require.NoError(t, r.UpsertComponent(ctx, newComponent("people/basic")))
	require.NoError(t, r.CreateVersion(ctx, newVersion("people/basic", "1.0.0")))
	require.NoError(t, r.CreateVersion(ctx, newVersion("people/basic", "2.0.0")))

	require.NoError(t, r.PutArtifacts(ctx, "people/basic", "1.0.0", artifacts("1.0.0")))
	require.NoError(t, r.PutArtifacts(ctx, "people/basic", "2.0.0", artifacts("2.0.0")))

	v1, err := r.ListArtifacts(ctx, "people/basic", "1.0.0")
	require.NoError(t, err)
	require.Len(t, v1, 3)
	assert.Equal(t, "art-0", v1[0].ArtifactID)
	assert.Equal(t, []string{"proto/people/v1/people.proto"}, v1[0].Files)
	assert.Equal(t, "版本 1.0.0", v1[1].Description)
	assert.Equal(t, "registry.brickkit.io/people-basic:1.0.0", v1[2].Reference)

	v2, err := r.ListArtifacts(ctx, "people/basic", "2.0.0")
	require.NoError(t, err)
	require.Len(t, v2, 3)
	assert.Equal(t, "版本 2.0.0", v2[1].Description, "两个版本的产物互不干扰")

	// 覆盖写：重发同一版本的产物元数据不会叠加
	require.NoError(t, r.PutArtifacts(ctx, "people/basic", "1.0.0", artifacts("1.0.0")[:1]))
	v1, err = r.ListArtifacts(ctx, "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Len(t, v1, 1)

	one, err := r.GetArtifact(ctx, "people/basic", "1.0.0", "art-0")
	require.NoError(t, err)
	assert.Equal(t, model.ArtifactTypeAPIContract, one.Type)

	_, err = r.GetArtifact(ctx, "people/basic", "1.0.0", "art-9")
	assert.ErrorIs(t, err, repo.ErrNotFound)
}

func testArtifactUploadMark(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	require.NoError(t, r.UpsertComponent(ctx, newComponent("people/basic")))
	require.NoError(t, r.CreateVersion(ctx, newVersion("people/basic", "1.0.0")))
	require.NoError(t, r.PutArtifacts(ctx, "people/basic", "1.0.0", artifacts("1.0.0")))

	require.NoError(t, r.MarkArtifactUploaded(ctx, "people/basic", "1.0.0", "art-0", "proto/people/v1/people.proto"))
	// 重复标记同一个文件不应产生重复记录
	require.NoError(t, r.MarkArtifactUploaded(ctx, "people/basic", "1.0.0", "art-0", "proto/people/v1/people.proto"))

	got, err := r.GetArtifact(ctx, "people/basic", "1.0.0", "art-0")
	require.NoError(t, err)
	assert.Equal(t, []string{"proto/people/v1/people.proto"}, got.Uploaded)

	assert.ErrorIs(t,
		r.MarkArtifactUploaded(ctx, "people/basic", "1.0.0", "art-9", "x"), repo.ErrNotFound)
}

// ============================================================
// 访问策略
// ============================================================

func testAccessPolicies(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	require.NoError(t, r.UpsertComponent(ctx, newComponent("mycompany/approval")))

	policies := []model.AccessPolicy{
		{ComponentID: "mycompany/approval", TargetType: model.TargetUser, TargetID: "user-admin-1", Permission: "read"},
		{ComponentID: "mycompany/approval", TargetType: model.TargetOrganization, TargetID: "org-mycompany", Permission: "read"},
	}
	require.NoError(t, r.ReplaceAccessPolicies(ctx, "mycompany/approval", policies))

	got, err := r.ListAccessPolicies(ctx, "mycompany/approval")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// 覆盖写
	require.NoError(t, r.ReplaceAccessPolicies(ctx, "mycompany/approval", policies[:1]))
	got, err = r.ListAccessPolicies(ctx, "mycompany/approval")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "user-admin-1", got[0].TargetID)

	// 清空
	require.NoError(t, r.ReplaceAccessPolicies(ctx, "mycompany/approval", nil))
	got, err = r.ListAccessPolicies(ctx, "mycompany/approval")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ============================================================
// 用户与令牌
// ============================================================

func testUsersAndTokens(t *testing.T, r repo.Repository) {
	ctx := context.Background()
	user := &model.User{
		UserID: "user-1", Username: "zhangsan", Email: "zhangsan@example.com",
		PasswordHash: "hash", OrgID: "org-1",
	}

	require.NoError(t, r.CreateUser(ctx, user))
	assert.ErrorIs(t, r.CreateUser(ctx, &model.User{UserID: "user-2", Username: "zhangsan"}),
		repo.ErrConflict, "用户名唯一")

	got, err := r.GetUserByUsername(ctx, "zhangsan")
	require.NoError(t, err)
	assert.Equal(t, "user-1", got.UserID)
	assert.Equal(t, "hash", got.PasswordHash)
	assert.Equal(t, "org-1", got.OrgID)
	assert.False(t, got.CreatedAt.IsZero())

	got, err = r.GetUserByID(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, "zhangsan", got.Username)

	_, err = r.GetUserByUsername(ctx, "nobody")
	assert.ErrorIs(t, err, repo.ErrNotFound)

	// 管理员标记（运维指南 §6.5 启动引导会把配置里的账号提成管理员）
	assert.False(t, got.IsAdmin, "普通注册的用户默认不是管理员")
	require.NoError(t, r.SetUserAdmin(ctx, "user-1", true))
	got, err = r.GetUserByID(ctx, "user-1")
	require.NoError(t, err)
	assert.True(t, got.IsAdmin)
	assert.Equal(t, "hash", got.PasswordHash, "提权不该动口令")

	assert.ErrorIs(t, r.SetUserAdmin(ctx, "user-404", true), repo.ErrNotFound)

	// 改口令哈希（运维指南 §9 Q5 的重置路径）
	require.NoError(t, r.SetUserPassword(ctx, "user-1", "new-hash"))
	got, err = r.GetUserByID(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, "new-hash", got.PasswordHash)
	assert.True(t, got.IsAdmin, "改口令不该动权限")

	assert.ErrorIs(t, r.SetUserPassword(ctx, "user-404", "x"), repo.ErrNotFound)

	// 令牌
	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, r.CreateToken(ctx, &model.Token{
		Token: "tok-abc", UserID: "user-1", Username: "zhangsan", ExpiresAt: expires,
	}))

	tok, err := r.GetToken(ctx, "tok-abc")
	require.NoError(t, err)
	assert.Equal(t, "user-1", tok.UserID)
	assert.Equal(t, "zhangsan", tok.Username)
	assert.WithinDuration(t, expires, tok.ExpiresAt, time.Second)

	require.NoError(t, r.DeleteToken(ctx, "tok-abc"))
	_, err = r.GetToken(ctx, "tok-abc")
	assert.ErrorIs(t, err, repo.ErrNotFound)

	// 删除不存在的令牌是幂等的（登出两次不该报错）
	assert.NoError(t, r.DeleteToken(ctx, "tok-abc"))

	// 按用户吊销全部令牌（改口令时用：旧令牌必须一起失效）
	for _, tok := range []string{"tok-1", "tok-2"} {
		require.NoError(t, r.CreateToken(ctx, &model.Token{
			Token: tok, UserID: "user-1", Username: "zhangsan", ExpiresAt: expires,
		}))
	}
	require.NoError(t, r.CreateUser(ctx, &model.User{UserID: "user-9", Username: "lisi"}))
	require.NoError(t, r.CreateToken(ctx, &model.Token{
		Token: "tok-other", UserID: "user-9", Username: "lisi", ExpiresAt: expires,
	}))

	require.NoError(t, r.DeleteTokensOfUser(ctx, "user-1"))
	for _, tok := range []string{"tok-1", "tok-2"} {
		_, err = r.GetToken(ctx, tok)
		assert.ErrorIs(t, err, repo.ErrNotFound, "%s 应已被吊销", tok)
	}
	_, err = r.GetToken(ctx, "tok-other")
	assert.NoError(t, err, "不能误伤其他用户的令牌")

	// 该用户没有令牌时也不算错
	assert.NoError(t, r.DeleteTokensOfUser(ctx, "user-1"))
}

// ============================================================
// 审计
// ============================================================

func testAudit(t *testing.T, r repo.Repository) {
	ctx := context.Background()

	entries := []*model.AuditEntry{
		{Action: model.ActionVersionPublished, ComponentID: "people/basic", Version: "1.0.0", Operator: "zhangsan", Result: model.ResultSuccess},
		{Action: model.ActionVersionPublished, ComponentID: "department/tree", Version: "1.0.0", Operator: "lisi", Result: model.ResultSuccess},
		{Action: model.ActionVisibilityChanged, ComponentID: "people/basic", Operator: "zhangsan", Result: model.ResultSuccess},
	}
	for _, e := range entries {
		require.NoError(t, r.AppendAudit(ctx, e))
		assert.NotEmpty(t, e.AuditID, "落库后要回填审计 ID")
		assert.False(t, e.Time.IsZero(), "落库后要回填时间")
	}

	all, err := r.ListAudit(ctx, repo.AuditQuery{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, model.ActionVisibilityChanged, all[0].Action, "按时间倒序")

	byComponent, err := r.ListAudit(ctx, repo.AuditQuery{ComponentID: "people/basic"})
	require.NoError(t, err)
	assert.Len(t, byComponent, 2)

	byAction, err := r.ListAudit(ctx, repo.AuditQuery{Action: model.ActionVersionPublished})
	require.NoError(t, err)
	assert.Len(t, byAction, 2)

	limited, err := r.ListAudit(ctx, repo.AuditQuery{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, limited, 1)
}

// ============================================================
// 不存在的记录
// ============================================================

func testNotFound(t *testing.T, r repo.Repository) {
	ctx := context.Background()

	_, err := r.GetComponent(ctx, "nope/missing")
	assert.ErrorIs(t, err, repo.ErrNotFound)

	_, err = r.GetVersion(ctx, "nope/missing", "1.0.0")
	assert.ErrorIs(t, err, repo.ErrNotFound)

	_, err = r.GetUserByID(ctx, "nobody")
	assert.ErrorIs(t, err, repo.ErrNotFound)

	// 列表类查询对不存在的组件返回空而不是报错
	versions, err := r.ListVersions(ctx, "nope/missing")
	require.NoError(t, err)
	assert.Empty(t, versions)

	arts, err := r.ListArtifacts(ctx, "nope/missing", "1.0.0")
	require.NoError(t, err)
	assert.Empty(t, arts)

	policies, err := r.ListAccessPolicies(ctx, "nope/missing")
	require.NoError(t, err)
	assert.Empty(t, policies)
}

// ============================================================
// 辅助
// ============================================================

func idsOf(components []model.Component) []string {
	out := make([]string, 0, len(components))
	for _, c := range components {
		out = append(out, c.ComponentID)
	}
	return out
}

func versionsOf(versions []model.Version) []string {
	out := make([]string, 0, len(versions))
	for _, v := range versions {
		out = append(out, v.Version)
	}
	return out
}

// testOrganizations 是组织的行为契约（007 §9.5）。
//
// 成员关系记在 users.org_id 上，因此这套用例同时钉住"改组织"这件事
// 落在用户记录上，而不是另开一张成员表。
func testOrganizations(t *testing.T, r repo.Repository) {
	ctx := context.Background()

	require.NoError(t, r.CreateUser(ctx, &model.User{UserID: "user-1", Username: "alice"}))
	require.NoError(t, r.CreateUser(ctx, &model.User{UserID: "user-2", Username: "bob"}))

	org := &model.Organization{OrgID: "org-1", Name: "Acme", OwnerID: "user-1"}
	require.NoError(t, r.CreateOrganization(ctx, org))
	assert.False(t, org.CreatedAt.IsZero(), "创建时间由仓储补齐")

	t.Run("重复创建返回冲突", func(t *testing.T) {
		err := r.CreateOrganization(ctx, &model.Organization{OrgID: "org-1", Name: "别的", OwnerID: "user-2"})
		assert.ErrorIs(t, err, repo.ErrConflict)
	})

	t.Run("按 ID 查询", func(t *testing.T) {
		got, err := r.GetOrganization(ctx, "org-1")
		require.NoError(t, err)
		assert.Equal(t, "Acme", got.Name)
		assert.Equal(t, "user-1", got.OwnerID)
	})

	t.Run("查不到返回 ErrNotFound", func(t *testing.T) {
		_, err := r.GetOrganization(ctx, "org-不存在")
		assert.ErrorIs(t, err, repo.ErrNotFound)
	})

	t.Run("列表按创建时间排序", func(t *testing.T) {
		require.NoError(t, r.CreateOrganization(ctx,
			&model.Organization{OrgID: "org-2", Name: "另一家", OwnerID: "user-2"}))

		orgs, err := r.ListOrganizations(ctx)
		require.NoError(t, err)
		require.Len(t, orgs, 2)
		assert.Equal(t, "org-1", orgs[0].OrgID)
		assert.Equal(t, "org-2", orgs[1].OrgID)
	})

	t.Run("设置用户所属组织", func(t *testing.T) {
		require.NoError(t, r.SetUserOrg(ctx, "user-2", "org-1"))

		user, err := r.GetUserByID(ctx, "user-2")
		require.NoError(t, err)
		assert.Equal(t, "org-1", user.OrgID)
	})

	t.Run("退出组织写空串", func(t *testing.T) {
		require.NoError(t, r.SetUserOrg(ctx, "user-2", ""))

		user, err := r.GetUserByID(ctx, "user-2")
		require.NoError(t, err)
		assert.Empty(t, user.OrgID)
	})

	t.Run("给不存在的用户设组织返回 ErrNotFound", func(t *testing.T) {
		assert.ErrorIs(t, r.SetUserOrg(ctx, "user-不存在", "org-1"), repo.ErrNotFound)
	})
}
