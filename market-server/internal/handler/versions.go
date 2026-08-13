package handler

import (
	"net/http"
	"time"

	"github.com/brickkit/market-server/internal/model"
)

// publish 处理 POST /api/v1/components/{id}/versions（007 §3.7、18.1）。
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

// listVersions 处理 GET /api/v1/components/{id}/versions（007 §4.4、18.2）。
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

// setVersionStatus 处理 PUT /api/v1/components/{id}/versions/{ver}（007 §6.3、18.17）。
func (a *api) setVersionStatus(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	var body struct {
		Status string `json:"status"`
		// Reason 只用于审计，不影响判定（运维指南 §6.5 的 blocked 示例会带上它）。
		Reason string `json:"reason,omitempty"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}

	err := a.svc.SetVersionStatus(r.Context(), id, p.componentID(), p["version"], body.Status)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"componentId": p.componentID(), "version": p["version"], "status": body.Status,
	})
}

// deleteVersion 处理 DELETE /api/v1/components/{id}/versions/{ver}（18.24）。
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

// manifest 处理 GET /api/v1/components/{id}/versions/{ver}/manifest（007 §4.5、18.3）。
//
// 这是 `brickkit add` 的入口端点，响应形状受 CLI 契约（D47）约束：
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

// listAudit 处理 GET /api/v1/audit（007 §16、18.13）。
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
