// 本文件是 Step 15-B 的业务行为测试：`brickkit down`（004 §3.6）。
// 覆盖 15.13、15.14、15.21。
package cli

import (
	"errors"
	"path/filepath"
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
	assert.Empty(t, eng.downs[0].Services, "不带 --only 就是整个项目")
	assert.Equal(t, filepath.Join(f.Dir, ".brickkit", "generated", "docker-compose.yaml"),
		eng.downs[0].File)
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
// 15.14 / 15.21 --only 与停止顺序
// ============================================================

func TestDownOnlyStopsGivenComponents(t *testing.T) {
	f, eng := startedProject(t)

	r := runWithEngine(t, eng, f.Dir, "down", "--only", "erp/backend")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	require.Len(t, eng.downs, 1)
	assert.Equal(t, []string{"erp-backend-1-0-0"}, eng.downs[0].Services, "15.14")
}

// 15.21：停止顺序与启动顺序相反——依赖方先停，被依赖方后停。
//
// 反过来的话，被依赖方先没了，依赖方在关闭过程中还在调它，
// 日志里会留下一串没有意义的连接错误。
func TestDownStopsInReverseStartOrder(t *testing.T) {
	f, eng := startedProject(t)

	r := runWithEngine(t, eng, f.Dir, "down",
		"--only", "people/basic,erp/backend")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"erp-backend-1-0-0", "people-basic-1-0-0"},
		eng.downs[0].Services, "15.21：erp/backend 依赖 people/basic，所以先停 erp")
	assert.Contains(t, r.stdout, "停止顺序")
}

// --only 指定了不存在的组件要报错，而不是悄悄什么都不停。
func TestDownOnlyUnknownComponent(t *testing.T) {
	f, eng := startedProject(t)

	r := runWithEngine(t, eng, f.Dir, "down", "--only", "not/here")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "not/here")
	assert.Empty(t, eng.downs)
}

// --only 支持 @版本：多版本共存时要能只停一个。
func TestDownOnlyWithVersion(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "people/basic@2.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: people/basic
    version: 2.0.0
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down", "--only", "people/basic@2.0.0")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"people-basic-2-0-0"}, eng.downs[0].Services)
}

// --only 不带版本时停掉该组件的所有版本（与 up --only 同一条规则，004 §3.5）。
func TestDownOnlyWithoutVersionStopsAllVersions(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "people/basic@2.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: people/basic
    version: 2.0.0
`)
	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down", "--only", "people/basic")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.ElementsMatch(t, []string{"people-basic-1-0-0", "people-basic-2-0-0"},
		eng.downs[0].Services)
}
