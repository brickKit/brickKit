package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// cacheKeyPrefix 让本组件的键在共享的 Redis 里不与别人打架（006 §7）。
//
// 一个项目里多个组件可能绑定同一个 cache 资源；不带前缀的话，
// 两个组件用了同一个键名就会互相覆盖，而且症状是"权限偶尔不对"。
const cacheKeyPrefix = "authorization-rbac:permissions:"

// cacheTimeout 是单次 Redis 操作的超时。
//
// 必须有，而且要短：缓存是加速器，等它比回源还慢就本末倒置了。
const cacheTimeout = 500 * time.Millisecond

// permissionSet 是一个人的权限解析结果，也是缓存里存的东西。
type permissionSet struct {
	PersonID     string   `json:"personId"`
	DepartmentID string   `json:"departmentId"`
	Roles        []string `json:"roles"`
	Permissions  []string `json:"permissions"`
}

// Cache 是权限缓存。
//
// **它是加速器，不是数据源。** 所有方法的错误都由调用方降级处理——
// Redis 挂了应当照常回源、只是慢一点；若因此报错，等于让一个可选的
// 基础设施变成单点，整个系统的每一次权限检查都会失败。
type Cache interface {
	Get(ctx context.Context, personID string) (permissionSet, bool, error)
	Set(ctx context.Context, personID string, set permissionSet) error
	Delete(ctx context.Context, personID string) error
}

// ============================================================
// 内存实现（测试与无 Redis 的本地把玩）
// ============================================================

type memoryCache struct {
	mu    sync.RWMutex
	items map[string]permissionSet
}

func newMemoryCache() *memoryCache {
	return &memoryCache{items: map[string]permissionSet{}}
}

func (m *memoryCache) Get(_ context.Context, personID string) (permissionSet, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	set, ok := m.items[personID]
	return set, ok, nil
}

func (m *memoryCache) Set(_ context.Context, personID string, set permissionSet) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items[personID] = set
	return nil
}

func (m *memoryCache) Delete(_ context.Context, personID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.items, personID)
	return nil
}

// ============================================================
// Redis 实现
// ============================================================

type redisCache struct {
	client *redis.Client
	ttl    time.Duration
}

func newRedisCache(cfg cacheConfig) *redisCache {
	return &redisCache{
		client: redis.NewClient(&redis.Options{
			Addr:     cfg.Addr(),
			Password: cfg.Password,
			DB:       0,
			// 连不上时快速失败，让调用方赶紧去回源
			DialTimeout:  cacheTimeout,
			ReadTimeout:  cacheTimeout,
			WriteTimeout: cacheTimeout,
		}),
		ttl: cfg.TTL,
	}
}

func (r *redisCache) Close() error { return r.client.Close() }

func (r *redisCache) Get(ctx context.Context, personID string) (permissionSet, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, cacheTimeout)
	defer cancel()

	raw, err := r.client.Get(ctx, cacheKeyPrefix+personID).Bytes()
	switch {
	case err == redis.Nil:
		// 没有这个键不是错误，就是没缓存
		return permissionSet{}, false, nil
	case err != nil:
		return permissionSet{}, false, err
	}

	var set permissionSet
	if err := json.Unmarshal(raw, &set); err != nil {
		// 缓存里的内容坏了：当作未命中去回源，顺手把这条脏数据删掉。
		// 报错的话，一条坏缓存会把这个人永久挡在门外
		_ = r.Delete(ctx, personID)
		return permissionSet{}, false, nil
	}
	return set, true, nil
}

func (r *redisCache) Set(ctx context.Context, personID string, set permissionSet) error {
	ctx, cancel := context.WithTimeout(ctx, cacheTimeout)
	defer cancel()

	raw, err := json.Marshal(set)
	if err != nil {
		return err
	}
	// 一定要有 TTL：授权变更时我们会主动失效，但万一漏了一条，
	// TTL 是最后的兜底，不至于让一份错的权限永远留在缓存里
	return r.client.Set(ctx, cacheKeyPrefix+personID, raw, r.ttl).Err()
}

func (r *redisCache) Delete(ctx context.Context, personID string) error {
	ctx, cancel := context.WithTimeout(ctx, cacheTimeout)
	defer cancel()

	return r.client.Del(ctx, cacheKeyPrefix+personID).Err()
}
