package main

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// ErrNotFound 表示部门不存在。
var ErrNotFound = errors.New("部门不存在")

// Department 是一个部门。
//
// 树结构用 parentId 表达而不是嵌套结构：扁平列表在两种协议里都好表示，
// 也让"按上级过滤"这类查询不用先把整棵树读出来。
type Department struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId"`
	Level    int    `json:"level"`
}

// Store 是部门数据的存取接口。
//
// 抽成接口是为了让业务逻辑与存储实现分开测：HTTP / gRPC 的行为测试用内存实现，
// SQL 的正确性由契约测试对着真库跑（见 store_test.go）。
type Store interface {
	// List 返回全部部门；parentID 非空时只返回其直接下级。结果按 ID 排序。
	List(ctx context.Context, parentID string) ([]Department, error)
	// Get 按 ID 查询，不存在时返回 ErrNotFound。
	Get(ctx context.Context, id string) (Department, error)
	// Subtree 返回该部门及其全部下级；根不存在时返回 ErrNotFound。
	Subtree(ctx context.Context, rootID string) ([]Department, error)
}

// seeder 是迁移时写入初始数据的能力。两种存储都实现它。
type seeder interface {
	// ensureSchema 建表（幂等）。
	ensureSchema(ctx context.Context) error
	// upsert 写入或更新一个部门（幂等）。
	upsert(ctx context.Context, d Department) error
}

// initialDepartments 是随迁移写入的初始部门树。
//
// 这是一个平台自测组件：它的数据是固定的样例组织架构，
// 让依赖它的组件（people/basic、erp/backend）有稳定的东西可查。
func initialDepartments() []Department {
	return []Department{
		{ID: "d-root", Name: "总公司", ParentID: "", Level: 1},
		{ID: "d-tech", Name: "技术中心", ParentID: "d-root", Level: 2},
		{ID: "d-hr", Name: "人力资源部", ParentID: "d-root", Level: 2},
		{ID: "d-backend", Name: "后端组", ParentID: "d-tech", Level: 3},
	}
}

// migrate 建表并写入初始数据（002 §8：迁移由平台在启动前执行）。
//
// 必须幂等：容器每次重启都会再跑一遍，第二次失败等于服务再也起不来。
func migrate(ctx context.Context, store Store) error {
	s, ok := store.(seeder)
	if !ok {
		return errors.New("该存储实现不支持迁移")
	}
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}
	for _, d := range initialDepartments() {
		if err := s.upsert(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================
// 内存实现
// ============================================================

// memoryStore 是内存实现，供测试与本地把玩使用。
type memoryStore struct {
	mu    sync.RWMutex
	items map[string]Department
}

func newMemoryStore(seed ...Department) *memoryStore {
	store := &memoryStore{items: map[string]Department{}}
	for _, d := range seed {
		store.items[d.ID] = d
	}
	return store
}

func (m *memoryStore) ensureSchema(context.Context) error { return nil }

func (m *memoryStore) upsert(_ context.Context, d Department) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items[d.ID] = d
	return nil
}

func (m *memoryStore) List(_ context.Context, parentID string) ([]Department, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return sortByID(m.filter(func(d Department) bool {
		return parentID == "" || d.ParentID == parentID
	})), nil
}

func (m *memoryStore) Get(_ context.Context, id string) (Department, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	d, ok := m.items[id]
	if !ok {
		return Department{}, ErrNotFound
	}
	return d, nil
}

func (m *memoryStore) Subtree(_ context.Context, rootID string) ([]Department, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.items[rootID]; !ok {
		return nil, ErrNotFound
	}

	// 逐层展开而不是递归：树不深，但递归在数据被人为改出环时会栈溢出
	inSubtree := map[string]bool{rootID: true}
	for changed := true; changed; {
		changed = false
		for _, d := range m.items {
			if !inSubtree[d.ID] && inSubtree[d.ParentID] {
				inSubtree[d.ID] = true
				changed = true
			}
		}
	}

	return sortByID(m.filter(func(d Department) bool { return inSubtree[d.ID] })), nil
}

func (m *memoryStore) filter(keep func(Department) bool) []Department {
	out := make([]Department, 0, len(m.items))
	for _, d := range m.items {
		if keep(d) {
			out = append(out, d)
		}
	}
	return out
}

func sortByID(items []Department) []Department {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
