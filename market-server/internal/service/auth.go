package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
)

// MinPasswordLength 是最短密码长度。
const MinPasswordLength = 8

// Identity 是一次请求的调用者身份。
type Identity struct {
	UserID   string
	Username string
	OrgID    string
	IsAdmin  bool
	// Anonymous 为 true 表示没带令牌：只能访问 public 组件（007 §5.5）。
	Anonymous bool
}

// Anonymous 返回匿名身份。
func Anonymous() *Identity { return &Identity{Anonymous: true} }

// RegisterRequest 是注册请求（007 §9.5）。
type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
	OrgID    string `json:"orgId,omitempty"`
}

// Register 注册用户。
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*model.User, error) {
	if err := validateRegister(req); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.bcryptCost)
	if err != nil {
		return nil, model.Errorf(model.CodeInternal, "密码处理失败")
	}

	user := &model.User{
		UserID:       "user-" + s.newID(),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: string(hash),
		OrgID:        req.OrgID,
		CreatedAt:    s.now(),
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		if errors.Is(err, repo.ErrConflict) {
			return nil, model.Errorf(model.CodeConflict, "用户名已被占用："+req.Username)
		}
		return nil, internalError(err)
	}

	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionUserRegistered, Operator: user.Username, Result: model.ResultSuccess,
	})

	// 对外绝不返回密码哈希
	safe := *user
	safe.PasswordHash = ""
	return &safe, nil
}

func validateRegister(req RegisterRequest) error {
	switch {
	case strings.TrimSpace(req.Username) == "":
		return model.Errorf(model.CodeInvalidRequest, "用户名不能为空")
	case strings.ContainsAny(req.Username, " \t\n"):
		return model.Errorf(model.CodeInvalidRequest, "用户名不能包含空白字符")
	case req.Password == "":
		return model.Errorf(model.CodeInvalidRequest, "密码不能为空")
	case len(req.Password) < MinPasswordLength:
		return model.Errorf(model.CodeInvalidRequest, "密码至少需要 8 个字符")
	}
	return nil
}

// Login 校验用户名密码并签发访问令牌（007 §9.6）。
func (s *Service) Login(ctx context.Context, username, password string) (*model.Token, error) {
	user, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			// 用户不存在与密码错误返回同样的错误：避免枚举用户名
			return nil, invalidCredentials()
		}
		return nil, internalError(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, invalidCredentials()
	}

	token := &model.Token{
		Token:     s.newID() + s.newID(),
		UserID:    user.UserID,
		Username:  user.Username,
		ExpiresAt: s.now().Add(s.tokenTTL),
		CreatedAt: s.now(),
	}
	if err := s.repo.CreateToken(ctx, token); err != nil {
		return nil, internalError(err)
	}

	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionUserLogin, Operator: user.Username, Result: model.ResultSuccess,
	})
	return token, nil
}

func invalidCredentials() error {
	return model.Errorf(model.CodeUnauthorized, "用户名或密码错误")
}

// Logout 注销令牌。重复注销是幂等的。
func (s *Service) Logout(ctx context.Context, token string) error {
	if err := s.repo.DeleteToken(ctx, token); err != nil {
		return internalError(err)
	}
	return nil
}

// Authenticate 把 Bearer Token 解析成调用者身份。
//
// 令牌为空表示匿名——这不是错误：public 组件的查询本来就不需要认证（007 §9.6）。
func (s *Service) Authenticate(ctx context.Context, token string) (*Identity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Anonymous(), nil
	}

	stored, err := s.repo.GetToken(ctx, token)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, model.Errorf(model.CodeUnauthorized, "令牌无效，请重新登录")
		}
		return nil, internalError(err)
	}
	if s.now().After(stored.ExpiresAt) {
		return nil, model.Errorf(model.CodeUnauthorized, "令牌已过期，请重新登录")
	}

	user, err := s.repo.GetUserByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, model.Errorf(model.CodeUnauthorized, "令牌对应的用户已不存在")
		}
		return nil, internalError(err)
	}

	return &Identity{
		UserID:   user.UserID,
		Username: user.Username,
		OrgID:    user.OrgID,
		IsAdmin:  user.IsAdmin,
	}, nil
}

// ============================================================
// 鉴权
// ============================================================

// requireAuth 要求调用者已登录。
func requireAuth(id *Identity, action string) error {
	if id == nil || id.Anonymous {
		return model.Errorf(model.CodeUnauthorized, action+"需要先登录市场")
	}
	return nil
}

// requireOwner 要求调用者是组件所有者或市场管理员。
func requireOwner(id *Identity, c *model.Component, action string) error {
	if err := requireAuth(id, action); err != nil {
		return err
	}
	if id.IsAdmin || id.UserID == c.OwnerID {
		return nil
	}
	return model.Errorf(model.CodeForbidden, action+"需要是组件所有者："+c.ComponentID)
}

// requireAdmin 要求调用者是市场管理员（007 §6.3：blocked 只有管理员能标记）。
func requireAdmin(id *Identity, action string) error {
	if err := requireAuth(id, action); err != nil {
		return err
	}
	if !id.IsAdmin {
		return model.Errorf(model.CodeForbidden, action+"需要市场管理员权限")
	}
	return nil
}

// canRead 判断调用者能否看到该组件（007 §5）。
func (s *Service) canRead(ctx context.Context, id *Identity, c *model.Component) (bool, error) {
	if c.Visibility != model.VisibilityPrivate {
		return true, nil
	}
	if id == nil || id.Anonymous {
		return false, nil
	}
	if id.IsAdmin || id.UserID == c.OwnerID {
		return true, nil
	}

	policies, err := s.repo.ListAccessPolicies(ctx, c.ComponentID)
	if err != nil {
		return false, internalError(err)
	}
	for _, p := range policies {
		switch p.TargetType {
		case model.TargetUser:
			if p.TargetID == id.UserID {
				return true, nil
			}
		case model.TargetOrganization:
			if id.OrgID != "" && p.TargetID == id.OrgID {
				return true, nil
			}
		}
	}
	return false, nil
}

// requireRead 在无权访问时返回 403。
func (s *Service) requireRead(ctx context.Context, id *Identity, c *model.Component) error {
	ok, err := s.canRead(ctx, id, c)
	if err != nil {
		return err
	}
	if !ok {
		return model.Errorf(model.CodeForbidden, "无权访问该组件："+c.ComponentID).
			WithDetail("componentId", c.ComponentID).
			WithDetail("visibility", c.Visibility)
	}
	return nil
}

// visibleComponentIDs 返回调用者被显式授权的 private 组件 ID，用于搜索时放行。
func (s *Service) visibleComponentIDs(ctx context.Context, id *Identity) ([]string, error) {
	if id == nil || id.Anonymous {
		return nil, nil
	}

	// 私有组件不多，直接遍历一遍即可；真要做大再上倒排索引
	all, err := s.repo.ListComponents(ctx, repo.ComponentQuery{
		Visibilities: []string{model.VisibilityPrivate},
		PageSize:     maxScanComponents,
	})
	if err != nil {
		return nil, internalError(err)
	}

	var out []string
	for i := range all {
		ok, err := s.canRead(ctx, id, &all[i])
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, all[i].ComponentID)
		}
	}
	return out, nil
}

// randomID 生成随机十六进制串，用作用户 ID 与令牌。
func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败属于系统级异常，退化到时间戳也比继续跑要好
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}
