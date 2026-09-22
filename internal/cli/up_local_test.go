package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/sessionlock"
	"github.com/brickkit/brickkit/internal/workspace"
)

// addedProject（remove_test.go）只用 `add`（不带 --repo）搭项目：只拉 Manifest
// 缓存，从不往 components/ 下写源码目录（AGENTS.md §2.3：add 默认不 clone 源码）。
// 所以这条测试不需要任何"删掉源码目录"的步骤——它天然就不存在。
func TestUpModeLocalWithoutSourceDirectoryIsAnError(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    mode: local
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, "people/basic")
	assert.Contains(t, r.stdout+r.stderr, "mode: local")
}

// 不借 tests/components/demo-hello（那是给仓库自测容器化部署用的完整固件，
// 依赖多、体积大）——本地探测测试只需要一个能被 internal/runcmd 认出来的
// 最小 Go 目录，照 Plan 3 TestADetectedGoCommandReallyRuns 的路子，直接用
// writeTree 把 go.mod/main.go 写进 workspace.SourceDir。
func TestCollectLocalComponentsDetectsARealGoFixture(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	writeTree(t, workspace.SourceDir(f.Layout, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)
	order, err := resolver.Order(graph.Subgraph(states.Running()))
	require.NoError(t, err)
	env, err := inject.Build(cfg, graph, states)
	require.NoError(t, err)
	genResult, err := compose.Generate(cfg, graph, states, env, compose.Options{Engine: compose.EngineDocker})
	require.NoError(t, err)

	plans, err := collectLocalComponents(f.Layout, cfg, graph, order, genResult.LocalEnvFiles, envLookup(f.Dir))

	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, []string{"go", "run", "."}, plans[0].Command.Argv)
}

// ============================================================
// Plan 4b：本地进程环境变量的严格展开
// ============================================================

func TestBuildLocalEnvExpandsResolvableVars(t *testing.T) {
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	vars := []inject.Var{{Name: "DATABASE_PASSWORD", Value: "${DB_PASSWORD}"}}
	lookup := func(name string) (string, bool) {
		if name == "DB_PASSWORD" {
			return "secret123", true
		}
		return "", false
	}

	env, err := buildLocalEnv(ref, vars, 9000, []string{"PYTHONUNBUFFERED=1"}, lookup)

	require.NoError(t, err)
	assert.Equal(t, []string{"DATABASE_PASSWORD=secret123", "PYTHONUNBUFFERED=1", "PORT=9000"}, env)
}

func TestBuildLocalEnvErrorsOnUnresolvableVar(t *testing.T) {
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	vars := []inject.Var{{Name: "DATABASE_PASSWORD", Value: "${DB_PASSWORD}"}}
	lookup := func(string) (string, bool) { return "", false }

	_, err := buildLocalEnv(ref, vars, 9000, nil, lookup)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PASSWORD")
	assert.Contains(t, err.Error(), "people/basic")
}

func TestBuildLocalEnvSkipsExistingSecretRef(t *testing.T) {
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	vars := []inject.Var{{Name: "API_KEY", ExistingSecretRef: "some-k8s-secret"}}

	env, err := buildLocalEnv(ref, vars, 9000, nil, func(string) (string, bool) { return "", false })

	require.NoError(t, err)
	assert.Equal(t, []string{"PORT=9000"}, env, "existingSecret 在 docker-only 的 local 模式下没有对应的值")
}

// 两个 mode: local 组件之间也有依赖时，启动顺序必须跟着拓扑序走，不能因为
// 都不生成容器就各起各的。collectLocalComponents 直接复用 up.go 其余地方
// 已经在用的 order.Steps（resolver.Order 的结果），只是按 localMode 过滤，
// 相对顺序原样保留——这条测试锁住这一点，而不是只信"代码看起来对"。
func TestCollectLocalComponentsPreservesTopologicalOrderBetweenTwoLocalComponents(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0", Requires: []string{"demo/friend@1.0.0"}},
		{ID: "demo/friend", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	writeTree(t, workspace.SourceDir(f.Layout, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	writeTree(t, workspace.SourceDir(f.Layout, "demo/friend"), map[string]string{
		"go.mod":  "module example.com/friend\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
  - id: demo/friend
    version: 1.0.0
    mode: local
`)

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)
	order, err := resolver.Order(graph.Subgraph(states.Running()))
	require.NoError(t, err)
	env, err := inject.Build(cfg, graph, states)
	require.NoError(t, err)
	genResult, err := compose.Generate(cfg, graph, states, env, compose.Options{Engine: compose.EngineDocker})
	require.NoError(t, err)

	plans, err := collectLocalComponents(f.Layout, cfg, graph, order, genResult.LocalEnvFiles, envLookup(f.Dir))

	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.Equal(t, "demo-friend-1-0-0", plans[0].Service, "被依赖的 demo/friend 必须排在前面")
	assert.Equal(t, "demo-hello-1-0-0", plans[1].Service, "依赖方 demo/hello 必须排在后面")
}

// ============================================================
// Plan 4b：真实进程的前台监管
// ============================================================

// internal/cli/root.go 的 Run 只会 root.ExecuteC()，测试里没有办法注入一个
// 可取消的 context.Context——真实调用链上 cmd.Context() 永远是
// context.Background()。用两个会自己在短时间内终止的一次性小 Go 程序，
// 完全不需要外部取消。

// listenThenExitCleanly：先监听（net.Listen 是同步调用，返回时端口已经绑定，
// procsup.WaitListening 一定能探测到），再睡一小会儿、正常退出（code 0）。
const listenThenExitCleanly = `package main

import (
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	l, err := net.Listen("tcp", ":"+os.Getenv("PORT"))
	if err != nil {
		os.Exit(2)
	}
	go http.Serve(l, nil)
	time.Sleep(500 * time.Millisecond)
	os.Exit(0)
}
`

// exitImmediately：完全不监听、立刻以非零退出码结束——procsup 会在
// exited 通道关闭时立刻返回 ProbeExited，不需要等到探测超时。
const exitImmediately = `package main

import "os"

func main() { os.Exit(1) }
`

// crashAfterPrinting 先打一行能认出来的标记，再以非零码崩溃——用来验证
// --crash-lines 0 时这一行不该出现在最后一屏（TailLines 本身是否捕获到它
// 是 procsup 自己的事，CLI 层要在渲染时把它压下去）。
const crashAfterPrinting = `package main

import "os"

func main() {
	println("distinctive-crash-output-marker")
	os.Exit(1)
}
`

// localComponentPlansFor 搭一个真实项目、真的跑一遍解析+生成，返回
// collectLocalComponents 的结果——这是 Task 5 把 runLocalComponents 接进
// runUp 之前，Task 4 独立验证它的路子：不依赖还不存在的那条接线。
func localComponentPlansFor(t *testing.T, f *projectFixture, mainGo string) (*Options, []localComponentPlan) {
	t.Helper()
	writeTree(t, workspace.SourceDir(f.Layout, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": mainGo,
	})

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)
	order, err := resolver.Order(graph.Subgraph(states.Running()))
	require.NoError(t, err)
	env, err := inject.Build(cfg, graph, states)
	require.NoError(t, err)
	genResult, err := compose.Generate(cfg, graph, states, env, compose.Options{Engine: compose.EngineDocker})
	require.NoError(t, err)

	plans, err := collectLocalComponents(f.Layout, cfg, graph, order, genResult.LocalEnvFiles, envLookup(f.Dir))
	require.NoError(t, err)
	require.Len(t, plans, 1)

	opts := &Options{WorkDir: f.Dir, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	return opts, plans
}

// 同一个项目不能有两个前台会话同时跑——第二个会话拿不到锁，要报错点名
// 第一个会话的 PID，而不是安静地跟第一个会话抢同一批端口、同一份输出。
// 用测试进程自己先拿一次锁模拟"已经有一个会话在跑"：flock 锁的是文件
// 描述符对应的那次 open，不是进程本身，同一个进程里两次 Acquire 打开的是
// 两个独立的文件描述符，第二次一样会被第一次挡住。
func TestRunLocalComponentsRefusesASecondConcurrentSession(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	opts, plans := localComponentPlansFor(t, f, "package main\n\nfunc main() {}\n")

	held, err := sessionlock.Acquire(f.Layout.SessionLockPath())
	require.NoError(t, err)
	defer func() { _ = held.Release() }()

	err = runLocalComponents(context.Background(), opts, f.Layout, plans, 20)

	require.Error(t, err)
	assert.Contains(t, err.Error(), strconv.Itoa(os.Getpid()), "要点名持有者的 PID")
}

func TestRunLocalComponentsStartsARealComponentAndItListens(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	opts, plans := localComponentPlansFor(t, f, listenThenExitCleanly)

	err := runLocalComponents(context.Background(), opts, f.Layout, plans, 20)

	out := opts.Stdout.(*bytes.Buffer).String()
	require.NoError(t, err, out)
	assert.Contains(t, out, "demo-hello-1-0-0")
	assert.Contains(t, out, "listening on port")
}

func TestRunLocalComponentsReportsACrash(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	opts, plans := localComponentPlansFor(t, f, exitImmediately)

	err := runLocalComponents(context.Background(), opts, f.Layout, plans, 20)

	out := opts.Stdout.(*bytes.Buffer).String()
	require.Error(t, err)
	assert.Contains(t, out, "demo-hello-1-0-0")
	assert.Contains(t, out, "exit code 1")
}

// --crash-lines 的帮助文本承诺"0 = 只打印崩溃信息，不带输出行"——但
// procsup.Options.TailLines 自己的约定是 "<= 0 时用默认行数"（它自己的
// TestTailKeepsOnlyTheConfiguredNumberOfLines 锁死的），同一个 0 在两层
// 意思正好相反。这条测试锁住 CLI 这一层必须自己兑现"0 就是 0 行"的承诺，
// 不能假设 procsup 内部会照办（手动验证 Task 6 Step 5 用真实进程 + 真实
// --crash-lines 0 才发现这个两层语义对不上的真实 bug）。
func TestRunLocalComponentsCrashLinesZeroPrintsNoOutputLines(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	opts, plans := localComponentPlansFor(t, f, crashAfterPrinting)

	err := runLocalComponents(context.Background(), opts, f.Layout, plans, 0)

	out := opts.Stdout.(*bytes.Buffer).String()
	require.Error(t, err)
	assert.Contains(t, out, "demo-hello-1-0-0")
	assert.Contains(t, out, "exit code 1", "崩溃信息本身还在")
	// 标记行会在进程运行期间被实时流式打印一次（procsup 正常的输出转发，
	// 不受 --crash-lines 影响，也不该受影响——那是"正在发生的事"，跟最后一屏
	// 复述的 tail 是两回事）。只断言最后一屏"崩溃了"那段之后不再重复它，
	// 而不是断言它从没出现过。
	crashSection := out[strings.Index(out, "The following local component(s) crashed:"):]
	assert.NotContains(t, crashSection, "distinctive-crash-output-marker",
		"0 = 最后一屏不带任何输出行；实时流式输出不算")
}
