package manifest

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// ============================================================
// 合法 Manifest 解析（4.1、4.19–4.25）
// ============================================================

// 4.1 用 department/tree 的 component.yaml 测试（002 §12.5）。
func TestParseFileDepartmentTree(t *testing.T) {
	m, err := ParseFile(filepath.Join("testdata", "department-tree.yaml"))
	require.NoError(t, err)

	assert.Equal(t, "brickkit/v1", m.APIVersion)
	assert.Equal(t, "Component", m.Kind)

	assert.Equal(t, "department/tree", m.Metadata.ID)
	assert.Equal(t, "部门树组件", m.Metadata.Name)
	assert.Equal(t, "1.0.0", m.Metadata.Version)
	assert.Equal(t, "brickkit-official", m.Metadata.Vendor)
	assert.Equal(t, "MIT", m.Metadata.License)
	assert.Equal(t, []string{"department", "master-data", "backend", "grpc"}, m.Tags)

	// artifacts
	require.Len(t, m.Artifacts, 2)
	assert.Equal(t, "api-contract", m.Artifacts[0].Type)
	assert.Equal(t, "protobuf", m.Artifacts[0].Format)
	assert.Equal(t, "gRPC API 契约文件", m.Artifacts[0].Description)
	assert.Equal(t, []string{"proto/department/v1/department.proto"}, m.Artifacts[0].Files)
	assert.Equal(t, "api-docs", m.Artifacts[1].Type)
	assert.Equal(t, []string{"openapi.json"}, m.Artifacts[1].Files)

	// dependencies：空组件依赖 + 一个资源依赖
	require.NotNil(t, m.Dependencies)
	assert.Empty(t, m.Dependencies.Components)
	require.Len(t, m.Dependencies.Resources, 1)
	assert.Equal(t, "database", m.Dependencies.Resources[0].Kind)
	assert.Equal(t, "postgresql", m.Dependencies.Resources[0].Engine)

	// configSchema
	require.NotNil(t, m.ConfigSchema)
	assert.Equal(t, "object", m.ConfigSchema.Type)
	require.Contains(t, m.ConfigSchema.Properties, "defaultPageSize")
	assert.Equal(t, "integer", m.ConfigSchema.Properties["defaultPageSize"].Type)
	assert.Equal(t, 20, m.ConfigSchema.Properties["defaultPageSize"].Default)
	assert.Equal(t, "默认分页大小", m.ConfigSchema.Properties["defaultPageSize"].Description)
	assert.Contains(t, m.ConfigSchema.Properties, "maxTreeDepth")

	// deployment
	assert.Equal(t, "container", m.Deployment.Type)
	assert.Equal(t, "registry.brickkit.io/department-tree:1.0.0", m.Deployment.Image)
	assert.Equal(t, 8080, m.Deployment.Port)
	assert.Empty(t, m.Deployment.ExtraPorts)
	require.NotNil(t, m.Deployment.Resources)
	assert.Equal(t, "100m", m.Deployment.Resources.Requests.CPU)
	assert.Equal(t, "128Mi", m.Deployment.Resources.Requests.Memory)
	assert.Equal(t, "500m", m.Deployment.Resources.Limits.CPU)
	assert.Equal(t, "512Mi", m.Deployment.Resources.Limits.Memory)

	// migration（数组格式）
	require.NotNil(t, m.Migration)
	assert.Equal(t,
		[]string{"./migrate", "-path", "/migrations", "-database", "$DATABASE_URL", "up"},
		m.Migration.Command)

	// healthCheck
	assert.Equal(t, "http", m.HealthCheck.Type)
	assert.Equal(t, "/healthz", m.HealthCheck.Path)
}

// 4.19 弱依赖 optional: true 正确识别；extraPorts 正确解析（002 §2.2）。
func TestParseFilePeopleBasic(t *testing.T) {
	m, err := ParseFile(filepath.Join("testdata", "people-basic.yaml"))
	require.NoError(t, err)

	require.NotNil(t, m.Dependencies)
	require.Len(t, m.Dependencies.Components, 2)

	strong := m.Dependencies.Components[0]
	assert.Equal(t, "department/tree", strong.ID)
	assert.Equal(t, "1.0.0", strong.Version)
	assert.False(t, strong.Optional, "不写 optional 即强依赖")

	weak := m.Dependencies.Components[1]
	assert.Equal(t, "infra/redis-event-bus", weak.ID)
	assert.Equal(t, "1.0.0", weak.Version)
	assert.True(t, weak.Optional, "optional: true 应识别为弱依赖")

	require.Len(t, m.Deployment.ExtraPorts, 1)
	assert.Equal(t, "grpc", m.Deployment.ExtraPorts[0].Name)
	assert.Equal(t, 9090, m.Deployment.ExtraPorts[0].Port)

	assert.Equal(t, []string{"defaultPageSize"}, m.ConfigSchema.Required)
	assert.Equal(t, "https://docs.brickkit.io/people-basic/api", m.Metadata.APIDocs)
}

// minimalYAML 只包含必填字段。
const minimalYAML = `
apiVersion: brickkit/v1
kind: Component
metadata:
  id: infra/tool
  name: 工具组件
  version: 1.0.0
  description: 最小组件
deployment:
  type: container
  image: registry.brickkit.io/tool:1.0.0
  port: 8080
healthCheck:
  type: http
  path: /healthz
`

// 4.21–4.25 可选字段全部缺失时解析成功。
func TestParseMinimalManifest(t *testing.T) {
	m, err := Parse([]byte(minimalYAML), "component.yaml")
	require.NoError(t, err)

	assert.Nil(t, m.Dependencies, "4.21 无 dependencies 字段")
	assert.Empty(t, m.Artifacts, "4.22 无 artifacts 字段")
	assert.Empty(t, m.Deployment.ExtraPorts, "4.23 无 extraPorts 字段")
	assert.Nil(t, m.Migration, "4.24 无 migration 字段")
	assert.Nil(t, m.ConfigSchema, "4.25 无 configSchema 字段")
	assert.Nil(t, m.Deployment.Resources)
	assert.Empty(t, m.Tags)
}

// 4.20 空 dependencies 解析成功。
func TestParseEmptyDependencies(t *testing.T) {
	m, err := Parse([]byte(minimalYAML+`
dependencies:
  components: []
  resources: []
`), "component.yaml")
	require.NoError(t, err)

	require.NotNil(t, m.Dependencies)
	assert.Empty(t, m.Dependencies.Components)
	assert.Empty(t, m.Dependencies.Resources)
}

// 4.25 configSchema properties 为空（32.25）也是合法的。
func TestParseEmptyConfigSchemaProperties(t *testing.T) {
	m, err := Parse([]byte(minimalYAML+`
configSchema:
  type: object
  properties: {}
`), "component.yaml")
	require.NoError(t, err)
	require.NotNil(t, m.ConfigSchema)
	assert.Empty(t, m.ConfigSchema.Properties)
}

// ============================================================
// 校验失败（4.2–4.18、4.26–4.30）
// ============================================================

// mutate 基于最小合法 Manifest 做替换，构造非法输入。
func mutate(t *testing.T, old, new string) string {
	t.Helper()
	require.Contains(t, minimalYAML, old, "被替换的片段必须存在于最小 Manifest 中")
	return strings.Replace(minimalYAML, old, new, 1)
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		item     string // 开发计划验证项编号
		name     string
		yaml     string
		contains []string // 错误输出必须包含的片段（字段名 + 原因）
	}{
		{"4.2", "缺少 metadata.id", strings.Replace(minimalYAML, "  id: infra/tool\n", "", 1),
			[]string{"metadata.id", "缺失"}},
		{"4.3", "缺少 metadata.version", strings.Replace(minimalYAML, "  version: 1.0.0\n", "", 1),
			[]string{"metadata.version", "缺失"}},
		{"—", "缺少 metadata.name", strings.Replace(minimalYAML, "  name: 工具组件\n", "", 1),
			[]string{"metadata.name", "缺失"}},
		{"—", "缺少 metadata.description", strings.Replace(minimalYAML, "  description: 最小组件\n", "", 1),
			[]string{"metadata.description", "缺失"}},
		{"4.4", "缺少 deployment.image",
			strings.Replace(minimalYAML, "  image: registry.brickkit.io/tool:1.0.0\n", "", 1),
			[]string{"deployment.image", "缺失"}},
		{"4.5", "缺少 deployment.port", strings.Replace(minimalYAML, "  port: 8080\n", "", 1),
			[]string{"deployment.port", "缺失"}},
		{"4.6", "缺少 healthCheck",
			strings.Replace(minimalYAML, "healthCheck:\n  type: http\n  path: /healthz\n", "", 1),
			[]string{"healthCheck", "缺失"}},
		{"4.7", "版本号格式错误", mutate(t, "version: 1.0.0", `version: "abc"`),
			[]string{"metadata.version", "major.minor.patch"}},
		{"4.8", "版本号缺少 patch", mutate(t, "version: 1.0.0", `version: "1.0"`),
			[]string{"metadata.version", "major.minor.patch"}},
		{"4.16", "deployment.type 非 container", mutate(t, "type: container", "type: static"),
			[]string{"deployment.type", "container"}},
		{"4.26", "apiVersion 非 brickkit/v1", mutate(t, "apiVersion: brickkit/v1", "apiVersion: v2"),
			[]string{"apiVersion", "brickkit/v1"}},
		{"4.27", "kind 非 Component", mutate(t, "kind: Component", "kind: Service"),
			[]string{"kind", "Component"}},
		{"4.28", "组件 ID 含非法字符", mutate(t, "id: infra/tool", `id: "people basic"`),
			[]string{"metadata.id", "scope/name"}},
		{"4.29", "组件 ID 含大写", mutate(t, "id: infra/tool", "id: People/Basic"),
			[]string{"metadata.id", "小写"}},
		{"32.2", "组件 ID 含 @", mutate(t, "id: infra/tool", `id: "a@b/c"`),
			[]string{"metadata.id"}},
		{"32.12", "组件 ID 只有 scope", mutate(t, "id: infra/tool", `id: "people/"`),
			[]string{"metadata.id"}},
		{"32.13", "组件 ID 只有 name", mutate(t, "id: infra/tool", "id: basic"),
			[]string{"metadata.id", "scope/name"}},
		{"32.1", "组件 ID 超长",
			mutate(t, "id: infra/tool", "id: a/"+strings.Repeat("b", 256)),
			[]string{"metadata.id", "长度"}},
		{"32.14", "port 为 0", mutate(t, "port: 8080", "port: 0"),
			[]string{"deployment.port"}},
		{"32.15", "port 为负数", mutate(t, "port: 8080", "port: -1"),
			[]string{"deployment.port", "1~65535"}},
		{"32.16", "port 超过 65535", mutate(t, "port: 8080", "port: 99999"),
			[]string{"deployment.port", "1~65535"}},
		{"4.9", "依赖非精确版本（^）", minimalYAML + `
dependencies:
  components:
    - department/tree@^1.0.0
`, []string{"dependencies.components[0]", "精确版本"}},
		{"4.10", "依赖使用 ~", minimalYAML + `
dependencies:
  components:
    - department/tree@~1.0.0
`, []string{"dependencies.components[0]", "精确版本"}},
		{"—", "依赖缺少 @版本", minimalYAML + `
dependencies:
  components:
    - department/tree
`, []string{"dependencies.components[0]", "<组件ID>@<精确版本>"}},
		{"—", "依赖 ID 非法", minimalYAML + `
dependencies:
  components:
    - Department/Tree@1.0.0
`, []string{"dependencies.components[0]"}},
		{"—", "自依赖", minimalYAML + `
dependencies:
  components:
    - infra/tool@1.0.0
`, []string{"dependencies.components[0]", "自己"}},
		{"—", "资源依赖缺少 kind", minimalYAML + `
dependencies:
  resources:
    - engine: postgresql
`, []string{"dependencies.resources[0].kind", "缺失"}},
		{"—", "资源依赖缺少 engine", minimalYAML + `
dependencies:
  resources:
    - kind: database
`, []string{"dependencies.resources[0].engine", "缺失"}},
		{"4.11", "artifact 缺少 type", minimalYAML + `
artifacts:
  - files: [openapi.json]
`, []string{"artifacts[0].type", "缺失"}},
		{"4.12", "artifact 缺少 files", minimalYAML + `
artifacts:
  - type: api-docs
`, []string{"artifacts[0].files", "缺失"}},
		{"32.23", "artifact files 为空列表", minimalYAML + `
artifacts:
  - type: api-docs
    files: []
`, []string{"artifacts[0].files", "缺失"}},
		{"—", "artifact 文件路径为绝对路径", minimalYAML + `
artifacts:
  - type: api-docs
    files: ["/etc/passwd"]
`, []string{"artifacts[0].files[0]", "相对路径"}},
		{"—", "artifact 文件路径越界", minimalYAML + `
artifacts:
  - type: api-docs
    files: ["../../etc/passwd"]
`, []string{"artifacts[0].files[0]", "仓库根目录"}},
		{"4.13", "extraPorts name 重复", minimalYAML + `
`, nil}, // 占位，见下方独立用例
		{"4.14", "extraPorts 缺少 name", mutate(t, "  port: 8080", `  port: 8080
  extraPorts:
    - port: 9090`), []string{"deployment.extraPorts[0].name", "缺失"}},
		{"4.15", "extraPorts 缺少 port", mutate(t, "  port: 8080", `  port: 8080
  extraPorts:
    - name: grpc`), []string{"deployment.extraPorts[0].port", "缺失"}},
		{"32.17", "extraPort 与主端口相同", mutate(t, "  port: 8080", `  port: 8080
  extraPorts:
    - name: grpc
      port: 8080`), []string{"deployment.extraPorts[0].port", "主端口"}},
		{"—", "extraPort name 非法（K8s 端口名规则）", mutate(t, "  port: 8080", `  port: 8080
  extraPorts:
    - name: GRPC_PORT
      port: 9090`), []string{"deployment.extraPorts[0].name"}},
		{"4.17", "migration.command 非数组", minimalYAML + `
migration:
  command: "python manage.py migrate"
`, []string{"migration.command", "数组"}},
		{"—", "migration.command 为空数组", minimalYAML + `
migration:
  command: []
`, []string{"migration.command", "缺失"}},
		{"4.18", "configSchema type 非 object", minimalYAML + `
configSchema:
  type: array
`, []string{"configSchema.type", "object"}},
		{"4.18", "configSchema 属性类型非法", minimalYAML + `
configSchema:
  type: object
  properties:
    pageSize:
      type: int
`, []string{"configSchema.properties.pageSize.type"}},
		{"4.18", "configSchema required 项未声明", minimalYAML + `
configSchema:
  type: object
  properties:
    pageSize:
      type: integer
  required:
    - notDeclared
`, []string{"configSchema.required", "notDeclared"}},
		{"4.30", "resources 缺少 requests/limits", mutate(t, "  port: 8080", `  port: 8080
  resources: {}`), []string{"deployment.resources", "requests"}},
		{"4.30", "resources.requests 缺少 cpu 与 memory", mutate(t, "  port: 8080", `  port: 8080
  resources:
    requests: {}`), []string{"deployment.resources.requests"}},
		{"—", "healthCheck.type 非法", mutate(t, "  type: http\n  path: /healthz", "  type: grpc"),
			[]string{"healthCheck.type", "http"}},
		{"—", "healthCheck http 缺少 path", mutate(t, "  type: http\n  path: /healthz", "  type: http"),
			[]string{"healthCheck.path", "缺失"}},
		{"—", "healthCheck path 不以 / 开头",
			mutate(t, "  path: /healthz", "  path: healthz"),
			[]string{"healthCheck.path", "/"}},
	}

	for _, c := range cases {
		if c.contains == nil {
			continue // 占位用例
		}
		t.Run(c.item+" "+c.name, func(t *testing.T) {
			m, err := Parse([]byte(c.yaml), "component.yaml")
			require.Error(t, err, "该 Manifest 应校验失败")
			assert.Nil(t, m)

			e := clierr.As(err)
			assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
			out := e.Format()
			for _, want := range c.contains {
				assert.Contains(t, out, want)
			}
		})
	}
}

// 4.13 extraPorts name 重复（两个 name: grpc）。
func TestExtraPortsDuplicateName(t *testing.T) {
	y := mutate(t, "  port: 8080", `  port: 8080
  extraPorts:
    - name: grpc
      port: 9090
    - name: grpc
      port: 9091`)

	_, err := Parse([]byte(y), "component.yaml")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "deployment.extraPorts[1].name")
	assert.Contains(t, clierr.As(err).Format(), "重复")
}

// 一次报出全部问题，而不是每次只报一个。
func TestMultipleProblemsReportedTogether(t *testing.T) {
	y := `
apiVersion: v2
kind: Service
metadata:
  id: People/Basic
  version: "1.0"
deployment:
  type: static
`
	_, err := Parse([]byte(y), "component.yaml")
	require.Error(t, err)

	out := clierr.As(err).Format()
	for _, want := range []string{
		"apiVersion", "kind", "metadata.id", "metadata.version",
		"metadata.name", "metadata.description",
		"deployment.type", "deployment.image", "deployment.port", "healthCheck",
	} {
		assert.Contains(t, out, want)
	}
	assert.Contains(t, out, "component.yaml", "错误应指出来源文件")
}

// ============================================================
// 解析层错误
// ============================================================

// 32.10 非法 YAML 报错并指出行号。
func TestParseInvalidYAML(t *testing.T) {
	_, err := Parse([]byte("apiVersion: brickkit/v1\n  kind: Component\n\tbad: indent\n"), "component.yaml")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
	assert.Contains(t, e.Format(), "line")
}

// 32.11 空文件报错。
func TestParseEmptyFile(t *testing.T) {
	for _, in := range []string{"", "   \n\n", "# 只有注释\n"} {
		_, err := Parse([]byte(in), "component.yaml")
		require.Error(t, err, "输入 %q 应报错", in)
		assert.Contains(t, clierr.As(err).Format(), "为空")
	}
}

func TestParseFileNotExist(t *testing.T) {
	_, err := ParseFile(filepath.Join("testdata", "nope.yaml"))
	require.Error(t, err)

	e := clierr.As(err)
	assert.Contains(t, e.Format(), "component.yaml")
	assert.Contains(t, e.Format(), "不存在")
}

// 错误必须是 CLI 统一错误：非 0 退出码 + 建议。
func TestErrorsAreCLIErrors(t *testing.T) {
	_, err := Parse([]byte("apiVersion: brickkit/v1\nkind: Component\n"), "component.yaml")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.ExitError, e.ExitCode())
	assert.NotEmpty(t, e.Hints, "错误必须带建议（004 §10.2）")
	assert.Contains(t, e.Format(), "❌")
}

// ============================================================
// 边界（Step 32 提前覆盖）
// ============================================================

// 32.3 版本号超长仍可正常处理。
func TestParseVeryLongVersion(t *testing.T) {
	m, err := Parse([]byte(mutate(t, "version: 1.0.0", `version: "999999.999999.999999"`)), "component.yaml")
	require.NoError(t, err)
	assert.Equal(t, "999999.999999.999999", m.Metadata.Version)
}

// 32.5 依赖列表超大（100 个依赖）正常解析。
func TestParseManyDependencies(t *testing.T) {
	var b strings.Builder
	b.WriteString(minimalYAML)
	b.WriteString("\ndependencies:\n  components:\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "    - dep/c%d@1.0.0\n", i)
	}

	m, err := Parse([]byte(b.String()), "component.yaml")
	require.NoError(t, err)
	assert.Len(t, m.Dependencies.Components, 100)
}

// 32.24 artifacts 文件路径含空格属于合法输入。
func TestParseArtifactPathWithSpace(t *testing.T) {
	m, err := Parse([]byte(minimalYAML+`
artifacts:
  - type: api-contract
    files: ["a b/c.proto"]
`), "component.yaml")
	require.NoError(t, err)
	assert.Equal(t, []string{"a b/c.proto"}, m.Artifacts[0].Files)
}

// 前端组件（nginx，port 80，无 migration/依赖）也必须合法（002 §4.3）。
func TestParseFrontendComponent(t *testing.T) {
	m, err := Parse([]byte(`
apiVersion: brickkit/v1
kind: Component
metadata:
  id: portal/user-frontend
  name: 用户前端
  version: 1.0.0
  description: nginx 容器 serve 静态资源
deployment:
  type: container
  image: registry.brickkit.io/portal-user-frontend:1.0.0
  port: 80
healthCheck:
  type: http
  path: /
`), "component.yaml")
	require.NoError(t, err)
	assert.Equal(t, 80, m.Deployment.Port)
	assert.Equal(t, "/", m.HealthCheck.Path)
}

// healthCheck type: tcp / none 也是合法值（002 §9.1）。
func TestParseHealthCheckTypes(t *testing.T) {
	for _, tc := range []struct{ yaml, wantType string }{
		{"  type: tcp", "tcp"},
		{"  type: none", "none"},
	} {
		y := mutate(t, "  type: http\n  path: /healthz", tc.yaml)
		m, err := Parse([]byte(y), "component.yaml")
		require.NoError(t, err, "type=%s 应合法", tc.wantType)
		assert.Equal(t, tc.wantType, m.HealthCheck.Type)
		assert.Empty(t, m.HealthCheck.Path)
	}
}
