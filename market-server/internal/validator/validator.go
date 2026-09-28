// Package validator 实现市场的发布校验。
//
// Manifest 的规则就是 CLI 的 internal/manifest：同一份代码，不是两份各自维护的拷贝。
// 服务端对每个请求自己做校验，所以绕过 CLI 直接调 API 的请求同样被拦下——这道防线
// 靠的是"服务端校验"，不靠"两份实现"。两份实现的实际结果是各自漂移。
//
// 市场在 Manifest 规则之上只加发布请求自己的规则：
//   - 市场版本必须有 deployment.image（装的人手上没有源码可以构建）
//   - configSchema 的键不许撞上平台保留变量（CLI 注入时警告并跳过，市场直接拒收）
//   - 来源、仓库地址、版本号、可见性
//   - 闭源组件提供 API 时必须带 api-contract 产物
//
// 规则的文字来自 CLI 的 i18n 目录；市场从不设置语言，于是始终是英文。
package validator

import (
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"

	"github.com/brickkit/brickkit/market-server/internal/model"
)

// manifestSource 是 manifest.Parse 错误里的来源名：请求体里的 manifest 字段。
const manifestSource = "manifest"

// Validate 校验一次发布请求，通过时返回解析好的 Manifest。
//
// 顺序：Manifest 与请求（一次报全部）→ 保留变量冲突 → 闭源 API 契约。
// 前一层不过就不进下一层：字段都还没齐的 Manifest，谈配置项命名没有意义。
func Validate(req model.PublishRequest) (*manifest.Manifest, error) {
	m, problems := parseManifest(req)
	manifestProblems := len(problems)
	problems = append(problems, validateRequest(req, m)...)
	if len(problems) > 0 {
		if manifestProblems > 0 {
			return nil, model.Errorf(model.CodeManifestInvalid, "the Manifest failed validation").
				WithDetail("problems", problems)
		}
		return nil, model.Errorf(model.CodeInvalidRequest, "the publish request is invalid").
			WithDetail("problems", problems)
	}

	if hits := inject.ReservedHits(m); len(hits) > 0 {
		conflicts := make([]model.ReservedConflict, 0, len(hits))
		for _, h := range hits {
			conflicts = append(conflicts, model.ReservedConflict{
				ConfigKey: h.Key, ConflictPattern: h.Pattern, Suggestion: h.Suggestion,
			})
		}
		return nil, model.Errorf(model.CodeReservedVariableConflict,
			"a configSchema item name collides with a platform-reserved variable").
			WithDetail("componentId", m.Metadata.ID).
			WithDetail("version", m.Metadata.Version).
			WithDetail("conflicts", conflicts)
	}

	if err := validateClosedSourceContract(req, m); err != nil {
		return nil, err
	}
	return m, nil
}

// parseManifest 用 CLI 的解析器读发布的 Manifest（JSON 就是合法的 YAML），再加上
// 市场版本必须有镜像这一条。解析失败时 m 为 nil。
func parseManifest(req model.PublishRequest) (*manifest.Manifest, []model.Problem) {
	m, err := manifest.Parse(req.Manifest, manifestSource)
	if err != nil {
		return nil, cliProblems(err)
	}
	if m.Deployment.Image == "" {
		return m, []model.Problem{{
			Field: "deployment.image",
			Reason: "is required for a market release: whoever installs from the market has no source to build from. " +
				"A component that is only built from source (deployment.build) is distributed through a git source",
		}}
	}
	return m, nil
}

// cliProblems 把 CLI 的校验错误逐条转成 {field, reason}。没有逐字段问题的错误
// （YAML 语法错、整份为空）作为整份 manifest 的一条问题。
func cliProblems(err error) []model.Problem {
	e := clierr.As(err)
	if len(e.Problems) > 0 {
		out := make([]model.Problem, 0, len(e.Problems))
		for _, p := range e.Problems {
			field := p.Field
			if field == manifest.FileName {
				// CLI 把"整份文档"的问题记在文件名下；在市场里整份文档就是 manifest 字段
				field = manifestSource
			}
			out = append(out, model.Problem{Field: field, Reason: p.Reason})
		}
		return out
	}
	reason := strings.TrimSpace(e.Message)
	for _, d := range e.Details {
		if d.Value != manifestSource {
			reason += "; " + d.Key + ": " + d.Value
		}
	}
	return []model.Problem{{Field: manifestSource, Reason: reason}}
}

// validateRequest 校验发布请求本身（不属于 Manifest 的部分）。m 为 nil 时跳过与它比对的那一条。
func validateRequest(req model.PublishRequest, m *manifest.Manifest) []model.Problem {
	var p []model.Problem

	switch req.SourceType {
	case model.SourceTypeGit:
		if strings.TrimSpace(req.GitURL) == "" {
			p = append(p, model.Problem{Field: "gitUrl", Reason: "an open-source component (sourceType: git) must provide a Git repository URL"})
		}
	case model.SourceTypeRegistry:
	default:
		p = append(p, model.Problem{Field: "sourceType", Reason: "must be git or registry"})
	}

	switch {
	case req.Version == "":
		p = append(p, model.Problem{Field: "version", Reason: "is required"})
	case m != nil && req.Version != m.Metadata.Version:
		p = append(p, model.Problem{
			Field:  "version",
			Reason: "does not match metadata.version (" + m.Metadata.Version + ") in the Manifest",
		})
	}

	if req.Visibility != "" &&
		req.Visibility != model.VisibilityPublic && req.Visibility != model.VisibilityPrivate {
		p = append(p, model.Problem{Field: "visibility", Reason: "must be public or private"})
	}
	return p
}

// validateClosedSourceContract：闭源组件（registry）提供 API，就必须带 api-contract 产物——
// 代码可以闭源，接口契约不可以。deployment.port 是必填的，所以每个组件都提供 API。
func validateClosedSourceContract(req model.PublishRequest, m *manifest.Manifest) error {
	if req.SourceType != model.SourceTypeRegistry {
		return nil
	}
	for _, a := range m.Artifacts {
		if a.Type == model.ArtifactTypeAPIContract {
			return nil
		}
	}
	return model.Errorf(model.CodeClosedSourceMissingAPIContract, "a closed-source component that offers an API must upload an API contract file").
		WithDetail("componentId", m.Metadata.ID).
		WithDetail("version", m.Metadata.Version).
		WithDetail("sourceType", model.SourceTypeRegistry).
		WithDetail("hint", "declare at least one artifact with type: api-contract under artifacts (the code may be closed-source; the API contract may not)")
}
