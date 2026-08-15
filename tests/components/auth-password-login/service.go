package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// service 是登录组件的业务逻辑与 HTTP 出口。
//
// 职责边界：**本组件只管"怎么证明你是你"**。姓名、部门等身份信息在
// people/basic 里，登录时现取——那样主体的存废由人员系统说了算，
// 也不会出现两份会漂移的身份数据。
type service struct {
	store  Store
	people peopleClient
	issuer *tokenIssuer
	cfg    config
}

func newService(store Store, people peopleClient, issuer *tokenIssuer, cfg config) *service {
	return &service{store: store, people: people, issuer: issuer, cfg: cfg}
}

// routes 组装 HTTP 路由。
func (s *service) routes() http.Handler {
	mux := http.NewServeMux()

	// 健康检查只回答"本进程还活着吗"（002 §9.4）。
	// 它**不查库、不调 people/basic**：否则依赖一抖，编排系统就会把这个本身
	// 完全正常的容器杀掉重启，故障从一个组件扩散成一片。
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/login", s.handleLogin)
	mux.HandleFunc("/api/v1/verify", s.handleVerify)
	return mux
}

// ============================================================
// POST /api/v1/login
// ============================================================

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	PersonID  string    `json:"personId"`
	Username  string    `json:"username"`
}

func (s *service) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 POST")
		return
	}

	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	// 请求本身不合法是 400，不是 401：401 是"你是谁我不认"，
	// 400 是"你根本没说清楚要什么"。混在一起会让调用方以为是凭据问题
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username 与 password 都不能为空")
		return
	}

	sub, err := s.authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}

	token, err := s.issuer.issue(sub)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "签发令牌失败")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:     token,
		ExpiresAt: s.issuer.expiresAt(),
		PersonID:  sub.PersonID,
		Username:  sub.Username,
	})
}

// errInvalidCredentials 是对外统一的"认不了你"。
//
// 用户名不存在与口令错误必须映射到**同一个**错误：任何差别都能被拿来把一份
// 用户名字典筛成"这些账号真实存在"，而那是撞库的第一步。
var errInvalidCredentials = errors.New("用户名或密码错误")

// authenticate 完成一次认证：查凭据 → 验口令 → 向 people/basic 确认主体仍在。
func (s *service) authenticate(ctx context.Context, username, password string) (subject, error) {
	cred, err := s.store.GetByUsername(ctx, username)
	switch {
	case errors.Is(err, ErrCredentialNotFound):
		// 即使用户不存在也要走一遍哈希校验，让两条路径的耗时接近；
		// 否则"秒回"与"算了 600000 轮"的时间差本身就泄露了用户是否存在
		verifyPassword(dummyHash, password)
		return subject{}, errInvalidCredentials
	case err != nil:
		return subject{}, errStorageUnavailable
	}

	if !verifyPassword(cred.PasswordHash, password) {
		return subject{}, errInvalidCredentials
	}

	// 强依赖：确认这个人还在人员系统里。
	// 员工离职后从 people/basic 删掉，凭据表哪怕还留着也不能放进来
	p, err := s.people.GetPerson(ctx, cred.PersonID)
	switch {
	case errors.Is(err, errPersonNotFound):
		return subject{}, errInvalidCredentials
	case err != nil:
		return subject{}, errDependencyUnavailable
	}

	return subject{
		PersonID:     p.ID,
		Username:     cred.Username,
		DepartmentID: p.DepartmentID,
	}, nil
}

// dummyHash 是一份格式合法、但谁也对不上的哈希，用于抹平时间差（见 authenticate）。
var dummyHash = mustHash("this-password-matches-nobody")

func mustHash(password string) string {
	h, err := hashPassword(password)
	if err != nil {
		panic(err) // 只在进程启动时执行一次，失败说明 crypto/rand 坏了
	}
	return h
}

// writeAuthError 把内部错误映射成对外的状态码。
//
// 这个映射是本组件最要紧的一处判断：
//
//	401  你是谁我不认（用户不存在 / 口令错 / 人已不在人员系统）
//	503  我这边有问题（库连不上 / people/basic 挂了）
//
// 把 503 报成 401，使用者会在自己的密码上白折腾半天；
// 把 401 报成 503，又会让人去查根本没坏的依赖。
func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalidCredentials):
		writeError(w, http.StatusUnauthorized, errInvalidCredentials.Error())
	case errors.Is(err, errDependencyUnavailable):
		writeError(w, http.StatusServiceUnavailable, "依赖的人员服务暂时不可用，请稍后重试")
	case errors.Is(err, errStorageUnavailable):
		writeError(w, http.StatusServiceUnavailable, "服务暂时不可用，请稍后重试")
	default:
		writeError(w, http.StatusInternalServerError, "服务内部错误")
	}
}

// ============================================================
// POST /api/v1/verify
// ============================================================

// handleVerify 校验令牌并返回其中的身份。
//
// 计划里 Step 23 只要求"签发 JWT"，但令牌是用 HS256 签的——密钥只有本组件有，
// 下游（erp/backend、authorization/rbac）拿到令牌根本验不了。没有这个端点，
// 签出去的令牌对他们就是一串不可用的字符串。
func (s *service) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 POST")
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}

	c, err := s.issuer.parse(req.Token)
	if err != nil {
		// 不回显具体原因（过期？签名错？缺 sub？）：那等于告诉伪造者
		// 他离成功还差哪一步
		writeError(w, http.StatusUnauthorized, "令牌无效或已过期")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"personId":     c.Subject,
		"username":     c.Username,
		"departmentId": c.DepartmentID,
		"expiresAt":    c.ExpiresAt.Time,
	})
}

// ============================================================
// HTTP 辅助
// ============================================================

// maxBodyBytes 限制请求体大小：登录请求就几十字节，
// 不设上限的话一个超大 body 就能把内存吃干净。
const maxBodyBytes = 64 << 10

func decodeJSON(r *http.Request, out any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	return decoder.Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError 输出统一的错误结构。
//
// 只有一句面向使用者的话，**不带底层原因**：把 "pq: connection refused"
// 透出去既帮不上调用方，又把内部拓扑告诉了外面（004 的错误信息约定）。
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
