// 本文件是 Step 21「department/tree」的业务行为测试。
//
// 覆盖开发计划 21.1（HTTP）、21.2（gRPC）、21.4/21.5（健康检查），
// 以及这个组件存在的意义：**同一份数据必须能从两种协议拿到一样的结果**。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	departmentv1 "github.com/brickkit/components/department-tree/gen/department/v1"
)

// ============================================================
// 夹具
// ============================================================

// seedTree 是测试用的部门树：
//
//	总公司
//	├── 技术中心
//	│   └── 后端组
//	└── 人力资源部
func seedTree() []Department {
	return []Department{
		{ID: "d-root", Name: "总公司", ParentID: "", Level: 1},
		{ID: "d-tech", Name: "技术中心", ParentID: "d-root", Level: 2},
		{ID: "d-hr", Name: "人力资源部", ParentID: "d-root", Level: 2},
		{ID: "d-backend", Name: "后端组", ParentID: "d-tech", Level: 3},
	}
}

func newTestService() *service {
	return newService(newMemoryStore(seedTree()...), config{ComponentID: "department/tree", Version: "1.0.0"})
}

// getJSON 发一次 HTTP 请求并解析响应。
func getJSON(t *testing.T, svc *service, path string) (int, map[string]any) {
	t.Helper()

	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	body := map[string]any{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("响应不是合法 JSON：%s", rec.Body.String())
		}
	}
	return rec.Code, body
}

// idsOf 从 HTTP 响应里取出部门 ID 列表。
func idsOf(t *testing.T, body map[string]any) []string {
	t.Helper()

	items, ok := body["departments"].([]any)
	if !ok {
		t.Fatalf("响应里没有 departments 数组：%v", body)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		dept, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("departments 元素不是对象：%v", item)
		}
		out = append(out, dept["id"].(string))
	}
	return out
}

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

// ============================================================
// 21.1 HTTP API
// ============================================================

func TestListDepartmentsOverHTTP(t *testing.T) {
	svc := newTestService()

	code, body := getJSON(t, svc, "/api/v1/departments")

	if code != http.StatusOK {
		t.Fatalf("21.1 期望 200，实际 %d", code)
	}
	got := idsOf(t, body)
	want := []string{"d-backend", "d-hr", "d-root", "d-tech"}
	if !equalStrings(got, want) {
		t.Fatalf("期望按 ID 排序的全部部门 %v，实际 %v", want, got)
	}
	if total, _ := body["total"].(float64); int(total) != 4 {
		t.Fatalf("total 应为 4，实际 %v", body["total"])
	}
}

// parentId 过滤只返回直接下级，不返回孙节点。
func TestListDepartmentsFilteredByParent(t *testing.T) {
	svc := newTestService()

	_, body := getJSON(t, svc, "/api/v1/departments?parentId=d-root")

	got := idsOf(t, body)
	want := []string{"d-hr", "d-tech"}
	if !equalStrings(got, want) {
		t.Fatalf("期望只返回直接下级 %v，实际 %v", want, got)
	}
}

func TestGetDepartmentOverHTTP(t *testing.T) {
	svc := newTestService()

	code, body := getJSON(t, svc, "/api/v1/departments/d-tech")

	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", code)
	}
	if body["name"] != "技术中心" {
		t.Fatalf("期望技术中心，实际 %v", body["name"])
	}
	if body["parentId"] != "d-root" {
		t.Fatalf("parentId 应为 d-root，实际 %v", body["parentId"])
	}
}

// 查不到要给 404 与明确的错误信息，而不是 200 + 空对象。
func TestGetUnknownDepartmentReturns404(t *testing.T) {
	svc := newTestService()

	code, body := getJSON(t, svc, "/api/v1/departments/nobody")

	if code != http.StatusNotFound {
		t.Fatalf("期望 404，实际 %d", code)
	}
	if body["error"] == nil {
		t.Fatalf("404 响应要说明原因：%v", body)
	}
}

// 子树返回该部门自己 + 全部下级（不止一层）。
func TestSubtreeIncludesAllDescendants(t *testing.T) {
	svc := newTestService()

	code, body := getJSON(t, svc, "/api/v1/departments/d-tech/subtree")

	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", code)
	}
	got := idsOf(t, body)
	want := []string{"d-backend", "d-tech"}
	if !equalStrings(got, want) {
		t.Fatalf("期望 %v（自己 + 下级），实际 %v", want, got)
	}
}

func TestSubtreeOfUnknownDepartmentReturns404(t *testing.T) {
	svc := newTestService()

	code, _ := getJSON(t, svc, "/api/v1/departments/nobody/subtree")

	if code != http.StatusNotFound {
		t.Fatalf("期望 404，实际 %d", code)
	}
}

// ============================================================
// 21.2 gRPC API
// ============================================================

func TestListDepartmentsOverGRPC(t *testing.T) {
	svc := newTestService()

	resp, err := svc.ListDepartments(context.Background(), &departmentv1.ListDepartmentsRequest{})
	if err != nil {
		t.Fatalf("21.2 gRPC 调用失败：%v", err)
	}

	if len(resp.Departments) != 4 || resp.Total != 4 {
		t.Fatalf("期望 4 个部门，实际 %d（total=%d）", len(resp.Departments), resp.Total)
	}
	if resp.Departments[0].Id != "d-backend" {
		t.Fatalf("顺序应与 HTTP 一致，实际首个为 %s", resp.Departments[0].Id)
	}
}

func TestGetDepartmentOverGRPC(t *testing.T) {
	svc := newTestService()

	dept, err := svc.GetDepartment(context.Background(),
		&departmentv1.GetDepartmentRequest{Id: "d-backend"})
	if err != nil {
		t.Fatalf("gRPC 调用失败：%v", err)
	}

	if dept.Name != "后端组" || dept.ParentId != "d-tech" || dept.Level != 3 {
		t.Fatalf("字段不对：%+v", dept)
	}
}

// gRPC 的"查不到"要用 NOT_FOUND 状态码，不是返回空对象。
func TestGetUnknownDepartmentOverGRPCReturnsNotFound(t *testing.T) {
	svc := newTestService()

	_, err := svc.GetDepartment(context.Background(),
		&departmentv1.GetDepartmentRequest{Id: "nobody"})

	if err == nil {
		t.Fatal("期望报错")
	}
	if !isNotFound(err) {
		t.Fatalf("期望 NOT_FOUND 状态码，实际：%v", err)
	}
}

func TestSubtreeOverGRPC(t *testing.T) {
	svc := newTestService()

	resp, err := svc.GetSubtree(context.Background(),
		&departmentv1.GetSubtreeRequest{RootId: "d-root"})
	if err != nil {
		t.Fatalf("gRPC 调用失败：%v", err)
	}

	if len(resp.Departments) != 4 {
		t.Fatalf("d-root 的子树应含全部 4 个部门，实际 %d", len(resp.Departments))
	}
}

// ============================================================
// 双协议一致性
// ============================================================

// 同一份数据从两种协议拿到的结果必须一致 ——
// 这正是"单端口双协议"这件事的意义：一套业务逻辑，两种协议出口。
// 如果两边各写一遍逻辑，迟早会出现"HTTP 说有、gRPC 说没有"。
func TestHTTPAndGRPCReturnTheSameData(t *testing.T) {
	svc := newTestService()

	_, body := getJSON(t, svc, "/api/v1/departments")
	overHTTP := idsOf(t, body)

	resp, err := svc.ListDepartments(context.Background(), &departmentv1.ListDepartmentsRequest{})
	if err != nil {
		t.Fatalf("gRPC 调用失败：%v", err)
	}
	overGRPC := make([]string, 0, len(resp.Departments))
	for _, d := range resp.Departments {
		overGRPC = append(overGRPC, d.Id)
	}

	if !equalStrings(overHTTP, overGRPC) {
		t.Fatalf("两种协议结果不一致：HTTP=%v gRPC=%v", overHTTP, overGRPC)
	}
}

// 过滤条件在两种协议下的语义也必须一样。
func TestFilterIsConsistentAcrossProtocols(t *testing.T) {
	svc := newTestService()

	_, body := getJSON(t, svc, "/api/v1/departments?parentId=d-root")
	overHTTP := idsOf(t, body)

	resp, err := svc.ListDepartments(context.Background(),
		&departmentv1.ListDepartmentsRequest{ParentId: "d-root"})
	if err != nil {
		t.Fatalf("gRPC 调用失败：%v", err)
	}
	overGRPC := make([]string, 0, len(resp.Departments))
	for _, d := range resp.Departments {
		overGRPC = append(overGRPC, d.Id)
	}

	if !equalStrings(overHTTP, overGRPC) {
		t.Fatalf("过滤结果不一致：HTTP=%v gRPC=%v", overHTTP, overGRPC)
	}
}

// ============================================================
// 21.4 / 21.5 健康检查
// ============================================================

func TestHealthzReturns200(t *testing.T) {
	svc := newTestService()

	code, body := getJSON(t, svc, "/healthz")

	if code != http.StatusOK {
		t.Fatalf("21.4 期望 200，实际 %d", code)
	}
	if body["status"] != "ok" {
		t.Fatalf("期望 status=ok，实际 %v", body["status"])
	}
}

// failingStore 的每个方法都报错，用来证明健康检查没碰数据。
type failingStore struct{ calls int }

func (s *failingStore) List(context.Context, string) ([]Department, error) {
	s.calls++
	return nil, errors.New("数据库连接已断开")
}

func (s *failingStore) Get(context.Context, string) (Department, error) {
	s.calls++
	return Department{}, errors.New("数据库连接已断开")
}

func (s *failingStore) Subtree(context.Context, string) ([]Department, error) {
	s.calls++
	return nil, errors.New("数据库连接已断开")
}

// 002 §9.4：/healthz 只检查本进程存活，**禁止**检查数据库或任何外部系统。
//
// 这条不是洁癖：健康检查一旦连库，数据库抖一下就会让所有组件被判死重启，
// 把一次数据库故障放大成整个系统雪崩。
func TestHealthzDoesNotTouchTheDatabase(t *testing.T) {
	store := &failingStore{}
	svc := newService(store, config{ComponentID: "department/tree", Version: "1.0.0"})

	rec := httptest.NewRecorder()
	svc.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("21.5 数据库不可用时 /healthz 仍必须返回 200，实际 %d", rec.Code)
	}
	if store.calls != 0 {
		t.Fatalf("21.5 /healthz 不该访问数据存储，实际调用了 %d 次", store.calls)
	}
}

// 数据库真的坏了时，业务接口要如实报 503，而不是假装成功。
func TestBusinessEndpointReportsStoreFailure(t *testing.T) {
	svc := newService(&failingStore{}, config{ComponentID: "department/tree", Version: "1.0.0"})

	code, body := getJSON(t, svc, "/api/v1/departments")

	if code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d", code)
	}
	if body["error"] == nil {
		t.Fatalf("要说明原因：%v", body)
	}
	// 002 §11.3：错误信息不向外暴露内部实现细节
	if msg, _ := body["error"].(string); msg == "数据库连接已断开" {
		t.Fatalf("不该把底层错误原样透出：%v", msg)
	}
}
