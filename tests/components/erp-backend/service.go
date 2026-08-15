package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// service 是 erp/backend 的业务逻辑与 HTTP 出口。
//
// 它是一个**连接组件**：自己几乎没有数据，价值全在"把四个组件正确地串起来"。
// 一次订单查询要走三个强依赖：
//
//	auth/password-login   这个令牌是谁的
//	authorization/rbac    这个人能不能看订单（gRPC）
//	people/basic          把人员姓名与部门补上（gRPC，走 extraPorts 的 9090）
//
// 外加一个弱依赖 infra/redis-event-bus：有就发事件，没有就跳过。
type service struct {
	orders Orders
	auth   authClient
	authz  authorizationClient
	people peopleClient
	bus    eventBus
	cfg    config
}

func newService(
	orders Orders, auth authClient, authz authorizationClient,
	people peopleClient, bus eventBus, cfg config,
) *service {
	return &service{orders: orders, auth: auth, authz: authz, people: people, bus: bus, cfg: cfg}
}

// 本组件用到的权限名。它们由 authorization/rbac 的数据定义，这里只是引用。
const (
	permissionOrderRead    = "erp.order.read"
	permissionOrderApprove = "erp.order.approve"
)

// ============================================================
// 路由
// ============================================================

func (s *service) routes() http.Handler {
	mux := http.NewServeMux()

	// 健康检查只回答"本进程还活着吗"（002 §9.4、开发计划 25.7）。
	// 连接组件依赖四个组件；健康检查若逐个去探，任意一个抖动都会让它被杀掉重启，
	// 而它本身完全正常——只是暂时干不了活，那该由业务接口如实报 503
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/login", s.handleLogin)
	mux.HandleFunc("/api/v1/orders", s.handleListOrders)
	mux.HandleFunc("/api/v1/orders/", s.handleOrderAction)
	return mux
}

// ============================================================
// POST /api/v1/login —— 转交给 auth/password-login
// ============================================================

// handleLogin 把登录转交给 auth 组件，并附上本组件的会话策略。
//
// 本组件**不碰口令**：它没有凭据表，也不该有。这里只是把前端的登录请求
// 转给真正管认证的那个组件，再把 sessionTtlSeconds 这类本组件的策略带回去。
func (s *service) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 POST")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username 与 password 都不能为空")
		return
	}

	result, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeDependencyError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":    result.Token,
		"personId": result.PersonID,
		"username": result.Username,
		// 25.5：会话时长是**本组件**的配置（configSchema.sessionTtlSeconds），
		// 由使用者在 brickkit.yaml 里覆盖，与 auth 组件自己的令牌有效期无关
		"sessionTtlSeconds": int(s.cfg.SessionTTL.Seconds()),
	})
}

// ============================================================
// GET /api/v1/orders
// ============================================================

func (s *service) handleListOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET")
		return
	}

	id, ok := s.authorize(w, r, permissionOrderRead)
	if !ok {
		return
	}

	orders, err := s.orders.List(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用，请稍后重试")
		return
	}

	enriched, err := s.enrich(r.Context(), orders)
	if err != nil {
		writeDependencyError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"orders":    enriched,
		"total":     len(enriched),
		"requestBy": id.PersonID,
	})
}

// enrich 用 people/basic 补全订单上的人员姓名与部门（gRPC，走 extraPorts）。
//
// 姓名不在本组件存：连接组件不重复存别人的主数据，每次现取。
// 同一个人只查一次——一屏订单往往属于同几个人，逐条查会把 N 条订单
// 变成 N 次 gRPC 调用。
func (s *service) enrich(ctx context.Context, orders []Order) ([]enrichedOrder, error) {
	cache := map[string]person{}
	out := make([]enrichedOrder, 0, len(orders))

	for _, o := range orders {
		p, ok := cache[o.OwnerID]
		if !ok {
			fetched, err := s.people.GetPerson(ctx, o.OwnerID)
			switch {
			case errors.Is(err, errPersonNotFound):
				// 订单的所有者已经不在人员系统里了：订单本身还在，
				// 姓名留空而不是让整个列表失败
				fetched = person{ID: o.OwnerID}
			case err != nil:
				return nil, err
			}
			cache[o.OwnerID] = fetched
			p = fetched
		}

		out = append(out, enrichedOrder{
			Order:           o,
			OwnerName:       p.Name,
			OwnerDepartment: p.DepartmentName,
		})
	}
	return out, nil
}

// ============================================================
// POST /api/v1/orders/{id}/approve
// ============================================================

func (s *service) handleOrderAction(w http.ResponseWriter, r *http.Request) {
	orderID, action, ok := parseOrderPath(r.URL.Path)
	if !ok || action != "approve" {
		writeError(w, http.StatusNotFound, "接口不存在")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 POST")
		return
	}

	id, ok := s.authorize(w, r, permissionOrderApprove)
	if !ok {
		return
	}

	if err := s.orders.Approve(r.Context(), orderID); err != nil {
		if errors.Is(err, errOrderNotFound) {
			writeError(w, http.StatusNotFound, "订单不存在")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用，请稍后重试")
		return
	}

	// 弱依赖：发事件失败**不影响审批结果**。
	//
	// 若因为发不出事件就让审批失败（或回滚），弱依赖就成了事实上的强依赖——
	// 而 003 §4.3 对弱依赖的定义是"有就用、没有就降级"。
	// 但要如实告诉调用方事件到底发出去没有，不能假装发了
	published := s.publishApproved(r.Context(), id.PersonID, orderID)

	writeJSON(w, http.StatusOK, map[string]any{
		"orderId":        orderID,
		"status":         orderApproved,
		"approvedBy":     id.PersonID,
		"eventPublished": published,
	})
}

func (s *service) publishApproved(ctx context.Context, actor, orderID string) bool {
	if !s.bus.Enabled() {
		return false
	}
	return s.bus.Publish(ctx, event{
		Type:    eventOrderApproved,
		Actor:   actor,
		Subject: orderID,
		Time:    time.Now().UTC(),
	}) == nil
}

// ============================================================
// 认证 + 授权
// ============================================================

// authorize 走完"你是谁 → 你能不能"两步，并把状态码分清楚。
//
//	401  我不知道你是谁（没令牌 / 令牌无效）
//	403  我知道你是谁，但你不能做这个
//	503  我这边或下游有问题
//
// 401 与 403 混在一起会让调用方去重新登录，而登录一百次也不会有权限。
func (s *service) authorize(w http.ResponseWriter, r *http.Request, permission string) (identity, bool) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "缺少 Authorization: Bearer <token>")
		return identity{}, false
	}

	id, err := s.auth.Verify(r.Context(), token)
	if err != nil {
		if errors.Is(err, errTokenInvalid) {
			writeError(w, http.StatusUnauthorized, "令牌无效或已过期")
			return identity{}, false
		}
		writeDependencyError(w, err)
		return identity{}, false
	}

	allowed, err := s.authz.Check(r.Context(), id.PersonID, permission)
	if err != nil {
		writeDependencyError(w, err)
		return identity{}, false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "没有权限："+permission)
		return identity{}, false
	}
	return id, true
}

func bearerToken(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}

// parseOrderPath 解析 /api/v1/orders/{id}/{action}。
func parseOrderPath(path string) (orderID, action string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/v1/orders/")
	if rest == path {
		return "", "", false
	}
	orderID, action, found := strings.Cut(rest, "/")
	if !found || orderID == "" || action == "" {
		return "", "", false
	}
	return orderID, action, true
}

// ============================================================
// HTTP 辅助
// ============================================================

const maxBodyBytes = 64 << 10

// writeDependencyError 把下游故障统一报成 503。
//
// **不带底层原因**：把 "rpc error: code = Unavailable ... dial tcp" 透出去，
// 既帮不上调用方，又把内部拓扑告诉了外面。
func writeDependencyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTokenInvalid):
		writeError(w, http.StatusUnauthorized, "令牌无效或已过期")
	case errors.Is(err, errDependencyUnavailable), errors.Is(err, errPersonNotFound):
		writeError(w, http.StatusServiceUnavailable, "依赖的组件暂时不可用，请稍后重试")
	default:
		writeError(w, http.StatusServiceUnavailable, "依赖的组件暂时不可用，请稍后重试")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
