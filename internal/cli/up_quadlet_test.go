package cli

// 本文件是 podman.units: quadlet 的 up / down / status 接线。引擎是假的：命令层的职责是“生成什么、写到哪、
// 交给引擎什么”，systemctl 与 podman 怎么调由 internal/engine 的用例盯住，真 systemd 上的行为另行真跑验证。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// quadletFixture 造一个 target: podman、podman.units: quadlet 的项目：api 有迁移，web 依赖 api。
func quadletFixture(t *testing.T) *projectFixture {
	t.Helper()
	api := comp{ID: "erp/api", Version: "1.0.0", Migration: []string{"/app/api", "migrate"}}
	web := comp{ID: "erp/web", Version: "1.0.0", Requires: []string{"erp/api@1.0.0"}}
	f := addedProject(t, []comp{api, web}, web.ref(), api.ref())
	editFile(t, in(f.Dir, "deploy.yaml"), "target: docker", "target: podman\npodman:\n  units: quadlet")
	return f
}

func newQuadletEngine() *fakeEngine {
	eng := newFakeEngine()
	eng.name = engine.Podman
	return eng
}

func quadletDir(f *projectFixture) string {
	return filepath.Join(f.Dir, ".brickkit", "generated", "quadlet")
}

// --dry-run 只写生成目录里的单元，不碰引擎；没有 compose.yaml。
func TestUpQuadletDryRunWritesTheUnitsOnly(t *testing.T) {
	f := quadletFixture(t)
	eng := newQuadletEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Empty(t, eng.ups)
	for _, name := range []string{
		"brickkit-my-erp.network",
		"brickkit-my-erp_erp-api-1-0-0.container",
		"brickkit-my-erp_erp-api-1-0-0-migration.container",
		"brickkit-my-erp_erp-web-1-0-0.container",
	} {
		assert.FileExists(t, filepath.Join(quadletDir(f), name))
	}
	assert.Contains(t, readFile(t, filepath.Join(quadletDir(f), "brickkit-my-erp_erp-web-1-0-0.container")),
		"Wants=brickkit-my-erp_erp-api-1-0-0.service\n")
	assert.NoFileExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "compose.yaml"))
	assert.Contains(t, r.stdout, i18n.T(msgid.CliUpQuadletDryRun))
}

// up 把单元、迁移与组件的单元名交给引擎（引擎侧项目名与 compose 相同），再按状态汇报；
// 卸下的东西（不再生成的单元、compose 留下的容器）如实列出来。
func TestUpQuadletHandsTheUnitsToTheEngine(t *testing.T) {
	f := quadletFixture(t)
	eng := newQuadletEngine()
	eng.pruned = []string{"brickkit-my-erp_erp-old-1-0-0.service"}

	r := runWithEngine(t, eng, f.Dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	up := eng.lastUp(t)
	assert.Equal(t, "brickkit-my-erp", up.Project)
	require.NotNil(t, up.Quadlet)
	assert.Equal(t, []string{"brickkit-my-erp_erp-api-1-0-0-migration.service"}, up.Quadlet.Migrations)
	assert.Equal(t, []string{"brickkit-my-erp_erp-api-1-0-0.service", "brickkit-my-erp_erp-web-1-0-0.service"}, up.Quadlet.Components)
	assert.Len(t, up.Quadlet.Files, 4)
	assert.Contains(t, r.stdout, "brickkit-my-erp_erp-old-1-0-0.service")
	assert.Contains(t, r.stdout, i18n.T(msgid.CliUpAllComponentsStarted, 2))
}

// 迁移失败：引擎只认得单元名，命令层补上是哪个组件的。
func TestUpQuadletMigrationFailureNamesTheComponent(t *testing.T) {
	f := quadletFixture(t)
	eng := newQuadletEngine()
	eng.upErr = clierr.New(clierr.CodeMigrationFailed, i18n.T(msgid.EngineMigrationFailed)).
		WithDetail(i18n.T(msgid.EngineLabelUnit), "brickkit-my-erp_erp-api-1-0-0-migration.service")

	r := runWithEngine(t, eng, f.Dir, "up")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.EngineMigrationFailed))
	assert.Contains(t, r.stdout+r.stderr, "erp/api@1.0.0")
}

// 换了部署形式，生成目录只留这一种：compose → Quadlet 收走 compose.yaml，Quadlet → compose 收走单元目录。
func TestSwitchingBetweenComposeAndQuadletPrunesTheGeneratedFiles(t *testing.T) {
	f := quadletFixture(t)
	editFile(t, in(f.Dir, "deploy.yaml"), "podman:\n  units: quadlet", "podman:\n  units: compose")
	r := runWithEngine(t, newQuadletEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.FileExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "compose.yaml"))

	editFile(t, in(f.Dir, "deploy.yaml"), "podman:\n  units: compose", "podman:\n  units: quadlet")
	r = runWithEngine(t, newQuadletEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoFileExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "compose.yaml"))
	assert.DirExists(t, quadletDir(f))

	editFile(t, in(f.Dir, "deploy.yaml"), "podman:\n  units: quadlet", "podman:\n  units: compose")
	r = runWithEngine(t, newQuadletEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	_, err := os.Stat(quadletDir(f))
	assert.True(t, os.IsNotExist(err))
}

// down 与 status 走同一个引擎、同一个引擎侧项目名；status 的日志提示指向 journald。
func TestQuadletDownAndStatus(t *testing.T) {
	f := quadletFixture(t)
	eng := newQuadletEngine()
	eng.statuses = []engine.Status{{Service: "erp-api-1-0-0", State: "exited", ExitCode: 1, Replicas: 1}}

	r := runWithEngine(t, eng, f.Dir, "down")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, eng.downs, 1)
	assert.Equal(t, "brickkit-my-erp", eng.downs[0].Project)

	r = runWithEngine(t, eng, f.Dir, "status")
	assert.Contains(t, r.stdout+r.stderr, "journalctl --user -u 'brickkit-my-erp_")
}

func TestLogsCommandForQuadlet(t *testing.T) {
	assert.Equal(t, "journalctl --user -u 'brickkit-shop_*'", logsCommand(logsQuadlet, "brickkit-shop", ""))
	assert.Equal(t, "journalctl --user -u 'brickkit-shop_erp-api-1-0-0*'", logsCommand(logsQuadlet, "brickkit-shop", "erp-api-1-0-0"))
	assert.True(t, strings.HasPrefix(logsCommand(engine.Podman, "brickkit-shop", ""), "podman compose"))
}

// status 说出上一次没跑成的迁移：Quadlet 下迁移失败时组件照跑上一个版本，表格全绿，不说就没人知道。
func TestStatusNamesAFailedMigration(t *testing.T) {
	f := quadletFixture(t)
	eng := newQuadletEngine()
	eng.statuses = []engine.Status{
		{Service: "erp-api-1-0-0", State: "running", Health: "healthy", Replicas: 1, Ready: 1},
		{Service: "erp-web-1-0-0", State: "running", Health: "healthy", Replicas: 1, Ready: 1},
		{Service: "erp-api-1-0-0-migration", State: "failed", Replicas: 1},
	}
	r := runWithEngine(t, eng, f.Dir, "status")
	assert.Contains(t, r.stdout, i18n.T(msgid.CliStatusMigrationFailed, "erp/api@1.0.0",
		"journalctl --user -u 'brickkit-my-erp_erp-api-1-0-0-migration*'"))
}

// elsewhereEngine 是一个“另一边的 systemd 里还装着这个项目”的引擎替身。
type elsewhereEngine struct {
	*fakeEngine
}

func (e elsewhereEngine) ElsewhereWarning(project, _ string) *clierr.Error {
	return clierr.New(clierr.CodeDeployedElsewhere, "Warning: deployed elsewhere: "+project).AsWarning()
}

// down 与 status 只看得到当前身份那一份：另一边还装着时照样做完自己的，再警告一句（FRB-012）。
// up 把项目目录交给引擎：root 据此找部署过它的用户。
func TestDownAndStatusWarnWhenDeployedElsewhere(t *testing.T) {
	f := quadletFixture(t)
	eng := elsewhereEngine{newQuadletEngine()}
	for _, command := range []string{"status", "down"} {
		r := runWith(t, func(o *Options) { o.Engine = eng }, f.Dir, command)
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
		assert.Contains(t, r.stdout, "⚠️ Warning: deployed elsewhere: brickkit-my-erp", command)
	}
	assert.Len(t, eng.downs, 1, "down 照样做自己这一边")

	plain := newQuadletEngine()
	r := runWithEngine(t, plain, f.Dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, plain.ups, 1)
	root, err := filepath.EvalSymlinks(f.Dir)
	require.NoError(t, err)
	got, err := filepath.EvalSymlinks(plain.ups[0].ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, root, got)
}

