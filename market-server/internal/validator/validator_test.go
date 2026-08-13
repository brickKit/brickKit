// 本文件是 Step 18-A「发布校验器」的业务行为测试。
//
// 覆盖开发计划 18.6–18.11 与 007 §18 的全部 Manifest 校验规则。
// 市场的校验是**源头防御**：不合规的组件根本进不来，而不是等 CLI 在使用者机器上报错。
package validator

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/model"
)

// manifestJSON 生成一份合法的 Manifest JSON，overrides 用于按需改写字段。
func manifestJSON(t *testing.T, overrides map[string]any) json.RawMessage {
	t.Helper()
	doc := map[string]any{
		"apiVersion": "brickkit/v1",
		"kind":       "Component",
		"metadata": map[string]any{
			"id":          "people/basic",
			"name":        "基础人员组件",
			"version":     "1.2.0",
			"description": "提供基础人员管理能力",
		},
		"deployment": map[string]any{
			"type":  "container",
			"image": "registry.brickkit.io/people-basic:1.2.0",
			"port":  8080,
		},
		"healthCheck": map[string]any{"type": "http", "path": "/healthz"},
	}
	for k, v := range overrides {
		if v == nil {
			delete(doc, k)
			continue
		}
		doc[k] = v
	}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	return raw
}

// req 生成一份合法的发布请求。
func req(t *testing.T, manifest json.RawMessage, mutate ...func(*model.PublishRequest)) model.PublishRequest {
	t.Helper()
	r := model.PublishRequest{
		Version:    "1.2.0",
		Manifest:   manifest,
		SourceType: model.SourceTypeGit,
		GitURL:     "https://github.com/brickkit/people-basic.git",
	}
	for _, m := range mutate {
		m(&r)
	}
	return r
}

func requireValid(t *testing.T, r model.PublishRequest) *model.Manifest {
	t.Helper()
	m, err := Validate(r)
	require.NoError(t, err, "应校验通过")
	require.NotNil(t, m)
	return m
}

func requireInvalid(t *testing.T, r model.PublishRequest) *model.APIError {
	t.Helper()
	_, err := Validate(r)
	require.Error(t, err, "应校验失败")
	apiErr, ok := err.(*model.APIError)
	require.True(t, ok, "校验失败必须返回 *model.APIError，实际 %T", err)
	return apiErr
}

// ============================================================
// 合法请求
// ============================================================

func TestValidManifestPasses(t *testing.T) {
	m := requireValid(t, req(t, manifestJSON(t, nil)))

	assert.Equal(t, "people/basic", m.Metadata.ID)
	assert.Equal(t, "1.2.0", m.Metadata.Version)
	assert.Equal(t, 8080, m.Deployment.Port)
}

// 007 §12.5 的完整组件（含 artifacts / 依赖 / configSchema / migration / resources）也要通过。
func TestFullManifestPasses(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"tags": []string{"people", "master-data"},
		"artifacts": []any{
			map[string]any{"type": "api-contract", "format": "protobuf", "files": []string{"proto/people/v1/people.proto"}},
			map[string]any{"type": "container", "reference": "registry.brickkit.io/people-basic:1.2.0"},
		},
		"dependencies": map[string]any{
			"components": []any{
				"department/tree@1.0.0",
				map[string]any{"id": "infra/redis-event-bus@1.0.0", "optional": true},
			},
			"resources": []any{map[string]any{"kind": "database", "engine": "postgresql"}},
		},
		"configSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"defaultPageSize": map[string]any{"type": "integer", "default": 20},
			},
		},
		"migration": map[string]any{"command": []string{"python", "manage.py", "migrate"}},
		"deployment": map[string]any{
			"type":  "container",
			"image": "registry.brickkit.io/people-basic:1.2.0",
			"port":  8080,
			"resources": map[string]any{
				"requests": map[string]any{"cpu": "100m", "memory": "128Mi"},
				"limits":   map[string]any{"cpu": "500m", "memory": "512Mi"},
			},
		},
	})

	m := requireValid(t, req(t, raw))
	require.Len(t, m.Artifacts, 2)
	require.NotNil(t, m.Dependencies)
	assert.Len(t, m.Dependencies.Components, 2)
	assert.Equal(t, []string{"python", "manage.py", "migrate"}, m.Migration.Command)
}

// ============================================================
// 18.6 / 18.7 保留变量校验（源头防御，007 §18.1）
// ============================================================

func TestReservedVariableConflictIsRejected(t *testing.T) {
	cases := []struct {
		configKey string
		envVar    string
		pattern   string
	}{
		{"departmentTreeEndpoint", "DEPARTMENT_TREE_ENDPOINT", "*_ENDPOINT"},
		{"componentId", "COMPONENT_ID", "COMPONENT_ID"},
		{"componentVersion", "COMPONENT_VERSION", "COMPONENT_VERSION"},
		{"databaseHost", "DATABASE_HOST", "DATABASE_*"},
		{"redisPort", "REDIS_PORT", "REDIS_*"},
		{"mqUrl", "MQ_URL", "MQ_*"},
		{"storageBucket", "STORAGE_BUCKET", "STORAGE_*"},
		{"searchIndex", "SEARCH_INDEX", "SEARCH_*"},
		{"smtpHost", "SMTP_HOST", "SMTP_*"},
	}

	for _, c := range cases {
		t.Run(c.configKey, func(t *testing.T) {
			raw := manifestJSON(t, map[string]any{
				"configSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{c.configKey: map[string]any{"type": "string"}},
				},
			})

			apiErr := requireInvalid(t, req(t, raw))

			// 007 §18.1 的错误码与详情字段
			assert.Equal(t, model.CodeReservedVariableConflict, apiErr.Code)
			assert.Contains(t, apiErr.Message, "保留变量")

			conflicts := conflictsOf(t, apiErr)
			require.Len(t, conflicts, 1)
			assert.Equal(t, c.configKey, conflicts[0].ConfigKey)
			assert.Equal(t, c.envVar, conflicts[0].EnvVarName)
			assert.Equal(t, c.pattern, conflicts[0].ConflictPattern)
			assert.NotEmpty(t, conflicts[0].Suggestion, "18.7：要给出改名建议")
		})
	}
}

// 18.7 详情里要带组件与版本，且多个冲突一次全报。
func TestReservedVariableConflictDetails(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"configSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"departmentTreeEndpoint": map[string]any{"type": "string"},
				"databaseHost":           map[string]any{"type": "string"},
				"defaultPageSize":        map[string]any{"type": "integer"},
			},
		},
	})

	apiErr := requireInvalid(t, req(t, raw))

	assert.Equal(t, "people/basic", apiErr.Details["componentId"])
	assert.Equal(t, "1.2.0", apiErr.Details["version"])
	conflicts := conflictsOf(t, apiErr)
	assert.Len(t, conflicts, 2, "两个冲突项要一次报全")

	keys := []string{conflicts[0].ConfigKey, conflicts[1].ConfigKey}
	assert.ElementsMatch(t, []string{"departmentTreeEndpoint", "databaseHost"}, keys)
}

// 不冲突的配置项照常通过（007 §18.1 的正例）。
func TestNonConflictingConfigKeysPass(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"configSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"customPageSize":  map[string]any{"type": "integer"},
				"defaultPageSize": map[string]any{"type": "integer"},
				"enableAudit":     map[string]any{"type": "boolean"},
				"endpointTimeout": map[string]any{"type": "integer"},
			},
		},
	})

	requireValid(t, req(t, raw))
}

// ============================================================
// 18.8 / 18.9 / 18.10 闭源组件 API 契约校验（007 §18.3）
// ============================================================

// 18.8 闭源 + 提供 API + 无 api-contract → 拒绝。
func TestClosedSourceWithoutAPIContractIsRejected(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"artifacts": []any{
			map[string]any{"type": "api-docs", "format": "openapi", "files": []string{"openapi.json"}},
		},
	})

	apiErr := requireInvalid(t, req(t, raw, func(r *model.PublishRequest) {
		r.SourceType = model.SourceTypeRegistry
		r.GitURL = ""
	}))

	assert.Equal(t, model.CodeClosedSourceMissingAPIContract, apiErr.Code)
	assert.Equal(t, "people/basic", apiErr.Details["componentId"])
	assert.Contains(t, apiErr.Message, "API 契约")
}

// 18.9 闭源 + 有 api-contract → 通过。
func TestClosedSourceWithAPIContractPasses(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"artifacts": []any{
			map[string]any{"type": "api-contract", "format": "protobuf", "files": []string{"proto/rbac.proto"}},
		},
	})

	requireValid(t, req(t, raw, func(r *model.PublishRequest) {
		r.SourceType = model.SourceTypeRegistry
		r.GitURL = ""
	}))
}

// 18.10 开源组件无 API 契约 → 不强制，通过。
func TestOpenSourceWithoutAPIContractPasses(t *testing.T) {
	requireValid(t, req(t, manifestJSON(t, nil)))
}

// 闭源但不提供 API（没有 port）时也不强制契约。
func TestClosedSourceWithoutPortPasses(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"deployment": map[string]any{
			"type":  "container",
			"image": "registry.brickkit.io/batch-job:1.0.0",
		},
		"healthCheck": map[string]any{"type": "none"},
	})

	requireValid(t, req(t, raw, func(r *model.PublishRequest) {
		r.SourceType = model.SourceTypeRegistry
		r.GitURL = ""
	}))
}

// ============================================================
// 18.11 migration 格式校验
// ============================================================

func TestMigrationCommandMustBeArray(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"migration": map[string]any{"command": "python manage.py migrate"},
	})

	apiErr := requireInvalid(t, req(t, raw))

	assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
	assert.Contains(t, problemsText(t, apiErr), "migration.command")
	assert.Contains(t, problemsText(t, apiErr), "数组")
}

func TestMigrationCommandMustNotBeEmpty(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"migration": map[string]any{"command": []string{}},
	})

	apiErr := requireInvalid(t, req(t, raw))
	assert.Contains(t, problemsText(t, apiErr), "migration.command")
}

// ============================================================
// deployment.resources 格式校验（007 §18）
// ============================================================

func TestDeploymentResourcesFormat(t *testing.T) {
	cases := map[string]any{
		"既无 requests 也无 limits": map[string]any{},
		"requests 既无 cpu 也无 memory": map[string]any{
			"requests": map[string]any{},
		},
	}
	for name, resources := range cases {
		t.Run(name, func(t *testing.T) {
			raw := manifestJSON(t, map[string]any{
				"deployment": map[string]any{
					"type": "container", "image": "img:1", "port": 8080, "resources": resources,
				},
			})

			apiErr := requireInvalid(t, req(t, raw))
			assert.Contains(t, problemsText(t, apiErr), "deployment.resources")
		})
	}
}

// ============================================================
// Manifest 基础字段（007 §18）
// ============================================================

func TestManifestRequiredFields(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]any
		field     string
	}{
		{"apiVersion 非法", map[string]any{"apiVersion": "brickkit/v2"}, "apiVersion"},
		{"kind 非法", map[string]any{"kind": "Service"}, "kind"},
		{"缺 metadata.id", map[string]any{"metadata": map[string]any{
			"name": "x", "version": "1.0.0", "description": "d"}}, "metadata.id"},
		{"id 格式非法", map[string]any{"metadata": map[string]any{
			"id": "peoplebasic", "name": "x", "version": "1.0.0", "description": "d"}}, "metadata.id"},
		{"id 含大写", map[string]any{"metadata": map[string]any{
			"id": "People/Basic", "name": "x", "version": "1.0.0", "description": "d"}}, "metadata.id"},
		{"缺 metadata.name", map[string]any{"metadata": map[string]any{
			"id": "people/basic", "version": "1.0.0", "description": "d"}}, "metadata.name"},
		{"缺 metadata.description", map[string]any{"metadata": map[string]any{
			"id": "people/basic", "name": "x", "version": "1.0.0"}}, "metadata.description"},
		{"版本非精确", map[string]any{"metadata": map[string]any{
			"id": "people/basic", "name": "x", "version": "1.0", "description": "d"}}, "metadata.version"},
		{"deployment.type 非 container", map[string]any{"deployment": map[string]any{
			"type": "static", "image": "img:1", "port": 8080}}, "deployment.type"},
		{"缺 deployment.image", map[string]any{"deployment": map[string]any{
			"type": "container", "port": 8080}}, "deployment.image"},
		{"port 超范围", map[string]any{"deployment": map[string]any{
			"type": "container", "image": "img:1", "port": 70000}}, "deployment.port"},
		{"healthCheck 类型非法", map[string]any{"healthCheck": map[string]any{"type": "ping"}}, "healthCheck.type"},
		{"http 健康检查缺 path", map[string]any{"healthCheck": map[string]any{"type": "http"}}, "healthCheck.path"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			apiErr := requireInvalid(t, req(t, manifestJSON(t, c.overrides)))
			assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
			assert.Contains(t, problemsText(t, apiErr), c.field)
		})
	}
}

// 一次报出全部问题，而不是遇到第一个就返回。
func TestAllProblemsReportedAtOnce(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"apiVersion":  "brickkit/v9",
		"kind":        "Service",
		"healthCheck": map[string]any{"type": "ping"},
	})

	apiErr := requireInvalid(t, req(t, raw))
	problems := problemsOf(t, apiErr)
	assert.GreaterOrEqual(t, len(problems), 3, "至少报出 apiVersion / kind / healthCheck 三个问题")
}

// ============================================================
// artifacts 格式（007 §18.2）
// ============================================================

func TestArtifactFormat(t *testing.T) {
	cases := []struct {
		name     string
		artifact map[string]any
		field    string
	}{
		{"缺 type", map[string]any{"files": []string{"a.proto"}}, "artifacts[0].type"},
		{"非 container 缺 files", map[string]any{"type": "api-contract"}, "artifacts[0].files"},
		{"container 缺 reference", map[string]any{"type": "container"}, "artifacts[0].reference"},
		{"files 为空数组", map[string]any{"type": "api-docs", "files": []string{}}, "artifacts[0].files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := manifestJSON(t, map[string]any{"artifacts": []any{c.artifact}})
			apiErr := requireInvalid(t, req(t, raw))
			assert.Contains(t, problemsText(t, apiErr), c.field)
		})
	}
}

// type 与 format 是自由字符串，市场不校验其取值（002 §2.3）。
func TestArtifactTypeAndFormatAreFreeStrings(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"artifacts": []any{
			map[string]any{"type": "ml-model", "format": "onnx", "files": []string{"model.onnx"}},
			map[string]any{"type": "随便什么类型", "files": []string{"x.bin"}},
		},
	})

	requireValid(t, req(t, raw))
}

// ============================================================
// 依赖与来源类型
// ============================================================

func TestDependencyVersionsMustBeExact(t *testing.T) {
	for _, ref := range []string{"department/tree@^1.0.0", "department/tree@~1.0.0", "department/tree"} {
		t.Run(ref, func(t *testing.T) {
			raw := manifestJSON(t, map[string]any{
				"dependencies": map[string]any{"components": []any{ref}},
			})
			apiErr := requireInvalid(t, req(t, raw))
			assert.Contains(t, problemsText(t, apiErr), "dependencies.components[0]")
		})
	}
}

func TestResourceDependencyRequiresKindAndEngine(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"dependencies": map[string]any{
			"resources": []any{map[string]any{"kind": "database"}},
		},
	})
	apiErr := requireInvalid(t, req(t, raw))
	assert.Contains(t, problemsText(t, apiErr), "dependencies.resources[0].engine")
}

func TestSourceTypeMustBeValid(t *testing.T) {
	apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
		r.SourceType = "svn"
	}))
	assert.Contains(t, problemsText(t, apiErr), "sourceType")
}

// 开源组件必须给出仓库地址（007 §11.1：开源组件必须提供 Git 仓库地址）。
func TestOpenSourceRequiresGitURL(t *testing.T) {
	apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
		r.GitURL = ""
	}))
	assert.Contains(t, problemsText(t, apiErr), "gitUrl")
}

// 请求里的版本号必须与 Manifest 中的版本一致，避免张冠李戴。
func TestRequestVersionMustMatchManifest(t *testing.T) {
	apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
		r.Version = "9.9.9"
	}))
	assert.Contains(t, problemsText(t, apiErr), "version")
}

// ============================================================
// 请求本身的健壮性
// ============================================================

func TestMalformedManifestJSON(t *testing.T) {
	apiErr := requireInvalid(t, req(t, json.RawMessage(`{"apiVersion": `)))
	assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
}

func TestEmptyManifest(t *testing.T) {
	apiErr := requireInvalid(t, req(t, json.RawMessage(`{}`)))
	assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
	assert.NotEmpty(t, problemsOf(t, apiErr))
}

// Manifest 里多写了市场不认识的字段：忽略而不是拒绝（前向兼容）。
func TestUnknownManifestFieldsAreIgnored(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"futureField": map[string]any{"whatever": true},
	})
	requireValid(t, req(t, raw))
}

// ============================================================
// 辅助
// ============================================================

func problemsOf(t *testing.T, e *model.APIError) []model.Problem {
	t.Helper()
	raw, ok := e.Details["problems"]
	if !ok {
		return nil
	}
	problems, ok := raw.([]model.Problem)
	require.True(t, ok, "problems 应为 []model.Problem，实际 %T", raw)
	return problems
}

func problemsText(t *testing.T, e *model.APIError) string {
	t.Helper()
	out := e.Message
	for _, p := range problemsOf(t, e) {
		out += "\n" + p.Field + "：" + p.Reason
	}
	return out
}

func conflictsOf(t *testing.T, e *model.APIError) []model.ReservedConflict {
	t.Helper()
	raw, ok := e.Details["conflicts"]
	require.True(t, ok, "详情里应包含 conflicts")
	conflicts, ok := raw.([]model.ReservedConflict)
	require.True(t, ok, "conflicts 应为 []model.ReservedConflict，实际 %T", raw)
	return conflicts
}
