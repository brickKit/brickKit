// 本文件是 Step 12 在命令层的业务行为测试：`brickkit up --dry-run`
// 只生成部署文件、不启动任何东西（004 §3.5）。
//
// Step 12 的交付物是"生成"，`--dry-run` 正好就是这条路径；真正的启动属 Step 15。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// generatedCompose 读出生成的 docker-compose.yaml。
func generatedCompose(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".brickkit", "generated", "docker-compose.yaml"))
	require.NoError(t, err, "应生成 .brickkit/generated/docker-compose.yaml")
	return string(data)
}

// composeProject 建一个带资源绑定的两组件项目。
func composeProject(t *testing.T) *projectFixture {
	t.Helper()

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
    expose: true
    exposePort: 18080

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
	return f
}

// ============================================================
// 生成
// ============================================================

func TestUpDryRunGeneratesComposeFile(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	text := generatedCompose(t, f.Dir)

	assert.Contains(t, text, "由 BrickKit CLI 自动生成")
	assert.Contains(t, text, "people-basic-1-0-0:")
	assert.Contains(t, text, "erp-backend-1-0-0:")
	assert.Contains(t, text, "PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080")
	assert.Contains(t, r.stdout, "📄 已生成")
	assert.Contains(t, r.stdout, ".brickkit/generated/docker-compose.yaml")
}

// --dry-run 不能启动任何东西：它的全部意义就是"先看看会生成什么"。
func TestUpDryRunDoesNotStartAnything(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code)
	assert.NotContains(t, r.stdout, "正在启动")
	assert.Contains(t, r.stdout, "未启动任何组件")
}

// 生成前先展示级联结果与启动顺序：使用者要能看出"这次会跑哪些、为什么"。
func TestUpDryRunShowsStatesAndOrder(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	assert.Contains(t, r.stdout, "📋 组件状态计算：")
	assert.Contains(t, r.stdout, "📋 启动顺序")
	assert.Contains(t, r.stdout, "1. people-basic-1-0-0")
}

// 006 §9.5：CLI 不建库，但必须告诉使用者要建哪些库、怎么建。
func TestUpDryRunReportsRequiredDatabases(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "brickkit_people")
	assert.Contains(t, r.stdout, "CREATE DATABASE")
	assert.Contains(t, r.stdout, "people/basic", "要说清是哪个组件用这个库")
}

// 重复执行覆盖同一个文件，且内容一致（生成是确定性的）。
func TestUpDryRunIsRepeatable(t *testing.T) {
	f := composeProject(t)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "up", "--dry-run").code)
	first := generatedCompose(t, f.Dir)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "up", "--dry-run").code)

	assert.Equal(t, strings.Count(first, "services:"), 1)
	assert.Equal(t, first, generatedCompose(t, f.Dir))
}

// ============================================================
// 错误路径
// ============================================================

// 端口冲突在生成阶段就要报出来（P4）。
func TestUpDryRunReportsExposePortConflict(t *testing.T) {
	comps := []comp{
		{ID: "portal/user-frontend", Version: "1.0.0"},
		{ID: "admin/console", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "portal/user-frontend@1.0.0", "admin/console@1.0.0")
	f.writeConfig(t, `components:
  - id: portal/user-frontend
    version: 1.0.0
    expose: true
    exposePort: 8080
  - id: admin/console
    version: 1.0.0
    expose: true
    exposePort: 8080
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "8080")
	assert.Contains(t, r.stderr, "exposePort")
}

// 一个组件都不启动时不生成文件：一份空的 compose 只会让人困惑。
func TestUpDryRunWithNothingRunning(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    enabled: false
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "本次没有组件会启动")
	assert.NoFileExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "docker-compose.yaml"))
}

// 空项目给出引导，而不是报错。
func TestUpDryRunOnEmptyProject(t *testing.T) {
	f := newProjectFixture(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "当前项目没有组件")
}

// 不带 --dry-run 的 up 仍属 Step 15，尚未实现——
// 这条用例会在 Step 15 落地时失败，提醒回来更新。
func TestUpWithoutDryRunIsStillNotImplemented(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "尚未实现")
	assert.Contains(t, r.stderr, "Step 15")
}
