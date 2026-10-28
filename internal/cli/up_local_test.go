package cli

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/runcmd"
	"github.com/brickkit/brickkit/internal/sessionlock"
)

// addedProject（remove_test.go）只用 `add`（不带 --repo）搭项目：只拉 Manifest
// 缓存，从不往 components/ 下写源码目录（add 默认不 clone 源码）。
// 所以这条测试不需要任何"删掉源码目录"的步骤——它天然就不存在。
func TestUpModeLocalWithoutSourceDirectoryIsAnError(t *testing.T) {
	// 从 git 源装的组件没有 --repo 克隆时，本地没有它的仓库，mode: local 无从启动
	g := newGitOrgProject(t)
	g.release(comp{ID: "people/basic", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "people/basic@1.0.0")
	setMode(t, dir, "people/basic", "local")

	r := g.run(dir, "up", "--dry-run")

	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, "people/basic")
	assert.Contains(t, r.stdout+r.stderr, "mode: local")
}

// 不借 tests/components/demo-hello（那是给仓库自测容器化部署用的完整固件，
// 依赖多、体积大）——本地探测测试只需要一个能被 internal/runcmd 认出来的
// 最小 Go 目录，照 Plan 3 TestADetectedGoCommandReallyRuns 的路子，直接用
// writeTree 把 go.mod/main.go 写进组件的本地仓库（f.repoDir）。
func TestCollectLocalComponentsDetectsARealGoFixture(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	writeTree(t, f.repoDir(t, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)

	plans, err := localPlans(t, f)

	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, []string{"go", "run", "."}, plans[0].Command.Argv)
}

// ============================================================
// Plan 4b：本地进程环境变量的严格展开
// ============================================================

func TestBuildLocalEnvUsesEvaluatedValues(t *testing.T) {
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	file := compose.LocalEnvFile{Vars: []inject.Var{
		{Name: "DB_PASSWORD", Value: inject.Literal("secret123")}, {Name: "PORT", Value: inject.Literal("9000")},
	}}

	env, err := buildLocalEnv(ref, "", file, []string{"PYTHONUNBUFFERED=1"})

	require.NoError(t, err)
	assert.Equal(t, []string{"DB_PASSWORD=secret123", "PORT=9000", "PYTHONUNBUFFERED=1"}, env)
}

// brickkit 自己拉起的进程对缺失的 ${VAR} 是严格的：带着字面占位符启动只会换来
// 一个莫名其妙的运行时错误。报错要点名组件、缺的环境变量与它要填的配置项。
func TestBuildLocalEnvErrorsOnUnresolvableVar(t *testing.T) {
	ref := resolver.Ref{ID: "people/basic", Version: "1.0.0"}
	file := compose.LocalEnvFile{
		Vars:       []inject.Var{{Name: "DB_PASSWORD", Value: inject.Literal("${PEOPLE_DB_PW}")}},
		Unresolved: map[string]string{"DB_PASSWORD": "PEOPLE_DB_PW"},
	}

	_, err := buildLocalEnv(ref, "", file, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PEOPLE_DB_PW")
	assert.Contains(t, err.Error(), "people/basic")
}

// brickKit 反馈：shell 里曾经全局设过 NODE_OPTIONS/JAVA_TOOL_OPTIONS（哪怕是为了
// 别的原因、这次运行根本没打算调试这个组件），mode: local 的子进程会原样继承
// procsup 追加的 os.Environ()、悄悄卡在等调试器连接——buildLocalEnv 必须按语言
// 主动清空这几个变量，保证没人主动要求调试时启动总是干净的。
func TestBuildLocalEnvStripsNodeDebugEnvVar(t *testing.T) {
	ref := resolver.Ref{ID: "demo/hello", Version: "1.0.0"}

	env, err := buildLocalEnv(ref, runcmd.LangNode, compose.LocalEnvFile{}, nil)

	require.NoError(t, err)
	assert.Contains(t, env, "NODE_OPTIONS=")
}

func TestBuildLocalEnvStripsJavaDebugEnvVars(t *testing.T) {
	ref := resolver.Ref{ID: "demo/hello", Version: "1.0.0"}

	env, err := buildLocalEnv(ref, runcmd.LangJava, compose.LocalEnvFile{}, nil)

	require.NoError(t, err)
	assert.Contains(t, env, "JAVA_TOOL_OPTIONS=")
	assert.Contains(t, env, "JDK_JAVA_OPTIONS=")
}

// Go/Python 的启动方式不会被任何环境变量意外触发调试等待，不需要清空任何东西——
// 这条测试锁住"只按语言表清空"，不是无条件地给所有语言塞两条空值。
func TestBuildLocalEnvDoesNotStripUnaffectedLanguages(t *testing.T) {
	ref := resolver.Ref{ID: "demo/hello", Version: "1.0.0"}

	env, err := buildLocalEnv(ref, runcmd.LangGo, compose.LocalEnvFile{}, nil)

	require.NoError(t, err)
	assert.Empty(t, env)
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
	writeTree(t, f.repoDir(t, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	writeTree(t, f.repoDir(t, "demo/friend"), map[string]string{
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

	plans, err := localPlans(t, f)

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
// collectLocalComponents 的结果——单独验证收集这一步，不经过 runUp 与
// runLocalComponents 的接线。
func localComponentPlansFor(t *testing.T, f *projectFixture, mainGo string) (*Options, []localComponentPlan) {
	t.Helper()
	writeTree(t, f.repoDir(t, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": mainGo,
	})

	plans, err := localPlans(t, f)
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

// brickKit 反馈：本地进程会继承启动 brickkit up 那个 shell 的完整环境
// （procsup 是 append(os.Environ(), spec.Env...)）——如果这个 shell 里曾经
// 为了别的原因全局设过 NODE_OPTIONS，mode: local 启动的 Node 组件会在没人
// 主动要求调试的情况下卡住等调试器连接。这条测试用真实 node/npm 端到端验证
// buildLocalEnv 的清空确实生效：t.Setenv 模拟"shell 里全局设过"，若清空没
// 生效，npm 自己（它也是个 Node 进程）会先被 --inspect-brk 卡住、永远不会
// 把 server.js 拉起来，10 秒的 ctx 超时会让测试明确失败，而不是真的卡住
// 整个测试进程。
func TestRunLocalComponentsStripsInheritedNodeDebugEnvVar(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("这台机器没有 node")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("这台机器没有 npm")
	}
	t.Setenv("NODE_OPTIONS", "--inspect-brk=0")

	comps := []comp{{ID: "demo/webapp", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/webapp@1.0.0")
	writeTree(t, f.repoDir(t, "demo/webapp"), map[string]string{
		"package.json": `{"scripts": {"start": "node server.js"}}`,
		"server.js": `const http = require("http");
const port = process.env.PORT;
const server = http.createServer((req, res) => res.end("ok"));
server.listen(port, () => {
	setTimeout(() => process.exit(0), 500);
});
`,
	})
	f.writeConfig(t, `components:
  - id: demo/webapp
    version: 1.0.0
    mode: local
`)

	plans, err := localPlans(t, f)
	require.NoError(t, err)
	require.Len(t, plans, 1)

	opts := &Options{WorkDir: f.Dir, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = runLocalComponents(ctx, opts, f.Layout, plans, 20)

	out := opts.Stdout.(*bytes.Buffer).String()
	require.NoError(t, err, out)
	assert.Contains(t, out, "listening on port",
		"NODE_OPTIONS 被继承的话，node/npm 会卡在等调试器连接，探测不到监听端口")
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

// 端口被别的程序占着：启动之前就报 PORT_CONFLICT，一个进程都不启动——否则就绪探测连上的是
// 那个程序，先报一句假的"已监听"（反馈 FRB-002）。
func TestRunLocalComponentsRefusesATakenPortBeforeStartingAnything(t *testing.T) {
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
	holder, err := net.Listen("tcp", ":"+strconv.Itoa(plans[0].Port))
	require.NoError(t, err)
	defer func() { _ = holder.Close() }()

	err = runLocalComponents(context.Background(), opts, f.Layout, plans, 20)

	require.Error(t, err)
	assert.Equal(t, clierr.CodePortConflict, clierr.As(err).Code)
	text := clierr.As(err).Format()
	assert.Contains(t, text, strconv.Itoa(plans[0].Port))
	assert.Contains(t, text, "demo-hello-1-0-0")
	assert.Contains(t, text, "localPort", "主端口挪得动：提示写 localPort")
	out := opts.Stdout.(*bytes.Buffer).String()
	assert.NotContains(t, out, "listening on port")
	assert.NotContains(t, out, "Starting", "一个进程都没启动")
}

// 本机进程要绑的端口：自己的主端口（写 localPort 挪得动）、extraPorts，外壳还有它替成员监听的端口。
func TestPortsOfCoversExtraPortsAndHostedMembers(t *testing.T) {
	shell := &manifest.Manifest{Deployment: manifest.Deployment{Port: 8080,
		ExtraPorts: []manifest.ExtraPort{{Name: "admin", Port: 9000}}}}
	member := &resolver.Node{Ref: resolver.Ref{ID: "erp/a", Version: "1.0.0"}, Manifest: &manifest.Manifest{
		Deployment: manifest.Deployment{Port: 8081, ExtraPorts: []manifest.ExtraPort{{Name: "grpc", Port: 9090}}}}}

	ports := portsOf("erp-shell-1-0-0", 8085, shell, []*resolver.Node{member})

	assert.Equal(t, []localPort{
		{Port: 8085, Of: "erp-shell-1-0-0", Movable: true},
		{Port: 9000, Of: "erp-shell-1-0-0 (admin)"},
		{Port: 8081, Of: "erp-a-1-0-0"},
		{Port: 9090, Of: "erp-a-1-0-0 (grpc)"},
	}, ports)
}

// --crash-lines 的帮助文本承诺"0 = 只打印崩溃信息，不带输出行"——但
// procsup.Options.TailLines 自己的约定是 "<= 0 时用默认行数"（它自己的
// TestTailKeepsOnlyTheConfiguredNumberOfLines 锁死的），同一个 0 在两层
// 意思正好相反。这条测试锁住 CLI 这一层必须自己兑现"0 就是 0 行"的承诺，
// 不能假设 procsup 内部会照办（用真实进程 + 真实
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

// localPlans 走一遍与 up 相同的流水线，拿到 collectLocalComponents 的结果。
func localPlans(t *testing.T, f *projectFixture) ([]localComponentPlan, error) {
	t.Helper()
	proj := f.project(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, proj), proj)
	require.NoError(t, err)
	order, err := resolver.Order(graph.Subgraph(states.Running()))
	require.NoError(t, err)
	env, err := inject.Build(proj, graph, states)
	require.NoError(t, err)
	genResult, err := compose.Generate(proj, graph, states, env, compose.Options{
		Engine: compose.EngineDocker, Root: f.Dir, Lookup: envref.Lookup(f.Dir),
	})
	require.NoError(t, err)
	return collectLocalComponents(proj, graph, order, genResult.LocalEnvFiles, genResult.MemberEnvFiles, hostedMembers(proj, states))
}

// 本机进程运行的组件不跑迁移容器，提醒里要说出它真实的 mode：mode: local 的组件被说成
// "mode: debug 组件"，读的人会去找一个根本没写过的 debug。
func TestLocalMigrationNoteNamesTheRealMode(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml":                     "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n",
		"deploy.yaml":                       "target: docker\ncomponents:\n  - id: erp/api\n    mode: local\n",
		"components/erp/api/component.yaml": comp{ID: "erp/api", Version: "1.0.0", Migration: []string{"/app/api", "migrate"}}.yamlText(),
		"components/erp/api/go.mod":         "module erp/api\n\ngo 1.22\n",
		"components/erp/api/main.go":        "package main\n\nfunc main() {}\n",
	})
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "a mode: local component's database migration won't run automatically")
	assert.NotContains(t, out, "mode: debug component's database migration")
}

// 没写 local.migrationCommand：提醒里不再给出镜像里的路径当作"在本机手动运行"的命令（照着敲退出码 127），
// 而是说它只在镜像里运行，从源码运行要组件写 local.migrationCommand（反馈 FRR-005）。
func TestLocalMigrationNoteDoesNotOfferTheImagePath(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml":                     "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n",
		"deploy.yaml":                       "target: docker\ncomponents:\n  - id: erp/api\n    mode: local\n",
		"components/erp/api/component.yaml": comp{ID: "erp/api", Version: "1.0.0", Migration: []string{"/app/api", "migrate"}}.yamlText(),
		"components/erp/api/go.mod":         "module erp/api\n\ngo 1.22\n",
		"components/erp/api/main.go":        "package main\n\nfunc main() {}\n",
	})
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	out := r.stdout + r.stderr
	assert.NotContains(t, out, "by hand on this machine")
	assert.Contains(t, out, i18n.T(msgid.ComposeHintMigrationOnlyInImage, "/app/api migrate"))
	assert.Contains(t, out, "local.migrationCommand")
}

// 写了 local.migrationCommand：mode: local 时 up 自己从源码跑它，不再提醒"不会自动执行"，
// dry-run 把它列进启动前的迁移；mode: debug 时进程归使用者，提醒里给的是这条从源码运行的命令。
func TestLocalMigrationCommandIsListedOrHandedOver(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	tree := func(mode string) string {
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{
			"brickkit.yaml": "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n",
			"deploy.yaml":   "target: docker\ncomponents:\n  - id: erp/api\n",
			"components/erp/api/component.yaml": comp{ID: "erp/api", Version: "1.0.0",
				Migration: []string{"/app/api", "migrate"}, LocalMigration: []string{"go", "run", ".", "migrate"}}.yamlText(),
			"components/erp/api/go.mod":  "module erp/api\n\ngo 1.22\n",
			"components/erp/api/main.go": "package main\n\nfunc main() {}\n",
		})
		// mode: debug 只能写在 deploy.local.yaml：两种 mode 都走本地模式那份文件
		r := runWithEngine(t, newFakeEngine(), dir, "local", "on")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
		writeTree(t, dir, map[string]string{
			"deploy.local.yaml": "target: docker\ncomponents:\n  - id: erp/api\n    mode: " + mode + "\n",
		})
		return dir
	}

	r := runWithEngine(t, newFakeEngine(), tree("local"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	out := r.stdout + r.stderr
	assert.NotContains(t, out, "database migration won't run automatically")
	assert.Contains(t, out, i18n.T(msgid.CliUpDatabaseMigrationsThatRunBefore))
	assert.Contains(t, out, "erp/api@1.0.0  go run . migrate")

	r = runWithEngine(t, newFakeEngine(), tree("debug"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	out = r.stdout + r.stderr
	assert.Contains(t, out, "a mode: debug component's database migration won't run automatically")
	assert.Contains(t, out, i18n.T(msgid.ComposeHintRunMigrationByHand, "go run . migrate"))
}

// local.migrationCommand 在启动进程之前跑，用的是同一份环境；成功了进程照常启动。
func TestRunLocalComponentsRunsTheMigrationFirst(t *testing.T) {
	opts, layout, plans := localMigrationPlans(t, 0)

	err := runLocalComponents(context.Background(), opts, layout, plans, 20)

	out := opts.Stdout.(*bytes.Buffer).String()
	require.NoError(t, err, out)
	migrated := strings.Index(out, "migrated with PORT=")
	listening := strings.Index(out, "listening on port")
	require.GreaterOrEqual(t, migrated, 0, out)
	require.Greater(t, listening, migrated, "迁移在进程启动之前：%s", out)
	assert.Contains(t, out, "demo-hello-1-0-0-migration")
	assert.Contains(t, out, "migrated with PORT="+strconv.Itoa(plans[0].Port), "迁移拿到的是进程那份环境")
}

// 迁移失败：报 MIGRATION_FAILED，进程不启动——与容器迁移失败挡住主服务同理。
func TestRunLocalComponentsDoesNotStartAfterAFailedMigration(t *testing.T) {
	opts, layout, plans := localMigrationPlans(t, 3)

	err := runLocalComponents(context.Background(), opts, layout, plans, 20)

	out := opts.Stdout.(*bytes.Buffer).String()
	require.Error(t, err)
	assert.Equal(t, clierr.CodeMigrationFailed, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Format(), "go run . migrate")
	assert.Contains(t, clierr.As(err).Format(), "exit code") // go run 把程序的退出码一律换成 1，具体数不比
	assert.NotContains(t, out, "listening on port", "迁移失败，进程不启动")
}

// localMigrationPlans 搭一个写了 local.migrationCommand 的 mode: local 组件：迁移打印它拿到的 PORT，
// 再以 migrateExit 退出；主进程监听 PORT、等探测连上之后干净退出（同 listenThenExitCleanly）。
func localMigrationPlans(t *testing.T, migrateExit int) (*Options, project.Layout, []localComponentPlan) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{{ID: "demo/hello", Version: "1.0.0", Migration: []string{"/app/hello", "migrate"},
		LocalMigration: []string{"go", "run", ".", "migrate"}}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	main := `package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		fmt.Println("migrated with PORT=" + os.Getenv("PORT"))
		os.Exit(` + strconv.Itoa(migrateExit) + `)
	}
	l, err := net.Listen("tcp", ":"+os.Getenv("PORT"))
	if err != nil {
		os.Exit(2)
	}
	go http.Serve(l, nil)
	time.Sleep(500 * time.Millisecond)
}
`
	opts, plans := localComponentPlansFor(t, f, main)
	require.Len(t, plans[0].Migrations, 1)
	return opts, f.Layout, plans
}

// mode: local 的进程继承终端环境，但平台管的名字只能来自平台：保留名、*_ENDPOINT、组件自己的配置项。
// 否则终端里 export 过的 DEMO_BUS_ENDPOINT 会让"可选依赖缺席时变量不存在"失效，旧的 DB_HOST 会盖过 config/。
func TestLocalProcessesDoNotInheritNamesThePlatformOwns(t *testing.T) {
	m := &manifest.Manifest{ConfigSchema: &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DB_HOST": {Type: "string"},
	}}}
	withhold := platformOwned(m)
	for _, name := range []string{"DB_HOST", "DEMO_BUS_ENDPOINT", "PORT", "COMPONENT_ID", "COMPONENT_VERSION",
		"BRICKKIT_SERVED_MEMBERS", "BRICKKIT_SERVED_MEMBERS_CONFIG"} {
		assert.True(t, withhold(name), name)
	}
	for _, name := range []string{"PATH", "HOME", "GOPATH", "DB_HOST_EXTRA"} {
		assert.False(t, withhold(name), name)
	}
}

// Ctrl+C 之后等最慢的那个：本机进程里声明得最长的宽限期；都没声明时是 0，交给 procsup 的默认值。
func TestLocalGracePeriodIsTheLongestDeclared(t *testing.T) {
	assert.Equal(t, time.Duration(0), localGracePeriod([]localComponentPlan{{}, {}}))
	assert.Equal(t, 25*time.Second, localGracePeriod([]localComponentPlan{
		{StopGrace: 10 * time.Second}, {}, {StopGrace: 25 * time.Second},
	}))
}

// 组件有迁移、带着自己的代码（写了 build），却没写 local.migrationCommand：lint 提醒作者——从源码运行它时
// （mode: local、焦点运行、本机外壳里）迁移不会跑。只给契约、不给代码的组件没有源码可跑，不提醒。
func TestLintWarnsWhenACodeCarryingComponentHasNoMigrationFromSource(t *testing.T) {
	lint := func(c comp) string {
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{
			"brickkit.yaml":                     "project: shop\nsources:\n  - {name: dev, type: local, path: ./components}\ncomponents:\n  - {id: erp/api, version: 1.0.0}\n",
			"deploy.yaml":                       "target: docker\ncomponents:\n  - id: erp/api\n",
			"components/erp/api/component.yaml": c.yamlText(),
		})
		r := runWithEngine(t, newFakeEngine(), dir, "lint")
		return r.stdout + r.stderr
	}
	warning := i18n.T(msgid.ManifestLocalMigrationMissing, "erp/api")

	assert.Contains(t, lint(comp{ID: "erp/api", Version: "1.0.0", Build: true, Migration: []string{"/app/api", "migrate"}}), warning)
	assert.NotContains(t, lint(comp{ID: "erp/api", Version: "1.0.0", Build: true, Migration: []string{"/app/api", "migrate"},
		LocalMigration: []string{"go", "run", ".", "migrate"}}), warning)
	assert.NotContains(t, lint(comp{ID: "erp/api", Version: "1.0.0", Migration: []string{"/app/api", "migrate"}}), warning,
		"只写 image：没有源码可跑")
}
