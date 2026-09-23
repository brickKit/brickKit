// 本文件是 Step 15-A 的业务行为测试：`brickkit up` 真正把项目启动起来
// （004 §3.5）。覆盖 15.1–15.6、15.19、15.22–15.25。
//
// 引擎是假的：命令层的职责是"决定谁该启动、先检查什么、按什么顺序说给人听"，
// 不是"怎么调 docker"。真引擎另有真实运行验证。
package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/workspace"
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
	assert.Contains(t, r.stdout, "All components started")
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
	assert.NotContains(t, r.stdout, "All components started")
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
	assert.Contains(t, r.stderr, "Component", "还要说清是哪个组件在用它")
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
// 15.2–15.5 启停判定
// ============================================================

// 15.2：没人依赖、也没钉住的组件不会被启动。
func TestUpCascadeSkipsUnneededComponent(t *testing.T) {
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

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Empty(t, eng.ups, "15.2/15.4：一个都不该启动")
	assert.Contains(t, r.stdout, "No component will start this run")
}

// 一个都不跑时，得告诉使用者去改**哪一行**。
//
// 顶层有两种死法，指向配置里不同的行：自己被 mode: disable 关掉，
// 或者强依赖被关掉后跟着倒下。这里关的是 **people/basic**（不是顶层），
// erp/backend 那两行里根本没有 mode 字段。
//
// 从前这里只要看到**任何**组件被关就断言"顶层都被关掉了，移除它们的
// mode: disable"——照着去找只会扑空，还把人从上面那张表已经写对的
// 答案（"强依赖 people/basic 不启动"）上引开。
func TestNothingRunningDoesNotBlameTheTopLevel(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    mode: disable
  - id: erp/backend
    version: 1.0.0
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "No component will start this run")
	assert.Contains(t, r.stdout, "erp/backend@1.0.0", "要把顶层点名列出来")
	assert.NotContains(t, r.stdout, "Remove mode: disable from one of them",
		"erp/backend 没写过 mode: disable，叫人去删它只会扑空")
	assert.Contains(t, r.stdout, "The top level itself isn't turned off")
}

// 15.3：钉住的组件即使没人依赖也要启动。
func TestUpPinnedComponentStartsAnyway(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    mode: enabled
  - id: erp/backend
    version: 1.0.0
    mode: disable
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"people-basic-1-0-0"}, eng.lastUp(t).Services, "15.3")
}

// Plan 4a：mode: local 的组件不生成容器（跟 mode: debug 一样），它的服务名
// 不该出现在传给引擎的目标列表里——历史上漏掉这一半判断（当时漏的是
// servedBy）导致真机 `docker compose up` 报 no such service，collectTargets
// 的注释记着这次教训，这条测试直接守住它不再复发。
func TestUpModeLocalComponentIsNotAWorkloadTarget(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	// mode: local 从 Plan 4b 起要求本地源码目录（跟 mode: debug 不同——平台自己
	// 要 cd 进去执行探测出的命令）；main() 空函数立刻干净退出，探测阶段够用，
	// 后续任务真正启动它时也不会挂起等待。
	writeTree(t, workspace.SourceDir(f.Layout, "people/basic"), map[string]string{
		"go.mod":  "module example.com/basic\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    mode: local
  - id: erp/backend
    version: 1.0.0
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"erp-backend-1-0-0"}, eng.lastUp(t).Services,
		"mode: local 的组件不该混进传给引擎的目标列表")
}

// Task 5：容器与本地进程混部同一个项目——容器由引擎负责，mode: local 组件由
// runLocalComponents 负责，start() 里先 reportStarted 后 runLocalComponents，
// 输出顺序必须体现这一点：容器的汇报先出现，本地组件的"已监听端口"后出现。
func TestUpMixesContainerAndLocalComponents(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	writeTree(t, workspace.SourceDir(f.Layout, "people/basic"), map[string]string{
		"go.mod":  "module example.com/basic\n",
		"main.go": listenThenExitCleanly,
	})
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    mode: local
  - id: erp/backend
    version: 1.0.0
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	containerIdx := strings.Index(r.stdout, "erp-backend-1-0-0")
	localIdx := strings.Index(r.stdout, "listening on port")
	require.NotEqual(t, -1, containerIdx, r.stdout)
	require.NotEqual(t, -1, localIdx, r.stdout)
	assert.Less(t, containerIdx, localIdx, "容器汇报必须先于本地组件的输出出现")
	// mode: local 组件已经被 runLocalComponents 真的拉起来了，"下一步"
	// 提示不该再让使用者去 IDE 里手动加载它（那是 mode: debug 的话术）。
	assert.NotContains(t, r.stdout, "Local debugging")
}

// Task 6 手动验证时用真实 Docker 发现的真实 bug：一个项目里全部组件都是
// mode: local（没有任何容器要起），plan.services 因此是空切片，而
// start() 原来无条件调 eng.Up()——真 docker compose 对着一份 services: {}
// 的空文件跑 up 会报 "no service selected"，命令直接以 ENGINE_FAILED 收场，
// 而这个项目其实一个容器都不需要，不该被要求装 Docker。假引擎测不出这个
// bug（它对任何请求都来者不拒），这里只锁住"引擎压根不该被调用"这一半——
// 真 docker 会不会报错，由 Task 6 的手动验证覆盖。
func TestUpWithOnlyLocalComponentsNeverCallsTheEngine(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("这台机器没有 go 工具链")
	}
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	writeTree(t, workspace.SourceDir(f.Layout, "demo/hello"), map[string]string{
		"go.mod":  "module example.com/hello\n",
		"main.go": listenThenExitCleanly,
	})
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Empty(t, eng.ups, "这个项目没有容器要起，引擎压根不该被调用")
	assert.Contains(t, r.stdout, "listening on port")
}

// 15.5：钉住的组件强依赖了一个被显式关掉的组件——两个意图直接冲突，必须报错。
func TestUpDisabledStrongDependencyIsAnError(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    mode: disable
  - id: erp/backend
    version: 1.0.0
    mode: enabled
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

	assert.Contains(t, r.stdout, "📋 Component state calculation:", "15.22")
	assert.Contains(t, r.stdout, "✅", "15.22")
	assert.Contains(t, r.stdout, "📋 Start order", "15.23")
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

// mode: debug 的组件不由引擎启动，但要提示使用者去 IDE 里跑。
func TestUpWithLocalComponentTellsHowToDebug(t *testing.T) {
	f := localDebugProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, strings.Join(eng.lastUp(t).Services, ","), "people-basic",
		"local 组件没有容器，不该出现在启动列表里")
	assert.Contains(t, r.stdout, "Local debugging")
	assert.Contains(t, r.stdout, "local-debug.people-basic-1-0-0.env")
}

// servedBy 成员没有自己的容器，真机 `up`（非 --dry-run）传给引擎的目标
// service 列表里不该混进它的版本化服务名——否则 docker compose 会因为
// 这个 service 在生成文件里根本不存在而报 no such service，整个命令
// 直接失败，一个容器都起不来（brickKit 反馈：真机 brickkit up 对
// servedBy 成员报 no_such_service）。
func TestUpSkipsServedByComponentInTargetServices(t *testing.T) {
	f := servedByProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.ElementsMatch(t, []string{"infra-shell-go-core-1-0-0"}, eng.lastUp(t).Services,
		"servedBy 成员没有自己的容器，不该出现在启动目标里")
}

// ---- --ignore-served-by（brickKit 反馈：两个降低 servedBy 运维摩擦的
// 架构提案，提案二）：内存里清空全部 servedBy 声明再跑一次，验证"每个
// 组件必须能独立 brickkit up 起来"这条设计原则，不写回 brickkit.yaml ----

// 加了这个 flag，原本被收编的成员要当成独立组件一样启动，出现在引擎的
// 目标 service 列表里——这正好是 TestUpSkipsServedByComponentInTargetServices
// 不带这个 flag 时的反面。
func TestUpIgnoreServedByStartsMemberStandalone(t *testing.T) {
	f := servedByProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--ignore-served-by")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.ElementsMatch(t, []string{"infra-shell-go-core-1-0-0", "mdm-customer-1-0-7"}, eng.lastUp(t).Services,
		"--ignore-served-by 之后，mdm/customer 要像从没写过 servedBy 一样独立启动")
}

// 命中这个 flag 要在输出里留一句提示，跟 --dry-run 现有的提示风格一致，
// 免得使用者事后忘了这是一次非常规运行、把结果误当成真实的部署形态。
func TestUpIgnoreServedByPrintsBanner(t *testing.T) {
	f := servedByProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--ignore-served-by")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "All servedBy declarations are ignored")
}

// 这是一次内存里的验证运行，不是持久化配置的方式——brickkit.yaml 本身
// 一个字节都不该变（AGENTS.md §9.9：配置即真相，不搞临时覆盖落盘）。
func TestUpIgnoreServedByDoesNotModifyConfigFile(t *testing.T) {
	f := servedByProject(t)
	before, err := os.ReadFile(filepath.Join(f.Dir, "brickkit.yaml"))
	require.NoError(t, err)

	eng := newFakeEngine()
	r := runWithEngine(t, eng, f.Dir, "up", "--ignore-served-by")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	after, err := os.ReadFile(filepath.Join(f.Dir, "brickkit.yaml"))
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

// 空项目不该去调引擎。
func TestUpOnEmptyProjectDoesNothing(t *testing.T) {
	f := newProjectFixture(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Empty(t, eng.ups)
	assert.Contains(t, r.stdout, "The current project has no components")
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

// ============================================================
// Plan 4b：--crash-lines
// ============================================================

func TestUpWarnsWhenCrashLinesIsSetWithoutAnyModeLocalComponent(t *testing.T) {
	f := composeProject(t) // 现成的 fixture：erp/backend 依赖 people/basic，都不是 mode: local
	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run", "--crash-lines", "5")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr, "crash-lines")
}

func TestUpDoesNotWarnAboutCrashLinesWhenNotPassed(t *testing.T) {
	f := composeProject(t)
	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "crash-lines")
}

// ============================================================
// 外壳独立部署回落：外壳没跑时，servedBy 成员按普通组件独立部署
// ============================================================

func TestUpDryRunFallsBackServedByMemberWhenShellDisabled(t *testing.T) {
	comps := []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.7"},
	}
	f := addedProject(t, comps, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.7")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
    mode: disable
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
`)

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	composeContent := generatedCompose(t, f.Dir)
	assert.Contains(t, composeContent, "mdm-customer-1-0-7",
		"外壳没跑，这个成员该有自己生成的 service")
	assert.NotContains(t, composeContent, "infra-shell-go-core-1-0-0",
		"关掉的外壳自己不该出现")
}

// 只测 --dry-run 生成的文件不够：真正 up 的时候交给引擎的 service 列表是
// 单独一份判断算出来的（collectTargets），必须跟生成器用同一个判据，
// 否则文件里有这个 service、但没人真的把它启动起来，命令却报成功。
func TestUpStartsFallbackMemberForReal(t *testing.T) {
	comps := []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.7"},
	}
	f := addedProject(t, comps, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.7")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
    mode: disable
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	req := eng.lastUp(t)
	assert.Contains(t, req.Services, "mdm-customer-1-0-7",
		"外壳没跑，这个成员该被真的交给引擎启动，不是只出现在生成的文件里")
}

// 外壳没跑时，资源绑定校验不该再把外壳的绑定当成这个成员自己的绑定——
// 不然它会以"一切正常"的样子启动，实际上一个 DATABASE_* 都没有。
func TestUpBlocksFallbackMemberMissingItsOwnResourceBinding(t *testing.T) {
	comps := []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.7", ResourceDeps: []string{"database:postgresql"}},
	}
	f := addedProject(t, comps, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.7")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
    mode: disable
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0

resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: postgres
    port: 5432
    username: brickkit
    password: ${DB_PASSWORD}
    bindings:
      - componentId: infra/shell-go-core
        database: shared
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code, "外壳没跑，绑给外壳的资源不该再替成员挡住这条校验")
	assert.Contains(t, r.stderr, "mdm/customer",
		"报错要点名是哪个组件缺资源绑定")
}
