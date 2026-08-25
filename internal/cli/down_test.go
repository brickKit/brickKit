// 本文件是 Step 15-B 的业务行为测试：`brickkit down`（004 §3.6）。
// 覆盖 15.13、15.14、15.21。
package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// startedProject 是一个已经 up 过、留下部署文件的项目。
func startedProject(t *testing.T) (*projectFixture, *fakeEngine) {
	t.Helper()

	f := composeProject(t)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)
	eng.ups = nil // 之后的断言只看 down
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

// 从没 up 过就 down：给出引导而不是把引擎的报错甩出来。
func TestDownWithoutDeployFile(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "down")

	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Empty(t, eng.downs, "文件都没有，没什么可停的")
	assert.Contains(t, r.stdout, "尚未启动")
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
