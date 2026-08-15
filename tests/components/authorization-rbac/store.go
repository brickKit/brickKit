package main

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// errStorageUnavailable 表示存储本身不可用（连不上、超时）。
//
// 与"查出来是空的"必须分开：前者是 503，后者是"这个人确实没有权限"。
// 混成一种，数据库一抖就会变成"所有人都没有权限"——一个静默的全局降权。
var errStorageUnavailable = errors.New("存储不可用")

// 授权主体的类型。
//
// 角色可以授予**人**，也可以授予**部门**。后者是这个组件强依赖 people/basic
// 的原因：部门在那边，没有它就算不出完整的权限。
const (
	SubjectPerson     = "person"
	SubjectDepartment = "department"
)

// Role 是一组权限的集合。
type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// Grant 把一个角色授予某个主体。
type Grant struct {
	RoleID string `json:"roleId"`
	// SubjectType 是 person 或 department。
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
}

// Store 是授权数据的存取接口。
//
// 建表与初始数据不在这里，它们是 migrations/*.sql（002 §8）。
type Store interface {
	// Roles 返回全部角色及其权限。
	Roles(ctx context.Context) ([]Role, error)
	// GrantsFor 返回授予该人员、以及授予其所在部门的全部授权。
	//
	// 两者一次查完而不是分两次：授权判断是最高频的调用，
	// 少一次往返就是每个请求都少一次。
	GrantsFor(ctx context.Context, personID, departmentID string) ([]Grant, error)
}

// ============================================================
// 内存实现
// ============================================================

type memoryStore struct {
	mu     sync.RWMutex
	roles  []Role
	grants []Grant
	// queries 记录回源次数，供缓存测试断言"这次没有查库"。
	queries int
}

func newMemoryStore(roles []Role, grants []Grant) *memoryStore {
	return &memoryStore{roles: roles, grants: grants}
}

func (m *memoryStore) Roles(context.Context) ([]Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.queries++
	return append([]Role(nil), m.roles...), nil
}

func (m *memoryStore) GrantsFor(_ context.Context, personID, departmentID string) ([]Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.queries++
	var out []Grant
	for _, g := range m.grants {
		switch {
		case g.SubjectType == SubjectPerson && g.SubjectID == personID:
			out = append(out, g)
		case g.SubjectType == SubjectDepartment && departmentID != "" && g.SubjectID == departmentID:
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RoleID < out[j].RoleID })
	return out, nil
}

// setGrants 换掉授权数据，供测试模拟"权限被收回"。
func (m *memoryStore) setGrants(grants []Grant) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.grants = grants
}
