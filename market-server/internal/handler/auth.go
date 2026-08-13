package handler

import (
	"net/http"

	"github.com/brickkit/market-server/internal/service"
)

// register 处理 POST /api/v1/auth/register（007 §9.5、18.19）。
func (a *api) register(w http.ResponseWriter, r *http.Request, _ params) {
	var req service.RegisterRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}

	user, err := a.svc.Register(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	// model.User 的 PasswordHash 标了 json:"-"，密码哈希不会出现在响应里
	writeJSON(w, http.StatusCreated, user)
}

// login 处理 POST /api/v1/auth/login（007 §9.6、18.20）。
//
// 返回的 token 与 expiresAt 就是 CLI 写进 .brickkit/credentials 的内容（004 §5.3）。
func (a *api) login(w http.ResponseWriter, r *http.Request, _ params) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}

	token, err := a.svc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, token)
}

// logout 处理 POST /api/v1/auth/logout。
//
// 只注销当次请求携带的令牌，不需要请求体；重复注销是幂等的。
func (a *api) logout(w http.ResponseWriter, r *http.Request, _ params) {
	if err := a.svc.Logout(r.Context(), bearerToken(r)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"loggedOut": true})
}
