package cli

import (
	"bufio"
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/market"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/source"
)

// loginFlags 是 brickkit login 的参数。
type loginFlags struct {
	market        string
	username      string
	passwordStdin bool
}

// newLoginCommand 实现 brickkit login（004 §3.12）。
func newLoginCommand(opts *Options) *cobra.Command {
	var f loginFlags

	cmd := &cobra.Command{
		Use:     "login",
		Short:   i18n.T(msgid.CliLoginShort),
		GroupID: groupMarket,
		Long:    i18n.T(msgid.CliLoginLong),
		Example: `  brickkit login
  brickkit login --market https://market.brickkit.io/api/v1
  echo "$PASSWORD" | brickkit login --username ci-bot --password-stdin`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd.Context(), opts, f)
		},
	}

	cmd.Flags().StringVar(&f.market, "market", "", i18n.T(msgid.CliLoginMarketAddressDefaultsToThe))
	cmd.Flags().StringVar(&f.username, "username", "", i18n.T(msgid.CliLoginUserNameAskedInteractivelyIf))
	cmd.Flags().BoolVar(&f.passwordStdin, "password-stdin", false, i18n.T(msgid.CliLoginReadThePasswordFromStandard))
	return cmd
}

func runLogin(ctx context.Context, opts *Options, f loginFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	marketURL, err := resolveMarketURL(layout, f.market)
	if err != nil {
		return err
	}

	opts.Printf("%s\n", i18n.T(msgid.CliLoginLoggingInToTheBrickkit))
	opts.Printf("%s\n", i18n.T(msgid.CliLoginMarketAddress, marketURL))

	// 用户名与密码共用同一个带缓冲的读取器：每次新建会把上一行之后
	// 已经读进缓冲区的内容丢掉，密码就再也读不到了。
	in := newPrompter(opts)
	username, err := in.username(f.username)
	if err != nil {
		return err
	}
	password, err := in.password(f.passwordStdin)
	if err != nil {
		return err
	}

	result, err := market.New(marketURL, "").Login(ctx, username, password)
	if err != nil {
		return loginError(err)
	}

	// 凭据里记下是哪个市场的 Token：绝不把 A 市场的凭据发给 B 市场（008）
	creds := &source.Credentials{
		Type:      source.CredentialTypePassword,
		MarketURL: marketURL,
		Username:  result.Username,
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt,
		CreatedAt: opts.now().UTC(),
	}
	if creds.Username == "" {
		creds.Username = username
	}
	if err := source.SaveCredentials(layout.CredentialsPath(), creds); err != nil {
		return err
	}

	opts.Printf("%s\n", i18n.T(msgid.CliLoginLoggedIn))
	opts.Printf("%s\n", i18n.T(msgid.CliLogoutUser, creds.Username))
	opts.Printf("%s\n", i18n.T(msgid.CliLoginTokenStoredAt, config.DirBrickkit+"/"+config.FileCredentials))
	if !creds.ExpiresAt.IsZero() {
		opts.Printf("%s\n", i18n.T(msgid.CliLoginValidUntil, creds.ExpiresAt.Format(time.RFC3339)))
	}
	return nil
}

// loginError 把市场返回的认证失败翻译成"登录失败"。
//
// 同样是 401，publish 时该说"去登录"，而登录时说"去登录"就成了废话——
// 这里要说的是"用户名或密码不对"。
func loginError(err error) error {
	cliErr := clierr.As(err)
	if cliErr == nil || cliErr.Code != clierr.CodeAuthRequired {
		return err
	}
	return clierr.New(clierr.CodeAuthFailed, i18n.T(msgid.CliLoginErrorLoginFailedWrongUser)).
		WithHint(i18n.T(msgid.CliLoginCheckTheUserNameAnd), i18n.T(msgid.CliLoginIfYouForgotThePassword))
}

// resolveMarketURL 决定要登录哪个市场。
//
// 优先 --market；否则取 brickkit.yaml 中启用的 market 安装源。
// 有多个时不猜——猜错就把 Token 发给了另一个市场。
func resolveMarketURL(layout config.Layout, explicit string) (string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit, nil
	}

	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return "", clierr.New(clierr.CodeAuthRequired, i18n.T(msgid.CliLoginErrorCannotDetermineWhichMarket)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliLoginTheCurrentDirectoryIsNot)).
			WithHint(i18n.T(msgid.CliLoginSpecifyTheMarketAddressWith))
	}

	var candidates []config.Source
	for _, s := range cfg.Sources {
		if s.Type == config.SourceTypeMarket && s.IsEnabled() {
			candidates = append(candidates, s)
		}
	}

	switch len(candidates) {
	case 1:
		return candidates[0].URL, nil
	case 0:
		return "", clierr.New(clierr.CodeAuthRequired, i18n.T(msgid.CliLoginErrorBrickkitYamlHasNo)).
			WithHint(
				i18n.T(msgid.CliLoginAddAnInstallSourceOf),
				i18n.T(msgid.CliLoginOrSpecifyTheMarketAddress),
			)
	default:
		err := clierr.New(clierr.CodeAuthRequired, i18n.T(msgid.CliLoginErrorSeveralMarketInstallSources)).
			WithHint(i18n.T(msgid.CliLoginSpecifyOneOfTheMarket))
		for _, s := range candidates {
			err = err.WithDetailf(s.ID, "%s", s.URL)
		}
		return "", err
	}
}

// prompter 负责交互式读取凭据。
type prompter struct {
	opts   *Options
	reader *bufio.Reader
}

func newPrompter(opts *Options) *prompter {
	var reader *bufio.Reader
	if opts.Stdin != nil {
		reader = bufio.NewReader(opts.Stdin)
	}
	return &prompter{opts: opts, reader: reader}
}

// username 取用户名：优先参数，其次交互输入。
func (p *prompter) username(explicit string) (string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		p.opts.Printf("%s\n", i18n.T(msgid.CliLoginUserName2, explicit))
		return explicit, nil
	}

	p.opts.Printf("%s", i18n.T(msgid.CliLoginUserName))
	line, err := p.line()
	if err != nil {
		return "", err
	}
	p.opts.Printf("%s\n", line)
	if line == "" {
		return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliLoginErrorTheUserNameMust)).
			WithHint(i18n.T(msgid.CliLoginRunBrickkitLoginAgainAnd2))
	}
	return line, nil
}

// password 取密码。终端上隐藏输入；管道里按一行读（CI 场景）。
//
// 无论哪条路径，密码都不回显——终端里回显会被旁人看到，
// 管道里回显会被写进 CI 日志。
func (p *prompter) password(fromStdin bool) (string, error) {
	if !fromStdin {
		p.opts.Printf("%s", i18n.T(msgid.CliLoginPassword))
	}

	value, err := p.readSecret()
	if err != nil {
		return "", err
	}
	if !fromStdin {
		p.opts.Printf("\n")
	}
	if value == "" {
		return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliLoginErrorThePasswordMustNot)).
			WithHint(i18n.T(msgid.CliLoginRunBrickkitLoginAgainAnd))
	}
	return value, nil
}

// readSecret 在真终端上关闭回显读取，其余情况按一行读。
func (p *prompter) readSecret() (string, error) {
	file, ok := p.opts.Stdin.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return p.line()
	}

	raw, err := term.ReadPassword(int(file.Fd()))
	if err != nil {
		return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliLoginErrorFailedToReadThe)).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).WithCause(err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// line 从标准输入读一行。
func (p *prompter) line() (string, error) {
	if p.reader == nil {
		return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliLoginErrorNoReadableStandardInput)).
			WithHint(i18n.T(msgid.CliLoginRunBrickkitLoginInA))
	}

	line, err := p.reader.ReadString('\n')
	if err != nil && line == "" {
		return "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliLoginErrorFailedToReadThe2)).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).WithCause(err)
	}
	return strings.TrimSpace(line), nil
}
