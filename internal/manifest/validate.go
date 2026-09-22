package manifest

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/runcmd"
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
	return clierr.NewProblemSet(clierr.CodeManifestInvalid, i18n.T(msgid.ProblemValidationFailed, FileName)).
		WithSource(i18n.T(msgid.LabelFile), source).
		WithHint(
			i18n.T(msgid.ManifestHintFieldReference),
		)
}

// Validate 校验 Manifest 的全部字段，一次返回所有问题。
func (m *Manifest) Validate() error {
	p := newProblems(m.Source)

	if m.APIVersion == "" {
		p.Missing("apiVersion")
	} else if m.APIVersion != APIVersion {
		p.Add("apiVersion", i18n.T(msgid.ManifestMustBe, APIVersion, m.APIVersion))
	}
	if m.Kind == "" {
		p.Missing("kind")
	} else if m.Kind != Kind {
		p.Add("kind", i18n.T(msgid.ManifestMustBe, Kind, m.Kind))
	}

	m.validateMetadata(p)
	m.validateArtifacts(p)
	m.validateDependencies(p)
	m.validateConfigSchema(p)
	m.validateDeployment(p)
	m.validateMigration(p)
	m.validateHealthCheck(p)
	m.validateLocal(p)

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
		p.Add("metadata.version", i18n.T(msgid.ManifestVersionNotExact, m.Metadata.Version))
	}
}

// componentIDProblem 返回组件 ID 的不合法原因；合法时返回空字符串。
func componentIDProblem(id string) string {
	if len(id) > MaxComponentIDLen {
		return i18n.T(msgid.ManifestIDTooLong, len(id), MaxComponentIDLen)
	}
	if hasUpper(id) {
		return i18n.T(msgid.ManifestIDMustBeLowercase)
	}
	if !componentIDRe.MatchString(id) {
		return i18n.T(msgid.ManifestIDBadFormat)
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
			p.Add(prefix+".files", i18n.T(msgid.ManifestArtifactFilesMissing))
			continue
		}
		for j, file := range a.Files {
			field := fmt.Sprintf("%s.files[%d]", prefix, j)
			switch {
			case strings.TrimSpace(file) == "":
				p.Missing(field)
			case filepath.IsAbs(file):
				p.Add(field, i18n.T(msgid.ManifestArtifactFileMustBeRelative))
			case escapesRepoRoot(file):
				p.Add(field, i18n.T(msgid.ManifestArtifactFileEscapes))
			}
		}
	}
}

// escapesRepoRoot 判断路径是否用 .. 跳出仓库根目录。
func escapesRepoRoot(path string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

// dependencyClaim 记下"这个组件 ID 先被哪一条依赖占了"。
type dependencyClaim struct {
	index   int
	version string
}

// checkDuplicateDependency 拦下"同一个组件 ID 出现两次"。
//
// # 为什么按组件 ID 去重，而不是按 <ID>@<版本>
//
// 依赖地址的环境变量名**基于组件 ID，不带版本号**（001 §8.3）：
//
//	demo/hello@1.0.0  →  DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:8080
//	demo/hello@2.0.0  →  DEMO_HELLO_ENDPOINT=http://demo-hello-2-0-0:9090
//
// 两条算出来的是**同一个变量名**，而注入引擎按变量名写表
// （inject.envBuilder.set），后写的直接覆盖先写的。放行的后果不是
// "其中一条不生效"，而是：两个容器都起来了、都 healthy、`up` 全绿，
// 而 1.0.0 那个**没有任何人能访问它**——调用方拿到的全是 2.0.0 的地址，
// 连额外端口一起。整个过程零警告。
//
// 这比"报错"难查得多：地址完全合法、连得通、有响应，只是版本不对，
// 表现成"调用成功但行为不符合预期"。
//
// # 这不与"多版本共存"矛盾
//
// 多版本共存是**项目级**能力：brickkit.yaml 里可以同时跑 X@1 与 X@2，
// 供不同的调用方各用各的（002 §3.6）。而单个组件的视角里，
// "我依赖 X 的哪个版本"只能有一个答案——这正是变量名不带版本换来的：
// 组件代码在升级时一个字都不用改。
//
// 菱形依赖（A 依赖 X@1、B 依赖 X@2）不受影响，各拿各的，那是健康的形状。
func checkDuplicateDependency(
	p *clierr.ProblemSet, field string, index int,
	dep ComponentDep, seen map[string]dependencyClaim,
) {
	if dep.ID == "" {
		return
	}

	prev, taken := seen[dep.ID]
	if !taken {
		seen[dep.ID] = dependencyClaim{index: index, version: dep.Version}
		return
	}

	// 完全相同的一条写了两遍：单纯的手误，照原来的说法报
	if prev.version == dep.Version {
		p.Add(field, i18n.T(msgid.ManifestDependencyDuplicate, prev.index, dep.Ref))
		return
	}

	// 分行写：挤成一整行的话，终端里这段话会绕三四圈，
	// 而真正要看的"是哪两个版本"埋在中间
	p.Add(field, i18n.T(msgid.ManifestDependencyTwoVersions, dep.ID, prev.version, prev.index, dep.Ref, EndpointEnvVar(dep.ID)))
}

func (m *Manifest) validateDependencies(p *clierr.ProblemSet) {
	if m.Dependencies == nil {
		return
	}

	// 按**组件 ID** 记，不带版本——理由见 checkDuplicateDependency
	seen := make(map[string]dependencyClaim)
	for i, dep := range m.Dependencies.Components {
		field := fmt.Sprintf("dependencies.components[%d]", i)
		switch {
		case strings.TrimSpace(dep.Ref) == "":
			p.Add(field, i18n.T(msgid.ManifestDependencyMissing))
			continue
		case dep.Version == "":
			p.Add(field, i18n.T(msgid.ManifestDependencyNoVersion, dep.Ref))
			continue
		}

		if reason := componentIDProblem(dep.ID); reason != "" {
			p.Add(field, i18n.T(msgid.ManifestDependencyIDInvalid, dep.ID, reason))
		}
		if !exactVersionRe.MatchString(dep.Version) {
			p.Add(field, i18n.T(msgid.ManifestDependencyVersionNotExact, dep.Version))
		}
		if dep.ID != "" && dep.ID == m.Metadata.ID {
			p.Add(field, i18n.T(msgid.ManifestDependencyOnSelf))
		}
		checkDuplicateDependency(p, field, i, dep, seen)
	}

	for i, res := range m.Dependencies.Resources {
		prefix := fmt.Sprintf("dependencies.resources[%d]", i)
		switch {
		case res.Kind == "":
			p.Missing(prefix + ".kind")
		case !IsKnownResourceKind(res.Kind):
			// 与 brickkit.yaml 侧同一条规则：kind 是按字符串比对的，
			// 组件写了平台不认识的类型，使用者照着绑也换不来任何连接变量
			p.Add(prefix+".kind", i18n.T(msgid.ProblemResourceKindUnknown, res.Kind, ResourceKindsText()))
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
		p.Add("configSchema.type", i18n.T(msgid.ManifestMustBe, "object", m.ConfigSchema.Type))
	}

	for name, prop := range m.ConfigSchema.Properties {
		field := "configSchema.properties." + name
		switch {
		case prop.Type == "":
			p.Missing(field + ".type")
		case !configSchemaTypes[prop.Type]:
			p.Add(field+".type", i18n.T(msgid.ManifestConfigTypeInvalid))
		}
	}

	for _, name := range m.ConfigSchema.Required {
		if _, ok := m.ConfigSchema.Properties[name]; !ok {
			p.Add("configSchema.required", i18n.T(msgid.ManifestConfigRequiredNotDeclared, name))
		}
	}
}

func (m *Manifest) validateDeployment(p *clierr.ProblemSet) {
	d := m.Deployment

	if d.Type == "" {
		p.Missing("deployment.type")
	} else if d.Type != DeploymentTypeContainer {
		p.Add("deployment.type", i18n.T(msgid.ManifestDeploymentTypeMustBe, DeploymentTypeContainer))
	}

	if d.Image == "" {
		p.Missing("deployment.image")
	}

	switch {
	case d.Port == 0:
		p.Missing("deployment.port")
	case d.Port < MinPort || d.Port > MaxPort:
		p.Add("deployment.port", i18n.T(msgid.ProblemPortOutOfRange, MinPort, MaxPort, d.Port))
	}

	names := make(map[string]int)
	for i, ep := range d.ExtraPorts {
		prefix := fmt.Sprintf("deployment.extraPorts[%d]", i)
		switch {
		case ep.Name == "":
			p.Missing(prefix + ".name")
		case len(ep.Name) > MaxPortNameLen || !portNameRe.MatchString(ep.Name):
			p.Add(prefix+".name", i18n.T(msgid.ManifestPortNameInvalid, MaxPortNameLen))
		}
		if prev, ok := names[ep.Name]; ok && ep.Name != "" {
			p.Add(prefix+".name", i18n.T(msgid.ManifestPortNameDuplicate, prev))
		} else if ep.Name != "" {
			names[ep.Name] = i
		}

		switch {
		case ep.Port == 0:
			p.Missing(prefix + ".port")
		case ep.Port < MinPort || ep.Port > MaxPort:
			p.Add(prefix+".port", i18n.T(msgid.ProblemPortOutOfRange, MinPort, MaxPort, ep.Port))
		case ep.Port == d.Port:
			p.Add(prefix+".port", i18n.T(msgid.ManifestPortSameAsMain, d.Port))
		}
	}

	m.validateResources(p)
	ValidateLabels(d.Labels, "deployment.labels", p.Add)
}

func (m *Manifest) validateResources(p *clierr.ProblemSet) {
	r := m.Deployment.Resources
	if r == nil {
		return
	}
	if r.Requests == nil && r.Limits == nil {
		p.Add("deployment.resources", i18n.T(msgid.ManifestResourcesNeedOne))
		return
	}
	if r.Requests != nil && r.Requests.CPU == "" && r.Requests.Memory == "" {
		p.Add("deployment.resources.requests", i18n.T(msgid.ManifestResourcesNeedCPUOrMemory))
	}
	if r.Limits != nil && r.Limits.CPU == "" && r.Limits.Memory == "" {
		p.Add("deployment.resources.limits", i18n.T(msgid.ManifestResourcesNeedCPUOrMemory))
	}
}

func (m *Manifest) validateMigration(p *clierr.ProblemSet) {
	if m.Migration == nil {
		return
	}
	if len(m.Migration.Command) == 0 {
		p.Add("migration.command", i18n.T(msgid.ManifestMigrationCommandMissing))
		return
	}
	for i, arg := range m.Migration.Command {
		if strings.TrimSpace(arg) == "" {
			p.Missing(fmt.Sprintf("migration.command[%d]", i))
		}
	}
}

func (m *Manifest) validateLocal(p *clierr.ProblemSet) {
	if m.Local == nil {
		return
	}
	if m.Local.Language != "" && !slices.Contains(runcmd.Languages(), m.Local.Language) {
		p.Add("local.language", i18n.T(msgid.ManifestLocalLanguageInvalid,
			m.Local.Language, strings.Join(runcmd.Languages(), "/")))
	}
	for i, arg := range m.Local.RunCommand {
		if strings.TrimSpace(arg) == "" {
			p.Missing(fmt.Sprintf("local.runCommand[%d]", i))
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
			p.Add("healthCheck.path", i18n.T(msgid.ManifestHealthPathMustStartWithSlash, h.Path))
		}
	case HealthCheckTCP, HealthCheckNone:
		// tcp / none 不需要 path
	default:
		p.Add("healthCheck.type", i18n.T(msgid.ProblemMustBeOneOfThree, HealthCheckHTTP, HealthCheckTCP, HealthCheckNone, h.Type))
	}

	validateStartPeriod(p, h)
}

// maxStartPeriodSeconds 是启动宽限期的上限（1 小时）。
//
// 有上限不是怕数值大——宽限期长本身没有危害。是怕**手滑写成了毫秒**：
// `startPeriodSeconds: 60000` 看着像"60 秒"，实际是 16 小时，
// 于是一个真的起不来的组件会一直挂在 starting 上，谁也不会去看它。
const maxStartPeriodSeconds = 3600

// validateStartPeriod 校验启动宽限期（002 §9.3）。
func validateStartPeriod(p *clierr.ProblemSet, h HealthCheck) {
	if h.StartPeriodSeconds == 0 {
		return // 没写 = 用默认值
	}

	switch {
	case h.Type == HealthCheckNone:
		// 与 brickkit.yaml 侧 localPort / exposePort 同一条规矩：
		// 写了不生效的字段必须出声，否则使用者以为自己调过了
		p.Add("healthCheck.startPeriodSeconds", i18n.T(msgid.ManifestStartPeriodIgnoredForNone))
	case h.StartPeriodSeconds < 0:
		p.Add("healthCheck.startPeriodSeconds", i18n.T(msgid.ManifestStartPeriodMustBePositive, h.StartPeriodSeconds))
	case h.StartPeriodSeconds > maxStartPeriodSeconds:
		p.Add("healthCheck.startPeriodSeconds",
			i18n.T(msgid.ManifestStartPeriodTooLarge, maxStartPeriodSeconds, h.StartPeriodSeconds))
	}
}
