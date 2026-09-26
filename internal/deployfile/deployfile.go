// Package deployfile 负责部署层文件：团队的 deploy.yaml、个人的 deploy.local.yaml、
// 以及 -f 显式指定的环境文件（deploy.prod.yaml 之类）。三者结构完全相同，只有"角色"不同——
// mode: debug 只允许出现在个人文件里（提案 §6.4）。
package deployfile

import (
	"strings"

	"github.com/brickkit/brickkit/internal/manifest"
)

const (
	TargetDocker = "docker"
	TargetPodman = "podman"
	TargetK8s    = "k8s"

	ModeEnabled = "enabled"
	ModeDisable = "disable"
	ModeLocal   = "local"
	ModeDebug   = "debug"

	PodSecurityRestricted = "restricted"

	FileTeam  = "deploy.yaml"
	FileLocal = "deploy.local.yaml"

	MinPort = 1
	MaxPort = 65535
)

// Role 是这份部署文件的角色。
type Role int

const (
	// RoleTeam：deploy.yaml 或 -f 指定的文件，进 Git、给团队评审。
	RoleTeam Role = iota
	// RoleLocal：deploy.local.yaml，个人本地文件，不进 Git。
	RoleLocal
)

// File 是一份部署文件的完整结构。
type File struct {
	Target string `yaml:"target" jsonschema:"enum=docker|podman|k8s"`
	// K8s 收拢所有只在 target: k8s 下有意义的项目级设置；其它 target 下写了会警告。
	K8s *K8s `yaml:"k8s,omitempty"`
	// Vars 覆盖 config/vars.yaml 里的同名公共变量，只作用于 $var: 查找（附录 A、提案 §6.1）。
	// 值里的 ${VAR} 解析时不展开——它们多半是密钥，何时求值由渲染器决定。
	Vars       map[string]any `yaml:"vars,omitempty"`
	Components []Component    `yaml:"components,omitempty"`

	// Source 是文件路径，只用于报错。
	Source string `yaml:"-"`
}

// K8s 是 K8s 专属的项目级设置（字段语义与旧 brickkit.yaml 的 deploy.* 相同）。
type K8s struct {
	Context            string            `yaml:"context,omitempty"`
	Namespace          string            `yaml:"namespace,omitempty"`
	CreateNamespace    *bool             `yaml:"createNamespace,omitempty"`
	PodSecurity        string            `yaml:"podSecurity,omitempty" jsonschema:"enum=restricted"`
	ImagePullSecrets   []string          `yaml:"imagePullSecrets,omitempty"`
	IngressClass       string            `yaml:"ingressClass,omitempty"`
	IngressAnnotations map[string]string `yaml:"ingressAnnotations,omitempty"`
	NetworkPolicy      *NetworkPolicy    `yaml:"networkPolicy,omitempty"`
	ServiceAccount     *ServiceAccount   `yaml:"serviceAccount,omitempty"`
}

// NetworkPolicy 按依赖图生成网络策略的开关与补充规则。
type NetworkPolicy struct {
	Enabled           bool                     `yaml:"enabled"`
	IngressController *IngressControllerSource `yaml:"ingressController,omitempty"`
	AllowFrom         []AllowFromSource        `yaml:"allowFrom,omitempty"`
	Egress            *Egress                  `yaml:"egress,omitempty"`
}

// IngressControllerSource 定位 ingress controller 的 Pod。
type IngressControllerSource struct {
	Namespace   string            `yaml:"namespace"`
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
}

// AllowFromSource 是依赖图之外的合法入站来源（监控、备份……）。
type AllowFromSource struct {
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace"`
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
	Ports       []int             `yaml:"ports,omitempty"`
}

// Egress 是出站白名单。
type Egress struct {
	Enabled bool            `yaml:"enabled"`
	AllowTo []AllowToTarget `yaml:"allowTo,omitempty"`
}

// AllowToTarget 是一个出站目标：集群内写 Namespace，集群外写 CIDR。
//
// 旧版的 resource 写法随 resources 一起废除（附录 A15）：数据库地址现在只是某个组件
// config 里的一串字符，平台不再知道它在哪，只能由使用者直接写位置与端口。
type AllowToTarget struct {
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace,omitempty"`
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
	CIDR        string            `yaml:"cidr,omitempty"`
	Ports       []int             `yaml:"ports,omitempty"`
}

// ServiceAccount 是"每个组件一个不挂令牌的 SA"开关。
type ServiceAccount struct {
	Enabled bool `yaml:"enabled"`
}

// Component 是一个组件的部署条目。
//
// ID 是裸 ID（覆盖该 ID 所有没有专属条目的版本）或 id@version（只覆盖那一个版本）。
// 外壳条目用 Members 列出实际收编的成员——这是成员关系的唯一来源（提案 §8.4）。
type Component struct {
	ID                 string              `yaml:"id"`
	Mode               string              `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|local|debug"`
	LocalPort          int                 `yaml:"localPort,omitempty"`
	Expose             bool                `yaml:"expose,omitempty"`
	ExposePort         int                 `yaml:"exposePort,omitempty"`
	Hostname           string              `yaml:"hostname,omitempty"`
	TLSSecret          string              `yaml:"tlsSecret,omitempty"`
	Replicas           *int                `yaml:"replicas,omitempty"`
	ServiceAccountName string              `yaml:"serviceAccountName,omitempty"`
	Resources          *manifest.Resources `yaml:"resources,omitempty"`
	Labels             map[string]string   `yaml:"labels,omitempty"`
	Members            []string            `yaml:"members,omitempty"`
}

// Key 把条目 ID 拆成组件 ID 与版本（裸 ID 时版本为空）。
func (c Component) Key() (id, version string) {
	if i := strings.LastIndex(c.ID, "@"); i >= 0 {
		return c.ID[:i], c.ID[i+1:]
	}
	return c.ID, ""
}

// ReplicaCount 返回副本数，未写时为 1。
func (c Component) ReplicaCount() int {
	if c.Replicas == nil {
		return 1
	}
	return *c.Replicas
}

// IsDisabled 表示钉死不跑。
func (c Component) IsDisabled() bool { return c.Mode == ModeDisable }

// IsPinned 表示钉死要跑（enabled / local / debug）。
func (c Component) IsPinned() bool {
	return c.Mode == ModeEnabled || c.Mode == ModeLocal || c.Mode == ModeDebug
}

// IsBareProcess 表示以裸进程运行，不生成容器（local / debug）。
func (c Component) IsBareProcess() bool { return c.Mode == ModeLocal || c.Mode == ModeDebug }

// Entry 返回覆盖 id@version 的条目：专属条目优先，其次是裸 ID 条目。
func (f *File) Entry(id, version string) (Component, bool) {
	var bare *Component
	for i := range f.Components {
		entryID, entryVersion := f.Components[i].Key()
		if entryID != id {
			continue
		}
		if entryVersion == version {
			return f.Components[i], true
		}
		if entryVersion == "" {
			bare = &f.Components[i]
		}
	}
	if bare != nil {
		return *bare, true
	}
	return Component{}, false
}

// Settings 返回 K8s 设置；没写 k8s: 时是零值。
func (f *File) Settings() K8s {
	if f.K8s == nil {
		return K8s{}
	}
	return *f.K8s
}

// ShouldCreateNamespace 返回是否由 CLI 创建命名空间（缺省 true）。
func (f *File) ShouldCreateNamespace() bool {
	s := f.Settings()
	return s.CreateNamespace == nil || *s.CreateNamespace
}

// NetworkPolicyEnabled 返回是否生成 NetworkPolicy。
func (f *File) NetworkPolicyEnabled() bool {
	np := f.Settings().NetworkPolicy
	return np != nil && np.Enabled
}

// EgressEnabled 返回是否生成出站策略。
func (f *File) EgressEnabled() bool {
	return f.NetworkPolicyEnabled() && f.K8s.NetworkPolicy.Egress != nil && f.K8s.NetworkPolicy.Egress.Enabled
}

// ServiceAccountEnabled 返回是否为每个组件生成 ServiceAccount。
func (f *File) ServiceAccountEnabled() bool {
	sa := f.Settings().ServiceAccount
	return sa != nil && sa.Enabled
}
