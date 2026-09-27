// Package deployfile 负责部署层文件：团队的 deploy.yaml、个人的 deploy.local.yaml、
// 以及 -f 显式指定的环境文件（deploy.prod.yaml 之类）。三者结构完全相同，只有"角色"不同——
// mode: debug 只允许出现在个人文件里（提案 §6.4）。
package deployfile

import (
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/yamlfile"
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
	// 保留成 YAML 节点：数字要按原文取值（VER: 1.10 是 "1.10"），由 configdir.ParseVarsMap 解释。
	Vars       map[string]yaml.Node `yaml:"vars,omitempty"`
	Components []Component          `yaml:"components,omitempty"`

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

// Entry 是一个组件的部署条目：顶层条目与外壳下面的成员条目共用这组字段。
//
// ID 是裸 ID（覆盖该 ID 的默认版本，即 brickkit.yaml 里不带 requiredBy 的那一行）或
// id@version（只覆盖那一个版本）。
type Entry struct {
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
	// SkipWaitFor 列出启动时不等的强依赖（组件 ID）：只去掉 depends_on 与启动顺序里的等待，
	// 照样连得到它们（附录 A23）。代价由写的人承担——组件得扛住这些依赖暂时没就绪。
	// 只在 docker / podman 下起作用：K8s 的 Pod 之间没有启动顺序。
	SkipWaitFor []string `yaml:"skipWaitFor,omitempty"`
}

// Component 是 components 下面的一个顶层条目。
//
// 外壳条目把它实际收编的成员作为完整条目嵌在 Members 下面——这是成员关系的唯一来源
// （提案 §8.4、附录 A21）。成员条目的字段是它"自己跑"时的部署配置：外壳这次不跑时，
// 成员就按这些字段独立部署。成员条目是 Entry，没有 Members：只嵌一层由类型本身保证。
type Component struct {
	Entry   `yaml:",inline"`
	Members []Entry `yaml:"members,omitempty"`
}

// Key 把条目 ID 拆成组件 ID 与版本（裸 ID 时版本为空）。
func (c Entry) Key() (id, version string) {
	id, version, _ = manifest.SplitRef(c.ID)
	return id, version
}

// ReplicaCount 返回副本数，未写时为 1。
func (c Entry) ReplicaCount() int {
	if c.Replicas == nil {
		return 1
	}
	return *c.Replicas
}

// IsDisabled 表示钉死不跑。
func (c Entry) IsDisabled() bool { return c.Mode == ModeDisable }

// IsPinned 表示钉死要跑（enabled / local / debug）。
func (c Entry) IsPinned() bool {
	return c.Mode == ModeEnabled || c.Mode == ModeLocal || c.Mode == ModeDebug
}

// IsBareProcess 表示以裸进程运行，不生成容器（local / debug）。
func (c Entry) IsBareProcess() bool { return c.Mode == ModeLocal || c.Mode == ModeDebug }

// Located 是摊平后的一个条目：带着它在文件里的字段路径与所属外壳条目的 ID（顶层条目为空）。
type Located struct {
	Entry
	// Field 是字段路径，如 components[0] 或 components[0].members[1]。
	Field string
	// Shell 是外壳条目的 ID（原样，可能带 @version）；顶层条目为空。
	Shell string
}

// All 返回全部条目，外壳下面的成员条目紧跟在外壳之后。逐条目的检查与查找都走它，
// 不必各自再记得往 Members 里看一层。
func (f *File) All() []Located {
	var out []Located
	for i, c := range f.Components {
		field := yamlfile.Indexed("components", i)
		out = append(out, Located{Entry: c.Entry, Field: field})
		for j, m := range c.Members {
			out = append(out, Located{Entry: m, Field: yamlfile.Indexed(field+".members", j), Shell: c.ID})
		}
	}
	return out
}

// Entry 返回覆盖 id@version 的条目（顶层或外壳下面）：专属条目优先；裸 ID 条目只在该版本是
// 默认版本（isDefault，由 brickkit.yaml 决定）时覆盖它。
func (f *File) Entry(id, version string, isDefault bool) (Entry, bool) {
	l, ok := f.EntryAt(id, version, isDefault)
	return l.Entry, ok
}

// EntryAt 与 Entry 同一条规则，另外带回条目在文件里的字段路径（报错指向它）。
func (f *File) EntryAt(id, version string, isDefault bool) (Located, bool) {
	var bare *Located
	for _, l := range f.All() {
		entryID, entryVersion := l.Key()
		if entryID != id {
			continue
		}
		if entryVersion == version {
			return l, true
		}
		if entryVersion == "" && isDefault {
			found := l
			bare = &found
		}
	}
	if bare != nil {
		return *bare, true
	}
	return Located{}, false
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
