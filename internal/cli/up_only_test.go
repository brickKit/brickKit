// 本文件是 Step 15-C 的业务行为测试：`--only`、`--config` 与升级路径
// （004 §3.5、§3.5.1）。覆盖 15.8–15.12，以及延后项 P5（资源密码硬编码告警）、
// P10（升级拉新版本）、P15（CheckUpgrade 接线）。
//
// 15.7 与 P22 曾经由 `--check-resources` 承担，那个参数已经删掉
// （理由见 TestUpNeverProbesResources）。
package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
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

// --only 点名一个只被弱依赖引用的组件：单独把它跑起来是合法用法。
func TestUpOnlyStartsAWeaklyReferencedComponent(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "erp/backend", Version: "1.0.0", Optional: []string{"infra/bus@1.0.0"}},
		{ID: "infra/bus", Version: "1.0.0"},
	}, "erp/backend@1.0.0")
	eng := newFakeEngine()

	// 不点名时两个都跑（003 §4.3：弱依赖一视同仁）
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)
	assert.Contains(t, eng.lastUp(t).Services, "infra-bus-1-0-0")

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "infra/bus")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"infra-bus-1-0-0"}, eng.lastUp(t).Services,
		"点名它就该启动它")
}

// --only 之下根组件不再自动启动——那正是它要收窄掉的东西。
func TestUpOnlyDoesNotAutoStartRoots(t *testing.T) {
	f := threeTierProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "erp/backend")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, eng.lastUp(t).Services, "portal-user-frontend-1-0-0",
		"portal 是根组件，但这次没点它的名")
}

// --only 点名的组件，它的**强依赖**被关掉时报错。
//
// 这是"点名等于钉住"自动带来的：两个意图直接冲突（既要它跑、又关掉了它跑起来
// 必需的东西）。静默跳过只会让人对着一个空空的 docker ps 发懵。
func TestUpOnlyErrorsWhenARequirementIsDisabled(t *testing.T) {
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
`)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "erp/backend")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "people/basic", "要点出是谁被关掉了")
	assert.Contains(t, r.stderr, "--only", "理由是这一次点了名，不是 enabled: true")
	assert.Empty(t, eng.ups)
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
// 启动前不做资源体检
// ============================================================

// `up` 从不拨号探测基础资源。
//
// 曾经有个 `--check-resources` 干这件事，两半都删掉了：
//
//	资源可达性  006 §8.3 自己就论证过它证明不了什么——组件用的是**自己那套
//	            凭据**、从**容器网络里**连，CLI 从宿主机用另一套凭据连成功
//	            说明不了组件也能连成功。而 host: localhost 时它给的是**反的**
//	            答案：宿主机上通，容器里连的却是它自己。
//	宿主机端口  docker 会为它发布的每个端口报出清楚的 `port is already
//	            allocated`；它唯一多覆盖的是 local 组件自己监听的端口，
//	            而那一类的典型命中恰恰是**开发者自己刚启动的进程**——
//	            一个典型命中就是假警报的检查，只会训练人忽略警告。
func TestUpNeverProbesResources(t *testing.T) {
	f := externalResourceProject(t)
	eng := newFakeEngine()

	probed := false
	r := runWith(t, func(o *Options) {
		o.Engine = eng
		o.Probe = func(context.Context, string) error { probed = true; return nil }
	}, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.False(t, probed, "up 不该拨号——那个结论不如组件自己的失败准确")
	assert.NotEmpty(t, eng.ups, "照常启动")
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

// ============================================================
// 悬空资源绑定：警告，不阻断
// ============================================================

// 绑定指向一个 components 里没有的组件 → 警告，但项目照常起来。
//
// 它曾经是校验硬错误，代价完全不成比例：那条绑定的唯一后果是自己不生效
// （没有组件会读它），而阻断的后果是整个项目跑不了。
func TestUpWarnsAboutDanglingBinding(t *testing.T) {
	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: postgres-main
    host: host.docker.internal
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
      - componentId: ghost/none
        database: ghost
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "警告不阻断：%s%s", r.stdout, r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "ghost/none")
	assert.Contains(t, out, "postgres-main")
	assert.Contains(t, out, "不会生效")
}

// 绑定都指向已声明的组件时，不该有这条警告。
func TestUpDoesNotWarnWhenAllBindingsDeclared(t *testing.T) {
	f := externalResourceProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "components 里不存在的组件")
}

// 用了 ${ENV_VAR} 就不该有这条警告。
func TestUpDoesNotWarnAboutEnvVarPassword(t *testing.T) {
	f := externalResourceProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "明文密码")
}

// 变量**真的配了**的时候更不该报警——这正是使用者做对了的情形。
//
// 解析器在读配置时就把 ${VAR} 展开掉了（003 §5.4），拿展开后的值去判断
// "是不是明文密码"，结论会完全反过来：配对了才骂人，漏配反而不吭声。
func TestUpDoesNotWarnWhenEnvVarIsSet(t *testing.T) {
	t.Setenv("POSTGRES_PASSWORD", "a-real-password")
	f := externalResourceProject(t)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "明文密码")
	assert.NotContains(t, r.stdout+r.stderr, "a-real-password", "密码本身不该出现在输出里")
}

// --dry-run 不需要引擎，也不拨号。
//
// 这条从前是 "--check-resources 与 --dry-run 一起用时照样体检"；
// 那个参数删掉之后，要守的就只剩"这台机器上没装 docker 也该能跑"。
func TestDryRunNeedsNoEngineAndNeverProbes(t *testing.T) {
	f := externalResourceProject(t)

	probed := false
	r := runWith(t, func(o *Options) {
		o.Probe = func(context.Context, string) error { probed = true; return nil }
	}, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.False(t, probed)
	assert.Contains(t, r.stdout, "只生成文件")
}
