package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authorizationv1 "github.com/brickkit/components/authorization-rbac/gen/authorization/v1"
)

// service 是授权判断的业务逻辑，同时作为 HTTP 处理器与 gRPC 服务实现。
//
// **一份逻辑、两个协议出口**：两边各写一遍权限计算，迟早出现
// "HTTP 说有、gRPC 说没有"——在权限系统里，那意味着同一个人在不同调用路径上
// 有不同的权限。service_test.go 里有专门的用例锁住两者一致。
type service struct {
	authorizationv1.UnimplementedAuthorizationServiceServer

	store  Store
	people peopleClient
	cache  Cache
	cfg    config
}

func newService(store Store, people peopleClient, cache Cache, cfg config) *service {
	return &service{store: store, people: people, cache: cache, cfg: cfg}
}

// 判断来源，写进 CheckResponse.reason 供排障用。
const (
	reasonDirect     = "direct"     // 直接授予这个人的角色
	reasonDepartment = "department" // 授予其所在部门的角色
	reasonNone       = "none"       // 没有任何路径给到这个权限
)

// ============================================================
// 业务逻辑（两种协议共用）
// ============================================================

// resolve 算出一个人的完整权限。
//
// 顺序是有讲究的：
//
//  1. **先看缓存**。命中就直接返回，连 people/basic 都不用调——这也让
//     people/basic 的短暂抖动不至于立刻变成全系统的鉴权失败。
//  2. 回源：先问 people/basic 要部门，再按「人 + 部门」查授权。
//  3. 写缓存。写失败只记一笔，不影响本次结果。
func (s *service) resolve(ctx context.Context, personID string) (permissionSet, bool, error) {
	// 缓存错误一律吞掉：它是加速器，不是数据源（见 Cache 的说明）
	if set, ok, err := s.cache.Get(ctx, personID); err == nil && ok {
		return set, true, nil
	}

	p, err := s.people.GetPerson(ctx, personID)
	if err != nil {
		// 这里**不做部分降级**：只知道直接授予的角色、不知道部门角色时，
		// 返回一个残缺的权限集比返回错误更危险——调用方会把它当成完整的用
		return permissionSet{}, false, err
	}

	grants, err := s.store.GrantsFor(ctx, personID, p.DepartmentID)
	if err != nil {
		return permissionSet{}, false, errStorageUnavailable
	}
	roles, err := s.store.Roles(ctx)
	if err != nil {
		return permissionSet{}, false, errStorageUnavailable
	}

	set := buildPermissionSet(p, grants, roles)
	// 写缓存失败不影响本次结果：下次再算一遍就是了
	_ = s.cache.Set(ctx, personID, set)
	return set, false, nil
}

// invalidate 让某个人的权限缓存立刻失效。
//
// 授权变更最常见的场景是"把某人的权限收回"。缓存不失效的话，被收回权限的人
// 在 TTL 到期前仍然畅通无阻——那是安全事故，不是延迟问题。
func (s *service) invalidate(ctx context.Context, personID string) error {
	return s.cache.Delete(ctx, personID)
}

// buildPermissionSet 把授权与角色合成最终的权限集合。
//
// 结果**排序且去重**：调用方常常直接比对这个数组，顺序随 map 遍历变的话，
// 同样的输入会得到不同的输出，缓存里也会存下两份内容不同但语义相同的值。
func buildPermissionSet(p person, grants []Grant, roles []Role) permissionSet {
	byID := make(map[string]Role, len(roles))
	for _, r := range roles {
		byID[r.ID] = r
	}

	roleIDs := map[string]bool{}
	permissions := map[string]bool{}
	for _, g := range grants {
		role, ok := byID[g.RoleID]
		if !ok {
			// 授权指向一个不存在的角色：跳过而不是报错。
			// 角色被删、授权还在，是运维过程中很正常的中间状态
			continue
		}
		roleIDs[role.ID] = true
		for _, permission := range role.Permissions {
			permissions[permission] = true
		}
	}

	return permissionSet{
		PersonID:     p.ID,
		DepartmentID: p.DepartmentID,
		Roles:        sortedKeys(roleIDs),
		Permissions:  sortedKeys(permissions),
	}
}

// check 判断某人是否拥有某个权限，并说明这个判断从哪来。
func (s *service) check(
	ctx context.Context, personID, permission string,
) (allowed bool, reason string, cached bool, err error) {
	set, cached, err := s.resolve(ctx, personID)
	if err != nil {
		return false, "", false, err
	}

	if !containsString(set.Permissions, permission) {
		return false, reasonNone, cached, nil
	}
	return true, s.reasonFor(ctx, set, permission), cached, nil
}

// reasonFor 说明这个权限是直接来的还是部门来的。
//
// 只在"确实有权限"时才算，因为它要多查一次授权——高频路径上不做无谓的工作。
// 算不出来时退回 direct：reason 是给人看的排障线索，不参与任何判定。
func (s *service) reasonFor(ctx context.Context, set permissionSet, permission string) string {
	grants, err := s.store.GrantsFor(ctx, set.PersonID, set.DepartmentID)
	if err != nil {
		return reasonDirect
	}
	roles, err := s.store.Roles(ctx)
	if err != nil {
		return reasonDirect
	}

	byID := make(map[string]Role, len(roles))
	for _, r := range roles {
		byID[r.ID] = r
	}
	for _, g := range grants {
		if role, ok := byID[g.RoleID]; ok && containsString(role.Permissions, permission) {
			if g.SubjectType == SubjectPerson {
				return reasonDirect
			}
			return reasonDepartment
		}
	}
	return reasonDirect
}

// ============================================================
// HTTP 出口
// ============================================================

func (s *service) routes() http.Handler {
	mux := http.NewServeMux()

	// 健康检查只回答"本进程还活着吗"（002 §9.4、开发计划 24.5）。
	// **不碰 Redis、不查库、不调 people/basic**：Redis 一抖，编排系统就会把这些
	// 本身完全正常的容器全部杀掉重启——而 Redis 在这里只是个加速器
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/permissions", s.handleListPermissions)
	mux.HandleFunc("/api/v1/check", s.handleCheck)
	return mux
}

func (s *service) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET")
		return
	}

	personID := r.URL.Query().Get("personId")
	if personID == "" {
		writeError(w, http.StatusBadRequest, "缺少查询参数 personId")
		return
	}

	set, cached, err := s.resolve(r.Context(), personID)
	if err != nil {
		writeResolveError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"personId":     set.PersonID,
		"departmentId": set.DepartmentID,
		// orEmpty：弱类型的调用方遍历 null 会直接崩
		"roles":       orEmpty(set.Roles),
		"permissions": orEmpty(set.Permissions),
		"cached":      cached,
	})
}

func (s *service) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 POST")
		return
	}

	var req struct {
		PersonID   string `json:"personId"`
		Permission string `json:"permission"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	if req.PersonID == "" || req.Permission == "" {
		writeError(w, http.StatusBadRequest, "personId 与 permission 都不能为空")
		return
	}

	allowed, reason, cached, err := s.check(r.Context(), req.PersonID, req.Permission)
	if err != nil {
		writeResolveError(w, err)
		return
	}

	// 没有权限是 200 + allowed:false，**不是 403**：这个端点回答的是
	// "他有没有这个权限"，调用方才决定要不要放行。用 403 会让调用方
	// 分不清"我没权限查这个接口"和"我查到了、答案是否"
	writeJSON(w, http.StatusOK, map[string]any{
		"personId":   req.PersonID,
		"permission": req.Permission,
		"allowed":    allowed,
		"reason":     reason,
		"cached":     cached,
	})
}

// writeResolveError 把内部错误映射成对外的状态码。
func writeResolveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPersonNotFound):
		writeError(w, http.StatusNotFound, "人员不存在")
	case errors.Is(err, errDependencyUnavailable):
		writeError(w, http.StatusServiceUnavailable, "依赖的人员服务暂时不可用，请稍后重试")
	case errors.Is(err, errStorageUnavailable):
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用，请稍后重试")
	default:
		writeError(w, http.StatusInternalServerError, "服务内部错误")
	}
}

// ============================================================
// gRPC 出口
// ============================================================

func (s *service) Check(
	ctx context.Context, req *authorizationv1.CheckRequest,
) (*authorizationv1.CheckResponse, error) {
	if req.GetPersonId() == "" || req.GetPermission() == "" {
		return nil, status.Error(codes.InvalidArgument, "person_id 与 permission 都不能为空")
	}

	allowed, reason, _, err := s.check(ctx, req.GetPersonId(), req.GetPermission())
	if err != nil {
		return nil, grpcError(err)
	}
	return &authorizationv1.CheckResponse{Allowed: allowed, Reason: reason}, nil
}

func (s *service) ListPermissions(
	ctx context.Context, req *authorizationv1.ListPermissionsRequest,
) (*authorizationv1.ListPermissionsResponse, error) {
	if req.GetPersonId() == "" {
		return nil, status.Error(codes.InvalidArgument, "person_id 不能为空")
	}

	set, cached, err := s.resolve(ctx, req.GetPersonId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &authorizationv1.ListPermissionsResponse{
		PersonId:    set.PersonID,
		Permissions: set.Permissions,
		Roles:       set.Roles,
		Cached:      cached,
	}, nil
}

// grpcError 把内部错误映射成 gRPC 状态码，与 HTTP 侧一一对应。
func grpcError(err error) error {
	switch {
	case errors.Is(err, errPersonNotFound):
		return status.Error(codes.NotFound, "人员不存在")
	case errors.Is(err, errDependencyUnavailable), errors.Is(err, errStorageUnavailable):
		return status.Error(codes.Unavailable, "服务暂时不可用，请稍后重试")
	default:
		return status.Error(codes.Internal, "服务内部错误")
	}
}

// ============================================================
// 辅助
// ============================================================

// maxBodyBytes 限制请求体大小：check 请求就几十字节，
// 不设上限的话一个超大 body 就能把内存吃干净。
const maxBodyBytes = 64 << 10

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// orEmpty 保证 JSON 里出现 [] 而不是 null。
func orEmpty(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError 输出统一的错误结构，**不带底层原因**：把 "pq: connection refused"
// 透出去既帮不上调用方，又把内部拓扑告诉了外面。
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
