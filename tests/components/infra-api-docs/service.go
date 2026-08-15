package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// service 聚合各组件的 API 文档，并用 Swagger UI 展示。
//
// 它是平台里唯一一个**全部依赖都是弱依赖**的组件：七个目标组件装了几个就
// 展示几个，一个都没装也照样起得来。这不是容错做得好，而是这个组件的
// 本来面目——文档入口不该因为某个业务组件没装就打不开。
type service struct {
	discoverer *Discoverer
	cfg        config
	logger     *slog.Logger
	webRoot    string

	mu       sync.RWMutex
	cached   []Source
	cachedAt time.Time
	now      func() time.Time
}

func newService(d *Discoverer, cfg config, logger *slog.Logger, webRoot string) *service {
	return &service{discoverer: d, cfg: cfg, logger: logger, webRoot: webRoot, now: time.Now}
}

// sources 返回探测结果，带一个短缓存。
//
// 每次刷新页面都去探七个组件的话，一个卡住的上游会让页面很慢；
// 而组件的 API 文档几乎不会在几十秒内变。
func (s *service) sources(ctx context.Context) []Source {
	s.mu.RLock()
	if s.cached != nil && s.now().Sub(s.cachedAt) < cacheTTL {
		defer s.mu.RUnlock()
		return s.cached
	}
	s.mu.RUnlock()

	found := s.discoverer.Discover(ctx, s.cfg.Targets)

	s.mu.Lock()
	s.cached, s.cachedAt = found, s.now()
	s.mu.Unlock()
	return found
}

func (s *service) routes() http.Handler {
	mux := http.NewServeMux()

	// 健康检查只回答"本进程还活着吗"（002 §9.4）。
	// **绝不去探那七个组件**：它们全是弱依赖，全挂了这个页面也该打得开——
	// 而且那时候正是最需要看文档的时候
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/sources", s.handleSources)
	mux.HandleFunc("/api/v1/openapi/", s.handleOpenAPI)

	// Swagger UI 与首页。放在最后：ServeMux 的 "/" 会兜住所有没匹配上的路径
	mux.Handle("/", http.FileServer(http.Dir(s.webRoot)))
	return mux
}

// handleSources 返回聚合状态：谁有文档、谁没装、谁连不上。
//
// 这个端点本身就是排障工具：文档页面空着的时候，先看它就知道是
// "组件没装"还是"组件挂了"还是"组件没提供文档"——三种情况的处理完全不同。
func (s *service) handleSources(w http.ResponseWriter, r *http.Request) {
	found := s.sources(r.Context())

	type view struct {
		Source
		// SpecURL 指向本组件代理出去的那份 OpenAPI，供 Swagger UI 直接加载。
		SpecURL string `json:"specUrl,omitempty"`
	}

	out := make([]view, 0, len(found))
	for _, source := range found {
		item := view{Source: source}
		if len(source.OpenAPI) > 0 {
			item.SpecURL = "/api/v1/openapi/" + source.ComponentID
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out, "total": len(out)})
}

// handleOpenAPI 把抓到的 OpenAPI 原样代理出去。
//
// 为什么不让浏览器直接去连组件：那些组件默认不暴露端口（008 §5.2），
// 浏览器根本连不上；就算连得上也会撞跨域。由本组件代理是唯一走得通的路。
func (s *service) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	componentID := strings.TrimPrefix(r.URL.Path, "/api/v1/openapi/")
	if componentID == "" {
		writeError(w, http.StatusBadRequest, "缺少组件 ID")
		return
	}

	for _, source := range s.sources(r.Context()) {
		if source.ComponentID != componentID {
			continue
		}
		if len(source.OpenAPI) == 0 {
			writeError(w, http.StatusNotFound, "该组件没有可展示的 OpenAPI 文档")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(source.OpenAPI)
		return
	}
	writeError(w, http.StatusNotFound, "未知的组件："+componentID)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
