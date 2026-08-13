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
			Reason: "无法解析：" + err.Error(),
		}})
	}

	problems := validateManifest(&m)
	manifestProblems := len(problems)
	problems = append(problems, validateRequest(req, &m)...)
	if len(problems) > 0 {
		if manifestProblems > 0 {
			return nil, manifestError(problems)
		}
		e := model.Errorf(model.CodeInvalidRequest, "发布请求不合法")
		return nil, e.WithDetail("problems", problems)
	}

	if conflicts := ReservedConflicts(m.ConfigSchema); len(conflicts) > 0 {
		// 007 §18.1 的错误结构
		e := model.Errorf(model.CodeReservedVariableConflict, "configSchema 配置项名称与平台保留变量冲突")
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
	e := model.Errorf(model.CodeManifestInvalid, "Manifest 校验失败")
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
		return nil, []model.Problem{{Field: "manifest", Reason: "不是合法的 JSON：" + err.Error()}}
	}
	if len(doc) == 0 {
		return nil, []model.Problem{{Field: "manifest", Reason: "内容为空"}}
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
				Reason: "必须是数组格式",
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
		p = append(p, model.Problem{Field: "apiVersion", Reason: "必须为 brickkit/v1"})
	}
	if m.Kind != "Component" {
		p = append(p, model.Problem{Field: "kind", Reason: "必须为 Component"})
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
		p = append(p, model.Problem{Field: "metadata.id", Reason: "必填"})
	case strings.ToLower(md.ID) != md.ID:
		p = append(p, model.Problem{Field: "metadata.id", Reason: "必须全部小写"})
	case !componentIDRe.MatchString(md.ID):
		p = append(p, model.Problem{Field: "metadata.id", Reason: "格式必须为 scope/name，如 people/basic"})
	}

	if md.Name == "" {
		p = append(p, model.Problem{Field: "metadata.name", Reason: "必填"})
	}
	if md.Description == "" {
		p = append(p, model.Problem{Field: "metadata.description", Reason: "必填"})
	}
	switch {
	case md.Version == "":
		p = append(p, model.Problem{Field: "metadata.version", Reason: "必填"})
	case !exactVersionRe.MatchString(md.Version):
		p = append(p, model.Problem{Field: "metadata.version", Reason: "必须是精确版本 major.minor.patch"})
	}
	return p
}

func validateArtifacts(artifacts []model.Artifact) []model.Problem {
	var p []model.Problem
	for i, a := range artifacts {
		field := indexed("artifacts", i)
		if a.Type == "" {
			p = append(p, model.Problem{Field: field + ".type", Reason: "必填"})
			continue
		}
		// type / format 是自由字符串，市场只校验"该有的字段在不在"（007 §18.2）
		if a.IsContainer() {
			if a.Reference == "" {
				p = append(p, model.Problem{Field: field + ".reference", Reason: "container 类型必须提供镜像地址"})
			}
			continue
		}
		if len(a.Files) == 0 {
			p = append(p, model.Problem{Field: field + ".files", Reason: "必填且不能为空数组"})
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
			p = append(p, model.Problem{Field: field, Reason: "必须写明组件 ID"})
		case !componentIDRe.MatchString(d.ID):
			p = append(p, model.Problem{Field: field, Reason: "组件 ID 格式必须为 scope/name"})
		case d.Version == "":
			p = append(p, model.Problem{Field: field, Reason: "必须写明精确版本，如 " + d.ID + "@1.0.0"})
		case !exactVersionRe.MatchString(d.Version):
			p = append(p, model.Problem{Field: field, Reason: "必须是精确版本 major.minor.patch，不接受 ^ 或 ~ 等范围约束"})
		}
	}

	for i, r := range deps.Resources {
		field := indexed("dependencies.resources", i)
		if r.Kind == "" {
			p = append(p, model.Problem{Field: field + ".kind", Reason: "必填"})
		}
		if r.Engine == "" {
			p = append(p, model.Problem{Field: field + ".engine", Reason: "必填"})
		}
	}
	return p
}

func validateDeployment(d model.Deployment) []model.Problem {
	var p []model.Problem

	if d.Type != "container" {
		p = append(p, model.Problem{Field: "deployment.type", Reason: "必须为 container：所有组件都是容器，包括前端组件"})
	}
	if d.Image == "" {
		p = append(p, model.Problem{Field: "deployment.image", Reason: "必填"})
	}
	if d.Port < 0 || d.Port > 65535 {
		p = append(p, model.Problem{Field: "deployment.port", Reason: "必须在 1–65535 之间"})
	}

	for i, ep := range d.ExtraPorts {
		field := indexed("deployment.extraPorts", i)
		if ep.Name == "" {
			p = append(p, model.Problem{Field: field + ".name", Reason: "必填"})
		}
		if ep.Port <= 0 || ep.Port > 65535 {
			p = append(p, model.Problem{Field: field + ".port", Reason: "必须在 1–65535 之间"})
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
			Reason: "至少要写 requests 或 limits 之一",
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
				Reason: "至少要写 cpu 或 memory 之一",
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
		return []model.Problem{{Field: "migration.command", Reason: "必须是非空数组"}}
	}
	for i, arg := range m.Command {
		if strings.TrimSpace(arg) == "" {
			return []model.Problem{{Field: indexed("migration.command", i), Reason: "不能是空字符串"}}
		}
	}
	return nil
}

func validateHealthCheck(h model.HealthCheck) []model.Problem {
	var p []model.Problem

	switch {
	case h.Type == "":
		p = append(p, model.Problem{Field: "healthCheck.type", Reason: "必填（http / tcp / none）"})
	case !healthCheckTypes[h.Type]:
		p = append(p, model.Problem{Field: "healthCheck.type", Reason: "必须是 http、tcp 或 none 之一"})
	case h.Type == "http" && h.Path == "":
		p = append(p, model.Problem{Field: "healthCheck.path", Reason: "http 健康检查必须提供路径，如 /healthz"})
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
		p = append(p, model.Problem{Field: "configSchema.type", Reason: "必须为 object"})
	}
	for _, key := range cs.Required {
		if _, ok := cs.Properties[key]; !ok {
			p = append(p, model.Problem{
				Field:  "configSchema.required",
				Reason: "required 中的 " + key + " 未在 properties 中声明",
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
			p = append(p, model.Problem{Field: "gitUrl", Reason: "开源组件（sourceType: git）必须提供 Git 仓库地址"})
		}
	case model.SourceTypeRegistry:
	default:
		p = append(p, model.Problem{Field: "sourceType", Reason: "必须是 git 或 registry"})
	}

	if req.Version != "" && m.Metadata.Version != "" && req.Version != m.Metadata.Version {
		p = append(p, model.Problem{
			Field:  "version",
			Reason: "与 Manifest 中的 metadata.version（" + m.Metadata.Version + "）不一致",
		})
	}
	if req.Version == "" {
		p = append(p, model.Problem{Field: "version", Reason: "必填"})
	}

	if req.Visibility != "" &&
		req.Visibility != model.VisibilityPublic && req.Visibility != model.VisibilityPrivate {
		p = append(p, model.Problem{Field: "visibility", Reason: "必须是 public 或 private"})
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

	e := model.Errorf(model.CodeClosedSourceMissingAPIContract, "闭源组件提供 API 时必须上传 API 契约文件")
	return e.
		WithDetail("componentId", m.Metadata.ID).
		WithDetail("version", m.Metadata.Version).
		WithDetail("sourceType", model.SourceTypeRegistry).
		WithDetail("hint", "在 artifacts 中声明至少一个 type: api-contract 的产物（002 §5.11：代码可以闭源，API 契约不能闭源）")
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
