package main

import (
	"context"
	"errors"
	"sync"
)

// 存储层的语义错误。上层据此决定回什么状态码——
// 把"没这个人"和"库连不上"混成一种，会让使用者在自己的密码上白折腾半天。
var (
	// ErrCredentialNotFound 表示没有这个用户名的凭据。
	ErrCredentialNotFound = errors.New("凭据不存在")
	// errStorageUnavailable 表示存储本身不可用（连不上、超时）。
	errStorageUnavailable = errors.New("存储不可用")
)

// Credential 是一条登录凭据。
//
// **这里只有"怎么证明你是你"，没有"你是谁"**：姓名、部门等身份信息在
// people/basic 里。分开的好处是主体的存废由人员系统说了算——员工从人员系统
// 里消失，凭据表哪怕还留着，也登录不进来（service_test.go 锁了这一条）。
type Credential struct {
	Username string
	// PersonID 指向 people/basic 中的人员。
	PersonID string
	// PasswordHash 是 bcrypt 哈希。**明文口令在任何地方都不存在**——
	// 请求处理完就随栈销毁，不进日志、不进库、不进令牌。
	PasswordHash string
}

// Store 是凭据的存取接口。
//
// 抽成接口是为了让 HTTP 行为测试用内存实现跑得飞快，
// 而 SQL 的正确性由契约测试对着真库验（store_test.go）。
//
// 建表与初始数据不在这里，它们是 migrations/*.sql（002 §8）。
type Store interface {
	// GetByUsername 按用户名取凭据，不存在时返回 ErrCredentialNotFound。
	GetByUsername(ctx context.Context, username string) (Credential, error)
	// Upsert 写入或更新一条凭据（改密码、初始化管理员用）。
	Upsert(ctx context.Context, c Credential) error
}

// ============================================================
// 内存实现
// ============================================================

type memoryStore struct {
	mu    sync.RWMutex
	items map[string]Credential
}

func newMemoryStore(seed ...Credential) *memoryStore {
	store := &memoryStore{items: map[string]Credential{}}
	for _, c := range seed {
		store.items[c.Username] = c
	}
	return store
}

func (m *memoryStore) GetByUsername(_ context.Context, username string) (Credential, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.items[username]
	if !ok {
		return Credential{}, ErrCredentialNotFound
	}
	return c, nil
}

func (m *memoryStore) Upsert(_ context.Context, c Credential) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items[c.Username] = c
	return nil
}
