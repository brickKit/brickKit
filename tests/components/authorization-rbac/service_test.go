package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	authorizationv1 "github.com/brickkit/components/authorization-rbac/gen/authorization/v1"
)

// 本文件覆盖开发计划 24.1（HTTP）、24.4 / 24.5（健康检查），
// 以及这个组件真正难的地方：**权限从哪来、算不出来的时候怎么办**。

// ============================================================
// 夹具
// ============================================================

// stubPeople 是 people/basic 的替身（强依赖：部门从这里来）。
type stubPeople struct {
	people map[string]person
	err    error
	calls  int
}

func (s *stubPeople) GetPerson(_ context.Context, id string) (person, error) {
	s.calls++
	if s.err != nil {
		return person{}, s.err
	}
	p, ok := s.people[id]
	if !ok {
		return person{}, errPersonNotFound
	}
	return p, nil
}

// 一份够用的授权数据：
//
//	r-viewer   直接授予 p-001
//	r-manager  授予部门 d-tech（p-001 与 p-002 都在 d-tech）
func testGrants() []Grant {
	return []Grant{
		{RoleID: "r-viewer", SubjectType: SubjectPerson, SubjectID: "p-001"},
		{RoleID: "r-manager", SubjectType: SubjectDepartment, SubjectID: "d-tech"},
	}
}

func testRoles() []Role {
	return []Role{
		{ID: "r-viewer", Name: "查看者", Permissions: []string{"erp.order.read"}},
		{ID: "r-manager", Name: "主管", Permissions: []string{"erp.order.read", "erp.order.approve"}},
	}
}

func testPeople() *stubPeople {
	return &stubPeople{people: map[string]person{
		"p-001": {ID: "p-001", DepartmentID: "d-tech"},
		"p-002": {ID: "p-002", DepartmentID: "d-tech"},
		"p-003": {ID: "p-003", DepartmentID: "d-hr"},
	}}
}

func newTestService(t *testing.T, people *stubPeople, cache Cache) (*service, *memoryStore) {
	t.Helper()

	if people == nil {
		people = testPeople()
	}
	if cache == nil {
		cache = newMemoryCache()
	}
	store := newMemoryStore(testRoles(), testGrants())
	return newService(store, people, cache, config{ComponentID: "authorization/rbac"}), store
}

// getJSON 发一次 GET 请求。
func getJSON(t *testing.T, svc *service, path string) (int, map[string]any) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("响应不是合法 JSON：%s", rec.Body.String())
		}
	}
	return rec.Code, body
}

func stringsOf(t *testing.T, value any) []string {
	t.Helper()

	items, ok := value.([]any)
	if !ok {
		t.Fatalf("期望数组，实际 %T：%v", value, value)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.(string))
	}
	sort.Strings(out)
	return out
}

// ============================================================
// 24.1 HTTP API
// ============================================================

// TestListPermissionsMergesDirectAndDepartment 是这个组件的核心语义。
//
// 权限来自两条路径的并集：直接授予这个人的角色，以及授予他所在**部门**的角色。
// 第二条正是它强依赖 people/basic 的原因——部门在那边，没有它就算不出完整权限。
func TestListPermissionsMergesDirectAndDepartment(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	code, body := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%v", code, body)
	}

	// r-viewer（直接） + r-manager（部门 d-tech）
	want := []string{"erp.order.approve", "erp.order.read"}
	if got := stringsOf(t, body["permissions"]); !equalStrings(got, want) {
		t.Errorf("权限应当是两条路径的并集：期望 %v，实际 %v", want, got)
	}
	if got := stringsOf(t, body["roles"]); !equalStrings(got, []string{"r-manager", "r-viewer"}) {
		t.Errorf("命中的角色不对：%v", got)
	}
}

// TestPermissionsAreDeduplicatedAndSorted：两个角色都有的权限只出现一次。
//
// 顺序也要稳定：调用方常常直接比对这个数组，顺序随 map 遍历变的话，
// 同样的输入会得到不同的输出。
func TestPermissionsAreDeduplicatedAndSorted(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	_, body := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	got := stringsOf(t, body["permissions"])

	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("权限 %q 重复出现", p)
		}
		seen[p] = true
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("权限应当有序：%v", got)
	}
}

// TestDepartmentOnlyMember：只靠部门拿到权限的人。
func TestDepartmentOnlyMember(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	_, body := getJSON(t, svc, "/api/v1/permissions?personId=p-002")

	want := []string{"erp.order.approve", "erp.order.read"}
	if got := stringsOf(t, body["permissions"]); !equalStrings(got, want) {
		t.Errorf("p-002 应当只通过部门 d-tech 拿到权限：%v", got)
	}
}

// TestPersonWithNoGrants：什么都没有的人拿到空数组，不是 null。
//
// 弱类型的调用方遍历 null 会直接崩。
func TestPersonWithNoGrants(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	code, body := getJSON(t, svc, "/api/v1/permissions?personId=p-003")
	if code != http.StatusOK {
		t.Fatalf("没有权限不是错误，应当 200，实际 %d", code)
	}
	if body["permissions"] == nil {
		t.Fatal("应当返回空数组 []，而不是 null")
	}
	if got := stringsOf(t, body["permissions"]); len(got) != 0 {
		t.Errorf("p-003 不该有任何权限：%v", got)
	}
}

func TestListPermissionsRequiresPersonID(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	code, _ := getJSON(t, svc, "/api/v1/permissions")
	if code != http.StatusBadRequest {
		t.Errorf("缺 personId 应当 400，实际 %d", code)
	}
}

// ============================================================
// check 端点
// ============================================================

func TestCheckAllowsAndDenies(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	cases := map[string]struct {
		personID   string
		permission string
		allowed    bool
	}{
		"直接授予的权限":   {"p-001", "erp.order.read", true},
		"部门授予的权限":   {"p-001", "erp.order.approve", true},
		"没有的权限":     {"p-001", "erp.order.delete", false},
		"无授权的人":     {"p-003", "erp.order.read", false},
		"只靠部门拿到的权限": {"p-002", "erp.order.approve", true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/check",
				strings.NewReader(`{"personId":"`+c.personID+`","permission":"`+c.permission+`"}`))
			rec := httptest.NewRecorder()
			svc.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("响应不是合法 JSON：%s", rec.Body.String())
			}
			if body["allowed"] != c.allowed {
				t.Errorf("期望 allowed=%v，实际 %v（reason=%v）", c.allowed, body["allowed"], body["reason"])
			}
		})
	}
}

// TestCheckDeniedIsNot403：没有权限是 200 + allowed:false，不是 403。
//
// 这个端点回答的是"他有没有这个权限"，**调用方**才决定要不要放行。
// 用 403 表示"查到了但没有"，会让调用方分不清"我没权限查这个接口"
// 和"我查到了、答案是否"。
func TestCheckDeniedIsNot403(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/check",
		strings.NewReader(`{"personId":"p-003","permission":"erp.order.read"}`))
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("查询成功就是 200，答案在 body 里；实际 %d", rec.Code)
	}
}

// ============================================================
// 24.3 Redis 缓存
// ============================================================

// TestSecondCallHitsCache：第二次查询不再回源。
//
// 权限检查是最高频的调用（每个请求都可能来一次）。每次都查库 + 调 people/basic
// 的话，这个组件会成为整个系统的瓶颈。
func TestSecondCallHitsCache(t *testing.T) {
	people := testPeople()
	cache := newMemoryCache()
	svc, store := newTestService(t, people, cache)

	_, first := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if first["cached"] != false {
		t.Error("首次查询不该来自缓存")
	}
	callsAfterFirst := people.calls
	queriesAfterFirst := store.queries

	_, second := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if second["cached"] != true {
		t.Error("第二次查询应当命中缓存")
	}
	if people.calls != callsAfterFirst {
		t.Errorf("命中缓存时不该再调 people/basic：%d → %d", callsAfterFirst, people.calls)
	}
	if store.queries != queriesAfterFirst {
		t.Errorf("命中缓存时不该再查库：%d → %d", queriesAfterFirst, store.queries)
	}

	// 结果本身必须一致，否则缓存就成了一个会给出不同答案的分支
	if !equalStrings(stringsOf(t, first["permissions"]), stringsOf(t, second["permissions"])) {
		t.Error("缓存返回的权限与回源结果不一致")
	}
}

// TestCacheIsPerPerson：缓存键必须按人分开。
//
// 键写错（比如漏了 personId）会让所有人共用同一份权限——这是权限系统里
// 最严重的一类事故，而且功能测试全都会通过。
func TestCacheIsPerPerson(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	_, _ = getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	_, body := getJSON(t, svc, "/api/v1/permissions?personId=p-003")

	if got := stringsOf(t, body["permissions"]); len(got) != 0 {
		t.Fatalf("p-003 拿到了别人的权限：%v —— 缓存键没有按人分开", got)
	}
}

// TestCacheFailureDegradesToSource 是这个组件最要紧的一条降级规则。
//
// **缓存是加速器，不是数据源。** Redis 挂了应当照常回源，只是慢一点；
// 若因此报错，等于让一个可选的基础设施变成单点——整个系统的每一次
// 权限检查都会失败。
func TestCacheFailureDegradesToSource(t *testing.T) {
	people := testPeople()
	svc, _ := newTestService(t, people, failingCache{})

	code, body := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if code != http.StatusOK {
		t.Fatalf("Redis 挂了也要照常回源，期望 200，实际 %d：%v", code, body)
	}

	want := []string{"erp.order.approve", "erp.order.read"}
	if got := stringsOf(t, body["permissions"]); !equalStrings(got, want) {
		t.Errorf("降级后结果应当与正常时一致：%v", got)
	}
	if body["cached"] != false {
		t.Error("回源就是回源，不能标成 cached")
	}
}

// TestGrantChangeInvalidatesCache：权限改了要立刻生效。
//
// 授权变更最常见的场景是"把某人的权限收回"。缓存不失效的话，
// 被收回权限的人在 TTL 到期前仍然畅通无阻——这是安全事故，不是延迟问题。
func TestGrantChangeInvalidatesCache(t *testing.T) {
	cache := newMemoryCache()
	svc, store := newTestService(t, nil, cache)

	_, before := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if len(stringsOf(t, before["permissions"])) == 0 {
		t.Fatal("前置条件不成立：p-001 本应有权限")
	}

	// 收回全部授权
	store.setGrants(nil)
	if err := svc.invalidate(context.Background(), "p-001"); err != nil {
		t.Fatalf("失效缓存失败：%v", err)
	}

	_, after := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if got := stringsOf(t, after["permissions"]); len(got) != 0 {
		t.Errorf("权限收回后仍能查到：%v —— 缓存没有失效", got)
	}
}

// ============================================================
// 强依赖 people/basic
// ============================================================

// TestPeopleOutageFailsClosedOnCacheMiss：算不出来时**拒绝**，不猜。
//
// 部门在 people/basic 里。它挂掉且缓存没命中时，我们只知道"直接授予的角色"，
// 不知道部门角色——此时若返回一个不完整的权限集，调用方会当成完整的用，
// 于是一个本该有权限的人被拒绝，或者更糟：一个判断"是否为管理员"的逻辑
// 因为缺了部门角色而走进了别的分支。宁可如实报 503。
func TestPeopleOutageFailsClosedOnCacheMiss(t *testing.T) {
	people := &stubPeople{err: errDependencyUnavailable}
	svc, _ := newTestService(t, people, nil)

	code, body := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d：%v", code, body)
	}
	if body["permissions"] != nil {
		t.Error("算不出完整权限时不该返回一个残缺的集合")
	}
}

// TestPeopleOutageServedFromCache：缓存里有就照常返回。
//
// 这正是缓存在这里的第二个价值：它让 people/basic 的短暂抖动不至于
// 立刻变成全系统的鉴权失败。
func TestPeopleOutageServedFromCache(t *testing.T) {
	people := testPeople()
	cache := newMemoryCache()
	svc, _ := newTestService(t, people, cache)

	// 先跑一次把缓存热起来
	if code, _ := getJSON(t, svc, "/api/v1/permissions?personId=p-001"); code != http.StatusOK {
		t.Fatalf("前置查询失败：%d", code)
	}

	people.err = errDependencyUnavailable

	code, body := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if code != http.StatusOK {
		t.Fatalf("缓存里有就该照常返回，实际 %d：%v", code, body)
	}
	if body["cached"] != true {
		t.Error("这次应当标记为来自缓存")
	}
}

// TestUnknownPersonIsNotAnError：people/basic 里没这个人 → 无权限，不是 500。
func TestUnknownPersonIsNotAnError(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	code, body := getJSON(t, svc, "/api/v1/permissions?personId=p-nobody")
	if code != http.StatusNotFound {
		t.Fatalf("查无此人应当 404，实际 %d：%v", code, body)
	}
}

// ============================================================
// 24.4 / 24.5 健康检查
// ============================================================

func TestHealthzReturns200(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
}

// TestHealthzDoesNotTouchRedisOrDependencies 是 24.5 与 002 §9.4。
//
// 依赖全挂时健康检查仍要 200：它只回答"本进程还活着吗"。
// 去查 Redis 的话，Redis 一抖，编排系统就会把这些**本身完全正常**的容器
// 全部杀掉重启——而 Redis 在本组件里只是个加速器。
func TestHealthzDoesNotTouchRedisOrDependencies(t *testing.T) {
	people := &stubPeople{err: errDependencyUnavailable}
	svc := newService(failingStore{}, people, failingCache{}, config{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("依赖全挂时 healthz 仍应 200，实际 %d", rec.Code)
	}
	if people.calls != 0 {
		t.Errorf("healthz 调用了 people/basic %d 次", people.calls)
	}
}

// TestStorageFailureIsUnavailable：数据库挂了如实报 503，且不外泄底层细节。
func TestStorageFailureIsUnavailable(t *testing.T) {
	svc := newService(failingStore{}, testPeople(), newMemoryCache(), config{})

	code, body := getJSON(t, svc, "/api/v1/permissions?personId=p-001")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d：%v", code, body)
	}

	raw, _ := json.Marshal(body)
	for _, leaked := range []string{"pq:", "sql", "postgres", "connection refused"} {
		if strings.Contains(strings.ToLower(string(raw)), leaked) {
			t.Errorf("响应泄露了底层实现细节 %q：%s", leaked, raw)
		}
	}
}

// ============================================================
// 24.2 gRPC 与 HTTP 必须给出同一个答案
// ============================================================

// TestGRPCAndHTTPAgree 锁住"一份逻辑、两个协议出口"。
//
// 两边各写一遍权限计算，迟早出现"HTTP 说有、gRPC 说没有"——
// 而这在权限系统里意味着同一个人在不同调用路径上有不同的权限。
func TestGRPCAndHTTPAgree(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)
	ctx := context.Background()

	for _, personID := range []string{"p-001", "p-002", "p-003"} {
		grpcResp, err := svc.ListPermissions(ctx,
			&authorizationv1.ListPermissionsRequest{PersonId: personID})
		if err != nil {
			t.Fatalf("gRPC ListPermissions(%s) 失败：%v", personID, err)
		}

		_, httpBody := getJSON(t, svc, "/api/v1/permissions?personId="+personID)
		httpPerms := stringsOf(t, httpBody["permissions"])

		grpcPerms := append([]string(nil), grpcResp.GetPermissions()...)
		sort.Strings(grpcPerms)

		if !equalStrings(grpcPerms, httpPerms) {
			t.Errorf("%s 的权限两个协议不一致：gRPC %v vs HTTP %v", personID, grpcPerms, httpPerms)
		}
	}
}

func TestGRPCCheckMatchesHTTP(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	resp, err := svc.Check(context.Background(), &authorizationv1.CheckRequest{
		PersonId: "p-001", Permission: "erp.order.approve",
	})
	if err != nil {
		t.Fatalf("gRPC Check 失败：%v", err)
	}
	if !resp.GetAllowed() {
		t.Error("p-001 应当通过部门拿到 erp.order.approve")
	}
	if resp.GetReason() == "" {
		t.Error("reason 要说明这个判断从哪来，便于排障")
	}
}

// TestGRPCUnknownPersonReturnsNotFound：gRPC 侧要用对状态码。
func TestGRPCUnknownPersonReturnsNotFound(t *testing.T) {
	svc, _ := newTestService(t, nil, nil)

	_, err := svc.ListPermissions(context.Background(),
		&authorizationv1.ListPermissionsRequest{PersonId: "p-nobody"})
	if err == nil {
		t.Fatal("查无此人应当报错")
	}
	if !strings.Contains(err.Error(), "NotFound") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("应当是 NOT_FOUND 状态码，实际：%v", err)
	}
}

// ============================================================
// 测试替身
// ============================================================

type failingStore struct{}

func (failingStore) Roles(context.Context) ([]Role, error) { return nil, errStorageUnavailable }
func (failingStore) GrantsFor(context.Context, string, string) ([]Grant, error) {
	return nil, errStorageUnavailable
}

type failingCache struct{}

func (failingCache) Get(context.Context, string) (permissionSet, bool, error) {
	return permissionSet{}, false, errors.New("redis 不可用")
}
func (failingCache) Set(context.Context, string, permissionSet) error {
	return errors.New("redis 不可用")
}
func (failingCache) Delete(context.Context, string) error { return errors.New("redis 不可用") }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
