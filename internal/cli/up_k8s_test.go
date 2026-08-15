package cli

// 本文件是 Step 16-C「K8s 目标的 up / down / status 接线」的业务行为测试。
//
// 引擎是假的：命令层的职责是"生成什么、交给引擎什么、按什么顺序"，
// 而不是"怎么调 kubectl"（那一段由 internal/engine 的用例盯住）。
// 真集群验证是 P25。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
)

// newK8sEngine 返回一个假的 kubectl 引擎。
func newK8sEngine() *fakeEngine {
	eng := newFakeEngine()
	eng.name = engine.K8s
	return eng
}

func k8sDir(f *projectFixture) string {
	return filepath.Join(f.Dir, ".brickkit", "generated", "k8s")
}

// k8sProject 造一个 deploy.target: k8s 的项目。
func k8sProject(t *testing.T) *projectFixture {
	t.Helper()
	return k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "", "")
}

// k8sProjectWith 造一个 deploy.target: k8s 的项目。
//
// 不能用 f.writeConfig：它的头部已经写死了 deploy.target: docker。
// entryLines 是追加到组件条目里的行（如 local: true），extra 是追加到文件末尾的段（如 resources）。
func k8sProjectWith(t *testing.T, spec comp, entryLines, extra string) *projectFixture {
	t.Helper()

	f := addedProject(t, []comp{spec}, spec.ref())

	var b strings.Builder
	b.WriteString("project: my-erp\n\ndeploy:\n  target: k8s\n\nsources:\n")
	for _, s := range f.Sources {
		b.WriteString(s)
	}
	fmt.Fprintf(&b, "\ncomponents:\n  - id: %s\n    version: %s\n", spec.ID, spec.Version)
	b.WriteString(entryLines)
	b.WriteString(extra)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(b.String()), 0o644))
	return f
}

// pgResourceYAML 是一段绑定到 people/basic 的外部 PostgreSQL 声明。
func pgResourceYAML(password string) string {
	return `
resources:
  - kind: database
    engine: postgresql
    id: people-db
    host: postgres.infra.svc
    port: 5432
    username: people_user
    password: ` + password + `
    bindings:
      - componentId: people/basic
        database: people
`
}

// ============================================================
// up
// ============================================================

func TestUpK8sGeneratesManifests(t *testing.T) {
	f := k8sProject(t)

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.FileExists(t, filepath.Join(k8sDir(f), "namespace.yaml"))
	assert.FileExists(t, filepath.Join(k8sDir(f), "deployments", "people-basic-1-0-0.yaml"))
	assert.FileExists(t, filepath.Join(k8sDir(f), "services", "people-basic-1-0-0.yaml"))
	assert.NoFileExists(t,
		filepath.Join(f.Dir, ".brickkit", "generated", "docker-compose.yaml"),
		"K8s 项目不该留下一份毫无意义的 compose 文件")
}

// 交给引擎的是**清单目录**与**命名空间**，不是 compose 那套。
func TestUpK8sHandsDirectoryAndNamespaceToEngine(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	runWithEngine(t, eng, f.Dir, "up")

	up := eng.lastUp(t)

	assert.Equal(t, k8sDir(f), up.File, "K8s 目标交给引擎的是目录")
	assert.Equal(t, "brickkit-my-erp", up.Project, "命名空间是 brickkit-<项目名>")
	assert.Equal(t, []string{"people-basic-1-0-0"}, up.Services)
}

// 迁移 Job 名要传给引擎：清理旧 Job 与等待完成都靠它（16.14）。
func TestUpK8sPassesMigrationJobs(t *testing.T) {
	f := k8sProjectWith(t, comp{
		ID: "people/basic", Version: "1.0.0",
		Migration: []string{"/app/server", "migrate", "up"},
	}, "", "")
	eng := newK8sEngine()

	runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, []string{"people-basic-1-0-0-migration"}, eng.lastUp(t).MigrationJobs)
}

func TestUpK8sWithoutMigrationsPassesNoJobs(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	runWithEngine(t, eng, f.Dir, "up")

	assert.Empty(t, eng.lastUp(t).MigrationJobs)
}

// --dry-run 只生成清单，不碰集群。
func TestUpK8sDryRunDoesNotApply(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.FileExists(t, filepath.Join(k8sDir(f), "namespace.yaml"), "文件照常生成")
	assert.Empty(t, eng.ups, "但一条 kubectl 都不该执行")
}

// 输出里要告诉使用者清单在哪、命名空间叫什么。
func TestUpK8sOutputMentionsNamespaceAndPath(t *testing.T) {
	f := k8sProject(t)

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up")

	assert.Contains(t, r.stdout, filepath.Join(".brickkit", "generated", "k8s"))
	assert.Contains(t, r.stdout, "brickkit-my-erp")
}

// 建库提示两种目标都要有：K8s 下资源由运维部署，但库照样得先建出来。
func TestUpK8sReportsDatabaseRequirements(t *testing.T) {
	t.Setenv("PEOPLE_DB_PASSWORD", "s3cr3t")
	f := k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "",
		pgResourceYAML("${PEOPLE_DB_PASSWORD}"))

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, `CREATE DATABASE "people"`)
}

// ${VAR} 没定义时必须阻断，且不能留下半份清单。
func TestUpK8sBlocksOnUnresolvedEnvVar(t *testing.T) {
	f := k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "",
		pgResourceYAML("${PEOPLE_DB_PASSWORD_NOT_SET}"))
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "PEOPLE_DB_PASSWORD_NOT_SET")
	assert.Empty(t, eng.ups)
}

// local: true 在 K8s 下要报错，而且要说清是哪个组件。
func TestUpK8sRejectsLocalComponent(t *testing.T) {
	f := k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "    local: true\n", "")
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "local")
	assert.Contains(t, r.stderr, "people/basic")
	assert.Empty(t, eng.ups)
}

// ============================================================
// down / status
// ============================================================

func TestDownK8s(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	require.Len(t, eng.downs, 1)
	assert.Equal(t, k8sDir(f), eng.downs[0].File)
	assert.Equal(t, "brickkit-my-erp", eng.downs[0].Project)
}

// 还没 up 过时 down 不该报错，也不该去碰集群。
func TestDownK8sBeforeUp(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "down")

	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Empty(t, eng.downs)
	assert.Contains(t, r.stdout, "尚未启动过")
}

func TestStatusK8s(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "运行中")
}

// 排障命令必须是 kubectl 的，不能给一条 docker compose。
func TestK8sLogsCommandIsKubectl(t *testing.T) {
	command := logsCommand(engine.K8s, "brickkit-my-erp", ".brickkit/generated/k8s", "people-basic-1-0-0")

	assert.Contains(t, command, "kubectl logs")
	assert.Contains(t, command, "deployment/people-basic-1-0-0")
	assert.Contains(t, command, "-n brickkit-my-erp")
	assert.NotContains(t, command, "docker")
}
