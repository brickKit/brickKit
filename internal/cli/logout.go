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
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/market"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/source"
)

// newLogoutCommand 实现 brickkit logout（004 §3.13）。
func newLogoutCommand(opts *Options) *cobra.Command {
	var keepRemote bool

	cmd := &cobra.Command{
		Use:     "logout",
		Short:   i18n.T(msgid.CliLogoutLogOutOfTheMarket),
		GroupID: groupMarket,
		Long:    i18n.T(msgid.CliLogoutLogOutOfTheComponent),
		Example: i18n.T(msgid.CliLogoutBrickkitLogoutBrickkitLogoutKeep),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(cmd.Context(), opts, keepRemote)
		},
	}

	cmd.Flags().BoolVar(&keepRemote, "keep-remote", false,
		i18n.T(msgid.CliLogoutDeleteOnlyTheLocalCredentials))
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
		opts.Printf("%s\n", i18n.T(msgid.CliLogoutTheLoginCredentialsCouldNot, clierr.As(err).Message))
		if err := source.RemoveCredentials(path); err != nil {
			return err
		}
		opts.Printf("%s\n", i18n.T(msgid.CliLogoutDeleted, displayPath(opts.WorkDir, path)))
		opts.Printf("%s\n", i18n.T(msgid.CliLogoutTheTokenInsideItIf))
		return nil

	case creds == nil:
		// 文件根本不存在：没登录，什么都不用做，也不算失败
		opts.Printf("%s\n", i18n.T(msgid.CliLogoutThereAreNoLoginCredentials, displayPath(opts.WorkDir, path)))
		opts.Printf("%s\n", i18n.T(msgid.CliLogoutLogInToTheMarket))
		return nil
	}

	// 先通知市场，再删本地：反过来的话，本地都没了却发现市场调不通，
	// 使用者连"哪个 Token 还有效"都查不到
	remote := logoutRemote(ctx, opts, creds, keepRemote)

	if err := source.RemoveCredentials(path); err != nil {
		return err
	}

	opts.Printf("%s\n", i18n.T(msgid.CliLogoutLoggedOut))
	opts.Printf("%s\n", i18n.T(msgid.CliLogoutUser, creds.Username))
	opts.Printf("%s\n", i18n.T(msgid.CliLogoutDeleted2, displayPath(opts.WorkDir, path)))
	if remote != "" {
		opts.Printf("   ⚠️ %s\n", remote)
		opts.Printf("%s\n", i18n.T(msgid.CliLogoutThatTokenRemainsValidOn, creds.ExpiresAt.Format("2006-01-02 15:04:05")))
	}
	logging.Info(i18n.T(msgid.LogLoggedOut), "user", creds.Username, "market", creds.MarketURL)
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
		return i18n.T(msgid.CliLogoutSkippedNotifyingTheMarketBecause)
	case creds.MarketURL == "":
		return i18n.T(msgid.CliLogoutTheCredentialsRecordNoMarket)
	case creds.Token == "":
		return i18n.T(msgid.CliLogoutTheCredentialsContainNoToken)
	}

	client := market.New(creds.MarketURL, creds.Token)
	if err := client.Logout(ctx); err != nil {
		return i18n.T(msgid.CliLogoutTheMarketIsUnreachableSo, clierr.As(err).Message)
	}
	return ""
}
