package model

// Manifest 是市场侧的组件 Manifest 视图（002 §2.2）。
//
// 市场**独立实现**这套结构与校验，不复用 CLI 的解析器：
// 市场是源头防御的一环，两侧各自成立才叫双保险（007 §18）。
// 未知字段一律忽略，保证老市场也能收下新版本组件的 Manifest。
type Manifest struct {
	APIVersion    string         `json:"apiVersion"`
	Kind          string         `json:"kind"`
	Metadata      Metadata       `json:"metadata"`
	Tags          []string       `json:"tags,omitempty"`
	Artifacts     []Artifact     `json:"artifacts,omitempty"`
	Dependencies  *Dependencies  `json:"dependencies,omitempty"`
	ConfigSchema  *ConfigSchema  `json:"configSchema,omitempty"`
	Deployment    Deployment     `json:"deployment"`
	Migration     *Migration     `json:"migration,omitempty"`
	HealthCheck   HealthCheck    `json:"healthCheck"`
	Observability *Observability `json:"observability,omitempty"`
}

// Metadata 是组件元信息（002 §2.3）。
type Metadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Vendor      string `json:"vendor,omitempty"`
	License     string `json:"license,omitempty"`
}

// Artifact 是组件产物声明。type 与 format 都是自由字符串，市场不校验取值（002 §2.3）。
type Artifact struct {
	Type        string   `json:"type"`
	Format      string   `json:"format,omitempty"`
	Description string   `json:"description,omitempty"`
	Files       []string `json:"files,omitempty"`
	// Reference 是 container 类型产物的镜像地址（007 §10.4）。
	Reference string `json:"reference,omitempty"`
}

// IsContainer 判断该产物是否为镜像引用（这类产物没有文件列表）。
func (a Artifact) IsContainer() bool { return a.Type == ArtifactTypeContainer }

// Dependencies 是组件依赖声明（002 §3）。
type Dependencies struct {
	Components []ComponentDep `json:"components,omitempty"`
	Resources  []ResourceDep  `json:"resources,omitempty"`
}

// ComponentDep 是一条组件依赖，支持标量与映射两种写法（002 §3.2）。
type ComponentDep struct {
	ID       string
	Version  string
	Optional bool
	// Ref 是原始写法（如 department/tree@1.0.0），用于错误提示。
	Ref string
}

// ResourceDep 是一条资源依赖（002 §3.5）。
type ResourceDep struct {
	Kind   string `json:"kind"`
	Engine string `json:"engine"`
}

// ConfigSchema 是组件的配置说明书（002 §6.5）。
// 市场只校验它自身的格式与配置项命名，不校验使用者填的值。
type ConfigSchema struct {
	Type       string                    `json:"type,omitempty"`
	Properties map[string]ConfigProperty `json:"properties,omitempty"`
	Required   []string                  `json:"required,omitempty"`
}

// ConfigProperty 是一个配置项的声明。
type ConfigProperty struct {
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
}

// Deployment 是部署声明（002 §4）。
type Deployment struct {
	Type       string      `json:"type"`
	Image      string      `json:"image"`
	Port       int         `json:"port,omitempty"`
	ExtraPorts []ExtraPort `json:"extraPorts,omitempty"`
	Resources  *Resources  `json:"resources,omitempty"`
}

// ExtraPort 是额外端口声明（002 §4.5）。
type ExtraPort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol,omitempty"`
}

// Resources 是推荐资源配额（002 §4.6）。
type Resources struct {
	Requests *ResourceSpec `json:"requests,omitempty"`
	Limits   *ResourceSpec `json:"limits,omitempty"`
}

// ResourceSpec 是一组 cpu / memory 配额。
type ResourceSpec struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// Migration 是数据库迁移声明（002 §8）。
type Migration struct {
	Command []string `json:"command"`
}

// HealthCheck 是健康检查声明（002 §9）。
type HealthCheck struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
	Port int    `json:"port,omitempty"`
}

// Observability 是可观测性预留字段（002 §2.3）。
type Observability struct {
	Metrics bool `json:"metrics,omitempty"`
	Tracing bool `json:"tracing,omitempty"`
}
