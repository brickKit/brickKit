// 本文件是 Step 15-A 的业务行为测试：`brickkit up` 真正把项目启动起来
// （004 §3.5）。覆盖 15.1–15.6、15.19、15.22–15.25。
//
// 引擎是假的：命令层的职责是"决定谁该启动、先检查什么、按什么顺序说给人听"，
// 不是"怎么调 docker"。真引擎另有真实运行验证。
package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
)

// ============================================================
// 15.1 启动
// ============================================================

func TestUpStartsAllComponents(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.ElementsMatch(t,
		[]string{"people-basic-1-0-0", "erp-backend-1-0-0"},
		eng.lastUp(t).Services, "15.1")
	assert.Contains(t, r.stdout, "全部组件已启动")
}

// 部署文件交给引擎的是 CLI 刚生成的那一份。
func TestUpHandsGeneratedFileToEngine(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	expected := filepath.Join(f.Dir, ".brickkit", "generated", "docker-compose.yaml")
	assert.Equal(t, expected, eng.lastUp(t).File)
	assert.FileExists(t, expected)
}

// 引擎侧的项目名必须显式给出。
//
// compose 默认拿部署文件所在目录名当项目名，而我们的文件固定放在
// .brickkit/generated/ 下——那样同一台机器上**所有** BrickKit 项目
// 在引擎眼里都叫 "generated"，`up` 会把别的项目的容器顶掉，
// `down` 会停错项目。
func TestUpPassesProjectNameToEngine(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	project := eng.lastUp(t).Project
	assert.Contains(t, project, "my-erp", "项目名要能区分不同的 BrickKit 项目")
	assert.NotEqual(t, "generated", project)
}

// 启动完要如实汇报每个 service 的状态，而不是笼统一句"成功了"。
func TestUpReportsServiceStates(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
		{Service: "erp-backend-1-0-0", State: "running", Health: "healthy"},
	}

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "people-basic-1-0-0")
	assert.Contains(t, r.stdout, "erp-backend-1-0-0")
}

// 有 service 没起来时不能报"全部已启动"——那会让人以为可以开始用了。
func TestUpReportsPartialFailure(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
		{Service: "erp-backend-1-0-0", State: "exited", ExitCode: 1},
	}

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.NotEqual(t, clierr.ExitOK, r.code, "有组件没起来，退出码不能是 0")
	assert.Contains(t, r.stdout+r.stderr, "erp-backend-1-0-0")
	assert.NotContains(t, r.stdout, "全部组件已启动")
}

// 引擎自己失败时如实报出来，并保留它的输出（那才是真正有用的信息）。
func TestUpReportsEngineFailure(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()
	eng.upErr = errors.New("network brickkit-net not found")

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "network brickkit-net not found")
}

// ============================================================
// 15.6 --dry-run
// ============================================================

func TestUpDryRunDoesNotTouchTheEngine(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Empty(t, eng.ups, "15.6：--dry-run 不启动")
	assert.Empty(t, eng.checked, "--dry-run 也不该去问 registry")
}

// ============================================================
// 15.19 镜像拉取权限
// ============================================================

func TestUpChecksImagesBeforeStarting(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	assert.Len(t, eng.checked, 2, "15.19：每个要启动的组件都要检查镜像")
}

func TestUpImageUnauthorizedBlocksStart(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()
	eng.checkErr["registry.example.com/people-basic:1.0.0"] =
		clierr.New(clierr.CodeImageUnauthorized, "错误：镜像拉取未授权").
			WithDetail("镜像", "registry.example.com/people-basic:1.0.0").
			WithHint("执行 docker login <registry> 登录后重试")

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code, "15.19")
	assert.Contains(t, r.stderr, "docker login", "引擎给出的建议要原样传到使用者眼前")
	assert.Contains(t, r.stderr, "people-basic", "要说清是哪个镜像")
	assert.Contains(t, r.stderr, "组件", "还要说清是哪个组件在用它")
	assert.Empty(t, eng.ups, "镜像取不到就别启动了——启动只会得到一堆 ImagePullBackOff")
}

// 镜像检查失败但不是权限问题时，不要把引擎的说法换成"去 docker login"：
// 那会把人引向错误的方向（P18 踩过同样的坑）。
func TestUpImageCheckFailureKeepsTheRealReason(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()
	eng.checkErr["registry.example.com/people-basic:1.0.0"] =
		clierr.New(clierr.CodeNetworkUnreachable, "错误：无法连接镜像仓库").
			WithHint("检查网络与 registry 地址")

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "无法连接镜像仓库")
	assert.NotContains(t, r.stderr, "docker login")
}

// ============================================================
// 15.2–15.5 级联
// ============================================================

// 15.2：关掉一个组件，只有它自己不跑——它的依赖照常启动。
//
// 从前这里断言的是相反的事（"一个都不该启动"）：级联会从"没人需要它"
// 倒推出 people/basic 也该关掉。那种隐式决定已经删掉（003 §4.3）。
func TestUpDisablingOneComponentLeavesTheRestRunning(t *testing.T) {
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

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"people-basic-1-0-0"}, eng.lastUp(t).Services,
		"15.2：被关掉的只有 erp/backend")
	assert.Contains(t, r.stdout, "显式禁用")
}

// 15.3：enabled: true 与不写等价，都是启动。
func TestUpEnabledTrueStartsLikeOmitting(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    enabled: true
  - id: erp/backend
    version: 1.0.0
    enabled: false
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"people-basic-1-0-0"}, eng.lastUp(t).Services, "15.3")
}

// 15.5：强依赖了一个被显式关掉的组件——两个意图直接冲突，必须报错。
func TestUpDisabledStrongDependencyIsAnError(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    enabled: false
  - id: erp/backend
    version: 1.0.0
    enabled: true
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code, "15.5")
	assert.Contains(t, r.stderr, "people/basic")
	assert.Empty(t, eng.ups)
}

// ============================================================
// 15.22–15.25 输出
// ============================================================

func TestUpOutputShowsStatesAndOrder(t *testing.T) {
	f := composeProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	assert.Contains(t, r.stdout, "📋 组件状态计算：", "15.22")
	assert.Contains(t, r.stdout, "✅", "15.22")
	assert.Contains(t, r.stdout, "📋 启动顺序", "15.23")
	assert.Contains(t, r.stdout, "1. people-basic-1-0-0", "15.23")
}

func TestUpOutputShowsWeakDependencyWarning(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Optional: []string{"infra/bus@1.0.0"}},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout+r.stderr, "⚠️", "15.24")
	assert.Contains(t, r.stdout+r.stderr, "infra/bus")
}

// 15.25：迁移由部署文件驱动，但使用者得知道"这次会跑哪些迁移"。
func TestUpOutputShowsMigrations(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0", Migration: []string{"python", "manage.py", "migrate"}},
	}
	f := addedProject(t, comps, "people/basic@1.0.0")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "🔧", "15.25")
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "python manage.py migrate")
}

// 没有迁移的项目不该冒出一行空的"执行数据库迁移"。
func TestUpWithoutMigrationSaysNothingAboutIt(t *testing.T) {
	f := composeProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	assert.NotContains(t, r.stdout, "🔧")
}

// 启动完给出下一步：看状态、看日志。
func TestUpTellsWhatToDoNext(t *testing.T) {
	f := composeProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	assert.Contains(t, r.stdout, "brickkit status")
	assert.Contains(t, r.stdout, "logs")
}

// local: true 的组件不由引擎启动，但要提示使用者去 IDE 里跑。
func TestUpWithLocalComponentTellsHowToDebug(t *testing.T) {
	f := localDebugProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, strings.Join(eng.lastUp(t).Services, ","), "people-basic",
		"local 组件没有容器，不该出现在启动列表里")
	assert.Contains(t, r.stdout, "本地调试")
	assert.Contains(t, r.stdout, "local-debug.people-basic-1-0-0.env")
}

// 空项目不该去调引擎。
func TestUpOnEmptyProjectDoesNothing(t *testing.T) {
	f := newProjectFixture(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Empty(t, eng.ups)
	assert.Contains(t, r.stdout, "当前项目没有组件")
}

// CLI 打印给使用者的 logs 命令必须带 -p。
//
// 不带的话 compose 会拿部署文件所在目录名（generated）当项目名，
// 而容器在 brickkit-<项目> 底下——命令**静默返回空**，不报错也没有输出，
// 使用者会以为组件根本没打日志。真跑验证时撞到的。
func TestUpPrintsUsableLogsCommand(t *testing.T) {
	f := composeProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Contains(t, r.stdout, "logs")
	for _, line := range strings.Split(r.stdout, "\n") {
		if strings.Contains(line, "compose") && strings.Contains(line, "logs") {
			assert.Contains(t, line, "-p brickkit-my-erp", "logs 命令要能真的用：%s", line)
		}
	}
}

// 组件没起来时给出的排障命令同样要能用。
func TestFailureHintPrintsUsableLogsCommand(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()
	eng.statuses = []engine.Status{
		{Service: "people-basic-1-0-0", State: "running", Health: "healthy"},
		{Service: "erp-backend-1-0-0", State: "exited", ExitCode: 1},
	}

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Contains(t, r.stderr, "-p brickkit-my-erp")
}
