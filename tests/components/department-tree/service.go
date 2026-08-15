package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	departmentv1 "github.com/brickkit/components/department-tree/gen/department/v1"
)

// service 是业务逻辑，同时作为 HTTP 处理器与 gRPC 服务实现。
//
// **一份逻辑、两个协议出口**：如果 HTTP 与 gRPC 各写一遍查询，
// 迟早出现"HTTP 说有、gRPC 说没有"。两者都只是这里的薄壳。
type service struct {
	departmentv1.UnimplementedDepartmentServiceServer

	store Store
	cfg   config
}

func newService(store Store, cfg config) *service {
	return &service{store: store, cfg: cfg}
}

// ============================================================
// 业务逻辑（两种协议共用）
// ============================================================

func (s *service) list(ctx context.Context, parentID string) ([]Department, error) {
	return s.store.List(ctx, parentID)
}

func (s *service) get(ctx context.Context, id string) (Department, error) {
	return s.store.Get(ctx, id)
}

func (s *service) subtree(ctx context.Context, rootID string) ([]Department, error) {
	return s.store.Subtree(ctx, rootID)
}

// ============================================================
// gRPC 出口
// ============================================================

func (s *service) ListDepartments(
	ctx context.Context, req *departmentv1.ListDepartmentsRequest,
) (*departmentv1.ListDepartmentsResponse, error) {
	items, err := s.list(ctx, req.GetParentId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &departmentv1.ListDepartmentsResponse{
		Departments: toProtoList(items),
		Total:       int32(len(items)),
	}, nil
}

func (s *service) GetDepartment(
	ctx context.Context, req *departmentv1.GetDepartmentRequest,
) (*departmentv1.Department, error) {
	d, err := s.get(ctx, req.GetId())
	if err != nil {
		return nil, grpcError(err)
	}
	return toProto(d), nil
}

func (s *service) GetSubtree(
	ctx context.Context, req *departmentv1.GetSubtreeRequest,
) (*departmentv1.ListDepartmentsResponse, error) {
	items, err := s.subtree(ctx, req.GetRootId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &departmentv1.ListDepartmentsResponse{
		Departments: toProtoList(items),
		Total:       int32(len(items)),
	}, nil
}

func toProto(d Department) *departmentv1.Department {
	return &departmentv1.Department{
		Id: d.ID, Name: d.Name, ParentId: d.ParentID, Level: int32(d.Level),
	}
}

func toProtoList(items []Department) []*departmentv1.Department {
	out := make([]*departmentv1.Department, 0, len(items))
	for _, d := range items {
		out = append(out, toProto(d))
	}
	return out
}

// grpcError 把内部错误翻译成 gRPC 状态码。
//
// 存储故障对外只说"暂时不可用"：错误信息不暴露内部实现细节（002 §11.3）。
func grpcError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return status.Error(codes.NotFound, "部门不存在")
	}
	return status.Error(codes.Unavailable, "部门数据暂时不可用")
}

// isNotFound 判断是不是 NOT_FOUND 状态码。
func isNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}

// ============================================================
// HTTP 出口
// ============================================================

// routes 返回 HTTP 路由。
func (s *service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/openapi.json", handleOpenAPI)
	mux.HandleFunc("/api/v1/departments", s.handleList)
	mux.HandleFunc("/api/v1/departments/", s.handleByID)
	mux.HandleFunc("/", s.handleNotFound)
	return mux
}

// handleHealthz 只回答"本进程还活着吗"。
//
// 002 §9.4 明令禁止在这里检查数据库或依赖组件：健康检查一旦连库，
// 数据库抖一下就会让所有组件被判死重启，把一次故障放大成雪崩。
func (s *service) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"component": s.cfg.ComponentID,
		"version":   s.cfg.Version,
	})
}

func (s *service) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET")
		return
	}

	items, err := s.list(r.Context(), r.URL.Query().Get("parentId"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"departments": items,
		"total":       len(items),
	})
}

// handleByID 处理 /api/v1/departments/{id} 与 /api/v1/departments/{id}/subtree。
func (s *service) handleByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET")
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/departments/")
	id, suffix, _ := strings.Cut(rest, "/")
	if id == "" {
		writeError(w, http.StatusNotFound, "缺少部门 ID")
		return
	}

	switch suffix {
	case "":
		d, err := s.get(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, d)

	case "subtree":
		items, err := s.subtree(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"departments": items,
			"total":       len(items),
		})

	default:
		writeError(w, http.StatusNotFound, "未知的子资源："+suffix)
	}
}

func (s *service) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "接口不存在："+r.URL.Path)
}

// writeStoreError 把存储错误翻译成 HTTP 状态码。
func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "部门不存在")
		return
	}
	// 底层错误不外泄（002 §11.3），真实原因留在服务端日志里
	writeError(w, http.StatusServiceUnavailable, "部门数据暂时不可用")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

// openapiSpec 是本组件的 OpenAPI 文档，随二进制一起打包。
//
// 为什么要在运行时也暴露一份：它同时是**市场产物**（发布时上传，
// 调用方 add 之后就能拿到）。但产物是**发布那一刻**的快照，
// 而 infra/api-docs 之类的工具要回答的是"**此刻跑着的**服务长什么样"——
// 组件升级之后，只有运行时这一份是准的。
//
//go:embed openapi.json
var openapiSpec []byte

// handleOpenAPI 把本组件的 API 文档发出去。
//
// 路径固定为 /openapi.json：这是 FastAPI 之类的框架的惯例，
// 文档聚合组件也按这个路径来探（002 §7 契约即产物）。
func handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// 文档不常变，让代理与浏览器缓存一会儿
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openapiSpec)
}
