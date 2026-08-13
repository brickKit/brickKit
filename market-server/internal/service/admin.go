package service

import (
	"context"
	"errors"

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
