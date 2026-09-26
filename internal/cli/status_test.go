// 本文件是 Step 15-B 的业务行为测试：`brickkit status`（004 §3.7）。
// 覆盖 15.15–15.18。
//
// status 的价值在于"一眼看清现在是什么样"：谁在跑、谁没跑、为什么没跑、
// 哪些在 IDE 里、资源通不通。因此断言几乎都落在输出内容上。
package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/sessionlock"
)

// statusOf 用假引擎执行 status。
func statusOf(t *testing.T, eng *fakeEngine, dir string) result {
	t.Helper()
	return runWithEngine(t, eng, dir, "status")
}

// ============================================================
// 15.15 运行中的组件
// ============================================================

func TestStatusShowsRunningComponents(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy", Ports: "8080/tcp"},
		{Service: "erp-backend-1-0-0", State: "running", Health: "healthy",
			Ports: "0.0.0.0:18080->8080/tcp"},
	}

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Project status", "15.15")
	assert.Contains(t, r.stdout, "my-erp")
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "1.0.0")
	assert.Contains(t, r.stdout, "Running")
	assert.Contains(t, r.stdout, "18080->8080", "端口映射要看得见")
}

// 没起来的组件要单独标出来，而不是混在"运行中"里。
func TestStatusSeparatesUnhealthyComponent(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
		{Service: "erp-backend-1-0-0", State: "exited", ExitCode: 1},
	}

	r := statusOf(t, eng, f.Dir)

	assert.Contains(t, r.stdout, "erp/backend")
	assert.Contains(t, r.stdout, "exited")
	assert.Contains(t, r.stdout, "exit code 1", "退出码是排障的第一手信息")
}

// 迁移容器不该出现在组件列表里：它是平台的实现细节，不是使用者装的组件。
func TestStatusHidesMigrationContainers(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
		{Service: "people-basic-1-0-0-migration", State: "exited"},
		{Service: "erp-backend-1-0-0", State: "running", Health: "healthy"},
	}

	r := statusOf(t, eng, f.Dir)

	assert.NotContains(t, r.stdout, "migration")
}

// 还没 up 过时给出引导，而不是一张空表。
//
// 从前这里靠"生成的部署文件在不在"判断，据此打印"项目尚未启动过"。
// 那个判据是错的（那份文件随时可能被 git clean 清掉），而且"还没起过"
// 与"已经 down 过"引擎本来就分不出——所以现在两种情况给同一句话，
// 它对两者都成立，也都指向同一个下一步。
func TestStatusBeforeFirstUp(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "No components are running")
	assert.Contains(t, r.stdout, "brickkit up")
	assert.Contains(t, r.stdout, "not created", "组件逐个列出来，而不是一张空表")
}

// 部署文件在、但引擎里一个容器都没有：说明被 down 掉了。
func TestStatusWhenNothingIsRunning(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{}

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "No components are running")
}

// ============================================================
// 15.16 未启动的组件及原因
// ============================================================

func TestStatusShowsSkippedComponentsWithReason(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
    mode: disable
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "erp/backend")
	assert.Contains(t, r.stdout, "disabled explicitly", "15.16：要说清为什么没跑")
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "nothing above it is starting", "15.16：跟着上层不跑的也要给出原因")
}

// 本地模式下 deploy.local.yaml 里的 mode: disable 让这个组件这次不跑——status 得说清楚
// 这是个人文件造成的，不是团队的 deploy.yaml 写的，否则使用者会去改 deploy.yaml
// 却怎么也改不动结果。
func TestStatusLabelsComponentDisabledInLocalFile(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: erp/backend
    mode: disable
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	// erp/backend 是本地文件亲自改成 disable 的那个，它这一行要带出处；
	// people/basic 只是跟着上层没跑（"nothing above it is starting"），这一行
	// 不该被误贴上 deploy.local.yaml 的标记——只断言它出现在 stdout 某处，
	// 测不出标记贴错行这种问题。
	erpLine := lineContaining(t, r.stdout, "erp/backend")
	assert.Contains(t, erpLine, "mode: disable, via deploy.local.yaml)",
		"要并进已有的括注里，而不是再叠一层独立括号")
	assert.NotContains(t, erpLine, ") (deploy.local.yaml)", "不该出现双重括号")
	peopleLine := lineContaining(t, r.stdout, "people/basic")
	assert.NotContains(t, peopleLine, "deploy.local.yaml")
}

// mode: disable 是团队的 deploy.yaml 自己写的（本地模式没开）：不该出现
// deploy.local.yaml 这个词——那会让使用者去一份完全无关的文件里找一个根本不存在的设置。
func TestStatusDoesNotLabelComponentDisabledInTeamFile(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
    mode: disable
`)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "erp/backend")
	assert.NotContains(t, r.stdout, "deploy.local.yaml")
}

// ============================================================
// 15.17 本地调试组件
// ============================================================

func TestStatusShowsLocalComponents(t *testing.T) {
	f := localDebugProject(t)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "Local debugging", "15.17")
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "localhost:8081")
}

// debug 组件没有容器，不该被当成"没起来"报出来。
func TestStatusDoesNotReportLocalComponentAsDown(t *testing.T) {
	f := localDebugProject(t)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)
	eng.statuses = []engine.Status{
		{Service: "department-tree-1-0-0", State: "running", Health: "healthy"},
	}

	r := statusOf(t, eng, f.Dir)

	assert.NotContains(t, r.stdout, "not created")
}

// containerRefs 不该把 mode: local 组件算进"要生成容器的组件"——跟
// mode: debug 一样，它没有容器，塞进这份列表只会让 status 把它错当成
// "该有容器却没查到"报出来。不直接调用私有方法——这个包里没有任何既有测试
// 直接构造 *liveProject 调用 loadProject，一律走标准的 runIn/runWithEngine 命令
// 管线断言渲染出的文本，这条测试延续同一个惯例。
func TestStatusDoesNotReportModeLocalComponentAsNotRunning(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "not created", "mode: local 组件不该被当成缺容器报出来")
}

// ============================================================
// mode: local 的会话锁提示
// ============================================================

// 会话锁被持有时（模拟"另一个终端正在跑 brickkit up"），status 要打一行
// 指向信息，点名 PID——这是使用者唯一能从这个终端知道"那边有 local 会话
// 在跑"的办法。
func TestStatusShowsHintWhenLocalSessionIsRunning(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	layout := project.NewLayout(f.Dir)
	held, err := sessionlock.Acquire(layout.SessionLockPath())
	require.NoError(t, err)
	defer func() { _ = held.Release() }()
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, strconv.Itoa(os.Getpid()), "要点名持有者的 PID")
}

// 没有会话在跑时，不该冒出这条提示——沉默才是"没有事发生"的正确信号。
func TestStatusShowsNoHintWhenNoLocalSessionIsRunning(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "session")
}

// 项目里压根没有 mode: local 组件时，不该去碰会话锁文件——没有意义，
// 也避免每次 status 都多一次无谓的文件系统访问。
func TestStatusSkipsSessionCheckWhenNoModeLocalComponent(t *testing.T) {
	f, eng := startedProject(t)

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "session")
}

// ============================================================
// 15.18 资源状态
// ============================================================

// 没有声明资源的项目不该冒出一个空的"资源状态"小节。
func TestStatusWithoutResources(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := statusOf(t, eng, f.Dir)

	assert.NotContains(t, r.stdout, "Resource status")
}

// 引擎问不到状态时如实报错——比给出一张"全都没在跑"的假表好。
func TestStatusReportsEngineFailure(t *testing.T) {
	f, eng := startedProject(t)
	eng.statusErr = errors.New("cannot connect to the Docker daemon")

	r := statusOf(t, eng, f.Dir)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "Docker daemon")
}

// status 给出的排障命令同样要带 -p，否则跑出来是空的。
func TestStatusPrintsUsableLogsCommand(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "exited", ExitCode: 1},
		{Service: "erp-backend-1-0-0", State: "exited", ExitCode: 1},
	}

	r := statusOf(t, eng, f.Dir)

	assert.Contains(t, r.stdout, "-p brickkit-my-erp")
}

// ============================================================
// 依赖图取不到时的降级（A′）
// ============================================================

// 依赖图取不到时，"现在什么在跑"照样要答得出来。
//
// status 的五节里，只有"未启动"那一列**原因**真的需要依赖图；运行中、
// 未在运行问的是引擎与 brickkit.yaml / 部署文件。从前依赖图取不到就
// 整条命令报错退出，另外四节一起没了——使用者连"容器还在不在"都问不到，
// 而那恰恰是他打开 status 想知道的第一件事。
func TestStatusReportsRunningWhenGraphUnavailable(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy", Ports: "8080/tcp"},
	}
	breakLocalManifest(t, f, "erp/backend")

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Running")
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "The dependency graph could not be resolved", "信息不全就得说清楚为什么")
	assert.Contains(t, r.stdout, "prot", "把解析失败的原因原样带出来，他才知道去改哪一行")
	assert.NotContains(t, r.stdout, "Resource status", "status 早就没有资源状态这一节了")
}

// 降级时，brickkit.yaml 里声明过的组件一个都不能少。
//
// 少列一个的代价是使用者以为组件没了；而把"引擎里查不到"算成"未在运行"
// 又是在冤枉它——那一节的意思是"该跑却没跑"，可这时恰恰判不出它该不该跑。
// 所以一律进"未启动"，原因写实话。
func TestStatusListsEveryDeclaredComponentWhenGraphUnavailable(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
	}
	breakLocalManifest(t, f, "erp/backend")

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "erp/backend", "声明过的组件不能凭空消失")
	assert.Contains(t, r.stdout, "reason unknown")
	assert.NotContains(t, r.stdout, "❌ Not running", "引擎里查不到 ≠ 它没起来")
}

// 引擎里有记录、只是没在跑，那就是实打实的"未在运行"——降级也照报。
func TestStatusKeepsFailedComponentWhenGraphUnavailable(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
		{Service: "erp-backend-1-0-0", State: "exited", ExitCode: 1},
	}
	breakLocalManifest(t, f, "erp/backend")

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "❌ Not running")
	assert.Contains(t, r.stdout, "exit code 1")
}

// mode: disable 是 brickkit.yaml 里就写着的，不该跟着退化成"原因未知"。
func TestStatusKeepsDisabledReasonWhenGraphUnavailable(t *testing.T) {
	f, eng := startedProject(t)
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
    mode: disable
`)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
	}
	breakLocalManifest(t, f, "erp/backend")

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "disabled explicitly")
	assert.NotContains(t, r.stdout, "reason unknown", "配置里写着的原因就该照实说")
}

// status 回答的是"现在什么在跑"：config/ 里的问题不该让它报错退出。
func TestStatusIgnoresConfigProblems(t *testing.T) {
	f, eng := startedProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.Layout.ConfigDir(), "people-basic@1.0.0.yaml"),
		[]byte("X: $var:NOPE\n"), 0o644))

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "people/basic")
}
