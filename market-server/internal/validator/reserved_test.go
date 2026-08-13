// 本文件是 Step 18-A 校验器的代码层单测：命名转换、保留模式匹配与剩余字段分支。
package validator

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
)

// 004 §5.6：配置项名称转成大写下划线后就是环境变量名。
func TestEnvVarName(t *testing.T) {
	cases := map[string]string{
		"departmentTreeEndpoint": "DEPARTMENT_TREE_ENDPOINT",
		"defaultPageSize":        "DEFAULT_PAGE_SIZE",
		"componentId":            "COMPONENT_ID",
		"default-page-size":      "DEFAULT_PAGE_SIZE",
		"default.page.size":      "DEFAULT_PAGE_SIZE",
		"already_upper":          "ALREADY_UPPER",
		"ALLCAPS":                "ALLCAPS",
		"page2Size":              "PAGE2_SIZE",
		"a":                      "A",
		"":                       "",
	}
	for in, want := range cases {
		assert.Equal(t, want, EnvVarName(in), "输入 %q", in)
	}
}

func TestReservedConflictsWithoutConfigSchema(t *testing.T) {
	assert.Empty(t, ReservedConflicts(nil))
	assert.Empty(t, ReservedConflicts(&model.ConfigSchema{Type: "object"}))
}

// 冲突列表按配置项名称排序，便于测试与排查。
func TestReservedConflictsAreSorted(t *testing.T) {
	cs := &model.ConfigSchema{Properties: map[string]model.ConfigProperty{
		"zzzEndpoint":  {Type: "string"},
		"databaseHost": {Type: "string"},
		"componentId":  {Type: "string"},
	}}

	conflicts := ReservedConflicts(cs)
	require.Len(t, conflicts, 3)
	assert.Equal(t, "componentId", conflicts[0].ConfigKey)
	assert.Equal(t, "databaseHost", conflicts[1].ConfigKey)
	assert.Equal(t, "zzzEndpoint", conflicts[2].ConfigKey)
}

func TestMatchReserved(t *testing.T) {
	cases := []struct {
		envVar  string
		pattern string
		hit     bool
	}{
		{"COMPONENT_ID", "COMPONENT_ID", true},
		{"DEPARTMENT_TREE_ENDPOINT", "*_ENDPOINT", true},
		{"DATABASE_URL", "DATABASE_*", true},
		{"SMTP_HOST", "SMTP_*", true},
		{"ENDPOINT_TIMEOUT", "", false},
		{"MY_DATABASE", "", false},
		{"DEFAULT_PAGE_SIZE", "", false},
	}
	for _, c := range cases {
		pattern, hit := matchReserved(c.envVar)
		assert.Equal(t, c.hit, hit, c.envVar)
		assert.Equal(t, c.pattern, pattern, c.envVar)
	}
}

// ============================================================
// 字段校验的剩余分支
// ============================================================

func TestValidateExtraPorts(t *testing.T) {
	problems := validateDeployment(model.Deployment{
		Type: "container", Image: "img:1", Port: 8080,
		ExtraPorts: []model.ExtraPort{{Name: "", Port: 0}},
	})

	text := fieldsOf(problems)
	assert.Contains(t, text, "deployment.extraPorts[0].name")
	assert.Contains(t, text, "deployment.extraPorts[0].port")
}

func TestValidateResourcesAcceptsPartialSpec(t *testing.T) {
	assert.Empty(t, validateResources(nil))
	assert.Empty(t, validateResources(&model.Resources{
		Limits: &model.ResourceSpec{Memory: "512Mi"},
	}), "只写 limits.memory 是合法的")
}

func TestValidateMigrationRejectsBlankArgument(t *testing.T) {
	problems := validateMigration(&model.Migration{Command: []string{"python", "  "}})
	assert.Contains(t, fieldsOf(problems), "migration.command[1]")
}

func TestValidateConfigSchemaRequiredMustBeDeclared(t *testing.T) {
	problems := validateConfigSchema(&model.ConfigSchema{
		Type:       "object",
		Properties: map[string]model.ConfigProperty{"pageSize": {Type: "integer"}},
		Required:   []string{"pageSize", "missingKey"},
	})

	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Reason, "missingKey")
}

func TestValidateConfigSchemaTypeMustBeObject(t *testing.T) {
	problems := validateConfigSchema(&model.ConfigSchema{Type: "array"})
	assert.Contains(t, fieldsOf(problems), "configSchema.type")
}

func TestValidateHealthCheckTCPNeedsNoPath(t *testing.T) {
	assert.Empty(t, validateHealthCheck(model.HealthCheck{Type: "tcp"}))
	assert.Empty(t, validateHealthCheck(model.HealthCheck{Type: "none"}))
}

func TestValidateDependenciesNil(t *testing.T) {
	assert.Empty(t, validateDependencies(nil))
}

func TestValidateDependencyMissingID(t *testing.T) {
	problems := validateDependencies(&model.Dependencies{
		Components: []model.ComponentDep{{Version: "1.0.0"}},
	})
	assert.Contains(t, fieldsOf(problems), "dependencies.components[0]")
}

// 依赖 ID 写成大写等非法格式时给出格式提示。
func TestValidateDependencyBadID(t *testing.T) {
	problems := validateDependencies(&model.Dependencies{
		Components: []model.ComponentDep{{ID: "Department/Tree", Version: "1.0.0"}},
	})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Reason, "scope/name")
}

// ============================================================
// 形状检查
// ============================================================

func TestDecodeShapeRejectsNonArrayFields(t *testing.T) {
	raw := []byte(`{"apiVersion":"brickkit/v1","tags":"demo","artifacts":{},"dependencies":{"components":"x"}}`)

	_, problems := decodeShape(raw)

	text := fieldsOf(problems)
	assert.Contains(t, text, "tags")
	assert.Contains(t, text, "artifacts")
	assert.Contains(t, text, "dependencies.components")
}

func TestDecodeShapeIgnoresAbsentFields(t *testing.T) {
	_, problems := decodeShape([]byte(`{"apiVersion":"brickkit/v1","migration":null}`))
	assert.Empty(t, problems)
}

func TestLookup(t *testing.T) {
	doc := map[string]any{"migration": map[string]any{"command": []any{"a"}}}

	value, ok := lookup(doc, []string{"migration", "command"})
	assert.True(t, ok)
	assert.NotNil(t, value)

	_, ok = lookup(doc, []string{"migration", "nope"})
	assert.False(t, ok)

	_, ok = lookup(doc, []string{"migration", "command", "deeper"})
	assert.False(t, ok, "标量之下没有更深的路径")
}

// 版本号为空（请求里漏填）时要报出来。
func TestValidateRequestVersionRequired(t *testing.T) {
	problems := validateRequest(model.PublishRequest{
		SourceType: model.SourceTypeRegistry,
	}, &model.Manifest{})
	assert.Contains(t, fieldsOf(problems), "version")
}

func TestValidateRequestVisibility(t *testing.T) {
	problems := validateRequest(model.PublishRequest{
		Version: "1.0.0", SourceType: model.SourceTypeRegistry, Visibility: "secret",
	}, &model.Manifest{Metadata: model.Metadata{Version: "1.0.0"}})
	assert.Contains(t, fieldsOf(problems), "visibility")
}

func TestIndexed(t *testing.T) {
	assert.Equal(t, "artifacts[0]", indexed("artifacts", 0))
	assert.Equal(t, "artifacts[12]", indexed("artifacts", 12))
}

// 完整流程：请求级问题与 Manifest 问题同时存在时，按 Manifest 错误码上报。
func TestManifestProblemsTakePrecedenceOverRequestProblems(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"apiVersion": "brickkit/v9", "kind": "Component"})
	require.NoError(t, err)

	_, err = Validate(model.PublishRequest{
		Version: "1.0.0", Manifest: raw, SourceType: "svn",
	})
	require.Error(t, err)

	apiErr, ok := err.(*model.APIError)
	require.True(t, ok)
	assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
}

func fieldsOf(problems []model.Problem) string {
	out := ""
	for _, p := range problems {
		out += p.Field + "：" + p.Reason + "\n"
	}
	return out
}
