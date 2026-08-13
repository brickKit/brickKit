// Package handler 是市场的 HTTP 层。
//
// 它只做协议转换：解析路径与请求体、把 Bearer Token 换成调用者身份、
// 把服务层的结果与错误装进统一信封。**所有"谁能做什么"的判断都在服务层**，
// 这里一条权限规则都不写——否则同一条规则会有两份实现，迟早分叉。
//
// 路径与响应形状来自 007 §4、§9，以及 CLI 侧已经实现的契约（D47/D48）。
package handler

import (
	"net/http"
	"time"

	"github.com/brickkit/market-server/internal/middleware"
	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/service"
)

// Options 是 HTTP 层的可选配置。
type Options struct {
	// Version 是构建版本，健康检查会回显它，便于确认部署的是哪个镜像。
	Version string
	// Now 用于测试注入时间。
	Now func() time.Time
	// Logf 记录访问日志与 panic，默认走标准库 log。
	Logf func(format string, args ...any)
}

// api 持有 HTTP 层需要的依赖。
type api struct {
	svc     *service.Service
	version string
	now     func() time.Time
}

// New 组装市场的 HTTP 路由。
func New(svc *service.Service, opts Options) http.Handler {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	rt := &router{api: &api{svc: svc, version: opts.Version, now: opts.Now}}

	rt.handle(http.MethodGet, "/api/v1/health", (*api).health)

	rt.handle(http.MethodPost, "/api/v1/auth/register", (*api).register)
	rt.handle(http.MethodPost, "/api/v1/auth/login", (*api).login)
	rt.handle(http.MethodPost, "/api/v1/auth/logout", (*api).logout)

	rt.handle(http.MethodGet, "/api/v1/audit", (*api).listAudit)

	rt.handle(http.MethodGet, "/api/v1/components", (*api).searchComponents)
	rt.handle(http.MethodGet, "/api/v1/components/:scope/:name", (*api).componentDetail)
	rt.handle(http.MethodPut, "/api/v1/components/:scope/:name/visibility", (*api).setVisibility)
	rt.handle(http.MethodGet, "/api/v1/components/:scope/:name/access", (*api).listAccess)
	rt.handle(http.MethodPut, "/api/v1/components/:scope/:name/access", (*api).setAccess)

	rt.handle(http.MethodGet, "/api/v1/components/:scope/:name/versions", (*api).listVersions)
	rt.handle(http.MethodPost, "/api/v1/components/:scope/:name/versions", (*api).publish)
	rt.handle(http.MethodPut, "/api/v1/components/:scope/:name/versions/:version", (*api).setVersionStatus)
	rt.handle(http.MethodDelete, "/api/v1/components/:scope/:name/versions/:version", (*api).deleteVersion)
	rt.handle(http.MethodGet, "/api/v1/components/:scope/:name/versions/:version/manifest", (*api).manifest)

	rt.handle(http.MethodGet, "/api/v1/components/:scope/:name/versions/:version/artifacts",
		(*api).listArtifacts)
	rt.handle(http.MethodPost, "/api/v1/components/:scope/:name/versions/:version/artifacts/:artifactId/upload",
		(*api).uploadArtifact)
	rt.handle(http.MethodGet, "/api/v1/components/:scope/:name/versions/:version/artifacts/:artifactId/download",
		(*api).downloadArtifact)

	return middleware.Recover(opts.Logf)(middleware.AccessLog(opts.Logf)(rt))
}

// identity 把 Authorization 头换成调用者身份。
//
// 没带 Token 时返回匿名身份而不是错误：public 组件的查询本来就不需要登录
// （007 §5.5），"要不要认证"由服务层按被访问的资源决定。
func (a *api) identity(r *http.Request) (*service.Identity, error) {
	return a.svc.Authenticate(r.Context(), bearerToken(r))
}

// bearerToken 取出 Authorization: Bearer <token> 里的令牌。
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	value := r.Header.Get("Authorization")
	if len(value) > len(prefix) && equalFold(value[:len(prefix)], prefix) {
		return value[len(prefix):]
	}
	return ""
}

// equalFold 只比较 ASCII 大小写，够用且不引入依赖。
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// health 是健康检查（18.23、运维指南 §4 的 compose healthcheck 探针）。
//
// 它必须匿名可访问，也不查库：探针要回答的是"进程还活着吗"，
// 让它依赖数据库会在数据库抖动时把还能提供只读服务的实例一起判死。
func (a *api) health(w http.ResponseWriter, _ *http.Request, _ params) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": a.version,
		"time":    a.now().UTC().Format(time.RFC3339),
	})
}

// requireIdentity 解析身份，失败时直接写出错误响应。
func (a *api) requireIdentity(w http.ResponseWriter, r *http.Request) (*service.Identity, bool) {
	id, err := a.identity(r)
	if err != nil {
		writeError(w, err)
		return nil, false
	}
	return id, true
}

// missingQuery 构造"缺少查询参数"的错误。
func missingQuery(name, why string) error {
	return model.Errorf(model.CodeInvalidRequest, "缺少查询参数 "+name+"："+why)
}
