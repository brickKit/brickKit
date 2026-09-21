// Package validator 实现市场的发布校验（007 §18）。
//
// 市场的校验是**源头防御**：不合规的组件根本进不来。它与 CLI 侧的解析校验
// 各自独立实现——两边都成立才叫双保险，任何一边被绕过（比如有人直接调 API）
// 另一边仍然拦得住。
package validator

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/brickkit/market-server/internal/model"
)

// 组件 ID 与精确版本的格式（002 §2.3、§7.1）。
var (
	componentIDRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?/[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	exactVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// 合法的健康检查类型（002 §9.1）。
var healthCheckTypes = map[string]bool{"http": true, "tcp": true, "none": true}

// Validate 校验一次发布请求，通过时返回解析好的 Manifest。
//
// 校验顺序：结构 → 字段（一次报全部）→ 保留变量冲突 → 闭源 API 契约。
// 前一层不过就不进下一层：字段都还没齐的 Manifest，谈配置项命名没有意义。
func Validate(req model.PublishRequest) (*model.Manifest, error) {
	doc, shapeProblems := decodeShape(req.Manifest)
	if len(shapeProblems) > 0 {
		return nil, manifestError(shapeProblems)
	}

	var m model.Manifest
	if err := json.Unmarshal(req.Manifest, &m); err != nil {
		return nil, manifestError([]model.Problem{{
			Field:  "manifest",
			Reason: "could not be parsed: " + err.Error(),
		}})
	}

	problems := validateManifest(&m)
	manifestProblems := len(problems)
	problems = append(problems, validateRequest(req, &m)...)
	if len(problems) > 0 {
		if manifestProblems > 0 {
			return nil, manifestError(problems)
		}
		e := model.Errorf(model.CodeInvalidRequest, "the publish request is invalid")
		return nil, e.WithDetail("problems", problems)
	}

	if conflicts := ReservedConflicts(m.ConfigSchema); len(conflicts) > 0 {
		// 007 §18.1 的错误结构
		e := model.Errorf(model.CodeReservedVariableConflict, "a configSchema item name collides with a platform-reserved variable")
		return nil, e.
			WithDetail("componentId", m.Metadata.ID).
			WithDetail("version", m.Metadata.Version).
			WithDetail("conflicts", conflicts)
	}

	if err := validateClosedSourceContract(req, &m); err != nil {
		return nil, err
	}

	_ = doc
	return &m, nil
}

func manifestError(problems []model.Problem) *model.APIError {
	e := model.Errorf(model.CodeManifestInvalid, "the Manifest failed validation")
	return e.WithDetail("problems", problems)
}

// ============================================================
// 结构检查
// ============================================================

// sequenceFields 是必须写成数组的字段。先做形状检查，才能给出
// "migration.command 必须是数组" 这种精确提示，而不是 json 库的类型报错。
var sequenceFields = [][]string{
	{"tags"},
	{"artifacts"},
	{"migration", "command"},
	{"dependencies", "components"},
	{"dependencies", "resources"},
	{"configSchema", "required"},
}

func decodeShape(raw []byte) (map[string]any, []model.Problem) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, []model.Problem{{Field: "manifest", Reason: "is not valid JSON: " + err.Error()}}
	}
	if len(doc) == 0 {
		return nil, []model.Problem{{Field: "manifest", Reason: "is empty"}}
	}

	var problems []model.Problem
	for _, path := range sequenceFields {
		value, ok := lookup(doc, path)
		if !ok || value == nil {
			continue
		}
		if _, isSlice := value.([]any); !isSlice {
			problems = append(problems, model.Problem{
				Field:  strings.Join(path, "."),
				Reason: "must be an array",
			})
		}
	}
	return doc, problems
}

func lookup(doc map[string]any, path []string) (any, bool) {
	var current any = doc
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// ============================================================
// 字段校验（007 §18）
// ============================================================

func validateManifest(m *model.Manifest) []model.Problem {
	var p []model.Problem

	if m.APIVersion != "brickkit/v1" {
		p = append(p, model.Problem{Field: "apiVersion", Reason: "must be brickkit/v1"})
	}
	if m.Kind != "Component" {
		p = append(p, model.Problem{Field: "kind", Reason: "must be Component"})
	}

	p = append(p, validateMetadata(m.Metadata)...)
	p = append(p, validateArtifacts(m.Artifacts)...)
	p = append(p, validateDependencies(m.Dependencies)...)
	p = append(p, validateDeployment(m.Deployment)...)
	p = append(p, validateMigration(m.Migration)...)
	p = append(p, validateHealthCheck(m.HealthCheck)...)
	p = append(p, validateConfigSchema(m.ConfigSchema)...)
	return p
}

func validateMetadata(md model.Metadata) []model.Problem {
	var p []model.Problem

	switch {
	case md.ID == "":
		p = append(p, model.Problem{Field: "metadata.id", Reason: "is required"})
	case strings.ToLower(md.ID) != md.ID:
		p = append(p, model.Problem{Field: "metadata.id", Reason: "must be all lowercase"})
	case !componentIDRe.MatchString(md.ID):
		p = append(p, model.Problem{Field: "metadata.id", Reason: "must be in the form scope/name, e.g. people/basic"})
	}

	if md.Name == "" {
		p = append(p, model.Problem{Field: "metadata.name", Reason: "is required"})
	}
	if md.Description == "" {
		p = append(p, model.Problem{Field: "metadata.description", Reason: "is required"})
	}
	switch {
	case md.Version == "":
		p = append(p, model.Problem{Field: "metadata.version", Reason: "is required"})
	case !exactVersionRe.MatchString(md.Version):
		p = append(p, model.Problem{Field: "metadata.version", Reason: "must be an exact major.minor.patch version"})
	}
	return p
}

func validateArtifacts(artifacts []model.Artifact) []model.Problem {
	var p []model.Problem
	for i, a := range artifacts {
		field := indexed("artifacts", i)
		if a.Type == "" {
			p = append(p, model.Problem{Field: field + ".type", Reason: "is required"})
			continue
		}
		// type / format 是自由字符串，市场只校验"该有的字段在不在"（007 §18.2）
		if a.IsContainer() {
			if a.Reference == "" {
				p = append(p, model.Problem{Field: field + ".reference", Reason: "a container-type artifact must provide an image reference"})
			}
			continue
		}
		if len(a.Files) == 0 {
			p = append(p, model.Problem{Field: field + ".files", Reason: "is required and must not be an empty array"})
		}
	}
	return p
}

func validateDependencies(deps *model.Dependencies) []model.Problem {
	if deps == nil {
		return nil
	}
	var p []model.Problem

	for i, d := range deps.Components {
		field := indexed("dependencies.components", i)
		switch {
		case d.ID == "":
			p = append(p, model.Problem{Field: field, Reason: "must name a component ID"})
		case !componentIDRe.MatchString(d.ID):
			p = append(p, model.Problem{Field: field, Reason: "component ID must be in the form scope/name"})
		case d.Version == "":
			p = append(p, model.Problem{Field: field, Reason: "must name an exact version, e.g. " + d.ID + "@1.0.0"})
		case !exactVersionRe.MatchString(d.Version):
			p = append(p, model.Problem{Field: field, Reason: "must be an exact major.minor.patch version; range constraints like ^ or ~ are not accepted"})
		}
	}

	for i, r := range deps.Resources {
		field := indexed("dependencies.resources", i)
		if r.Kind == "" {
			p = append(p, model.Problem{Field: field + ".kind", Reason: "is required"})
		}
		if r.Engine == "" {
			p = append(p, model.Problem{Field: field + ".engine", Reason: "is required"})
		}
	}
	return p
}

func validateDeployment(d model.Deployment) []model.Problem {
	var p []model.Problem

	if d.Type != "container" {
		p = append(p, model.Problem{Field: "deployment.type", Reason: "must be container: every component is a container, frontend components included"})
	}
	if d.Image == "" {
		p = append(p, model.Problem{Field: "deployment.image", Reason: "is required"})
	}
	// port 缺失（0）也必须拦下。007 §18 写的是"必须存在，正整数"，而这里
	// 一度写成 `d.Port < 0`：0 就这样溜了过去，连带 validateClosedSourceContract
	// 也被绕开（它以 Port == 0 判断"这个组件不提供 API"），闭源组件少写一个
	// port 就不用交 API 契约了。CLI 侧本来就要求它非零，于是市场收得下、
	// 使用者装不了——错落在装的人身上，而问题在发布的人那里。
	if d.Port < 1 || d.Port > 65535 {
		p = append(p, model.Problem{Field: "deployment.port", Reason: "is required and must be between 1 and 65535"})
	}

	for i, ep := range d.ExtraPorts {
		field := indexed("deployment.extraPorts", i)
		if ep.Name == "" {
			p = append(p, model.Problem{Field: field + ".name", Reason: "is required"})
		}
		if ep.Port <= 0 || ep.Port > 65535 {
			p = append(p, model.Problem{Field: field + ".port", Reason: "must be between 1 and 65535"})
		}
	}

	p = append(p, validateResources(d.Resources)...)
	return p
}

// validateResources 校验推荐资源配额的**格式**。
// 数值是否合理不归市场管（002 §4.6：市场不校验具体数值）。
func validateResources(r *model.Resources) []model.Problem {
	if r == nil {
		return nil
	}
	if r.Requests == nil && r.Limits == nil {
		return []model.Problem{{
			Field:  "deployment.resources",
			Reason: "at least one of requests or limits must be set",
		}}
	}

	var p []model.Problem
	for name, spec := range map[string]*model.ResourceSpec{"requests": r.Requests, "limits": r.Limits} {
		if spec == nil {
			continue
		}
		if spec.CPU == "" && spec.Memory == "" {
			p = append(p, model.Problem{
				Field:  "deployment.resources." + name,
				Reason: "at least one of cpu or memory must be set",
			})
		}
	}
	return p
}

func validateMigration(m *model.Migration) []model.Problem {
	if m == nil {
		return nil
	}
	if len(m.Command) == 0 {
		return []model.Problem{{Field: "migration.command", Reason: "must be a non-empty array"}}
	}
	for i, arg := range m.Command {
		if strings.TrimSpace(arg) == "" {
			return []model.Problem{{Field: indexed("migration.command", i), Reason: "must not be an empty string"}}
		}
	}
	return nil
}

func validateHealthCheck(h model.HealthCheck) []model.Problem {
	var p []model.Problem

	switch {
	case h.Type == "":
		p = append(p, model.Problem{Field: "healthCheck.type", Reason: "is required (http / tcp / none)"})
	case !healthCheckTypes[h.Type]:
		p = append(p, model.Problem{Field: "healthCheck.type", Reason: "must be one of http, tcp, or none"})
	case h.Type == "http" && h.Path == "":
		p = append(p, model.Problem{Field: "healthCheck.path", Reason: "an http health check must provide a path, e.g. /healthz"})
	}
	return p
}

// validateConfigSchema 只校验 configSchema 自身的格式。
// 使用者填的 config 值不归市场管（002 §6.5：它是说明书，不是安检机）。
func validateConfigSchema(cs *model.ConfigSchema) []model.Problem {
	if cs == nil {
		return nil
	}
	var p []model.Problem
	if cs.Type != "" && cs.Type != "object" {
		p = append(p, model.Problem{Field: "configSchema.type", Reason: "must be object"})
	}
	for _, key := range cs.Required {
		if _, ok := cs.Properties[key]; !ok {
			p = append(p, model.Problem{
				Field:  "configSchema.required",
				Reason: key + " in required is not declared in properties",
			})
		}
	}
	return p
}

// validateRequest 校验发布请求本身（不属于 Manifest 的部分）。
func validateRequest(req model.PublishRequest, m *model.Manifest) []model.Problem {
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

	if req.Version != "" && m.Metadata.Version != "" && req.Version != m.Metadata.Version {
		p = append(p, model.Problem{
			Field:  "version",
			Reason: "does not match metadata.version (" + m.Metadata.Version + ") in the Manifest",
		})
	}
	if req.Version == "" {
		p = append(p, model.Problem{Field: "version", Reason: "is required"})
	}

	if req.Visibility != "" &&
		req.Visibility != model.VisibilityPublic && req.Visibility != model.VisibilityPrivate {
		p = append(p, model.Problem{Field: "visibility", Reason: "must be public or private"})
	}
	return p
}

// validateClosedSourceContract 实现 007 §18.3：
// 闭源组件（registry）若提供 API（有 deployment.port），必须带 api-contract 产物。
func validateClosedSourceContract(req model.PublishRequest, m *model.Manifest) error {
	if req.SourceType != model.SourceTypeRegistry || m.Deployment.Port == 0 {
		return nil
	}
	for _, a := range m.Artifacts {
		if a.Type == model.ArtifactTypeAPIContract {
			return nil
		}
	}

	e := model.Errorf(model.CodeClosedSourceMissingAPIContract, "a closed-source component that offers an API must upload an API contract file")
	return e.
		WithDetail("componentId", m.Metadata.ID).
		WithDetail("version", m.Metadata.Version).
		WithDetail("sourceType", model.SourceTypeRegistry).
		WithDetail("hint", "declare at least one artifact with type: api-contract under artifacts (the code may be closed-source; the API contract may not)")
}

func indexed(field string, i int) string {
	return field + "[" + itoa(i) + "]"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
