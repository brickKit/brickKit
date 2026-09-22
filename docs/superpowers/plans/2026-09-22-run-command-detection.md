# 启动命令自动识别（`internal/runcmd`）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增一个**先不接线**的内部包 `internal/runcmd`——给定一个组件源码目录，读它自己语言生态的标准约定文件（`go.mod`、`package.json`、`pom.xml`……），产出一条能在本机跑起来的启动命令。它不启动进程（那是 `procsup` 的事，见 Plan 2），也不读 `component.yaml`/`brickkit.yaml`（那是 Plan 4 的事）——调用方把 `component.yaml` 将来的 `local:` 块翻译成 `Hints` 传进来。支持 Go、Rust、.NET、Node、Java（Spring Boot）、Python（Django）、Ruby（Rails）七种语言。

**Architecture:** 一个"适配器表"，每种语言一个探测函数，只通过一个共享的 `scan` 类型读文件系统（一次 `ReadDir`，多次复用）。探测规则遵循三条原则：只认廉价而确定的信号（标记文件本身，不猜测框架）；认不准的语言宁可报错也不猜（Python/Ruby 刻意收窄到 Django/Rails 这类强信号，其余交给用户手写 `runCommand`）；两种语言都能跑就报歧义，绝不悄悄选一个。产出的命令只是"平时怎么跑"，不包含任何调试挂载参数——原因见下面"设计决定"第 1 条，这是本计划期间用真实进程做实验才发现的一处设计分歧，因此没有像 spec §4 原计划的那样把调试参数编码进各语言适配器。

**Tech Stack:** Go 1.22 标准库（`go/build` 判断 Go 包是不是可执行的 `main`、`encoding/json` 解析 `package.json`、`os/exec.LookPath` 校验程序是否存在）；测试用 testify。**不新增任何第三方依赖**——`slices` 包是标准库（Go 1.21 起），不是 `golang.org/x/exp`。

**Spec:** `docs/superpowers/specs/2026-09-21-mode-field-and-local-execution-design.md` 的 §2（字段归属：`local:` 块落在 `component.yaml`，本计划不做这部分，见"这份计划在四份里的位置"）、§4（启动命令怎么来）。

## Global Constraints

- Go 版本下限 **1.22**（`go.mod`），本计划**不修改 `go.mod`**——用到的都是标准库。
- **验证范围**：只在 Linux 上实际验证；Windows 侧的命令（`mvnw.cmd`、`.venv\Scripts\python.exe` 等）用 `goos` 参数在 Linux 上做条件测试（生产代码走 `runtime.GOOS`），不强求真机跑通，跟 Plan 2 的验证范围一致（spec §6.5）。落地前先跑 `make check-cross-build`（Plan 2 已经建好，这次不用新建）确认 windows/darwin/linux 都编得过、测试文件也过 `vet`。
- 生产代码里不许写死带中文的字符串字面量（`tests/i18nguard`）。本包的 `Error()` 文字全部是英文、用 `%w`/`%q` 包着字段——它们是给开发者看的诊断信息，不是面向用户的文案；等 Plan 4 在 CLI 层读这些结构化字段（`Problem.Reason`、`Problem.Detail`……）用 `i18n.T(msgid.X)` 组出中英文提示。注释可以写中文（仓库惯例）。
- Argv **不经过 shell**：`os/exec` 直接执行 `Argv[0]`，没有 shell 展开、没有管道、没有 `&&`。这也是为什么 Node 的 `package.json.scripts.start` 只按"哪个包管理器 + 哪个脚本名"识别，不解析脚本内容本身——脚本内容可能是任意 shell 语法，交给 `npm run` 自己去解释。
- 仓库惯例：`defer func() { _ = x.Close() }()`；测试与被测代码同包；测试名用英文、注释用中文；提交信息用中文；提交前跑**完整**的 `make lint`（只跑 `go vet` + `go test` 会漏 golangci-lint 的 errcheck 之类）；`go test -race` 必须干净。
- **不接线**：本计划不改 `component.yaml`/`brickkit.yaml` 的字段、不新增 `local:` 块、不改任何 CLI 命令。因此**不改 `AGENTS.md` 与任何 `docs/`**——文档里不能先出现一个还不存在的字段。Manifest 的 `local:` 块（`language`/`runCommand`/`debugCommand`）连同它的解析、校验、JSON Schema，留给 Plan 4——它需要同时决定 `mode: local` 怎么读这个块、字段名对不对得上、`debugCommand` 到底怎么用（见本计划"设计决定"第 1 条的遗留问题），揉在一起比现在单独加一个没人读的 Manifest 字段更连贯。
- `docs/{en,zh}/07-patterns/05-deployment-selection-guide.md` 本次不改（另一个项目会先做完整实操测试，根据反馈再更新）。
- 判断退出码：这台机器是 zsh，`${PIPESTATUS[0]}` 不存在，`make lint | grep` 会把失败看成成功。写成 `make lint > /tmp/lint.log 2>&1; echo "exit=$?"` 再 `tail /tmp/lint.log`。
- 提交命令直接从 `git` 开始，别加 `cd` 前缀（本会话就在仓库根目录）。

---

## 这份计划在四份里的位置

| 计划 | 内容 | 状态 |
| --- | --- | --- |
| Plan 1 | `enabled` + `local` 合并成 `mode` 字段（`mode: debug` 取代 `local: true`） | 已完成，提交在 `worktree-mode-field-migration` 分支 |
| Plan 2 | 本地进程监管：`procsup`、`sessionlock`、交叉编译守卫 | 已完成，提交在 `worktree-mode-field-migration` 分支 |
| **Plan 3（本计划）** | 语言/启动命令自动识别：`internal/runcmd`，七个语言适配器 | 待执行 |
| Plan 4 | `mode: local` 整合：`component.yaml` 的 `local:` 块（字段、解析、校验、Schema）、调试挂载怎么接、`up` 前台监管模式、端口分配与覆盖、`PORT` 保留变量、`graph`/`status`/`down` 展示、`check-guides` 的 `local` 层、文档 | 待写 |

本计划交付的是"Plan 4 可以直接调用的一个库"。`component.yaml` 的 `local:` 块解析**不在这里做**——那需要同时定下 `language`/`runCommand`/`debugCommand` 在 Manifest 里的确切字段名、JSON Schema、`brickkit lint` 校验规则，跟"`mode: local` 怎么用这些信息"是同一个决定，揉在 Plan 4 里更连贯（本计划的 `Hints` 类型就是这条边界：Plan 4 从 `local:` 块转译出 `Hints`，本包不关心那个块长什么样）。

## 设计决定（spec 没写死、或本计划执行期间用实验推翻了 spec 假设的地方）

1. **不做调试挂载，`runcmd` 只产出"平时怎么跑"的命令。** spec §4 原本假设 Java/Node 用环境变量注入（`JAVA_TOOL_OPTIONS`/`NODE_OPTIONS`）就能让调试器挂上目标进程。写这份计划之前，我用真实的 Node 和 Java 进程做了两个实验，结果推翻了这个假设：
   - **Node**：`npm run start` 本身是一个 Node 进程（npm 的 CLI 就是用 Node 写的），`NODE_OPTIONS=--inspect=...` 会被父子两层 Node 进程同时读到。实测：npm 自己的进程抢到了调试端口并打出 `Debugger listening on ws://...`（调试器接上的其实是 npm 的初始化代码，不是业务代码），真正的 `node server.js` 子进程随后打印 `Starting inspector on ... failed: address already in use`，服务本身没死（继续正常监听业务端口），但**调试会话连的是错误的进程**，比"报错"更误导人。
   - **Java（Maven/Gradle wrapper）**：`mvn spring-boot:run`/`gradlew bootRun` 本身也是一个 JVM 进程，它会再 fork 一个新的 JVM 去跑真正的应用（跟 Node 的情况是同一种"外层进程也是同一运行时"的结构）。实测：外层 JVM 与被 fork 出来的应用 JVM 都读到了 `JAVA_TOOL_OPTIONS` 里的 JDWP 参数、都想绑同一个端口——外层先绑上，应用 JVM 因为 `Address already in use` **直接崩溃退出（退出码 2）**，这比 Node 的情况更糟：应用根本起不来。
   - 真正安全的修法是"只让最终跑业务代码的那一个进程拿到调试参数"，但这对 Maven 得用插件专属参数（`-Dspring-boot.run.jvmArguments="-agentlib:jdwp=..."`，而不是通用的 `JAVA_TOOL_OPTIONS`）、对 Gradle 得用 `--debug-jvm`（默认端口 5005、`suspend=y`，不易改成用户指定端口）、对 Node 目前没有不改探测策略就能做到的安全办法。这已经是"给每个构建工具单独编码它自己的调试挂载语法"，跟 `runcmd` 当前"只读标记文件、不理解语言内部机制"的定位不是一回事，而是启动 + 挂调试器这个更大的运行时问题的一部分。
   - **决定**：`runcmd` 不产出任何调试专属的 Argv/Env，`Command` 没有"调试变体"。Go 不受影响（spec 本来就是"不改启动方式，跑起来后 `dlv attach <pid>`"，这是 Plan 4 在进程起来之后单独做的一个动作，不需要 `runcmd` 参与）。Python 也不受影响（`python -m debugpy --listen ...` 是包一层解释器命令，不是环境变量，天然没有"外层进程也读到同一个环境变量"的问题）——但这次没有实现它，因为 spec 把它跟 Java/Node 一起归在"调试挂载"底下，而这一条决定是"`runcmd` 不做调试挂载"，不是"`runcmd` 只做安全的那几种"；一次性交给 Plan 4 作为一个整体的"怎么给一个已探测出的普通命令，安全地换出一个可调试版本"的设计问题，跟 `component.yaml` 的 `debugCommand` 字段到底解决哪一部分一起想清楚，比现在只做 Python 一种、说不清 Java/Node 什么时候补更连贯。
2. **目录读不了是一整个错误，不是每种语言各报一条"清单文件读不了"。** 最早的实现是每个适配器自己 `os.ReadFile`，目录本身没有读权限时，七个适配器会各自把"permission denied"包装成"这种语言的标记文件读不了"，产出一份具有误导性的、把根本不存在的语言也点名的错误列表。改成 `Detect` 入口先 `os.ReadDir` 一次、读不了就直接返回这个错误，各适配器共享同一份目录条目——顺带也让 `namesWithExt`（.NET 用）不用每次探测都重新扫一遍目录。
3. **Python/Django、Ruby/Rails 的启动命令必须显式绑 `0.0.0.0`，不能用框架默认值。** Django `runserver`、Rails `bin/rails server` 默认都只监听 `127.0.0.1`。`mode: local` 的裸进程被其他组件通过 Docker 的 `extra_hosts` → `host-gateway` 访问（跟 `mode: debug` 共用同一套寻址，spec §3 最后一条），这条路径走的不是回环地址；只监听回环地址的进程对访问它的容器是不可达的。这不是"更保险"的选择，是唯一能工作的选择，所以两个适配器都不把它做成可选项。
4. **Python 子进程必须带 `PYTHONUNBUFFERED=1`。** procsup（Plan 2）把子进程的 stdout/stderr 接到管道而不是终端（为了能按行加前缀合并多个进程的输出）。实测：Python 默认在检测到 stdout 不是 tty 时切换成整块缓冲——同一段"打印一行、睡 2 秒、再打印一行"的程序，默认情况下两行会在 2 秒后同时冒出来；带上 `PYTHONUNBUFFERED=1` 才会在真实的时间点逐行输出。这条已经写进 Plan 2 的设计决定第 2 条（"库不替语言擦屁股，这是语言适配器的事"），这里是那个承诺的落地。
5. **Go 判断"是不是可执行的 `main` 包"用标准库 `go/build`，不 shell 出去跑 `go list`。** `go/build.ImportDir` 能正确处理构建约束（`//go:build`）、区分 `_test.go`、识别 cgo 文件，而且不需要这台机器装了 `go` 命令——`runcmd.Detect` 本身在探测阶段完全不依赖任何外部工具链，只有 `Command.CheckProgram()`（调用方决定要不要校验时才调用）才会去 `exec.LookPath`。
6. **Node 的包管理器选择：`packageManager` 字段 > 锁文件 > 默认 `npm`；两把不同管理器的锁文件同时存在才算冲突。** `packageManager`（corepack 的标准约定，`"pnpm@9.1.0"` 这种形式）比锁文件更明确地表达了作者的意图，优先级最高。只看一把锁文件肯定没有歧义；两把同一管理器的锁文件（比如 `package-lock.json` 和 `npm-shrinkwrap.json` 都是 npm 自己的）不算冲突；真正的冲突只在"认出了不止一种包管理器"时报。
7. **Java 只认 Spring Boot，判断信号是"构建插件在不在"而不是"依赖里有没有 `spring-boot-starter`"。** Java 没有类似 `go run .`/`cargo run` 的通用运行命令，唯一有标准答案的运行方式来自 Spring Boot 自己的 Maven 插件（`spring-boot:run`）和 Gradle 插件（`bootRun`）——这两个命令只有插件真的配置了才存在，只有依赖没有插件时命令会直接报"没有这个目标"。多模块工程（Maven 的 `<modules>`、Gradle 的 `settings.gradle` 里 `include`）在根目录探测不出"那一个"应用，判多模块要先于判插件（否则聚合工程会被误报成"没配插件"，而真正的原因是插件在子模块里）。`gradlew`/`mvnw` wrapper 优先于全局命令（版本由项目锁定），wrapper 丢了可执行位（Windows 上 clone、解压 zip 都会）时退化成 `sh <wrapper>` 而不是让用户撞上一句 `permission denied`。
8. **Rust/`.NET` 只挡"明确跑不了"的情形，不试图挑出"最可能对的那个"。** Rust 只挡纯虚拟工作区（`[workspace]` 没有 `[package]`）——一个包里有多个二进制目标时，`cargo run` 自己会报出"could not determine which binary to run"外加候选列表，这句话会随崩溃的最后几行输出（procsup 的 `Tail`，见 Plan 2）一起交给用户，不需要在探测阶段重新实现一遍这套逻辑。.NET 只有恰好一个项目文件（`.csproj`/`.fsproj`/`.vbproj`）才认，多个或者只有解决方案文件（项目在子目录里）都交给用户用 `runCommand` 明说。
9. **手写的 `runCommand` 仍然会拿到对应语言的环境变量。** `Hints{Language: "python", RunCommand: [...]}` 探测阶段完全跳过（不读任何标记文件），但因为用户显式写明了 `language`，`PYTHONUNBUFFERED=1`/`SERVER_PORT=<port>` 这类"这种语言在这套运行方式下必须有"的环境变量仍然会带上——这些变量描述的是"输出走管道"和"平台分配了哪个端口"这两个运行时事实，跟命令是探测出来的还是手写的无关。没写 `language`（纯手写命令、不声明语言）时不猜，`Env` 为空。

## File Structure

- Create: `internal/runcmd/runcmd.go`——包文档、`Hints`/`Params`/`Command` 类型、语言表、`Detect`/`detect`、`CheckProgram`
- Create: `internal/runcmd/errors.go`——`Reason`、`Problem`、`NoCommandError`、`AmbiguousError`、`UnknownLanguageError`、`ProgramMissingError`
- Create: `internal/runcmd/scan.go`——`scan`/`outcome` 类型、文件系统读取的共享帮助函数、`wrapper`（Maven/Gradle wrapper 脚本选择）
- Create: `internal/runcmd/lang_go.go`、`lang_rust.go`、`lang_dotnet.go`、`lang_node.go`、`lang_java.go`、`lang_python.go`、`lang_ruby.go`——七个语言适配器
- Create: 对应的 `*_test.go`，外加 `helper_test.go`（测试固件帮助函数）、`integration_test.go`（真实工具链 + 仓库自己的 `tests/components/` 固件）
- Modify: `docs/superpowers/plans/2026-09-22-run-command-detection.md`（本文件）——收尾时回写执行结果

---

## Task 1: 驱动骨架 + Go 适配器（跑通整条链路的第一个语言）

**背景：** 先把"没有任何语言能识别"“识别出恰好一种”“识别出不止一种（歧义）”“用户手写了 `runCommand`”这几条主干逻辑，配上第一个真正的语言适配器（Go，规则最简单：有没有 `go.mod`、根目录或 `cmd/*` 底下是不是恰好一个 `main` 包）一次做完、跑通。后面每个任务只新增一个适配器文件 + 它的测试，不再碰驱动代码。

**Files:**
- Create: `internal/runcmd/runcmd.go`
- Create: `internal/runcmd/errors.go`
- Create: `internal/runcmd/scan.go`
- Create: `internal/runcmd/lang_go.go`
- Create: `internal/runcmd/helper_test.go`
- Create: `internal/runcmd/runcmd_test.go`
- Create: `internal/runcmd/lang_go_test.go`

**Interfaces:**
- Produces：`Hints{Language, RunCommand}`、`Params{Port}`、`Command{Language, Argv, Env, Dir, Evidence, Detected}`、`func Detect(dir string, hints Hints, params Params) (Command, error)`、`func (c Command) CheckProgram() error`、`func Languages() []string`——后续每个任务的适配器都通过在 `runcmd.go` 的 `adapters` 表里追加一行来接入，不改 `Detect` 本身。
- Consumes：无（这是最底层的任务）。

- [ ] **Step 1: 写 `runcmd.go`**

```go
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
```

- [ ] **Step 2: 写 `errors.go`**

```go
package runcmd

import (
	"fmt"
	"strings"
)

// 本文件的 Error() 文字是给开发者看的英文；面向用户的文字由调用方（CLI 层）按
// 这些结构化字段用消息目录生成，才能跟着 BRICKKIT_LANG 走。

// Reason 说明"认得这种语言、却给不出启动命令"的原因，是给调用方映射成消息用的稳定标识。
type Reason string

const (
	// ReasonMarkerMissing：用户指定了 language，但目录里没有这种语言的标记文件。
	ReasonMarkerMissing Reason = "marker-missing"
	// ReasonUnreadableManifest：标记文件在，却读不了或解析不了（package.json 不是合法 JSON 等）。
	ReasonUnreadableManifest Reason = "unreadable-manifest"
	// ReasonNoEntryPoint：找不到可执行入口（Go 没有 main 包、.NET 只有解决方案文件、Rust 是虚拟工作区）。
	ReasonNoEntryPoint Reason = "no-entry-point"
	// ReasonMultipleEntryPoints：有多个可执行入口，选哪个是用户的事（Go 的多个 cmd/*、.NET 的多个项目文件）。
	ReasonMultipleEntryPoints Reason = "multiple-entry-points"
	// ReasonNoStartScript：package.json 里既没有 start 也没有 dev 脚本。
	ReasonNoStartScript Reason = "no-start-script"
	// ReasonConflictingPackageManagers：同时有不同包管理器的锁文件，又没有 packageManager 字段裁决。
	ReasonConflictingPackageManagers Reason = "conflicting-package-managers"
	// ReasonUnsupportedPackageManager：packageManager 字段写的不是 npm / pnpm / yarn。
	ReasonUnsupportedPackageManager Reason = "unsupported-package-manager"
	// ReasonNotSpringBoot：Java 项目没有 Spring Boot 的构建插件，没有通用的"运行"命令可用。
	ReasonNotSpringBoot Reason = "not-spring-boot"
	// ReasonMultiModule：Maven / Gradle 多模块工程，在根目录跑不出"那一个"应用。
	ReasonMultiModule Reason = "multi-module"
	// ReasonConflictingBuildTools：Maven 与 Gradle 的构建文件同时存在。
	ReasonConflictingBuildTools Reason = "conflicting-build-tools"
)

// Problem 是某一种语言"认得出、但给不出命令"的一条记录。
type Problem struct {
	Language string
	Reason   Reason
	// Detail 是出问题的文件、目录或字段（"package.json"、"cmd"、"bun@1.1.0"……），没有就为空。
	Detail string
	// Options 是候选项（多个 cmd/*、冲突的锁文件……），没有则为空。
	Options []string
}

func (p Problem) String() string {
	s := p.Language + ": " + string(p.Reason)
	if p.Detail != "" {
		s += " (" + p.Detail + ")"
	}
	if len(p.Options) > 0 {
		s += " [" + strings.Join(p.Options, ", ") + "]"
	}
	return s
}

// NoCommandError：没有任何一种语言给出了启动命令。Problems 列出"认得出但给不出"的语言，
// 为空表示目录里根本没有认得的标记文件。两种情况调用方给出的提示相同：手写 local.runCommand。
type NoCommandError struct {
	Dir      string
	Problems []Problem
}

func (e *NoCommandError) Error() string {
	if len(e.Problems) == 0 {
		return fmt.Sprintf("runcmd: no start command for %s: no supported language recognised (%s)",
			e.Dir, strings.Join(Languages(), ", "))
	}
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.String()
	}
	return fmt.Sprintf("runcmd: no start command for %s: %s", e.Dir, strings.Join(parts, "; "))
}

// Candidate 是歧义报错里的一个候选。
type Candidate struct {
	Language string
	Argv     []string
}

// AmbiguousError：不止一种语言给出了可运行的命令。调用方应让用户在 local.language 里选一个。
type AmbiguousError struct {
	Candidates []Candidate
}

func (e *AmbiguousError) Error() string {
	parts := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		parts[i] = c.Language + " (" + strings.Join(c.Argv, " ") + ")"
	}
	return "runcmd: more than one language can start this component: " + strings.Join(parts, ", ")
}

// UnknownLanguageError：Hints.Language 不在 Languages() 里。
type UnknownLanguageError struct {
	Language string
}

func (e *UnknownLanguageError) Error() string {
	return fmt.Sprintf("runcmd: unknown language %q (supported: %s)", e.Language, strings.Join(Languages(), ", "))
}

// ProgramMissingError：命令的可执行文件不存在（通常是这台机器没装对应的工具链）。
type ProgramMissingError struct {
	Program string
}

func (e *ProgramMissingError) Error() string {
	return fmt.Sprintf("runcmd: program %q not found", e.Program)
}
```

- [ ] **Step 3: 写 `scan.go`**

```go
package runcmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// scan 是一次探测的上下文：要看的目录、目标操作系统、运行参数。
// 适配器只通过它读文件系统，所以"Windows 那一列命令"能在 Linux 上靠 goos 字段单独测。
type scan struct {
	dir    string
	goos   string
	params Params
	// entries 是目录根下的条目，探测入口读一次，之后按扩展名找文件都用它。
	entries []os.DirEntry
}

// outcome 是一个适配器的探测结果，三种互斥的情形：
//   - ok：给出了命令；
//   - problem 非空：认得出这种语言（标记文件在），但给不出命令；
//   - 两者皆空：目录里没有这种语言。
type outcome struct {
	cmd     Command
	ok      bool
	problem *Problem
}

func (s *scan) windows() bool { return s.goos == "windows" }

func (s *scan) port() string { return strconv.Itoa(s.params.Port) }

func (s *scan) path(rel string) string { return filepath.Join(s.dir, filepath.FromSlash(rel)) }

func (s *scan) isFile(rel string) bool {
	info, err := os.Stat(s.path(rel))
	return err == nil && info.Mode().IsRegular()
}

func (s *scan) executable(rel string) bool {
	info, err := os.Stat(s.path(rel))
	return err == nil && info.Mode()&0o111 != 0
}

// read 读一个标记文件。present 为 false 表示文件不存在（语言不在场）；
// present 为 true 而 err 非空表示文件在、却读不了。
func (s *scan) read(rel string) (text string, present bool, err error) {
	data, err := os.ReadFile(s.path(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	return string(data), true, nil
}

// namesWithExt 列出目录根下扩展名匹配的普通文件名（已排序）。
// 不用 filepath.Glob：目录路径里若有 `[`、`*` 之类，Glob 会把它们当成通配符。
func (s *scan) namesWithExt(exts ...string) []string {
	var names []string
	for _, e := range s.entries {
		if !e.Type().IsRegular() {
			continue
		}
		for _, ext := range exts {
			if strings.EqualFold(filepath.Ext(e.Name()), ext) {
				names = append(names, e.Name())
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

// subdirs 列出 rel 下的子目录名（已排序）；rel 不存在则为空。
func (s *scan) subdirs(rel string) []string {
	entries, err := os.ReadDir(s.path(rel))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func (s *scan) ok(argv []string, evidence ...string) outcome {
	return outcome{ok: true, cmd: Command{Argv: argv, Dir: s.dir, Evidence: evidence, Detected: true}}
}

func problemOutcome(reason Reason, detail string, options ...string) outcome {
	return outcome{problem: &Problem{Reason: reason, Detail: detail, Options: options}}
}

// wrapper 选出 Maven / Gradle 的启动前缀：项目自带的 wrapper 脚本优先（版本由项目锁定），
// 没有才退回 PATH 里的全局命令。返回前缀命令，以及用到的 wrapper 文件名（没用则为空）。
//
// Unix 上 wrapper 丢了可执行位很常见（Windows 上 clone、zip 解压都会），这时改用 sh 去跑它，
// 而不是让用户撞上一句 "permission denied"。
func (s *scan) wrapper(unix, windows, global string) ([]string, string) {
	if s.windows() {
		if s.isFile(windows) {
			return []string{s.path(windows)}, windows
		}
		return []string{global}, ""
	}
	if s.isFile(unix) {
		if s.executable(unix) {
			return []string{s.path(unix)}, unix
		}
		return []string{"sh", s.path(unix)}, unix
	}
	return []string{global}, ""
}
```

- [ ] **Step 4: 写第一个适配器 `lang_go.go`**

```go
package runcmd

import "go/build"

// probeGo：有 go.mod，且根目录是 main 包 → `go run .`；
// 根目录不是 main 包时看 cmd/*，恰好一个 main 包才用它，多个就交给用户选。
// 判断"是不是 main 包"交给标准库的 go/build（它认得构建约束、_test.go、平台后缀），不需要装 go。
func probeGo(s *scan) outcome {
	if !s.isFile("go.mod") {
		return outcome{}
	}
	if isGoCommand(s.path(".")) {
		return s.ok([]string{"go", "run", "."}, "go.mod", "package main")
	}
	var mains []string
	for _, name := range s.subdirs("cmd") {
		if isGoCommand(s.path("cmd/" + name)) {
			mains = append(mains, "./cmd/"+name)
		}
	}
	switch len(mains) {
	case 0:
		return problemOutcome(ReasonNoEntryPoint, "go.mod")
	case 1:
		return s.ok([]string{"go", "run", mains[0]}, "go.mod", mains[0])
	default:
		return problemOutcome(ReasonMultipleEntryPoints, "cmd", mains...)
	}
}

// isGoCommand 判断 dir 是不是一个能 `go run` 的 main 包：包名是 main，且至少有一个非测试的源文件。
func isGoCommand(dir string) bool {
	pkg, err := build.Default.ImportDir(dir, 0)
	return err == nil && pkg.IsCommand() && len(pkg.GoFiles)+len(pkg.CgoFiles) > 0
}
```

`runcmd.go` 的 `adapters` 表此刻应该已经是（Step 1 的代码里已经写了，这里只是提醒：后续每个任务只需要在这张表里追加对应的一行，不用再改 `Detect`/`detect` 本身）：

```go
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
}
```

- [ ] **Step 5: 写测试固件帮助函数 `helper_test.go`**

（`makeExecutable` 这个帮助函数要等 Task 4 写 Java 的 wrapper 测试时才用得上，那一步再补，这一步先不写，免得先声明一个没有调用方的函数。）

```go
package runcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// 测试里统一用的端口：它出现在 Django / Rails / Spring Boot 的命令与环境变量里。
const testPort = 18080

const (
	mainGo = "package main\n\nfunc main() {}\n"
	libGo  = "package lib\n"
)

// write 在临时目录里按 名字→内容 造出一棵源码树，返回根目录。
func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

// detectAs 在指定的目标操作系统下探测（生产代码里 goos 来自 runtime.GOOS）。
func detectAs(dir, goos string, hints Hints) (Command, error) {
	return detect(dir, hints, Params{Port: testPort}, goos)
}

// mustDetect 在 Linux 下探测，期望成功。
func mustDetect(t *testing.T, dir string, hints Hints) Command {
	t.Helper()
	cmd, err := detectAs(dir, "linux", hints)
	require.NoError(t, err)
	return cmd
}

// problemsOf 期望探测以 *NoCommandError 失败，返回它的 Problems。
func problemsOf(t *testing.T, dir string, hints Hints) []Problem {
	t.Helper()
	_, err := detectAs(dir, "linux", hints)
	var nce *NoCommandError
	require.ErrorAs(t, err, &nce)
	return nce.Problems
}
```

- [ ] **Step 6: 写驱动层测试 `runcmd_test.go`**

```go
package runcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectUsesTheRealOperatingSystem(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd, err := Detect(dir, Hints{}, Params{Port: testPort})

	require.NoError(t, err)
	assert.Equal(t, []string{"go", "run", "."}, cmd.Argv)
	assert.Equal(t, dir, cmd.Dir)
}

// ---- 手写的 runCommand：不探测，原样使用 ----

func TestAHandWrittenRunCommandIsUsedAsIs(t *testing.T) {
	// 目录里即便有 go.mod，也不会去探测。
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd := mustDetect(t, dir, Hints{RunCommand: []string{"go", "run", "./cmd/server", "--flag"}})

	assert.Equal(t, []string{"go", "run", "./cmd/server", "--flag"}, cmd.Argv)
	assert.Equal(t, dir, cmd.Dir)
	assert.False(t, cmd.Detected, "手写的命令不是探测出来的")
	assert.Empty(t, cmd.Evidence)
	assert.Empty(t, cmd.Language)
	assert.Empty(t, cmd.Env)
}

func TestARelativeProgramInARunCommandIsResolvedAgainstTheComponentDirectory(t *testing.T) {
	dir := write(t, map[string]string{"scripts/dev.sh": "#!/bin/sh\n"})

	cmd := mustDetect(t, dir, Hints{RunCommand: []string{"./scripts/dev.sh", "./keep-me-relative"}})

	assert.Equal(t, []string{filepath.Join(dir, "scripts", "dev.sh"), "./keep-me-relative"}, cmd.Argv,
		"只有程序本身落成绝对路径，参数原样保留")
}

func TestABareProgramInARunCommandIsLeftForPathLookup(t *testing.T) {
	cmd := mustDetect(t, t.TempDir(), Hints{RunCommand: []string{"make", "serve"}})

	assert.Equal(t, []string{"make", "serve"}, cmd.Argv)
}

func TestAnAbsoluteProgramInARunCommandIsNotTouched(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "bin", "server")

	cmd := mustDetect(t, t.TempDir(), Hints{RunCommand: []string{abs}})

	assert.Equal(t, []string{abs}, cmd.Argv)
}

func TestTheRunCommandSliceIsCopied(t *testing.T) {
	hint := []string{"./run.sh"}
	cmd := mustDetect(t, t.TempDir(), Hints{RunCommand: hint})

	assert.Equal(t, "./run.sh", hint[0], "调用方的切片不能被改写")
	assert.NotEqual(t, "./run.sh", cmd.Argv[0])
}

func TestThePortMustBeAValidPort(t *testing.T) {
	dir := t.TempDir()
	for _, port := range []int{0, -1, 65536} {
		_, err := detect(dir, Hints{}, Params{Port: port}, "linux")
		require.Error(t, err, "port=%d", port)
		assert.Contains(t, err.Error(), "out of range")
	}
	_, err := detect(dir, Hints{RunCommand: []string{"x"}}, Params{Port: 65535}, "linux")
	assert.NoError(t, err)
}

func TestTheDirectoryMustExistAndBeADirectory(t *testing.T) {
	_, err := detectAs(filepath.Join(t.TempDir(), "missing"), "linux", Hints{})
	require.ErrorIs(t, err, os.ErrNotExist)

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, err = detectAs(file, "linux", Hints{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestADirectoryThatCannotBeReadIsAnErrorNotAPileOfMisleadingProblems(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("root 与 Windows 下 chmod 000 挡不住读取")
	}
	dir := write(t, map[string]string{"package.json": "{}", "Cargo.toml": "", "pom.xml": ""})
	require.NoError(t, os.Chmod(dir, 0))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := detectAs(dir, "linux", Hints{})

	require.ErrorIs(t, err, os.ErrPermission, "报的是目录读不了，而不是每种语言各报一条清单文件读不了")
	var nce *NoCommandError
	assert.NotErrorAs(t, err, &nce)
}

func TestARelativeDirectoryBecomesAbsolute(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})
	wd, err := os.Getwd()
	require.NoError(t, err)
	rel, err := filepath.Rel(wd, dir)
	if err != nil {
		t.Skip("临时目录与当前目录不在同一个盘上，算不出相对路径")
	}

	cmd := mustDetect(t, rel, Hints{})

	assert.Equal(t, dir, cmd.Dir)
}

// ---- 多种语言的取舍 ----

func TestNothingRecognisedIsAnErrorThatSaysSo(t *testing.T) {
	dir := write(t, map[string]string{"README.md": "hi"})

	_, err := detectAs(dir, "linux", Hints{})

	var nce *NoCommandError
	require.ErrorAs(t, err, &nce)
	assert.Equal(t, dir, nce.Dir)
	assert.Empty(t, nce.Problems)
	assert.Contains(t, err.Error(), "no supported language recognised")
}

func TestAnExplicitLanguageWithoutItsMarkerFileSaysWhichFileIsMissing(t *testing.T) {
	dir := write(t, map[string]string{"package.json": `{"scripts":{"start":"x"}}`})

	problems := problemsOf(t, dir, Hints{Language: LangGo})

	assert.Equal(t, []Problem{{Language: "go", Reason: ReasonMarkerMissing, Detail: "go.mod"}}, problems)
}

func TestTheDetectedCommandCarriesTheDirectoryEvidenceAndLanguage(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, Command{
		Language: "go",
		Argv:     []string{"go", "run", "."},
		Dir:      dir,
		Evidence: []string{"go.mod", "package main"},
		Detected: true,
	}, cmd)
}

// ---- 错误文字 ----

func TestErrorTextsNameTheirSubject(t *testing.T) {
	problem := Problem{Language: "node", Reason: ReasonConflictingPackageManagers, Detail: "package.json",
		Options: []string{"pnpm-lock.yaml", "yarn.lock"}}
	assert.Equal(t, "node: conflicting-package-managers (package.json) [pnpm-lock.yaml, yarn.lock]", problem.String())
	assert.Equal(t, "go: no-entry-point", Problem{Language: "go", Reason: ReasonNoEntryPoint}.String())

	nce := &NoCommandError{Dir: "/x", Problems: []Problem{problem}}
	assert.Contains(t, nce.Error(), "/x")
	assert.Contains(t, nce.Error(), "conflicting-package-managers")

	assert.Contains(t, (&ProgramMissingError{Program: "mvn"}).Error(), `"mvn"`)
}

// ---- CheckProgram ----

func TestCheckProgramFindsABareProgramOnPath(t *testing.T) {
	name := "sh"
	if runtime.GOOS == "windows" {
		name = "cmd"
	}

	assert.NoError(t, Command{Argv: []string{name}}.CheckProgram())
}

func TestCheckProgramNamesAMissingBareProgram(t *testing.T) {
	err := Command{Argv: []string{"definitely-not-installed-xyz"}}.CheckProgram()

	var missing *ProgramMissingError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "definitely-not-installed-xyz", missing.Program)
}

func TestCheckProgramChecksAnAbsolutePathDirectly(t *testing.T) {
	dir := write(t, map[string]string{"run.sh": "#!/bin/sh\n"})

	assert.NoError(t, Command{Argv: []string{filepath.Join(dir, "run.sh")}}.CheckProgram())

	var missing *ProgramMissingError
	assert.ErrorAs(t, Command{Argv: []string{filepath.Join(dir, "gone.sh")}}.CheckProgram(), &missing)
	assert.ErrorAs(t, Command{Argv: []string{dir}}.CheckProgram(), &missing, "目录不是程序")
}

func TestCheckProgramRejectsAnEmptyCommand(t *testing.T) {
	var missing *ProgramMissingError
	assert.ErrorAs(t, Command{}.CheckProgram(), &missing)
}
```

- [ ] **Step 7: 写 Go 适配器测试 `lang_go_test.go`**

```go
package runcmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGoRunsTheRootPackageWhenItIsMain(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"go", "run", "."}, cmd.Argv)
	assert.Equal(t, []string{"go.mod", "package main"}, cmd.Evidence)
}

func TestGoFallsBackToTheOnlyMainPackageUnderCmd(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod":             "module x\n",
		"lib.go":             libGo,
		"cmd/api/main.go":    mainGo,
		"cmd/tools/lib.go":   libGo, // cmd/ 下不是 main 包的目录不算候选
		"internal/x/main.go": mainGo,
	})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"go", "run", "./cmd/api"}, cmd.Argv)
	assert.Equal(t, []string{"go.mod", "./cmd/api"}, cmd.Evidence)
}

func TestGoWithSeveralMainPackagesLeavesTheChoiceToTheUser(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod":          "module x\n",
		"cmd/worker/m.go": mainGo,
		"cmd/api/main.go": mainGo,
	})

	problems := problemsOf(t, dir, Hints{})

	assert.Equal(t, []Problem{{
		Language: "go", Reason: ReasonMultipleEntryPoints, Detail: "cmd",
		Options: []string{"./cmd/api", "./cmd/worker"},
	}}, problems, "候选按名字排序，结果稳定")
}

func TestGoWithoutAnyMainPackageHasNoEntryPoint(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "lib.go": libGo})

	assert.Equal(t, []Problem{{Language: "go", Reason: ReasonNoEntryPoint, Detail: "go.mod"}},
		problemsOf(t, dir, Hints{}))
}

func TestGoTheRootMainPackageWinsOverCmd(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod": "module x\n", "main.go": mainGo,
		"cmd/a/main.go": mainGo, "cmd/b/main.go": mainGo,
	})

	assert.Equal(t, []string{"go", "run", "."}, mustDetect(t, dir, Hints{}).Argv)
}

func TestGoIgnoresGoFilesThatCannotBeRun(t *testing.T) {
	// 只有测试文件的 main 包、以及 //go:build ignore 的脚本，都不是能 go run 的入口。
	dir := write(t, map[string]string{
		"go.mod":       "module x\n",
		"main_test.go": mainGo,
		"gen.go":       "//go:build ignore\n\npackage main\n\nfunc main() {}\n",
	})

	assert.Equal(t, []Problem{{Language: "go", Reason: ReasonNoEntryPoint, Detail: "go.mod"}},
		problemsOf(t, dir, Hints{}))
}

func TestGoWithoutAGoModIsNotGo(t *testing.T) {
	dir := write(t, map[string]string{"main.go": mainGo})

	assert.Empty(t, problemsOf(t, dir, Hints{}))
}

func TestGoIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"go.mod": "module x\n", "main.go": mainGo})

	cmd, err := detectAs(dir, "windows", Hints{})

	assert.NoError(t, err)
	assert.Equal(t, []string{"go", "run", "."}, cmd.Argv, "go 在 Windows 上由 PATHEXT 解析成 go.exe，命令本身不变")
}
```

- [ ] **Step 8: gofmt，跑测试确认全绿**

Run: `gofmt -l internal/runcmd`
Expected: 空输出（没有需要格式化的文件；如果有，跑 `gofmt -w internal/runcmd` 再确认一次）

Run: `go test ./internal/runcmd/ -v`
Expected: `PASS`，退出码 0。这一步的 `adapters` 表只有 Go 一种语言，所以依赖"至少两种语言同时存在"的测试（`TestTwoLanguagesThatCanBothRunAreAmbiguous`、`TestSeveralRecognisedButUnrunnableLanguagesAreAllReported` 等）和依赖"全部七种语言都注册了"的测试（`TestLanguagesAreCompleteAndInAFixedOrder`、`TestAnUnknownLanguageIsRejected` 等）在这一步的 `runcmd_test.go` 里**还不存在**——前者在 Task 3（Node 落地后）追加，后者在 Task 5（七个语言全部到位后）追加，都属于后面的任务，不是这一步漏写。

- [ ] **Step 9: `go vet` 与交叉编译确认**

Run: `go vet ./internal/runcmd/`
Expected: 退出码 0，无输出。

Run: `make check-cross-build`
Expected: 退出码 0（Plan 2 已经建好这个目标，这里只是确认新包没有引入平台相关代码——它确实不该有，`runcmd` 全程只用跨平台的标准库调用）。

- [ ] **Step 10: 提交**

```bash
git add internal/runcmd/runcmd.go internal/runcmd/errors.go internal/runcmd/scan.go internal/runcmd/lang_go.go internal/runcmd/helper_test.go internal/runcmd/runcmd_test.go internal/runcmd/lang_go_test.go
git commit -m "$(cat <<'EOF'
新增：runcmd 包骨架——Hints/Command/Detect 驱动逻辑，外加第一个语言适配器 Go

给定组件源码目录，探测本机启动命令，不启动进程（procsup 的事）也不读 brickkit
的配置文件（Plan 4 的事）。驱动逻辑处理三种主干情形：没识别出语言、识别出恰好
一种、识别出不止一种（报歧义，不悄悄选一个）；用户手写 runCommand 时完全跳过
探测。Go 适配器判断"是不是可执行的 main 包"用标准库 go/build，不 shell 出去
跑 go list，也不需要这台机器装 go 工具链。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Rust 与 .NET 适配器

**背景：** 这两种语言的探测规则最简单——`cargo run`/`dotnet run` 本身就是"这个目录的标准运行方式"，不需要像 Node/Java 那样在多个候选里挑。各自只挡一种"明确跑不了"的情形（Rust 的纯虚拟工作区、.NET 的多项目文件或者只有解决方案文件），理由见"设计决定"第 8 条。

**Files:**
- Create: `internal/runcmd/lang_rust.go`
- Create: `internal/runcmd/lang_dotnet.go`
- Create: `internal/runcmd/lang_rust_dotnet_test.go`
- Modify: `internal/runcmd/runcmd.go`（`adapters` 表追加两行）

**Interfaces:**
- Consumes：Task 1 的 `scan`/`outcome`/`problemOutcome`/`Reason` 系列。
- Produces：`probeRust`、`probeDotnet`（供 `adapters` 表引用，不对外导出更多符号）。

- [ ] **Step 1: 写 `lang_rust.go`**

```go
package runcmd

import "strings"

// probeRust：有 Cargo.toml → `cargo run`。
// 只挡一种便宜又确定的情形：纯虚拟工作区（有 [workspace] 没有 [package]），根目录没有可运行的包。
// 一个包里有多个二进制时 cargo 自己会报 "could not determine which binary to run"，
// 那句话会随崩溃的最后几行输出一起给到用户，不必在这里重复实现一遍。
func probeRust(s *scan) outcome {
	text, present, err := s.read("Cargo.toml")
	if !present {
		return outcome{}
	}
	if err != nil {
		return problemOutcome(ReasonUnreadableManifest, "Cargo.toml")
	}
	if tomlHasTable(text, "workspace") && !tomlHasTable(text, "package") {
		return problemOutcome(ReasonNoEntryPoint, "Cargo.toml")
	}
	return s.ok([]string{"cargo", "run"}, "Cargo.toml")
}

// tomlHasTable 判断 TOML 文本里有没有 `[name]` 这个表头（容忍空白与行尾注释）。
func tomlHasTable(text, name string) bool {
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if strings.Join(strings.Fields(line), "") == "["+name+"]" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 写 `lang_dotnet.go`**

```go
package runcmd

// probeDotnet：根目录恰好一个项目文件（.csproj / .fsproj / .vbproj）→ `dotnet run`。
// 多个项目文件时 dotnet 选不出来；只有解决方案文件（.sln）时项目在子目录里——两种都交给用户。
func probeDotnet(s *scan) outcome {
	projects := s.namesWithExt(".csproj", ".fsproj", ".vbproj")
	switch {
	case len(projects) == 1:
		return s.ok([]string{"dotnet", "run"}, projects[0])
	case len(projects) > 1:
		return problemOutcome(ReasonMultipleEntryPoints, "", projects...)
	}
	if solutions := s.namesWithExt(".sln", ".slnx"); len(solutions) > 0 {
		return problemOutcome(ReasonNoEntryPoint, solutions[0])
	}
	return outcome{}
}
```

- [ ] **Step 3: 把两个适配器接进 `adapters` 表**

在 `internal/runcmd/runcmd.go` 里，把

```go
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
}
```

改成

```go
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
	{LangRust, "Cargo.toml", probeRust, nil},
	{LangDotnet, "*.csproj", probeDotnet, nil},
}
```

顺序即 `Languages()` 的顺序，也是歧义报错里候选的顺序——不是随便排的，后面 Task 3–5 追加 Node/Java/Python/Ruby 时延续同一张表、同一种"先加行，测试自然覆盖"的模式。

- [ ] **Step 4: 写测试**

```go
package runcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- Rust ----

func TestRustRunsCargo(t *testing.T) {
	dir := write(t, map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n"})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangRust, cmd.Language)
	assert.Equal(t, []string{"cargo", "run"}, cmd.Argv)
	assert.Equal(t, []string{"Cargo.toml"}, cmd.Evidence)
}

func TestRustAWorkspaceRootThatIsAlsoAPackageStillRuns(t *testing.T) {
	dir := write(t, map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n\n[workspace]\nmembers = [\"y\"]\n"})

	assert.Equal(t, []string{"cargo", "run"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestRustAVirtualWorkspaceHasNoEntryPoint(t *testing.T) {
	// 表头里有空白、后面跟着注释，照样认得出来。
	dir := write(t, map[string]string{"Cargo.toml": "[ workspace ]  # members below\nmembers = [\"y\"]\n"})

	assert.Equal(t, []Problem{{Language: "rust", Reason: ReasonNoEntryPoint, Detail: "Cargo.toml"}},
		problemsOf(t, dir, Hints{}))
}

func TestRustACommentedOutPackageTableDoesNotCount(t *testing.T) {
	dir := write(t, map[string]string{"Cargo.toml": "# [package]\n[workspace]\n"})

	assert.Len(t, problemsOf(t, dir, Hints{}), 1)
}

func TestRustAnUnreadableManifestIsReported(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("root 与 Windows 下 chmod 000 挡不住读取")
	}
	dir := write(t, map[string]string{"Cargo.toml": "[package]\n"})
	require.NoError(t, os.Chmod(filepath.Join(dir, "Cargo.toml"), 0))

	assert.Equal(t, []Problem{{Language: "rust", Reason: ReasonUnreadableManifest, Detail: "Cargo.toml"}},
		problemsOf(t, dir, Hints{}))
}

func TestRustIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"Cargo.toml": "[package]\n"})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{"cargo", "run"}, cmd.Argv)
}

// ---- .NET ----

func TestDotnetRunsTheOnlyProjectFile(t *testing.T) {
	for _, project := range []string{"App.csproj", "App.fsproj", "App.vbproj"} {
		dir := write(t, map[string]string{project: "<Project />"})

		cmd := mustDetect(t, dir, Hints{})

		assert.Equal(t, LangDotnet, cmd.Language, project)
		assert.Equal(t, []string{"dotnet", "run"}, cmd.Argv, project)
		assert.Equal(t, []string{project}, cmd.Evidence, project)
	}
}

func TestDotnetASolutionFileNextToTheProjectDoesNotGetInTheWay(t *testing.T) {
	dir := write(t, map[string]string{"App.sln": "", "App.csproj": "<Project />"})

	assert.Equal(t, []string{"dotnet", "run"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestDotnetSeveralProjectFilesLeaveTheChoiceToTheUser(t *testing.T) {
	dir := write(t, map[string]string{"B.csproj": "", "A.csproj": "", "C.fsproj": ""})

	assert.Equal(t, []Problem{{
		Language: "dotnet", Reason: ReasonMultipleEntryPoints,
		Options: []string{"A.csproj", "B.csproj", "C.fsproj"},
	}}, problemsOf(t, dir, Hints{}))
}

func TestDotnetASolutionWithoutARootProjectHasNoEntryPoint(t *testing.T) {
	dir := write(t, map[string]string{"All.sln": "", "src/Api/Api.csproj": "<Project />"})

	assert.Equal(t, []Problem{{Language: "dotnet", Reason: ReasonNoEntryPoint, Detail: "All.sln"}},
		problemsOf(t, dir, Hints{}))
}

func TestDotnetANewStyleSolutionFileCountsToo(t *testing.T) {
	dir := write(t, map[string]string{"All.slnx": ""})

	assert.Len(t, problemsOf(t, dir, Hints{}), 1)
}

func TestDotnetWithNothingIsNotDotnet(t *testing.T) {
	dir := write(t, map[string]string{"notes.txt": ""})

	assert.Empty(t, problemsOf(t, dir, Hints{}))
}

func TestDotnetTheExtensionMatchIsCaseInsensitive(t *testing.T) {
	dir := write(t, map[string]string{"App.CSPROJ": "<Project />"})

	assert.Equal(t, []string{"dotnet", "run"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestDotnetIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"App.csproj": "<Project />"})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{"dotnet", "run"}, cmd.Argv)
}
```

- [ ] **Step 5: gofmt + 测试**

Run: `gofmt -w internal/runcmd && go test ./internal/runcmd/ -v -run 'TestRust|TestDotnet'`
Expected: 全部 `PASS`。

Run: `go test ./internal/runcmd/`
Expected: `PASS`（跑一遍全量，确认没有破坏 Task 1 的测试）。

- [ ] **Step 6: 提交**

```bash
git add internal/runcmd/lang_rust.go internal/runcmd/lang_dotnet.go internal/runcmd/lang_rust_dotnet_test.go internal/runcmd/runcmd.go
git commit -m "$(cat <<'EOF'
新增：runcmd 的 Rust 与 .NET 适配器

Rust 只挡纯虚拟工作区（有 [workspace] 没有 [package]，根目录没有可运行的包）；
一个包里有多个二进制目标不在这里挡——cargo run 自己会报出候选列表，这句话会
随崩溃的最后几行输出一起交给用户（procsup 的 Tail，见 Plan 2），不必在探测
阶段重新实现一遍。.NET 只有恰好一个项目文件才认，多个项目文件或者只有解决方案
文件（项目在子目录里）都交给用户用 runCommand 明说。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Node 适配器

**背景：** Node 的规则比 Rust/.NET 复杂一层：先看 `package.json` 有没有 `start`（没有则 `dev`）脚本，再决定用哪个包管理器跑它（`packageManager` 字段 > 锁文件 > 默认 `npm`，见"设计决定"第 6 条）。这也是第一个"识别出不止一种语言"的真实场景会被测到的任务——一个仓库里同时有 `go.mod` 和一个只写了 `lint` 脚本的 `package.json` 很常见，不该被误判成歧义。

**Files:**
- Create: `internal/runcmd/lang_node.go`
- Create: `internal/runcmd/lang_node_test.go`
- Modify: `internal/runcmd/runcmd.go`（`adapters` 表追加一行）
- Modify: `internal/runcmd/runcmd_test.go`（补上依赖"至少两种语言同时存在"的驱动层测试——Task 1 时这些测试还没有对应的第二种语言）

**Interfaces:**
- Consumes：Task 1 的 `scan`/`outcome`/`problemOutcome`。
- Produces：`probeNode`。

- [ ] **Step 1: 写 `lang_node.go`**

```go
package runcmd

import (
	"encoding/json"
	"slices"
	"strings"
)

type packageJSON struct {
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
}

// 锁文件与包管理器的对应，顺序即展示顺序。
var nodeLockfiles = []struct{ file, manager string }{
	{"pnpm-lock.yaml", "pnpm"},
	{"yarn.lock", "yarn"},
	{"package-lock.json", "npm"},
	{"npm-shrinkwrap.json", "npm"},
}

// probeNode：有 package.json 且带 start（没有则 dev）脚本 → `<包管理器> run <脚本>`。
//
// start 优先于 dev：start 是生态里唯一有专属简写的入口，也最接近容器镜像里真正跑的那条命令。
// 选错的代价都看得见——start 需要先构建就会立刻崩溃，崩溃现场里就是 node 的报错；
// 想要 dev，写 runCommand 即可。
func probeNode(s *scan) outcome {
	text, present, err := s.read("package.json")
	if !present {
		return outcome{}
	}
	var pkg packageJSON
	if err != nil || json.Unmarshal([]byte(text), &pkg) != nil {
		return problemOutcome(ReasonUnreadableManifest, "package.json")
	}

	script := ""
	for _, name := range []string{"start", "dev"} {
		if strings.TrimSpace(pkg.Scripts[name]) != "" {
			script = name
			break
		}
	}
	if script == "" {
		return problemOutcome(ReasonNoStartScript, "package.json")
	}

	manager, evidence, bad := s.nodeManager(pkg.PackageManager)
	if bad != nil {
		return *bad
	}
	return s.ok([]string{manager, "run", script}, append([]string{"package.json scripts." + script}, evidence...)...)
}

// nodeManager 决定用哪个包管理器：package.json 的 packageManager 字段（corepack 的约定）最权威，
// 其次是锁文件；没有锁文件就用 npm（装 Node 就有的那个）。
func (s *scan) nodeManager(declared string) (manager string, evidence []string, bad *outcome) {
	if declared != "" {
		name, _, _ := strings.Cut(declared, "@")
		switch name {
		case "npm", "pnpm", "yarn":
			return name, []string{"package.json packageManager"}, nil
		}
		o := problemOutcome(ReasonUnsupportedPackageManager, declared)
		return "", nil, &o
	}

	var managers, files []string
	for _, l := range nodeLockfiles {
		if s.isFile(l.file) {
			files = append(files, l.file)
			if !slices.Contains(managers, l.manager) {
				managers = append(managers, l.manager)
			}
		}
	}
	switch len(managers) {
	case 0:
		return "npm", nil, nil
	case 1:
		return managers[0], files, nil
	default:
		o := problemOutcome(ReasonConflictingPackageManagers, "package.json", files...)
		return "", nil, &o
	}
}
```

- [ ] **Step 2: 接进 `adapters` 表**

把 `internal/runcmd/runcmd.go` 里的 `adapters` 表从 Task 2 结束时的样子

```go
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
	{LangRust, "Cargo.toml", probeRust, nil},
	{LangDotnet, "*.csproj", probeDotnet, nil},
}
```

改成

```go
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
	{LangRust, "Cargo.toml", probeRust, nil},
	{LangDotnet, "*.csproj", probeDotnet, nil},
	{LangNode, "package.json", probeNode, nil},
}
```

- [ ] **Step 3: 写 Node 测试**

```go
package runcmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pkgJSON(scripts, extra string) string {
	if extra != "" {
		extra = ", " + extra
	}
	return `{"name": "x", "scripts": {` + scripts + `}` + extra + `}`
}

func TestNodeRunsTheStartScriptWithNpmByDefault(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"start": "node server.js"`, "")})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangNode, cmd.Language)
	assert.Equal(t, []string{"npm", "run", "start"}, cmd.Argv)
	assert.Equal(t, []string{"package.json scripts.start"}, cmd.Evidence)
}

func TestNodeStartBeatsDev(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"dev": "nodemon .", "start": "node ."`, "")})

	assert.Equal(t, []string{"npm", "run", "start"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestNodeFallsBackToDevWhenThereIsNoStart(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"dev": "nodemon ."`, "")})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"npm", "run", "dev"}, cmd.Argv)
	assert.Equal(t, []string{"package.json scripts.dev"}, cmd.Evidence)
}

func TestNodeABlankStartScriptDoesNotCount(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"start": "  ", "dev": "vite"`, "")})

	assert.Equal(t, []string{"npm", "run", "dev"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestNodeWithoutStartOrDevHasNoStartScript(t *testing.T) {
	for name, content := range map[string]string{
		"only lint":    pkgJSON(`"lint": "eslint ."`, ""),
		"no scripts":   `{"name": "x"}`,
		"empty object": `{}`,
	} {
		dir := write(t, map[string]string{"package.json": content})

		assert.Equal(t, []Problem{{Language: "node", Reason: ReasonNoStartScript, Detail: "package.json"}},
			problemsOf(t, dir, Hints{}), name)
	}
}

func TestNodeAPackageJSONThatIsNotValidIsReported(t *testing.T) {
	for name, content := range map[string]string{
		"not json":           "{ nope",
		"scripts not object": `{"scripts": ["start"]}`,
		"script not string":  `{"scripts": {"start": 1}}`,
	} {
		dir := write(t, map[string]string{"package.json": content})

		assert.Equal(t, []Problem{{Language: "node", Reason: ReasonUnreadableManifest, Detail: "package.json"}},
			problemsOf(t, dir, Hints{}), name)
	}
}

func TestNodePicksThePackageManagerFromTheLockfile(t *testing.T) {
	for lockfile, manager := range map[string]string{
		"pnpm-lock.yaml":      "pnpm",
		"yarn.lock":           "yarn",
		"package-lock.json":   "npm",
		"npm-shrinkwrap.json": "npm",
	} {
		dir := write(t, map[string]string{"package.json": pkgJSON(`"start": "node ."`, ""), lockfile: ""})

		cmd := mustDetect(t, dir, Hints{})

		assert.Equal(t, []string{manager, "run", "start"}, cmd.Argv, lockfile)
		assert.Equal(t, []string{"package.json scripts.start", lockfile}, cmd.Evidence, lockfile)
	}
}

func TestNodeTwoLockfilesOfTheSameManagerAreNotAConflict(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json":      pkgJSON(`"start": "node ."`, ""),
		"package-lock.json": "", "npm-shrinkwrap.json": "",
	})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"npm", "run", "start"}, cmd.Argv)
	assert.Equal(t, []string{"package.json scripts.start", "package-lock.json", "npm-shrinkwrap.json"}, cmd.Evidence)
}

func TestNodeLockfilesOfDifferentManagersConflict(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json":   pkgJSON(`"start": "node ."`, ""),
		"pnpm-lock.yaml": "", "yarn.lock": "",
	})

	assert.Equal(t, []Problem{{
		Language: "node", Reason: ReasonConflictingPackageManagers, Detail: "package.json",
		Options: []string{"pnpm-lock.yaml", "yarn.lock"},
	}}, problemsOf(t, dir, Hints{}))
}

func TestNodeThePackageManagerFieldOutranksLockfiles(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json":   pkgJSON(`"start": "node ."`, `"packageManager": "pnpm@9.1.0"`),
		"pnpm-lock.yaml": "", "yarn.lock": "", // 冲突的锁文件也不再要紧
	})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"pnpm", "run", "start"}, cmd.Argv)
	assert.Equal(t, []string{"package.json scripts.start", "package.json packageManager"}, cmd.Evidence)
}

func TestNodeThePackageManagerFieldMayCarryAHash(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json": pkgJSON(`"start": "node ."`, `"packageManager": "yarn@4.1.0+sha256.abc123"`),
	})

	assert.Equal(t, []string{"yarn", "run", "start"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestNodeAnUnsupportedPackageManagerIsReported(t *testing.T) {
	dir := write(t, map[string]string{
		"package.json": pkgJSON(`"start": "node ."`, `"packageManager": "bun@1.1.0"`),
	})

	assert.Equal(t, []Problem{{Language: "node", Reason: ReasonUnsupportedPackageManager, Detail: "bun@1.1.0"}},
		problemsOf(t, dir, Hints{}))
}

func TestNodeIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"package.json": pkgJSON(`"start": "node ."`, ""), "pnpm-lock.yaml": ""})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{"pnpm", "run", "start"}, cmd.Argv, "pnpm 在 Windows 上由 PATHEXT 解析成 pnpm.cmd，命令本身不变")
}
```

- [ ] **Step 4: 把驱动层里依赖"至少两种语言"的测试补进 `runcmd_test.go`**

Task 1 写 `runcmd_test.go` 时 `adapters` 表只有 Go 一种语言，下面这些用例当时写不出来。现在 Node 已经接入，把 `runcmd_test.go` 补成完整版——用 Edit 在文件末尾追加这些测试函数（跟 Task 1 已有的测试函数不重复，只是新增）：

```go
func TestTwoLanguagesThatCanBothRunAreAmbiguous(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod": "module x\n", "main.go": mainGo,
		"package.json": `{"scripts":{"start":"node server.js"}}`,
	})

	_, err := detectAs(dir, "linux", Hints{})

	var amb *AmbiguousError
	require.ErrorAs(t, err, &amb)
	assert.Equal(t, []Candidate{
		{Language: "go", Argv: []string{"go", "run", "."}},
		{Language: "node", Argv: []string{"npm", "run", "start"}},
	}, amb.Candidates)
	assert.Contains(t, err.Error(), "go (go run .), node (npm run start)")
}

func TestAnExplicitLanguageResolvesTheAmbiguity(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod": "module x\n", "main.go": mainGo,
		"package.json": `{"scripts":{"start":"node server.js"}}`,
	})

	cmd := mustDetect(t, dir, Hints{Language: LangNode})

	assert.Equal(t, LangNode, cmd.Language)
	assert.Equal(t, []string{"npm", "run", "start"}, cmd.Argv)
}

func TestALanguageThatCannotRunDoesNotBlockOneThatCan(t *testing.T) {
	// 很常见的形状：Go 仓库里放了一个只有 lint 脚本的 package.json。
	dir := write(t, map[string]string{
		"go.mod": "module x\n", "main.go": mainGo,
		"package.json": `{"scripts":{"lint":"eslint ."}}`,
	})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangGo, cmd.Language)
}

func TestSeveralRecognisedButUnrunnableLanguagesAreAllReported(t *testing.T) {
	dir := write(t, map[string]string{
		"go.mod":       "module x\n",
		"package.json": `{"scripts":{"lint":"eslint ."}}`,
	})

	problems := problemsOf(t, dir, Hints{})

	assert.Equal(t, []Problem{
		{Language: "go", Reason: ReasonNoEntryPoint, Detail: "go.mod"},
		{Language: "node", Reason: ReasonNoStartScript, Detail: "package.json"},
	}, problems)
}
```

- [ ] **Step 5: gofmt + 测试**

Run: `gofmt -w internal/runcmd && go test ./internal/runcmd/ -v -run 'TestNode|TestTwoLanguages|TestAnExplicitLanguageResolvesTheAmbiguity|TestALanguageThatCannotRunDoesNotBlockOneThatCan|TestSeveralRecognisedButUnrunnableLanguagesAreAllReported'`
Expected: 全部 `PASS`。

Run: `go test ./internal/runcmd/`
Expected: `PASS`。

- [ ] **Step 6: 提交**

```bash
git add internal/runcmd/lang_node.go internal/runcmd/lang_node_test.go internal/runcmd/runcmd.go internal/runcmd/runcmd_test.go
git commit -m "$(cat <<'EOF'
新增：runcmd 的 Node 适配器；补上依赖多语言并存的驱动层测试

package.json 有 start（没有则 dev）脚本才认；start 优先于 dev 是因为它是生态
里唯一有专属简写的入口，也最接近容器镜像里真正跑的那条命令。包管理器的选择：
packageManager 字段（corepack 约定）最权威，其次看锁文件，都没有就用 npm；
两把同一管理器的锁文件不算冲突，真正的冲突只在认出不止一种包管理器时报。
Node 落地后 adapters 表里第一次同时有两种语言，驱动层"歧义""一种认得出另一种
认不出不该互相干扰"这些测试这才补得上。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Java 适配器（Maven / Gradle，仅 Spring Boot）

**背景：** Java 没有通用的"运行这个项目"命令，唯一有标准答案的是 Spring Boot 的构建插件（Maven 的 `spring-boot:run`、Gradle 的 `bootRun`）——信号是"插件配了没有"而不是"依赖里有没有 `spring-boot-starter`"，见"设计决定"第 7 条。这是七个适配器里规则最多的一个：要先判断 Maven/Gradle 构建文件是否同时存在（冲突）、要在判"是不是 Spring Boot"之前先判多模块（否则聚合工程会被误报）、要处理 wrapper 脚本优先级与丢失可执行位的退化。

**Files:**
- Create: `internal/runcmd/lang_java.go`
- Create: `internal/runcmd/lang_java_test.go`
- Modify: `internal/runcmd/runcmd.go`（`adapters` 表追加一行；这一行带 `env` 字段，是本计划第一次真正用到它）
- Modify: `internal/runcmd/helper_test.go`（补 `makeExecutable` 帮助函数——Java 的 wrapper 测试需要造出"有执行位"和"没有执行位"两种脚本）

**Interfaces:**
- Consumes：Task 1 的 `scan.wrapper`（Maven/Gradle wrapper 脚本选择，Task 1 就写好了，之前没有消费者）。
- Produces：`probeJava`、`javaEnv`（第一个非空的 `adapter.env`）。

- [ ] **Step 1: 写 `lang_java.go`**

```go
package runcmd

import "strings"

// probeJava 只认 Spring Boot：Java 没有通用的"运行这个项目"命令，唯一有标准答案的是
// Spring Boot 的 Maven 插件（spring-boot:run）与 Gradle 插件（bootRun）。
// 信号取"构建插件在不在"而不是"依赖里有没有 spring-boot-starter"：只有依赖没有插件，
// 那两个命令根本不存在。Maven / Gradle 都优先用项目自带的 wrapper（版本由项目锁定）。
// 已知的边界：Gradle 用版本目录写插件（alias(libs.plugins.…)）时，构建文件里没有
// org.springframework.boot 这个字面量，认不出来，交给 runCommand。
func probeJava(s *scan) outcome {
	pom, hasPom, pomErr := s.read("pom.xml")
	gradleFile := ""
	for _, name := range []string{"build.gradle.kts", "build.gradle"} {
		if s.isFile(name) {
			gradleFile = name
			break
		}
	}

	switch {
	case !hasPom && gradleFile == "":
		return outcome{}
	case hasPom && gradleFile != "":
		return problemOutcome(ReasonConflictingBuildTools, "pom.xml", "pom.xml", gradleFile)
	case hasPom:
		return probeMaven(s, pom, pomErr)
	default:
		return probeGradle(s, gradleFile)
	}
}

func probeMaven(s *scan, pom string, readErr error) outcome {
	if readErr != nil {
		return problemOutcome(ReasonUnreadableManifest, "pom.xml")
	}
	// 聚合工程的插件在子模块里，先判多模块，免得把它误报成"不是 Spring Boot"。
	if strings.Contains(pom, "<modules>") {
		return problemOutcome(ReasonMultiModule, "pom.xml")
	}
	if !strings.Contains(pom, "spring-boot-maven-plugin") {
		return problemOutcome(ReasonNotSpringBoot, "pom.xml")
	}
	prefix, wrapper := s.wrapper("mvnw", "mvnw.cmd", "mvn")
	return s.ok(append(prefix, "spring-boot:run"), evidenceOf("pom.xml", wrapper)...)
}

func probeGradle(s *scan, buildFile string) outcome {
	build, _, err := s.read(buildFile)
	if err != nil {
		return problemOutcome(ReasonUnreadableManifest, buildFile)
	}
	for _, name := range []string{"settings.gradle.kts", "settings.gradle"} {
		if settings, _, _ := s.read(name); gradleIncludes(settings) {
			return problemOutcome(ReasonMultiModule, name)
		}
	}
	if !strings.Contains(build, "org.springframework.boot") {
		return problemOutcome(ReasonNotSpringBoot, buildFile)
	}
	prefix, wrapper := s.wrapper("gradlew", "gradlew.bat", "gradle")
	return s.ok(append(prefix, "bootRun"), evidenceOf(buildFile, wrapper)...)
}

// gradleIncludes 判断 settings.gradle(.kts) 里有没有 include(...) 语句（多项目工程的标志）。
// includeBuild 不算：它是复合构建，不改变根项目自己能不能 bootRun。
func gradleIncludes(settings string) bool {
	for _, line := range strings.Split(settings, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"include(", "include ", "include\t"} {
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
	}
	return false
}

func evidenceOf(files ...string) []string {
	var out []string
	for _, f := range files {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// javaEnv：Spring Boot 把环境变量 SERVER_PORT 宽松绑定到 server.port，且默认监听所有网卡，
// 所以只需要告诉它端口。
func javaEnv(s *scan) []string {
	return []string{"SERVER_PORT=" + s.port()}
}
```

- [ ] **Step 2: 接进 `adapters` 表**

把 `adapters` 表从 Task 3 结束时的样子

```go
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
	{LangRust, "Cargo.toml", probeRust, nil},
	{LangDotnet, "*.csproj", probeDotnet, nil},
	{LangNode, "package.json", probeNode, nil},
}
```

改成

```go
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
	{LangRust, "Cargo.toml", probeRust, nil},
	{LangDotnet, "*.csproj", probeDotnet, nil},
	{LangNode, "package.json", probeNode, nil},
	{LangJava, "pom.xml / build.gradle", probeJava, javaEnv},
}
```

- [ ] **Step 3: 在 `helper_test.go` 里补 `makeExecutable`**

Task 1 的 `helper_test.go` 还没有这个帮助函数（Go/Rust/.NET/Node 的测试都不需要操作可执行位）。用 Edit，在 `write` 函数的结尾 `}` 之后、`detectAs` 函数之前，插入：

```go
func makeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.Chmod(filepath.Join(dir, filepath.FromSlash(name)), 0o755))
}
```

- [ ] **Step 4: 写 Java 测试**

```go
package runcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	springPom = `<project><build><plugins><plugin>
<artifactId>spring-boot-maven-plugin</artifactId></plugin></plugins></build></project>`
	plainPom      = `<project><dependencies></dependencies></project>`
	aggregatorPom = `<project><modules><module>api</module></modules></project>`

	springGradle = "plugins { id 'org.springframework.boot' version '3.3.0' }\n"
	springKts    = "plugins { id(\"org.springframework.boot\") version \"3.3.0\" }\n"
)

func TestJavaMavenRunsTheSpringBootPlugin(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangJava, cmd.Language)
	assert.Equal(t, []string{"mvn", "spring-boot:run"}, cmd.Argv, "没有 wrapper 就用 PATH 里的 mvn")
	assert.Equal(t, []string{"pom.xml"}, cmd.Evidence)
	assert.Equal(t, []string{"SERVER_PORT=18080"}, cmd.Env)
}

func TestJavaMavenPrefersTheWrapper(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom, "mvnw": "#!/bin/sh\n"})
	makeExecutable(t, dir, "mvnw")

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{filepath.Join(dir, "mvnw"), "spring-boot:run"}, cmd.Argv)
	assert.Equal(t, []string{"pom.xml", "mvnw"}, cmd.Evidence)
}

func TestJavaAWrapperThatLostItsExecutableBitIsRunThroughSh(t *testing.T) {
	// Windows 上 clone、解压 zip 都会丢可执行位；与其让用户撞上 permission denied，不如用 sh 去跑它。
	dir := write(t, map[string]string{"pom.xml": springPom, "mvnw": "#!/bin/sh\n"})
	require.NoError(t, os.Chmod(filepath.Join(dir, "mvnw"), 0o644))

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, []string{"sh", filepath.Join(dir, "mvnw"), "spring-boot:run"}, cmd.Argv)
}

func TestJavaMavenOnWindowsUsesTheCmdWrapper(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom, "mvnw": "", "mvnw.cmd": ""})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "mvnw.cmd"), "spring-boot:run"}, cmd.Argv)
	assert.Equal(t, []string{"pom.xml", "mvnw.cmd"}, cmd.Evidence)
}

func TestJavaMavenOnWindowsWithOnlyTheUnixWrapperFallsBackToMvn(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom, "mvnw": ""})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, []string{"mvn", "spring-boot:run"}, cmd.Argv)
}

func TestJavaMavenWithoutTheSpringBootPluginHasNoGenericRunCommand(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": plainPom})

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonNotSpringBoot, Detail: "pom.xml"}},
		problemsOf(t, dir, Hints{}))
}

func TestJavaAMavenAggregatorIsMultiModule(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": aggregatorPom})

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonMultiModule, Detail: "pom.xml"}},
		problemsOf(t, dir, Hints{}), "聚合工程的插件在子模块里，不能被误报成 not-spring-boot")
}

func TestJavaAnUnreadablePomIsReported(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("root 与 Windows 下 chmod 000 挡不住读取")
	}
	dir := write(t, map[string]string{"pom.xml": springPom})
	require.NoError(t, os.Chmod(filepath.Join(dir, "pom.xml"), 0))

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonUnreadableManifest, Detail: "pom.xml"}},
		problemsOf(t, dir, Hints{}))
}

func TestJavaAnUnreadableGradleBuildFileIsReported(t *testing.T) {
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("root 与 Windows 下 chmod 000 挡不住读取")
	}
	dir := write(t, map[string]string{"build.gradle": springGradle})
	require.NoError(t, os.Chmod(filepath.Join(dir, "build.gradle"), 0))

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonUnreadableManifest, Detail: "build.gradle"}},
		problemsOf(t, dir, Hints{}))
}

func TestJavaGradleRunsBootRun(t *testing.T) {
	for file, content := range map[string]string{"build.gradle": springGradle, "build.gradle.kts": springKts} {
		dir := write(t, map[string]string{file: content})

		cmd := mustDetect(t, dir, Hints{})

		assert.Equal(t, []string{"gradle", "bootRun"}, cmd.Argv, file)
		assert.Equal(t, []string{file}, cmd.Evidence, file)
		assert.Equal(t, []string{"SERVER_PORT=18080"}, cmd.Env, file)
	}
}

func TestJavaGradlePrefersTheWrapper(t *testing.T) {
	dir := write(t, map[string]string{"build.gradle": springGradle, "gradlew": "#!/bin/sh\n", "gradlew.bat": ""})
	makeExecutable(t, dir, "gradlew")

	unix := mustDetect(t, dir, Hints{})
	assert.Equal(t, []string{filepath.Join(dir, "gradlew"), "bootRun"}, unix.Argv)
	assert.Equal(t, []string{"build.gradle", "gradlew"}, unix.Evidence)

	windows, err := detectAs(dir, "windows", Hints{})
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "gradlew.bat"), "bootRun"}, windows.Argv)
}

func TestJavaGradleWithoutTheSpringBootPluginHasNoGenericRunCommand(t *testing.T) {
	dir := write(t, map[string]string{"build.gradle": "plugins { id 'java' }\n"})

	assert.Equal(t, []Problem{{Language: "java", Reason: ReasonNotSpringBoot, Detail: "build.gradle"}},
		problemsOf(t, dir, Hints{}))
}

func TestJavaAMultiProjectGradleBuildIsMultiModule(t *testing.T) {
	for settings, content := range map[string]string{
		"settings.gradle":     "rootProject.name = 'x'\ninclude 'api', 'worker'\n",
		"settings.gradle.kts": "rootProject.name = \"x\"\n  include(\":api\")\n",
	} {
		dir := write(t, map[string]string{"build.gradle": springGradle, settings: content})

		assert.Equal(t, []Problem{{Language: "java", Reason: ReasonMultiModule, Detail: settings}},
			problemsOf(t, dir, Hints{}), settings)
	}
}

func TestJavaGradleIncludeBuildIsNotMultiProject(t *testing.T) {
	dir := write(t, map[string]string{
		"build.gradle":    springGradle,
		"settings.gradle": "rootProject.name = 'x'\nincludeBuild('../shared')\n",
	})

	assert.Equal(t, []string{"gradle", "bootRun"}, mustDetect(t, dir, Hints{}).Argv)
}

func TestJavaBothBuildToolsAtOnceIsAConflict(t *testing.T) {
	dir := write(t, map[string]string{"pom.xml": springPom, "build.gradle.kts": springKts})

	assert.Equal(t, []Problem{{
		Language: "java", Reason: ReasonConflictingBuildTools, Detail: "pom.xml",
		Options: []string{"pom.xml", "build.gradle.kts"},
	}}, problemsOf(t, dir, Hints{}))
}

func TestJavaWithoutABuildFileIsNotJava(t *testing.T) {
	dir := write(t, map[string]string{"Main.java": "class Main {}"})

	assert.Empty(t, problemsOf(t, dir, Hints{}))
}
```

- [ ] **Step 5: gofmt + 测试**

Run: `gofmt -w internal/runcmd && go test ./internal/runcmd/ -v -run TestJava`
Expected: 全部 `PASS`（含两条会在 root 用户下自动跳过的 `chmod 0` 权限测试——`TestJavaAnUnreadablePomIsReported`、`TestJavaAnUnreadableGradleBuildFileIsReported`）。

Run: `go test ./internal/runcmd/`
Expected: `PASS`。

- [ ] **Step 6: 提交**

```bash
git add internal/runcmd/lang_java.go internal/runcmd/lang_java_test.go internal/runcmd/runcmd.go internal/runcmd/helper_test.go
git commit -m "$(cat <<'EOF'
新增：runcmd 的 Java 适配器——只认配了插件的 Spring Boot 项目

Java 没有通用的"运行这个项目"命令，唯一有标准答案的是 spring-boot-maven-plugin
（spring-boot:run）和 Gradle 的 org.springframework.boot 插件（bootRun）——只
看依赖里有没有 spring-boot-starter 不够，那只说明用了 Spring Boot 的库，不代表
配了能跑的插件目标。多模块工程（Maven 的 <modules>、Gradle 的 include）要先于
"是不是 Spring Boot"判断，否则聚合工程的插件在子模块里、会被误报成没配插件。
Maven/Gradle 都优先用项目自带的 wrapper（版本由项目锁定），wrapper 丢了可执行
位（Windows 上 clone、解压 zip 都会）时退化成 sh <wrapper>。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Python 与 Ruby 适配器（刻意收窄）

**背景：** 这两种语言没有"标准的运行方式"这种东西——Flask、FastAPI、纯脚本的入口都是开发者自己起的名字，猜错了还能跑起来，比直接报错更难排查（"设计决定"呼应 spec 本身的态度：宁可让用户手写 `runCommand`）。所以只认最强的信号：Django 的 `manage.py`、Rails 的 `bin/rails` + `Gemfile`。两者都要显式绑 `0.0.0.0`（"设计决定"第 3 条），Python 还要带 `PYTHONUNBUFFERED=1`（第 4 条）。

**Files:**
- Create: `internal/runcmd/lang_python.go`
- Create: `internal/runcmd/lang_ruby.go`
- Create: `internal/runcmd/lang_python_ruby_test.go`
- Modify: `internal/runcmd/runcmd.go`（`adapters` 表追加两行，七个语言适配器至此全部到位）
- Modify: `internal/runcmd/runcmd_test.go`（补上依赖"全部七种语言都注册了"的驱动层测试——`detect` 对 `Hints.Language` 的校验先于读 `RunCommand`，所以哪怕只是手写命令、只是声明 `language: python`，也要 Python 适配器已经在 `adapters` 表里；断言报错文字里带着 "ruby" 的测试更是要等最后一个语言落地）

**Interfaces:**
- Consumes：Task 1 的 `scan`/`outcome`/`problemOutcome`。
- Produces：`probePython`、`pythonEnv`、`probeRuby`。这是最后两个适配器——本任务完成后 `Languages()` 返回全部七种语言，`adapters` 表定稿。

- [ ] **Step 1: 写 `lang_python.go`**

```go
package runcmd

import "strings"

// probePython 刻意收窄：只认 Django 的 manage.py。Python 没有"这个项目怎么启动"的通用约定
// （Flask、FastAPI 的入口都是用户自己起的名字），猜错了还能跑起来比报错更难排查，
// 所以其余情况一律交给用户手写 runCommand。
//
// runserver 显式绑 0.0.0.0：它默认只听 127.0.0.1，而容器经 host-gateway 访问宿主机上的进程，
// 走的不是回环地址，只听回环的进程对容器不可达。
func probePython(s *scan) outcome {
	text, present, _ := s.read("manage.py")
	if !present || !strings.Contains(strings.ToLower(text), "django") {
		return outcome{}
	}
	python, venv := s.pythonInterpreter()
	return s.ok([]string{python, "manage.py", "runserver", "0.0.0.0:" + s.port()},
		evidenceOf("manage.py", venv)...)
}

// pythonInterpreter 优先用项目自己的虚拟环境（.venv、venv），没有才用 PATH 里的 python。
// 第二个返回值是用到的虚拟环境解释器的相对路径（没用则为空）。
func (s *scan) pythonInterpreter() (string, string) {
	candidates := []string{".venv/bin/python", "venv/bin/python"}
	fallback := "python3"
	if s.windows() {
		candidates = []string{".venv/Scripts/python.exe", "venv/Scripts/python.exe"}
		fallback = "python"
	}
	for _, c := range candidates {
		if s.isFile(c) {
			return s.path(c), c
		}
	}
	return fallback, ""
}

// pythonEnv：子进程的 stdout 是管道而不是终端，Python 会因此整块缓冲，日志要攒够一大块才出来。
// PYTHONUNBUFFERED=1 让它逐行输出（foreman 一类的工具都这么做）。
func pythonEnv(*scan) []string {
	return []string{"PYTHONUNBUFFERED=1"}
}
```

- [ ] **Step 2: 写 `lang_ruby.go`**

```go
package runcmd

// probeRuby 刻意收窄，理由同 Python：只认 Rails（bin/rails + Gemfile）。
//
// 用 `ruby bin/rails` 而不是直接执行 bin/rails：binstub 靠 shebang 与可执行位，Windows 上都没有。
// -b 0.0.0.0 是必须的：Rails 开发服务器默认只听回环地址，容器经 host-gateway 够不到它。
func probeRuby(s *scan) outcome {
	if !s.isFile("bin/rails") || !s.isFile("Gemfile") {
		return outcome{}
	}
	return s.ok([]string{"ruby", "bin/rails", "server", "-b", "0.0.0.0", "-p", s.port()}, "bin/rails", "Gemfile")
}
```

- [ ] **Step 3: 接进 `adapters` 表（七个语言全部到位）**

把 `adapters` 表从 Task 4 结束时的样子改成最终版——跟已经验证过的完整 `runcmd.go` 逐字节一致：

```go
var adapters = []adapter{
	{LangGo, "go.mod", probeGo, nil},
	{LangRust, "Cargo.toml", probeRust, nil},
	{LangDotnet, "*.csproj", probeDotnet, nil},
	{LangNode, "package.json", probeNode, nil},
	{LangJava, "pom.xml / build.gradle", probeJava, javaEnv},
	{LangPython, "manage.py", probePython, pythonEnv},
	{LangRuby, "bin/rails", probeRuby, nil},
}
```

- [ ] **Step 4: 写测试**

```go
package runcmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const djangoManage = "#!/usr/bin/env python\nos.environ.setdefault('DJANGO_SETTINGS_MODULE', 'site.settings')\n"

// ---- Python（只认 Django） ----

func TestPythonRunsDjangoOnAllInterfaces(t *testing.T) {
	dir := write(t, map[string]string{"manage.py": djangoManage})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangPython, cmd.Language)
	assert.Equal(t, []string{"python3", "manage.py", "runserver", "0.0.0.0:18080"}, cmd.Argv)
	assert.Equal(t, []string{"manage.py"}, cmd.Evidence)
	assert.Equal(t, []string{"PYTHONUNBUFFERED=1"}, cmd.Env, "输出走管道，Python 会整块缓冲")
}

func TestPythonPrefersTheProjectsVirtualEnvironment(t *testing.T) {
	for _, venv := range []string{".venv", "venv"} {
		dir := write(t, map[string]string{"manage.py": djangoManage, venv + "/bin/python": ""})

		cmd := mustDetect(t, dir, Hints{})

		assert.Equal(t, filepath.Join(dir, venv, "bin", "python"), cmd.Argv[0], venv)
		assert.Equal(t, []string{"manage.py", venv + "/bin/python"}, cmd.Evidence, venv)
	}
}

func TestPythonDotVenvBeatsVenv(t *testing.T) {
	dir := write(t, map[string]string{"manage.py": djangoManage, ".venv/bin/python": "", "venv/bin/python": ""})

	assert.Equal(t, filepath.Join(dir, ".venv", "bin", "python"), mustDetect(t, dir, Hints{}).Argv[0])
}

func TestPythonOnWindowsUsesScriptsAndPlainPython(t *testing.T) {
	plain := write(t, map[string]string{"manage.py": djangoManage})
	cmd, err := detectAs(plain, "windows", Hints{})
	require.NoError(t, err)
	assert.Equal(t, []string{"python", "manage.py", "runserver", "0.0.0.0:18080"}, cmd.Argv)

	withVenv := write(t, map[string]string{"manage.py": djangoManage, ".venv/Scripts/python.exe": ""})
	cmd, err = detectAs(withVenv, "windows", Hints{})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(withVenv, ".venv", "Scripts", "python.exe"), cmd.Argv[0])
	assert.Equal(t, []string{"manage.py", ".venv/Scripts/python.exe"}, cmd.Evidence)

	// Unix 的 .venv/bin/python 在 Windows 上不算。
	unixVenv := write(t, map[string]string{"manage.py": djangoManage, ".venv/bin/python": ""})
	cmd, err = detectAs(unixVenv, "windows", Hints{})
	require.NoError(t, err)
	assert.Equal(t, "python", cmd.Argv[0])
}

func TestPythonAManagePyThatIsNotDjangoIsNotGuessedAt(t *testing.T) {
	dir := write(t, map[string]string{"manage.py": "# flask-script manager\nprint('hi')\n"})

	assert.Empty(t, problemsOf(t, dir, Hints{}), "认不准就不认：交给用户手写 runCommand")
}

func TestPythonTheOtherPopularEntryPointsAreDeliberatelyNotRecognised(t *testing.T) {
	// FastAPI / Flask / 纯脚本：入口是用户自己起的名字，没有约定可循。
	dir := write(t, map[string]string{
		"requirements.txt": "fastapi\nuvicorn\n",
		"app/main.py":      "app = object()\n",
		"pyproject.toml":   "[project]\nname = 'x'\n",
	})

	assert.Empty(t, problemsOf(t, dir, Hints{}))
}

// ---- Ruby（只认 Rails） ----

func TestRubyRunsRailsOnAllInterfaces(t *testing.T) {
	dir := write(t, map[string]string{"bin/rails": "#!/usr/bin/env ruby\n", "Gemfile": "source 'https://rubygems.org'\n"})

	cmd := mustDetect(t, dir, Hints{})

	assert.Equal(t, LangRuby, cmd.Language)
	assert.Equal(t, []string{"ruby", "bin/rails", "server", "-b", "0.0.0.0", "-p", "18080"}, cmd.Argv)
	assert.Equal(t, []string{"bin/rails", "Gemfile"}, cmd.Evidence)
	assert.Empty(t, cmd.Env)
}

func TestRubyNeedsBothTheBinstubAndTheGemfile(t *testing.T) {
	onlyBinstub := write(t, map[string]string{"bin/rails": ""})
	assert.Empty(t, problemsOf(t, onlyBinstub, Hints{}))

	onlyGemfile := write(t, map[string]string{"Gemfile": ""})
	assert.Empty(t, problemsOf(t, onlyGemfile, Hints{}))
}

func TestRubyIsTheSameOnWindows(t *testing.T) {
	dir := write(t, map[string]string{"bin/rails": "", "Gemfile": ""})

	cmd, err := detectAs(dir, "windows", Hints{})

	require.NoError(t, err)
	assert.Equal(t, "ruby", cmd.Argv[0], "binstub 靠 shebang，Windows 上没有，所以显式交给 ruby")
}
```

- [ ] **Step 5: 把依赖"全部七种语言都注册了"的驱动层测试追加进 `runcmd_test.go`**

这三条测试之前的每个任务都写不出来：`TestLanguagesAreCompleteAndInAFixedOrder` 断言 `Languages()` 返回全部七个语言、`TestAnUnknownLanguageIsRejected` 断言报错文字里带着最后一个语言 "ruby"、`TestALanguageGivenWithARunCommandStillContributesItsEnvironment` 需要 `LangPython`/`LangJava` 已经注册在 `adapters` 表里（`detect` 对 `Hints.Language` 的校验先于读 `RunCommand`，见 Task 1 的 `runcmd.go`）。现在七个语言全部到位，用 Edit 在 `runcmd_test.go` 文件末尾追加：

```go
func TestLanguagesAreCompleteAndInAFixedOrder(t *testing.T) {
	assert.Equal(t, []string{"go", "rust", "dotnet", "node", "java", "python", "ruby"}, Languages())
}

func TestALanguageGivenWithARunCommandStillContributesItsEnvironment(t *testing.T) {
	dir := t.TempDir()

	python := mustDetect(t, dir, Hints{Language: LangPython, RunCommand: []string{"uvicorn", "app:app"}})
	assert.Equal(t, LangPython, python.Language)
	assert.Equal(t, []string{"PYTHONUNBUFFERED=1"}, python.Env)

	java := mustDetect(t, dir, Hints{Language: LangJava, RunCommand: []string{"java", "-jar", "app.jar"}})
	assert.Equal(t, []string{"SERVER_PORT=18080"}, java.Env)

	goCmd := mustDetect(t, dir, Hints{Language: LangGo, RunCommand: []string{"air"}})
	assert.Equal(t, LangGo, goCmd.Language)
	assert.Empty(t, goCmd.Env, "Go 没有额外的环境变量")
}

// ---- 语言与参数的校验 ----

func TestAnUnknownLanguageIsRejected(t *testing.T) {
	dir := t.TempDir()

	_, err := detectAs(dir, "linux", Hints{Language: "cobol"})
	var unknown *UnknownLanguageError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "cobol", unknown.Language)
	assert.Contains(t, err.Error(), "cobol")
	assert.Contains(t, err.Error(), "ruby", "报错里列出所有支持的语言")

	_, err = detectAs(dir, "linux", Hints{Language: "cobol", RunCommand: []string{"x"}})
	assert.ErrorAs(t, err, &unknown, "手写了 runCommand 也要校验 language，别让拼错的值悄悄溜过去")
}
```

Run: `go test ./internal/runcmd/ -v -run 'TestPython|TestRuby|TestLanguagesAreCompleteAndInAFixedOrder|TestAnUnknownLanguageIsRejected|TestALanguageGivenWithARunCommandStillContributesItsEnvironment'`
Expected: 全部 `PASS`。

- [ ] **Step 6: gofmt + 全量测试**

Run: `gofmt -w internal/runcmd && go test ./internal/runcmd/`
Expected: `PASS`。

- [ ] **Step 7: 提交**

```bash
git add internal/runcmd/lang_python.go internal/runcmd/lang_ruby.go internal/runcmd/lang_python_ruby_test.go internal/runcmd/runcmd.go
git commit -m "$(cat <<'EOF'
新增：runcmd 的 Python 与 Ruby 适配器（只认 Django / Rails），七个语言适配器至此全部到位

Python、Ruby 没有"标准运行方式"这种东西——Flask/FastAPI 的入口是开发者自己起的
名字，猜错了还能跑起来，比直接报错更难排查，所以只认最强的信号：Django 的
manage.py、Rails 的 bin/rails + Gemfile，其余一律交给用户手写 runCommand。两者
都显式绑 0.0.0.0：mode: local 的裸进程经 host-gateway 被其他容器访问，走的不是
回环地址，两个框架默认都只监听 127.0.0.1，不显式绑就无法从容器那一侧访问到。
Python 额外带 PYTHONUNBUFFERED=1：procsup 把子进程输出接到管道而不是终端，
Python 检测到非 tty 会默认切换成整块缓冲，日志会被延迟到攒够一大块才冒出来。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: 集成测试、覆盖率、变异验证、收尾

**背景：** 前五个任务各自验证了"探测逻辑本身对不对"，这个任务验证"探测出来的命令真的能跑"——用这台机器上真实装着的 Go 和 Node 工具链，把 `Detect` 探测出的命令原样执行一遍，断言进程真的打印出了预期的输出，而不是停留在断言 `Argv` 的形状。再用仓库自己 `tests/components/` 下的真实固件组件（六个 Go 组件、两个 FastAPI 组件、一个只有测试没有 `main` 的前端模块）交叉核对，作为"探测规则在真实代码上到底准不准"的最后一道检查。

**Files:**
- Create: `internal/runcmd/integration_test.go`

**Interfaces:**
- Consumes：Task 1 的 `Detect`、`Command.CheckProgram`；`tests/components/` 下已经存在的固件组件（本计划不新建固件，直接读用现成的）。
- Produces：无新的生产代码，这个任务只加测试。

- [ ] **Step 1: 写集成测试**

```go
package runcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDetected 用真实的操作系统探测 dir，再把探测出的命令原样跑一遍，返回合并后的输出。
// 探测出的 argv 只有真的能跑，"认得出"才有意义——所以这里不停留在核对 argv 的形状上。
func runDetected(t *testing.T, dir string) string {
	t.Helper()
	cmd, err := Detect(dir, Hints{}, Params{Port: testPort})
	require.NoError(t, err)
	require.NoError(t, cmd.CheckProgram())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, cmd.Argv[0], cmd.Argv[1:]...)
	c.Dir = cmd.Dir
	c.Env = append(os.Environ(), append(cmd.Env, "NO_UPDATE_NOTIFIER=1", "npm_config_update_notifier=false")...)
	out, err := c.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func TestADetectedGoCommandReallyRuns(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	dir := write(t, map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"runcmd-go-ok\") }\n",
	})

	assert.Contains(t, runDetected(t, dir), "runcmd-go-ok")
}

func TestADetectedNodeCommandReallyRuns(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("这台机器没有 npm")
	}
	dir := write(t, map[string]string{
		"package.json": `{"name": "hello", "version": "1.0.0", "scripts": {"start": "node server.js"}}`,
		"server.js":    "console.log('runcmd-node-ok');\n",
	})

	assert.Contains(t, runDetected(t, dir), "runcmd-node-ok")
}

// 仓库自己的自测组件是最真实的样本：六个 Go 组件、两个 FastAPI 组件、一个只有测试的 Go 模块。
// 这里同时记下"刻意不认"的边界——FastAPI 没有约定可循，得由用户手写 local.runCommand。
func TestTheRepositorysOwnTestComponents(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "components")

	for _, name := range []string{
		"demo-hello", "demo-caller", "auth-password-login", "authorization-rbac",
		"department-tree", "erp-backend", "infra-api-docs",
	} {
		cmd := mustDetect(t, filepath.Join(root, name), Hints{})
		assert.Equal(t, []string{"go", "run", "."}, cmd.Argv, name)
	}

	for _, name := range []string{"people-basic", "infra-redis-event-bus"} {
		assert.Empty(t, problemsOf(t, filepath.Join(root, name), Hints{}), name)
	}

	assert.Equal(t, []Problem{{Language: "go", Reason: ReasonNoEntryPoint, Detail: "go.mod"}},
		problemsOf(t, filepath.Join(root, "portal-user-frontend"), Hints{}))
}
```

- [ ] **Step 2: 跑一遍，确认真实工具链的用例真的执行了（不是被跳过）**

Run: `go test ./internal/runcmd/ -v -run 'ReallyRuns|RepositorysOwn'`
Expected:

```
=== RUN   TestADetectedGoCommandReallyRuns
--- PASS: TestADetectedGoCommandReallyRuns (0.17s)
=== RUN   TestADetectedNodeCommandReallyRuns
--- PASS: TestADetectedNodeCommandReallyRuns (0.20s)
=== RUN   TestTheRepositorysOwnTestComponents
--- PASS: TestTheRepositorysOwnTestComponents (0.01s)
PASS
```

（具体耗时会因机器而异，但都应该是零点几秒级别，不是 0.00s——0.00s 说明测试被 `t.Skip` 跳过了，没有真的启动进程。这台机器装了 `go`（`go1.2x`）和 `npm`（`11.x`），两条用例都会真的执行到；缺 `go`/`npm` 的机器上这两条各自单独 skip，不影响其余测试。）

- [ ] **Step 3: 覆盖率**

Run: `go test ./internal/runcmd/ -coverprofile=/tmp/runcmd.cover -count=1 && go tool cover -func=/tmp/runcmd.cover | tail -5`
Expected: `total:` 那一行在 99% 以上。**已知有一行覆盖不到、不用补测试**：`runcmd.go` 里 `filepath.Abs` 返回错误的分支——这个错误只在 `os.Getwd()` 失败时才会发生（比如进程当前工作目录被外部删除），没有可移植、不依赖破坏测试环境本身的方式触发它，留着比硬凑一个脆弱的测试更诚实。

- [ ] **Step 4: `-race` 连跑三遍**

Run: `go test -race -count=3 ./internal/runcmd/`
Expected: `ok`，退出码 0。（本包不启动 goroutine 做并发工作，这一步主要是排除"共享 `s.entries` 切片被测试间意外修改"一类的隐患。）

- [ ] **Step 5: 五个平台的 `vet`（发布矩阵 + Plan 2 新增的 `darwin/amd64`）**

Run 五遍，`GOOS` 依次是 `linux/amd64`、`linux/arm64`、`darwin/arm64`、`darwin/amd64`、`windows/amd64`：

```bash
GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go vet ./internal/runcmd/
```

Expected: 每一遍退出码都是 0。`make check-cross-build` 本身只覆盖 `linux/amd64`、`darwin/arm64`、`windows/amd64` 三个（Plan 2 定的），这一步多测的 `linux/arm64`、`darwin/amd64` 是额外确认，不是这个包引入了新的平台相关代码才需要——它确实没有引入任何 `//go:build` 约束。

- [ ] **Step 6: 完整 lint**

Run: `make lint > /tmp/lint.log 2>&1; echo "exit=$?"`
Expected: `exit=0`。

- [ ] **Step 7: 提交测试文件**

```bash
git add internal/runcmd/integration_test.go
git commit -m "$(cat <<'EOF'
新增：runcmd 的集成测试——真实跑一遍探测出的命令，交叉核对仓库自己的固件组件

前五个任务验证的是"探测逻辑本身对不对"，这个任务验证"探测出的命令真的能跑"：
用这台机器上真实装着的 go/npm 工具链，把 Detect 探测出的 Argv 原样执行，断言
进程真的打印出了预期输出。另外用 tests/components/ 下已有的真实固件组件（六个
Go 组件、两个 FastAPI 组件、一个没有 main 包的前端模块）交叉核对——FastAPI 两个
组件按设计不被识别（Python 只认 Django），这也是需要断言到的预期行为，不是遗漏。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 8: 回写执行结果，收尾提交**

执行完前 7 个 Step 后，在本文件末尾追加"执行结果与遗留"一节（内容真实反映实际跑出来的数字：覆盖率百分比、`-race` 耗时、`make lint` 的结果、有没有偏离计划的地方），把 Self-Review Checklist 逐条打勾，然后单独提交这次文档更新：

```bash
git add docs/superpowers/plans/2026-09-22-run-command-detection.md
git commit -m "$(cat <<'EOF'
文档：Plan 3（启动命令自动识别）的执行结果回写进计划

七个语言适配器全部完成，集成测试覆盖真实工具链与仓库自己的固件组件。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Checklist（执行完 6 个任务后逐条核对）

- [ ] `go test ./internal/runcmd/ -v` 全绿，没有任何 `SKIP`（除了这台机器本来就缺的工具链，如果有的话）
- [ ] `go test -race -count=3 ./internal/runcmd/` 干净
- [ ] 覆盖率 99% 以上，唯一的缺口（`filepath.Abs` 错误分支）有说明，不是漏测
- [ ] 五个平台的 `go vet` 全部退出码 0
- [ ] `make check-cross-build` 退出码 0
- [ ] `make lint` 完整跑一遍，退出码 0
- [ ] `Languages()` 返回全部七种语言，顺序固定：`go, rust, dotnet, node, java, python, ruby`
- [ ] 手写 `RunCommand` 时完全不读文件系统（不探测），但显式声明的 `Language` 仍然带出对应的 `Env`
- [ ] `Detect` 对不存在的目录、不是目录的路径、读不了的目录，三种情形分别返回三种不同的普通 `error`（不是本包的结构化类型），跟"认得出语言但给不出命令"（`*NoCommandError`）、"两种语言都能跑"（`*AmbiguousError`）、"language 拼错了"（`*UnknownLanguageError`）区分开
- [ ] `internal/runcmd` 没有被 `internal/` 下任何其他包引用（`grep -rn '"github.com/brickkit/brickkit/internal/runcmd"' internal/ cmd/` 除了本包自己的测试文件之外没有命中）——这是本计划"不接线"的验收标准，跟 Plan 2 的 `procsup`/`sessionlock` 一样
- [ ] `AGENTS.md`、`docs/en/`、`docs/zh/` 没有任何改动
- [ ] `go.mod`、`go.sum` 没有任何改动
- [ ] 没有写死带中文的字符串字面量在生产代码里（`go test ./tests/i18nguard/` 通过）

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-22-run-command-detection.md`. Two execution options:

1. **Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

选哪种？
