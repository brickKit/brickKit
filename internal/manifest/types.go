// Package manifest 负责组件 Manifest（component.yaml）的解析与校验。
//
// 设计依据：002 组件规范 §2（Manifest 结构）、§3（依赖）、§4（部署形态）、
// §8（迁移）、§9（健康检查）、§10（组件 ID 命名），附录 B（完整字段参考）。
package manifest

// APIVersion 与 Kind 的固定值（002 §2.2）。
const (
	APIVersion = "brickkit/v1"
	Kind       = "Component"
)

// DeploymentTypeContainer 是唯一合法的部署类型：
// 所有组件都是 container，包括前端组件（002 §4.1）。
const DeploymentTypeContainer = "container"

// 健康检查类型（002 §9.1）。
const (
	HealthCheckHTTP = "http"
	HealthCheckTCP  = "tcp"
	HealthCheckNone = "none"
)

// Manifest 是 component.yaml 的完整结构。
type Manifest struct {
	APIVersion    string         `yaml:"apiVersion"`
	Kind          string         `yaml:"kind"`
	Metadata      Metadata       `yaml:"metadata"`
	Tags          []string       `yaml:"tags,omitempty"`
	Artifacts     []Artifact     `yaml:"artifacts,omitempty"`
	Dependencies  *Dependencies  `yaml:"dependencies,omitempty"`
	ConfigSchema  *ConfigSchema  `yaml:"configSchema,omitempty"`
	Deployment    Deployment     `yaml:"deployment"`
	Migration     *Migration     `yaml:"migration,omitempty"`
	HealthCheck   HealthCheck    `yaml:"healthCheck"`
	Observability *Observability `yaml:"observability,omitempty"`
	Compatibility *Compatibility `yaml:"compatibility,omitempty"`

	// Source 是该 Manifest 的来源（文件路径或安装源描述），只用于错误提示。
	Source string `yaml:"-"`
}

// Metadata 是组件元信息（002 §2.3）。
type Metadata struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
	Vendor      string `yaml:"vendor,omitempty"`
	License     string `yaml:"license,omitempty"`
	APIDocs     string `yaml:"apiDocs,omitempty"`
}

// Artifact 是组件附带的产物（002 §2.3、附录 B.5）。
// type 与 format 都是自由字符串，平台不限枚举、不解析文件内容。
type Artifact struct {
	Type        string   `yaml:"type"`
	Format      string   `yaml:"format,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Files       []string `yaml:"files"`
}

// Dependencies 是组件依赖声明（002 §3）。
type Dependencies struct {
	Components []ComponentDep `yaml:"components,omitempty"`
	Resources  []ResourceDep  `yaml:"resources,omitempty"`
}

// ComponentDep 是一条组件依赖。支持两种 YAML 写法（002 §3.2）：
//
//   - department/tree@1.0.0                 # 强依赖
//   - id: infra/redis-event-bus@1.0.0       # 弱依赖
//     optional: true
type ComponentDep struct {
	// ID 是组件 ID（不含版本）。
	ID string
	// Version 是精确版本。
	Version string
	// Optional 为 true 表示弱依赖：缺失时警告但继续，且完全不注入环境变量。
	Optional bool
	// Ref 是 YAML 中的原始写法（如 department/tree@1.0.0），用于错误提示。
	Ref string
}

// ResourceDep 是一条资源依赖（002 §3.5）。
type ResourceDep struct {
	Kind   string `yaml:"kind"`
	Engine string `yaml:"engine"`
}

// ConfigSchema 是组件的"配置说明书"（002 §6.5）。
// CLI 不用它校验使用者填写的 config 值类型，只在发布/解析时校验其自身结构。
type ConfigSchema struct {
	Type       string                    `yaml:"type,omitempty"`
	Properties map[string]ConfigProperty `yaml:"properties,omitempty"`
	Required   []string                  `yaml:"required,omitempty"`
}

// ConfigProperty 是单个配置项的声明。
type ConfigProperty struct {
	Type        string   `yaml:"type"`
	Default     any      `yaml:"default,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Enum        []any    `yaml:"enum,omitempty"`
	Items       *ItemDef `yaml:"items,omitempty"`
}

// ItemDef 描述数组类型配置项的元素类型。
type ItemDef struct {
	Type string `yaml:"type"`
}

// Deployment 是部署声明（002 §4）。
type Deployment struct {
	Type       string      `yaml:"type"`
	Image      string      `yaml:"image"`
	Port       int         `yaml:"port"`
	ExtraPorts []ExtraPort `yaml:"extraPorts,omitempty"`
	Resources  *Resources  `yaml:"resources,omitempty"`
}

// ExtraPort 是额外端口声明（附录 B.7）。
type ExtraPort struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port"`
}

// Resources 是推荐的资源配额（002 §4.6）。CLI 透传，不校验数值合理性。
type Resources struct {
	Requests *ResourceSpec `yaml:"requests,omitempty"`
	Limits   *ResourceSpec `yaml:"limits,omitempty"`
}

// ResourceSpec 是一组 CPU / 内存值。
type ResourceSpec struct {
	CPU    string `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

// Migration 是数据库迁移声明（002 §8.2）。command 必须是数组格式。
type Migration struct {
	Command []string `yaml:"command"`
}

// HealthCheck 是健康检查声明（002 §9）。
// 注意：/healthz 只检查本进程存活，禁止检查外部依赖（002 §9.4）。
type HealthCheck struct {
	Type string `yaml:"type"`
	Path string `yaml:"path,omitempty"`
}

// Observability 是预留的可观测性声明（002 §2.3）。
type Observability struct {
	Metrics bool `yaml:"metrics"`
	Tracing bool `yaml:"tracing"`
}

// Compatibility 声明 CLI 版本兼容性（002 §2.3）。
type Compatibility struct {
	MinCliVersion string `yaml:"minCliVersion,omitempty"`
}

// IsOptional 返回该依赖是否为弱依赖。
func (d ComponentDep) IsOptional() bool { return d.Optional }
