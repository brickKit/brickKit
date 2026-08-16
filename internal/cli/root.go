// Package cli 实现 BrickKit CLI 的命令树。
//
// 设计依据：004 §3 命令集设计（11 个命令 + version）。
//
// 输出分工（见 internal/logging 说明）：人类可读输出走 stdout，
// 结构化 JSON 日志与错误块走 stderr。
package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/version"
)

// DefaultConfigFile 是默认的项目配置文件名（003）。
const DefaultConfigFile = "brickkit.yaml"

// 命令分组 ID，用于 --help 中的归类展示。
const (
	groupProject   = "project"
	groupComponent = "component"
	groupLifecycle = "lifecycle"
	groupMarket    = "market"
)

// Options 是所有命令共享的全局选项与 IO 句柄。
type Options struct {
	// WorkDir 是项目根目录。默认是进程当前目录；显式传入可让命令
	// 不依赖进程级 cwd（测试与将来的嵌套调用都需要这个注入点）。
	WorkDir string
	// ConfigPath 是 --config 指定的项目配置文件路径（004 §3.5）。
	ConfigPath string
	// LogLevel 是 --log-level 指定的日志级别。
	LogLevel string
	// Stdin 承载交互式确认的输入（add 的"是否刷新缓存"等）。为空时不读输入，
	// 等价于用户直接回车（即拒绝）。
	Stdin io.Reader
	// Stdout 承载面向用户的输出，Stderr 承载日志与错误。
	Stdout io.Writer
	Stderr io.Writer
	// Now 提供当前时间（reset 的"恢复时间"等）。为空时用 time.Now，
	// 便于测试锁定输出而不依赖真实时钟。
	Now func() time.Time
	// Engine 是容器引擎。为空时按 005 §7 自动检测（docker → podman）。
	//
	// 命令层的职责是"决定谁该启动、先检查什么"，不是"怎么调 docker"；
	// 把它做成注入点之后，这些决定可以在没有 Docker 的机器上被完整测试。
	Engine engine.Engine
	// Probe 检查一个 host:port 是否可达（status 的资源探测、--check-resources）。
	// 为空时用真实 TCP 拨号。
	Probe func(ctx context.Context, address string) error
}

// probe 拨号检查可达性，未注入时用真实 TCP。
func (o *Options) probe(ctx context.Context, address string) error {
	if o.Probe != nil {
		return o.Probe(ctx, address)
	}

	// 超时要短：这是一次"看一眼"的体检，不是等它恢复
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return conn.Close()
}

// now 返回当前时间，未注入时钟时回落到 time.Now。
func (o *Options) now() time.Time {
	if o.Now == nil {
		return time.Now()
	}
	return o.Now()
}

// NewOptions 返回默认全局选项（输出到真实 stdout/stderr）。
func NewOptions() *Options {
	return &Options{
		WorkDir:    ".",
		ConfigPath: DefaultConfigFile,
		LogLevel:   envOr(logging.EnvLogLevel, logging.LevelInfo),
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Now:        time.Now,
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Printf 把人类可读输出写到 stdout。
func (o *Options) Printf(format string, args ...any) {
	fmt.Fprintf(o.Stdout, format, args...)
}

// Println 把人类可读输出写到 stdout。
func (o *Options) Println(args ...any) {
	fmt.Fprintln(o.Stdout, args...)
}

// NewRootCommand 构建完整的命令树。
func NewRootCommand(opts *Options) *cobra.Command {
	if opts == nil {
		opts = NewOptions()
	}

	root := &cobra.Command{
		Use:   "brickkit",
		Short: "BrickKit：组件管理与拼装平台的命令行工具",
		Long: `BrickKit CLI —— 像搭积木一样构建系统。

CLI 只做六件事（001 §5.1）：
  1. 管理项目配置（brickkit.yaml）
  2. 拉取组件与产物（市场 / Git / 本地）
  3. 解析依赖与推测顺序（强/弱依赖 + 拓扑排序）
  4. 生成部署文件并执行迁移（compose / K8s / Job）
  5. 调用底层引擎与发布（docker compose / kubectl / publish）
  6. 管理组件源码工作区（--repo / sync）

它不是常驻服务：执行完命令就退出，不占用后台资源。`,
		Example: `  brickkit init my-project              初始化项目
  brickkit add erp/backend@1.0.0        添加组件（递归拉取依赖）
  brickkit order                        查看启动顺序
  brickkit up                           生成部署文件并启动
  brickkit status                       查看运行状态
  brickkit down                         停止（不删除 volume）`,
		SilenceUsage:          true, // 错误由 clierr 统一渲染，不打印 usage 噪音
		SilenceErrors:         true,
		DisableFlagsInUseLine: true,
		// 未指定子命令时打印帮助，而不是报错。
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.SetOut(opts.Stdout)
	root.SetErr(opts.Stderr)

	root.PersistentFlags().StringVarP(&opts.ConfigPath, "config", "c", opts.ConfigPath,
		"项目配置文件路径（用于多环境部署，如 brickkit.prod.yaml）")
	root.PersistentFlags().StringVar(&opts.LogLevel, "log-level", opts.LogLevel,
		fmt.Sprintf("stderr 上 JSON 日志的级别（%s）", strings.Join(logging.LevelNames(), " | ")))

	// flag 解析错误统一转成 CLI 错误格式。
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierr.Newf(clierr.CodeInvalidArgument, "错误：参数不合法").
			WithDetail("命令", cmd.CommandPath()).
			WithDetail("原因", err.Error()).
			WithHint(fmt.Sprintf("执行 %s --help 查看该命令的参数说明", cmd.CommandPath())).
			WithExit(clierr.ExitUsage).
			WithCause(err)
	})

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if !logging.IsValidLevel(opts.LogLevel) {
			return clierr.Newf(clierr.CodeInvalidArgument, "错误：日志级别不合法").
				WithDetail("传入值", opts.LogLevel).
				WithDetail("合法值", strings.Join(logging.LevelNames(), " | ")).
				WithExit(clierr.ExitUsage)
		}
		logging.SetLevel(opts.LogLevel)
		logging.Info("命令开始执行",
			"command", cmd.CommandPath(),
			"args", args,
			"config", opts.ConfigPath,
			"version", version.Version,
		)
		return nil
	}

	root.AddGroup(
		&cobra.Group{ID: groupProject, Title: "项目命令："},
		&cobra.Group{ID: groupComponent, Title: "组件命令："},
		&cobra.Group{ID: groupLifecycle, Title: "生命周期命令："},
		&cobra.Group{ID: groupMarket, Title: "市场命令："},
	)

	root.AddCommand(
		newInitCommand(opts),
		newResetCommand(opts),
		newAddCommand(opts),
		newRemoveCommand(opts),
		newSyncCommand(opts),
		newOrderCommand(opts),
		newUpCommand(opts),
		newDownCommand(opts),
		newStatusCommand(opts),
		newLoginCommand(opts),
		newPublishCommand(opts),
		newVersionCommand(opts),
	)

	localize(root)
	return root
}

// usageTemplate 是中文化的 usage 模板（cobra 默认模板的中文版），
// 使帮助信息与设计书中的中文输出风格一致。
const usageTemplate = `用法：{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} <命令> [参数]{{end}}{{if gt (len .Aliases) 0}}

别名：
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例：
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

可用命令：{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

其他命令：{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

参数：
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局参数：
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

帮助主题：{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

执行 "{{.CommandPath}} <命令> --help" 查看某个命令的详细说明。{{end}}
`

// localize 中文化 cobra 内置的 help / completion 命令与 -h 参数说明。
func localize(root *cobra.Command) {
	root.SetUsageTemplate(usageTemplate)

	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		switch c.Name() {
		case "help":
			c.Short = "查看某个命令的帮助信息"
		case "completion":
			c.Short = "生成指定 shell 的自动补全脚本"
		}
	}

	// 递归中文化 -h/--help 的说明文案。
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = fmt.Sprintf("查看 %s 的帮助信息", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// Execute 运行 CLI，返回进程退出码。
func Execute() int {
	opts := NewOptions()
	root := NewRootCommand(opts)
	return Run(root, opts, os.Args[1:])
}

// Run 执行给定的命令树，返回退出码。测试通过它注入参数与 IO。
func Run(root *cobra.Command, opts *Options, args []string) int {
	// 先按环境变量/默认级别初始化日志，保证 --help 等不进入
	// PersistentPreRunE 的路径也有正确的输出目标；flag 解析后再调整级别。
	logging.Init(opts.Stderr, opts.LogLevel)

	start := time.Now()
	root.SetArgs(args)

	// ExecuteC 返回实际执行的命令，日志里才能记到子命令名。
	executed, err := root.ExecuteC()
	elapsed := time.Since(start)
	path := root.CommandPath()
	if executed != nil {
		path = executed.CommandPath()
	}

	if err == nil {
		logging.Info("命令执行完成",
			"command", path,
			"elapsed_ms", elapsed.Milliseconds(),
			"exit_code", clierr.ExitOK,
		)
		return clierr.ExitOK
	}

	e := translate(err)
	code := clierr.Render(opts.Stderr, e)
	level := logging.Error
	if e.Warning {
		level = logging.Warn
	}
	level("命令执行失败",
		"command", path,
		"elapsed_ms", elapsed.Milliseconds(),
		"error_code", string(e.Code),
		"error", e.Error(),
		"exit_code", code,
	)
	return code
}

var unknownCommandRe = regexp.MustCompile(`unknown command "([^"]+)"`)

// translate 把 cobra 产生的原生错误翻译成 CLI 统一错误。
//
// 约定：所有命令的 RunE 只返回 *clierr.Error，因此这里遇到的非 *clierr.Error
// 一定来自 cobra 的命令/参数解析阶段，按用法错误处理（退出码 2）。
func translate(err error) *clierr.Error {
	if e := clierr.As(err); e != nil && e.Code != clierr.CodeInternal {
		return e
	}

	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "unknown command"):
		name := "（未识别）"
		if m := unknownCommandRe.FindStringSubmatch(msg); len(m) == 2 {
			name = m[1]
		}
		return clierr.Newf(clierr.CodeInvalidArgument, "错误：未知命令 %s", name).
			WithDetail("用法", "brickkit <命令> [参数]").
			WithHint("执行 brickkit --help 查看所有可用命令").
			WithExit(clierr.ExitUsage).
			WithCause(err)
	default:
		return clierr.New(clierr.CodeInvalidArgument, "错误：命令用法不正确").
			WithDetail("原因", msg).
			WithHint("执行 brickkit --help 查看命令用法").
			WithExit(clierr.ExitUsage).
			WithCause(err)
	}
}
