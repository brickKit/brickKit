// 本文件是 Step 4「component.yaml 解析器」的代码层测试：形状错误、
// null 字段、各条校验规则的分支与边界。
//
// 业务行为由 manifest_test.go 从"解析出来的 Manifest 里有什么"那一侧盯住；
// 这里补的是从外面不容易逼出来的分支（YAML 别名、读不动的文件、
// 依赖写成序列、越界的 extraPort）。
package manifest

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
// 形状检查（解码前）
// ============================================================

func TestShapeErrors(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		contains []string
	}{
		{"顶层是数组", "- a\n- b\n", []string{"component.yaml", "顶层必须是一个 YAML 映射"}},
		{"顶层是标量", "just-a-string\n", []string{"顶层必须是一个 YAML 映射"}},
		{"tags 非数组", minimalYAML + "tags: people\n", []string{"tags", "必须是数组格式", "标量"}},
		{"artifacts 非数组", minimalYAML + "artifacts: openapi.json\n", []string{"artifacts", "必须是数组格式"}},
		{"artifacts 元素非映射", minimalYAML + "artifacts:\n  - just-a-string\n",
			[]string{"artifacts[0]", "必须是映射"}},
		{"artifacts[].files 非数组", minimalYAML + "artifacts:\n  - type: api-docs\n    files: openapi.json\n",
			[]string{"artifacts[0].files", "必须是数组格式"}},
		{"dependencies.components 非数组", minimalYAML + "dependencies:\n  components: department/tree@1.0.0\n",
			[]string{"dependencies.components", "必须是数组格式"}},
		{"dependencies.resources 非数组", minimalYAML + "dependencies:\n  resources: database\n",
			[]string{"dependencies.resources", "必须是数组格式"}},
		{"extraPorts 非数组", minimalYAML + "  extraPortsX: x\n", nil}, // 占位：字段名不同，不触发
		{"configSchema.required 非数组", minimalYAML + "configSchema:\n  type: object\n  required: pageSize\n",
			[]string{"configSchema.required", "必须是数组格式"}},
		{"migration.command 是映射", minimalYAML + "migration:\n  command:\n    cmd: migrate\n",
			[]string{"migration.command", "必须是数组格式", "映射"}},
	}
	for _, c := range cases {
		if c.contains == nil {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml), "component.yaml")
			require.Error(t, err)
			out := clierr.As(err).Format()
			for _, want := range c.contains {
				assert.Contains(t, out, want)
			}
		})
	}
}

// YAML 锚点/别名指向标量时，仍应报出"必须是数组格式"。
func TestShapeErrorWithYAMLAlias(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`
anchors: &tag people
tags: *tag
`), "component.yaml")
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "tags")
	assert.Contains(t, out, "别名")
}

// apiVersion / kind 整体缺失时按"必填字段缺失"报错。
func TestAPIVersionAndKindMissing(t *testing.T) {
	_, err := Parse([]byte(`
metadata:
  id: infra/tool
  name: 工具
  version: 1.0.0
  description: d
deployment:
  type: container
  image: img:1
  port: 8080
healthCheck:
  type: http
  path: /healthz
`), "component.yaml")
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "apiVersion：缺失")
	assert.Contains(t, out, "kind：缺失")
}

// artifacts 文件路径为空字符串。
func TestArtifactEmptyFilePath(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`
artifacts:
  - type: api-docs
    files: ["", "openapi.json"]
`), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "artifacts[0].files[0]：缺失")
}

// 依赖映射中字段类型写错（optional 不是布尔值）。
func TestDependencyMappingBadOptionalType(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`
dependencies:
  components:
    - id: department/tree@1.0.0
      optional: [not, a, bool]
`), "component.yaml")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeManifestInvalid, clierr.As(err).Code)
}

// 字段显式写成 null 时按"未声明"处理，不报形状错误。
func TestNullFieldsAreTreatedAsAbsent(t *testing.T) {
	m, err := Parse([]byte(minimalYAML+`
tags:
artifacts:
dependencies:
migration:
`), "component.yaml")
	require.NoError(t, err)
	assert.Empty(t, m.Tags)
	assert.Empty(t, m.Artifacts)
	assert.Nil(t, m.Migration)
}

// ============================================================
// 解码类型错误（形状检查之后仍可能发生）
// ============================================================

func TestDecodeTypeErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"port 是字符串", mustReplace(minimalYAML, "port: 8080", `port: "abc"`)},
		{"metadata 是数组", mustReplace(minimalYAML, "metadata:", "metadata: [a, b]\nignored:")},
		{"依赖项是数组元素", minimalYAML + "dependencies:\n  components:\n    - [a, b]\n"},
		{"observability.metrics 是字符串", minimalYAML + "observability:\n  metrics: not-a-bool\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml), "component.yaml")
			require.Error(t, err)

			e := clierr.As(err)
			assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
			assert.Contains(t, e.Format(), "component.yaml")
		})
	}
}

func mustReplace(s, old, new string) string {
	out := s
	if idx := indexOf(s, old); idx >= 0 {
		out = s[:idx] + new + s[idx+len(old):]
	}
	return out
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ============================================================
// 依赖写法的两种形式（UnmarshalYAML）
// ============================================================

func TestComponentDepUnmarshal(t *testing.T) {
	cases := []struct {
		name        string
		yaml        string
		wantID      string
		wantVersion string
		wantOpt     bool
		wantRef     string
	}{
		{"标量强依赖", `department/tree@1.0.0`, "department/tree", "1.0.0", false, "department/tree@1.0.0"},
		{"映射弱依赖", "{id: infra/redis-event-bus@1.0.0, optional: true}",
			"infra/redis-event-bus", "1.0.0", true, "infra/redis-event-bus@1.0.0"},
		{"映射未写 optional", "{id: people/basic@2.0.0}", "people/basic", "2.0.0", false, "people/basic@2.0.0"},
		{"映射 optional: false", "{id: people/basic@2.0.0, optional: false}", "people/basic", "2.0.0", false, "people/basic@2.0.0"},
		{"无 @ 版本", `department/tree`, "department/tree", "", false, "department/tree"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var dep ComponentDep
			require.NoError(t, yaml.Unmarshal([]byte(c.yaml), &dep))
			assert.Equal(t, c.wantID, dep.ID)
			assert.Equal(t, c.wantVersion, dep.Version)
			assert.Equal(t, c.wantOpt, dep.Optional)
			assert.Equal(t, c.wantOpt, dep.IsOptional())
			assert.Equal(t, c.wantRef, dep.Ref)
		})
	}
}

func TestComponentDepUnmarshalRejectsSequence(t *testing.T) {
	var dep ComponentDep
	err := yaml.Unmarshal([]byte("[a, b]"), &dep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<组件ID>@<版本>")
}

// ============================================================
// ParseFile 与 source 处理
// ============================================================

func TestParseFileUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户会绕过文件权限")
	}
	path := filepath.Join(t.TempDir(), FileName)
	require.NoError(t, os.WriteFile(path, []byte(minimalYAML), 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := ParseFile(path)
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
	assert.Contains(t, e.Format(), "读取 component.yaml 失败")
}

// source 为空时错误信息回落到默认文件名。
func TestParseWithoutSourceName(t *testing.T) {
	_, err := Parse([]byte("apiVersion: brickkit/v1\nkind: Component\n"), "")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "文件：component.yaml")
}

// Source 字段记录来源，供后续 Step 的错误提示使用。
func TestParseRecordsSource(t *testing.T) {
	m, err := Parse([]byte(minimalYAML), "components/infra/tool/component.yaml")
	require.NoError(t, err)
	assert.Equal(t, "components/infra/tool/component.yaml", m.Source)
}

// Validate 可以直接对手工构造的 Manifest 调用（后续 Step 会这样用）。
func TestValidateOnConstructedManifest(t *testing.T) {
	m := &Manifest{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata: Metadata{
			ID: "people/basic", Name: "人员", Version: "1.0.0", Description: "d",
		},
		Deployment:  Deployment{Type: DeploymentTypeContainer, Image: "img:1", Port: 8080},
		HealthCheck: HealthCheck{Type: HealthCheckHTTP, Path: "/healthz"},
	}
	require.NoError(t, m.Validate())

	m.Metadata.Version = "1.0"
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "文件：component.yaml")
}

// ============================================================
// 校验细节
// ============================================================

func TestComponentIDProblem(t *testing.T) {
	valid := []string{"people/basic", "infra/redis-event-bus", "a/b", "a1/b2-c3", "portal/user-frontend"}
	for _, id := range valid {
		assert.Empty(t, componentIDProblem(id), id)
	}

	invalid := map[string]string{
		"People/Basic":  "小写",
		"people basic":  "scope/name",
		"basic":         "scope/name",
		"people/":       "scope/name",
		"/basic":        "scope/name",
		"a/b/c":         "scope/name",
		"people_basic":  "scope/name",
		"-people/basic": "scope/name",
		"people/basic-": "scope/name",
	}
	for id, want := range invalid {
		assert.Contains(t, componentIDProblem(id), want, id)
	}

	long := "a/" + string(make([]byte, MaxComponentIDLen))
	assert.Contains(t, componentIDProblem(long), "长度")
}

func TestExactVersionRule(t *testing.T) {
	for _, v := range []string{"1.0.0", "0.0.1", "999999.999999.999999", "10.20.30"} {
		assert.True(t, exactVersionRe.MatchString(v), v)
	}
	for _, v := range []string{"1.0", "1", "abc", "^1.0.0", "~1.0.0", "1.0.0-beta", "1.0.0+build", "v1.0.0", "1.0.x", ""} {
		assert.False(t, exactVersionRe.MatchString(v), v)
	}
}

func TestEscapesRepoRoot(t *testing.T) {
	for _, p := range []string{"../x", "../../etc/passwd", "a/../../b", ".."} {
		assert.True(t, escapesRepoRoot(p), p)
	}
	for _, p := range []string{"a/b.proto", "./a/b.proto", "a/../b.proto", "openapi.json", "a b/c.proto"} {
		assert.False(t, escapesRepoRoot(p), p)
	}
}

// 资源配额只写 limits（不写 requests）是合法的。
func TestResourcesLimitsOnly(t *testing.T) {
	m, err := Parse([]byte(mustReplace(minimalYAML, "  port: 8080", `  port: 8080
  resources:
    limits:
      memory: "512Mi"`)), "component.yaml")
	require.NoError(t, err)
	assert.Nil(t, m.Deployment.Resources.Requests)
	assert.Equal(t, "512Mi", m.Deployment.Resources.Limits.Memory)
}

func TestResourcesEmptyLimitsRejected(t *testing.T) {
	_, err := Parse([]byte(mustReplace(minimalYAML, "  port: 8080", `  port: 8080
  resources:
    limits: {}`)), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "deployment.resources.limits")
}

// migration.command 中出现空参数应报错（拼错命令的常见形式）。
func TestMigrationCommandEmptyArg(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`
migration:
  command: ["python", "  "]
`), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "migration.command[1]")
}

// extraPorts 端口越界与端口名超长。
func TestExtraPortBoundaries(t *testing.T) {
	_, err := Parse([]byte(mustReplace(minimalYAML, "  port: 8080", `  port: 8080
  extraPorts:
    - name: grpc
      port: 70000`)), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "deployment.extraPorts[0].port")

	_, err = Parse([]byte(mustReplace(minimalYAML, "  port: 8080", `  port: 8080
  extraPorts:
    - name: aaaaaaaaaaaaaaaaaaaa
      port: 9090`)), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "deployment.extraPorts[0].name")
}

// configSchema 的每个属性都必须声明 type。
func TestConfigSchemaPropertyMissingType(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`
configSchema:
  type: object
  properties:
    pageSize:
      default: 20
`), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "configSchema.properties.pageSize.type")
}

// configSchema 支持 enum 与 array items（002 §6.5 的说明书用途）。
func TestConfigSchemaEnumAndItems(t *testing.T) {
	m, err := Parse([]byte(minimalYAML+`
configSchema:
  type: object
  properties:
    logLevel:
      type: string
      enum: [debug, info, warn, error]
      default: info
    allowedHosts:
      type: array
      items:
        type: string
`), "component.yaml")
	require.NoError(t, err)
	assert.Equal(t, []any{"debug", "info", "warn", "error"}, m.ConfigSchema.Properties["logLevel"].Enum)
	require.NotNil(t, m.ConfigSchema.Properties["allowedHosts"].Items)
	assert.Equal(t, "string", m.ConfigSchema.Properties["allowedHosts"].Items.Type)
}

// 重复声明同一依赖应报错。
func TestDuplicateDependency(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`
dependencies:
  components:
    - department/tree@1.0.0
    - department/tree@1.0.0
`), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "重复声明")
}

// 依赖写成映射但缺少 id。
func TestDependencyMappingWithoutID(t *testing.T) {
	_, err := Parse([]byte(minimalYAML+`
dependencies:
  components:
    - optional: true
`), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "dependencies.components[0]")
}

// nodeKindName 是错误文案的一部分，直接对每种节点类型断言一次。
func TestNodeKindName(t *testing.T) {
	cases := map[yaml.Kind]string{
		yaml.ScalarNode:   "标量",
		yaml.MappingNode:  "映射",
		yaml.SequenceNode: "数组",
		yaml.AliasNode:    "别名",
		yaml.DocumentNode: "未知类型",
	}
	for kind, want := range cases {
		assert.Equal(t, want, nodeKindName(&yaml.Node{Kind: kind}))
	}
	assert.Equal(t, "未知类型", nodeKindName(&yaml.Node{}))
}

// 导出给 config 包复用的两个规则函数（Step 5 的 brickkit.yaml 校验依赖它们）。
func TestExportedRuleHelpers(t *testing.T) {
	assert.Empty(t, ComponentIDProblem("people/basic"))
	assert.Contains(t, ComponentIDProblem("People/Basic"), "小写")

	assert.True(t, IsExactVersion("1.2.3"))
	assert.False(t, IsExactVersion("^1.2.3"))
	assert.False(t, IsExactVersion("1.2"))
}
