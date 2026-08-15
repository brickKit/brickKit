package config

import (
	"github.com/brickkit/brickkit/internal/manifest"
)

// 部署目标（003 §3.2）。
const (
	TargetDocker = "docker"
	TargetK8s    = "k8s"
	// PodSecurityRestricted 对应 K8s 官方 Pod Security Standards 的 restricted 级别。
	PodSecurityRestricted = "restricted"
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
//
// Context / Namespace / CreateNamespace 只在 deploy.target: k8s 下有意义。
type Deploy struct {
	Target string `yaml:"target"`
	// Context 钉住 kubeconfig 上下文（003 §3.2.1）。
	//
	// 不写就用 kubectl 当前的 context。写了就必须对得上——kubectl 默认
	// 部到 `kubectl config current-context` 指的集群，切走了忘记切回来，
	// 一份写着生产的配置会被安静地部到预发，反之亦然。
	Context string `yaml:"context,omitempty"`
	// Namespace 覆盖默认的命名空间（brickkit-<项目名>）。
	//
	// 很多组织的命名空间名是他们定的，而且只给你这一个命名空间的权限。
	Namespace string `yaml:"namespace,omitempty"`
	// PodSecurity 是 Pod 安全级别，目前只支持 "restricted"（K8s 官方
	// Pod Security Standards 的同名级别）。空表示不生成 securityContext。
	//
	// 默认不生成是有意的：加了可能让本来跑得好好的组件起不来（镜像以 root
	// 运行、要绑 1024 以下的端口……），不能默默替使用者决定。
	PodSecurity string `yaml:"podSecurity,omitempty"`
	// ImagePullSecrets 是拉私有镜像用的 Secret 名。
	ImagePullSecrets []string `yaml:"imagePullSecrets,omitempty"`
	// IngressClass 写进 Ingress 的 spec.ingressClassName。
	//
	// 不写时只有集群配了"默认 class"才会有人认领这条 Ingress；
	// 没有默认 class 的集群上 apply 成功、域名却打不开。
	IngressClass string `yaml:"ingressClass,omitempty"`
	// IngressAnnotations 原样透传到 Ingress 的注解（cert-manager、nginx 参数等）。
	// 平台不认识它们，也不该认识。
	IngressAnnotations map[string]string `yaml:"ingressAnnotations,omitempty"`
	// CreateNamespace 缺省为 true。置 false 时 CLI 既不生成也不 apply
	// namespace.yaml——只有命名空间级权限时，建命名空间会 Forbidden，
	// 而那个命名空间其实早就由运维建好了。
	CreateNamespace *bool `yaml:"createNamespace,omitempty"`
}

// ShouldCreateNamespace 返回是否由 CLI 创建命名空间（缺省 true）。
func (d Deploy) ShouldCreateNamespace() bool {
	return d.CreateNamespace == nil || *d.CreateNamespace
}

// Source 是一个安装源（003 §6）。
type Source struct {
	ID   string `yaml:"id"`
	Type string `yaml:"type"`
	URL  string `yaml:"url,omitempty"`
	Path string `yaml:"path,omitempty"`
	// Ref 是 git 源要取的分支 / tag / commit（003 §6.3）。
	// 空表示用仓库的默认分支。
	Ref       string `yaml:"ref,omitempty"`
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
	// TLSSecret 是 Ingress 用的 TLS 证书 Secret 名（仅 K8s，需要 expose: true）。
	TLSSecret string `yaml:"tlsSecret,omitempty"`
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
	Kind     string `yaml:"kind"`
	Engine   string `yaml:"engine"`
	ID       string `yaml:"id"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	// PasswordFromEnv 表示 password 原文写的是 ${ENV_VAR} 引用。
	//
	// 由解析器在展开环境变量**之前**记下（008：密码不该写进 brickkit.yaml）。
	// 展开之后两者长得一模一样，靠 Password 的值判断只会在使用者做对时误报。
	PasswordFromEnv bool      `yaml:"-"`
	Bindings        []Binding `yaml:"bindings,omitempty"`
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
	// PublicKeys 是项目信任的组件发布者公钥：签名里的 publicKeyRef → 公钥文件路径。
	//
	// 信任锚点必须在使用者这一侧。008 §8.3 的签名里只有 publicKeyRef 这个**名字**，
	// 没说该由谁来解析它；如果公钥跟着签名一起从市场取，就成了市场自己给自己发证
	// ——市场被攻破时攻击者把组件和公钥一起换掉，验签照样通过。那样 008 §14.1
	// 写的"市场被攻破 → 靠签名校验"就是一句空话。所以只认这里配的。
	//
	// 公钥不是密钥，应当跟着 brickkit.yaml 一起提交 Git，好让它的变更有评审记录。
	PublicKeys map[string]string `yaml:"publicKeys,omitempty"`
}

// RequireSignature 返回是否强制签名校验（默认 true）。
func (c *Config) RequireSignature() bool {
	if c.Installer == nil || c.Installer.RequireSignature == nil {
		return true
	}
	return *c.Installer.RequireSignature
}

// PublicKeys 返回项目信任的发布者公钥（ref → 文件路径），未配置时返回 nil。
func (c *Config) PublicKeys() map[string]string {
	if c.Installer == nil {
		return nil
	}
	return c.Installer.PublicKeys
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
