package main

import (
	"context"
	"os"
	"sort"
	"testing"
)

// 本文件是存储层的**行为契约测试**：内存实现与 PostgreSQL 实现跑同一份用例。
//
// 两边语义必须完全一致——尤其是 GrantsFor 的"人 + 部门"合并查询，
// SQL 那条 WHERE 稍有差错就会漏授或越授，而只测内存实现的话完全看不出来。

const envTestDatabaseURL = "RBAC_TEST_DATABASE_URL"

func TestStoreContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		runStoreContract(t, func(t *testing.T) Store {
			t.Helper()
			return newMemoryStore(testRoles(), testGrants())
		})
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv(envTestDatabaseURL)
		if dsn == "" {
			t.Skipf("未设置 %s，跳过 PostgreSQL 契约测试", envTestDatabaseURL)
		}
		runStoreContract(t, func(t *testing.T) Store {
			t.Helper()
			return seedPostgres(t, dsn)
		})
	})
}

// seedPostgres 建一个干净的库，并写入与 testRoles/testGrants 相同的数据。
func seedPostgres(t *testing.T, dsn string) *postgresStore {
	t.Helper()

	store := newTestPostgres(t, dsn)
	ctx := context.Background()

	// 先清掉 seed 迁移带来的样例数据，只留测试自己造的那一份
	for _, stmt := range []string{
		`DELETE FROM role_grants`,
		`DELETE FROM role_permissions`,
		`DELETE FROM roles`,
	} {
		if _, err := store.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("清理数据失败：%v", err)
		}
	}

	for _, r := range testRoles() {
		if _, err := store.db.ExecContext(ctx,
			`INSERT INTO roles (id, name) VALUES ($1, $2)`, r.ID, r.Name); err != nil {
			t.Fatalf("写入角色失败：%v", err)
		}
		for _, p := range r.Permissions {
			if _, err := store.db.ExecContext(ctx,
				`INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)`,
				r.ID, p); err != nil {
				t.Fatalf("写入权限失败：%v", err)
			}
		}
	}
	for _, g := range testGrants() {
		if _, err := store.db.ExecContext(ctx,
			`INSERT INTO role_grants (role_id, subject_type, subject_id) VALUES ($1, $2, $3)`,
			g.RoleID, g.SubjectType, g.SubjectID); err != nil {
			t.Fatalf("写入授权失败：%v", err)
		}
	}
	return store
}

// newTestPostgres 建一个干净的库：先 dropAll 再迁移。
func newTestPostgres(t *testing.T, dsn string) *postgresStore {
	t.Helper()

	store, err := newPostgresStore(dsn)
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	ctx := context.Background()
	if err := store.dropAll(ctx); err != nil {
		t.Fatalf("清库失败：%v", err)
	}
	if err := store.migrate(ctx, "authorization/rbac"); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func runStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()

	tests := map[string]func(*testing.T, Store){
		"角色带上自己的权限":      testStoreRoles,
		"按人查授权":          testStoreGrantsByPerson,
		"按部门查授权":         testStoreGrantsByDepartment,
		"人与部门的授权一次查完":    testStoreGrantsMerged,
		"部门为空时不能匹配到部门授权": testStoreEmptyDepartment,
		"查不到授权返回空而不是错误":  testStoreNoGrants,
	}

	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			fn(t, newStore(t))
		})
	}
}

func testStoreRoles(t *testing.T, store Store) {
	roles, err := store.Roles(context.Background())
	if err != nil {
		t.Fatalf("查角色失败：%v", err)
	}

	byID := map[string]Role{}
	for _, r := range roles {
		byID[r.ID] = r
	}

	manager, ok := byID["r-manager"]
	if !ok {
		t.Fatalf("缺少 r-manager：%+v", roles)
	}
	got := append([]string(nil), manager.Permissions...)
	sort.Strings(got)
	if !equalStrings(got, []string{"erp.order.approve", "erp.order.read"}) {
		t.Errorf("r-manager 的权限不对：%v", got)
	}
}

func testStoreGrantsByPerson(t *testing.T, store Store) {
	// 部门传空：只该拿到直接授予这个人的
	grants, err := store.GrantsFor(context.Background(), "p-001", "")
	if err != nil {
		t.Fatalf("查授权失败：%v", err)
	}

	if len(grants) != 1 || grants[0].RoleID != "r-viewer" {
		t.Errorf("只按人查应当只拿到 r-viewer：%+v", grants)
	}
}

func testStoreGrantsByDepartment(t *testing.T, store Store) {
	// 人传一个没有直接授权的：只该拿到部门的
	grants, err := store.GrantsFor(context.Background(), "p-002", "d-tech")
	if err != nil {
		t.Fatalf("查授权失败：%v", err)
	}

	if len(grants) != 1 || grants[0].RoleID != "r-manager" {
		t.Errorf("p-002 应当只通过部门拿到 r-manager：%+v", grants)
	}
}

// testStoreGrantsMerged：两条路径一次查完，不多不少。
func testStoreGrantsMerged(t *testing.T, store Store) {
	grants, err := store.GrantsFor(context.Background(), "p-001", "d-tech")
	if err != nil {
		t.Fatalf("查授权失败：%v", err)
	}

	got := make([]string, 0, len(grants))
	for _, g := range grants {
		got = append(got, g.RoleID)
	}
	sort.Strings(got)
	if !equalStrings(got, []string{"r-manager", "r-viewer"}) {
		t.Errorf("应当同时拿到直接授权与部门授权：%v", got)
	}
}

// testStoreEmptyDepartment 挡一条很容易写错的 SQL。
//
// 部门为空时，`subject_id = ”` 这样的条件会去匹配 subject_id 为空串的行；
// 更糟的写法会让部门条件整个失效，把**所有部门**的授权都查出来——
// 那是一次静默的全局越权。
func testStoreEmptyDepartment(t *testing.T, store Store) {
	grants, err := store.GrantsFor(context.Background(), "p-002", "")
	if err != nil {
		t.Fatalf("查授权失败：%v", err)
	}

	if len(grants) != 0 {
		t.Errorf("没有部门的人不该匹配到任何部门授权：%+v", grants)
	}
}

func testStoreNoGrants(t *testing.T, store Store) {
	grants, err := store.GrantsFor(context.Background(), "p-nobody", "d-nowhere")
	if err != nil {
		t.Fatalf("查不到授权不该报错：%v", err)
	}
	if len(grants) != 0 {
		t.Errorf("期望空结果，实际 %+v", grants)
	}
}

// ============================================================
// 迁移（对着真库跑）
// ============================================================

func TestMigrationIsIdempotent(t *testing.T) {
	dsn := os.Getenv(envTestDatabaseURL)
	if dsn == "" {
		t.Skipf("未设置 %s，跳过", envTestDatabaseURL)
	}

	store := newTestPostgres(t, dsn)
	ctx := context.Background()

	if err := store.migrate(ctx, "authorization/rbac"); err != nil {
		t.Fatalf("重复迁移应当无事发生，实际报错：%v", err)
	}

	// 样例数据应当在（0002_seed_rbac）
	grants, err := store.GrantsFor(ctx, "p-001", "d-tech")
	if err != nil {
		t.Fatalf("查授权失败：%v", err)
	}
	if len(grants) != 2 {
		t.Errorf("样例数据里 p-001 应当有直接授权 + 部门授权，实际 %+v", grants)
	}
}

// TestSeedRollbackRemovesSampleData：真实部署要能把样例角色清掉。
func TestSeedRollbackRemovesSampleData(t *testing.T) {
	dsn := os.Getenv(envTestDatabaseURL)
	if dsn == "" {
		t.Skipf("未设置 %s，跳过", envTestDatabaseURL)
	}

	store := newTestPostgres(t, dsn)
	ctx := context.Background()

	if err := store.rollback(ctx, "authorization/rbac", 1); err != nil {
		t.Fatalf("回退失败：%v", err)
	}

	roles, err := store.Roles(ctx)
	if err != nil {
		t.Fatalf("查角色失败：%v", err)
	}
	if len(roles) != 0 {
		t.Errorf("样例角色应当已被清掉：%+v", roles)
	}

	// 表结构要留着：回退的是数据那一版，不是建表那一版
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO roles (id, name) VALUES ('r-real', '真实角色')`); err != nil {
		t.Errorf("回退样例数据不该把表也删掉：%v", err)
	}
}
