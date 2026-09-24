// 本文件是 Step 15-B 的业务行为测试：`brickkit status`（004 §3.7）。
// 覆盖 15.15–15.18。
//
// status 的价值在于"一眼看清现在是什么样"：谁在跑、谁没跑、为什么没跑、
// 哪些在 IDE 里、资源通不通。因此断言几乎都落在输出内容上。
package cli

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
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

// override.yaml 里的 mode: disable 让这个组件这次不跑——status 得说清楚这是本地
// override.yaml 造成的，不是 brickkit.yaml 自己写的，否则使用者会去改 brickkit.yaml
// 却怎么也改不动结果（设计书 §9："a component that isn't running because of a
// local disable is labeled as such, not left unexplained"）。
func TestStatusLabelsComponentDisabledByOverride(t *testing.T) {
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
	// erp/backend 是 override.yaml 亲自点名 disable 的那个，它这一行要带出处；
	// people/basic 只是跟着上层没跑（"nothing above it is starting"），它自己
	// 从没被 override.yaml 提过，这一行不该被误贴上 override.yaml 的标记——
	// 只断言"override.yaml"出现在 stdout 某处，测不出标记贴错行这种问题。
	erpLine := lineContaining(t, r.stdout, "erp/backend")
	assert.Contains(t, erpLine, "override.yaml")
	peopleLine := lineContaining(t, r.stdout, "people/basic")
	assert.NotContains(t, peopleLine, "override.yaml")
}

// mode: disable 是 brickkit.yaml 自己写的（没有 override.yaml 介入）：不该出现
// override.yaml 这个词——那会让使用者去一份完全无关的文件里找一个根本不存在的设置。
func TestStatusDoesNotLabelComponentDisabledInBrickkitYamlItself(t *testing.T) {
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
	assert.NotContains(t, r.stdout, "override.yaml")
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
// 直接构造 *project 调用 loadProject，一律走标准的 runIn/runWithEngine 命令
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
	layout := config.NewLayout(f.Dir, "")
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

// CLI 托管的资源（host 是服务名）在容器网络里，宿主机拨号根本连不上——
// 它的可达性要看容器状态，而不是去 dial 一个解析不了的主机名。
func TestStatusReportsManagedResourceFromContainerState(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
		{Service: "erp-backend-1-0-0", State: "running", Health: "healthy"},
		{Service: "postgres", State: "running", Health: "healthy"},
	}

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "Resource status", "15.18")
	assert.Contains(t, r.stdout, "postgres-main")
	assert.Contains(t, r.stdout, "reachable")
}

func TestStatusReportsManagedResourceDown(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
		{Service: "erp-backend-1-0-0", State: "running", Health: "healthy"},
		{Service: "postgres", State: "exited", ExitCode: 1},
	}

	r := statusOf(t, eng, f.Dir)

	assert.Contains(t, r.stdout, "postgres-main")
	assert.Contains(t, r.stdout, "unreachable")
}

// 外部资源（运维已部署）不在容器里，只能真的拨一下号。
func TestStatusProbesExternalResource(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: postgres-external
    host: db.internal.example.com
    port: 5432
    username: brickkit
    password: ${POSTGRES_PASSWORD}
    bindings:
      - componentId: people/basic
        database: brickkit_people
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	var probed []string
	r := runWith(t, func(o *Options) {
		o.Engine = eng
		o.Probe = func(_ context.Context, address string) error {
			probed = append(probed, address)
			return nil
		}
	}, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"db.internal.example.com:5432"}, probed, "15.18")
	assert.Contains(t, r.stdout, "postgres-external")
	assert.Contains(t, r.stdout, "reachable")
}

func TestStatusReportsUnreachableExternalResource(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0

resources:
  - kind: cache
    engine: redis
    id: redis-external
    host: 10.0.0.9
    port: 6379
    bindings:
      - componentId: people/basic
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWith(t, func(o *Options) {
		o.Engine = eng
		o.Probe = func(context.Context, string) error {
			return errors.New("connection refused")
		}
	}, f.Dir, "status")

	// 资源连不上不该让 status 失败：status 的职责是**报告**现状
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "redis-external")
	assert.Contains(t, r.stdout, "unreachable")
	assert.Contains(t, r.stdout, "connection refused", "说清连不上的原因")
}

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
// 未在运行、资源可达性问的是引擎与 brickkit.yaml。从前依赖图取不到就
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
	assert.NotContains(t, r.stdout, "Not running", "引擎里查不到 ≠ 它没起来")
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
	assert.Contains(t, r.stdout, "Not running")
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

// 资源可达性只问 brickkit.yaml 与 TCP，依赖图取不到不该连它一起丢掉。
func TestStatusStillProbesResourcesWhenGraphUnavailable(t *testing.T) {
	f, eng := startedProject(t)
	breakLocalManifest(t, f, "erp/backend")

	r := runWith(t, func(o *Options) {
		o.Engine = eng
		o.Probe = func(context.Context, string) error { return nil }
	}, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "postgres-main")
	assert.Contains(t, r.stdout, "reachable")
}
