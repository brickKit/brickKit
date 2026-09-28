// 本文件测市场的发布校验：Manifest 规则就是 CLI 的 internal/manifest（同一份代码），
// 市场只在上面加发布请求自己的规则——市场版本必须有镜像、配置项不许撞保留变量、
// 可见性与来源、闭源组件的 API 契约。
package validator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/manifest"

	"github.com/brickkit/brickkit/market-server/internal/model"
)

// manifestJSON 生成一份合法的 Manifest JSON，overrides 按顶层键改写（nil 表示删掉）。
func manifestJSON(t *testing.T, overrides map[string]any) json.RawMessage {
	t.Helper()
	doc := map[string]any{
		"apiVersion": "brickkit/v1",
		"kind":       "Component",
		"metadata": map[string]any{
			"id":          "people/basic",
			"name":        "People",
			"version":     "1.2.0",
			"description": "Basic people management",
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

func closedSource(r *model.PublishRequest) {
	r.SourceType = model.SourceTypeRegistry
	r.GitURL = ""
}

func requireValid(t *testing.T, r model.PublishRequest) *manifest.Manifest {
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
// 三层模型的 Manifest 收得下
// ============================================================

func TestValidateAcceptsThreeLayerManifest(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"configSchema": map[string]any{
			"properties": map[string]any{
				"DB_HOST": map[string]any{"type": "string", "description": "database host"},
			},
			"required": []string{"DB_HOST"},
		},
		"deployment": map[string]any{
			"type":  "container",
			"image": "registry.brickkit.io/people-basic:1.2.0",
			"build": map[string]any{"context": "."},
			"port":  8080,
		},
		"local": map[string]any{"runCommand": []string{"./bin/server"}},
		"shell": map[string]any{"members": []string{"people/profile@1.0.0"}},
	})

	m := requireValid(t, req(t, raw))
	assert.Equal(t, "people/basic", m.Metadata.ID)
	assert.Equal(t, "1.2.0", m.Metadata.Version)
	assert.True(t, m.IsShell())
	assert.Contains(t, m.ConfigSchema.Properties, "DB_HOST")
}

// ============================================================
// CLI 拒收的，市场同样拒收，而且逐条给出 {field, reason}
// ============================================================

func TestValidateRejectsWhatTheCLIRejects(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]any
		field     string
	}{
		{"unknown field", map[string]any{"futureField": true}, "futureField"},
		{"range dependency", map[string]any{
			"dependencies": map[string]any{"components": []string{"department/tree@^1.0.0"}},
		}, "dependencies.components[0]"},
		{"type mismatch", map[string]any{
			"deployment": map[string]any{"type": "container", "image": "r/p:1", "port": "abc"},
		}, "deployment.port"},
		{"bare member", map[string]any{
			"shell": map[string]any{"members": []string{"people/profile"}},
		}, "shell.members[0]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := manifestJSON(t, c.overrides)
			_, cliErr := manifest.Parse(raw, "manifest")
			require.Error(t, cliErr, "前提：CLI 自己就拒收这一份")

			apiErr := requireInvalid(t, req(t, raw))
			assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
			problems := problemsOf(t, apiErr)
			require.NotEmpty(t, problems)
			assert.Contains(t, fields(problems), c.field, "字段路径就是 manifest.Parse 报的那一个")
			for _, p := range problems {
				assert.NotEmpty(t, p.Reason, "每一条问题都有原因：%s", p.Field)
			}
		})
	}
}

// 一次报全部问题：每一条是单独的 {field, reason}，不是拼成一段的文字。
func TestValidateReportsEachProblemSeparately(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"metadata":    map[string]any{"id": "people/basic", "version": "1.2.0"},
		"healthCheck": map[string]any{"type": "http"},
	})
	apiErr := requireInvalid(t, req(t, raw))
	got := fields(problemsOf(t, apiErr))
	assert.Contains(t, got, "metadata.name")
	assert.Contains(t, got, "metadata.description")
	assert.Contains(t, got, "healthCheck.path")
}

// YAML/JSON 本身就读不懂时没有逐字段的问题：整份 Manifest 作为一条问题。
func TestValidateUnparsableManifest(t *testing.T) {
	for _, raw := range []string{`{"apiVersion": `, ``, `[]`} {
		apiErr := requireInvalid(t, req(t, json.RawMessage(raw)))
		assert.Equal(t, model.CodeManifestInvalid, apiErr.Code, raw)
		problems := problemsOf(t, apiErr)
		require.Len(t, problems, 1, raw)
		assert.Equal(t, "manifest", problems[0].Field)
		assert.NotEmpty(t, problems[0].Reason)
	}
}

// 规则的文字是英文：市场只说英文，不跟着任何机器的语言设置走。
func TestValidateSpeaksEnglish(t *testing.T) {
	raw := manifestJSON(t, map[string]any{"futureField": true})
	apiErr := requireInvalid(t, req(t, raw))
	for _, p := range problemsOf(t, apiErr) {
		for _, r := range p.Reason {
			assert.Less(t, r, rune(0x2E80), "原因里出现了中文：%q", p.Reason)
		}
	}
}

// ============================================================
// 市场自己的规则
// ============================================================

// 从市场装组件的人手上没有源码，没法 brickkit build：只能构建的组件走 git 分发。
func TestValidateRequiresImageForTheMarket(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"deployment": map[string]any{
			"type":  "container",
			"build": map[string]any{"context": "."},
			"port":  8080,
		},
	})
	_, cliErr := manifest.Parse(raw, "manifest")
	require.NoError(t, cliErr, "前提：只写 build 的 Manifest 对 CLI 是合法的")

	apiErr := requireInvalid(t, req(t, raw))
	assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
	problems := problemsOf(t, apiErr)
	require.Len(t, problems, 1)
	assert.Equal(t, "deployment.image", problems[0].Field)
	assert.Contains(t, problems[0].Reason, "git")
}

// AGENTS §5.2：配置项撞上保留变量，CLI 注入时警告并跳过，市场发布时直接拒收。
func TestValidateRefusesReservedKey(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"configSchema": map[string]any{
			"properties": map[string]any{
				"PORT":       map[string]any{"type": "integer"},
				"X_ENDPOINT": map[string]any{"type": "string"},
				"PAGE_SIZE":  map[string]any{"type": "integer"},
			},
		},
	})
	apiErr := requireInvalid(t, req(t, raw))
	assert.Equal(t, model.CodeReservedVariableConflict, apiErr.Code)
	assert.Equal(t, "people/basic", apiErr.Details["componentId"])
	assert.Equal(t, []model.ReservedConflict{
		{ConfigKey: "PORT", ConflictPattern: "PORT", Suggestion: "CUSTOM_PORT"},
		{ConfigKey: "X_ENDPOINT", ConflictPattern: "*_ENDPOINT", Suggestion: "X_BASE_URL"},
	}, conflictsOf(t, apiErr))
}

// 资源废除之后，DATABASE_* 这类名字就是普通的配置项。
func TestValidateAcceptsFormerResourcePrefixes(t *testing.T) {
	raw := manifestJSON(t, map[string]any{
		"configSchema": map[string]any{
			"properties": map[string]any{
				"DATABASE_URL": map[string]any{"type": "string"},
				"REDIS_HOST":   map[string]any{"type": "string"},
			},
		},
	})
	requireValid(t, req(t, raw))
}

func TestValidateKeepsRequestRules(t *testing.T) {
	t.Run("visibility", func(t *testing.T) {
		apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
			r.Visibility = "secret"
		}))
		assert.Equal(t, model.CodeInvalidRequest, apiErr.Code)
		assert.Contains(t, fields(problemsOf(t, apiErr)), "visibility")
	})
	t.Run("source type", func(t *testing.T) {
		apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
			r.SourceType = "svn"
		}))
		assert.Contains(t, fields(problemsOf(t, apiErr)), "sourceType")
	})
	t.Run("open source needs a git url", func(t *testing.T) {
		apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
			r.GitURL = ""
		}))
		assert.Contains(t, fields(problemsOf(t, apiErr)), "gitUrl")
	})
	t.Run("version matches the manifest", func(t *testing.T) {
		apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
			r.Version = "9.9.9"
		}))
		assert.Contains(t, fields(problemsOf(t, apiErr)), "version")
	})
	t.Run("version is required", func(t *testing.T) {
		apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
			r.Version = ""
		}))
		assert.Contains(t, fields(problemsOf(t, apiErr)), "version")
	})
	t.Run("closed source without an api contract", func(t *testing.T) {
		raw := manifestJSON(t, map[string]any{
			"artifacts": []any{
				map[string]any{"type": "api-docs", "format": "openapi", "files": []string{"openapi.json"}},
			},
		})
		apiErr := requireInvalid(t, req(t, raw, closedSource))
		assert.Equal(t, model.CodeClosedSourceMissingAPIContract, apiErr.Code)
		assert.Equal(t, "people/basic", apiErr.Details["componentId"])
	})
	t.Run("closed source with an api contract", func(t *testing.T) {
		raw := manifestJSON(t, map[string]any{
			"artifacts": []any{
				map[string]any{"type": "api-contract", "format": "protobuf", "files": []string{"proto/rbac.proto"}},
			},
		})
		requireValid(t, req(t, raw, closedSource))
	})
	t.Run("open source needs no api contract", func(t *testing.T) {
		requireValid(t, req(t, manifestJSON(t, nil)))
	})
}

// Manifest 的问题与请求的问题一起报：发布者改一轮就能改完。
func TestValidateReportsManifestAndRequestProblemsTogether(t *testing.T) {
	raw := manifestJSON(t, map[string]any{"futureField": true})
	apiErr := requireInvalid(t, req(t, raw, func(r *model.PublishRequest) { r.Visibility = "secret" }))
	assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
	got := fields(problemsOf(t, apiErr))
	assert.Contains(t, got, "futureField")
	assert.Contains(t, got, "visibility")
}

// 组件文档的上限由服务端自己把关（绕过 CLI 的请求同样拦下），而且与别的问题一起报。
func TestValidateRefusesOversizedDoc(t *testing.T) {
	apiErr := requireInvalid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
		r.Doc = strings.Repeat("x", manifest.MaxDocBytes+1)
	}))
	assert.Equal(t, model.CodeInvalidRequest, apiErr.Code)
	assert.Equal(t, []string{"doc"}, fields(problemsOf(t, apiErr)))

	requireValid(t, req(t, manifestJSON(t, nil), func(r *model.PublishRequest) {
		r.Doc = strings.Repeat("x", manifest.MaxDocBytes)
	}))
}

func TestValidateReportsDocWithManifestProblems(t *testing.T) {
	apiErr := requireInvalid(t, req(t, manifestJSON(t, map[string]any{"futureField": true}), func(r *model.PublishRequest) {
		r.Doc = strings.Repeat("x", manifest.MaxDocBytes+1)
	}))
	assert.Equal(t, model.CodeManifestInvalid, apiErr.Code)
	got := fields(problemsOf(t, apiErr))
	assert.Contains(t, got, "futureField")
	assert.Contains(t, got, "doc")
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

func fields(problems []model.Problem) []string {
	out := make([]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.Field)
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
