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
	"github.com/brickkit/brickkit/internal/market"
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
		Short:   "登录组件市场，Token 存入 .brickkit/credentials",
		GroupID: groupMarket,
		Long: `登录 BrickKit 市场（004 §3.12）。

行为：
  1. 终端输入用户名
  2. 终端输入密码（隐藏输入）
  3. 调用市场 API 验证凭据
  4. 成功则把 Token 写入 .brickkit/credentials（权限 0600）

市场地址取自 brickkit.yaml 中类型为 market 的安装源；配了多个时用 --market 指定。

Token 优先级：.brickkit/credentials > brickkit.yaml 中的 sources.authToken。
CLI 每次使用 Token 前检查 expiresAt，过期则提示重新登录（不做自动刷新）。`,
		Example: `  brickkit login
  brickkit login --market https://market.brickkit.io/api/v1
  echo "$PASSWORD" | brickkit login --username ci-bot --password-stdin`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd.Context(), opts, f)
		},
	}

	cmd.Flags().StringVar(&f.market, "market", "", "市场地址（默认取 brickkit.yaml 中的 market 安装源）")
	cmd.Flags().StringVar(&f.username, "username", "", "用户名（不指定则交互式输入）")
	cmd.Flags().BoolVar(&f.passwordStdin, "password-stdin", false, "从标准输入读密码（CI 用，不回显也不进历史）")
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

	opts.Printf("🔐 登录 BrickKit Market\n")
	opts.Printf("   市场地址：%s\n", marketURL)

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

	opts.Printf("✅ 登录成功\n")
	opts.Printf("   用户：%s\n", creds.Username)
	opts.Printf("   Token 已存储到 %s\n", config.DirBrickkit+"/"+config.FileCredentials)
	if !creds.ExpiresAt.IsZero() {
		opts.Printf("   有效期至：%s\n", creds.ExpiresAt.Format(time.RFC3339))
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
	return clierr.New(clierr.CodeAuthFailed, "错误：登录失败：用户名或密码错误").
		WithHint("确认用户名与密码后重试", "忘记密码请联系市场管理员")
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
		return "", clierr.New(clierr.CodeAuthRequired, "错误：无法确定要登录的市场地址").
			WithDetail("原因", "当前目录不是 BrickKit 项目，也没有指定 --market").
			WithHint("用 --market 指定市场地址，例如 brickkit login --market https://market.example.com/api/v1")
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
		return "", clierr.New(clierr.CodeAuthRequired, "错误：brickkit.yaml 中没有可用的市场安装源").
			WithHint(
				"在 brickkit.yaml → sources 中添加 type: market 的安装源",
				"或用 --market 直接指定市场地址",
			)
	default:
		err := clierr.New(clierr.CodeAuthRequired, "错误：配置了多个市场安装源，无法确定登录哪一个").
			WithHint("用 --market 指定其中一个市场地址")
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
		p.opts.Printf("   用户名：%s\n", explicit)
		return explicit, nil
	}

	p.opts.Printf("   用户名：")
	line, err := p.line()
	if err != nil {
		return "", err
	}
	p.opts.Printf("%s\n", line)
	if line == "" {
		return "", clierr.New(clierr.CodeInvalidArgument, "错误：用户名不能为空").
			WithHint("重新执行 brickkit login 并输入用户名")
	}
	return line, nil
}

// password 取密码。终端上隐藏输入；管道里按一行读（CI 场景）。
//
// 无论哪条路径，密码都不回显——终端里回显会被旁人看到，
// 管道里回显会被写进 CI 日志。
func (p *prompter) password(fromStdin bool) (string, error) {
	if !fromStdin {
		p.opts.Printf("   密码：")
	}

	value, err := p.readSecret()
	if err != nil {
		return "", err
	}
	if !fromStdin {
		p.opts.Printf("\n")
	}
	if value == "" {
		return "", clierr.New(clierr.CodeInvalidArgument, "错误：密码不能为空").
			WithHint("重新执行 brickkit login 并输入密码")
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
		return "", clierr.New(clierr.CodeInvalidArgument, "错误：读取密码失败").
			WithDetail("原因", err.Error()).WithCause(err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// line 从标准输入读一行。
func (p *prompter) line() (string, error) {
	if p.reader == nil {
		return "", clierr.New(clierr.CodeInvalidArgument, "错误：没有可读的标准输入").
			WithHint("在终端中执行 brickkit login，或用 --username 与 --password-stdin 提供凭据")
	}

	line, err := p.reader.ReadString('\n')
	if err != nil && line == "" {
		return "", clierr.New(clierr.CodeInvalidArgument, "错误：读取输入失败").
			WithDetail("原因", err.Error()).WithCause(err)
	}
	return strings.TrimSpace(line), nil
}
