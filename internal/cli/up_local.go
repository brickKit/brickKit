package cli

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/procsup"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/runcmd"
	"github.com/brickkit/brickkit/internal/sessionlock"
	"github.com/brickkit/brickkit/internal/workspace"
)

// anyModeLocal 判断项目里有没有 mode: local 组件。
func anyModeLocal(components []config.Component) bool {
	for _, c := range components {
		if c.Mode == config.ModeLocal {
			return true
		}
	}
	return false
}

// checkLocalSources 确认每个即将运行的 mode: local 组件都有本地源码目录。
//
// 跟 mode: debug 不一样：debug 的进程由用户自己在 IDE 里启动，可能在这台机器
// 上的任何地方，平台不关心；local 的进程由平台自己拉起，得知道去哪个目录、
// cd 进去执行探测出的命令——没有源码目录，这件事根本无从谈起。
//
// 放在生成阶段查（buildUpPlan 里，--dry-run 也会走到）：跟资源绑定检查
// （resolver.CheckRunningResourceBindings）同一类"生成阶段就该知道、
// 不该等运行时才炸"的检查。
func checkLocalSources(layout config.Layout, cfg *config.Config, running []resolver.Ref) error {
	runningSet := make(map[string]bool, len(running))
	for _, ref := range running {
		runningSet[ref.ID] = true
	}

	p := clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpLocalComponentsMissingSource))
	for i, c := range cfg.Components {
		if c.Mode != config.ModeLocal || !runningSet[c.ID] {
			continue
		}
		if !workspace.Exists(layout, c.ID) {
			p.Add(fmt.Sprintf("components[%d]", i), i18n.T(msgid.CliUpNoLocalSourceFor, c.ID))
		}
	}
	return p.Err()
}

// localComponentPlan 是一个 mode: local 组件要怎么启动的全部结论。
type localComponentPlan struct {
	Ref     resolver.Ref
	Service string
	Dir     string
	Command runcmd.Command
	Env     []string
	Port    int
}

// collectLocalComponents 按拓扑序收集全部 mode: local 组件，探测出各自的启动命令。
//
// lookup 这一步还用不上（Task 3 才会把它接进 buildLocalEnv）——现在就放进签名，
// 是不想等 Task 3 再回头改一次签名、连带改掉这一步已经写好的调用点与测试。
func collectLocalComponents(
	layout config.Layout, cfg *config.Config, graph *resolver.Graph,
	order *resolver.Plan, localEnvFiles []compose.LocalEnvFile, lookup func(string) (string, bool),
) ([]localComponentPlan, error) {
	localMode := make(map[string]bool, len(cfg.Components))
	for _, c := range cfg.Components {
		if c.Mode == config.ModeLocal {
			localMode[c.ID] = true
		}
	}
	if len(localMode) == 0 {
		return nil, nil
	}

	portOf := make(map[resolver.Ref]int, len(localEnvFiles))
	varsOf := make(map[resolver.Ref][]inject.Var, len(localEnvFiles))
	for _, f := range localEnvFiles {
		portOf[f.Ref] = f.Port
		varsOf[f.Ref] = f.Vars
	}

	var out []localComponentPlan
	for _, step := range order.Steps {
		ref := step.Ref
		if !localMode[ref.ID] {
			continue
		}
		node := graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		port, ok := portOf[ref]
		if !ok {
			continue // localEnvFiles 只含"这次真的在跑"的组件；跟 collectTargets 同一份 running 集合
		}

		dir := workspace.SourceDir(layout, ref.ID)
		hints := hintsFromManifest(node.Manifest.Local)
		cmd, err := runcmd.Detect(dir, hints, runcmd.Params{Port: port})
		if err != nil {
			return nil, detectionError(ref, err)
		}
		if err := cmd.CheckProgram(); err != nil {
			return nil, programMissingError(ref, err)
		}

		env, err := buildLocalEnv(ref, varsOf[ref], port, cmd.Env, lookup)
		if err != nil {
			return nil, err
		}
		out = append(out, localComponentPlan{
			Ref: ref, Service: step.Service, Dir: dir, Command: cmd, Env: env, Port: port,
		})
	}
	return out, nil
}

// hintsFromManifest 把 component.yaml 的 local: 块翻译成 runcmd.Hints；
// 没写这个块（*Local 为 nil）就是全部留空，交给自动探测。
func hintsFromManifest(l *manifest.Local) runcmd.Hints {
	if l == nil {
		return runcmd.Hints{}
	}
	return runcmd.Hints{Language: l.Language, RunCommand: l.RunCommand}
}

// runcmd 那几个结构化错误类型（*NoCommandError/*AmbiguousError/*UnknownLanguageError/
// *ProgramMissingError）各自的 Error() 已经把 Problem.Reason/Detail/Options（或候选
// 语言/命令）拼成了一句完整、可读的英文诊断。不逐条翻译底层错误的文字本身（那会是
// 一张几乎复述 runcmd/errors.go 全部枚举值的翻译表，且两边的分类迟早会走漂），
// 照抄 engineFailure 的手法：一条通用的顶层消息，把 err.Error() 原样放进一条
// Detail（复用现成的 msgid.LabelReason）。
func detectionError(ref resolver.Ref, err error) error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpCouldNotDetermineHowToStart, ref.String())).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.CliUpWriteLocalRunCommandInThe, ref.ID))
}

func programMissingError(ref resolver.Ref, err error) error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpTheDetectedProgramIsNotInstalled, ref.String())).
		WithDetail(i18n.T(msgid.LabelReason), err.Error())
}

// localEnvVarRe 跟 internal/compose/local.go 里的那条是同一份规则（003 §5.4）——
// 不导出，各自维护一份是现有仓库惯例，internal/config 与 internal/compose 也
// 各自有一份同规则的正则，不是本计划引入的新重复。
var localEnvVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// buildLocalEnv 把 vars 严格展开成 "KEY=VALUE" 列表，叠上语言适配器自己的
// 环境变量与 PORT。展开不了任何一个 ${VAR} 都直接报错、点名是哪个变量——
// 跟 local-debug.*.env 文件"展开不了就留着占位符"的宽松策略不同：这是真的
// 要喂给进程的环境，不是给人看的排障文件。
func buildLocalEnv(
	ref resolver.Ref, vars []inject.Var, port int, cmdEnv []string, lookup func(string) (string, bool),
) ([]string, error) {
	out := make([]string, 0, len(vars)+len(cmdEnv)+1)
	for _, v := range vars {
		if v.ExistingSecretRef != "" {
			// existingSecret 引用的是 K8s Secret；mode: local 跟 mode: debug 一样
			// docker-only，这条引用在这两种模式下都没有对应的值（005 §5.6）。
			continue
		}
		value, missing := expandStrict(v.Value, lookup)
		if missing != "" {
			return nil, missingEnvVarError(ref, v.Name, missing)
		}
		out = append(out, v.Name+"="+value)
	}
	out = append(out, cmdEnv...)
	out = append(out, fmt.Sprintf("PORT=%d", port))
	return out, nil
}

// expandStrict 展开 raw 里的 ${VAR}；第一个展开不了的变量名通过 missing 返回。
func expandStrict(raw string, lookup func(string) (string, bool)) (value, missing string) {
	if lookup == nil || !strings.Contains(raw, "${") {
		return raw, ""
	}
	var firstMissing string
	expanded := localEnvVarRe.ReplaceAllStringFunc(raw, func(match string) string {
		name := match[2 : len(match)-1]
		if v, ok := lookup(name); ok {
			return v
		}
		if firstMissing == "" {
			firstMissing = name
		}
		return match
	})
	return expanded, firstMissing
}

func missingEnvVarError(ref resolver.Ref, envVarName, missingVarName string) error {
	return clierr.New(clierr.CodeConfigInvalid,
		i18n.T(msgid.CliUpMissingEnvVarFor, ref.String(), missingVarName, envVarName))
}

// renderLocalComponentCommands 在 --dry-run 下告诉使用者每个 mode: local
// 组件探测出的启动命令是什么——"看看会发生什么"这条命令的整个意义所在，
// 本地组件不该是唯一说不清楚的部分。
func renderLocalComponentCommands(opts *Options, plans []localComponentPlan) {
	if len(plans) == 0 {
		return
	}
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpLocalComponentsWouldStart))
	for _, p := range plans {
		opts.Printf("   %s  %s\n", p.Ref, strings.Join(p.Command.Argv, " "))
	}
}

// startupProbeTimeout 是等一个本地组件监听期望端口的上限——不是健康检查，
// 只是"命令跑起来了没有"的一次性检测（procsup 包文档）。
const startupProbeTimeout = 30 * time.Second

// runLocalComponents 拿会话锁，按拓扑序拉起每个本地组件，阻塞到全部退出、
// 某个进程崩溃触发整个会话收尾、或者用户按了 Ctrl+C。
//
// 顺序上，这一步永远在 eng.Up() 之后调用（设计决定第 5 条）：容器已经在跑，
// 本地进程依赖的容器地址已经可用，不需要把容器和本地进程交织进同一个
// 拓扑序循环里。
func runLocalComponents(
	ctx context.Context, opts *Options, layout config.Layout,
	plans []localComponentPlan, crashLines int,
) error {
	if len(plans) == 0 {
		return nil
	}

	lock, err := sessionlock.Acquire(layout.SessionLockPath())
	if err != nil {
		var held *sessionlock.HeldError
		if errors.As(err, &held) {
			return clierr.New(clierr.CodeConfigInvalid,
				i18n.T(msgid.CliUpSessionAlreadyRunning, held.Info.PID))
		}
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToAcquireTheSession)).WithCause(err)
	}
	defer func() { _ = lock.Release() }()

	ctx, stop := signal.NotifyContext(ctx, procsup.StopSignals...)
	defer stop()

	widest := 0
	for _, p := range plans {
		if len(p.Service) > widest {
			widest = len(p.Service)
		}
	}
	sup, err := procsup.New(procsup.Options{Out: opts.Stdout, NameWidth: widest, TailLines: crashLines})
	if err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToStartTheLocal)).WithCause(err)
	}
	defer sup.Shutdown()

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpStartingLocalComponents, len(plans)))

	for _, p := range plans {
		proc, err := sup.Start(procsup.Spec{Name: p.Service, Argv: p.Command.Argv, Dir: p.Dir, Env: p.Env})
		if errors.Is(err, procsup.ErrStopped) {
			break // 已经有别的进程崩了，会话在收尾：别再启动新的
		}
		if err != nil {
			return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToStartTheLocal)).
				WithDetail(i18n.T(msgid.LabelComponent), p.Service).WithCause(err)
		}

		// 组件的身份统一用 p.Service（版本化服务名）——procsup 给这个进程自己
		// 输出加的行前缀（Spec.Name 就是它）、容器组件在 reportStarted 里的
		// 汇报，用的都是这同一个名字，这里的状态行不该另起一套 ID@version。
		switch procsup.WaitListening(ctx, p.Port, proc.Done(), startupProbeTimeout) {
		case procsup.ProbeExited:
			// 进程自己没起来，是硬失败；也可能是别的进程崩了、这个是被叫停的——
			// 到 Run 返回之后统一从 Exits() 里看谁 Crashed()，这里不重复判断
		case procsup.ProbeTimedOut:
			opts.Printf("%s\n", i18n.T(msgid.CliUpWarningStillNotListeningOn, p.Service, p.Port))
		case procsup.ProbeCanceled:
			return renderCrashSummary(opts, sup.Exits(), crashLines)
		case procsup.ProbeListening:
			opts.Printf("%s\n", i18n.T(msgid.CliUpListeningOnPort, p.Service, p.Port))
		}
	}

	sup.Run(ctx)

	return renderCrashSummary(opts, sup.Exits(), crashLines)
}

// renderCrashSummary 在最后一屏只打印崩溃的那几个进程，不被收尾时其余进程
// 的输出冲走（Plan 2 的设计决定 1）。被叫停的、干净退出的都不打印。
//
// crashLines 是使用者原始传的 --crash-lines 值，跟 procsup.Exit.Tail 里
// 实际捕获了多少行是两回事：procsup.Options.TailLines 把 <= 0 当成"调用方
// 没配，给个够用的默认值"（它自己的 TestTailKeepsOnlyTheConfiguredNumberOfLines
// 已经锁死了这条约定，是它作为通用监管库的合理默认，不该为了这一个调用方改）；
// 而 --crash-lines 的帮助文本对使用者的承诺是"0 = 只打印崩溃信息，不带
// 输出行"——同一个 0，两层意思完全相反。这条 CLI 专属的承诺只能在 CLI 自己
// 这层兑现：crashLines <= 0 时，不管 procsup 内部实际捕获、塞进 Exit.Tail
// 的是默认的 20 行还是别的，这里都不打印任何一行（手动验证 Task 6 Step 5
// 时用真实进程试 --crash-lines 0 才发现两层语义对不上）。
func renderCrashSummary(opts *Options, exits []procsup.Exit, crashLines int) error {
	var crashed []procsup.Exit
	for _, e := range exits {
		if e.Crashed() {
			crashed = append(crashed, e)
		}
	}
	if len(crashed) == 0 {
		return nil
	}

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpTheFollowingLocalComponentsCrashed))
	for _, e := range crashed {
		how := i18n.T(msgid.CliUpExitCode, e.Code)
		if e.Signal != "" {
			how = i18n.T(msgid.CliUpKilledBySignal, e.Signal)
		}
		opts.Printf("   %s  %s  %s\n", e.Name, how, e.Duration.Round(time.Second))
		if crashLines <= 0 {
			continue
		}
		for _, line := range e.Tail {
			opts.Printf("      %s\n", line)
		}
	}
	return clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.CliUpLocalComponentsCrashed, len(crashed)))
}
