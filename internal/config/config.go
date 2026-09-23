package config

import (
	"github.com/brickkit/brickkit/internal/manifest"
)

// 部署目标（003 §3.2）。
const (
	TargetDocker = "docker"
	TargetK8s    = "k8s"
	// TargetPodman 是 override.yaml 才能选的取值（override.yaml 设计书 §5）——
	// brickkit.yaml 自身的 deploy.target 校验（validateDeploy）从不接受它；这个
	// 常量存在只是为了给 internal/override 与后续读取"已被覆盖过的 cfg.Deploy.Target"
	// 的代码一个共享的取值名字，避免到处写裸字符串 "podman"。
	TargetPodman = "podman"
	// PodSecurityRestricted 对应 K8s 官方 Pod Security Standards 的 restricted 级别。
	PodSecurityRestricted = "restricted"
)

// 安装源类型（003 §6.1）。
const (
	SourceTypeMarket = "market"
	SourceTypeGit    = "git"
	SourceTypeLocal  = "local"
)

// 基础资源类型（006 §2.1）。每类资源有各自的连接变量命名（006 §5.2）。
//
// 权威定义在 internal/manifest：组件 Manifest 与 brickkit.yaml 用的是同一套
// kind，`matchResource` 直接按字符串比对它们，两处各写一份迟早会分叉。
const (
	ResourceKindDatabase = manifest.ResourceKindDatabase
	ResourceKindCache    = manifest.ResourceKindCache
	ResourceKindMQ       = manifest.ResourceKindMQ
	ResourceKindStorage  = manifest.ResourceKindStorage
	ResourceKindSearch   = manifest.ResourceKindSearch
	ResourceKindSMTP     = manifest.ResourceKindSMTP
)

// Config 是 brickkit.yaml 的完整结构（003、附录 D.1）。
//
// 它连同引用到的全部类型，就是 schemas/brickkit.schema.json 的来源（internal/schemagen 反射生成）。
// 两件事因此要记得：yaml tag 里有没有 omitempty 决定该字段在 schema 里是不是必填（规则见
// schemagen 的包注释），加减 omitempty 之前先看那里；`jsonschema` tag 写的是封闭取值的约束，
// 与 Validate 里的规则是同一份取值，改一处要改另一处，schemas_test.go 会核对。
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
//
// Target 的 jsonschema enum 与 validateDeploy 里的 case 是同一份取值，改一处要改另一处，
// schemas_test.go 会核对（见 internal/schemagen）。
type Deploy struct {
	Target string `yaml:"target" jsonschema:"enum=docker|k8s"`
	// Context 钉住 kubeconfig 上下文（005 §5.11）。
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
	// NetworkPolicy 打开后按依赖图生成 NetworkPolicy（P26）。
	//
	// 默认不生成：集群里可能压根没有能执行策略的 CNI（那时生成的文件会被
	// 静默忽略，白写还让人以为收紧了），也可能运维已在命名空间级别铺了一套。
	NetworkPolicy *NetworkPolicy `yaml:"networkPolicy,omitempty"`
	// ServiceAccount 打开后给每个组件生成一个不挂载令牌的 ServiceAccount（P26）。
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty"`
}

// ShouldCreateNamespace 返回是否由 CLI 创建命名空间（缺省 true）。
func (d Deploy) ShouldCreateNamespace() bool {
	return d.CreateNamespace == nil || *d.CreateNamespace
}

// NetworkPolicyEnabled 返回是否生成 NetworkPolicy。
func (d Deploy) NetworkPolicyEnabled() bool {
	return d.NetworkPolicy != nil && d.NetworkPolicy.Enabled
}

// ServiceAccountEnabled 返回是否为每个组件生成 ServiceAccount。
func (d Deploy) ServiceAccountEnabled() bool {
	return d.ServiceAccount != nil && d.ServiceAccount.Enabled
}

// NetworkPolicy 是网络策略生成开关（P26）。
//
// 只生成**入站**方向：出站方向 BrickKit 生成不出正确的规则——DNS 得放行
// kube-dns（各集群位置不一），数据库在 K8s 下由运维部署（005 §5.1），
// 配置里只有一个 host 字符串，变不成 podSelector 也变不成 CIDR。
// 生成一份为了不误伤而放行 0.0.0.0/0 的出站策略，比不生成更糟。
type NetworkPolicy struct {
	Enabled bool `yaml:"enabled"`
	// IngressController 说明 ingress controller 在哪。
	//
	// 有 expose: true 的组件时必填：不填的话生成的策略会把 ingress controller
	// 一起挡在门外，结果是部署全部成功、网站直接打不开。
	IngressController *IngressControllerSource `yaml:"ingressController,omitempty"`
	// AllowFrom 是依赖图之外的合法入站来源（P36）。
	//
	// 生成的规则只放行依赖图里的组件，而监控、备份、服务网格这些都不在那张图上。
	// 最典型的是 Prometheus 抓 /metrics：挡掉之后**指标悄悄停了**，
	// 而服务本身完全正常，没有任何报错——这是最难查的一类故障。
	//
	// 每一条都会加到**每一个**组件的策略上：监控要抓的是全部组件。
	AllowFrom []AllowFromSource `yaml:"allowFrom,omitempty"`
	// Egress 是出站策略（P37）。不写就完全不管出站。
	Egress *Egress `yaml:"egress,omitempty"`
}

// Egress 是出站策略（P37）。
//
// ⚠️ **它会翻转默认行为。** NetworkPolicy 的语义是：一个 Pod 只要在 Egress
// 方向被任何策略选中，该方向**未明确允许的一律拒绝**。所以打开这个开关的那一刻，
// 组件就从"想连谁连谁"变成"只能连白名单"。
//
// 漏掉一项的后果取决于组件什么时候建连：启动时建连的起不来，
// 首次请求才建连的则健康检查照过、业务请求失败（/healthz 只查本进程，002 §9.4）。
// 更阴险的是改策略不会杀掉已建立的连接——正在跑的实例照常工作，
// 问题要等到下一次重启才暴露。
//
// 因此平台承担了大部分：DNS 自动放行、组件依赖从依赖图推导；
// 只有资源（平台知道谁要用、不知道它在集群哪儿）需要声明，
// **声明不全会在生成阶段阻断**。
type Egress struct {
	Enabled bool `yaml:"enabled"`
	// AllowTo 是允许连出去的目标。
	AllowTo []AllowToTarget `yaml:"allowTo,omitempty"`
}

// AllowToTarget 是一个出站目标（P37）。
//
// 位置二选一：集群内写 Namespace（可再加 PodSelector 收窄），集群外写 CIDR。
type AllowToTarget struct {
	// Name 说明这条口子是为谁开的，用于报错与注解。
	Name string `yaml:"name"`
	// Resource 对应 resources[].id。写了它，端口就由平台从 resources[].port 补——
	// 那个值配置里已经有了，抄第二遍只会出现两处不一致。
	Resource string `yaml:"resource,omitempty"`
	// Namespace 是集群内目标所在的命名空间。
	Namespace string `yaml:"namespace,omitempty"`
	// PodSelector 进一步收窄到该命名空间里的哪些 Pod。
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
	// CIDR 是集群外目标的地址段（托管数据库、第三方 API 等）。
	CIDR string `yaml:"cidr,omitempty"`
	// Ports 限定放行哪些端口。写了 Resource 时不能再写它。
	Ports []int `yaml:"ports,omitempty"`
}

// EgressEnabled 返回是否生成出站策略。
func (d Deploy) EgressEnabled() bool {
	return d.NetworkPolicyEnabled() &&
		d.NetworkPolicy.Egress != nil && d.NetworkPolicy.Egress.Enabled
}

// AllowFromSource 是一个依赖图之外的入站来源（P36）。
type AllowFromSource struct {
	// Name 说明这条口子是为谁开的，会写进生成策略的注解。
	//
	// 不是装饰：半年后有人 `kubectl get networkpolicy -o yaml` 看到一条放行
	// 某个命名空间的规则，得能立刻知道它干什么用——否则只能在"不敢删"里躺着。
	Name string `yaml:"name"`
	// Namespace 是来源所在的命名空间。
	Namespace string `yaml:"namespace"`
	// PodSelector 进一步收窄到该命名空间里的哪些 Pod；不写则放行该命名空间全部。
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
	// Ports 限定放行哪些端口；不写则放行组件**声明过**的全部端口。
	//
	// 默认放全部是因为使用者多半不知道每个组件的端口是几——那本来就是组件
	// 自己声明的，逼他抄一遍既啰嗦又容易过期（组件加了 extraPorts 就得回来改）。
	Ports []int `yaml:"ports,omitempty"`
}

// IngressControllerSource 定位 ingress controller 的 Pod。
type IngressControllerSource struct {
	// Namespace 是 ingress controller 所在的命名空间，如 ingress-nginx。
	Namespace string `yaml:"namespace"`
	// PodSelector 进一步收窄到该命名空间里的哪些 Pod。
	//
	// 不写就放行该命名空间的所有 Pod：各家 controller 的标签五花八门
	// （ingress-nginx / traefik / higress……），不该逼使用者非得写对。
	PodSelector map[string]string `yaml:"podSelector,omitempty"`
}

// ServiceAccount 是 ServiceAccount 生成开关（P26）。
//
// 打开后每个组件一个专属 SA，且不挂载令牌。默认情况下所有 Pod 共用命名空间的
// default SA 并被塞进一张能跟 API Server 说话的令牌，而业务组件没有一个需要它。
type ServiceAccount struct {
	Enabled bool `yaml:"enabled"`
}

// Source 是一个安装源（003 §6）。
//
// Type 的 jsonschema enum 与 SourceType* 常量、validateSources 里的 case 是同一份取值，改一处要改另一处，
// schemas_test.go 会核对（见 internal/schemagen）。
type Source struct {
	ID   string `yaml:"id"`
	Type string `yaml:"type" jsonschema:"enum=market|git|local"`
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

const (
	ModeEnabled = "enabled"
	ModeDisable = "disable"
	ModeDebug   = "debug"
	ModeLocal   = "local"
)

// Component 是 components 列表中的一个条目（003 §4.1）。
//
// Version 的 jsonschema pattern 是 manifest.IsExactVersion 那条正则的等价写法（tag 里不写反斜杠，所以用 [0-9]、[.]）：
// 与 validateComponents 里的规则是同一份取值，改一处要改另一处，schemas_test.go 会核对（见 internal/schemagen）。
type Component struct {
	ID      string `yaml:"id"`
	Version string `yaml:"version" jsonschema:"pattern=^[0-9]+[.][0-9]+[.][0-9]+$"`
	// Mode 取代了 Enabled/Local 两个字段（mode 字段迁移设计）：
	// ""（未写）= 跟随上层，走容器；"enabled" = 钉住，走容器；
	// "disable" = 钉住不跑；"debug" = 裸进程，用户自己启动；
	// "local" = 裸进程，brickkit 自己拉起（Plan 4b 起才真正启动，Plan 4a 只保证
	// 这个取值本身合法、生成阶段安全跳过）。
	// 校验器负责按 deploy.target 决定这五个取值里哪些合法（k8s 下只认前三个）。
	Mode      string `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|debug|local"`
	LocalPort int    `yaml:"localPort,omitempty"`
	// ServedBy 表示这个组件的工作负载由另一个组件条目提供（外壳合并部署，
	// servedBy 设计书）。声明了它的组件不生成自己的容器/迁移 Job，但平台
	// 照常为依赖它的其它组件计算正确的 *_ENDPOINT——地址指向 servedBy
	// 指向的那个组件实际的位置，端口用这个组件自己声明的那个。
	ServedBy   string `yaml:"servedBy,omitempty"`
	Expose     bool   `yaml:"expose,omitempty"`
	Hostname   string `yaml:"hostname,omitempty"`
	ExposePort int    `yaml:"exposePort,omitempty"`
	// TLSSecret 是 Ingress 用的 TLS 证书 Secret 名（仅 K8s，需要 expose: true）。
	TLSSecret string `yaml:"tlsSecret,omitempty"`
	// ServiceAccountName 让本组件使用一个**已存在**的 ServiceAccount（仅 K8s，P26）。
	//
	// 云上很常见：SA 上绑着 IRSA / Workload Identity 的注解，由运维创建并授权。
	// 写了它，平台就只引用、不生成——重新生成一份会把那份授权安静地抹掉。
	ServiceAccountName string `yaml:"serviceAccountName,omitempty"`
	// Config 覆盖 configSchema 默认值。CLI 不校验值的类型（003 §4.6）。
	//
	// 值里的 ${VAR} **解析时不展开**（见 deferredRefs）：写进 compose 文件还是进 Secret 是渲染器的事。
	// 想知道"这一项写的是不是引用"，看值本身就是原文。这个位置也允许写成 existingSecret 引用
	// 的形状（见 ExistingSecretRef，Task 4），同样不受这里的展开逻辑影响。
	Config map[string]any `yaml:"config,omitempty"`
	// Resources 覆盖 Manifest 中的 deployment.resources（003 §4.7）。
	// 与 Manifest 共用同一结构，Step 11 负责按优先级链合并。
	Resources *manifest.Resources `yaml:"resources,omitempty"`
	// Replicas 是副本数（005 §5.8，**仅 K8s**）。nil 表示不写，按 1 处理。
	//
	// 用指针是为了区分"没写"与"写了 0"：后者必须报错而不是当成关闭组件——
	// 关组件已经有 `mode: disable`，它会走级联计算、会提醒依赖方；
	// 而 replicas: 0 绕过这一切，依赖方照常启动、照常拿到地址，然后连一个
	// 不存在的后端，表现是 503 而状态表里那个组件显示"正常"。
	Replicas *int `yaml:"replicas,omitempty"`
	// Labels 是透传给底层引擎的部署元数据（003 §4.11）。
	//
	// 平台**不解释键值**：Docker 写进 service 的 labels，K8s 写进
	// Deployment 与 Pod 的 annotations。逐键覆盖 Manifest 的
	// deployment.labels（002 §4.7）。这是使用者自己接 Traefik / Prometheus
	// 这类"读容器标签"的工具的唯一入口——平台不做网关，也就必须让人自己做。
	Labels map[string]string `yaml:"labels,omitempty"`
}

// ReplicaCount 返回副本数，未声明时为 1。
//
// 默认必须是 1：加了这个字段不该让任何既有项目的副本数发生变化——
// 那是没人会在升级 CLI 时预期收到的改动。
func (c Component) ReplicaCount() int {
	if c.Replicas == nil {
		return 1
	}
	return *c.Replicas
}

// IsDisabled 表示这个组件被钉死不跑（mode: disable，003 §4.3）。
//
// 从前 Enabled 是三态 *bool，还包过一层 EnabledState 枚举，已删除——它是
// 第二份真相（改判定规则要动两处，而只有一处会被测到），生产代码一次都
// 没调用过它。这次引入 Mode 不是重蹈覆辙：Mode 直接取代了 Enabled/Local
// 两个字段本身，判定逻辑唯一地放在这里与 IsPinned 上，不在别处另存一份。
//
// 启停判定按 Ref 查的那一套在 cascade.declSet：那里要覆盖图里有、
// 配置里没有的组件，查法不一样。
func (c Component) IsDisabled() bool { return c.Mode == ModeDisable }

// IsPinned 表示这个组件被钉死一定要跑：mode: enabled、mode: debug、mode: local
// 都算——不管有没有别的组件依赖它，这三种写法都是"我就是要它跑"的明确意图，
// 不能被 cascade 判定为"没人需要就不跑"。
func (c Component) IsPinned() bool {
	return c.Mode == ModeEnabled || c.Mode == ModeDebug || c.Mode == ModeLocal
}

// Ref 返回 <组件ID>@<版本> 形式，用于日志与错误提示。
func (c Component) Ref() string { return c.ID + "@" + c.Version }

// Resource 是一个基础资源声明与绑定（003 §5）。
//
// Kind 的 jsonschema enum 就是 manifest.ResourceKinds：改一处要改另一处，schemas_test.go 会核对
// （见 internal/schemagen）。
type Resource struct {
	Kind     string `yaml:"kind" jsonschema:"enum=database|cache|mq|storage|search|smtp"`
	Engine   string `yaml:"engine"`
	ID       string `yaml:"id"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username,omitempty"`
	// Password 的值里的 ${VAR} 解析时不展开，理由同 Component.Config。
	Password string `yaml:"password,omitempty"`
	// ExistingSecret 是这个资源的密钥字段（database/cache/mq/smtp 的 password，
	// storage 的 secret-key）该引用的 K8s Secret 名，而不是由平台生成一份——
	// 用在运维已经用 Vault Secrets Operator / External Secrets Operator / Sealed Secrets
	// 之类的工具把密钥同步进集群的场景，CLI 从头到尾不接触值本身（仅 K8s，语义与
	// ServiceAccountName 一致：只引用、不生成）。与 Password 二选一，两者都写会报错——
	// 已经有一个外部管理的 Secret 时，Password 是多余的、也可能对不上。
	ExistingSecret string    `yaml:"existingSecret,omitempty"`
	Bindings       []Binding `yaml:"bindings,omitempty"`
}

// Binding 把资源绑定到某个组件（003 §5.3、§5.6）。
type Binding struct {
	ComponentID string `yaml:"componentId"`

	// Database / Vhost / Bucket / Index 是**同一格**：这个组件在这个资源里占哪一块。
	//
	// 分成四个名字而不是一个，是因为使用者第一反应写的就是他心里那个词——
	// 接 RabbitMQ 的人会去写 `vhost:`，接 MinIO 的人会去写 `bucket:`。
	// 从前只有 `database` 一个字段兼任四种含义，于是：
	//
	//	写对了（database: orders 当 vhost 用）  读起来是错的，别人看不懂
	//	写了 vhost:                            未知字段 → "是不是想写 database？"
	//	                                       ——一条帮倒忙的建议
	//
	// 哪个 kind 用哪个名字由校验强制（validateBindingSlot），写错了会点名
	// 该用哪个。cache 与 smtp 两格都不用：它们没有"占哪一块"这回事。
	Database string `yaml:"database,omitempty"`
	Vhost    string `yaml:"vhost,omitempty"`
	Bucket   string `yaml:"bucket,omitempty"`
	Index    string `yaml:"index,omitempty"`

	// EnvPrefix 用于一个组件绑定多个同类资源时区分环境变量（如 PRIMARY_ / ARCHIVE_）。
	EnvPrefix string `yaml:"envPrefix,omitempty"`
}

// Slot 返回这条绑定占的那一块，没写时返回空串。
//
// 四个字段互斥（校验保证），所以取到哪个就是哪个。
func (b Binding) Slot() string {
	switch {
	case b.Database != "":
		return b.Database
	case b.Vhost != "":
		return b.Vhost
	case b.Bucket != "":
		return b.Bucket
	case b.Index != "":
		return b.Index
	default:
		return ""
	}
}

// BindingSlotName 返回某种资源类型下"占哪一块"该用的字段名，
// 空串表示这种资源没有这一格。
//
// 与 inject.resourceVars 里那几个变量一一对应：
//
//	database → DATABASE_NAME    mq     → MQ_VHOST
//	storage  → STORAGE_BUCKET   search → SEARCH_INDEX
//	cache / smtp                没有
func BindingSlotName(kind string) string {
	switch kind {
	case ResourceKindDatabase:
		return "database"
	case ResourceKindMQ:
		return "vhost"
	case ResourceKindStorage:
		return "bucket"
	case ResourceKindSearch:
		return "index"
	default:
		return ""
	}
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
