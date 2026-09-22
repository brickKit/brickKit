package cli

import (
	"fmt"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/runcmd"
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
	for _, f := range localEnvFiles {
		portOf[f.Ref] = f.Port
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

		out = append(out, localComponentPlan{
			Ref: ref, Service: step.Service, Dir: dir, Command: cmd, Port: port,
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
