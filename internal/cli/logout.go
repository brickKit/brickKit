package cli

// 本文件实现 brickkit logout（004 §3.13）。
//
// # 为什么需要它
//
// `login` 把 Token 写进 `.brickkit/credentials`，而在此之前**没有任何命令能
// 撤销这件事**：市场早就有 `POST /api/v1/auth/logout`（007 §9.5），CLI 侧却是
// 空的。使用者只能手工 `rm .brickkit/credentials`——而那只删了本地那一份，
// 服务端那个 Token 一直有效到过期为止。换台机器、换个账号、或者只是不想让一份
// 长期有效的凭据躺在盘上，都没有正经的出路。

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/market"
	"github.com/brickkit/brickkit/internal/source"
)

// newLogoutCommand 实现 brickkit logout（004 §3.13）。
func newLogoutCommand(opts *Options) *cobra.Command {
	var keepRemote bool

	cmd := &cobra.Command{
		Use:     "logout",
		Short:   "退出市场登录：作废服务端的 Token，并删除本地凭据",
		GroupID: groupMarket,
		Long: `退出组件市场的登录（004 §3.13）。

做两件事：
  1. 调市场的 POST /auth/logout 作废这个 Token（服务端那一侧）
  2. 删掉 .brickkit/credentials（本地那一侧）

**本地那一份一定会删**，即使市场连不上——否则一次网络抖动就让人以为自己已经
退出了，而凭据还躺在盘上。市场不可达时只警告一句，并说明那个 Token 仍然有效
到过期为止。

没登录时什么都不做，也不算失败。`,
		Example: `  brickkit logout
  brickkit logout --keep-remote   只删本地凭据，不通知市场`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(cmd.Context(), opts, keepRemote)
		},
	}

	cmd.Flags().BoolVar(&keepRemote, "keep-remote", false,
		"只删本地凭据，不调市场作废 Token（离线时用）")
	return cmd
}

// runLogout 退出登录。
func runLogout(ctx context.Context, opts *Options, keepRemote bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	path := layout.CredentialsPath()

	creds, err := source.LoadCredentials(path)
	switch {
	case err != nil:
		// 文件在、但读不出来（格式坏了）。**照样删掉**——那正是使用者想清掉它的
		// 时候，而这时既不知道用户名也不知道 Token，通知市场无从谈起
		opts.Printf("⚠️ 登录凭据读不出来：%s\n", clierr.As(err).Message)
		if err := source.RemoveCredentials(path); err != nil {
			return err
		}
		opts.Printf("✅ 已删除 %s\n", displayPath(opts.WorkDir, path))
		opts.Printf("   ⚠️ 里面那个 Token（如果有）在市场那边仍然有效，直到它自己过期\n")
		return nil

	case creds == nil:
		// 文件根本不存在：没登录，什么都不用做，也不算失败
		opts.Printf("📋 当前没有登录凭据（%s 不存在）\n", displayPath(opts.WorkDir, path))
		opts.Printf("   用 brickkit login 登录市场\n")
		return nil
	}

	// 先通知市场，再删本地：反过来的话，本地都没了却发现市场调不通，
	// 使用者连"哪个 Token 还有效"都查不到
	remote := logoutRemote(ctx, opts, creds, keepRemote)

	if err := source.RemoveCredentials(path); err != nil {
		return err
	}

	opts.Printf("✅ 已退出登录\n")
	opts.Printf("   用户：%s\n", creds.Username)
	opts.Printf("   已删除：%s\n", displayPath(opts.WorkDir, path))
	if remote != "" {
		opts.Printf("   ⚠️ %s\n", remote)
		opts.Printf("      那个 Token 在市场那边仍然有效，直到 %s 过期\n",
			creds.ExpiresAt.Format("2006-01-02 15:04:05"))
	}
	logging.Info("已退出登录", "user", creds.Username, "market", creds.MarketURL)
	return nil
}

// logoutRemote 调市场作废 Token；没做或没做成时返回一句该说明的话。
//
// **失败绝不阻断。** 本地凭据一定要删掉——否则一次网络抖动就让人以为自己已经
// 退出了，而那份凭据还躺在盘上，比没退出更糟。
func logoutRemote(
	ctx context.Context, opts *Options, creds *source.Credentials, keepRemote bool,
) string {
	switch {
	case keepRemote:
		return "按 --keep-remote 跳过了通知市场"
	case creds.MarketURL == "":
		return "凭据里没记市场地址，无法通知市场作废"
	case creds.Token == "":
		return "凭据里没有 Token，无需通知市场"
	}

	client := market.New(creds.MarketURL, creds.Token)
	if err := client.Logout(ctx); err != nil {
		return "市场不可达，没能作废它（" + clierr.As(err).Message + "）"
	}
	return ""
}
