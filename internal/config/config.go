package config

import (
	"github.com/brickkit/brickkit/internal/manifest"
)

// 部署目标（003 §3.2）。
const (
	TargetDocker = "docker"
	TargetK8s    = "k8s"
)

// 安装源类型（003 §6.1）。
const (
	SourceTypeMarket = "market"
	SourceTypeGit    = "git"
	SourceTypeLocal  = "local"
)

// 基础资源类型（006 §2.2）。每类资源有各自的连接变量命名（006 §5.2）。
const (
	ResourceKindDatabase = "database"
	ResourceKindCache    = "cache"
	ResourceKindMQ       = "mq"
	ResourceKindStorage  = "storage"
	ResourceKindSearch   = "search"
	ResourceKindSMTP     = "smtp"
)

// Config 是 brickkit.yaml 的完整结构（003、附录 D.1）。
type Config struct {
	Project    string      `yaml:"project"`
	Deploy     Deploy      `yaml:"deploy"`
	Sources    []Source    `yaml:"sources,omitempty"`
	Components []Component `yaml:"components,omitempty"`
	Resources  []Resource  `yaml:"resources,omitempty"`
	Installer  *Installer  `yaml:"installer,omitempty"`

	// Source 是该配置的来源路径，只用于错误提示。
	Source string `yaml:"-"`
}

// Deploy 是部署目标声明。
type Deploy struct {
	Target string `yaml:"target"`
}

// Source 是一个安装源（003 §6）。
type Source struct {
	ID        string `yaml:"id"`
	Type      string `yaml:"type"`
	URL       string `yaml:"url,omitempty"`
	Path      string `yaml:"path,omitempty"`
	AuthToken string `yaml:"authToken,omitempty"`
	// Enabled 缺省视为 true（003 §6.2）。
	Enabled *bool `yaml:"enabled,omitempty"`
}

// IsEnabled 返回该安装源是否启用（缺省为 true）。
func (s Source) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// EnabledState 是 enabled 字段的三种状态（003 §4.3、附录 D.1.1）。
type EnabledState int

const (
	// EnabledDefault 表示没写 enabled：默认开启，但可被级联关闭。
	EnabledDefault EnabledState = iota
	// EnabledPinned 表示 enabled: true：钉住，不可被级联关闭。
	EnabledPinned
	// EnabledDisabled 表示 enabled: false：显式关闭，一定不启动。
	EnabledDisabled
)

// String 返回状态的中文说明，用于 up / status 的输出。
func (s EnabledState) String() string {
	switch s {
	case EnabledPinned:
		return "钉住"
	case EnabledDisabled:
		return "显式禁用"
	default:
		return "默认开启"
	}
}

// Component 是 components 列表中的一个条目（003 §4.1）。
type Component struct {
	ID      string `yaml:"id"`
	Version string `yaml:"version"`
	// Enabled 是三态字段：nil=默认开启可被级联 / true=钉住 / false=显式关闭。
	Enabled    *bool  `yaml:"enabled,omitempty"`
	Local      bool   `yaml:"local,omitempty"`
	LocalPort  int    `yaml:"localPort,omitempty"`
	Expose     bool   `yaml:"expose,omitempty"`
	Hostname   string `yaml:"hostname,omitempty"`
	ExposePort int    `yaml:"exposePort,omitempty"`
	// Config 覆盖 configSchema 默认值。CLI 不校验值的类型（003 §4.6）。
	Config map[string]any `yaml:"config,omitempty"`
	// Resources 覆盖 Manifest 中的 deployment.resources（003 §4.7）。
	// 与 Manifest 共用同一结构，Step 11 负责按优先级链合并。
	Resources *manifest.Resources `yaml:"resources,omitempty"`
}

// EnabledState 返回该组件的启用状态。
func (c Component) EnabledState() EnabledState {
	switch {
	case c.Enabled == nil:
		return EnabledDefault
	case *c.Enabled:
		return EnabledPinned
	default:
		return EnabledDisabled
	}
}

// IsPinned 表示显式钉住（enabled: true）。
func (c Component) IsPinned() bool { return c.EnabledState() == EnabledPinned }

// IsDisabled 表示显式关闭（enabled: false）。
func (c Component) IsDisabled() bool { return c.EnabledState() == EnabledDisabled }

// Ref 返回 <组件ID>@<版本> 形式，用于日志与错误提示。
func (c Component) Ref() string { return c.ID + "@" + c.Version }

// Resource 是一个基础资源声明与绑定（003 §5）。
type Resource struct {
	Kind     string    `yaml:"kind"`
	Engine   string    `yaml:"engine"`
	ID       string    `yaml:"id"`
	Host     string    `yaml:"host"`
	Port     int       `yaml:"port"`
	Username string    `yaml:"username,omitempty"`
	Password string    `yaml:"password,omitempty"`
	Bindings []Binding `yaml:"bindings,omitempty"`
}

// Binding 把资源绑定到某个组件（003 §5.3、§5.6）。
type Binding struct {
	ComponentID string `yaml:"componentId"`
	Database    string `yaml:"database,omitempty"`
	// EnvPrefix 用于一个组件绑定多个同类资源时区分环境变量（如 PRIMARY_ / ARCHIVE_）。
	EnvPrefix string `yaml:"envPrefix,omitempty"`
}

// Installer 是安装器行为配置（003 §3.6）。
type Installer struct {
	// RequireSignature 缺省为 true（附录 D.1）。
	RequireSignature *bool `yaml:"requireSignature,omitempty"`
}

// RequireSignature 返回是否强制签名校验（默认 true）。
func (c *Config) RequireSignature() bool {
	if c.Installer == nil || c.Installer.RequireSignature == nil {
		return true
	}
	return *c.Installer.RequireSignature
}

// ComponentsByID 返回指定组件 ID 的全部版本条目（多版本共存，003 §4.8）。
func (c *Config) ComponentsByID(id string) []Component {
	var out []Component
	for _, item := range c.Components {
		if item.ID == id {
			out = append(out, item)
		}
	}
	return out
}

// HasMultipleVersions 表示该组件 ID 在配置中存在多个版本。
func (c *Config) HasMultipleVersions(id string) bool {
	return len(c.ComponentsByID(id)) > 1
}

// EnabledSources 返回启用中的安装源，保持配置顺序（003 §6.5 优先级）。
func (c *Config) EnabledSources() []Source {
	var out []Source
	for _, s := range c.Sources {
		if s.IsEnabled() {
			out = append(out, s)
		}
	}
	return out
}
