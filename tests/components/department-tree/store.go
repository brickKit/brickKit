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
//
// 注意：**建表与初始数据不在这里**，它们是 migrations/*.sql（002 §8）。
// 内存实现只是测试替身，用 newMemoryStore(seed...) 直接给数据即可。
type Store interface {
	// List 返回全部部门；parentID 非空时只返回其直接下级。结果按 ID 排序。
	List(ctx context.Context, parentID string) ([]Department, error)
	// Get 按 ID 查询，不存在时返回 ErrNotFound。
	Get(ctx context.Context, id string) (Department, error)
	// Subtree 返回该部门及其全部下级；根不存在时返回 ErrNotFound。
	Subtree(ctx context.Context, rootID string) ([]Department, error)
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
