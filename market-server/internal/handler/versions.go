package handler

import (
	"io"
	"net/http"
	"time"

	"github.com/brickkit/brickkit/market-server/internal/model"
)

// publish 处理 POST /api/v1/components/{id}/versions。
func (a *api) publish(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	var req model.PublishRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}

	version, err := a.svc.Publish(r.Context(), id, p.componentID(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

// listVersions 处理 GET /api/v1/components/{id}/versions。
func (a *api) listVersions(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	versions, err := a.svc.ListVersions(r.Context(), id, p.componentID())
	if err != nil {
		writeError(w, err)
		return
	}

	// 版本列表不带 Manifest：列表页用不上，而它是整个响应里最大的字段
	out := make([]model.Version, 0, len(versions))
	for _, v := range versions {
		v.Manifest = nil
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// setVersionStatus 处理 PUT /api/v1/components/{id}/versions/{ver}。
func (a *api) setVersionStatus(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	var body struct {
		Status string `json:"status"`
		// Reason 落进审计条目的 detail，不影响判定。
		// 这里曾经解析出来就丢掉——注释写着"只用于审计"，而它哪儿都没去。
		Reason string `json:"reason,omitempty"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}

	err := a.svc.SetVersionStatus(
		r.Context(), id, p.componentID(), p["version"], body.Status, body.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"componentId": p.componentID(), "version": p["version"], "status": body.Status,
	})
}

// deleteVersion 处理 DELETE /api/v1/components/{id}/versions/{ver}。
//
// 是软删除：对外视同不存在，但版本号继续占位。
func (a *api) deleteVersion(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	if err := a.svc.DeleteVersion(r.Context(), id, p.componentID(), p["version"]); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"componentId": p.componentID(), "version": p["version"], "status": model.VersionDeleted,
	})
}

// manifest 处理 GET /api/v1/components/{id}/versions/{ver}/manifest。
//
// 这是 `brickkit add` 的入口端点，响应形状受 CLI 契约约束：
// data.manifest 是 component.yaml 本身，data.sourceType / data.gitUrl
// 供 `--repo` 判断开源还是闭源。
func (a *api) manifest(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	view, err := a.svc.GetManifest(r.Context(), id, p.componentID(), p["version"])
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// doc 处理 GET /api/v1/components/{id}/versions/{ver}/doc：这个版本的 BRICKKIT.md（?lang=<代码> 时是那种
// 语言的译本），原样的 Markdown，不包信封（它就是一个文件）。错误照常是 JSON 信封。
func (a *api) doc(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	doc, err := a.svc.GetDoc(r.Context(), id, p.componentID(), p["version"], r.URL.Query().Get("lang"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	// 正文是发布者写的，从市场的域名送出：不许浏览器把它猜成 HTML 之类
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, doc)
}

// listAudit 处理 GET /api/v1/audit。
func (a *api) listAudit(w http.ResponseWriter, r *http.Request, _ params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	entries, err := a.svc.ListAudit(r.Context(), id, auditQuery(r))
	if err != nil {
		writeError(w, err)
		return
	}
	if entries == nil {
		entries = []model.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// orEmpty 保证 JSON 里出现 [] 而不是 null：
// 弱类型客户端遍历 null 会直接崩。
func orEmpty(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
