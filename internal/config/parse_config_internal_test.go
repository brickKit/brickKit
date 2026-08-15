package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
)

// ============================================================
// ExpandEnv
// ============================================================

func TestExpandEnv(t *testing.T) {
	t.Setenv("NAME", "brickkit")
	t.Setenv("EMPTY_VALUE", "")

	cases := map[string]string{
		"":                       "",
		"no vars here":           "no vars here",
		"${NAME}":                "brickkit",
		"a-${NAME}-b":            "a-brickkit-b",
		"${NAME}${NAME}":         "brickkitbrickkit",
		"${EMPTY_VALUE}":         "",
		"${MISSING_VAR_XYZ}":     "${MISSING_VAR_XYZ}",
		"${NAME}/${MISSING_XYZ}": "brickkit/${MISSING_XYZ}",
		"$NAME":                  "$NAME",
		"${}":                    "${}",
		"${1INVALID}":            "${1INVALID}",
		"${lower_case}":          "${lower_case}",
		"{NAME}":                 "{NAME}",
	}
	for in, want := range cases {
		assert.Equal(t, want, ExpandEnv(in), "ExpandEnv(%q)", in)
	}
}

// 小写环境变量名也能引用（shell 惯例允许）。
func TestExpandEnvLowercaseName(t *testing.T) {
	t.Setenv("lower_case", "ok")
	assert.Equal(t, "ok", ExpandEnv("${lower_case}"))
}

// ============================================================
// 形状检查
// ============================================================

func TestConfigShapeErrorDetails(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		contains []string
	}{
		{"组件条目不是映射", baseConfig + "components:\n  - people/basic\n",
			[]string{"components[0]", "必须是映射", "id 与 version"}},
		{"components[].config 不是映射", baseConfig + `
components:
  - id: a/b
    version: 1.0.0
    config: not-a-map
`, []string{"components[0].config", "必须是映射", "标量"}},
		{"components[].resources 不是映射", baseConfig + `
components:
  - id: a/b
    version: 1.0.0
    resources: [1, 2]
`, []string{"components[0].resources", "必须是映射", "数组"}},
		{"资源条目不是映射", baseConfig + "resources:\n  - postgres\n",
			[]string{"resources[0]", "必须是映射"}},
		{"bindings 不是数组", baseConfig + `
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: h
    port: 5432
    bindings: people/basic
`, []string{"resources[0].bindings", "必须是数组格式"}},
		{"别名指向标量", baseConfig + "anchor: &c x\ncomponents: *c\n",
			[]string{"components", "别名"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(c.yaml), "brickkit.yaml")
			require.Error(t, err)
			out := clierr.As(err).Format()
			for _, want := range c.contains {
				assert.Contains(t, out, want)
			}
		})
	}
}

// 字段显式为 null 时视为未声明。
func TestConfigNullFieldsTreatedAsAbsent(t *testing.T) {
	c, err := ParseConfig([]byte(`
project: p
deploy:
  target: docker
sources:
components:
resources:
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Empty(t, c.Sources)
	assert.Empty(t, c.Components)
	assert.Empty(t, c.Resources)
}

func TestConfigNullSubFieldsTreatedAsAbsent(t *testing.T) {
	c, err := ParseConfig([]byte(baseConfig+`
components:
  - id: a/b
    version: 1.0.0
    config:
    resources:
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: h
    port: 5432
    bindings:
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Nil(t, c.Components[0].Config)
	assert.Nil(t, c.Components[0].Resources)
	assert.Empty(t, c.Resources[0].Bindings)
}

func TestLookupNodeNonMappingIntermediate(t *testing.T) {
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: scalar\n"), &root))
	doc := root.Content[0]

	assert.Nil(t, lookupNode(doc, "a", "b"), "中间层不是映射时返回 nil")
	assert.Nil(t, lookupNode(doc, "missing"))
	assert.NotNil(t, lookupNode(doc, "a"))
}

func TestNodeKind(t *testing.T) {
	cases := map[yaml.Kind]string{
		yaml.ScalarNode:   "标量",
		yaml.MappingNode:  "映射",
		yaml.SequenceNode: "数组",
		yaml.AliasNode:    "别名",
		yaml.DocumentNode: "未知类型",
	}
	for kind, want := range cases {
		assert.Equal(t, want, nodeKind(&yaml.Node{Kind: kind}))
	}
}

// ============================================================
// 解码类型错误
// ============================================================

func TestConfigDecodeTypeErrors(t *testing.T) {
	cases := map[string]string{
		"port 是字符串": baseConfig + `
resources:
  - kind: database
    engine: postgresql
    id: pg
    host: h
    port: "abc"
`,
		"enabled 是字符串": baseConfig + `
components:
  - id: a/b
    version: 1.0.0
    enabled: maybe
`,
		"deploy 是标量": "project: p\ndeploy: docker\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseConfig([]byte(in), "brickkit.yaml")
			require.Error(t, err)
			assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
		})
	}
}

// ============================================================
// ParseConfigFile
// ============================================================

func TestParseConfigFileUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户会绕过文件权限")
	}
	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	require.NoError(t, os.WriteFile(path, []byte(minimalConfig), 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := ParseConfigFile(path)
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "读取项目配置文件失败")
}

func TestParseConfigFileMissingUsesProjectMissingCode(t *testing.T) {
	_, err := ParseConfigFile(filepath.Join(t.TempDir(), DefaultConfigFile))
	require.Error(t, err)
	assert.Equal(t, clierr.CodeProjectMissing, clierr.As(err).Code)
}

func TestParseConfigWithoutSourceName(t *testing.T) {
	_, err := ParseConfig([]byte("deploy:\n  target: docker\n"), "")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "文件：brickkit.yaml")
}

func TestParseConfigRecordsSource(t *testing.T) {
	c, err := ParseConfig([]byte(minimalConfig), "envs/brickkit.prod.yaml")
	require.NoError(t, err)
	assert.Equal(t, "envs/brickkit.prod.yaml", c.Source)
}

// Validate 可以直接对手工构造的 Config 调用（后续 Step 会这样用）。
func TestValidateOnConstructedConfig(t *testing.T) {
	c := &Config{Project: "demo", Deploy: Deploy{Target: TargetK8s}}
	require.NoError(t, c.Validate())

	c.Deploy.Target = "ecs"
	require.Error(t, c.Validate())
}

// ============================================================
// 访问器
// ============================================================

func TestEnabledSourcesFiltersDisabled(t *testing.T) {
	c, err := ParseConfig([]byte(baseConfig+`
sources:
  - id: local-dev
    type: local
    path: ./components
  - id: my-git
    type: git
    url: https://github.com/org/repo.git
    enabled: false
  - id: market
    type: market
    url: https://market.brickkit.io/api/v1
    enabled: true
`), "brickkit.yaml")
	require.NoError(t, err)

	require.Len(t, c.Sources, 3)
	assert.True(t, c.Sources[0].IsEnabled(), "缺省 enabled 视为启用")
	assert.False(t, c.Sources[1].IsEnabled())

	enabled := c.EnabledSources()
	require.Len(t, enabled, 2, "只返回启用中的源，且保持配置顺序（003 §6.5 优先级）")
	assert.Equal(t, "local-dev", enabled[0].ID)
	assert.Equal(t, "market", enabled[1].ID)
}

func TestComponentRefAndLookup(t *testing.T) {
	c, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0
  - id: department/tree
    version: 2.1.3
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.Equal(t, "people/basic@1.0.0", c.Components[0].Ref())
	assert.Len(t, c.ComponentsByID("people/basic"), 1)
	assert.Empty(t, c.ComponentsByID("nope/x"))
	assert.False(t, c.HasMultipleVersions("people/basic"))
}

func TestRequireSignatureDefaults(t *testing.T) {
	// installer 缺省 → true（附录 D.1）
	c, err := ParseConfig([]byte(minimalConfig), "brickkit.yaml")
	require.NoError(t, err)
	assert.True(t, c.RequireSignature())

	// installer 存在但未写 requireSignature → 仍为 true
	c, err = ParseConfig([]byte(minimalConfig+"installer: {}\n"), "brickkit.yaml")
	require.NoError(t, err)
	assert.True(t, c.RequireSignature())

	// 显式 true
	c, err = ParseConfig([]byte(minimalConfig+"installer:\n  requireSignature: true\n"), "brickkit.yaml")
	require.NoError(t, err)
	assert.True(t, c.RequireSignature())
}

// TestPublicKeysParsed 覆盖 installer.publicKeys：项目自己声明信任哪些发布者公钥。
func TestPublicKeysParsed(t *testing.T) {
	c, err := ParseConfig([]byte(minimalConfig+`installer:
  requireSignature: true
  publicKeys:
    keys/people-basic-release.pub: keys/people-basic-release.pub
    keys/vendor-release.pub: /etc/brickkit/vendor-release.pub
`), "brickkit.yaml")
	require.NoError(t, err)

	assert.True(t, c.RequireSignature())
	assert.Equal(t, map[string]string{
		"keys/people-basic-release.pub": "keys/people-basic-release.pub",
		"keys/vendor-release.pub":       "/etc/brickkit/vendor-release.pub",
	}, c.PublicKeys())
}

// TestPublicKeysAbsentIsNotAnError：没配公钥不是配置错误。
//
// requireSignature 默认为 true，若在解析阶段就要求必须配公钥，所有还没用上签名的
// 现有项目会连 brickkit status 都跑不起来。该拦的地方是安装时。
func TestPublicKeysAbsentIsNotAnError(t *testing.T) {
	c, err := ParseConfig([]byte(minimalConfig), "brickkit.yaml")
	require.NoError(t, err)

	assert.Nil(t, c.PublicKeys())
	assert.True(t, c.RequireSignature())
}

func TestEnabledStateStringFallback(t *testing.T) {
	assert.Equal(t, "默认开启", EnabledState(99).String())
}

// ============================================================
// 校验细节补充
// ============================================================

// 合法的 git / market / local 三种源都应通过校验。
func TestAllSourceTypesValid(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
sources:
  - id: market
    type: market
    url: https://market.brickkit.io/api/v1
    authToken: ${SOME_TOKEN}
  - id: git
    type: git
    url: https://github.com/org/repo.git
  - id: local
    type: local
    path: ./components
`), "brickkit.yaml")
	require.NoError(t, err)
}

// Docker 环境下 expose 不写 hostname 是合法的（003 §4.5）。
func TestExposeWithoutHostnameValidOnDocker(t *testing.T) {
	c, err := ParseConfig([]byte(baseConfig+`
components:
  - id: portal/web
    version: 1.0.0
    expose: true
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.True(t, c.Components[0].Expose)
	assert.Empty(t, c.Components[0].Hostname)
}

func TestExposePortOutOfRange(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
components:
  - id: portal/web
    version: 1.0.0
    expose: true
    exposePort: 70000
`), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "components[0].exposePort")
}

// 多资源绑定的合法 envPrefix（003 §5.6）。
func TestValidEnvPrefix(t *testing.T) {
	c, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: pg-primary
    host: primary-db
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
        envPrefix: PRIMARY
  - kind: database
    engine: postgresql
    id: pg-archive
    host: archive-db
    port: 5432
    bindings:
      - componentId: people/basic
        database: people_archive
        envPrefix: ARCHIVE_DB2
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "PRIMARY", c.Resources[0].Bindings[0].EnvPrefix)
	assert.Equal(t, "ARCHIVE_DB2", c.Resources[1].Bindings[0].EnvPrefix)
}

// local: true 但不写 localPort 是合法的（CLI 在 Step 13 自动分配）。
func TestLocalWithoutPortIsValid(t *testing.T) {
	c, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0
    local: true
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.True(t, c.Components[0].Local)
	assert.Zero(t, c.Components[0].LocalPort)
}

// 安装源缺少 type。
func TestSourceMissingType(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
sources:
  - id: s
    url: https://x
`), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "sources[0].type：缺失")
}

// localPort 写了但没写 local: true（常见误配）。
func TestLocalPortWithoutLocal(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
components:
  - id: people/basic
    version: 1.0.0
    localPort: 8081
`), "brickkit.yaml")
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "components[0].localPort")
	assert.Contains(t, out, "local: true")
}

// 资源缺少 kind。
func TestResourceMissingKind(t *testing.T) {
	_, err := ParseConfig([]byte(baseConfig+`
resources:
  - engine: postgresql
    id: pg
    host: h
    port: 5432
`), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "resources[0].kind：缺失")
}

// PasswordFromEnv 必须记录**原文**写没写 ${ENV_VAR}。
//
// 展开之后 `${POSTGRES_PASSWORD}` 与一个写死的密码长得一模一样，
// 分不出来的话，008 要求的"密码不写进 brickkit.yaml"就没法检查。
func TestPasswordFromEnvRecordsTheReference(t *testing.T) {
	t.Setenv("PG_PASSWORD", "resolved-value")

	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: from-env
    host: db.example.com
    port: 5432
    password: ${PG_PASSWORD}
    bindings:
      - componentId: people/basic
        database: people
  - kind: cache
    engine: redis
    id: hardcoded
    host: redis.example.com
    port: 6379
    password: written-in-plain-text
    bindings:
      - componentId: people/basic
`), "brickkit.yaml")

	require.NoError(t, err)
	require.Len(t, c.Resources, 2)

	assert.True(t, c.Resources[0].PasswordFromEnv, "写的是 ${ENV_VAR}")
	assert.Equal(t, "resolved-value", c.Resources[0].Password, "值照常展开")
	assert.False(t, c.Resources[1].PasswordFromEnv, "写死的就是写死的")
}

// 变量没配时占位符原样保留，同样算"写的是引用"。
func TestPasswordFromEnvWhenVariableMissing(t *testing.T) {
	c, err := ParseConfig([]byte(`project: demo
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: from-env
    host: db.example.com
    port: 5432
    password: ${PG_PASSWORD_NOT_SET}
    bindings:
      - componentId: people/basic
        database: people
`), "brickkit.yaml")

	require.NoError(t, err)
	assert.True(t, c.Resources[0].PasswordFromEnv)
	assert.Equal(t, "${PG_PASSWORD_NOT_SET}", c.Resources[0].Password, "漏配时保留占位符")
}
