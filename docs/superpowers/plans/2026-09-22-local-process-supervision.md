# 本地进程监管（procsup 与 sessionlock）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增两个**先不接线**的内部包——`internal/procsup`（前台监管一组裸进程：进程组 / Job Object、带前缀的合并输出、先体面停止再强杀、退出情况（含崩溃时最后几行输出，行数可调）、一次性端口监听探测）与 `internal/sessionlock`（项目级会话锁，靠操作系统文件锁，持有者一死自动释放）——外加一个交叉编译守卫，让平台相关代码不会在 Windows / macOS 上悄悄编不过。

**Architecture:** `procsup` 把每个子进程放进独立的进程组（Unix：`Setpgid`，信号发给整组；Windows：Job Object，关闭即整棵树收掉），所有子进程的 stdout / stderr 共用一根管道、按行加名字前缀后汇到同一个 `io.Writer`，收尾时先 `SIGTERM`（Windows：`CTRL_BREAK_EVENT`）、宽限期后强杀。一个进程崩了，整个会话随即收尾，并把崩溃的现场（退出码、信号、最后几行输出，行数可调）留在退出记录里；不做重启、不做健康检查、不跨会话存活。`sessionlock` 用 `flock` / `LockFileEx` 实现项目级锁，"有没有会话"看的是锁本身而不是 PID 文件里的号码。两个包都是纯库，不依赖 `internal` 里的任何别的包，Plan 4 再把它们接进 `up` / `status` / `down`。

**Tech Stack:** Go 1.22 标准库（`os/exec`、`syscall`、`net`、`context`）；Windows 侧用 `golang.org/x/sys/windows`（已在 `go.sum` 里，本来是 cobra 的传递依赖，这次起成为直接依赖）；测试用 testify。

**Spec:** `docs/superpowers/specs/2026-09-21-mode-field-and-local-execution-design.md` 的 §3（本地进程监管）、§4 最后几条（端口监听检测）、§6.4（两种失败分别处理）、§6.5（验证范围）。

## Global Constraints

- Go 版本下限 **1.22**（`go.mod`）。
- 新增第三方依赖只有一个：`golang.org/x/sys`（v0.18.0，`go.sum` 里已有；`go.mod` 里去掉它的 `// indirect`）。不引入任何别的库。
- 发布矩阵是 `linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64`，`CGO_ENABLED=0`（`Makefile` 的 `RELEASE_PLATFORMS`）——新代码必须在这五个目标上都编得过。
- **验证范围（spec §6.5）**：只在 Linux 上实际验证；Windows / macOS 只要理论上站得住、并且能编过就行，不强求真机跑通。"编得过"由 Task 1 的 `make check-cross-build` 守着。
- 生产代码里不许写死带中文的字符串字面量（`tests/i18nguard`）。本计划的代码没有面向用户的文字：库返回的 error 是英文、用 `%w` 包着，等 Plan 4 在 CLI 层用 `i18n.T(msgid.X)` 包成 `clierr`。注释可以写中文（仓库惯例）。
- 子进程的 stdout / stderr 走**管道**，不走 PTY；stdin 是空设备。子进程的环境变量只经由 `exec.Cmd.Env` 在内存里传，不落盘（spec §6.2）。
- 仓库惯例：`defer func() { _ = x.Close() }()`；测试与被测代码同包；测试名用英文、注释用中文；提交信息用中文；提交前跑**完整**的 `make lint`（只跑 `go vet` + `go test` 会漏东西）；`go test -race` 必须干净。
- **不接线**：本计划不改任何 CLI 命令、不改 `brickkit.yaml` / `component.yaml` 的字段、不新增 `mode: local`。因此**不改 `AGENTS.md` 与任何 `docs/`**——文档里不能先出现一个还不存在的模式。这些是 Plan 4 的事。
- `docs/{en,zh}/07-patterns/05-deployment-selection-guide.md` 本次不改（另一个项目会先做完整实操测试，根据反馈再更新）。
- 判断退出码：这台机器是 zsh，`${PIPESTATUS[0]}` 不存在，`make lint | grep` 会把失败看成成功。写成 `make lint > /tmp/lint.log 2>&1; echo "exit=$?"` 再 `tail /tmp/lint.log`。
- 提交命令直接从 `git` 开始，别加 `cd` 前缀（本会话就在仓库根目录）。

---

## 这份计划在四份里的位置

| 计划 | 内容 | 状态 |
| --- | --- | --- |
| Plan 1 | `enabled` + `local` 合并成 `mode` 字段（`mode: debug` 取代 `local: true`） | 已完成，提交在 `worktree-mode-field-migration` 分支 |
| **Plan 2（本计划）** | 本地进程监管：`procsup`、`sessionlock`、交叉编译守卫 | 待执行 |
| Plan 3 | 语言 / 启动命令自动识别（`component.yaml` 的 `local:` 块，各语言适配器） | 待写 |
| Plan 4 | `mode: local` 整合：字段与校验、`up` 前台监管模式、端口分配与覆盖、`PORT` 保留变量、`graph` / `status` / `down` 展示、`check-guides` 的 `local` 层、文档 | 待写 |

本计划交付的是"Plan 4 可以直接调用的两个库"。spec §3 里的下列条目**不在这里做**，因为它们要改 CLI 命令的行为，属于 Plan 4：只在存在 `mode: local` 组件时才切前台模式、docker ↔ local 切换（复用孤儿清理）、`status` / `down` 读锁后打印提示、按拓扑序启动（本计划的 `Start` 一个个调用就是它的基础）。

## 设计决定（spec 没写死、这份计划里定下的）

1. **一个进程崩了，整个会话收尾，只把崩溃的那个进程打印出来。**（用户 2026-09-22 的决定，spec §3 已同步。）这里的"进程"指 `procsup` 监管的进程——也就是 Plan 4 里 `mode: local` 组件被 brickkit 自己拉起的那些裸进程；`mode: debug` 的进程是用户自己在 IDE 里启动的，brickkit 不监管、也看不见它崩没崩，docker / k8s 的容器由 dockerd / kubelet 管，都不进这个监管器。"崩" = 不是我们叫它停的、并且退出码非零（含被外部信号杀死，比如 OOM killer）；自己干净退出（退出码 0）不算崩溃，也不会连累别人。有进程崩了，其余被监管的进程被体面地停掉；容器不受影响——`up` 本来就不管容器的生死，跟 spec §3"docker/k8s 部分不受影响"一致。**告诉用户什么崩了，只做一次、只针对崩溃的那个**：收尾结束之后，在最后一屏打印 `Exits()` 里 `Crashed()` 为真的进程——组件名、退出码或信号、跑了多久，以及它最后的若干行输出（`Exit.Tail`）；被我们叫停的、干净退出的都不打印。没有"崩溃时立刻打一行"的回调（`OnExit` 已经拿掉，库更简单）：收尾很快，而且收尾时其余进程的关闭输出会刷屏、把崩溃现场冲出屏幕，所以汇总必须放在**最后**。**行数可调**：库用 `Options.TailLines`（≤ 0 取 `DefaultTailLines` = 20），内部是环形缓冲，每来一行 O(1)。给用户的旋钮属于 Plan 4，建议是 `brickkit up --crash-lines N`（默认 20；0 = 只打印崩溃信息、不打印输出行）——这是新增的 CLI 表面，Plan 4 写之前先跟用户确认名字与位置。**启动阶段用的是同一套机制**：命令刚拉起就退出，或者已经起来的某个进程在别的进程还在启动时崩了，Supervisor 都会收尾，之后的 `Start` 返回 `ErrStopped`。Plan 4 判断"谁是元凶"要看 `Exits()` 里哪些 `Crashed()`，不能看 `WaitListening` 的结果：被我们叫停的进程它也会返回 `ProbeExited`，但那不是它自己崩的。
2. **输出走管道，不走 PTY。** 加前缀必须拦下输出。代价：子进程看到的 stdout 不是终端，有些语言会因此改变行为——Python 默认整块缓冲（输出会延迟）、很多工具会关掉彩色。库不替语言擦屁股：这属于 Plan 3 的语言适配器（例如 Python 适配器产出的环境变量里带上 `PYTHONUNBUFFERED=1`）。
3. **不用 Linux 的 `Pdeathsig`。** 它绑的是"创建子进程的那个**线程**"而不是进程，Go 运行时调度线程时会出现误杀。代价：brickkit 被 `kill -9` / OOM 杀掉时，Unix 上的子进程会变成孤儿（Windows 的 Job Object 能兜住）。正常的三种收尾——Ctrl+C、`kill`、关终端——由 `StopSignals` 覆盖，并且有用真信号驱动的测试。
4. **收尾时无条件再对进程组发一遍 `SIGKILL`。** 领头进程可能已经自己退了，组里却还留着不理睬 `SIGTERM` 的孙进程；只等领头进程会漏。
5. **会话锁的探测有微秒级窗口。** 探测 = 试着拿一下锁再放掉，恰好在这一瞬间调用 `Acquire` 的会话会被误判成"已被持有"，重试一次即可。不为它引入别的机制（比如 PID 文件），那会带回陈旧问题。
6. **Windows 入 Job 有一个已知缺口。** `os/exec` 没法以挂起状态创建进程，所以"启动到入 Job"之间那几微秒里拉起的孙进程会逃出 Job。真实的启动器不会在第一微秒就 fork，接受它。
7. **端口探测里，进程退出优先于监听。** 端口被别的东西占着、而我们的进程因为 "address in use" 死掉时，要报"退出"，不能被那个占着端口的东西骗成"成功"。盲区：进程还活着但没绑上、端口又被别人占着，探测不出来（Plan 4 默认给 `local` 组件分配空闲端口，降低这个风险）。
8. **两个包，不是一个。** 监管与会话锁职责不同、被不同命令用（`up` 两个都用，`status` / `down` 只读锁），分开更清楚。

## File Structure

- Create: `internal/procsup/prefix.go`——按行加前缀、把多个进程的输出汇到同一个 Writer（纯逻辑，无平台差异）
- Create: `internal/procsup/supervisor.go`——`Supervisor` / `Spec` / `Options` / `Exit` / `Proc`：启动、等待、收尾
- Create: `internal/procsup/proc_unix.go`——进程组（`//go:build unix`）
- Create: `internal/procsup/proc_windows.go`——Job Object（`//go:build windows`）
- Create: `internal/procsup/probe.go`——`WaitListening`
- Create: `internal/procsup/helper_test.go`、`prefix_test.go`、`supervisor_test.go`、`supervisor_unix_test.go`、`probe_test.go`
- Create: `internal/sessionlock/lock.go`、`lock_unix.go`、`lock_windows.go`、`lock_test.go`
- Modify: `Makefile`——新增 `check-cross-build` 并挂进 `lint`
- Modify: `go.mod`——`golang.org/x/sys` 去掉 `// indirect`
- Modify: `docs/superpowers/plans/2026-09-22-local-process-supervision.md`（本文件）——收尾时回写执行结果

---

## Task 1: 交叉编译守卫 `make check-cross-build`

**背景：** 这个仓库到今天为止**没有任何平台相关的代码**，所以 Windows / macOS 能编过纯属"全是纯 Go"的巧合，没有任何东西守着。Task 3 起会引入 `syscall.Kill`、`Setpgid`、`flock` 这类只在 Unix 上存在的东西，以及 Windows 专属的 Job Object——日常开发只在 Linux 上，如果没有守卫，编不过的问题要到 `make release-artifacts` 那一刻才暴露。守卫必须**先于**平台相关代码落地，这样后面每个任务的 `make lint` 都在它的保护下。我已在现有代码上验证过：`go build` 与 `go vet`（含全部测试文件）对 `./cmd/...` `./internal/...` 在 linux / darwin / windows 上本来就是干净的，所以守卫可以一次覆盖整个 `internal`。

**Files:**
- Modify: `Makefile`（第 130 行 `lint` 的依赖与说明；在 `check-no-binaries` 目标后面新增一段）

**Interfaces:**
- Produces: `make check-cross-build`——对 `linux/amd64`、`darwin/arm64`、`windows/amd64` 各跑一遍 `go build` 与 `go vet`（范围 `./cmd/... ./internal/...`，`CGO_ENABLED=0`）。Task 3、Task 5 依赖它来证明 Windows / macOS 编得过。

- [ ] **Step 1: 新增目标，并挂进 `lint`**

把 `Makefile` 里 `lint:` 那一行的依赖 `check-i18n cover-check` 改成 `check-i18n check-cross-build cover-check`，说明里的 `多语言守卫 + 覆盖率门槛` 改成 `多语言守卫 + 三平台可编译 + 覆盖率门槛`。改完这一行是：

```make
lint: check-docs check-cli-docs check-doc-tree check-doc-fields check-schemas check-docs-bilingual check-market-api check-install-sh check-no-binaries check-guide-output check-i18n check-cross-build cover-check ## 静态检查（文档引用 + 命令/参数防伪造 + .brickkit/ 目录树 + 字段骨架与字段参考 + JSON Schema 与结构体一致 + 双语镜像 + 市场 API 表 + 安装脚本 + 仓库无二进制 + 教程输出核对 + 多语言守卫 + 三平台可编译 + 覆盖率门槛）
```

再在 `check-no-binaries` 目标（`@python3 scripts/check-no-binaries.py` 那一行）之后、`# check-i18n 守住…` 那段注释之前，插入：

```make
# 每个 GOOS 一个目标就够：平台相关的代码按操作系统分文件（proc_unix.go / proc_windows.go），不按 CPU 架构分。
# 发布矩阵里有 windows 与 darwin，而日常开发只在 Linux 上——不守着的话，一处只在 Linux 上编得过的
# 系统调用要到 make release-artifacts 那一刻才暴露。vet 顺带把测试文件也编一遍。
CROSS_TARGETS := linux/amd64 darwin/arm64 windows/amd64

.PHONY: check-cross-build
check-cross-build: ## 三个操作系统都编得过、测试文件也过 vet（平台相关代码不能只在 Linux 上编得过）
	@for target in $(CROSS_TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "▶ GOOS=$$os GOARCH=$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build ./cmd/... ./internal/... || exit 1; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) vet ./cmd/... ./internal/... || exit 1; \
	done
	@echo "✅ linux / darwin / windows 都编得过"
```

（配方行必须用 Tab 缩进，不是空格。）

- [ ] **Step 2: 跑守卫，确认现在能过**

Run: `make check-cross-build`
Expected: 退出码 0，输出恰好是

```
▶ GOOS=linux GOARCH=amd64
▶ GOOS=darwin GOARCH=arm64
▶ GOOS=windows GOARCH=amd64
✅ linux / darwin / windows 都编得过
```

- [ ] **Step 3: 变异验证——确认守卫真的会红**

一个从来没红过的守卫和不存在一样。创建 `internal/config/zz_crossbuild_probe.go`：

```go
package config

import "syscall"

var _ = syscall.Kill
```

Run: `make check-cross-build`
Expected: **失败**（退出码非 0）。linux、darwin 两轮通过，windows 一轮报：

```
# github.com/brickkit/brickkit/internal/config
internal/config/zz_crossbuild_probe.go:5:17: undefined: syscall.Kill
make: *** [Makefile:168: check-cross-build] Error 1
```

然后删掉这个文件，再跑一次确认回到通过：

Run: `rm internal/config/zz_crossbuild_probe.go && make check-cross-build`
Expected: 退出码 0。

- [ ] **Step 4: 完整 lint**

Run: `make lint > /tmp/lint.log 2>&1; echo "exit=$?"`
Expected: `exit=0`，`/tmp/lint.log` 里能看到 `✅ linux / darwin / windows 都编得过`。

- [ ] **Step 5: 提交**

```bash
git add Makefile
git commit -m "$(cat <<'EOF'
新增：make check-cross-build——三个操作系统都得编得过，测试文件也要过 vet

这个仓库此前没有任何平台相关的代码，Windows / macOS 能编过只是因为全是纯 Go，
没有东西守着。接下来要引入 syscall.Kill、Setpgid、flock 这类 Unix 专属的调用和
Windows 的 Job Object，日常开发只在 Linux 上，不守着的话编不过的问题要到 make
release-artifacts 才暴露。每个 GOOS 一个目标就够（平台相关代码按操作系统分文件），
并挂进 lint。已用一个故意引入 syscall.Kill 的文件做过变异验证：windows 一轮会红。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: 输出前缀写入器

**背景：** 多个进程的输出要合并到同一个终端，每行前面带上组件名（参考 foreman / overmind）。这是纯逻辑，没有任何平台差异，也不需要真的启动进程，所以最先做、最容易测。四个要点：① 一行一行地写，多个进程的行不会互相穿插；② 一行可能被拆成多次 `Write`（管道读到多少写多少），要按换行符重新拼；③ `Write` **永远不能返回错误**——`exec` 的拷贝 goroutine 一旦遇到写错误就会停止读管道，子进程随后会在写满管道时被卡死，不值得为终端上的一行输出把整个进程卡住；④ 每个进程留下**最近若干行输出**（`Tail`，行数由调用方定；用环形缓冲，每来一行 O(1)，行数调得再大也不拖慢输出）——收尾时其余进程的关闭输出会刷屏，把崩溃的现场冲出屏幕，所以要把最后几行留在 `Exit` 里，由调用方在最后一屏重新打出来。

**Files:**
- Create: `internal/procsup/prefix.go`
- Test: `internal/procsup/prefix_test.go`

**Interfaces:**
- Produces（同包内，Task 3 使用）：
  - `func prefixFor(name string, width int) string`——生成 `"<name 补齐到 width> | "`
  - `type lineSink struct{ mu sync.Mutex; out io.Writer }` 与 `func (s *lineSink) writeLine(prefix string, line []byte)`——一次写一整行，行间互斥
  - `type prefixWriter struct{ sink *lineSink; prefix string; tailCap int; ... }`（`tailCap` 是 `Tail` 最多留多少行，≤ 0 表示不留），实现 `io.Writer`，另有 `func (w *prefixWriter) Flush()`——把没遇到换行的尾巴当成一行吐出去；`func (w *prefixWriter) Tail() []string`——最近至多 `tailCap` 行输出（不带前缀，最旧的在前，返回副本）
  - `const maxLine = 64 * 1024`

- [ ] **Step 1: 写失败的测试**

创建 `internal/procsup/prefix_test.go`：

```go
package procsup

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tailForTest 是这个文件里的测试给 Tail 留的行数。默认行数常量要到 Task 3 才有，这里不依赖它。
const tailForTest = 20

func newPrefixWriter(prefix string) (*prefixWriter, *bytes.Buffer) {
	var out bytes.Buffer
	return &prefixWriter{sink: &lineSink{out: &out}, prefix: prefix, tailCap: tailForTest}, &out
}

func TestPrefixForPadsNamesToWidth(t *testing.T) {
	assert.Equal(t, "web | ", prefixFor("web", 0))
	assert.Equal(t, "web    | ", prefixFor("web", 6))
	assert.Equal(t, "people/basic | ", prefixFor("people/basic", 6), "比宽度长的名字原样保留")
}

func TestPrefixWriterPrefixesEveryLine(t *testing.T) {
	w, out := newPrefixWriter("x | ")

	_, err := w.Write([]byte("a\nb\n"))

	require.NoError(t, err)
	assert.Equal(t, "x | a\nx | b\n", out.String())
}

func TestPrefixWriterJoinsLinesSplitAcrossWrites(t *testing.T) {
	w, out := newPrefixWriter("x | ")

	for _, chunk := range []string{"hel", "lo\nwor", "ld\n"} {
		_, err := w.Write([]byte(chunk))
		require.NoError(t, err)
	}

	assert.Equal(t, "x | hello\nx | world\n", out.String())
}

func TestPrefixWriterFlushEmitsTheUnterminatedTail(t *testing.T) {
	w, out := newPrefixWriter("x | ")
	_, _ = w.Write([]byte("tail"))
	assert.Empty(t, out.String(), "没有换行之前不该输出")

	w.Flush()

	assert.Equal(t, "x | tail\n", out.String())
	w.Flush()
	assert.Equal(t, "x | tail\n", out.String(), "重复 Flush 不该再吐东西")
}

func TestPrefixWriterStripsCarriageReturnAndKeepsBlankLines(t *testing.T) {
	w, out := newPrefixWriter("x | ")

	_, _ = w.Write([]byte("a\r\n\nb\n"))

	assert.Equal(t, "x | a\nx | \nx | b\n", out.String())
}

func TestPrefixWriterSplitsOverlongLines(t *testing.T) {
	w, out := newPrefixWriter("x | ")

	_, _ = w.Write([]byte(strings.Repeat("a", maxLine*2+10) + "\n"))

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Len(t, lines[0], len("x | ")+maxLine)
	assert.Len(t, lines[1], len("x | ")+maxLine)
	assert.Len(t, lines[2], len("x | ")+10)
}

func TestPrefixWriterKeepsTheMostRecentLinesForTroubleshooting(t *testing.T) {
	w, _ := newPrefixWriter("x | ")
	for i := 0; i < tailForTest+5; i++ {
		_, _ = w.Write([]byte(fmt.Sprintf("line-%d\n", i)))
	}
	_, _ = w.Write([]byte("unfinished"))
	w.Flush()

	tail := w.Tail()

	require.Len(t, tail, tailForTest, "只留最近的 tailCap 行")
	assert.Equal(t, "line-6", tail[0], "更早的被挤掉了")
	assert.Equal(t, "line-24", tail[len(tail)-2])
	assert.Equal(t, "unfinished", tail[len(tail)-1], "没换行的尾巴 Flush 之后也算一行")
}

// 行数由调用方定。环形缓冲绕了一圈又一圈之后，"最旧的在前"的顺序不能乱。
func TestPrefixWriterTailHonoursItsCapacityAcrossWrapArounds(t *testing.T) {
	cases := []struct {
		name  string
		cap   int
		lines int
		want  []string
	}{
		{"只留最后一行", 1, 4, []string{"l3"}},
		{"没填满", 5, 3, []string{"l0", "l1", "l2"}},
		{"刚好填满", 3, 3, []string{"l0", "l1", "l2"}},
		{"绕过头两圈之后顺序仍然是最旧的在前", 3, 8, []string{"l5", "l6", "l7"}},
		{"容量是 0 就什么都不留", 0, 4, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &prefixWriter{sink: &lineSink{out: io.Discard}, prefix: "x | ", tailCap: c.cap}
			for i := 0; i < c.lines; i++ {
				_, _ = w.Write([]byte(fmt.Sprintf("l%d\n", i)))
			}

			assert.Equal(t, c.want, w.Tail())
		})
	}
}

func TestPrefixWriterTailIsEmptyForASilentProcessAndIsACopy(t *testing.T) {
	w, _ := newPrefixWriter("x | ")
	assert.Empty(t, w.Tail())

	_, _ = w.Write([]byte("only\n"))
	got := w.Tail()
	got[0] = "tampered"

	assert.Equal(t, []string{"only"}, w.Tail(), "改返回值不能影响内部状态")
}

func TestPrefixWriterNeverFailsTheChild(t *testing.T) {
	w := &prefixWriter{sink: &lineSink{out: failingWriter{}}, prefix: "x | "}

	n, err := w.Write([]byte("hello\n"))

	require.NoError(t, err, "终端写失败不能连累子进程")
	assert.Equal(t, 6, n)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("closed") }

// 并发写同一个 sink：每一行都得完整，前缀不能被别的进程的输出截断。
func TestPrefixWritersSharingASinkNeverInterleaveWithinALine(t *testing.T) {
	var out bytes.Buffer
	sink := &lineSink{out: &out}
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c"} {
		w := &prefixWriter{sink: sink, prefix: name + " | "}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				// 故意把一行切成两次写，逼出"写到一半被别人插队"的可能
				_, _ = w.Write([]byte(fmt.Sprintf("%s-", name)))
				_, _ = w.Write([]byte(fmt.Sprintf("%d\n", i)))
			}
		}()
	}
	wg.Wait()

	lineRe := regexp.MustCompile(`^([abc]) \| ([abc])-\d+$`)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	assert.Len(t, lines, 600)
	for _, line := range lines {
		m := lineRe.FindStringSubmatch(line)
		require.NotNil(t, m, "行被弄坏了：%q", line)
		assert.Equal(t, m[1], m[2], "前缀与内容不是同一个进程的：%q", line)
	}
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test ./internal/procsup/ 2>&1 | head`
Expected: 编译失败，报 `undefined: prefixFor`、`undefined: prefixWriter`、`undefined: lineSink`、`undefined: maxLine`。

- [ ] **Step 3: 写实现**

创建 `internal/procsup/prefix.go`：

```go
package procsup

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// maxLine 是没有换行符时最多攒多长：超过就当一行吐出去，
// 免得一个不带换行的超长输出把内存吃光。
const maxLine = 64 * 1024

// prefixFor 生成一行输出的前缀，形如 "people/basic | "；width 用来把多个组件的前缀对齐。
func prefixFor(name string, width int) string {
	return fmt.Sprintf("%-*s | ", width, name)
}

// lineSink 把所有子进程的输出汇到同一个 io.Writer：一次只写一整行，行与行之间不会穿插。
type lineSink struct {
	mu  sync.Mutex
	out io.Writer
}

func (s *lineSink) writeLine(prefix string, line []byte) {
	buf := make([]byte, 0, len(prefix)+len(line)+1)
	buf = append(buf, prefix...)
	buf = append(buf, line...)
	buf = append(buf, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(buf)
}

// prefixWriter 是一个进程的输出端：按行切开，每行加上前缀再交给 lineSink。
//
// Write 永远不返回错误：exec 的拷贝 goroutine 一旦遇到写错误就会停止读管道，
// 子进程随后会在写满管道时被卡死——为了终端上的一行输出把整个进程卡住不划算。
type prefixWriter struct {
	sink   *lineSink
	prefix string

	// tailCap 是 Tail 最多留多少行；<= 0 表示不留。收尾时其余进程的关闭输出会刷屏，
	// 把崩溃的现场冲出屏幕之外，所以要把最后这几行留在 Exit 里，由调用方在最后一屏重新打出来。
	tailCap int

	mu   sync.Mutex
	buf  []byte
	ring []string // 最近的 tailCap 行；满了之后从 next 开始覆盖最旧的
	next int
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		w.buf = append(w.buf, chunk...)
		for len(w.buf) > maxLine {
			w.emit(w.buf[:maxLine])
			w.buf = append(w.buf[:0], w.buf[maxLine:]...)
		}
		if i < 0 {
			break
		}
		w.emit(w.buf)
		w.buf = w.buf[:0]
		p = p[i+1:]
	}
	return n, nil
}

// Flush 把还没遇到换行的尾巴当成一行吐出去。进程退出之后调用。
func (w *prefixWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = w.buf[:0]
	}
}

func (w *prefixWriter) emit(line []byte) {
	line = bytes.TrimSuffix(line, []byte("\r"))
	w.remember(string(line))
	w.sink.writeLine(w.prefix, line)
}

// remember 把一行记进环形缓冲：满了就覆盖最旧的那一行。每行 O(1)——行数调得再大，
// 也不会因为每来一行就整体挪一遍而拖慢输出。
func (w *prefixWriter) remember(line string) {
	if w.tailCap <= 0 {
		return
	}
	if len(w.ring) < w.tailCap {
		w.ring = append(w.ring, line)
		return
	}
	w.ring[w.next] = line
	w.next = (w.next + 1) % w.tailCap
}

// Tail 返回最近输出的几行（不带前缀，最旧的在前），最多 tailCap 行。返回的是副本。
func (w *prefixWriter) Tail() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := make([]string, 0, len(w.ring))
	out = append(out, w.ring[w.next:]...) // 没满时 next 是 0，这一段就是全部
	return append(out, w.ring[:w.next]...)
}
```

- [ ] **Step 4: 跑测试，确认通过**

Run: `go test -race -count=1 ./internal/procsup/`
Expected: `ok  	github.com/brickkit/brickkit/internal/procsup`

- [ ] **Step 5: 完整 lint**

Run: `make lint > /tmp/lint.log 2>&1; echo "exit=$?"`
Expected: `exit=0`

- [ ] **Step 6: 提交**

```bash
git add internal/procsup/prefix.go internal/procsup/prefix_test.go
git commit -m "$(cat <<'EOF'
新增：procsup 的输出前缀写入器——按行加名字前缀，多进程输出互不穿插

把多个子进程的输出汇到同一个 Writer 时，每行带上组件名（形如 "people/basic | ..."）。
一行被拆成多次 Write 时按换行符重新拼；超长且不带换行的输出按 64KB 切开，免得吃光内存；
Write 永远不返回错误——exec 的拷贝 goroutine 一遇到写错误就停止读管道，子进程会在写满
管道时被卡死。同时留下每个进程最近若干行输出（Tail，环形缓冲，行数由调用方定），供崩溃之后
排查：收尾时其余进程的输出会刷屏，把崩溃的现场冲出屏幕之外。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: 进程监管器

**背景：** 这是本计划的核心。要点都来自 spec §3 和上面的"设计决定"：

- 每个子进程自成一个进程组（Unix `Setpgid`），信号发给整组——`go run`、`npm run dev` 这类"启动器"会再拉起真正的服务进程，只给启动器发信号收不干净。副作用是终端的 Ctrl+C 不再直接打到子进程，收尾完全由 `Supervisor` 转发；所以前台会话必须自己接住 `StopSignals`。
- Windows 用 Job Object（`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`）：关闭 Job、或者 brickkit 自己死掉，系统把整棵树收走。优雅停止用 `CTRL_BREAK_EVENT`（需要子进程有自己的进程组）。这一半**只保证能编过**（spec §6.5），没有真机验证。
- 不自动重启。**一个进程崩了（`Exit.Crashed()` = 不是我们叫停的、并且退出码非零，含被外部信号杀死），整个会话随即收尾**（设计决定 1）：`watch` 在关闭 `Done` 之后起一个 goroutine 调 `Shutdown`——`Shutdown` 要等所有进程的 `Done`，包括崩溃的这个自己，所以不能同步调；之后 `Start` 返回 `ErrStopped`。自己干净退出（退出码 0）不算崩溃，也不连累别人。
- 崩溃的现场留在 `Exit` 里：退出码、信号、跑了多久，以及最后若干行输出（`Tail`，行数由 `Options.TailLines` 定、默认 20，来自 Task 2 的 `prefixWriter.Tail()`）。被我们叫停的进程 `Stopped == true`、`Crashed()` 为假——否则最后一屏会把受害者当成元凶。调用方在 `Run` 返回之后，只需要把 `Crashed()` 为真的那几个打出来。
- `Run(ctx)` 阻塞到所有进程自己退出，或 ctx 取消后完成收尾（有进程崩了会话会自己收尾，`Run` 随之返回）。收尾 = 先请体面退出 → 等 `GracePeriod` → 无条件强杀 → 释放平台资源。

测试要真的启动子进程。做法是**让测试二进制自己当子进程**：`helperEnv` 一置位，`TestMain` 就不跑测试，而是按第一个参数扮演某种行为（打印若干行、指定退出码、装作没听见 SIGTERM、拉起孙进程、扮演一个完整的前台会话……）。这样不依赖 `sh`、`sleep` 这类外部命令。注意 `-race` 下每个子进程也是竞态插桩过的，启动慢一截，`procsup` 的测试在 `-race` 下要跑十几秒，不是卡住了。

**Files:**
- Create: `internal/procsup/helper_test.go`、`internal/procsup/supervisor_test.go`、`internal/procsup/supervisor_unix_test.go`
- Create: `internal/procsup/supervisor.go`、`internal/procsup/proc_unix.go`、`internal/procsup/proc_windows.go`
- Modify: `go.mod`

**Interfaces:**
- Consumes（Task 2）：`prefixFor(name string, width int) string`、`lineSink`、`prefixWriter`（含 `Flush`、`Tail`）
- Produces（Plan 4 依赖，签名不要改）：
  - `type Spec struct { Name string; Argv []string; Dir string; Env []string }`——`Env` 是 `"KEY=VALUE"`，追加在当前进程环境之后，同名以后者为准
  - `type Options struct { Out io.Writer; NameWidth int; GracePeriod time.Duration; TailLines int }`——`TailLines <= 0` 取 `DefaultTailLines`
  - `type Exit struct { Name string; PID int; Code int; Signal string; Stopped bool; Duration time.Duration; Tail []string }`；`func (e Exit) Crashed() bool`
  - `type Proc`；`func (p *Proc) Name() string`、`PID() int`、`Done() <-chan struct{}`、`Exit() Exit`
  - `func New(opts Options) (*Supervisor, error)`
  - `func (s *Supervisor) Start(spec Spec) (*Proc, error)`
  - `func (s *Supervisor) Run(ctx context.Context) []Exit`
  - `func (s *Supervisor) Shutdown()`、`func (s *Supervisor) Exits() []Exit`
  - `var StopSignals []os.Signal`（`os.Interrupt`、`SIGTERM`、`SIGHUP`）、`const DefaultGracePeriod = 5 * time.Second`、`const DefaultTailLines = 20`、`var ErrStopped error`
  - 包内平台钩子（`proc_unix.go` / `proc_windows.go` 各实现一份）：`newSysState() (*sysState, error)`、`(*sysState).configure(*exec.Cmd)`、`attach(*exec.Cmd) error`、`terminate([]*Proc)`、`kill([]*Proc)`、`close()`，以及 `signalName(*os.ProcessState) string`

- [ ] **Step 1: 写失败的测试（三个文件）**

创建 `internal/procsup/helper_test.go`：

```go
package procsup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 这个包的测试要真的启动子进程。做法是让测试二进制自己当子进程：
// helperEnv 一置位，TestMain 就不跑测试，而是按第一个参数扮演某种行为的子进程。
// 这样不依赖 sh、sleep 这类外部命令，Windows 上也能用。
const helperEnv = "PROCSUP_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		runHelper(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

func runHelper(args []string) {
	switch args[0] {
	case "lines": // lines <n> <tag>：打印 n 行 "<tag>-<i>"
		n, _ := strconv.Atoi(args[1])
		for i := 0; i < n; i++ {
			fmt.Printf("%s-%d\n", args[2], i)
		}
	case "exit": // exit <code> [text]
		if len(args) > 2 {
			fmt.Println(args[2])
		}
		code, _ := strconv.Atoi(args[1])
		os.Exit(code)
	case "env": // env <KEY>：打印这个环境变量与工作目录
		fmt.Printf("%s=%s\n", args[1], os.Getenv(args[1]))
		wd, _ := os.Getwd()
		fmt.Println("cwd=" + wd)
	case "sleep":
		fmt.Println("ready")
		time.Sleep(time.Hour)
	case "ignore-term": // 装作没听见 SIGTERM，逼 Supervisor 走强杀
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("ready")
		time.Sleep(time.Hour)
	case "grandchild": // 拉起一个继承 stdout 的孙进程，打印它的 pid，然后自己等着
		fmt.Printf("grandchild %d\n", startGrandchild())
		fmt.Println("ready")
		time.Sleep(time.Hour)
	case "orphan": // 拉起孙进程之后自己立刻退出，把孙进程留成孤儿
		fmt.Printf("grandchild %d\n", startGrandchild())
	case "supervise": // 扮演前台会话：监管一个 sleep 子进程，收到 StopSignals 就收尾
		superviseOneChild()
	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode:", args[0])
		os.Exit(2)
	}
}

func superviseOneChild() {
	ctx, stop := signal.NotifyContext(context.Background(), StopSignals...)
	defer stop()
	sup, err := New(Options{Out: os.Stdout})
	if err != nil {
		fmt.Println("new failed:", err)
		os.Exit(2)
	}
	p, err := sup.Start(helperSpec("child", "sleep"))
	if err != nil {
		fmt.Println("start failed:", err)
		os.Exit(2)
	}
	fmt.Printf("child %d\n", p.PID())
	sup.Run(ctx)
	fmt.Println("session over")
}

func startGrandchild() int {
	gc := exec.Command(os.Args[0], "sleep")
	gc.Env = append(os.Environ(), helperEnv+"=1")
	gc.Stdout = os.Stdout
	if err := gc.Start(); err != nil {
		fmt.Println("grandchild failed:", err)
		os.Exit(2)
	}
	return gc.Process.Pid
}

func helperSpec(name string, args ...string) Spec {
	return Spec{Name: name, Argv: append([]string{os.Args[0]}, args...), Env: []string{helperEnv + "=1"}}
}

// syncBuffer 是可以一边被 Supervisor 写、一边被测试读的缓冲。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitForOutput 等输出里出现某段文字。
func waitForOutput(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Contains(out.String(), want) },
		5*time.Second, 10*time.Millisecond, "等不到输出 %q，目前的输出：\n%s", want, out.String())
}
```

创建 `internal/procsup/supervisor_test.go`：

```go
package procsup

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSupervisor(t *testing.T, opts Options) (*Supervisor, *syncBuffer) {
	t.Helper()
	out := &syncBuffer{}
	opts.Out = out
	sup, err := New(opts)
	require.NoError(t, err)
	t.Cleanup(sup.Shutdown)
	return sup, out
}

func TestExitCrashed(t *testing.T) {
	cases := []struct {
		name string
		exit Exit
		want bool
	}{
		{"自己干净退出", Exit{Code: 0}, false},
		{"自己以非零码退出", Exit{Code: 3}, true},
		{"自己被信号杀死", Exit{Code: -1, Signal: "segmentation fault"}, true},
		{"被我们叫停之后以非零码退出", Exit{Code: 143, Stopped: true}, false},
		{"被我们叫停、被信号杀死", Exit{Code: -1, Signal: "terminated", Stopped: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.exit.Crashed())
		})
	}
}

func TestRunReturnsOnceEveryProcessHasExited(t *testing.T) {
	sup, out := newSupervisor(t, Options{NameWidth: 5})
	_, err := sup.Start(helperSpec("web", "lines", "2", "w"))
	require.NoError(t, err)

	exits := sup.Run(context.Background())

	require.Len(t, exits, 1)
	assert.Equal(t, "web", exits[0].Name)
	assert.Equal(t, 0, exits[0].Code)
	assert.False(t, exits[0].Crashed())
	assert.Empty(t, exits[0].Signal)
	assert.Equal(t, "web   | w-0\nweb   | w-1\n", out.String())
}

func TestNonZeroExitIsACrashAndKeepsItsLastOutput(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	p, err := sup.Start(helperSpec("api", "exit", "3", "boom"))
	require.NoError(t, err)

	exits := sup.Run(context.Background())

	require.Len(t, exits, 1)
	assert.Equal(t, 3, exits[0].Code)
	assert.True(t, exits[0].Crashed())
	assert.Equal(t, []string{"boom"}, exits[0].Tail, "崩溃前最后的输出要留在 Exit 里，供排查")
	assert.Equal(t, "api | boom\n", out.String())
	<-p.Done()
	assert.Equal(t, exits[0], p.Exit())
	assert.Equal(t, "api", p.Name())
	assert.Positive(t, p.PID())
}

func TestACrashShutsDownTheWholeSessionAndNamesTheCulprit(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	_, err := sup.Start(helperSpec("steady", "sleep"))
	require.NoError(t, err)
	waitForOutput(t, out, "steady | ready")
	_, err = sup.Start(helperSpec("crasher", "exit", "1", "boom"))
	require.NoError(t, err)

	exits := sup.Run(context.Background()) // 没有人取消 ctx：崩溃本身就让会话收尾

	require.Len(t, exits, 2)
	steady, crasher := exits[0], exits[1]
	assert.True(t, crasher.Crashed(), "谁崩了：Crashed() 为真的那个")
	assert.Equal(t, 1, crasher.Code)
	assert.Equal(t, []string{"boom"}, crasher.Tail, "崩溃前最后的输出留了下来")
	assert.True(t, steady.Stopped, "没崩的被叫停")
	assert.False(t, steady.Crashed(), "被我们叫停的不算崩溃，否则最后一屏会把受害者当成元凶")
}

// 留多少行由调用方定；不管进程是不是崩了，Tail 都是它最后的那几行。
func TestTailKeepsOnlyTheConfiguredNumberOfLines(t *testing.T) {
	cases := []struct {
		name      string
		tailLines int
		want      []string
	}{
		{"不设就是默认行数", 0, lastLines("x", 30, DefaultTailLines)},
		{"调小", 3, lastLines("x", 30, 3)},
		{"调得比输出还多就是全部", 100, lastLines("x", 30, 30)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sup, _ := newSupervisor(t, Options{TailLines: c.tailLines})
			_, err := sup.Start(helperSpec("x", "lines", "30", "x"))
			require.NoError(t, err)

			exits := sup.Run(context.Background())

			require.Len(t, exits, 1)
			assert.False(t, exits[0].Crashed())
			assert.Equal(t, c.want, exits[0].Tail)
		})
	}
}

// lastLines 是 helper 的 "lines" 模式输出的最后 keep 行。
func lastLines(tag string, total, keep int) []string {
	if keep > total {
		keep = total
	}
	var out []string
	for i := total - keep; i < total; i++ {
		out = append(out, fmt.Sprintf("%s-%d", tag, i))
	}
	return out
}

func TestACleanExitDoesNotStopItsSiblings(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	quick, err := sup.Start(helperSpec("quick", "exit", "0"))
	require.NoError(t, err)
	_, err = sup.Start(helperSpec("steady", "sleep"))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []Exit, 1)
	go func() { done <- sup.Run(ctx) }()

	<-quick.Done()
	waitForOutput(t, out, "steady | ready")
	select {
	case <-done:
		t.Fatal("自己干净退出（退出码 0）不是崩溃，Run 不该跟着结束")
	default:
	}
	cancel()
	exits := <-done

	require.Len(t, exits, 2)
	assert.False(t, exits[0].Crashed())
	assert.False(t, exits[0].Stopped)
	assert.True(t, exits[1].Stopped)
}

func TestNothingCanStartOnceASessionHasCrashed(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})
	p, err := sup.Start(helperSpec("crasher", "exit", "2"))
	require.NoError(t, err)
	<-p.Done()

	require.Eventually(t, func() bool {
		sup.mu.Lock()
		defer sup.mu.Unlock()
		return sup.stopping
	}, 5*time.Second, 5*time.Millisecond, "崩溃之后 Supervisor 应该自己进入收尾")
	_, err = sup.Start(helperSpec("late", "exit", "0"))
	assert.ErrorIs(t, err, ErrStopped)
}

func TestStartReportsAMissingBinaryAndRegistersNothing(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})

	_, err := sup.Start(Spec{Name: "ghost", Argv: []string{filepath.Join(t.TempDir(), "nope")}})

	require.Error(t, err)
	assert.ErrorContains(t, err, "ghost")
	assert.ErrorIs(t, err, fs.ErrNotExist, "要能用 errors.Is 认出'文件不存在'")
	assert.Empty(t, sup.Exits())
}

func TestStartRejectsAnEmptyCommand(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})

	_, err := sup.Start(Spec{Name: "blank"})

	assert.ErrorContains(t, err, "blank")
}

func TestStartAfterShutdownIsRefused(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})
	sup.Shutdown()

	_, err := sup.Start(helperSpec("late", "exit", "0"))

	assert.ErrorIs(t, err, ErrStopped)
}

func TestShutdownIsIdempotent(t *testing.T) {
	sup, _ := newSupervisor(t, Options{})
	sup.Shutdown()
	sup.Shutdown()
}

func TestEnvAndDirReachTheChild(t *testing.T) {
	t.Setenv("PROCSUP_INHERITED", "from-parent")
	t.Setenv("PROCSUP_OVERRIDDEN", "old")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	sup, out := newSupervisor(t, Options{})
	inherited := helperSpec("inherited", "env", "PROCSUP_INHERITED")
	inherited.Dir = dir
	_, err = sup.Start(inherited)
	require.NoError(t, err)
	overridden := helperSpec("overridden", "env", "PROCSUP_OVERRIDDEN")
	overridden.Env = append(overridden.Env, "PROCSUP_OVERRIDDEN=new")
	_, err = sup.Start(overridden)
	require.NoError(t, err)

	sup.Run(context.Background())

	got := out.String()
	assert.Contains(t, got, "inherited | PROCSUP_INHERITED=from-parent", "继承父进程的环境")
	assert.Contains(t, got, "inherited | cwd="+dir, "工作目录")
	assert.Contains(t, got, "overridden | PROCSUP_OVERRIDDEN=new", "同名以 Spec.Env 里的为准")
}

func TestOutputOfConcurrentProcessesStaysLineIntact(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	for _, tag := range []string{"p0", "p1", "p2"} {
		_, err := sup.Start(helperSpec(tag, "lines", "300", tag))
		require.NoError(t, err)
	}

	sup.Run(context.Background())

	lineRe := regexp.MustCompile(`^(p[0-2]) \| (p[0-2])-(\d+)$`)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, 900)
	next := map[string]int{}
	for _, line := range lines {
		m := lineRe.FindStringSubmatch(line)
		require.NotNil(t, m, "行被弄坏了：%q", line)
		assert.Equal(t, m[1], m[2], "前缀与内容不是同一个进程的：%q", line)
		n, err := strconv.Atoi(m[3])
		require.NoError(t, err)
		assert.Equal(t, next[m[1]], n, "同一个进程的输出顺序不能乱：%q", line)
		next[m[1]]++
	}
}

func TestStopSignalsAreDefinedEverywhere(t *testing.T) {
	assert.Contains(t, StopSignals, os.Interrupt)
	assert.Len(t, StopSignals, 3)
}
```

创建 `internal/procsup/supervisor_unix_test.go`（信号语义只在 Unix 上有意义，所以单独一个带 build tag 的文件）：

```go
//go:build unix

package procsup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// processAlive 判断一个进程是不是还活着。僵尸进程（已经死了、只是没人收尸）算死的：
// 测试环境里 1 号进程不一定会回收孤儿。
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true // 没有 /proc（macOS）：分不出僵尸，按活着算
	}
	return !strings.Contains(string(stat), ") Z")
}

func requireGone(t *testing.T, pid int) {
	t.Helper()
	require.Eventually(t, func() bool { return !processAlive(pid) },
		5*time.Second, 20*time.Millisecond, "进程 %d 还活着", pid)
}

var grandchildRe = regexp.MustCompile(`grandchild (\d+)`)

func grandchildPID(t *testing.T, out *syncBuffer) int {
	t.Helper()
	waitForOutput(t, out, "grandchild ")
	m := grandchildRe.FindStringSubmatch(out.String())
	require.NotNil(t, m)
	pid, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	return pid
}

// runUntilReady 启动 Run，等到输出里出现 "ready"，返回取消函数与拿结果的通道。
func runUntilReady(t *testing.T, sup *Supervisor, out *syncBuffer) (cancel func(), result <-chan []Exit) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	done := make(chan []Exit, 1)
	go func() { done <- sup.Run(ctx) }()
	waitForOutput(t, out, "ready")
	return cancelFn, done
}

func awaitExits(t *testing.T, result <-chan []Exit) []Exit {
	t.Helper()
	select {
	case exits := <-result:
		return exits
	case <-time.After(15 * time.Second):
		t.Fatal("Run 没有及时返回")
		return nil
	}
}

func TestShutdownAsksPolitelyFirst(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	_, err := sup.Start(helperSpec("svc", "sleep"))
	require.NoError(t, err)
	cancel, result := runUntilReady(t, sup, out)

	cancel()
	exits := awaitExits(t, result)

	require.Len(t, exits, 1)
	assert.True(t, exits[0].Stopped)
	assert.False(t, exits[0].Crashed(), "被我们叫停的不算崩溃")
	assert.Equal(t, "terminated", exits[0].Signal, "先发的是 SIGTERM")
	assert.Equal(t, -1, exits[0].Code)
}

// 被外部信号杀死（比如内存不够被 OOM killer 干掉）同样是崩溃：不是我们叫它停的，也不是干净退出。
func TestAProcessKilledBySomeoneElseCountsAsACrash(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	victim, err := sup.Start(helperSpec("victim", "sleep"))
	require.NoError(t, err)
	_, err = sup.Start(helperSpec("bystander", "sleep"))
	require.NoError(t, err)
	done := make(chan []Exit, 1)
	go func() { done <- sup.Run(context.Background()) }()
	waitForOutput(t, out, "victim | ready")
	waitForOutput(t, out, "bystander | ready")

	require.NoError(t, syscall.Kill(victim.PID(), syscall.SIGKILL))

	exits := awaitExits(t, done)
	require.Len(t, exits, 2)
	assert.True(t, exits[0].Crashed())
	assert.Equal(t, "killed", exits[0].Signal)
	assert.Equal(t, -1, exits[0].Code)
	assert.True(t, exits[1].Stopped, "旁观者被叫停，会话结束")
	assert.False(t, exits[1].Crashed())
}

func TestShutdownEscalatesToKillWhenTheProcessIgnoresTerm(t *testing.T) {
	sup, out := newSupervisor(t, Options{GracePeriod: 200 * time.Millisecond})
	_, err := sup.Start(helperSpec("stubborn", "ignore-term"))
	require.NoError(t, err)
	cancel, result := runUntilReady(t, sup, out)

	started := time.Now()
	cancel()
	exits := awaitExits(t, result)

	require.Len(t, exits, 1)
	assert.Equal(t, "killed", exits[0].Signal)
	assert.True(t, exits[0].Stopped)
	assert.GreaterOrEqual(t, time.Since(started), 200*time.Millisecond, "得先给它宽限期")
}

func TestShutdownKillsTheWholeProcessTree(t *testing.T) {
	sup, out := newSupervisor(t, Options{})
	_, err := sup.Start(helperSpec("launcher", "grandchild"))
	require.NoError(t, err)
	cancel, result := runUntilReady(t, sup, out)
	pid := grandchildPID(t, out)
	require.True(t, processAlive(pid), "前提：孙进程此刻还活着")

	cancel()
	awaitExits(t, result)

	requireGone(t, pid)
}

func TestAnOrphanLeftBehindByAnExitedLauncherIsCleanedUp(t *testing.T) {
	oldDelay := waitDelay
	waitDelay = 200 * time.Millisecond
	t.Cleanup(func() { waitDelay = oldDelay })
	sup, out := newSupervisor(t, Options{})
	_, err := sup.Start(helperSpec("launcher", "orphan"))
	require.NoError(t, err)

	exits := sup.Run(context.Background())

	require.Len(t, exits, 1)
	assert.Equal(t, 0, exits[0].Code, "启动器自己是干净退出的")
	requireGone(t, grandchildPID(t, out))
}

// 前台会话跑在另一个进程里，用真的信号去停它：Ctrl+C（SIGINT）、被 kill（SIGTERM）、
// 终端被关掉（SIGHUP）。三种都得把子进程一起带走，并且让会话自己体面地收尾。
func TestStopSignalsEndTheSessionAndTakeTheChildrenWithIt(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			session := exec.Command(os.Args[0], "supervise")
			session.Env = append(os.Environ(), helperEnv+"=1")
			out := &syncBuffer{}
			session.Stdout = out
			require.NoError(t, session.Start())
			t.Cleanup(func() { _ = session.Process.Kill(); _ = session.Wait() })
			waitForOutput(t, out, "child ")
			waitForOutput(t, out, "child | ready")
			m := childRe.FindStringSubmatch(out.String())
			require.NotNil(t, m)
			childPID, err := strconv.Atoi(m[1])
			require.NoError(t, err)
			require.True(t, processAlive(childPID))

			require.NoError(t, session.Process.Signal(sig))

			waitForOutput(t, out, "session over")
			require.NoError(t, session.Wait())
			requireGone(t, childPID)
		})
	}
}

var childRe = regexp.MustCompile(`(?m)^child (\d+)$`)
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test ./internal/procsup/ 2>&1 | head`
Expected: 编译失败，报 `undefined: Options`、`undefined: New`、`undefined: Spec`、`undefined: StopSignals`、`undefined: waitDelay` 等。

- [ ] **Step 3: 写实现（三个文件）**

创建 `internal/procsup/supervisor.go`：

```go
// Package procsup 在前台监管一组裸进程——不经过容器引擎，由 brickkit 自己启动、自己收尾。
//
// 它只管四件事：把每个进程放进独立的进程组（Windows 是 Job Object），让整棵进程树能一起收掉；
// 把所有进程的 stdout/stderr 按行加上名字前缀，汇到同一个 io.Writer；收尾时先请进程体面地停，
// 超时再强杀；把每个进程的退出情况交给调用方。
//
// 它刻意不做的：重启、健康检查、跨会话存活。进程的生命周期严格等于持有 Supervisor 的这段前台会话
// ——所以不需要 PID 文件、也不需要后台服务。
//
// 一个进程崩了（不是我们叫它停的、退出码非零，含被外部信号杀死），整个会话随即收尾：其余进程被体面地停掉。
// 崩溃的现场留在 Exit 里——退出码、信号、跑了多久，以及它最后的若干行输出（行数由 Options.TailLines 定）
// ——调用方在 Run 返回之后，只需要把 Crashed() 为真的那几个打在最后一屏，不会被收尾时其余进程的输出冲走。
package procsup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// DefaultGracePeriod 是 Shutdown 请进程体面退出之后，等多久才动手强杀。
	DefaultGracePeriod = 5 * time.Second
	// DefaultTailLines 是每个进程默认留多少行最近的输出。
	DefaultTailLines = 20
)

// StopSignals 是前台会话应该当作"停"的信号：Ctrl+C、被 kill、终端被关掉。
// 这三个常量在各平台上都有定义。
var StopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

var (
	// 子进程退出之后，最多再等这么久让输出管道排空。孙进程如果继承了 stdout 又活得比子进程久，
	// 不设上限的话 cmd.Wait 会一直挂着。
	waitDelay = 2 * time.Second
	// 强杀之后最多再等这么久：SIGKILL 杀不掉的只有卡在内核里的进程，不能因此让 Ctrl+C 永远挂着。
	killWait = 5 * time.Second
)

// ErrStopped 表示 Supervisor 已经在收尾，不再接受新进程。
var ErrStopped = errors.New("procsup: supervisor is shutting down")

// Spec 描述要启动的一个进程。
type Spec struct {
	// Name 用作输出前缀与 Exit.Name，通常是组件 ID。
	Name string
	// Argv[0] 是可执行文件（按 PATH 查找，含路径分隔符时相对 Dir），其余是参数。不经过 shell。
	Argv []string
	// Dir 是工作目录；空表示继承当前目录。
	Dir string
	// Env 是 "KEY=VALUE" 列表，追加在当前进程的环境之后，同名以后者为准。
	// 值只经由 exec.Cmd.Env 在内存里传给子进程，不会落盘。
	Env []string
}

// Options 配置 Supervisor。
type Options struct {
	// Out 接收所有进程带前缀的输出；nil 表示丢弃。
	Out io.Writer
	// NameWidth 把前缀里的名字补齐到这个宽度，多个进程的输出才对得齐；0 表示不补。
	NameWidth int
	// GracePeriod 见 DefaultGracePeriod；<= 0 时取默认值。
	GracePeriod time.Duration
	// TailLines 是每个进程留多少行最近的输出（Exit.Tail），排查崩溃用；<= 0 时取 DefaultTailLines。
	// 内存占用只取决于进程实际输出了多少行，不会预先按这个数分配。
	TailLines int
}

// Exit 是一个进程的退出情况。
type Exit struct {
	Name string
	PID  int
	// Code 是退出码；被信号杀死时为 -1。
	Code int
	// Signal 是杀死它的信号名（"terminated"、"killed"……），没被信号杀死时为空；Windows 上恒为空。
	Signal string
	// Stopped 表示这个进程是 Shutdown 要求它停的，而不是自己先退的。
	Stopped bool
	// Duration 是从启动到退出的时长。
	Duration time.Duration
	// Tail 是它最后的若干行输出（stdout 与 stderr 合并，不带前缀，最旧的在前），
	// 最多 Options.TailLines 行，排查崩溃用。
	Tail []string
}

// Crashed 表示进程自己异常退出了：不是我们叫它停的，而且退出码非零（含被信号杀死）。
func (e Exit) Crashed() bool { return !e.Stopped && e.Code != 0 }

// Proc 是一个已经启动的进程。
type Proc struct {
	name    string
	pid     int
	started time.Time

	stopping atomic.Bool
	done     chan struct{}
	exit     Exit // 只在 done 关闭之后才可读
}

// Name 返回 Spec.Name。
func (p *Proc) Name() string { return p.name }

// PID 返回进程号。
func (p *Proc) PID() int { return p.pid }

// Done 在进程退出（并且输出排空或等待超时）之后关闭。
func (p *Proc) Done() <-chan struct{} { return p.done }

// Exit 返回退出情况；只有 Done 关闭之后才有意义。
func (p *Proc) Exit() Exit { return p.exit }

// Supervisor 监管一组进程。零值不可用，用 New 创建。
type Supervisor struct {
	opts Options
	sink *lineSink
	sys  *sysState

	mu       sync.Mutex
	procs    []*Proc
	stopping bool

	once sync.Once
}

// New 创建一个 Supervisor。用完必须调用 Shutdown（Windows 上它还负责释放 Job Object）。
func New(opts Options) (*Supervisor, error) {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	sys, err := newSysState()
	if err != nil {
		return nil, fmt.Errorf("procsup: %w", err)
	}
	if opts.TailLines <= 0 {
		opts.TailLines = DefaultTailLines
	}
	return &Supervisor{opts: opts, sink: &lineSink{out: out}, sys: sys}, nil
}

// Start 启动一个进程并立刻返回。进程的输出随即开始流向 Options.Out。
func (s *Supervisor) Start(spec Spec) (*Proc, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("procsup: %s has an empty command", spec.Name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return nil, ErrStopped
	}

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.WaitDelay = waitDelay
	w := &prefixWriter{sink: s.sink, prefix: prefixFor(spec.Name, s.opts.NameWidth), tailCap: s.opts.TailLines}
	cmd.Stdout, cmd.Stderr = w, w // 同一个 Writer：一根管道，stdout 与 stderr 的先后顺序不会被打乱
	s.sys.configure(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("procsup: start %s: %w", spec.Name, err)
	}
	if err := s.sys.attach(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("procsup: attach %s: %w", spec.Name, err)
	}

	p := &Proc{name: spec.Name, pid: cmd.Process.Pid, started: time.Now(), done: make(chan struct{})}
	s.procs = append(s.procs, p)
	go s.watch(p, cmd, w)
	return p, nil
}

func (s *Supervisor) watch(p *Proc, cmd *exec.Cmd, w *prefixWriter) {
	_ = cmd.Wait() // *ExitError、ErrWaitDelay 都不影响下面从 ProcessState 读到的退出情况
	w.Flush()

	code, signal := -1, ""
	if ps := cmd.ProcessState; ps != nil {
		code, signal = ps.ExitCode(), signalName(ps)
	}
	p.exit = Exit{
		Name: p.name, PID: p.pid, Code: code, Signal: signal,
		Stopped: p.stopping.Load(), Duration: time.Since(p.started), Tail: w.Tail(),
	}
	close(p.done)

	// 一个崩了，整个会话收尾。用 goroutine 是因为 Shutdown 要等所有进程的 Done，包括我们自己。
	if p.exit.Crashed() {
		go s.Shutdown()
	}
}

// Run 阻塞到所有已启动的进程都退出，或者 ctx 被取消——取消时先 Shutdown 再返回。
// 返回全部进程的退出情况，按启动顺序。Run 期间不要再 Start。
//
// 有进程崩了，Supervisor 会自己收尾（见包注释），Run 随之返回：返回值里 Crashed() 为真的就是"谁崩了"，
// 其余的是被叫停的（Stopped）。自己干净退出（退出码 0）的进程不算崩溃，也不会连累别的进程。
func (s *Supervisor) Run(ctx context.Context) []Exit {
	s.mu.Lock()
	procs := append([]*Proc(nil), s.procs...)
	s.mu.Unlock()

	all := make(chan struct{})
	go func() {
		for _, p := range procs {
			<-p.done
		}
		close(all)
	}()

	select {
	case <-all:
	case <-ctx.Done():
	}
	s.Shutdown()
	return s.Exits()
}

// Shutdown 收尾：先请每个进程组体面地退出，等 GracePeriod，还没走的强杀，最后释放平台资源。
// 可以重复调用，也可以并发调用；后到的调用者会等到收尾完成。
func (s *Supervisor) Shutdown() { s.once.Do(s.shutdown) }

func (s *Supervisor) shutdown() {
	s.mu.Lock()
	s.stopping = true
	procs := append([]*Proc(nil), s.procs...)
	s.mu.Unlock()

	for _, p := range procs {
		p.stopping.Store(true)
	}
	s.sys.terminate(procs)

	grace := s.opts.GracePeriod
	if grace <= 0 {
		grace = DefaultGracePeriod
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
wait:
	for _, p := range procs {
		select {
		case <-p.done:
		case <-timer.C:
			break wait
		}
	}

	// 无条件强杀一遍：进程组的领头进程可能已经走了，组里却还留着不理睬 SIGTERM 的孙进程。
	s.sys.kill(procs)

	deadline := time.NewTimer(killWait)
	defer deadline.Stop()
	for _, p := range procs {
		select {
		case <-p.done:
		case <-deadline.C:
			s.sys.close()
			return
		}
	}
	s.sys.close()
}

// Exits 返回已经退出的进程的退出情况，按启动顺序。
func (s *Supervisor) Exits() []Exit {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Exit
	for _, p := range s.procs {
		select {
		case <-p.done:
			out = append(out, p.exit)
		default:
		}
	}
	return out
}
```

创建 `internal/procsup/proc_unix.go`：

```go
//go:build unix

package procsup

import (
	"os"
	"os/exec"
	"syscall"
)

// sysState 是平台相关的状态。Unix 上不需要任何东西：进程组本身就是收尾的单位。
type sysState struct{}

func newSysState() (*sysState, error) { return &sysState{}, nil }

// configure 让子进程自成一个进程组（pgid == pid），信号才能发给整组：
// `go run`、`npm run dev` 这类"启动器"会再拉起真正的服务进程，只给启动器发信号收不干净。
// 副作用是终端的 Ctrl+C 不再直接打到子进程——收尾完全由 Supervisor 转发。
func (*sysState) configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (*sysState) attach(*exec.Cmd) error { return nil }

// terminate 请每个进程组体面地退出。组已经没了（ESRCH）不算错。
func (*sysState) terminate(procs []*Proc) { signalGroups(procs, syscall.SIGTERM) }

// kill 强杀每个进程组。
func (*sysState) kill(procs []*Proc) { signalGroups(procs, syscall.SIGKILL) }

func (*sysState) close() {}

func signalGroups(procs []*Proc, sig syscall.Signal) {
	for _, p := range procs {
		_ = syscall.Kill(-p.pid, sig)
	}
}

func signalName(ps *os.ProcessState) string {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return ""
}
```

创建 `internal/procsup/proc_windows.go`（Windows 侧：只需要编得过）：

```go
//go:build windows

package procsup

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sysState 持有一个 Job Object：所有子进程（连同它们后来拉起的孙进程）都在里面，
// 关闭它、或者 brickkit 自己死掉，整棵进程树一起被系统收走。
type sysState struct{ job windows.Handle }

func newSysState() (*sysState, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &sysState{job: job}, nil
}

// configure 给子进程一个新的进程组，这样才能只对它发 CTRL_BREAK_EVENT。
func (*sysState) configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// attach 把刚启动的进程放进 Job Object。
//
// 进程是先启动、后入 Job 的（os/exec 没法以挂起状态创建进程），所以启动到入 Job 之间
// 那几微秒里它拉起的孙进程会逃出 Job。这是已知的缺口：真实场景里启动器不会在第一微秒就 fork。
func (st *sysState) attach(cmd *exec.Cmd) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return windows.AssignProcessToJobObject(st.job, h)
}

// terminate 给每个进程组发 CTRL_BREAK_EVENT，相当于在控制台里对它按 Ctrl+Break。
// 没有控制台可共享时发不出去，忽略即可：GracePeriod 之后 kill 会兜底。
func (*sysState) terminate(procs []*Proc) {
	for _, p := range procs {
		_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.pid))
	}
}

// kill 直接终止整个 Job，里面所有进程一起死。
func (st *sysState) kill([]*Proc) { _ = windows.TerminateJobObject(st.job, 1) }

func (st *sysState) close() { _ = windows.CloseHandle(st.job) }

func signalName(*os.ProcessState) string { return "" }
```

- [ ] **Step 4: `golang.org/x/sys` 改成直接依赖**

`proc_windows.go` 直接 import 了 `golang.org/x/sys/windows`，而 `go.mod` 里它还标着 `// indirect`。把那一行的 `// indirect` 去掉：

```diff
-	golang.org/x/sys v0.18.0 // indirect
+	golang.org/x/sys v0.18.0
```

Run: `go build ./... && git diff --stat go.mod go.sum`
Expected: 构建通过；`go.mod | 2 +-`，`go.sum` 没有变化（`go.sum` 里早就有 v0.18.0 的两条哈希）。**不要用 `make tidy` / `go mod tidy` 来做这件事，也不要给 go 命令传 `-mod=mod`**（它会悄悄改写 `go.mod`）：仓库现有的 `go.mod` 本身就不是 tidy 的——`golang.org/x/term` 和 `gopkg.in/yaml.v3` 被代码直接 import，却仍标着 `// indirect`（历史遗留，与本计划无关），tidy 会连它们一起改，把无关的改动混进这次提交。只手工去掉 x/sys 那一行的 `// indirect`，`git diff --stat go.mod` 必须恰好是一行改动。

- [ ] **Step 5: 跑测试，确认通过，并确认不抖**

Run: `go test -race -count=1 ./internal/procsup/`
Expected: `ok  	github.com/brickkit/brickkit/internal/procsup`（十几秒）

Run: `go test -race -count=5 ./internal/procsup/`
Expected: 5 遍全过。这些测试里有等待子进程、发真信号、等进程消失的部分，`-count=5` 是为了在时序上有问题时把它逼出来；出现抖动就先查是不是漏了"等到 ready 再发信号"。

- [ ] **Step 6: 确认 Windows / macOS 编得过**

Run: `make check-cross-build`
Expected: 退出码 0，三个 `▶ GOOS=…` 之后是 `✅ linux / darwin / windows 都编得过`。这是唯一能证明 `proc_windows.go` 没有笔误的东西——它在 Linux 上不参与编译。

- [ ] **Step 7: 完整 lint**

Run: `make lint > /tmp/lint.log 2>&1; echo "exit=$?"`
Expected: `exit=0`。`tests/i18nguard` 会扫这个包的生产代码是否写死了中文字符串字面量（应当没有）。

- [ ] **Step 8: 提交**

```bash
git add internal/procsup/ go.mod
git commit -m "$(cat <<'EOF'
新增：procsup 进程监管器——进程组 / Job Object 收尾整棵进程树，先体面停止再强杀

前台监管一组裸进程：每个子进程自成进程组（Windows 是 Job Object），信号发给整组，
"go run"、"npm run dev" 这类启动器拉起的孙进程也一起收掉；所有进程的输出带名字前缀汇到
同一个 Writer；收尾先 SIGTERM（Windows 是 CTRL_BREAK_EVENT），等宽限期，再无条件强杀，
连领头进程已经走了、组里却残留的孤儿也清掉。不重启、不做健康检查、不跨会话存活；一个进程
一个进程崩了（不是我们叫它停的、退出码非零，含被外部信号杀死）整个会话随即收尾，其余进程被体面地停掉；
崩溃的现场——退出码、信号、跑了多久、最后若干行输出（Options.TailLines，默认 20）——留在 Exit 里，
调用方在收尾之后只需要把 Crashed() 的那几个打在最后一屏。

测试让测试二进制自己当子进程，覆盖：退出码、崩溃判定与会话收尾（含被外部信号杀死、干净退出不连累别人）、环境与工作目录、并发输出行完整、
SIGTERM 不理睬时升级为 SIGKILL、孙进程与孤儿的清理，以及用真的 SIGINT / SIGTERM /
SIGHUP 驱动一个跑在另一个进程里的前台会话。Windows 那一半只保证编得过（spec §6.5）。
golang.org/x/sys 从间接依赖改成直接依赖。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: 一次性端口监听探测 `WaitListening`

**背景：** spec §4 / §6.4：local 模式**不上完整的 healthCheck**，只保留一个轻量的一次性检测，专门发现"命令跑起来了，但没监听正确的端口"。失败分两种，处理不同：进程直接退了（硬失败，Plan 4 报错并提示手写 `runCommand`）；进程在跑但迟迟没监听（软警告，不杀进程——慢启动是正常情况）。所以探测要能区分**四种结果**：监听了 / 进程退了 / 超时了 / 被取消了（用户按了 Ctrl+C）。它不是健康检查：不发请求、不看响应，连上就算。

**Files:**
- Create: `internal/procsup/probe.go`
- Test: `internal/procsup/probe_test.go`

**Interfaces:**
- Consumes（Task 3）：`Proc.Done() <-chan struct{}`——调用方把它作为 `exited` 传进来
- Produces（Plan 4 依赖）：`type ProbeResult int`，取值 `ProbeListening`、`ProbeExited`、`ProbeTimedOut`、`ProbeCanceled`；`func WaitListening(ctx context.Context, port int, exited <-chan struct{}, timeout time.Duration) ProbeResult`

- [ ] **Step 1: 写失败的测试**

创建 `internal/procsup/probe_test.go`：

```go
package procsup

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freePort 找一个此刻没人监听的端口。
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func listenOn(t *testing.T, addr string) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func TestWaitListeningSeesAnExistingListener(t *testing.T) {
	l := listenOn(t, "127.0.0.1:0")

	got := WaitListening(context.Background(), l.Addr().(*net.TCPAddr).Port, nil, time.Second)

	assert.Equal(t, ProbeListening, got)
}

func TestWaitListeningSeesAListenerThatStartsLater(t *testing.T) {
	port := freePort(t)
	started := make(chan net.Listener, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			l = nil
		}
		started <- l
	}()

	got := WaitListening(context.Background(), port, nil, 3*time.Second)

	assert.Equal(t, ProbeListening, got)
	if l := <-started; l != nil {
		_ = l.Close()
	}
}

func TestWaitListeningTimesOutWhenNothingListens(t *testing.T) {
	started := time.Now()

	got := WaitListening(context.Background(), freePort(t), nil, 300*time.Millisecond)

	assert.Equal(t, ProbeTimedOut, got)
	assert.GreaterOrEqual(t, time.Since(started), 300*time.Millisecond)
}

func TestWaitListeningReportsAProcessThatExited(t *testing.T) {
	exited := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		close(exited)
	}()

	got := WaitListening(context.Background(), freePort(t), exited, 5*time.Second)

	assert.Equal(t, ProbeExited, got)
}

// 端口被别的东西占着、我们的进程却因为 "address in use" 死了：得报退出，不能被那个占着端口的东西骗过去。
func TestWaitListeningPrefersExitOverAForeignListener(t *testing.T) {
	l := listenOn(t, "127.0.0.1:0")
	exited := make(chan struct{})
	close(exited)

	got := WaitListening(context.Background(), l.Addr().(*net.TCPAddr).Port, exited, time.Second)

	assert.Equal(t, ProbeExited, got)
}

func TestWaitListeningStopsWhenTheContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	got := WaitListening(ctx, freePort(t), nil, 5*time.Second)

	assert.Equal(t, ProbeCanceled, got)
}

func TestWaitListeningAlsoSeesAnIPv6OnlyListener(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("这台机器没有 IPv6 回环：%v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	got := WaitListening(context.Background(), l.Addr().(*net.TCPAddr).Port, nil, time.Second)

	assert.Equal(t, ProbeListening, got)
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test ./internal/procsup/ -run WaitListening 2>&1 | head`
Expected: 编译失败，报 `undefined: WaitListening`、`undefined: ProbeListening` 等。

- [ ] **Step 3: 写实现**

创建 `internal/procsup/probe.go`：

```go
package procsup

import (
	"context"
	"net"
	"strconv"
	"time"
)

// ProbeResult 是 WaitListening 的结论。
type ProbeResult int

const (
	// ProbeListening：端口上有东西在监听了。
	ProbeListening ProbeResult = iota
	// ProbeExited：等的过程中进程退出了。"命令没跑起来"，是硬失败。
	ProbeExited
	// ProbeTimedOut：进程还在跑，但到时间了端口还没监听。只是警告的理由：慢启动是正常情况。
	ProbeTimedOut
	// ProbeCanceled：ctx 被取消了（通常是用户按了 Ctrl+C）。
	ProbeCanceled
)

const (
	probeInterval    = 100 * time.Millisecond
	probeDialTimeout = 200 * time.Millisecond
)

// WaitListening 等本机的 port 上出现监听，最多等 timeout；exited 是被监管进程的 Proc.Done()，
// 它先关闭就说明进程已经退出了，不必再等。
//
// 这只是一次性的"命令跑起来了吗"检测，不是健康检查：不发请求、不看响应，连上就算。
// 它有一处盲区——分不出端口上的监听者是不是被监管的那个进程。所以进程退出优先于监听：
// 端口被别的东西占着、而我们的进程因为 "address in use" 死掉时，要报退出，不能报成功。
func WaitListening(ctx context.Context, port int, exited <-chan struct{}, timeout time.Duration) ProbeResult {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(probeInterval)
	defer tick.Stop()

	for {
		select {
		case <-exited:
			return ProbeExited
		default:
		}
		if listening(port) {
			return ProbeListening
		}
		select {
		case <-exited:
			return ProbeExited
		case <-ctx.Done():
			return ProbeCanceled
		case <-deadline.C:
			return ProbeTimedOut
		case <-tick.C:
		}
	}
}

// listening 试着连一下 IPv4 与 IPv6 的回环地址：进程可能只绑了其中一个。
func listening(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), probeDialTimeout)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试，确认通过**

Run: `go test -race -count=3 -run WaitListening ./internal/procsup/`
Expected: `ok`。`TestWaitListeningAlsoSeesAnIPv6OnlyListener` 在没有 IPv6 回环的机器上会 `SKIP`，不算失败。

- [ ] **Step 5: 完整 lint**

Run: `make lint > /tmp/lint.log 2>&1; echo "exit=$?"`
Expected: `exit=0`

- [ ] **Step 6: 提交**

```bash
git add internal/procsup/probe.go internal/procsup/probe_test.go
git commit -m "$(cat <<'EOF'
新增：procsup.WaitListening——一次性端口监听探测，进程退出优先于监听

local 模式不上完整的 healthCheck，只保留一个轻量检测，专门发现"命令跑起来了但没监听正确
端口"。结果分四种：监听了 / 进程退了（硬失败）/ 超时了（只是警告，慢启动是正常情况）/
被取消了。进程退出优先于监听：端口被别的东西占着、我们的进程因 address in use 死掉时，要
报退出，不能被占着端口的东西骗成成功。IPv4 与 IPv6 回环都试，进程可能只绑了其中一个。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: 项目级会话锁 `sessionlock`

**背景：** spec §3 的并发保护与跨终端可见性：同一个项目同一时刻只允许一个前台会话监管本地进程；另一个终端里的 `status` / `down` 要能看见"这个项目有个会话在跑，PID xxx"。锁的持有者是**活着的前台监管进程**，进程一死自动释放——这正是选文件锁（`flock` / `LockFileEx`）而不是 PID 文件的理由：PID 文件会陈旧（进程被 `kill -9` 之后文件还在，还得猜那个 PID 有没有被别的进程复用）。文件里写的 PID 与启动时间只是给人看的提示，判断"有没有会话"看的是锁本身。

Windows 的文件锁是**强制**的：被锁住的区间别的进程读不了，而提示信息写在文件开头，得让别的终端读得到。所以 Windows 版锁的是文件末尾之外很远的一个字节，不是文件内容本身。这一半只保证能编过。

**Files:**
- Create: `internal/sessionlock/lock.go`、`internal/sessionlock/lock_unix.go`、`internal/sessionlock/lock_windows.go`
- Test: `internal/sessionlock/lock_test.go`

**Interfaces:**
- Produces（Plan 4 依赖）：
  - `type Info struct { PID int; Started time.Time }`
  - `type HeldError struct{ Info Info }`，实现 `error`——锁被别的活着的会话持有；`Info` 是它留下的提示，读不出来时是零值
  - `type Lock`；`func Acquire(path string) (*Lock, error)`——拿不到返回 `*HeldError`，目录不存在会被创建
  - `func (l *Lock) Release() error`
  - `func Inspect(path string) (info Info, held bool, err error)`——文件不存在或没人持有都是 `held == false`
  - 包内平台钩子：`tryLock(f *os.File) (bool, error)`、`unlock(f *os.File) error`

- [ ] **Step 1: 写失败的测试**

创建 `internal/sessionlock/lock_test.go`：

```go
package sessionlock

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 跨进程的测试让测试二进制自己当"另一个会话"：helperEnv 一置位，TestMain 就去拿锁并一直占着。
const helperEnv = "SESSIONLOCK_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		if _, err := Acquire(os.Args[1]); err != nil {
			fmt.Println("acquire failed:", err)
			os.Exit(2)
		}
		fmt.Println("held")
		time.Sleep(time.Hour)
		return
	}
	os.Exit(m.Run())
}

func lockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "session.lock")
}

func TestAcquireLeavesTheHoldersInfoForOtherTerminals(t *testing.T) {
	path := lockPath(t)
	l, err := Acquire(path)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()

	info, held, err := Inspect(path)

	require.NoError(t, err)
	assert.True(t, held)
	assert.Equal(t, os.Getpid(), info.PID)
	assert.WithinDuration(t, time.Now(), info.Started, time.Minute)
}

func TestASecondAcquireIsRefusedWhileTheLockIsHeld(t *testing.T) {
	path := lockPath(t)
	l, err := Acquire(path)
	require.NoError(t, err)
	defer func() { _ = l.Release() }()

	_, err = Acquire(path)

	var held *HeldError
	require.ErrorAs(t, err, &held)
	assert.Equal(t, os.Getpid(), held.Info.PID, "报错里要带着持有者是谁")
	assert.ErrorContains(t, err, fmt.Sprint(os.Getpid()))
}

func TestReleaseFreesTheLockAndClearsTheHint(t *testing.T) {
	path := lockPath(t)
	l, err := Acquire(path)
	require.NoError(t, err)

	require.NoError(t, l.Release())

	_, held, err := Inspect(path)
	require.NoError(t, err)
	assert.False(t, held)
	st, err := os.Stat(path)
	require.NoError(t, err, "锁文件永远不删")
	assert.Zero(t, st.Size(), "提示信息要清掉，免得留着一个过期的 PID")
	l2, err := Acquire(path)
	require.NoError(t, err, "释放之后能再拿")
	require.NoError(t, l2.Release())
}

func TestInspectOfAMissingFileIsNotHeld(t *testing.T) {
	info, held, err := Inspect(lockPath(t))

	require.NoError(t, err)
	assert.False(t, held)
	assert.Zero(t, info)
}

func TestInspectDoesNotLeaveTheLockTaken(t *testing.T) {
	path := lockPath(t)
	l, err := Acquire(path)
	require.NoError(t, err)
	require.NoError(t, l.Release())

	_, _, err = Inspect(path)
	require.NoError(t, err)

	l, err = Acquire(path)
	require.NoError(t, err, "探测完锁必须还是空闲的")
	require.NoError(t, l.Release())
}

func TestAcquireFailsCleanlyWhenTheDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))

	_, err := Acquire(filepath.Join(blocker, "sub", "session.lock"))

	require.Error(t, err)
	var held *HeldError
	assert.NotErrorAs(t, err, &held, "建不出目录不是'被别人持有'")
}

func TestAcquireFailsCleanlyWhenThePathIsADirectory(t *testing.T) {
	_, err := Acquire(t.TempDir())

	require.Error(t, err)
	var held *HeldError
	assert.NotErrorAs(t, err, &held)
}

func TestAcquireCreatesMissingDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "session.lock")

	l, err := Acquire(path)

	require.NoError(t, err)
	require.NoError(t, l.Release())
}

// 锁的生死跟持有者进程绑在一起：进程被 kill -9 之后，文件里还留着它的 PID，
// 但锁已经被内核释放了——判断有没有会话看的是锁，不是那个 PID。
func TestALockHeldByAnotherProcessDiesWithIt(t *testing.T) {
	path := lockPath(t)
	cmd := exec.Command(os.Args[0], path)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "held\n", line)

	info, held, err := Inspect(path)
	require.NoError(t, err)
	assert.True(t, held)
	assert.Equal(t, cmd.Process.Pid, info.PID, "看得到另一个进程持有着")
	_, err = Acquire(path)
	var heldErr *HeldError
	require.ErrorAs(t, err, &heldErr)
	assert.Equal(t, cmd.Process.Pid, heldErr.Info.PID)

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	_, held, err = Inspect(path)
	require.NoError(t, err)
	assert.False(t, held, "持有者死了，锁就该空出来")
	l, err := Acquire(path)
	require.NoError(t, err)
	require.NoError(t, l.Release())
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test ./internal/sessionlock/ 2>&1 | head`
Expected: 编译失败，报 `undefined: Acquire`、`undefined: Inspect`、`undefined: HeldError`。

- [ ] **Step 3: 写实现（三个文件）**

创建 `internal/sessionlock/lock.go`：

```go
// Package sessionlock 是项目目录级的会话锁：同一时刻只允许一个前台会话监管本地进程，
// 并且让别的终端里的 status / down 能看见"这个项目现在有个会话在跑"。
//
// 锁靠操作系统的文件锁（Unix 的 flock、Windows 的 LockFileEx）实现，持有者进程一死，
// 内核就自动释放——所以判断"有没有会话"看的是锁本身，不是文件里写的 PID。
// PID 文件那套的毛病正是会陈旧：进程被 kill -9 之后文件还在，还得再去猜那个 PID 是不是被别的进程复用了。
// 文件里的内容（持有者的 PID 与启动时间）只是拿来给人看的提示。
package sessionlock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Info 是持有者写在锁文件里的提示信息。
type Info struct {
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
}

// HeldError 表示锁已经被另一个活着的会话持有。Info 是那个会话留下的提示，
// 读不出来时（刚好赶上它在写）是零值。
type HeldError struct{ Info Info }

func (e *HeldError) Error() string {
	return fmt.Sprintf("session lock is held by pid %d", e.Info.PID)
}

// Lock 是一把已经拿到手的锁。
type Lock struct {
	f *os.File
}

// Acquire 在 path 上拿锁；path 所在目录不存在会被创建。
// 已经被别的会话持有时返回 *HeldError。
//
// 锁文件永远不会被删除：删除会让"正在等着 open 同一个路径的另一个进程"拿到一个已经脱离目录的
// inode，两边各自以为自己持有锁。空文件留在原地没有任何代价。
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	locked, err := tryLock(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !locked {
		_ = f.Close()
		info, _ := readInfo(path)
		return nil, &HeldError{Info: info}
	}

	l := &Lock{f: f}
	if err := l.writeInfo(); err != nil {
		_ = l.Release()
		return nil, err
	}
	return l, nil
}

func (l *Lock) writeInfo() error {
	b, err := json.Marshal(Info{PID: os.Getpid(), Started: time.Now().UTC()})
	if err != nil {
		return err
	}
	if err := l.f.Truncate(0); err != nil {
		return err
	}
	_, err = l.f.WriteAt(b, 0)
	return err
}

// Release 释放锁。进程退出时不调用也行——内核会替你释放——但正常收尾时应该调用，
// 它会顺手清掉文件里的提示信息。
func (l *Lock) Release() error {
	_ = l.f.Truncate(0)
	return errors.Join(unlock(l.f), l.f.Close())
}

// Inspect 看 path 上现在有没有活着的持有者：held 为 true 时 info 是它留下的提示。
// 文件不存在、或者没人持有，都返回 held == false。
//
// 探测的办法是试着拿一下锁再立刻放掉，所以它和 Acquire 之间有一个微秒级的窗口：
// 恰好在这一瞬间调用 Acquire 的会话会被误判成"已被持有"。代价是重试一次，不值得为它引入别的机制。
func Inspect(path string) (info Info, held bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, false, nil
	}
	if err != nil {
		return Info{}, false, err
	}
	defer func() { _ = f.Close() }()

	locked, err := tryLock(f)
	if err != nil {
		return Info{}, false, err
	}
	if locked {
		return Info{}, false, unlock(f)
	}
	info, _ = readInfo(path)
	return info, true, nil
}

func readInfo(path string) (Info, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	err = json.Unmarshal(b, &info)
	return info, err
}
```

创建 `internal/sessionlock/lock_unix.go`：

```go
//go:build unix

package sessionlock

import (
	"os"
	"syscall"
)

// tryLock 以不阻塞的方式拿独占锁；false 表示别人持有着。
// flock 锁的是"打开的文件描述"，所以同一个进程里再 open 一次、再 flock，同样会被拒绝。
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if err == syscall.EWOULDBLOCK {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
```

创建 `internal/sessionlock/lock_windows.go`（只需要编得过）：

```go
//go:build windows

package sessionlock

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockOffsetHigh 让被锁的那一个字节落在文件内容之外很远的地方。
// Windows 的文件锁是强制的：被锁住的区间别的进程读不了，而持有者的提示信息就写在文件开头，
// 得让别的终端里的 status / down 读得到。锁一个不存放内容的字节，两件事就互不妨碍。
const lockOffsetHigh = 0x7fffffff

func lockRange(f *os.File, flags uint32) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{OffsetHigh: lockOffsetHigh})
}

// tryLock 以不阻塞的方式拿独占锁；false 表示别人持有着。
func tryLock(f *os.File) (bool, error) {
	err := lockRange(f, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err == nil {
		return true, nil
	}
	if err == windows.ERROR_LOCK_VIOLATION {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{OffsetHigh: lockOffsetHigh})
}
```

- [ ] **Step 4: 跑测试，确认通过**

Run: `go test -race -count=3 ./internal/sessionlock/`
Expected: `ok  	github.com/brickkit/brickkit/internal/sessionlock`。其中 `TestALockHeldByAnotherProcessDiesWithIt` 是最要紧的一条：另一个进程持锁、被 `kill -9` 之后，锁必须空出来，而文件里还留着它的 PID。

- [ ] **Step 5: 确认 Windows / macOS 编得过**

Run: `make check-cross-build`
Expected: 退出码 0。

- [ ] **Step 6: 完整 lint**

Run: `make lint > /tmp/lint.log 2>&1; echo "exit=$?"`
Expected: `exit=0`，`internal 覆盖率` 仍高于门槛 92%（这个包自己约 82%，未覆盖的是几个几乎不可能触发的系统调用错误分支）。

- [ ] **Step 7: 提交**

```bash
git add internal/sessionlock/
git commit -m "$(cat <<'EOF'
新增：sessionlock 项目级会话锁——靠文件锁，持有者一死自动释放

同一个项目同一时刻只允许一个前台会话监管本地进程，另一个终端里的 status / down 要能看见
"有个会话在跑，PID xxx"。用 flock / LockFileEx 而不是 PID 文件：进程被 kill -9 之后内核
自动释放锁，判断有没有会话看的是锁本身，不用再猜文件里那个 PID 有没有被别的进程复用。
文件里的 PID 与启动时间只是给人看的提示。锁文件永远不删——删除会让正在等着 open 同一路径
的另一个进程拿到脱离目录的 inode。Windows 的锁是强制的，所以锁的是文件内容之外的一个字节，
好让别的进程读得到提示。测试里有一条用另一个进程持锁、再 kill -9 的跨进程用例。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: 收尾——竞态、确认没接线、回写执行结果

**Files:**
- Modify: `docs/superpowers/plans/2026-09-22-local-process-supervision.md`（本文件末尾追加"执行结果与遗留"）

- [ ] **Step 1: 两个新包的竞态与重复运行**

Run: `go test -race -count=3 ./internal/procsup/ ./internal/sessionlock/`
Expected: 两个包都 `ok`。

- [ ] **Step 2: 整个 `internal` 的竞态检测**

Run: `make test-race > /tmp/race.log 2>&1; echo "exit=$?"`
Expected: `exit=0`（要几分钟）。

- [ ] **Step 3: 确认确实没有接线**

Run: `grep -rn "internal/procsup\|internal/sessionlock" --include='*.go' cmd internal | grep -v "^internal/procsup/\|^internal/sessionlock/"`
Expected: 没有任何输出——除了这两个包自己，没有任何代码引用它们。这是本计划"先不接线"的证据，Plan 4 才会第一次引用。

- [ ] **Step 4: 跨平台再确认一次**

Run: `make check-cross-build`
Expected: 退出码 0。

- [ ] **Step 5: 回写执行结果**

在本文件末尾追加下面这一节，方括号里换成实际值；如果执行中有和计划不一样的地方（多做了什么、哪条测试为什么改了），如实写在"偏离计划的地方"里：

```markdown
## 执行结果与遗留（供 Plan 3、Plan 4 参考）

六个任务的提交依次是 `<Task 1 提交>` → `<Task 2>` → `<Task 3>` → `<Task 4>` → `<Task 5>`。

- `make lint` `exit=0`；`internal` 覆盖率 `<数字>%`（门槛 92%）；`make test-race` 干净。
- `procsup` 覆盖率 `<数字>%`，`sessionlock` 覆盖率 `<数字>%`。
- 没有接线：`grep` 确认除两个包自己之外没有任何引用。
- Windows / macOS：`make check-cross-build` 通过，**没有真机验证**（spec §6.5）。
- 偏离计划的地方：`<没有 / 具体写>`。
```

- [ ] **Step 6: 提交**

```bash
git add docs/superpowers/plans/2026-09-22-local-process-supervision.md
git commit -m "$(cat <<'EOF'
文档：Plan 2（本地进程监管）的执行结果回写进计划

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## 留给 Plan 4 的衔接点

下面是 Plan 4 里 `up` 的前台监管模式大致会怎么调用这两个包——**只是示意，不在本计划实现**。写在这里是为了让本计划的 API 形状有一个"真实调用方"可以对照：

```go
// 1. 会话锁：拿不到就报错，点名持有者
lock, err := sessionlock.Acquire(lockPath) // 锁文件放哪、要不要进 .gitignore，是 Plan 4 的决定
var held *sessionlock.HeldError
if errors.As(err, &held) { /* i18n 报错：这个项目已有一个 local 会话在跑，PID held.Info.PID */ }
defer func() { _ = lock.Release() }()

// 2. 前台会话自己接住停止信号（子进程不在终端的前台进程组里，收不到 Ctrl+C）
ctx, stop := signal.NotifyContext(ctx, procsup.StopSignals...)
defer stop()

// 3. 监管器：NameWidth 取所有 local 组件名里最长的，输出才对得齐；
//    TailLines 是用户想在崩溃汇总里看到最后几行（建议的旋钮：brickkit up --crash-lines N，默认 20，待确认）
sup, _ := procsup.New(procsup.Options{Out: stdout, NameWidth: widest, TailLines: crashLines})
defer sup.Shutdown()

// 4. 按拓扑序一个个启动，每个启动完做一次性的端口探测
for _, c := range localComponentsInTopologicalOrder {
	p, err := sup.Start(procsup.Spec{Name: c.ID, Argv: argv, Dir: srcDir, Env: env})
	if errors.Is(err, procsup.ErrStopped) { break } // 已经有进程崩了，会话在收尾：别再启动新的
	// 其他 err：命令根本起不来，按"硬失败"处理，提示手写 runCommand
	switch procsup.WaitListening(ctx, port, p.Done(), timeout) {
	case procsup.ProbeExited:   // 两种可能：它自己没跑起来（硬失败：报错、点名组件、提示手写 runCommand），
	                            // 或者别的进程崩了、它是被我们叫停的——到 sup.Exits() 里看谁 Crashed()
	case procsup.ProbeTimedOut: // 软警告：进程在跑但没监听期望端口，不杀
	case procsup.ProbeCanceled: // 用户按了 Ctrl+C：直接收尾
	}
}

// 5. 阻塞到全部退出或被取消；有进程崩了会话自己收尾，Run 随之返回
exits := sup.Run(ctx)

// 6. 收尾结束之后，在最后一屏只把崩溃的那几个打出来——不被刚才其余进程被停掉的输出冲走。
//    被叫停的、干净退出的都不打印；crashLines == 0 就只打信息、不打输出行
for _, e := range exits {
	if e.Crashed() { /* i18n：组件名、退出码或信号、跑了多久，然后逐行打出 e.Tail 供排查 */ }
}
```

几件 Plan 4（与 Plan 3）要记住的事：

- **面向用户的文字全归 Plan 4**：最后一屏的崩溃汇总、`HeldError` 对应的提示、探测三种结果对应的警告 / 报错，都走 `internal/msgid` 与两份 `catalog_*.go`，本计划的库里没有一句面向用户的话。
- **`--crash-lines` 是新增的 CLI 表面，Plan 4 写之前先跟用户确认。** 建议 `brickkit up --crash-lines N`，默认 `procsup.DefaultTailLines`（20），`0` = 只打印崩溃信息、不打印输出行（库那边 `TailLines <= 0` 取默认值，所以 0 要由 Plan 4 在打印时处理）。选 flag 而不是 `brickkit.yaml` 字段，是因为它是每个开发者当下的偏好、要"随时调整"，不该进版本库；想要个人的长期偏好，可以再加环境变量（比如 `BRICKKIT_CRASH_LINES`，参照 `BRICKKIT_LANG`）。项目里没有 `mode: local` 组件时写了它不起作用，按仓库惯例（`warnTargetOnlyFields`）要警告，不能静默。
- **`Spec.Env` 由 Plan 4 拼**：依赖地址、资源连接、自己的配置、`PORT`，用 `internal/cli/up_k8s.go` 的 `envLookup` 解析 `${VAR}`；库只负责把它们在内存里交给子进程。
- **管道不是 PTY**（设计决定 2）：Python 需要 `PYTHONUNBUFFERED=1`，属于 Plan 3 的语言适配器。
- **`brickkit` 被 `kill -9` 杀掉时 Unix 上的子进程会变孤儿**（设计决定 3）：如果 Plan 4 觉得不可接受，有两个办法——启动时先扫一遍上一次会话留下的孤儿，或者重新评估 `Pdeathsig`——但都不该悄悄加进库里。

## Self-Review Checklist（执行完 6 个任务后逐条核对）

- [ ] spec §3「Go 内置、进程组 / Job Object」→ Task 3（`proc_unix.go`、`proc_windows.go`）
- [ ] spec §3「日志按组件名做行前缀」→ Task 2 + Task 3（`NameWidth`）
- [ ] spec §3「不自动重启、退出码本身就是崩溃信号」+ 用户的决定「一个崩了全停，结束前告诉用户什么崩了」→ Task 3（`Exit.Crashed`、崩溃触发整个会话收尾、`Exit.Tail`）+ Task 2（`prefixWriter.Tail`）
- [ ] 用户的决定「只打印崩溃的进程；行数可调」→ Task 3 的 `Options.TailLines`（库，含默认值与环形缓冲）；用户能用的旋钮归 Plan 4
- [ ] spec §3「并发保护：项目目录级锁，进程死锁自动释放」→ Task 5（含 `kill -9` 之后锁空出来的跨进程用例）
- [ ] spec §3「`status` / `down` 跨终端可见性」→ Task 5 提供 `Inspect`；展示归 Plan 4
- [ ] spec §3「多个本地进程按拓扑序启动、不卡在健康检查」→ Task 3 的 `Start` 一次一个、立刻返回；编排归 Plan 4
- [ ] spec §4 / §6.4「一次性端口监听检测；进程崩了硬失败、没监听软警告」→ Task 4（四种结果）
- [ ] spec §6.5「Windows / macOS 只要理论上站得住」→ Task 1（`make check-cross-build`）+ Task 3、Task 5 的 Windows 文件
- [ ] `go test -race` 干净，`make lint` `exit=0`，`internal` 覆盖率不低于 92%
- [ ] 两个包除了各自的测试，没有被任何别的代码引用（Task 6 Step 3）
- [ ] `AGENTS.md` 与 `docs/` 没有任何改动；`deployment-selection-guide.md` 未改动
- [ ] 计划里的每个代码块都能原样通过 `go vet`（Task 3、5 的交叉编译步骤证明了 Windows / macOS）

## Execution Handoff

计划已保存到 `docs/superpowers/plans/2026-09-22-local-process-supervision.md`。两种执行方式：

**1. Subagent-Driven（推荐）**——每个任务派一个新的子代理去做，任务之间做审查，迭代更快
**2. Inline Execution**——在当前会话里按任务顺序执行，批量执行、有检查点

选哪种？
