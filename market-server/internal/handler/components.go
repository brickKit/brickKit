package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
)

// searchComponents 处理 GET /api/v1/components（007 §4.2、18.15）。
func (a *api) searchComponents(w http.ResponseWriter, r *http.Request, _ params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	result, err := a.svc.SearchComponents(r.Context(), id, searchQuery(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// searchQuery 解析搜索条件。
//
// 分页参数写错时按默认值处理，不报错：搜索是只读操作，
// 为一个 ?page=abc 把整页搜索打回去，对使用者没有任何帮助。
func searchQuery(r *http.Request) repo.ComponentQuery {
	query := r.URL.Query()

	q := repo.ComponentQuery{
		Keyword: strings.TrimSpace(query.Get("keyword")),
		Page:    positiveInt(query.Get("page")),
		// 列表接口只返回未下架的 public（及有权访问的 private）组件，
		// 具体过滤由服务层下推，这里不设 Visibilities。
		PageSize: positiveInt(query.Get("pageSize")),
	}
	for _, raw := range query["tags"] {
		for _, tag := range strings.Split(raw, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				q.Tags = append(q.Tags, tag)
			}
		}
	}
	return q
}

// positiveInt 解析正整数，解析不出来或不是正数时返回 0（表示用默认值）。
func positiveInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

// componentView 是组件详情的响应体（007 §4.3）。
//
// 这里把服务层的 ComponentDetail 摊平成一层：007 的详情页示例是平铺的，
// 前端与文档都按那个形状写。
type componentView struct {
	ComponentID   string   `json:"componentId"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Vendor        string   `json:"vendor,omitempty"`
	Tags          []string `json:"tags"`
	Visibility    string   `json:"visibility"`
	SourceType    string   `json:"sourceType"`
	GitURL        string   `json:"gitUrl,omitempty"`
	Status        string   `json:"status"`
	LatestVersion string   `json:"latestVersion,omitempty"`
	Versions      []string `json:"versions"`
	Downloads     int64    `json:"downloads"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

// componentDetail 处理 GET /api/v1/components/{id}（007 §4.3、18.16）。
func (a *api) componentDetail(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	detail, err := a.svc.GetComponent(r.Context(), id, p.componentID())
	if err != nil {
		writeError(w, err)
		return
	}

	c := detail.Component
	view := componentView{
		ComponentID:   c.ComponentID,
		Name:          c.Name,
		Description:   c.Description,
		Vendor:        c.Vendor,
		Tags:          orEmpty(c.Tags),
		Visibility:    c.Visibility,
		SourceType:    c.SourceType,
		GitURL:        c.GitURL,
		Status:        c.Status,
		LatestVersion: detail.LatestVersion,
		Versions:      orEmpty(detail.Versions),
		Downloads:     c.Downloads,
		CreatedAt:     rfc3339(c.CreatedAt),
		UpdatedAt:     rfc3339(c.UpdatedAt),
	}
	writeJSON(w, http.StatusOK, view)
}

// setVisibility 处理 PUT /api/v1/components/{id}/visibility（007 §9.4、18.18）。
func (a *api) setVisibility(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	var body struct {
		Visibility string `json:"visibility"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}

	if err := a.svc.SetVisibility(r.Context(), id, p.componentID(), body.Visibility); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"componentId": p.componentID(), "visibility": body.Visibility,
	})
}

// listAccess 处理 GET /api/v1/components/{id}/access（007 §9.4）。
func (a *api) listAccess(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	policies, err := a.svc.ListAccessPolicies(r.Context(), id, p.componentID())
	if err != nil {
		writeError(w, err)
		return
	}
	if policies == nil {
		policies = []model.AccessPolicy{}
	}
	writeJSON(w, http.StatusOK, policies)
}

// setAccess 处理 PUT /api/v1/components/{id}/access（007 §9.4）。
//
// 语义是整体覆盖而非增量：增量语义下"撤销一条授权"需要额外的接口，
// 而覆盖语义天然支持撤销，也不会出现两次调用顺序不同结果不同的问题。
func (a *api) setAccess(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	var body struct {
		Policies []model.AccessPolicy `json:"policies"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}

	if err := a.svc.SetAccessPolicies(r.Context(), id, p.componentID(), body.Policies); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"componentId": p.componentID(), "policies": len(body.Policies),
	})
}
