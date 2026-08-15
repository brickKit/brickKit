package service

// 本文件实现组织管理（007 §9.5）。
//
// 组织存在的意义只有一个：把 private 组件按组织授权出去（007 §5.3 的
// allowedOrganizations）。因此**成员关系就是授权本身**——它必须只有一个入口，
// 而且那个入口要有门。在这三个方法存在之前，成员关系只能靠注册时自报 orgId，
// 那等于任何人写上别人的组织 ID 就能读走该组织的全部 private 组件。

import (
	"context"
	"errors"
	"strings"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
)

// CreateOrganizationRequest 是创建组织的请求。
type CreateOrganizationRequest struct {
	Name string `json:"name"`
}

// AddMemberRequest 是添加成员的请求。
type AddMemberRequest struct {
	Username string `json:"username"`
}

// CreateOrganization 创建组织，创建者成为所有者并自动入组。
func (s *Service) CreateOrganization(
	ctx context.Context, id *Identity, req CreateOrganizationRequest,
) (*model.Organization, error) {
	if id == nil || id.Anonymous {
		return nil, model.Errorf(model.CodeUnauthorized, "创建组织需要登录")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, model.Errorf(model.CodeInvalidRequest, "组织名称不能为空")
	}
	// 一个人只能属于一个组织（users.org_id 是单值）。已经在别的组织里时明确报错，
	// 不能悄悄把他挪走——那会让他原组织的 private 组件突然读不到了
	if id.OrgID != "" {
		return nil, model.Errorf(model.CodeConflict,
			"你已经属于组织 "+id.OrgID+"，一个用户只能属于一个组织")
	}

	org := &model.Organization{
		OrgID:     "org-" + s.newID(),
		Name:      name,
		OwnerID:   id.UserID,
		CreatedAt: s.now(),
	}
	if err := s.repo.CreateOrganization(ctx, org); err != nil {
		return nil, internalError(err)
	}
	// 创建者自动入组：否则他建完组织自己还进不去，得再给自己加一次
	if err := s.repo.SetUserOrg(ctx, id.UserID, org.OrgID); err != nil {
		return nil, internalError(err)
	}

	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionOrganizationCreated, Operator: id.Username, Result: model.ResultSuccess,
	})
	return org, nil
}

// ListOrganizations 列出调用者能看到的组织。
//
// 普通用户只看得到自己所属的那个：组织名与成员关系本身就是信息，
// 一个内部平台不该让任何人把所有客户的组织列一遍。
func (s *Service) ListOrganizations(ctx context.Context, id *Identity) ([]model.Organization, error) {
	if id == nil || id.Anonymous {
		return nil, model.Errorf(model.CodeUnauthorized, "查询组织需要登录")
	}

	all, err := s.repo.ListOrganizations(ctx)
	if err != nil {
		return nil, internalError(err)
	}
	if id.IsAdmin {
		return all, nil
	}

	out := []model.Organization{}
	for _, org := range all {
		if org.OrgID == id.OrgID || org.OwnerID == id.UserID {
			out = append(out, org)
		}
	}
	return out, nil
}

// AddOrganizationMember 把一个用户加进组织。
//
// 只有组织所有者与市场管理员能加人。放开这道门等于回到"谁都能自称是成员"。
func (s *Service) AddOrganizationMember(
	ctx context.Context, id *Identity, orgID string, req AddMemberRequest,
) error {
	if id == nil || id.Anonymous {
		return model.Errorf(model.CodeUnauthorized, "添加成员需要登录")
	}

	org, err := s.repo.GetOrganization(ctx, orgID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return model.Errorf(model.CodeNotFound, "组织不存在："+orgID)
		}
		return internalError(err)
	}
	if !id.IsAdmin && org.OwnerID != id.UserID {
		return model.Errorf(model.CodeForbidden, "只有组织所有者或市场管理员可以添加成员")
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		return model.Errorf(model.CodeInvalidRequest, "用户名不能为空")
	}
	user, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return model.Errorf(model.CodeNotFound, "用户不存在："+username)
		}
		return internalError(err)
	}

	switch user.OrgID {
	case org.OrgID:
		// 已经在这个组织里了：幂等，运维脚本可以放心重跑
		return nil
	case "":
	default:
		return model.Errorf(model.CodeConflict,
			username+" 已属于组织 "+user.OrgID+"，请先让其退出原组织")
	}

	if err := s.repo.SetUserOrg(ctx, user.UserID, org.OrgID); err != nil {
		return internalError(err)
	}

	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionMemberAdded, Operator: id.Username, Result: model.ResultSuccess,
	})
	return nil
}
