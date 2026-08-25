// 本文件是 Step 15-B 的业务行为测试：`brickkit status`（004 §3.7）。
// 覆盖 15.15–15.18。
//
// status 的价值在于"一眼看清现在是什么样"：谁在跑、谁没跑、为什么没跑、
// 哪些在 IDE 里、资源通不通。因此断言几乎都落在输出内容上。
package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
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
	assert.Contains(t, r.stdout, "项目状态", "15.15")
	assert.Contains(t, r.stdout, "my-erp")
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "1.0.0")
	assert.Contains(t, r.stdout, "运行中")
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
	assert.Contains(t, r.stdout, "退出码 1", "退出码是排障的第一手信息")
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
func TestStatusBeforeFirstUp(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "尚未启动")
	assert.Contains(t, r.stdout, "brickkit up")
}

// 部署文件在、但引擎里一个容器都没有：说明被 down 掉了。
func TestStatusWhenNothingIsRunning(t *testing.T) {
	f, eng := startedProject(t)
	eng.statuses = []engine.Status{}

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "没有正在运行的组件")
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
    enabled: false
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "erp/backend")
	assert.Contains(t, r.stdout, "显式禁用", "15.16：要说清为什么没跑")
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "上层都不启动", "15.16：跟着上层不跑的也要给出原因")
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
	assert.Contains(t, r.stdout, "本地调试", "15.17")
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "localhost:8081")
}

// local 组件没有容器，不该被当成"没起来"报出来。
func TestStatusDoesNotReportLocalComponentAsDown(t *testing.T) {
	f := localDebugProject(t)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)
	eng.statuses = []engine.Status{
		{Service: "department-tree-1-0-0", State: "running", Health: "healthy"},
	}

	r := statusOf(t, eng, f.Dir)

	assert.NotContains(t, r.stdout, "未创建")
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
	assert.Contains(t, r.stdout, "资源状态", "15.18")
	assert.Contains(t, r.stdout, "postgres-main")
	assert.Contains(t, r.stdout, "可达")
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
	assert.Contains(t, r.stdout, "不可达")
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
	assert.Contains(t, r.stdout, "可达")
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
	assert.Contains(t, r.stdout, "不可达")
	assert.Contains(t, r.stdout, "connection refused", "说清连不上的原因")
}

// 没有声明资源的项目不该冒出一个空的"资源状态"小节。
func TestStatusWithoutResources(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := statusOf(t, eng, f.Dir)

	assert.NotContains(t, r.stdout, "资源状态")
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
