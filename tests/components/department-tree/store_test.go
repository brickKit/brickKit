// 本文件是数据存储的行为契约测试。
//
// 内存实现与 PostgreSQL 实现共用同一份契约：两者一旦分叉，
// 单测全绿而真容器跑不起来的事就会发生。PostgreSQL 那一份只在设置了
// DEPARTMENT_TEST_DATABASE_URL 时运行，其余环境自动跳过。
package main

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestStoreContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		// 内存实现是测试替身，不跑 SQL 迁移：直接给它与
		// migrations/0002_seed_departments.sql 一致的数据。
		// 两边一旦漂移，下面 postgres 那一组就会失败。
		runStoreContract(t, func(t *testing.T) Store {
			return newMemoryStore(seedTree()...)
		})
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("DEPARTMENT_TEST_DATABASE_URL")
		if dsn == "" {
			t.Skip("未设置 DEPARTMENT_TEST_DATABASE_URL，跳过 PostgreSQL 集成测试")
		}
		runStoreContract(t, func(t *testing.T) Store {
			store, err := newPostgresStore(dsn)
			if err != nil {
				t.Fatalf("连接数据库失败：%v", err)
			}
			t.Cleanup(func() { _ = store.Close() })

			if err := store.dropAll(context.Background()); err != nil {
				t.Fatalf("清库失败：%v", err)
			}
			if err := store.migrate(context.Background(), "department/tree"); err != nil {
				t.Fatalf("迁移失败：%v", err)
			}
			return store
		})
	})
}

func runStoreContract(t *testing.T, newStore func(*testing.T) Store) {
	t.Helper()
	ctx := context.Background()
	store := newStore(t)

	// 全量列举：按 ID 排序，顺序稳定
	all, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List 失败：%v", err)
	}
	if len(all) < 2 {
		t.Fatalf("迁移后应有初始数据，实际 %d 条", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].ID > all[i].ID {
			t.Fatalf("List 必须按 ID 排序，实际：%v", idsOfDepartments(all))
		}
	}

	// 根部门：parentId 为空、level 为 1
	var root Department
	for _, d := range all {
		if d.ParentID == "" {
			root = d
			break
		}
	}
	if root.ID == "" {
		t.Fatal("应存在一个根部门")
	}
	if root.Level != 1 {
		t.Fatalf("根部门 level 应为 1，实际 %d", root.Level)
	}

	// 按上级过滤只返回直接下级
	children, err := store.List(ctx, root.ID)
	if err != nil {
		t.Fatalf("List(parent) 失败：%v", err)
	}
	if len(children) == 0 {
		t.Fatal("根部门应有下级")
	}
	for _, d := range children {
		if d.ParentID != root.ID {
			t.Fatalf("过滤结果里混入了非直接下级：%+v", d)
		}
	}

	// 单个查询
	got, err := store.Get(ctx, root.ID)
	if err != nil {
		t.Fatalf("Get 失败：%v", err)
	}
	if got.ID != root.ID || got.Name != root.Name {
		t.Fatalf("Get 返回的不是同一条：%+v vs %+v", got, root)
	}

	// 不存在时返回 ErrNotFound，而不是零值
	_, err = store.Get(ctx, "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际：%v", err)
	}

	// 子树：包含自己 + 全部下级（不止一层）
	subtree, err := store.Subtree(ctx, root.ID)
	if err != nil {
		t.Fatalf("Subtree 失败：%v", err)
	}
	if len(subtree) != len(all) {
		t.Fatalf("根部门的子树应含全部部门：期望 %d，实际 %d", len(all), len(subtree))
	}

	// 叶子节点的子树只有它自己
	leaf := deepest(all)
	subtree, err = store.Subtree(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("Subtree 失败：%v", err)
	}
	if len(subtree) != 1 || subtree[0].ID != leaf.ID {
		t.Fatalf("叶子节点的子树应只含自己，实际：%v", idsOfDepartments(subtree))
	}

	_, err = store.Subtree(ctx, "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("子树查询不存在的根应返回 ErrNotFound，实际：%v", err)
	}
}

func idsOfDepartments(items []Department) []string {
	out := make([]string, 0, len(items))
	for _, d := range items {
		out = append(out, d.ID)
	}
	return out
}

// deepest 返回层级最深的部门（叶子）。
func deepest(items []Department) Department {
	var out Department
	for _, d := range items {
		if d.Level > out.Level {
			out = d
		}
	}
	return out
}
