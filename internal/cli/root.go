// Package cli 实现 BrickKit CLI 的命令树。
//
// 命令分两类：项目命令（init / add / up …）与 CLI 自身的命令（version / lang）。
//
// 输出分工（见 internal/logging 说明）：人类可读输出走 stdout，
// 结构化 JSON 日志与错误块走 stderr。
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/version"
)

// DefaultConfigFile 是项目声明文件名。
const DefaultConfigFile = project.FileDecl

// addDeployFileFlags 给读取部署文件的命令（up / down / status / sync / lint / graph）
// 加上 -f / --file 与 --no-local（提案 §11.6）。
func addDeployFileFlags(cmd *cobra.Command, opts *Options) {
	cmd.Flags().StringVarP(&opts.DeployFile, "file", "f", opts.DeployFile, i18n.T(msgid.CliRootFlagDeployFile))
	cmd.Flags().BoolVar(&opts.NoLocal, "no-local", opts.NoLocal, i18n.T(msgid.CliRootFlagNoLocal))
	_ = cmd.RegisterFlagCompletionFunc("file", completeDeployFiles(opts))
}

// loadOptions 把命令行选择翻译成装载选项。
func (o *Options) loadOptions() project.LoadOptions {
	return project.LoadOptions{DeployFile: o.DeployFile, NoLocal: o.NoLocal}
}

// annotFindsProject 标出"作用于项目"的命令：在项目的子目录里运行时向上找项目根（设计 §3）。
// 只作用于当前目录的命令（init、release、publish、skills）不带它。
const annotFindsProject = "brickkit/finds-project"

// findsProject 看命令自己或它的上级有没有这个标记（local on 之类的子命令跟着 local 走）。
func findsProject(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[annotFindsProject] == "true" {
			return true
		}
	}
	return false
}

// findsProjectAnnotation 是给项目命令用的 Annotations。
func findsProjectAnnotation() map[string]string {
	return map[string]string{annotFindsProject: "true"}
}

// display 把路径显示成相对使用者所在目录的样子。
func (o *Options) display(path string) string { return displayPath(o.CallDir, path) }

// enterProject 在项目命令开始前定位项目根：当前目录没有 brickkit.yaml 就往上找；找到了就
// 换过去并说一句用的是哪个项目。找不到时什么都不改，命令照旧报它自己的 PROJECT_MISSING。
func (o *Options) enterProject() error {
	root, found, err := project.FindRoot(o.WorkDir)
	if err != nil || !found {
		return err
	}
	if filepath.Clean(root) == filepath.Clean(o.CallDir) {
		return nil
	}
	o.WorkDir = root
	name := filepath.Base(root)
	if decl, err := projfile.ParseFile(project.NewLayout(root).DeclPath()); err == nil && decl.Project != "" {
		name = decl.Project
	}
	o.Printf("%s\n", i18n.T(msgid.CliProjectFoundAbove, o.display(root), name))
	return nil
}

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
	// 项目命令在子目录里运行时，它会被换成向上找到的项目根（见 enterProject）。
	WorkDir string
	// CallDir 是使用者敲命令时所在的目录（绝对路径）。项目命令向上找到项目后，WorkDir 换成
	// 项目根，而显示给人看的路径、"我在哪个组件里"都按 CallDir 算——像 git 一样。
	CallDir string
	// DeployFile 是 -f / --file 指定的部署文件（deploy.prod.yaml 之类）：指定了就只读它，
	// 本地模式被忽略（提案 §11.6）。空表示按默认规则选择。
	DeployFile string
	// NoLocal 是 --no-local：本次忽略 deploy.local.yaml，不改变本地模式的开关。
	NoLocal bool
	// LogLevel 是 --log-level 指定的日志级别。
	LogLevel string
	// Stdin 承载交互式确认的输入（add 的"是否刷新缓存"等）。为空时不读输入，
	// 等价于用户直接回车（即拒绝）。
	Stdin io.Reader
	// Stdout 承载面向用户的输出，Stderr 承载日志与错误。
	Stdout io.Writer
	Stderr io.Writer
	// Now 提供当前时间（登录凭据的签发/过期判断等）。为空时用 time.Now，
	// 便于测试锁定输出而不依赖真实时钟。
	Now func() time.Time
	// ResolveDigest 把镜像 tag 解析成 registry 里的 digest。
	// 为空时用真实实现（docker buildx imagetools）。测试可替换。
	ResolveDigest func(ctx context.Context, image string) (string, error)
	// RepoCacheDir 是 git 源的 bare 仓库缓存目录。空表示用户级默认位置（附录 A12）；
	// 测试用它隔离，不是面向使用者的开关。
	RepoCacheDir string
	// stdinReader 是 Stdin 上唯一的缓冲读取器：一次命令里有好几个问题时，
	// 每次新建读取器会把后面的回答吞进前一个的缓冲区。
	stdinReader *bufio.Reader
	// Images 是本机镜像的操作（build、up 的镜像检查）。为空时按部署目标用 docker 或 podman；
	// 测试可替换。
	Images engine.Images
	// Engine 是容器引擎。为空时按部署目标自动选择。
	//
	// 命令层的职责是"决定谁该启动、先检查什么"，不是"怎么调 docker"；
	// 把它做成注入点之后，这些决定可以在没有 Docker 的机器上被完整测试。
	Engine engine.Engine
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
		WorkDir:  ".",
		LogLevel: envOr(logging.EnvLogLevel, logging.LevelWarn),
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Now:      time.Now,
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
	_, _ = fmt.Fprintf(o.Stdout, format, args...)
}

// Println 把人类可读输出写到 stdout。
func (o *Options) Println(args ...any) {
	_, _ = fmt.Fprintln(o.Stdout, args...)
}

// NewRootCommand 构建完整的命令树。
func NewRootCommand(opts *Options) *cobra.Command {
	if opts == nil {
		opts = NewOptions()
	}

	// 每次都重新解析并设置当前语言（跟 internal/logging.Init 在每次
	// Run() 都重新初始化是同一种用法）：命令树里每个子命令的 Short/Long
	// 是在这里构建时就写死的字符串，语言必须先确定下来。
	lang, _ := i18n.Resolve()
	i18n.SetCurrent(lang)

	root := &cobra.Command{
		Use:                   "brickkit",
		Short:                 i18n.T(msgid.CliRootShort),
		Long:                  i18n.T(msgid.CliRootLong),
		Example:               i18n.T(msgid.CliRootExample),
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

	root.PersistentFlags().StringVar(&opts.LogLevel, "log-level", opts.LogLevel,
		i18n.T(msgid.CliRootLevelOfTheJSONLogs, strings.Join(logging.LevelNames(), " | ")))

	// flag 解析错误统一转成 CLI 错误格式。
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliRootErrorInvalidArguments)).
			WithDetail(i18n.T(msgid.LabelCommand), cmd.CommandPath()).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.CliRootRunHelpToSeeThe, cmd.CommandPath())).
			WithExit(clierr.ExitUsage).
			WithCause(err)
	})

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if !logging.IsValidLevel(opts.LogLevel) {
			return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliRootErrorInvalidLogLevel)).
				WithDetail(i18n.T(msgid.CliRootValueGiven), opts.LogLevel).
				WithDetail(i18n.T(msgid.CliRootValidValues), strings.Join(logging.LevelNames(), " | ")).
				WithExit(clierr.ExitUsage).WithHint(i18n.T(msgid.CliRootHintLogLevel))
		}
		logging.SetLevel(opts.LogLevel)
		if opts.CallDir == "" {
			abs, err := filepath.Abs(opts.WorkDir)
			if err != nil {
				return err
			}
			opts.WorkDir, opts.CallDir = abs, abs
		}
		if findsProject(cmd) {
			if err := opts.enterProject(); err != nil {
				return err
			}
		}
		logging.Info(i18n.T(msgid.LogCommandStarted),
			"command", cmd.CommandPath(),
			"args", args,
			"deployFile", opts.DeployFile,
			"version", version.Version,
		)
		return nil
	}

	root.AddGroup(
		&cobra.Group{ID: groupProject, Title: i18n.T(msgid.CliRootProjectCommands)},
		&cobra.Group{ID: groupComponent, Title: i18n.T(msgid.CliRootComponentCommands)},
		&cobra.Group{ID: groupLifecycle, Title: i18n.T(msgid.CliRootLifecycleCommands)},
		&cobra.Group{ID: groupMarket, Title: i18n.T(msgid.CliRootMarketCommands)},
	)

	root.AddCommand(
		newInitCommand(opts),
		newSkillsCommand(opts),
		newGraphCommand(opts),
		newDepsCommand(opts),
		newLintCommand(opts),
		newNewCommand(opts),
		newAddCommand(opts),
		newRemoveCommand(opts),
		newFetchCommand(opts),
		newBuildCommand(opts),
		newUpgradeCommand(opts),
		newSyncCommand(opts),
		newLocalCommand(opts),
		newRestoreCommand(opts),
		newUpCommand(opts),
		newDownCommand(opts),
		newStatusCommand(opts),
		newLoginCommand(opts),
		newLogoutCommand(opts),
		newPublishCommand(opts),
		newReleaseCommand(opts),
		newVersionCommand(opts),
		newLangCommand(opts),
	)

	localize(root)
	return root
}

// localize 用当前语言目录里的文案替换 cobra 自带的模板与说明；目录里是空字符串就保留 cobra 自带的英文。
func localize(root *cobra.Command) {
	if tmpl := i18n.T(msgid.CobraUsageTemplate); tmpl != "" {
		root.SetUsageTemplate(tmpl)
	}
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		switch c.Name() {
		case "help":
			if s := i18n.T(msgid.CobraHelpShort); s != "" {
				c.Short = s
			}
		case "completion":
			if s := i18n.T(msgid.CobraCompletionShort); s != "" {
				c.Short = s
			}
		}
	}
	flagText := i18n.T(msgid.CobraHelpFlag)
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil && flagText != "" {
			f.Usage = i18n.T(msgid.CobraHelpFlag, c.DisplayName(), c.CommandPath())
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
		logging.Info(i18n.T(msgid.LogCommandFinished),
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
	level(i18n.T(msgid.LogCommandFailed),
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
//
// 判据必须是"本来就是不是 *clierr.Error"（Structured），不能是"Code 不等于
// CodeInternal"：后者会把写配置失败这类真正的 CodeInternal 也说成
// "命令用法不正确，执行 brickkit --help"——磁盘满了却让人去查命令怎么写。
func translate(err error) *clierr.Error {
	if e, ok := clierr.Structured(err); ok {
		return e
	}

	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "unknown command"):
		name := i18n.T(msgid.CliRootUnrecognized)
		if m := unknownCommandRe.FindStringSubmatch(msg); len(m) == 2 {
			name = m[1]
		}
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliRootErrorUnknownCommand, name)).
			WithDetail(i18n.T(msgid.LabelUsage), i18n.T(msgid.CliRootBrickkitCommandArguments)).
			WithHint(i18n.T(msgid.CliRootHintHelpForCommands)).
			WithExit(clierr.ExitUsage).
			WithCause(err)
	default:
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliRootErrorIncorrectCommandUsage)).
			WithDetail(i18n.T(msgid.LabelReason), msg).
			WithHint(i18n.T(msgid.CliRootHintHelpForUsage)).
			WithExit(clierr.ExitUsage).
			WithCause(err)
	}
}
