package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件覆盖开发计划 23.1 / 23.3 / 23.4 的 HTTP 行为，
// 以及 002 §9.4（健康检查不越界）与 008（认证组件的基本底线）。

// ============================================================
// 夹具
// ============================================================

// stubPeople 是 people/basic 的替身（强依赖）。
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

// newTestService 组装一个可测的 service：内存凭据 + people 替身 + 固定时钟。
func newTestService(t *testing.T, people *stubPeople, creds ...Credential) (*service, *stubPeople) {
	t.Helper()

	if people == nil {
		people = &stubPeople{people: map[string]person{
			"p-1": {ID: "p-1", Name: "张三", DepartmentID: "d-tech"},
		}}
	}
	issuer := newTestIssuer(t, fixedClock("2026-01-01T10:00:00Z"))
	return newService(newMemoryStore(creds...), people, issuer, config{ComponentID: "auth/password-login"}), people
}

// seedCredential 造一条凭据（口令为 correct-horse-battery）。
func seedCredential(t *testing.T, username, personID string) Credential {
	t.Helper()

	hash, err := hashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("造口令哈希失败：%v", err)
	}
	return Credential{Username: username, PersonID: personID, PasswordHash: hash}
}

// login 发一次登录请求，返回状态码与响应体。
func login(t *testing.T, svc *service, body string) (int, map[string]any) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(body))
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
// 23.1 登录 API 正常
// ============================================================

func TestLoginReturnsToken(t *testing.T) {
	svc, people := newTestService(t, nil, seedCredential(t, "zhangsan", "p-1"))

	code, body := login(t, svc, `{"username":"zhangsan","password":"correct-horse-battery"}`)
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%v", code, body)
	}

	token, _ := body["token"].(string)
	if token == "" {
		t.Fatal("响应里没有 token")
	}
	if strings.Count(token, ".") != 2 {
		t.Errorf("token 应当是 JWT，实际 %q", token)
	}
	if body["expiresAt"] == nil {
		t.Error("响应要带 expiresAt，调用方才知道什么时候该重新登录")
	}

	// 强依赖必须真的被调到：登录不只是查自己的库，还要确认这个人还在
	if people.calls != 1 {
		t.Errorf("应当调用 people/basic 确认主体存在，实际调用 %d 次", people.calls)
	}
}

// TestLoginResponseNeverLeaksHash：响应里绝不能出现口令哈希。
func TestLoginResponseNeverLeaksHash(t *testing.T) {
	cred := seedCredential(t, "zhangsan", "p-1")
	svc, _ := newTestService(t, nil, cred)

	_, body := login(t, svc, `{"username":"zhangsan","password":"correct-horse-battery"}`)

	raw, _ := json.Marshal(body)
	for _, forbidden := range []string{cred.PasswordHash, "passwordHash", "correct-horse-battery"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("响应里泄露了 %q：%s", forbidden, raw)
		}
	}
}

// ============================================================
// 23.3 密码错误返回 401
// ============================================================

func TestLoginRejectsWrongPassword(t *testing.T) {
	svc, _ := newTestService(t, nil, seedCredential(t, "zhangsan", "p-1"))

	code, body := login(t, svc, `{"username":"zhangsan","password":"wrong-password"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("期望 401，实际 %d：%v", code, body)
	}
	if body["token"] != nil {
		t.Error("认证失败还发了 token")
	}
}

// TestLoginDoesNotRevealWhetherUserExists 挡用户枚举。
//
// "用户不存在"与"口令错误"必须给出**完全一样**的响应。任何差别——状态码、
// 文案、字段——都能被拿来把一份用户名字典筛成"这些账号真实存在"，
// 而那正是撞库的第一步。
func TestLoginDoesNotRevealWhetherUserExists(t *testing.T) {
	svc, _ := newTestService(t, nil, seedCredential(t, "zhangsan", "p-1"))

	wrongPassword, bodyA := login(t, svc, `{"username":"zhangsan","password":"wrong-password"}`)
	noSuchUser, bodyB := login(t, svc, `{"username":"nobody","password":"wrong-password"}`)

	if wrongPassword != noSuchUser {
		t.Errorf("状态码不一致：口令错 %d、用户不存在 %d", wrongPassword, noSuchUser)
	}

	a, _ := json.Marshal(bodyA)
	b, _ := json.Marshal(bodyB)
	if string(a) != string(b) {
		t.Errorf("响应体不一致，可被用来枚举用户：\n口令错：%s\n无此人：%s", a, b)
	}
}

// TestLoginRejectsMissingFields：请求本身不合法时是 400，不是 401。
//
// 两者要分清：401 是"你是谁我不认"，400 是"你根本没说清楚要什么"。
// 混在一起会让调用方以为是凭据问题，白查一圈。
func TestLoginRejectsMissingFields(t *testing.T) {
	svc, _ := newTestService(t, nil, seedCredential(t, "zhangsan", "p-1"))

	for name, body := range map[string]string{
		"没有用户名":   `{"password":"correct-horse-battery"}`,
		"没有口令":    `{"username":"zhangsan"}`,
		"不是 JSON": `not json at all`,
	} {
		t.Run(name, func(t *testing.T) {
			code, _ := login(t, svc, body)
			if code != http.StatusBadRequest {
				t.Errorf("期望 400，实际 %d", code)
			}
		})
	}
}

func TestLoginRejectsWrongMethod(t *testing.T) {
	svc, _ := newTestService(t, nil, seedCredential(t, "zhangsan", "p-1"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("期望 405，实际 %d", rec.Code)
	}
}

// ============================================================
// 强依赖 people/basic 的行为（002 §6：强依赖不可用要如实报）
// ============================================================

// TestLoginReportsDependencyOutageAsUnavailable：people/basic 挂了是 503，不是 401。
//
// 这条分得清清楚楚很重要：401 会让使用者以为自己密码错了，去反复重试、
// 去改密码；503 才会让他去看依赖组件。把基础设施故障报成认证失败，
// 是排障时最费时间的一类错误。
func TestLoginReportsDependencyOutageAsUnavailable(t *testing.T) {
	people := &stubPeople{err: errDependencyUnavailable}
	svc, _ := newTestService(t, people, seedCredential(t, "zhangsan", "p-1"))

	code, body := login(t, svc, `{"username":"zhangsan","password":"correct-horse-battery"}`)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d：%v", code, body)
	}
	if body["token"] != nil {
		t.Error("依赖不可用时不该发 token")
	}
}

// TestLoginRejectsWhenPersonGone：凭据还在，但人已经不在 people/basic 里了。
//
// 员工离职后从人员系统里删掉，凭据表却还留着——此时必须拒绝登录。
// 这正是把"身份"放在 people/basic、把"口令"放在本组件的意义：
// 主体的存废由人员系统说了算。
func TestLoginRejectsWhenPersonGone(t *testing.T) {
	people := &stubPeople{people: map[string]person{}} // 查无此人
	svc, _ := newTestService(t, people, seedCredential(t, "zhangsan", "p-1"))

	code, _ := login(t, svc, `{"username":"zhangsan","password":"correct-horse-battery"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("期望 401，实际 %d", code)
	}
}

// TestTokenCarriesPersonInfoFromDependency：令牌里的部门来自 people/basic，
// 不是本组件自己存的——那样就成了两份会漂移的数据。
func TestTokenCarriesPersonInfoFromDependency(t *testing.T) {
	people := &stubPeople{people: map[string]person{
		"p-1": {ID: "p-1", Name: "张三", DepartmentID: "d-backend"},
	}}
	svc, _ := newTestService(t, people, seedCredential(t, "zhangsan", "p-1"))

	_, body := login(t, svc, `{"username":"zhangsan","password":"correct-horse-battery"}`)
	token, _ := body["token"].(string)

	claims, err := svc.issuer.parse(token)
	if err != nil {
		t.Fatalf("解析令牌失败：%v", err)
	}
	if claims.DepartmentID != "d-backend" {
		t.Errorf("部门应当来自 people/basic，实际 %q", claims.DepartmentID)
	}
	if claims.Subject != "p-1" {
		t.Errorf("sub 应当是 personId，实际 %q", claims.Subject)
	}
}

// ============================================================
// 23.4 健康检查
// ============================================================

func TestHealthzReturns200(t *testing.T) {
	svc, _ := newTestService(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
}

// TestHealthzDoesNotTouchDependencies 是 002 §9.4 的硬约束。
//
// 健康检查只回答"本进程还活着吗"。若它去查库、去调 people/basic，
// 那么依赖一抖，编排系统就会把这个**本身完全正常**的容器杀掉重启——
// 故障就这样从一个组件扩散成一片。
func TestHealthzDoesNotTouchDependencies(t *testing.T) {
	people := &stubPeople{err: errDependencyUnavailable}
	// 存储也换成"一碰就报错"的：healthz 若查库，这里会立刻暴露
	svc := newService(failingStore{}, people,
		newTestIssuer(t, fixedClock("2026-01-01T10:00:00Z")), config{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("依赖全挂时 healthz 仍应返回 200，实际 %d", rec.Code)
	}
	if people.calls != 0 {
		t.Errorf("healthz 调用了 people/basic %d 次", people.calls)
	}
}

// TestStorageFailureIsNotAuthFailure：数据库挂了是 503，不是 401。
func TestStorageFailureIsNotAuthFailure(t *testing.T) {
	svc := newService(failingStore{}, &stubPeople{people: map[string]person{}},
		newTestIssuer(t, fixedClock("2026-01-01T10:00:00Z")), config{})

	code, body := login(t, svc, `{"username":"zhangsan","password":"correct-horse-battery"}`)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d：%v", code, body)
	}

	// 底层错误不外泄（004 §错误信息不暴露内部实现细节）
	raw, _ := json.Marshal(body)
	for _, leaked := range []string{"pq:", "sql", "connection refused", "postgres"} {
		if strings.Contains(strings.ToLower(string(raw)), leaked) {
			t.Errorf("响应泄露了底层实现细节 %q：%s", leaked, raw)
		}
	}
}

// ============================================================
// /api/v1/verify：令牌签出去之后总得有人能验
// ============================================================

// TestVerifyAcceptsIssuedToken 说明这个端点为什么存在。
//
// 计划里 Step 23 只要求"签发 JWT"。但令牌是用 HS256 签的——密钥只有本组件
// 有，别的组件拿到令牌根本验不了。没有这个端点，签出去的令牌对下游
// （erp/backend、authorization/rbac）就是一串不可用的字符串。
func TestVerifyAcceptsIssuedToken(t *testing.T) {
	svc, _ := newTestService(t, nil, seedCredential(t, "zhangsan", "p-1"))

	_, loginBody := login(t, svc, `{"username":"zhangsan","password":"correct-horse-battery"}`)
	token, _ := loginBody["token"].(string)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/verify",
		strings.NewReader(`{"token":"`+token+`"}`))
	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%s", rec.Body.String())
	}
	if body["personId"] != "p-1" || body["username"] != "zhangsan" {
		t.Errorf("verify 应返回令牌里的身份，实际 %v", body)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	svc, _ := newTestService(t, nil)

	for name, token := range map[string]string{
		"空":        "",
		"不是 JWT":   "hello",
		"alg=none": "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJwLWFkbWluIn0.",
		"三段但签名是假的": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJwLWFkbWluIn0.AAAA",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/verify",
				strings.NewReader(`{"token":"`+token+`"}`))
			rec := httptest.NewRecorder()
			svc.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("期望 401，实际 %d：%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// ============================================================
// 口令哈希
// ============================================================

// TestPasswordHashIsSalted：同一个口令两次哈希必须不同。
//
// 没有盐的话，哈希值本身就成了口令的指纹：一眼就能看出"这两个人用了
// 同一个密码"，彩虹表也能直接查。
func TestPasswordHashIsSalted(t *testing.T) {
	a, err := hashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("哈希失败：%v", err)
	}
	b, err := hashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("哈希失败：%v", err)
	}

	if a == b {
		t.Fatal("同一口令两次哈希不该相同——说明没有加盐")
	}
	if !verifyPassword(a, "correct-horse-battery") || !verifyPassword(b, "correct-horse-battery") {
		t.Fatal("加了盐也必须验得过")
	}
	if verifyPassword(a, "wrong-password") {
		t.Fatal("错误口令不该通过")
	}
}

// TestVerifyPasswordRejectsMalformedHash：库里的哈希坏了要判为失败，不能判为通过。
func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for _, hash := range []string{"", "not-a-hash", "$2a$", "plaintext-password"} {
		if verifyPassword(hash, "plaintext-password") {
			t.Errorf("损坏的哈希 %q 不该验证通过", hash)
		}
	}
}

// failingStore 是一碰就报错的存储替身。
type failingStore struct{}

func (failingStore) GetByUsername(context.Context, string) (Credential, error) {
	return Credential{}, errStorageUnavailable
}

func (failingStore) Upsert(context.Context, Credential) error { return errStorageUnavailable }
