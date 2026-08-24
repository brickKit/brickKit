package cli

// 本文件测 `external:` 在命令层的行为（P39）。
//
// # 这些用例是被一次真跑逼出来的
//
// 生成层（compose / k8s）与注入层都已经正确处理了 external，单元测试全绿。
// 然后真跑两个项目，消费方 `up` 当场失败：
//
//	no such service: demo-hello-1-0-0
//
// 原因是命令层另有一份**"这次要启动哪些服务"**的名单，它是按依赖图算的，
// 不看生成物——于是把一个根本没生成的服务名传给了 docker。
//
// 教训不在于漏了一处，而在于**"不生成它"这件事分散在三层**：
// 生成层不写、命令层不点名、引擎层不清理。三处任意一处漏掉，
// 表现都不一样：这次是当场报错（还算幸运），
// 漏在别处可能是"启动成功但没连上"那种要几周才发现的。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// deployedExternalEngine 是"对方项目已经部署过"的假引擎。
//
// 多数 external 用例测的是别的东西（点不点名、查不查镜像、输出标不标注），
// 它们都得先过 up 的启动前检查——对方没部署时那一步直接阻断（见文件末尾）。
func deployedExternalEngine() *fakeEngine {
	eng := newFakeEngine()
	eng.networks = map[string]bool{"brickkit-platform-shared-net": true}
	return eng
}

// externalProject 造「caller 依赖 external 的 hello」。
func externalProject(t *testing.T) *projectFixture {
	t.Helper()

	f := addedProject(t, []comp{
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		{ID: "demo/hello", Version: "1.0.0"},
	}, "demo/caller@1.0.0")

	f.writeConfig(t, `components:
  - id: demo/caller
    version: 1.0.0
  - id: demo/hello
    version: 1.0.0
    external:
      project: platform-shared
`)
	return f
}

// 启动时不能点名 external 组件的服务。
//
// 它没有被生成，点名它 docker 会直接报 `no such service` 并**整个 up 失败**——
// 连本项目自己的组件都起不来。
func TestUpDoesNotStartExternalService(t *testing.T) {
	f := externalProject(t)
	eng := deployedExternalEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	services := eng.lastUp(t).Services
	assert.NotContains(t, services, "demo-hello-1-0-0",
		"P39：它没被生成，点名它会让整个 up 失败（真跑到过 no such service）")
	assert.Contains(t, services, "demo-caller-1-0-0", "本项目自己的组件照常启动")
}

// 也不该为 external 组件做镜像预检。
//
// 镜像由**对方项目**负责拉。在这边检查，轻则多一次无谓的 registry 往返，
// 重则因为本项目没有那个私有仓库的凭据而**误报一个根本不属于自己的问题**。
func TestUpDoesNotCheckExternalImage(t *testing.T) {
	f := externalProject(t)
	eng := deployedExternalEngine()

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	for _, image := range eng.checkedImages() {
		assert.NotContains(t, image, "demo-hello",
			"P39：镜像由对方项目负责拉，这边检查只会误报别人的问题")
	}
}

// 输出里要看得出它是外部的。
//
// 不标注的话，使用者看到"✅ demo/hello@1.0.0"会以为是自己这次起的，
// 于是去 `docker ps` 里找它——找不到，然后开始怀疑平台。
func TestUpMarksExternalComponentInOutput(t *testing.T) {
	f := externalProject(t)

	r := runWithEngine(t, deployedExternalEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "platform-shared",
		"P39：要说清它由哪个项目部署，否则使用者会去 docker ps 里白找一遍：%s", r.stdout)
}

// ============================================================
// external 依赖的项目没部署（P39，Docker 目标）
// ============================================================

// 对方没部署时 `up` 当场失败，而且要说清楚：哪个组件、缺哪张网络、去哪个项目 up。
//
// compose 自己也会失败，但它给的是
//
//	network brickkit-platform-shared-net declared as external, but could not be found
//
// 里面没有 external、没有对方的项目名、也没有下一步——使用者得先知道
// 网络名是 brickkit-<项目名>-net 才能反推回去。
func TestUpFailsClearlyWhenExternalProjectNotDeployed(t *testing.T) {
	f := externalProject(t)
	eng := newFakeEngine() // networks 为 nil：一张网络都不存在

	r := runWithEngine(t, eng, f.Dir, "up")

	require.NotEqual(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "external 依赖的项目还没部署")
	assert.Contains(t, out, "platform-shared", "要点名是哪个项目")
	assert.Contains(t, out, "brickkit-platform-shared-net", "要点名缺哪张网络")
	assert.Contains(t, out, "brickkit up", "要给出下一步")
	assert.Empty(t, eng.ups, "没通过检查就不该去调引擎")
}

// 对方部署过（网络在）时照常启动。
func TestUpProceedsWhenExternalNetworkExists(t *testing.T) {
	f := externalProject(t)
	eng := deployedExternalEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.NotContains(t, eng.lastUp(t).Services, "demo-hello-1-0-0",
		"external 组件由对方部署，不该被点名启动")
}

// ============================================================
// 同一个坑的另外三处：status / down / order
// ============================================================
//
// 上面那条教训（"不生成它"分散在三层，漏一处表现各不相同）当时只在 `up` 上
// 修到位。`down` / `status` / `order` 共用的是**另一份**名单
// （internal/cli/lifecycle.go 的 project），它只认识 local: true，
// 不认识 external——于是同一个坑在这三处原样重现。

// status 不能把 external 组件报成"未在运行"。
//
// 那是一条**常驻的假失败**：本项目从来就不为它生成容器，引擎里当然查不到。
// 使用者看到红叉会去 docker ps 里找它，找不到，然后开始怀疑平台。
func TestStatusDoesNotReportExternalAsDown(t *testing.T) {
	f := externalProject(t)
	eng := deployedExternalEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "未创建",
		"external 组件不该出现在「未在运行」那张表里")
	assert.Contains(t, r.stdout, "外部依赖", "要单列一节说清楚它由谁部署")
	assert.Contains(t, r.stdout, "platform-shared")
}

// down --only 点名 external 组件时，不能把它的服务名递给引擎。
//
// 递过去换来的是 `no such service`，而且是**整条命令失败**——与 up 上那次
// 一模一样。这里断言的是"引擎没收到这个服务名"，而不只是"命令成功了"。
func TestDownOnlyExternalDoesNotReachEngine(t *testing.T) {
	f := externalProject(t)
	eng := deployedExternalEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down", "--only", "demo/hello")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Empty(t, eng.downs, "一个 down 都不该发出去")
	assert.Contains(t, r.stdout, "没有可停止的组件")
	assert.Contains(t, r.stdout, "platform-shared", "要说清为什么停不了")
}

// **最危险的那条路**：点名的组件全都停不了时，绝不能退化成"停整个项目"。
//
// 空的服务名列表在引擎眼里就是"停全部"。过滤掉 external 之后如果不显式拦一道，
// 一条 `down --only <一个 external 组件>` 会把本项目所有东西都停掉——
// 那比原来的报错严重得多。
func TestDownOnlyExternalNeverStopsWholeProject(t *testing.T) {
	f := externalProject(t)
	eng := deployedExternalEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	require.Equal(t, clierr.ExitOK,
		runWithEngine(t, eng, f.Dir, "down", "--only", "demo/hello").code)

	assert.Empty(t, eng.downs,
		"空的服务名列表会被引擎理解成「停整个项目」——必须在到达引擎之前拦下")
}

// 混着点名时，能停的照停，停不了的说清楚。
func TestDownOnlyMixedStopsWhatItCan(t *testing.T) {
	f := externalProject(t)
	eng := deployedExternalEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down", "--only", "demo/hello,demo/caller")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	require.Len(t, eng.downs, 1)
	assert.Equal(t, []string{"demo-caller-1-0-0"}, eng.downs[0].Services,
		"只停本项目自己的那个")
	assert.Contains(t, r.stdout, "platform-shared")
}

// down --only 点名一个 local: true 组件同理：它也没有容器。
//
// 与 external 是同一个判据（"本项目生不生成它的容器"），
// 单列一条是因为它走的是另一个分支、给的是另一句理由。
func TestDownOnlyLocalComponentDoesNotReachEngine(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		{ID: "demo/hello", Version: "1.0.0"},
	}, "demo/caller@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/caller
    version: 1.0.0
  - id: demo/hello
    version: 1.0.0
    local: true
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down", "--only", "demo/hello")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Empty(t, eng.downs)
	assert.Contains(t, r.stdout, "IDE", "要说清它跑在哪儿")
}

// order 也要标注 external：启动顺序里出现它，不说一句就像本项目要启动它。
func TestOrderMarksExternalComponent(t *testing.T) {
	f := externalProject(t)

	r := runWithEngine(t, deployedExternalEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "别的项目")
	assert.Contains(t, r.stdout, "platform-shared")
}
