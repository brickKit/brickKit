package cli

import (
	"bytes"
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/resolver"
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
