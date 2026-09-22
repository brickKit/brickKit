// Package runcmd 回答一个问题：这个组件的源码目录，在本机上怎么启动？
//
// 它读组件源码目录里各语言生态自己的标准约定文件（go.mod、package.json、pom.xml……），
// 产出一条启动命令。它不启动进程（那是 procsup 的事），也不读 component.yaml /
// brickkit.yaml——调用方把 `local:` 块里的 language / runCommand 翻译成 Hints 传进来。
//
// 三条原则：
//   - 只认廉价而确定的信号。认不准的语言宁可报错、让用户手写 runCommand：猜错了还能跑起来，
//     比直接报错更难排查。所以 Python、Ruby 刻意收窄到 manage.py / bin/rails 这类强信号。
//   - 两种语言都认得出可运行的命令，就报歧义，绝不悄悄挑一个。
//   - 只产出普通的启动命令，不注入任何调试参数。JAVA_TOOL_OPTIONS / NODE_OPTIONS 会被
//     mvnw、gradlew、npm 这些包装进程自己先吃掉，真正的服务进程反而起不来或者挂不上调试器
//     （实测记录见 docs/superpowers/plans/2026-09-22-run-command-detection.md）。
package runcmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// 支持的语言标识，也是 component.yaml `local.language` 的取值。
const (
	LangGo     = "go"
	LangRust   = "rust"
	LangDotnet = "dotnet"
	LangNode   = "node"
	LangJava   = "java"
	LangPython = "python"
	LangRuby   = "ruby"
)

// Hints 是调用方手里已有的信息：component.yaml 的 `local:` 块。
type Hints struct {
	// Language 非空时只让这一种语言的适配器探测（消除跨语言歧义）；必须是 Languages() 里的一个。
	Language string
	// RunCommand 非空时不做任何探测，原样使用（Argv[0] 含路径分隔符时相对组件目录）。
	RunCommand []string
}

// Params 是本次启动的运行参数。
type Params struct {
	// Port 是这个进程必须监听的主端口（1–65535）。它对 Go / Node / Rust / .NET 没有影响——
	// 那些组件靠自己读 PORT 环境变量；对 Django、Rails、Spring Boot 则会翻译成框架自己的写法。
	Port int
}

// Command 是探测的结果，字段与 procsup.Spec 一一对应（Name 由调用方补）。
type Command struct {
	// Language 是语言标识；用户手写了 RunCommand 又没写 language 时为空。
	Language string
	// Argv[0] 是可执行文件：裸名字按 PATH 查找，绝对路径原样使用。不经过 shell。
	Argv []string
	// Env 是 "KEY=VALUE" 列表，追加在当前进程环境之后（例如 PYTHONUNBUFFERED=1）。
	Env []string
	// Dir 是工作目录，即组件源码目录的绝对路径。
	Dir string
	// Evidence 是探测依据（文件名等），给调用方展示"为什么是这条命令"；手写的命令没有。
	Evidence []string
	// Detected 为 true 表示命令是探测出来的，false 表示来自 Hints.RunCommand。
	Detected bool
}

type adapter struct {
	language string
	// marker 是这种语言的标记文件，放进"缺标记"的问题里给用户看。
	marker string
	probe  func(s *scan) outcome
	// env 产出这种语言额外需要的环境变量；nil 表示没有。
	env func(s *scan) []string
}

// 顺序固定：它既是 Languages() 的顺序，也是歧义报错里候选的顺序。
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
}

// Languages 返回全部支持的语言标识（固定顺序）。
func Languages() []string {
	out := make([]string, len(adapters))
	for i, a := range adapters {
		out[i] = a.language
	}
	return out
}

func adapterFor(language string) (adapter, bool) {
	for _, a := range adapters {
		if a.language == language {
			return a, true
		}
	}
	return adapter{}, false
}

// Detect 在组件源码目录 dir 里确定启动命令。
//
// 错误全是本包的类型（可用 errors.As 区分），调用方据此在 CLI 层给出面向用户的文字：
// *UnknownLanguageError、*NoCommandError、*AmbiguousError；目录本身读不了则是普通的 error。
func Detect(dir string, hints Hints, params Params) (Command, error) {
	return detect(dir, hints, params, runtime.GOOS)
}

// detect 比 Detect 多一个 goos，让测试在 Linux 上也能核对 Windows 那一列命令。
func detect(dir string, hints Hints, params Params, goos string) (Command, error) {
	if params.Port < 1 || params.Port > 65535 {
		return Command{}, fmt.Errorf("runcmd: port %d is out of range 1-65535", params.Port)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Command{}, fmt.Errorf("runcmd: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Command{}, fmt.Errorf("runcmd: %w", err)
	}
	if !info.IsDir() {
		return Command{}, fmt.Errorf("runcmd: %s is not a directory", abs)
	}
	// 目录本身读不了就直接报错。否则里面每个标记文件的读取都会以权限错误失败，
	// 被误报成"package.json 读不了""pom.xml 读不了"，而它们其实根本不存在。
	entries, err := os.ReadDir(abs)
	if err != nil {
		return Command{}, fmt.Errorf("runcmd: %w", err)
	}
	s := &scan{dir: abs, goos: goos, params: params, entries: entries}

	candidates := adapters
	var chosen *adapter
	if hints.Language != "" {
		a, ok := adapterFor(hints.Language)
		if !ok {
			return Command{}, &UnknownLanguageError{Language: hints.Language}
		}
		chosen = &a
		candidates = []adapter{a}
	}

	if len(hints.RunCommand) > 0 {
		return s.handWritten(hints, chosen), nil
	}

	var found []Command
	var problems []Problem
	for _, a := range candidates {
		o := a.probe(s)
		switch {
		case o.ok:
			o.cmd.Language = a.language
			if a.env != nil {
				o.cmd.Env = a.env(s)
			}
			found = append(found, o.cmd)
		case o.problem != nil:
			o.problem.Language = a.language
			problems = append(problems, *o.problem)
		}
	}

	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		if chosen != nil && len(problems) == 0 {
			problems = []Problem{{Language: chosen.language, Reason: ReasonMarkerMissing, Detail: chosen.marker}}
		}
		return Command{}, &NoCommandError{Dir: abs, Problems: problems}
	default:
		out := make([]Candidate, len(found))
		for i, c := range found {
			out[i] = Candidate{Language: c.Language, Argv: c.Argv}
		}
		return Command{}, &AmbiguousError{Candidates: out}
	}
}

// handWritten 处理用户自己写了 runCommand 的情况：不探测，只把相对路径的程序落到组件目录里，
// 并在用户写明了 language 时带上那种语言需要的环境变量。
func (s *scan) handWritten(hints Hints, a *adapter) Command {
	argv := append([]string(nil), hints.RunCommand...)
	// 相对路径统一在这里落成绝对路径，不指望 exec 去解析：Unix 上它相对 cmd.Dir，
	// Windows 上却相对父进程的当前目录。
	if strings.ContainsAny(argv[0], "/"+string(filepath.Separator)) && !filepath.IsAbs(argv[0]) {
		argv[0] = filepath.Join(s.dir, argv[0])
	}
	cmd := Command{Language: hints.Language, Argv: argv, Dir: s.dir}
	if a != nil && a.env != nil {
		cmd.Env = a.env(s)
	}
	return cmd
}

// CheckProgram 确认 Argv[0] 真的存在，让"这台机器没装 go / npm"在启动之前就得到一个点名的错误，
// 而不是 exec 的一句 "executable file not found in $PATH"。
func (c Command) CheckProgram() error {
	if len(c.Argv) == 0 {
		return &ProgramMissingError{}
	}
	program := c.Argv[0]
	if filepath.IsAbs(program) {
		if info, err := os.Stat(program); err != nil || info.IsDir() {
			return &ProgramMissingError{Program: program}
		}
		return nil
	}
	if _, err := exec.LookPath(program); err != nil {
		return &ProgramMissingError{Program: program}
	}
	return nil
}
