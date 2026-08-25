package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// minimalConfig 是 brickkit init 生成的最小合法配置（004 §3.2 骨架）。
const minimalConfig = `
project: my-project
deploy:
  target: docker
components: []
resources: []
`

// baseConfig 只有必填字段，供各用例追加 components / resources / sources 片段。
const baseConfig = `
project: my-project
deploy:
  target: docker
`

// ============================================================
// 5.1 完整 ERP 项目配置解析
// ============================================================

func TestParseConfigFileFullProject(t *testing.T) {
	t.Setenv("MARKET_TOKEN", "tok-123")
	t.Setenv("POSTGRES_PASSWORD", "pg-secret")
	t.Setenv("REDIS_PASSWORD", "redis-secret")

	c, err := ParseConfigFile(filepath.Join("testdata", "erp-project.yaml"))
	require.NoError(t, err)

	assert.Equal(t, "my-erp-project", c.Project)
	assert.Equal(t, TargetDocker, c.Deploy.Target)

	// ---- 5.15 sources：market + local 两种类型 ----
	require.Len(t, c.Sources, 2)
	assert.Equal(t, "brickkit-market", c.Sources[0].ID)
	assert.Equal(t, SourceTypeMarket, c.Sources[0].Type)
	assert.Equal(t, "https://market.brickkit.io/api/v1", c.Sources[0].URL)
	assert.Equal(t, "tok-123", c.Sources[0].AuthToken, "5.2 ${MARKET_TOKEN} 应被解析")
	assert.True(t, c.Sources[0].IsEnabled())
	assert.Equal(t, SourceTypeLocal, c.Sources[1].Type)
	assert.Equal(t, "./components", c.Sources[1].Path)

	// ---- 组件列表 ----
	require.Len(t, c.Components, 6)

	dept := c.Components[0]
	assert.Equal(t, "department/tree", dept.ID)
	assert.Equal(t, "1.0.0", dept.Version)
	assert.Nil(t, dept.Enabled, "5.5 不写 enabled → nil（跟着上层走）")

	people := c.Components[1]
	assert.True(t, people.Local, "5.22 local 正确解析")
	assert.Equal(t, 8081, people.LocalPort, "5.22 localPort 正确解析")
	require.NotNil(t, people.Resources, "5.7 components[].resources 正确解析")
	require.NotNil(t, people.Resources.Limits)
	assert.Equal(t, "1Gi", people.Resources.Limits.Memory)
	assert.Nil(t, people.Resources.Requests, "未覆盖的部分保持为空，由 Step 11 合并 Manifest 推荐值")

	rbac := c.Components[2]
	require.NotNil(t, rbac.Enabled)
	assert.True(t, *rbac.Enabled, "5.4 enabled: true → 一定跑")

	erp := c.Components[3]
	assert.Equal(t, map[string]any{"sessionTtlSeconds": 7200}, erp.Config, "5.20 config 正确解析")

	bus := c.Components[4]
	assert.True(t, bus.IsDisabled(), "5.6 enabled: false → 一定不跑")

	portal := c.Components[5]
	assert.True(t, portal.Expose, "5.21 expose 正确解析")
	assert.Equal(t, "portal.example.com", portal.Hostname)
	assert.Equal(t, 8080, portal.ExposePort)

	// ---- 5.16 resources + bindings ----
	require.Len(t, c.Resources, 2)
	pg := c.Resources[0]
	assert.Equal(t, "database", pg.Kind)
	assert.Equal(t, "postgresql", pg.Engine)
	assert.Equal(t, "postgres-main", pg.ID)
	assert.Equal(t, "localhost", pg.Host)
	assert.Equal(t, 5432, pg.Port)
	assert.Equal(t, "dev", pg.Username)
	assert.Equal(t, "pg-secret", pg.Password, "5.2 ${POSTGRES_PASSWORD} 应被解析")
	require.Len(t, pg.Bindings, 3)
	assert.Equal(t, "department/tree", pg.Bindings[0].ComponentID)
	assert.Equal(t, "department", pg.Bindings[0].Database)

	redis := c.Resources[1]
	assert.Equal(t, "cache", redis.Kind)
	assert.Equal(t, "redis-secret", redis.Password)
	require.Len(t, redis.Bindings, 2)
	assert.Empty(t, redis.Bindings[0].Database, "cache 资源不需要 database 名")

	// ---- 5.17 installer.requireSignature ----
	require.NotNil(t, c.Installer)
	require.NotNil(t, c.Installer.RequireSignature)
	assert.False(t, *c.Installer.RequireSignature)
	assert.False(t, c.RequireSignature())
}

// ============================================================
// 5.2 / 5.3 ${ENV_VAR} 解析
// ============================================================

func TestEnvVarResolved(t *testing.T) {
	t.Setenv("PG_PWD", "secret-value")

	c, err := ParseConfig([]byte(baseConfig+`
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: localhost
    port: 5432
    password: ${PG_PWD}
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "secret-value", c.Resources[0].Password)
}

// 5.3 环境变量不存在时保留原样（便于用户发现漏配，而不是静默注入空值）。
func TestEnvVarMissingKeptVerbatim(t *testing.T) {
	c, err := ParseConfig([]byte(baseConfig+`
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: localhost
    port: 5432
    password: ${DEFINITELY_NOT_SET_12345}
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "${DEFINITELY_NOT_SET_12345}", c.Resources[0].Password)
}

func TestEnvVarEdgeCases(t *testing.T) {
	t.Setenv("A", "1")
	t.Setenv("B", "2")
	t.Setenv("EMPTY", "")

	c, err := ParseConfig([]byte(`
project: my-project
deploy:
  target: docker
components:
  - id: demo/app
    version: 1.0.0
    config:
      both: "${A}-${B}"
      onlyOne: "prefix-${A}-suffix"
      missing: "x-${NOT_SET_XYZ}-y"
      emptyVar: "[${EMPTY}]"
      notAVar: "$A ${} ${1BAD}"
resources: []
`), "brickkit.yaml")
	require.NoError(t, err)

	cfg := c.Components[0].Config
	assert.Equal(t, "1-2", cfg["both"])
	assert.Equal(t, "prefix-1-suffix", cfg["onlyOne"])
	assert.Equal(t, "x-${NOT_SET_XYZ}-y", cfg["missing"])
	assert.Equal(t, "[]", cfg["emptyVar"], "环境变量存在但为空 → 注入空字符串")
	assert.Equal(t, "$A ${} ${1BAD}", cfg["notAVar"], "不合法的变量写法原样保留")
}

// 环境变量只在值上展开，不动 key。
func TestEnvVarNotExpandedInKeys(t *testing.T) {
	t.Setenv("KEYNAME", "hacked")

	c, err := ParseConfig([]byte(`
project: my-project
deploy:
  target: docker
components:
  - id: demo/app
    version: 1.0.0
    config:
      ${KEYNAME}: value
resources: []
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Contains(t, c.Components[0].Config, "${KEYNAME}")
	assert.NotContains(t, c.Components[0].Config, "hacked")
}

// ============================================================
// 5.4 / 5.5 / 5.6 enabled 三种写法
// ============================================================

func TestEnabledThreeStates(t *testing.T) {
	c, err := ParseConfig([]byte(`
project: my-project
deploy:
  target: docker
components:
  - id: a/pinned
    version: 1.0.0
    enabled: true
  - id: b/default
    version: 1.0.0
  - id: c/disabled
    version: 1.0.0
    enabled: false
resources: []
`), "brickkit.yaml")
	require.NoError(t, err)

	pinned, dflt, disabled := c.Components[0], c.Components[1], c.Components[2]

	// 三种写法直接由 *bool 表达，解析器要把"没写"与"写了 false"分开——
	// 混成同一个零值的话，跟着上层走的组件会全部变成一定不跑
	require.NotNil(t, pinned.Enabled)
	assert.True(t, *pinned.Enabled, "enabled: true → 一定跑")
	assert.False(t, pinned.IsDisabled())

	assert.Nil(t, dflt.Enabled, "不写 enabled → nil，不是 false")
	assert.False(t, dflt.IsDisabled(), "没写不等于关掉")

	require.NotNil(t, disabled.Enabled)
	assert.False(t, *disabled.Enabled)
	assert.True(t, disabled.IsDisabled(), "enabled: false → 一定不跑")
}

// ============================================================
// 5.14 多版本共存
// ============================================================

func TestSameComponentMultipleVersions(t *testing.T) {
	c, err := ParseConfig([]byte(`
project: my-project
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
  - id: people/basic
    version: 2.0.0
resources: []
`), "brickkit.yaml")
	require.NoError(t, err)

	require.Len(t, c.Components, 2, "同一组件 ID 的两个版本解析为两个独立条目")
	assert.Equal(t, "1.0.0", c.Components[0].Version)
	assert.Equal(t, "2.0.0", c.Components[1].Version)

	assert.Len(t, c.ComponentsByID("people/basic"), 2)
	assert.True(t, c.HasMultipleVersions("people/basic"))
	assert.False(t, c.HasMultipleVersions("nope/x"))
}

// ============================================================
// 5.18 / 5.19 空列表
// ============================================================

func TestEmptyListsAreValid(t *testing.T) {
	c, err := ParseConfig([]byte(minimalConfig), "brickkit.yaml")
	require.NoError(t, err)
	assert.Empty(t, c.Components, "5.18 空 components 列表")
	assert.Empty(t, c.Resources, "5.19 空 resources 列表")
	assert.Empty(t, c.Sources)
	assert.Nil(t, c.Installer)
	assert.True(t, c.RequireSignature(), "installer 缺省时 requireSignature 默认 true")
}

// components / resources 字段整体缺失也应可解析（等价于空列表）。
func TestOmittedListsAreValid(t *testing.T) {
	c, err := ParseConfig([]byte("project: my-project\ndeploy:\n  target: k8s\n"), "brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, TargetK8s, c.Deploy.Target)
	assert.Empty(t, c.Components)
}

// ============================================================
// 5.8–5.13 校验失败
// ============================================================

func TestConfigValidationErrors(t *testing.T) {
	cases := []struct {
		item     string
		name     string
		yaml     string
		contains []string
	}{
		{"5.8", "缺少 project", "deploy:\n  target: docker\ncomponents: []\n",
			[]string{"project", "缺失"}},
		{"—", "project 名称不合法", "project: My Project\ndeploy:\n  target: docker\n",
			[]string{"project", "小写"}},
		{"5.9", "缺少 deploy.target", "project: p\ncomponents: []\n",
			[]string{"deploy.target", "缺失"}},
		{"5.10", "deploy.target 非法值", "project: p\ndeploy:\n  target: ecs\n",
			[]string{"deploy.target", "docker", "k8s"}},
		{"5.11", "组件缺少 id", baseConfig + `
components:
  - version: 1.0.0
`, []string{"components[0].id", "缺失"}},
		{"5.12", "组件缺少 version", baseConfig + `
components:
  - id: people/basic
`, []string{"components[0].version", "缺失"}},
		{"5.13", "组件版本非精确版本", baseConfig + `
components:
  - id: people/basic
    version: ^1.0.0
`, []string{"components[0].version", "精确版本"}},
		{"5.13", "组件版本缺少 patch", baseConfig + `
components:
  - id: people/basic
    version: "1.0"
`, []string{"components[0].version", "精确版本"}},
		{"—", "组件 ID 含大写", baseConfig + `
components:
  - id: People/Basic
    version: 1.0.0
`, []string{"components[0].id", "小写"}},
		{"—", "组件 ID 缺少 scope", baseConfig + `
components:
  - id: basic
    version: 1.0.0
`, []string{"components[0].id", "scope/name"}},
		{"—", "同一组件同一版本重复声明", baseConfig + `
components:
  - id: people/basic
    version: 1.0.0
  - id: people/basic
    version: 1.0.0
`, []string{"components[1]", "重复"}},
		{"—", "localPort 越界", baseConfig + `
components:
  - id: people/basic
    version: 1.0.0
    local: true
    localPort: 99999
`, []string{"components[0].localPort", "1~65535"}},
		{"13.12", "localPort 冲突", baseConfig + `
components:
  - id: a/one
    version: 1.0.0
    local: true
    localPort: 8081
  - id: b/two
    version: 1.0.0
    local: true
    localPort: 8081
`, []string{"components[1].localPort", "冲突"}},
		{"—", "exposePort 冲突", baseConfig + `
components:
  - id: a/one
    version: 1.0.0
    expose: true
    exposePort: 8080
  - id: b/two
    version: 1.0.0
    expose: true
    exposePort: 8080
`, []string{"components[1].exposePort", "冲突"}},
		{"—", "K8s 环境 expose 缺少 hostname", `
project: p
deploy:
  target: k8s
components:
  - id: portal/web
    version: 1.0.0
    expose: true
`, []string{"components[0].hostname", "k8s"}},
		{"—", "未 expose 却设置 exposePort", baseConfig + `
components:
  - id: portal/web
    version: 1.0.0
    exposePort: 8080
`, []string{"components[0].exposePort", "expose"}},
		{"—", "安装源缺少 id", baseConfig + `
sources:
  - type: market
    url: https://x
`, []string{"sources[0].id", "缺失"}},
		{"—", "安装源类型非法", baseConfig + `
sources:
  - id: s
    type: ftp
    url: https://x
`, []string{"sources[0].type", "market"}},
		{"—", "market 源缺少 url", baseConfig + `
sources:
  - id: s
    type: market
`, []string{"sources[0].url", "缺失"}},
		{"—", "git 源缺少 url", baseConfig + `
sources:
  - id: s
    type: git
`, []string{"sources[0].url", "缺失"}},
		{"—", "local 源缺少 path", baseConfig + `
sources:
  - id: s
    type: local
`, []string{"sources[0].path", "缺失"}},
		{"—", "安装源 id 重复", baseConfig + `
sources:
  - id: s
    type: local
    path: ./a
  - id: s
    type: local
    path: ./b
`, []string{"sources[1].id", "重复"}},
		{"—", "资源缺少必填字段", baseConfig + `
resources:
  - kind: database
`, []string{"resources[0].engine", "resources[0].id", "resources[0].host", "resources[0].port"}},
		{"—", "资源 port 越界", baseConfig + `
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: localhost
    port: 70000
`, []string{"resources[0].port", "1~65535"}},
		{"—", "资源 id 重复", baseConfig + `
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: h1
    port: 5432
  - kind: database
    engine: postgresql
    id: pg
    host: h2
    port: 5432
`, []string{"resources[1].id", "重复"}},
		{"—", "binding 缺少 componentId", baseConfig + `
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: localhost
    port: 5432
    bindings:
      - database: people
`, []string{"resources[0].bindings[0].componentId", "缺失"}},
		{"—", "envPrefix 格式非法", `
project: p
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: localhost
    port: 5432
    bindings:
      - componentId: people/basic
        envPrefix: primary-db
`, []string{"resources[0].bindings[0].envPrefix"}},
	}

	for _, c := range cases {
		t.Run(c.item+" "+c.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(c.yaml), "brickkit.yaml")
			require.Error(t, err, "该配置应校验失败")
			assert.Nil(t, cfg)

			e := clierr.As(err)
			assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
			out := e.Format()
			for _, want := range c.contains {
				assert.Contains(t, out, want)
			}
		})
	}
}

// 悬空绑定（binding 指向 components 里没有的组件）**不阻断解析**，
// 只由 DanglingBindings 报出来供命令层警告。
//
// 它曾经是硬错误，代价完全不成比例：`brickkit remove` 之后必然残留一条，
// 于是一条报成功的命令让此后**每个**命令都跑不了；而它顺带还禁掉了
// "先声明资源与绑定、再 add 组件" 这种完全说得通的顺序。
func TestDanglingBindingIsNotAnError(t *testing.T) {
	cfg, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: host.docker.internal
    port: 5432
    bindings:
      - componentId: ghost/none
        database: x
      - componentId: people/basic
        database: people
`), "brickkit.yaml")
	require.NoError(t, err, "悬空绑定不该阻断解析")
	require.NotNil(t, cfg)

	dangling := cfg.DanglingBindings()
	require.Len(t, dangling, 1, "只有 ghost/none 是悬空的")
	assert.Equal(t, "pg", dangling[0].ResourceID)
	assert.Equal(t, "ghost/none", dangling[0].ComponentID)
}

// 没有悬空绑定时 DanglingBindings 返回空。
func TestDanglingBindingsEmptyWhenAllDeclared(t *testing.T) {
	cfg, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: host.docker.internal
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Empty(t, cfg.DanglingBindings())
}

// 一次报出全部问题。
func TestConfigMultipleProblemsReportedTogether(t *testing.T) {
	_, err := ParseConfig([]byte(`
deploy:
  target: ecs
components:
  - version: ^1.0.0
`), "brickkit.yaml")
	require.Error(t, err)

	out := clierr.As(err).Format()
	for _, want := range []string{"project", "deploy.target", "components[0].id", "components[0].version"} {
		assert.Contains(t, out, want)
	}
	assert.Contains(t, out, "brickkit.yaml")
}

// ============================================================
// 解析层错误（32.9 / 32.10）
// ============================================================

func TestParseConfigEmptyFile(t *testing.T) {
	for _, in := range []string{"", "  \n\n", "# 只有注释\n"} {
		_, err := ParseConfig([]byte(in), "brickkit.yaml")
		require.Error(t, err, "输入 %q 应报错", in)
		assert.Contains(t, clierr.As(err).Format(), "为空")
	}
}

func TestParseConfigInvalidYAML(t *testing.T) {
	_, err := ParseConfig([]byte("project: p\n\tbad: indent\n"), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "line", "32.10 应指出行号")
}

func TestParseConfigFileNotExist(t *testing.T) {
	_, err := ParseConfigFile(filepath.Join("testdata", "nope.yaml"))
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "不存在")
	assert.Contains(t, out, "brickkit init", "应提示先初始化项目")
}

// 必须是数组的字段写成标量时给出精确字段名。
func TestParseConfigShapeErrors(t *testing.T) {
	cases := map[string][]string{
		"project: p\ndeploy:\n  target: docker\ncomponents: people/basic\n": {"components", "数组"},
		"project: p\ndeploy:\n  target: docker\nresources: database\n":      {"resources", "数组"},
		"project: p\ndeploy:\n  target: docker\nsources: market\n":          {"sources", "数组"},
		"- a\n- b\n": {"顶层必须是一个 YAML 映射"},
	}
	for in, wants := range cases {
		_, err := ParseConfig([]byte(in), "brickkit.yaml")
		require.Error(t, err, in)
		out := clierr.As(err).Format()
		for _, want := range wants {
			assert.Contains(t, out, want)
		}
	}
}

// ============================================================
// 边界（Step 32 提前覆盖）
// ============================================================

// 32.6–32.8 config 值为空串 / null / 特殊字符都原样保留（CLI 不校验类型）。
func TestConfigValuePassthrough(t *testing.T) {
	c, err := ParseConfig([]byte(`
project: p
deploy:
  target: docker
components:
  - id: demo/app
    version: 1.0.0
    config:
      emptyString: ""
      nullValue:
      special: "a=b&c=d"
      number: 42
      boolean: false
      list: [1, 2]
      nested:
        key: value
resources: []
`), "brickkit.yaml")
	require.NoError(t, err)

	cfg := c.Components[0].Config
	assert.Equal(t, "", cfg["emptyString"])
	assert.Nil(t, cfg["nullValue"])
	assert.Equal(t, "a=b&c=d", cfg["special"])
	assert.Equal(t, 42, cfg["number"])
	assert.Equal(t, false, cfg["boolean"])
	assert.Equal(t, []any{1, 2}, cfg["list"])
	assert.Equal(t, map[string]any{"key": "value"}, cfg["nested"])
}

// 32.19 大量组件（50 个）正常解析。
func TestParseManyComponents(t *testing.T) {
	var b strings.Builder
	b.WriteString("project: big\ndeploy:\n  target: docker\ncomponents:\n")
	for i := 0; i < 50; i++ {
		b.WriteString("  - id: scope/c" + itoa(i) + "\n    version: 1.0.0\n")
	}

	c, err := ParseConfig([]byte(b.String()), "brickkit.yaml")
	require.NoError(t, err)
	assert.Len(t, c.Components, 50)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// ============================================================
// 两条绑定抢同一批连接变量
// ============================================================

// twoDatabases 造一份"两个 postgres 都绑给同一个组件"的配置。
// extra 追加到第二条绑定上（比如 envPrefix）。
func twoDatabases(extra string) string {
	return baseConfig + `
components:
  - id: people/basic
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: primary
    host: pri.example
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
  - kind: database
    engine: postgresql
    id: archive
    host: arc.example
    port: 5433
    bindings:
      - componentId: people/basic
        database: people_archive
` + extra
}

// ⚠️ 回归：这从前是静默通过的，而后果是前一个资源整个蒸发。
//
// 注入引擎按变量名写表，同名后写覆盖先写。两个 postgres 都没写 envPrefix 时，
// 组件拿到的 DATABASE_* 全来自配置里靠后的那个——它以为连着 primary，
// 实际连的是 archive。K8s 侧只生成 archive-secret，primary 在生成物里一处都不剩。
func TestTwoSameKindResourcesWithoutPrefixIsRejected(t *testing.T) {
	_, err := ParseConfig([]byte(twoDatabases("")), "brickkit.yaml")

	require.Error(t, err, "抢同一批变量名，必须当场拦下")
	text := err.Error()
	assert.Contains(t, text, "primary", "要点名是哪两个资源")
	assert.Contains(t, text, "archive")
	assert.Contains(t, text, "people/basic", "以及是哪个组件")
	assert.Contains(t, text, "DATABASE_HOST", "把抢的变量列出来，不然还得去翻 006 §5.2")
	assert.Contains(t, text, "envPrefix", "给出出路")
}

// 一个写了 envPrefix、一个没写：PRIMARY_DATABASE_* 与 DATABASE_*，不冲突。
//
// 这是合理写法（默认库 + 附加库），拦下它就是误伤。
func TestSameKindResourcesWithDistinctPrefixIsFine(t *testing.T) {
	_, err := ParseConfig([]byte(twoDatabases("        envPrefix: ARCHIVE\n")), "brickkit.yaml")

	require.NoError(t, err, "前缀不同就不冲突：%v", err)
}

// 两个都写了**同一个** envPrefix：照样是抢同一批变量。
func TestSameKindResourcesWithSamePrefixIsRejected(t *testing.T) {
	cfg := twoDatabases("")
	cfg = strings.Replace(cfg, "        database: people\n",
		"        database: people\n        envPrefix: MAIN\n", 1)
	cfg = strings.Replace(cfg, "        database: people_archive\n",
		"        database: people_archive\n        envPrefix: MAIN\n", 1)

	_, err := ParseConfig([]byte(cfg), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "MAIN_DATABASE_HOST", "变量名要带上那个前缀")
}

// 不同 kind 绑同一个组件：DATABASE_* 与 REDIS_*，本来就不会撞。
func TestDifferentKindsForSameComponentIsFine(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: pg
    host: pg.example
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
  - kind: cache
    engine: redis
    id: redis
    host: redis.example
    port: 6379
    bindings:
      - componentId: people/basic
`), "brickkit.yaml")

	require.NoError(t, err, "不同 kind 的变量前缀本来就不同：%v", err)
}

// 同一个资源里对同一组件写两条绑定：同一种事故的另一个入口。
//
// 两条各写各的 database 名，DATABASE_NAME 被写两次，后者赢——
// 与跨资源碰撞完全同构，所以由同一个判据抓住。
func TestDuplicateBindingsInOneResourceIsRejected(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: pg
    host: pg.example
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
      - componentId: people/basic
        database: people_v2
`), "brickkit.yaml")

	require.Error(t, err, "同一个组件在同一个资源里绑两次，后者会盖掉前者")
}

// 同一个资源绑给**不同**组件：各拿各的，完全正常。
func TestOneResourceBoundToManyComponentsIsFine(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0
  - id: department/tree
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: pg
    host: pg.example
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
      - componentId: department/tree
        database: department
`), "brickkit.yaml")

	require.NoError(t, err, "006 §7.3 推荐的正是这种写法：%v", err)
}
