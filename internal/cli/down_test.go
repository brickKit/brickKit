// 本文件是 Step 15-B 的业务行为测试：`brickkit down`（004 §3.6）。
// 覆盖 15.13、15.14、15.21。
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// startedProject 是一个已经 up 过、容器正在跑的项目。
//
// 不能清掉 eng.ups：假引擎正是靠它回答"现在有什么在跑"（见 fakeEngine.Status）。
// 清掉之后 down 会以为引擎里一个容器都没有——那是夹具的副作用，不是真实行为。
// down 的断言看的是 eng.downs，与 ups 互不干扰。
func startedProject(t *testing.T) (*projectFixture, *fakeEngine) {
	t.Helper()

	f := composeProject(t)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)
	return f, eng
}

// ============================================================
// 15.13 停止
// ============================================================

func TestDownStopsTheProject(t *testing.T) {
	f, eng := startedProject(t)

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, eng.downs, 1)
	// 交给引擎的只有项目名：停的是"这个项目现在跑着的一切"，
	// 而不是"生成目录里此刻写着的那些"（005 §5.9.3）
	assert.Equal(t, "brickkit-my-erp", eng.downs[0].Project)
	assert.Contains(t, r.stdout, "已停止")
}

// 停的必须是本项目：项目名传错会停掉别人的容器。
func TestDownPassesProjectName(t *testing.T) {
	f, eng := startedProject(t)

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "down").code)

	assert.Contains(t, eng.downs[0].Project, "my-erp")
}

// 15.13：数据卷必须保留。这一条在引擎层也有用例（不带 -v），
// 这里再从使用者视角确认一次：输出要明确说数据还在。
func TestDownTellsThatDataIsKept(t *testing.T) {
	f, eng := startedProject(t)

	r := runWithEngine(t, eng, f.Dir, "down")

	assert.Contains(t, r.stdout, "数据", "15.13：要让使用者知道数据没被删")
	assert.Contains(t, r.stdout, "docker volume rm", "并告诉他真想删该怎么做")
}

// 从没 up 过就 down：照样问引擎，然后如实说"没有容器在跑"。
//
// 这里的关键是**仍然调了引擎**。从前它看的是"生成的部署文件在不在"，
// 不在就提前返回、一条命令都不发——而那份文件在 .gitignore 里，
// 随时可能被 git clean 清掉（见下面那条回归用例）。
func TestDownWithNothingRunning(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "down")

	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	require.Len(t, eng.downs, 1, "该问的还是要问引擎，不能靠猜")
	assert.Contains(t, r.stdout, "没有容器在跑")
	assert.Contains(t, r.stdout, "brickkit up", "顺手告诉他下一步")
}

// ⚠️ 回归：生成目录被清掉之后，down 必须照样停得掉。
//
// `.brickkit/generated/` 在 .gitignore 里，003 §7.1 还明说它"整个都是可再生的"——
// 一次 `git clean -xdf` 就没了。从前 down 拿它在不在当"项目跑没跑"的判据，
// 于是这种情况下会报"📋 项目尚未启动过"、退出码 0、引擎一次都不调，
// 而容器好好地跑着。这与当初把 DownRequest.File 拿掉是同一个 bug，
// 当时只修了引擎那一层，闸门留在了命令层。
func TestDownStillStopsAfterGeneratedDirWiped(t *testing.T) {
	f, eng := startedProject(t)
	require.NoError(t, os.RemoveAll(filepath.Join(f.Dir, ".brickkit", "generated")))

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, eng.downs, 1, "生成目录没了，但容器还在——必须照样停")
	assert.Equal(t, "brickkit-my-erp", eng.downs[0].Project)
	assert.Contains(t, r.stdout, "已停止")
}

// status 同理：生成目录没了，也不能谎报"尚未启动过"。
func TestStatusStillReadsEngineAfterGeneratedDirWiped(t *testing.T) {
	f, eng := startedProject(t)
	require.NoError(t, os.RemoveAll(filepath.Join(f.Dir, ".brickkit", "generated")))

	r := runWithEngine(t, eng, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "运行中", "引擎说在跑，就得说在跑")
	assert.NotContains(t, r.stdout, "尚未启动")
}

func TestDownReportsEngineFailure(t *testing.T) {
	f, eng := startedProject(t)
	eng.downErr = errors.New("no such network")

	r := runWithEngine(t, eng, f.Dir, "down")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "no such network")
}

// ============================================================
// 15.14 只停其中几个：改 enabled 再 up
// ============================================================

// `down --only` 已删除（003 §4.3：要收窄范围就改配置）。它的用途由
// "写 enabled: false 再 up" 覆盖——生成的部署文件里没有它，
// 而 up 带着清理选择器（Docker 侧即 `--remove-orphans`），
// 引擎会把它的容器一并移除。
//
// 这条用例守的就是那句承诺：`down` 的帮助文本里写着这条路，它必须真的通。
func TestDisablingAComponentRemovesItsContainerOnNextUp(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)
	require.Contains(t, eng.lastUp(t).Services, "erp-backend-1-0-0", "前提：它本来在跑")

	// people/basic 要钉住：它唯一的上层就是 erp/backend，
	// 不钉的话它跟着一起不跑，那就成了"整个项目都停"，测不到这条
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    enabled: true
  - id: erp/backend
    version: 1.0.0
    enabled: false

resources:
  - kind: database
    engine: postgresql
    id: postgres-main
    host: postgres
    port: 5432
    username: brickkit
    password: ${POSTGRES_PASSWORD}
    bindings:
      - componentId: people/basic
        database: brickkit_people
`)
	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, eng.lastUp(t).Services, "erp-backend-1-0-0")
	assert.NotEmpty(t, eng.lastUp(t).PruneSelector,
		"必须带上清理选择器，否则那个容器会一直留着")
	assert.NotContains(t, generatedCompose(t, f.Dir), "erp-backend-1-0-0")
}

// 停止顺序交给引擎：compose 本身就按依赖倒序停。
//
// 15.21 要的是"依赖方先停、被依赖方后停"——不带服务名时 compose 自己就这么做，
// CLI 再排一遍只是多一份会与它分叉的真相。
func TestDownDelegatesStopOrderToTheEngine(t *testing.T) {
	f, eng := startedProject(t)

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	require.Len(t, eng.downs, 1)
	assert.Equal(t, "brickkit-my-erp", eng.downs[0].Project,
		"只交项目名，顺序由引擎负责")
}

// ============================================================
// 项目标签选择器：up 与 down 必须是同一个值
// ============================================================

// k8sProjectWithNamespace 造一个**命名空间与项目名不同**的 K8s 项目。
//
// 这个差异是下面两条用例的全部意义：默认命名空间是 brickkit-<项目名>，
// 两者只差一个前缀，把命名空间误当成标签值也能"看起来对"。
// 写了 deploy.namespace 之后它们毫不相干，错就藏不住了。
func k8sProjectWithNamespace(t *testing.T, namespace string) *projectFixture {
	t.Helper()

	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")

	var b strings.Builder
	b.WriteString("project: my-erp\n\ndeploy:\n  target: k8s\n")
	fmt.Fprintf(&b, "  namespace: %s\n  createNamespace: false\n\nsources:\n", namespace)
	for _, s := range f.Sources {
		b.WriteString(s)
	}
	b.WriteString("\ncomponents:\n  - id: people/basic\n    version: 1.0.0\n")
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(b.String()), 0o644))
	return f
}

// down 交给引擎的选择器，标签值必须是**项目名**，不是命名空间。
//
// 这是真集群上会出事的那一条：命名空间是运维建的（createNamespace: false）时，
// down 走的是"按标签逐类删"。引擎从前自己拿 Project（= 命名空间）拼选择器，
// 于是一个资源都匹配不到——八条 delete 全部命中 0 个对象、退出码 0，
// 而 CLI 打印"✅ 已停止全部组件"。本地 minikube 永远走删命名空间那条路，
// 所以这个 bug 一直没被试出来。
func TestDownSelectorUsesProjectNameNotNamespace(t *testing.T) {
	f := k8sProjectWithNamespace(t, "team-a-prod")
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, eng.downs, 1)

	assert.Equal(t, "brickkit.io/project=my-erp", eng.downs[0].Selector,
		"标签值是项目名——生成物上打的就是它")
	assert.Equal(t, "team-a-prod", eng.downs[0].Project,
		"而 Project 是命名空间，用于 -n；两者不是一回事")
}

// up 的孤儿清理与 down 的逐类删必须用**同一个**选择器。
//
// 这条守的是根因而不是症状：那个 bug 之所以存在，是因为选择器被算了两遍
// 而只有一遍算对。现在两边都走 projectSelector，这条用例保证它们不会再分叉——
// 将来谁把其中一处改了，这里会红。
func TestUpAndDownAgreeOnProjectSelector(t *testing.T) {
	f := k8sProjectWithNamespace(t, "team-a-prod")
	eng := newK8sEngine()

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "down").code)

	require.Len(t, eng.downs, 1)
	assert.Equal(t, eng.lastUp(t).PruneSelector, eng.downs[0].Selector,
		"同一个项目，两条命令认的必须是同一批资源")
}

// 命名空间是我们建的那条路上也要带选择器。
//
// 那条路当前用不上它（直接 delete namespace），但字段空着就是一个等着被踩的坑：
// 将来若因为任何原因改走逐类删，引擎会中止而不是把别人的资源删光。
func TestDownAlwaysPassesSelector(t *testing.T) {
	f := k8sProject(t) // 默认命名空间，createNamespace 缺省为 true
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "down").code)
	require.Len(t, eng.downs, 1)
	assert.True(t, eng.downs[0].DeleteNamespace, "前提：这条路是删命名空间")
	assert.Equal(t, "brickkit.io/project=my-erp", eng.downs[0].Selector,
		"即便这条路用不上，也不该留一个空字段")
}

// ============================================================
// down 不依赖安装源
// ============================================================

// 组件的 component.yaml 写坏了，down 照样要能把容器停掉。
//
// down 需要的只有项目名——它交给引擎的就是"停掉 brickkit-<项目名> 名下的一切"
// （005 §5.9.3）。而它从前会先把每个组件的 component.yaml 都读一遍去建依赖图，
// 那套结论一个字段都没用上，却让 component.yaml 里的一处笔误、本地源目录被删、
// 市场连不上……任何一种都能把"停容器"这件事拦下来，而容器还好好跑着。
//
// 一条停不掉项目的 down，比一条不存在的 down 更糟：使用者以为自己有退路。
func TestDownStopsEvenWhenManifestIsBroken(t *testing.T) {
	f, eng := startedProject(t)
	breakLocalManifest(t, f, "erp/backend")

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, eng.downs, 1, "容器必须真的被停掉，而不是先去解析一遍依赖图")
	assert.Equal(t, "brickkit-my-erp", eng.downs[0].Project)
	assert.Contains(t, r.stdout, "已停止")
}

// 本地安装源整个目录没了，down 同样照停。
//
// 这是最常见的一种：`components/` 在 init 生成的 .gitignore 里，
// 同事 clone 下来的仓库里根本没有它。
func TestDownStopsEvenWhenLocalSourceIsGone(t *testing.T) {
	f, eng := startedProject(t)
	matches, err := filepath.Glob(filepath.Join(f.Dir, "src*"))
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	for _, dir := range matches {
		require.NoError(t, os.RemoveAll(dir))
	}

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, eng.downs, 1)
}
