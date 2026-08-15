package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// 本文件是缓存层的**行为契约测试**：同一份用例跑两个实现——
// 内存实现（始终跑）与 Redis 实现（设置 RBAC_TEST_REDIS_ADDR 时跑）。
//
// 只测内存实现的话，序列化写错、键名拼错、TTL 忘了设，单测照样全绿，
// 上了真 Redis 才发现——而缓存的错通常表现为"权限偶尔不对"，最难查。

const envTestRedisAddr = "RBAC_TEST_REDIS_ADDR"

func TestCacheContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		runCacheContract(t, func(t *testing.T) Cache {
			t.Helper()
			return newMemoryCache()
		})
	})

	t.Run("redis", func(t *testing.T) {
		addr := os.Getenv(envTestRedisAddr)
		if addr == "" {
			t.Skipf("未设置 %s，跳过 Redis 契约测试", envTestRedisAddr)
		}
		runCacheContract(t, func(t *testing.T) Cache {
			t.Helper()
			return newTestRedisCache(t, addr)
		})
	})
}

func newTestRedisCache(t *testing.T, addr string) *redisCache {
	t.Helper()

	cache := newRedisCache(cacheConfig{
		Host:     hostOf(addr),
		Port:     portOf(t, addr),
		Password: os.Getenv("RBAC_TEST_REDIS_PASSWORD"),
		TTL:      time.Minute,
	})
	t.Cleanup(func() { _ = cache.Close() })

	// 每个用例从干净的键开始
	for _, personID := range []string{"p-001", "p-002", "p-ttl"} {
		if err := cache.Delete(context.Background(), personID); err != nil {
			t.Fatalf("连不上测试 Redis（%s）：%v", addr, err)
		}
	}
	return cache
}

func hostOf(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

func portOf(t *testing.T, addr string) int {
	t.Helper()

	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			port := 0
			for _, c := range addr[i+1:] {
				port = port*10 + int(c-'0')
			}
			return port
		}
	}
	return 6379
}

func runCacheContract(t *testing.T, newCache func(t *testing.T) Cache) {
	t.Helper()

	tests := map[string]func(*testing.T, Cache){
		"写入后能取回":      testCacheRoundTrip,
		"未命中不是错误":     testCacheMissIsNotAnError,
		"删除后取不到":      testCacheDelete,
		"不同的人互不影响":    testCacheIsolatesPeople,
		"空权限集也要能正确存取": testCacheEmptySet,
	}

	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			fn(t, newCache(t))
		})
	}
}

func testCacheRoundTrip(t *testing.T, cache Cache) {
	ctx := context.Background()
	want := permissionSet{
		PersonID:     "p-001",
		DepartmentID: "d-tech",
		Roles:        []string{"r-manager", "r-viewer"},
		Permissions:  []string{"erp.order.approve", "erp.order.read"},
	}

	if err := cache.Set(ctx, "p-001", want); err != nil {
		t.Fatalf("写缓存失败：%v", err)
	}

	got, ok, err := cache.Get(ctx, "p-001")
	if err != nil {
		t.Fatalf("读缓存失败：%v", err)
	}
	if !ok {
		t.Fatal("刚写进去就取不到")
	}
	if got.PersonID != want.PersonID || got.DepartmentID != want.DepartmentID {
		t.Errorf("身份字段丢了：%+v", got)
	}
	if !equalStrings(got.Permissions, want.Permissions) {
		t.Errorf("权限不一致：期望 %v，实际 %v", want.Permissions, got.Permissions)
	}
	if !equalStrings(got.Roles, want.Roles) {
		t.Errorf("角色不一致：期望 %v，实际 %v", want.Roles, got.Roles)
	}
}

// testCacheMissIsNotAnError：没有这个键不是错误，就是没缓存。
//
// 两者混在一起的话，第一次查询会走进"缓存出错"的分支，
// 而那条分支的日志会把每一次冷启动都渲染成故障。
func testCacheMissIsNotAnError(t *testing.T, cache Cache) {
	_, ok, err := cache.Get(context.Background(), "p-never-cached")

	if err != nil {
		t.Fatalf("未命中不该是错误：%v", err)
	}
	if ok {
		t.Fatal("从没写过的键不该命中")
	}
}

func testCacheDelete(t *testing.T, cache Cache) {
	ctx := context.Background()
	if err := cache.Set(ctx, "p-001", permissionSet{PersonID: "p-001"}); err != nil {
		t.Fatalf("写缓存失败：%v", err)
	}
	if err := cache.Delete(ctx, "p-001"); err != nil {
		t.Fatalf("删缓存失败：%v", err)
	}

	if _, ok, _ := cache.Get(ctx, "p-001"); ok {
		t.Fatal("删掉之后还能取到")
	}
}

// testCacheIsolatesPeople 挡住权限系统里最严重的一类事故。
//
// 缓存键漏了 personId 的话，所有人会共用同一份权限——而功能测试全都会通过，
// 因为每个人单独查都"有结果"。
func testCacheIsolatesPeople(t *testing.T, cache Cache) {
	ctx := context.Background()

	if err := cache.Set(ctx, "p-001", permissionSet{
		PersonID: "p-001", Permissions: []string{"erp.order.approve"},
	}); err != nil {
		t.Fatalf("写缓存失败：%v", err)
	}

	got, ok, err := cache.Get(ctx, "p-002")
	if err != nil {
		t.Fatalf("读缓存失败：%v", err)
	}
	if ok {
		t.Fatalf("p-002 取到了 p-001 的缓存：%+v —— 缓存键没有按人分开", got)
	}
}

// testCacheEmptySet：没有任何权限的人也要能被缓存。
//
// 不缓存空结果的话，"什么权限都没有"的人每次都要回源——而那恰恰是
// 被攻击时最容易被反复探测的一类账号。
func testCacheEmptySet(t *testing.T, cache Cache) {
	ctx := context.Background()
	want := permissionSet{PersonID: "p-002", DepartmentID: "d-hr", Roles: []string{}, Permissions: []string{}}

	if err := cache.Set(ctx, "p-002", want); err != nil {
		t.Fatalf("写缓存失败：%v", err)
	}

	got, ok, err := cache.Get(ctx, "p-002")
	if err != nil {
		t.Fatalf("读缓存失败：%v", err)
	}
	if !ok {
		t.Fatal("空权限集也该被缓存住")
	}
	if len(got.Permissions) != 0 {
		t.Errorf("空权限集取回来变成了 %v", got.Permissions)
	}
}

// TestRedisCacheSetsTTL 只对真 Redis 有意义。
//
// TTL 是最后的兜底：授权变更时我们会主动失效缓存，但万一漏了一条，
// 没有 TTL 就意味着一份错的权限**永远**留在缓存里。
func TestRedisCacheSetsTTL(t *testing.T) {
	addr := os.Getenv(envTestRedisAddr)
	if addr == "" {
		t.Skipf("未设置 %s，跳过", envTestRedisAddr)
	}

	cache := newTestRedisCache(t, addr)
	ctx := context.Background()
	if err := cache.Set(ctx, "p-ttl", permissionSet{PersonID: "p-ttl"}); err != nil {
		t.Fatalf("写缓存失败：%v", err)
	}

	ttl, err := cache.client.TTL(ctx, cacheKeyPrefix+"p-ttl").Result()
	if err != nil {
		t.Fatalf("查 TTL 失败：%v", err)
	}
	if ttl <= 0 {
		t.Fatalf("缓存必须设置 TTL，实际 %v（-1 表示永不过期）", ttl)
	}
	if ttl > time.Minute {
		t.Errorf("TTL 超过了配置值：%v", ttl)
	}
}

// TestRedisKeysArePrefixed：键要带组件前缀（006 §7）。
//
// 一个项目里多个组件可能绑定同一个 cache 资源；不带前缀的话，
// 两个组件用了同一个键名就会互相覆盖，症状是"权限偶尔不对"。
func TestRedisKeysArePrefixed(t *testing.T) {
	addr := os.Getenv(envTestRedisAddr)
	if addr == "" {
		t.Skipf("未设置 %s，跳过", envTestRedisAddr)
	}

	cache := newTestRedisCache(t, addr)
	ctx := context.Background()
	if err := cache.Set(ctx, "p-001", permissionSet{PersonID: "p-001"}); err != nil {
		t.Fatalf("写缓存失败：%v", err)
	}

	keys, err := cache.client.Keys(ctx, cacheKeyPrefix+"*").Result()
	if err != nil {
		t.Fatalf("列键失败：%v", err)
	}
	if len(keys) == 0 {
		t.Fatalf("没有找到带前缀 %q 的键", cacheKeyPrefix)
	}

	// 裸的 personId 不该出现在 Redis 里
	if bare, _ := cache.client.Exists(ctx, "p-001").Result(); bare != 0 {
		t.Error("出现了不带组件前缀的裸键 p-001")
	}
}
