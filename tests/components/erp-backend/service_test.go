package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖开发计划 25.1–25.7。
//
// erp/backend 是一个**连接组件**：它自己几乎没有数据，价值全在"把四个组件
// 正确地串起来"。因此这里的测试大多不是在测业务逻辑，而是在测**编排**：
// 谁在什么时候被调用、某一个挂了会怎样、弱依赖缺席时还能不能干活。

// ============================================================
// 依赖替身
// ============================================================

type stubAuth struct {
	identities map[string]identity
	err        error
	calls      int
}

func (s *stubAuth) Login(_ context.Context, username, _ string) (loginResult, error) {
	s.calls++
	if s.err != nil {
		return loginResult{}, s.err
	}
	for token, id := range s.identities {
		if id.Username == username {
			return loginResult{Token: token, PersonID: id.PersonID, Username: id.Username}, nil
		}
	}
	return loginResult{}, errTokenInvalid
}

func (s *stubAuth) Verify(_ context.Context, token string) (identity, error) {
	s.calls++
	if s.err != nil {
		return identity{}, s.err
	}
	id, ok := s.identities[token]
	if !ok {
		return identity{}, errTokenInvalid
	}
	return id, nil
}

type stubAuthorization struct {
	allowed map[string]bool // "personId|permission" → 允许与否
	err     error
	calls   int
}

func (s *stubAuthorization) Check(_ context.Context, personID, permission string) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	return s.allowed[personID+"|"+permission], nil
}

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

// stubEventBus 记录发出去的事件；err 非 nil 时模拟事件总线故障。
type stubEventBus struct {
	events []event
	err    error
}

func (s *stubEventBus) Publish(_ context.Context, e event) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, e)
	return nil
}

func (s *stubEventBus) Enabled() bool { return true }

// ============================================================
// 夹具
// ============================================================

type deps struct {
	auth  *stubAuth
	authz *stubAuthorization
	ppl   *stubPeople
	bus   *stubEventBus
}

func newDeps() *deps {
	return &deps{
		auth: &stubAuth{identities: map[string]identity{
			"token-zhangsan": {PersonID: "p-001", Username: "zhangsan", DepartmentID: "d-tech"},
			"token-zhaoliu":  {PersonID: "p-004", Username: "zhaoliu", DepartmentID: "d-backend"},
		}},
		authz: &stubAuthorization{allowed: map[string]bool{
			"p-001|erp.order.read":    true,
			"p-001|erp.order.approve": true,
			// p-004 什么权限都没有
		}},
		ppl: &stubPeople{people: map[string]person{
			"p-001": {ID: "p-001", Name: "张三", DepartmentID: "d-tech", DepartmentName: "技术中心"},
			"p-004": {ID: "p-004", Name: "赵六", DepartmentID: "d-backend", DepartmentName: "后端组"},
		}},
		bus: &stubEventBus{},
	}
}

func newTestService(t *testing.T, d *deps, cfg config) *service {
	t.Helper()

	if d == nil {
		d = newDeps()
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = time.Hour
	}
	var bus eventBus = d.bus
	if d.bus == nil {
		bus = disabledEventBus{}
	}
	return newService(newMemoryOrders(seedOrders()), d.auth, d.authz, d.ppl, bus, cfg)
}

// do 发一次带 Bearer 令牌的请求。
func do(t *testing.T, svc *service, method, path, token, body string) (int, map[string]any) {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	var decoded map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("响应不是合法 JSON：%s", rec.Body.String())
		}
	}
	return rec.Code, decoded
}

// ============================================================
// 25.1 所有强依赖调用正常
// ============================================================

// TestListOrdersCallsEveryStrongDependency 是 25.1 的主干。
//
// 一次业务请求要串起三个强依赖：
//
//	auth/password-login   这个令牌是谁的
//	authorization/rbac    这个人能不能看订单
//	people/basic          把人员姓名补上（gRPC，走 extraPorts）
//
// 少调一个都可能是"悄悄放行"：不问 rbac 就等于人人都能看。
func TestListOrdersCallsEveryStrongDependency(t *testing.T) {
	d := newDeps()
	svc := newTestService(t, d, config{})

	code, body := do(t, svc, http.MethodGet, "/api/v1/orders", "token-zhangsan", "")
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%v", code, body)
	}

	if d.auth.calls == 0 {
		t.Error("没有调用 auth/password-login 校验令牌")
	}
	if d.authz.calls == 0 {
		t.Error("没有调用 authorization/rbac 检查权限 —— 那等于人人都能看")
	}
	if d.ppl.calls == 0 {
		t.Error("没有调用 people/basic 补全人员信息")
	}

	orders, ok := body["orders"].([]any)
	if !ok || len(orders) == 0 {
		t.Fatalf("应当返回订单列表：%v", body)
	}
}

// TestOrderCarriesEnrichedPersonInfo：姓名来自 people/basic，不在本组件存。
//
// 连接组件的意义就在这里：它不重复存人员数据，每次现取。
func TestOrderCarriesEnrichedPersonInfo(t *testing.T) {
	svc := newTestService(t, nil, config{})

	_, body := do(t, svc, http.MethodGet, "/api/v1/orders", "token-zhangsan", "")
	orders := body["orders"].([]any)
	first := orders[0].(map[string]any)

	if first["ownerName"] != "张三" {
		t.Errorf("订单上的人员姓名应当来自 people/basic，实际 %v", first["ownerName"])
	}
	if first["ownerDepartment"] != "技术中心" {
		t.Errorf("部门名也该带上（people/basic 会补全它）：%v", first["ownerDepartment"])
	}
}

// ============================================================
// 认证与授权的边界
// ============================================================

func TestMissingTokenIsUnauthorized(t *testing.T) {
	d := newDeps()
	svc := newTestService(t, d, config{})

	code, _ := do(t, svc, http.MethodGet, "/api/v1/orders", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("没有令牌应当 401，实际 %d", code)
	}
	// 没令牌就不必再去问 rbac 与 people——省两次网络往返
	if d.authz.calls != 0 || d.ppl.calls != 0 {
		t.Error("认证没过就不该继续调用下游")
	}
}

func TestInvalidTokenIsUnauthorized(t *testing.T) {
	svc := newTestService(t, nil, config{})

	code, _ := do(t, svc, http.MethodGet, "/api/v1/orders", "token-forged", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("无效令牌应当 401，实际 %d", code)
	}
}

// TestNoPermissionIsForbidden：认证过了但没权限是 403，不是 401。
//
// 401 是"我不知道你是谁"，403 是"我知道你是谁，但你不能做这个"。
// 混在一起会让调用方去重新登录，而登录一百次也不会有权限。
func TestNoPermissionIsForbidden(t *testing.T) {
	svc := newTestService(t, nil, config{})

	code, _ := do(t, svc, http.MethodGet, "/api/v1/orders", "token-zhaoliu", "")
	if code != http.StatusForbidden {
		t.Fatalf("没有权限应当 403，实际 %d", code)
	}
}

// TestApproveNeedsStrongerPermission：不同操作查不同的权限。
func TestApproveNeedsStrongerPermission(t *testing.T) {
	d := newDeps()
	// 只给读的权限，不给审批
	d.authz.allowed = map[string]bool{"p-001|erp.order.read": true}
	svc := newTestService(t, d, config{})

	code, _ := do(t, svc, http.MethodPost, "/api/v1/orders/o-1/approve", "token-zhangsan", "")
	if code != http.StatusForbidden {
		t.Fatalf("只有读权限时审批应当 403，实际 %d", code)
	}
}

// ============================================================
// 强依赖故障（002 §6：如实报，不假装）
// ============================================================

func TestStrongDependencyOutageIsUnavailable(t *testing.T) {
	cases := map[string]func(*deps){
		"auth 挂了":   func(d *deps) { d.auth.err = errDependencyUnavailable },
		"rbac 挂了":   func(d *deps) { d.authz.err = errDependencyUnavailable },
		"people 挂了": func(d *deps) { d.ppl.err = errDependencyUnavailable },
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDeps()
			breakIt(d)
			svc := newTestService(t, d, config{})

			code, body := do(t, svc, http.MethodGet, "/api/v1/orders", "token-zhangsan", "")
			if code != http.StatusServiceUnavailable {
				t.Fatalf("强依赖挂了应当 503，实际 %d：%v", code, body)
			}

			// 底层细节不外泄
			raw, _ := json.Marshal(body)
			for _, leaked := range []string{"grpc", "dial tcp", "connection refused"} {
				if strings.Contains(strings.ToLower(string(raw)), leaked) {
					t.Errorf("响应泄露了底层细节 %q：%s", leaked, raw)
				}
			}
		})
	}
}

// ============================================================
// 25.2 / 25.3 弱依赖：事件总线
// ============================================================

// TestApprovePublishesEvent 是 25.2 的单元层验证。
//
// 真实的端到端验证要等 infra/redis-event-bus 建好（Step 27）。
func TestApprovePublishesEvent(t *testing.T) {
	d := newDeps()
	svc := newTestService(t, d, config{})

	code, _ := do(t, svc, http.MethodPost, "/api/v1/orders/o-1/approve", "token-zhangsan", "")
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", code)
	}

	if len(d.bus.events) != 1 {
		t.Fatalf("应当发出一条事件，实际 %d 条", len(d.bus.events))
	}
	e := d.bus.events[0]
	if e.Type != eventOrderApproved {
		t.Errorf("事件类型不对：%q", e.Type)
	}
	if e.Actor != "p-001" || e.Subject != "o-1" {
		t.Errorf("事件内容不对：%+v", e)
	}
}

// TestApproveSucceedsWhenEventBusDisabled 是 25.3 的核心。
//
// 弱依赖缺席时，平台**完全不注入** INFRA_REDIS_EVENT_BUS_ENDPOINT（003 §4.3、
// 开发进度 D140）。此时业务必须照常完成——弱依赖的定义就是"有就用、没有就降级"。
// 若因为发不出事件而让审批失败，它就成了事实上的强依赖。
func TestApproveSucceedsWhenEventBusDisabled(t *testing.T) {
	d := newDeps()
	d.bus = nil // 触发 disabledEventBus
	svc := newTestService(t, d, config{})

	code, body := do(t, svc, http.MethodPost, "/api/v1/orders/o-1/approve", "token-zhangsan", "")
	if code != http.StatusOK {
		t.Fatalf("弱依赖缺席时业务必须照常完成，实际 %d：%v", code, body)
	}
	if body["eventPublished"] != false {
		t.Error("要如实告诉调用方事件没发出去，而不是假装发了")
	}
}

// TestApproveSucceedsWhenEventBusFails：总线在，但调用失败。
//
// 与"没配置"是两种情形，但结论一样：不能因此让业务失败。
func TestApproveSucceedsWhenEventBusFails(t *testing.T) {
	d := newDeps()
	d.bus.err = errors.New("connection refused")
	svc := newTestService(t, d, config{})

	code, body := do(t, svc, http.MethodPost, "/api/v1/orders/o-1/approve", "token-zhangsan", "")
	if code != http.StatusOK {
		t.Fatalf("事件发送失败不该让审批失败，实际 %d：%v", code, body)
	}
	if body["eventPublished"] != false {
		t.Error("发失败了就要如实说 false")
	}
}

// TestOrderStateChangesEvenWithoutEvent：降级不能只是"接口返回 200"。
//
// 真正要保证的是**业务状态确实变了**。若审批在发事件失败时回滚，
// 那就是把弱依赖偷偷变成了强依赖。
func TestOrderStateChangesEvenWithoutEvent(t *testing.T) {
	d := newDeps()
	d.bus.err = errors.New("connection refused")
	svc := newTestService(t, d, config{})

	if code, _ := do(t, svc, http.MethodPost, "/api/v1/orders/o-1/approve", "token-zhangsan", ""); code != http.StatusOK {
		t.Fatalf("审批应当成功，实际 %d", code)
	}

	_, body := do(t, svc, http.MethodGet, "/api/v1/orders", "token-zhangsan", "")
	orders := body["orders"].([]any)
	for _, item := range orders {
		o := item.(map[string]any)
		if o["id"] == "o-1" {
			if o["status"] != orderApproved {
				t.Fatalf("订单状态应当已变为 %s，实际 %v —— 弱依赖失败时业务被回滚了",
					orderApproved, o["status"])
			}
			return
		}
	}
	t.Fatal("没找到 o-1")
}

// ============================================================
// 25.5 config 覆盖
// ============================================================

// TestSessionTTLComesFromConfig：SESSION_TTL_SECONDS 真的被用上了。
//
// 只断言"环境变量读进来了"是不够的——那只能证明配置解析没错，
// 证明不了它影响了任何行为。这里断言它出现在**响应**里。
func TestSessionTTLComesFromConfig(t *testing.T) {
	svc := newTestService(t, nil, config{SessionTTL: 2 * time.Hour})

	code, body := do(t, svc, http.MethodPost, "/api/v1/login", "",
		`{"username":"zhangsan","password":"demo-password"}`)
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%v", code, body)
	}

	ttl, ok := body["sessionTtlSeconds"].(float64)
	if !ok {
		t.Fatalf("响应里应当有 sessionTtlSeconds：%v", body)
	}
	if int(ttl) != 7200 {
		t.Errorf("config 覆盖应当生效：期望 7200，实际 %d", int(ttl))
	}
}

// ============================================================
// 25.6 / 25.7 健康检查
// ============================================================

func TestHealthzReturns200(t *testing.T) {
	svc := newTestService(t, nil, config{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
}

// TestHealthzDoesNotCheckDependencies 是 25.7。
//
// 连接组件依赖四个组件；健康检查若逐个去探，任意一个抖动都会让它被杀掉重启。
// 而这个组件本身完全正常——它只是暂时干不了活，那该由业务接口如实报 503。
func TestHealthzDoesNotCheckDependencies(t *testing.T) {
	d := newDeps()
	d.auth.err = errDependencyUnavailable
	d.authz.err = errDependencyUnavailable
	d.ppl.err = errDependencyUnavailable
	d.bus.err = errors.New("down")
	svc := newTestService(t, d, config{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("依赖全挂时 healthz 仍应 200，实际 %d", rec.Code)
	}
	if d.auth.calls+d.authz.calls+d.ppl.calls != 0 {
		t.Errorf("healthz 调用了下游组件：auth=%d rbac=%d people=%d",
			d.auth.calls, d.authz.calls, d.ppl.calls)
	}
}
