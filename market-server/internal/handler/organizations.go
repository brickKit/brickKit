package handler

// 本文件实现组织管理的三个端点（007 §9.5）。

import (
	"net/http"

	"github.com/brickkit/market-server/internal/service"
)

// listOrganizations 处理 GET /api/v1/organizations。
//
// 普通用户只看得到自己所属的组织，管理员看得到全部（见 service 层）。
func (a *api) listOrganizations(w http.ResponseWriter, r *http.Request, _ params) {
	id, err := a.identity(r)
	if err != nil {
		writeError(w, err)
		return
	}

	orgs, err := a.svc.ListOrganizations(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"organizations": orgs, "total": len(orgs)})
}

// createOrganization 处理 POST /api/v1/organizations。
func (a *api) createOrganization(w http.ResponseWriter, r *http.Request, _ params) {
	id, err := a.identity(r)
	if err != nil {
		writeError(w, err)
		return
	}

	var req service.CreateOrganizationRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}

	org, err := a.svc.CreateOrganization(r.Context(), id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

// addOrganizationMember 处理 POST /api/v1/organizations/{id}/members。
//
// 这是组织成员关系的**唯一**写入口，而成员关系就是 private 组件的授权本身
// （007 §5.3）——因此它必须有门：只有组织所有者与市场管理员能进。
func (a *api) addOrganizationMember(w http.ResponseWriter, r *http.Request, p params) {
	id, err := a.identity(r)
	if err != nil {
		writeError(w, err)
		return
	}

	var req service.AddMemberRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}

	if err := a.svc.AddOrganizationMember(r.Context(), id, p["orgId"], req); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"orgId": p["orgId"], "username": req.Username, "status": "added",
	})
}
