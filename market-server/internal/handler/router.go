package handler

import (
	"net/http"
	"strings"

	"github.com/brickkit/market-server/internal/model"
)

// params 是路径参数。
type params map[string]string

// componentID 拼出组件 ID。组件 ID 是两段式 scope/name（002 §10.3），
// 路径里的 `/` 是它自身的一部分，不做转义（007 §4.5）。
func (p params) componentID() string { return p["scope"] + "/" + p["name"] }

// route 是一条路由规则。segments 里以 `:` 开头的是参数段。
type route struct {
	method   string
	segments []string
	handle   func(*api, http.ResponseWriter, *http.Request, params)
}

// accepts 判断该路由是否接受这个方法。
//
// HEAD 走 GET 的处理器：HEAD 就是"只要响应头的 GET"，
// 而且很多探针（compose healthcheck 的 `wget --spider`、负载均衡器）默认发 HEAD。
// 响应体由 net/http 自动丢弃，处理器不用关心。
func (r route) accepts(method string) bool {
	return method == r.method || (method == http.MethodHead && r.method == http.MethodGet)
}

// router 是一个极简路由器。
//
// 不用 http.ServeMux 的原因：组件 ID 自带一个 `/`，而且未命中时
// ServeMux 会写出纯文本的 404/405——CLI 只会解析 JSON 信封，
// 拿到纯文本会报"响应无法解析"，把真正的原因盖掉。
type router struct {
	api    *api
	routes []route
}

func (rt *router) handle(method, pattern string, h func(*api, http.ResponseWriter, *http.Request, params)) {
	rt.routes = append(rt.routes, route{
		method:   method,
		segments: strings.Split(strings.Trim(pattern, "/"), "/"),
		handle:   h,
	})
}

func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	var allowed []string
	for _, route := range rt.routes {
		args, ok := match(route.segments, segments)
		if !ok {
			continue
		}
		if !route.accepts(r.Method) {
			allowed = append(allowed, route.method)
			continue
		}
		route.handle(rt.api, w, r, args)
		return
	}

	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		err := model.Errorf(model.CodeInvalidRequest, "该地址不支持 "+r.Method+" 方法").
			WithDetail("allow", strings.Join(allowed, ", "))
		err.Status = http.StatusMethodNotAllowed
		writeError(w, err)
		return
	}
	writeError(w, model.Errorf(model.CodeNotFound, "接口不存在："+r.URL.Path))
}

// match 把实际路径段与路由模式比对，返回路径参数。
func match(pattern, actual []string) (params, bool) {
	if len(pattern) != len(actual) {
		return nil, false
	}

	args := params{}
	for i, seg := range pattern {
		if strings.HasPrefix(seg, ":") {
			if actual[i] == "" {
				return nil, false
			}
			args[seg[1:]] = actual[i]
			continue
		}
		if seg != actual[i] {
			return nil, false
		}
	}
	return args, true
}
