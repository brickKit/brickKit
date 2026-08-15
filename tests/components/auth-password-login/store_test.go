package main

import (
	"context"
	"errors"
	"os"
	"testing"
)

// 本文件是存储层的**行为契约测试**：同一份用例跑两个实现——
// 内存实现（始终跑）与 PostgreSQL 实现（设置 AUTH_TEST_DATABASE_URL 时跑）。
//
// 两边语义必须完全一致。只测内存实现的话，SQL 写错了单测照样全绿，
// 上了真库才炸——而那时已经在容器里了，查起来贵得多。

// envTestDatabaseURL 指向用于集成测试的 PostgreSQL。未设置时跳过 PG 用例。
const envTestDatabaseURL = "AUTH_TEST_DATABASE_URL"

func TestStoreContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		runStoreContract(t, func(t *testing.T) Store {
			t.Helper()
			return newMemoryStore()
		})
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv(envTestDatabaseURL)
		if dsn == "" {
			t.Skipf("未设置 %s，跳过 PostgreSQL 契约测试", envTestDatabaseURL)
		}
		runStoreContract(t, func(t *testing.T) Store {
			t.Helper()
			return newTestPostgres(t, dsn)
		})
	})
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
	if err := store.migrate(ctx, "auth/password-login"); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func runStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()

	tests := map[string]func(*testing.T, Store){
		"写入后能按用户名取回":   testStoreUpsertAndGet,
		"取不到时返回专门的错误":  testStoreNotFound,
		"重复写入是更新而不是报错": testStoreUpsertIsIdempotent,
	}

	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			fn(t, newStore(t))
		})
	}
}

func testStoreUpsertAndGet(t *testing.T, store Store) {
	ctx := context.Background()
	hash, err := hashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("哈希失败：%v", err)
	}

	want := Credential{Username: "zhangsan", PersonID: "p-001", PasswordHash: hash}
	if err := store.Upsert(ctx, want); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	got, err := store.GetByUsername(ctx, "zhangsan")
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if got != want {
		t.Errorf("取回的凭据与写入的不一致：\n写入 %+v\n取回 %+v", want, got)
	}
}

// testStoreNotFound：查不到必须是 ErrCredentialNotFound，不能是别的错。
//
// 上层靠这个错误把"没这个人"（401）与"库连不上"（503）分开。
// 两边实现返回的错误不一致，这条分支就会在真库上走错。
func testStoreNotFound(t *testing.T, store Store) {
	_, err := store.GetByUsername(context.Background(), "nobody")

	if !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("期望 ErrCredentialNotFound，实际 %v", err)
	}
}

// testStoreUpsertIsIdempotent：改密码走的就是这条路。
func testStoreUpsertIsIdempotent(t *testing.T, store Store) {
	ctx := context.Background()
	first, _ := hashPassword("old-password")
	second, _ := hashPassword("new-password")

	cred := Credential{Username: "zhangsan", PersonID: "p-001", PasswordHash: first}
	if err := store.Upsert(ctx, cred); err != nil {
		t.Fatalf("首次写入失败：%v", err)
	}

	cred.PasswordHash = second
	if err := store.Upsert(ctx, cred); err != nil {
		t.Fatalf("重复写入应当是更新，而不是报唯一键冲突：%v", err)
	}

	got, err := store.GetByUsername(ctx, "zhangsan")
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if !verifyPassword(got.PasswordHash, "new-password") {
		t.Error("更新后应当能用新口令验过")
	}
	if verifyPassword(got.PasswordHash, "old-password") {
		t.Error("旧口令在改密后必须失效")
	}
}

// ============================================================
// 迁移（对着真库跑）
// ============================================================

// TestMigrationIsIdempotent：迁移重复执行不能出错。
//
// 平台每次 up 都会跑一遍迁移（005 §6），不幂等的话第二次启动就失败。
func TestMigrationIsIdempotent(t *testing.T) {
	dsn := os.Getenv(envTestDatabaseURL)
	if dsn == "" {
		t.Skipf("未设置 %s，跳过", envTestDatabaseURL)
	}

	store := newTestPostgres(t, dsn)
	ctx := context.Background()

	if err := store.migrate(ctx, "auth/password-login"); err != nil {
		t.Fatalf("重复迁移应当无事发生，实际报错：%v", err)
	}

	// 样例账号应当在（0002_seed_credentials）
	cred, err := store.GetByUsername(ctx, "zhangsan")
	if err != nil {
		t.Fatalf("样例账号应当存在：%v", err)
	}
	if cred.PersonID != "p-001" {
		t.Errorf("样例账号应当指向 people/basic 的 p-001，实际 %q", cred.PersonID)
	}
	if !verifyPassword(cred.PasswordHash, "demo-password") {
		t.Error("样例账号的口令应当是 demo-password（试用指南里写的就是它）")
	}
}

// TestSeedRollbackRemovesDemoAccounts：真实部署要能把试用账号清掉。
func TestSeedRollbackRemovesDemoAccounts(t *testing.T) {
	dsn := os.Getenv(envTestDatabaseURL)
	if dsn == "" {
		t.Skipf("未设置 %s，跳过", envTestDatabaseURL)
	}

	store := newTestPostgres(t, dsn)
	ctx := context.Background()

	// 回退最近 1 个迁移 = 0002_seed_credentials
	if err := store.rollback(ctx, "auth/password-login", 1); err != nil {
		t.Fatalf("回退失败：%v", err)
	}

	if _, err := store.GetByUsername(ctx, "zhangsan"); !errors.Is(err, ErrCredentialNotFound) {
		t.Errorf("样例账号应当已被清掉，实际 %v", err)
	}
	// 表结构要留着：回退的是数据那一版，不是建表那一版
	if err := store.Upsert(ctx, Credential{
		Username: "real-user", PersonID: "p-001", PasswordHash: "x",
	}); err != nil {
		t.Errorf("回退样例数据不该把表也删掉：%v", err)
	}
}
