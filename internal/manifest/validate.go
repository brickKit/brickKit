package manifest

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 组件 ID 规则（002 §10.1、§10.3）：格式 <scope>/<name>，
// 全部小写，只能包含字母、数字、斜杠、中划线。
var componentIDRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?/[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// 精确版本规则（002 §7.1）：major.minor.patch，不接受 ^ / ~ / 范围 / 预发布后缀。
var exactVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// K8s Service 端口名规则（IANA_SVC_NAME）：≤15 字符，小写字母数字与中划线，
// 首尾必须是字母或数字。extraPorts.name 会直接用作 Service 端口名（附录 B.7）。
var portNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

const (
	// MaxComponentIDLen 是组件 ID 长度上限。
	// ID 转换后的版本化服务名要符合 DNS 标签规则（≤63 字符，002 §10.4）。
	MaxComponentIDLen = 63
	// MaxPortNameLen 是 K8s Service 端口名长度上限。
	MaxPortNameLen = 15
	// MinPort / MaxPort 是合法端口范围。
	MinPort = 1
	MaxPort = 65535
)

// configSchemaTypes 是 configSchema 中允许的 JSON Schema 类型。
var configSchemaTypes = map[string]bool{
	"string": true, "integer": true, "number": true,
	"boolean": true, "array": true, "object": true,
}

// newProblems 创建 Manifest 校验用的问题收集器。
func newProblems(source string) *clierr.ProblemSet {
	if source == "" {
		source = FileName
	}
	return clierr.NewProblemSet(clierr.CodeManifestInvalid, "错误："+FileName+" 校验失败").
		WithSource("文件", source).
		WithHint(
			"参考 002 组件规范 §2.2 的 Manifest 完整结构",
			"参考附录 B.1 的完整字段参考",
		)
}

// Validate 校验 Manifest 的全部字段，一次返回所有问题。
func (m *Manifest) Validate() error {
	p := newProblems(m.Source)

	if m.APIVersion == "" {
		p.Missing("apiVersion")
	} else if m.APIVersion != APIVersion {
		p.Addf("apiVersion", "必须是 %s（当前是 %s）", APIVersion, m.APIVersion)
	}
	if m.Kind == "" {
		p.Missing("kind")
	} else if m.Kind != Kind {
		p.Addf("kind", "必须是 %s（当前是 %s）", Kind, m.Kind)
	}

	m.validateMetadata(p)
	m.validateArtifacts(p)
	m.validateDependencies(p)
	m.validateConfigSchema(p)
	m.validateDeployment(p)
	m.validateMigration(p)
	m.validateHealthCheck(p)

	return p.Err()
}

// ComponentIDProblem 返回组件 ID 的不合法原因；合法时返回空字符串。
//
// 组件 ID 规则由 002 §10.3 定义，brickkit.yaml 中的组件条目（Step 5）
// 与 Manifest 中的依赖声明共用同一套规则，因此导出给 config 包复用。
func ComponentIDProblem(id string) string { return componentIDProblem(id) }

// IsExactVersion 判断版本号是否为精确版本 major.minor.patch（002 §7.1）。
func IsExactVersion(version string) bool { return exactVersionRe.MatchString(version) }

// CompareVersions 比较两个精确版本（major.minor.patch），返回 -1 / 0 / 1。
//
// 按**数字**比较，不是按字符串：字符串比较会得出 "10.0.0" < "2.0.0"。
// resolver 的启动顺序与 add 的"取最新版"都依赖它，因此放在 manifest 包里只留一份。
// 版本号的合法性由 Manifest 校验保证；真收到非法输入时退回字符串比较，不 panic。
func CompareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		if aerr != nil || berr != nil {
			return strings.Compare(a, b)
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return len(as) - len(bs)
}

func (m *Manifest) validateMetadata(p *clierr.ProblemSet) {
	if m.Metadata.ID == "" {
		p.Missing("metadata.id")
	} else if reason := componentIDProblem(m.Metadata.ID); reason != "" {
		p.Add("metadata.id", reason)
	}

	if m.Metadata.Name == "" {
		p.Missing("metadata.name")
	}
	if m.Metadata.Description == "" {
		p.Missing("metadata.description")
	}

	if m.Metadata.Version == "" {
		p.Missing("metadata.version")
	} else if !exactVersionRe.MatchString(m.Metadata.Version) {
		p.Addf("metadata.version", "必须是精确版本 major.minor.patch（当前是 %s）", m.Metadata.Version)
	}
}

// componentIDProblem 返回组件 ID 的不合法原因；合法时返回空字符串。
func componentIDProblem(id string) string {
	if len(id) > MaxComponentIDLen {
		return fmt.Sprintf("长度 %d 超过上限 %d（转换后的版本化服务名需符合 DNS 标签规则）",
			len(id), MaxComponentIDLen)
	}
	if hasUpper(id) {
		return "必须全部小写（002 §10.3），格式为 scope/name"
	}
	if !componentIDRe.MatchString(id) {
		return "格式必须为 scope/name，只能包含小写字母、数字与中划线"
	}
	return ""
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func (m *Manifest) validateArtifacts(p *clierr.ProblemSet) {
	for i, a := range m.Artifacts {
		prefix := fmt.Sprintf("artifacts[%d]", i)
		if a.Type == "" {
			p.Missing(prefix + ".type")
		}
		if len(a.Files) == 0 {
			p.Add(prefix+".files", "缺失（必填字段，至少声明一个文件路径）")
			continue
		}
		for j, file := range a.Files {
			field := fmt.Sprintf("%s.files[%d]", prefix, j)
			switch {
			case strings.TrimSpace(file) == "":
				p.Missing(field)
			case filepath.IsAbs(file):
				p.Add(field, "必须是相对路径（相对于组件仓库根目录）")
			case escapesRepoRoot(file):
				p.Add(field, "不能超出组件仓库根目录")
			}
		}
	}
}

// escapesRepoRoot 判断路径是否用 .. 跳出仓库根目录。
func escapesRepoRoot(path string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

func (m *Manifest) validateDependencies(p *clierr.ProblemSet) {
	if m.Dependencies == nil {
		return
	}

	seen := make(map[string]int)
	for i, dep := range m.Dependencies.Components {
		field := fmt.Sprintf("dependencies.components[%d]", i)
		switch {
		case strings.TrimSpace(dep.Ref) == "":
			p.Add(field, "缺失（必填字段，格式为 <组件ID>@<精确版本>）")
			continue
		case dep.Version == "":
			p.Addf(field, "必须声明精确版本，格式为 <组件ID>@<精确版本>（当前是 %s）", dep.Ref)
			continue
		}

		if reason := componentIDProblem(dep.ID); reason != "" {
			p.Addf(field, "组件 ID %s %s", dep.ID, reason)
		}
		if !exactVersionRe.MatchString(dep.Version) {
			p.Addf(field, "版本 %s 必须是精确版本 major.minor.patch，不接受 ^ 或 ~ 等范围约束", dep.Version)
		}
		if dep.ID != "" && dep.ID == m.Metadata.ID {
			p.Add(field, "组件不能依赖自己")
		}
		if prev, ok := seen[dep.Ref]; ok {
			p.Addf(field, "与 dependencies.components[%d] 重复声明了 %s", prev, dep.Ref)
		} else {
			seen[dep.Ref] = i
		}
	}

	for i, res := range m.Dependencies.Resources {
		prefix := fmt.Sprintf("dependencies.resources[%d]", i)
		if res.Kind == "" {
			p.Missing(prefix + ".kind")
		}
		if res.Engine == "" {
			p.Missing(prefix + ".engine")
		}
	}
}

func (m *Manifest) validateConfigSchema(p *clierr.ProblemSet) {
	if m.ConfigSchema == nil {
		return
	}

	if m.ConfigSchema.Type != "" && m.ConfigSchema.Type != "object" {
		p.Addf("configSchema.type", "必须是 object（当前是 %s）", m.ConfigSchema.Type)
	}

	for name, prop := range m.ConfigSchema.Properties {
		field := "configSchema.properties." + name
		switch {
		case prop.Type == "":
			p.Missing(field + ".type")
		case !configSchemaTypes[prop.Type]:
			p.Addf(field+".type", "不是合法的 JSON Schema 类型（允许：string / integer / number / boolean / array / object）")
		}
	}

	for _, name := range m.ConfigSchema.Required {
		if _, ok := m.ConfigSchema.Properties[name]; !ok {
			p.Addf("configSchema.required", "配置项 %s 未在 properties 中声明", name)
		}
	}
}

func (m *Manifest) validateDeployment(p *clierr.ProblemSet) {
	d := m.Deployment

	if d.Type == "" {
		p.Missing("deployment.type")
	} else if d.Type != DeploymentTypeContainer {
		p.Addf("deployment.type", "必须是 %s（所有组件都是 container，包括前端组件）", DeploymentTypeContainer)
	}

	if d.Image == "" {
		p.Missing("deployment.image")
	}

	switch {
	case d.Port == 0:
		p.Missing("deployment.port")
	case d.Port < MinPort || d.Port > MaxPort:
		p.Addf("deployment.port", "必须在 %d~%d 之间（当前是 %d）", MinPort, MaxPort, d.Port)
	}

	names := make(map[string]int)
	for i, ep := range d.ExtraPorts {
		prefix := fmt.Sprintf("deployment.extraPorts[%d]", i)
		switch {
		case ep.Name == "":
			p.Missing(prefix + ".name")
		case len(ep.Name) > MaxPortNameLen || !portNameRe.MatchString(ep.Name):
			p.Addf(prefix+".name", "必须是 %d 字符以内的小写字母、数字与中划线（K8s Service 端口名规则）", MaxPortNameLen)
		}
		if prev, ok := names[ep.Name]; ok && ep.Name != "" {
			p.Addf(prefix+".name", "与 deployment.extraPorts[%d].name 重复", prev)
		} else if ep.Name != "" {
			names[ep.Name] = i
		}

		switch {
		case ep.Port == 0:
			p.Missing(prefix + ".port")
		case ep.Port < MinPort || ep.Port > MaxPort:
			p.Addf(prefix+".port", "必须在 %d~%d 之间（当前是 %d）", MinPort, MaxPort, ep.Port)
		case ep.Port == d.Port:
			p.Addf(prefix+".port", "不能与主端口 deployment.port(%d) 相同", d.Port)
		}
	}

	m.validateResources(p)
}

func (m *Manifest) validateResources(p *clierr.ProblemSet) {
	r := m.Deployment.Resources
	if r == nil {
		return
	}
	if r.Requests == nil && r.Limits == nil {
		p.Add("deployment.resources", "至少要声明 requests 或 limits 之一")
		return
	}
	if r.Requests != nil && r.Requests.CPU == "" && r.Requests.Memory == "" {
		p.Add("deployment.resources.requests", "至少要声明 cpu 或 memory 之一")
	}
	if r.Limits != nil && r.Limits.CPU == "" && r.Limits.Memory == "" {
		p.Add("deployment.resources.limits", "至少要声明 cpu 或 memory 之一")
	}
}

func (m *Manifest) validateMigration(p *clierr.ProblemSet) {
	if m.Migration == nil {
		return
	}
	if len(m.Migration.Command) == 0 {
		p.Add("migration.command", "缺失（必填字段，数组格式，如 [\"python\", \"manage.py\", \"migrate\"]）")
		return
	}
	for i, arg := range m.Migration.Command {
		if strings.TrimSpace(arg) == "" {
			p.Missing(fmt.Sprintf("migration.command[%d]", i))
		}
	}
}

func (m *Manifest) validateHealthCheck(p *clierr.ProblemSet) {
	h := m.HealthCheck

	switch h.Type {
	case "":
		p.Missing("healthCheck.type")
		return
	case HealthCheckHTTP:
		switch {
		case h.Path == "":
			p.Missing("healthCheck.path")
		case !strings.HasPrefix(h.Path, "/"):
			p.Addf("healthCheck.path", "必须以 / 开头（当前是 %s）", h.Path)
		}
	case HealthCheckTCP, HealthCheckNone:
		// tcp / none 不需要 path
	default:
		p.Addf("healthCheck.type", "必须是 %s / %s / %s 之一（当前是 %s）",
			HealthCheckHTTP, HealthCheckTCP, HealthCheckNone, h.Type)
	}
}
