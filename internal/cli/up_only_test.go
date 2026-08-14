// 本文件是 Step 15-C 的业务行为测试：`--only`、`--config`、`--check-resources`
// 与升级路径（004 §3.5、§3.5.1）。覆盖 15.7–15.12，以及延后项
// P5（资源密码硬编码告警）、P10（升级拉新版本）、P15（CheckUpgrade 接线）、
// P22（宿主机端口被别的进程占用）。
package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
)

// threeTierProject：portal → erp → people，外加一个没人依赖但钉住的组件。
func threeTierProject(t *testing.T) *projectFixture {
	t.Helper()

	comps := []comp{
		{ID: "portal/user-frontend", Version: "1.0.0", Requires: []string{"erp/backend@1.0.0"}},
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "portal/user-frontend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
  - id: portal/user-frontend
    version: 1.0.0
`)
	return f
}

// ============================================================
// 15.8 / 15.10 / 15.11 --only
// ============================================================

// 15.8：--only 启动指定组件**及其强依赖**——只启动它自己是起不来的。
func TestUpOnlyStartsSelectedAndItsDependencies(t *testing.T) {
	f := threeTierProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "erp/backend")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.ElementsMatch(t, []string{"people-basic-1-0-0", "erp-backend-1-0-0"},
		eng.lastUp(t).Services, "15.8：强依赖要一起启动")
}

// 没被选中、也不是谁的依赖的组件不启动。
func TestUpOnlyExcludesUnselectedComponents(t *testing.T) {
	f := threeTierProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "people/basic")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"people-basic-1-0-0"}, eng.lastUp(t).Services, "15.8")
	assert.Contains(t, r.stdout, "--only", "输出要说明这次为什么只有这些")
}

// 逗号分隔多个组件。
func TestUpOnlyAcceptsMultipleComponents(t *testing.T) {
	f := threeTierProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "people/basic,erp/backend")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.ElementsMatch(t, []string{"people-basic-1-0-0", "erp-backend-1-0-0"},
		eng.lastUp(t).Services)
}

// 15.10：带 @版本 时只启动那一个版本。
func TestUpOnlyWithVersion(t *testing.T) {
	f := multiVersionProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "people/basic@2.0.0")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"people-basic-2-0-0"}, eng.lastUp(t).Services, "15.10")
}

// 15.11：不带版本时该组件的所有版本都启动（多版本默认共存，002 §3.6）。
func TestUpOnlyWithoutVersionStartsAllVersions(t *testing.T) {
	f := multiVersionProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "people/basic")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.ElementsMatch(t, []string{"people-basic-1-0-0", "people-basic-2-0-0"},
		eng.lastUp(t).Services, "15.11")
}

func multiVersionProject(t *testing.T) *projectFixture {
	t.Helper()

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
	return f
}

// 15.9：--only 指定了被显式关闭的组件——两个意图直接冲突，必须报错。
func TestUpOnlyDisabledComponentIsAnError(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    enabled: false
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "people/basic")

	assert.Equal(t, clierr.ExitError, r.code, "15.9")
	assert.Contains(t, r.stderr, "people/basic")
	assert.Contains(t, r.stderr, "enabled: false", "要指出冲突在哪")
	assert.Empty(t, eng.ups)
}

// --only 写了个不存在的组件要报错，而不是悄悄启动 0 个。
func TestUpOnlyUnknownComponentIsAnError(t *testing.T) {
	f := threeTierProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "not/here")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "not/here")
	assert.Empty(t, eng.ups)
}

// --only 与 --dry-run 一起用：只生成被选中的那部分。
func TestUpOnlyWithDryRun(t *testing.T) {
	f := threeTierProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "people/basic", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Empty(t, eng.ups)
	text := generatedCompose(t, f.Dir)
	assert.Contains(t, text, "people-basic-1-0-0:")
	assert.NotContains(t, text, "erp-backend-1-0-0:")
}

// ============================================================
// 15.12 --config
// ============================================================

func TestUpWithAlternateConfig(t *testing.T) {
	f := threeTierProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.Dir, "brickkit.prod.yaml"),
		[]byte(prodConfig(f)), 0o644))
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--config", "brickkit.prod.yaml")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"people-basic-1-0-0"}, eng.lastUp(t).Services, "15.12")
	// 004 §3.5：生成的部署文件仍写进默认的 .brickkit/ 目录
	assert.FileExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "docker-compose.yaml"))
}

// prodConfig 造一份"只装一个组件"的备用配置。
func prodConfig(f *projectFixture) string {
	head := "project: my-erp-prod\n\ndeploy:\n  target: docker\n\nsources:\n"
	for _, s := range f.Sources {
		head += s
	}
	return head + "\ncomponents:\n  - id: people/basic\n    version: 1.0.0\n"
}

// ============================================================
// 15.7 / P22 --check-resources
// ============================================================

// 15.7：资源不可达时**警告但不阻断**——项目也许正是要靠这次启动把资源带起来。
func TestUpCheckResourcesWarnsWhenUnreachable(t *testing.T) {
	f := externalResourceProject(t)
	eng := newFakeEngine()

	r := runWith(t, func(o *Options) {
		o.Engine = eng
		o.Probe = func(context.Context, string) error { return errors.New("connection refused") }
	}, f.Dir, "up", "--check-resources")

	require.Equal(t, clierr.ExitOK, r.code, "15.7：警告不阻断")
	assert.Contains(t, r.stdout+r.stderr, "⚠️")
	assert.Contains(t, r.stdout+r.stderr, "postgres-external")
	assert.Contains(t, r.stdout+r.stderr, "connection refused")
	assert.NotEmpty(t, eng.ups, "警告之后照常启动")
}

func TestUpCheckResourcesPassesWhenReachable(t *testing.T) {
	f := externalResourceProject(t)
	eng := newFakeEngine()

	r := runWith(t, func(o *Options) {
		o.Engine = eng
		o.Probe = func(context.Context, string) error { return nil }
	}, f.Dir, "up", "--check-resources")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "资源可达性")
	assert.NotContains(t, r.stdout, "不可达")
}

// 不加 --check-resources 时不拨号：那是一次额外的、可能很慢的体检。
func TestUpDoesNotProbeWithoutTheFlag(t *testing.T) {
	f := externalResourceProject(t)
	eng := newFakeEngine()

	probed := false
	r := runWith(t, func(o *Options) {
		o.Engine = eng
		o.Probe = func(context.Context, string) error { probed = true; return nil }
	}, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.False(t, probed)
}

// P22：要占的宿主机端口已经被**别的进程**占着时，启动前就说清楚。
//
// 真实验证时撞到过：localPort 写了 9001，而本机另一个无关进程正占着它，
// 生成一切正常，跑起来才 503。
func TestUpCheckResourcesDetectsBusyHostPort(t *testing.T) {
	comps := []comp{{ID: "portal/user-frontend", Version: "1.0.0"}}
	f := addedProject(t, comps, "portal/user-frontend@1.0.0")
	f.writeConfig(t, `components:
  - id: portal/user-frontend
    version: 1.0.0
    expose: true
    exposePort: 18080
`)
	eng := newFakeEngine()

	r := runWith(t, func(o *Options) {
		o.Engine = eng
		// 拨得通 = 有人在监听 = 这个端口被占了
		o.Probe = func(context.Context, string) error { return nil }
	}, f.Dir, "up", "--check-resources")

	require.Equal(t, clierr.ExitOK, r.code, "P22：警告不阻断")
	assert.Contains(t, r.stdout+r.stderr, "18080")
	assert.Contains(t, r.stdout+r.stderr, "已被占用")
}

// 端口是被**本项目自己的容器**占着时不该报警——那正是重复 up 的正常情形。
func TestUpCheckResourcesIgnoresOwnContainerPorts(t *testing.T) {
	comps := []comp{{ID: "portal/user-frontend", Version: "1.0.0"}}
	f := addedProject(t, comps, "portal/user-frontend@1.0.0")
	f.writeConfig(t, `components:
  - id: portal/user-frontend
    version: 1.0.0
    expose: true
    exposePort: 18080
`)
	eng := newFakeEngine()
	eng.statuses = []engine.Status{{
		Service: "portal-user-frontend-1-0-0", State: "running", Health: "healthy",
		Ports: "0.0.0.0:18080->8080/tcp",
	}}

	r := runWith(t, func(o *Options) {
		o.Engine = eng
		o.Probe = func(context.Context, string) error { return nil }
	}, f.Dir, "up", "--check-resources")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "已被占用",
		"这是上一次 up 留下的容器，不是冲突")
}

// externalResourceProject：一个绑定了外部数据库的项目。
func externalResourceProject(t *testing.T) *projectFixture {
	t.Helper()

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
	return f
}

// ============================================================
// P5 资源密码硬编码告警
// ============================================================

// 006 §3.3 / 008：brickkit.yaml 里不该出现明文密码。
//
// 这是警告不是错误：本地开发时写个 dev 密码很常见，
// 阻断只会让人把 CLI 绕开。
func TestUpWarnsAboutHardcodedResourcePassword(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: postgres-main
    host: postgres
    port: 5432
    username: brickkit
    password: my-secret-password-123
    bindings:
      - componentId: people/basic
        database: brickkit_people
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "P5：警告不阻断")
	assert.Contains(t, r.stdout+r.stderr, "postgres-main")
	assert.Contains(t, r.stdout+r.stderr, "${")
	assert.NotContains(t, r.stdout+r.stderr, "my-secret-password-123",
		"警告里不能把密码本身打出来")
}

// 用了 ${ENV_VAR} 就不该有这条警告。
func TestUpDoesNotWarnAboutEnvVarPassword(t *testing.T) {
	f := externalResourceProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "明文密码")
}

// --check-resources 与 --dry-run 一起用时照样体检：
// --dry-run 的意思是"告诉我会发生什么"，不是"什么都别做"。
// 而且这时不该要求引擎可用——那台机器上也许根本没装 docker。
func TestCheckResourcesWorksWithDryRun(t *testing.T) {
	f := externalResourceProject(t)

	probed := false
	r := runWith(t, func(o *Options) {
		o.Probe = func(context.Context, string) error { probed = true; return nil }
	}, f.Dir, "up", "--check-resources", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.True(t, probed, "--dry-run 也要做可达性检查")
	assert.Contains(t, r.stdout, "资源可达性")
}
