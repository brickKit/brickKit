package service

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
)

// EnsureAdmin 保证市场里存在一个管理员账号（运维指南 §6.5）。
//
// 服务每次启动都会调用它，所以必须幂等：
//   - 账号不存在 → 按给定口令创建，并标记为管理员；
//   - 账号已存在 → 只补管理员权限，**不动口令**。
//
// 不覆盖口令是有意的：运维可能已经在市场里改过密码，
// 启动时按 .env 里的旧值把它改回去会很难排查。口令轮换请走登录后的改密流程。
//
// username 或 password 为空表示没配管理员（比如本地跑内存版），直接跳过。
func (s *Service) EnsureAdmin(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return nil
	}

	existing, err := s.repo.GetUserByUsername(ctx, username)
	switch {
	case err == nil:
		if existing.IsAdmin {
			return nil
		}
		if err := s.repo.SetUserAdmin(ctx, existing.UserID, true); err != nil {
			return internalError(err)
		}
		return nil
	case !errors.Is(err, repo.ErrNotFound):
		return internalError(err)
	}

	user, err := s.Register(ctx, RegisterRequest{Username: username, Password: password})
	if err != nil {
		return err
	}
	if err := s.repo.SetUserAdmin(ctx, user.UserID, true); err != nil {
		return internalError(err)
	}
	return nil
}

// ResetAdminPassword 重置管理员口令（运维指南 §9 Q5：忘记管理员密码）。
//
// EnsureAdmin 有意不覆盖口令（见 D118），所以"救回管理员账号"需要这条显式路径。
// 它同时做三件事：改口令、确保管理员权限、**吊销该账号已签发的全部令牌**——
// 会走到这里通常意味着凭据已经不可信，留着旧 Token 等于没改。
func (s *Service) ResetAdminPassword(ctx context.Context, username, password string) error {
	if len(password) < MinPasswordLength {
		return model.Errorf(model.CodeInvalidRequest, "密码至少需要 8 个字符")
	}

	user, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return model.Errorf(model.CodeNotFound, "用户不存在："+username)
		}
		return internalError(err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return model.Errorf(model.CodeInternal, "密码处理失败")
	}
	if err := s.repo.SetUserPassword(ctx, user.UserID, string(hash)); err != nil {
		return internalError(err)
	}
	if !user.IsAdmin {
		if err := s.repo.SetUserAdmin(ctx, user.UserID, true); err != nil {
			return internalError(err)
		}
	}
	if err := s.repo.DeleteTokensOfUser(ctx, user.UserID); err != nil {
		return internalError(err)
	}

	s.audit(ctx, &model.AuditEntry{
		Action: model.ActionUserRegistered, Operator: username,
		Result: model.ResultSuccess, Detail: "管理员口令已重置",
	})
	return nil
}
