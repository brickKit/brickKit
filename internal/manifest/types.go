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

// 基础资源类型（006 §2.1）。
//
// **这份列表是封闭的，而且每一项都对应一组固定的连接变量**（006 §5.2）：
// database→DATABASE_*、cache→REDIS_*、mq→MQ_*、storage→STORAGE_*、
// search→SEARCH_*、smtp→SMTP_*。kind 名字与变量前缀同源不是巧合——
// 平台认识一种 kind，靠的就是"知道该给它注入哪几个变量"。
//
// 因此不认识的 kind 必须**当场报错**，不能放过去：注入引擎对它无事可做，
// 组件一个连接变量都拿不到，而 `up` 一路绿灯、部署文件看上去完全正常，
// 要到运行时才炸。这正是平台最反对的静默失败。
const (
	ResourceKindDatabase = "database"
	ResourceKindCache    = "cache"
	ResourceKindMQ       = "mq"
	ResourceKindStorage  = "storage"
	ResourceKindSearch   = "search"
	ResourceKindSMTP     = "smtp"
)

// ResourceKinds 是全部合法的资源类型，顺序与 006 §2.1 的表一致。
var ResourceKinds = []string{
	ResourceKindDatabase, ResourceKindCache, ResourceKindMQ,
	ResourceKindStorage, ResourceKindSearch, ResourceKindSMTP,
}

// IsKnownResourceKind 判断资源类型是否是平台认识的那几种。
func IsKnownResourceKind(kind string) bool {
	for _, known := range ResourceKinds {
		if kind == known {
			return true
		}
	}
	return false
}

// resourceEnvPrefixes 是每种 kind 注入的连接变量前缀（006 §5.2）。
//
// 只有 cache 与 kind 名不同（REDIS 而不是 CACHE）——那是 006 §5.2 定下的，
// 因为使用者认得的是"Redis 的连接信息"，不是"缓存的连接信息"。
//
// 注入引擎（inject.resourceVars）按同一套前缀生成变量，
// TestResourceVarsMatchDeclaredPrefix 盯着两边不许分叉。
var resourceEnvPrefixes = map[string]string{
	ResourceKindDatabase: "DATABASE",
	ResourceKindCache:    "REDIS",
	ResourceKindMQ:       "MQ",
	ResourceKindStorage:  "STORAGE",
	ResourceKindSearch:   "SEARCH",
	ResourceKindSMTP:     "SMTP",
}

// ResourceEnvPrefix 返回该 kind 注入的连接变量前缀，如 database → DATABASE。
//
// 不认识的 kind 返回空串（那种 kind 在解析阶段就被拦下了，见上方注释）。
func ResourceEnvPrefix(kind string) string { return resourceEnvPrefixes[kind] }

// ResourceKindsText 把合法资源类型拼成一行，用于错误提示。
func ResourceKindsText() string {
	out := ""
	for i, kind := range ResourceKinds {
		if i > 0 {
			out += " / "
		}
		out += kind
	}
	return out
}

// Manifest 是 component.yaml 的完整结构。
type Manifest struct {
	APIVersion   string        `yaml:"apiVersion"`
	Kind         string        `yaml:"kind"`
	Metadata     Metadata      `yaml:"metadata"`
	Tags         []string      `yaml:"tags,omitempty"`
	Artifacts    []Artifact    `yaml:"artifacts,omitempty"`
	Dependencies *Dependencies `yaml:"dependencies,omitempty"`
	ConfigSchema *ConfigSchema `yaml:"configSchema,omitempty"`
	Deployment   Deployment    `yaml:"deployment"`
	Migration    *Migration    `yaml:"migration,omitempty"`
	HealthCheck  HealthCheck   `yaml:"healthCheck"`

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

// DefaultStartPeriodSeconds 是启动宽限期的默认值（002 §9.3）。
//
// # 为什么必须有这一段，以及为什么默认给到 60 秒
//
// 没有它时，平台给组件的启动预算是 interval × failureThreshold = 30 秒——
// **写死的、组件作者改不了的 30 秒**。Spring Boot 冷启动、Django 预加载、
// .NET 首次 JIT 都会超过它，于是：
//
//	Docker  连续三次探测失败 → unhealthy → `up -d --wait` 直接失败，
//	        依赖方卡在 service_healthy 上
//	K8s     livenessProbe 三连失败 → Pod 被 kill → 重启 → 再走一遍同样的 30 秒
//	        → **永久 CrashLoopBackOff**
//
// 而症状极具误导性：容器日志一路正常，最后一行往往正好是"服务已启动"。
// 这与 002 §9.3.1（镜像里没有 wget）是同一种事故，区别在于那一种组件作者
// 能修，这一种他做对了每件事也躲不掉。
//
// 默认值取 60 而不是 30，是因为两个方向的失败代价完全不对称：
// 给多了只是"判定 unhealthy 晚了几十秒"，而且**宽限期不会推迟 healthy**
// ——两秒就绪的组件照样在两秒后转 healthy；给少了是整类组件起不来。
const DefaultStartPeriodSeconds = 60

// HealthCheck 是健康检查声明（002 §9）。
// 注意：/healthz 只检查本进程存活，禁止检查外部依赖（002 §9.4）。
type HealthCheck struct {
	Type string `yaml:"type"`
	Path string `yaml:"path,omitempty"`
	// StartPeriodSeconds 是启动宽限期：这段时间内探测失败不算数（002 §9.3）。
	//
	// 这是 healthCheck 下**唯一**可由组件覆盖的时间参数。interval / timeout /
	// failureThreshold 三个由平台固定：它们管的是"跑起来之后多久发现它死了"，
	// 各个组件之间没有差别；而"我要多久才起得来"是每个组件自己的事实。
	StartPeriodSeconds int `yaml:"startPeriodSeconds,omitempty"`
}

// StartPeriod 返回生效的启动宽限期（秒），没写时取默认值。
func (h HealthCheck) StartPeriod() int {
	if h.StartPeriodSeconds > 0 {
		return h.StartPeriodSeconds
	}
	return DefaultStartPeriodSeconds
}

// 这里曾经有 observability 与 compatibility 两个字段，都已删除。
//
//	observability   `metrics: false` / `tracing: false`。全项目没有任何一处读它，
//	                而且**没有通往消费者的路**：设计书说"未来由可观测性工具组件
//	                读取"，可组件根本读不到别的组件的 Manifest——只有 CLI 有。
//	                真要做可观测性时，需要的多半也不是一个布尔，而是抓取路径与端口
//	                （那已经能用 extraPorts 表达）。
//	compatibility   `minCliVersion`。同样没人读，而它比单纯的死字段更糟——
//	                长得像一道安全闸：写了 minCliVersion: 2.0.0 的组件，
//	                在 0.1.0 的 CLI 上照装不误。
//
// 什么时候把 minCliVersion 加回来：**真的有了两个 CLI 版本、且 Manifest 语义
// 不同**的那一天。在那之前它守不住任何东西。届时也该重新想清楚形状——
// 是"最低 CLI 版本"，还是"我用到了哪些能力"。
//
// 顺带一提：组件用了新版本才有的**字段**时，老 CLI 现在会直接报"未知字段"
// （002 §2.2.1）。那句话对这种情形是误导的（它不是拼写错误），
// 也正是把 minCliVersion 加回来的信号之一。

// IsOptional 返回该依赖是否为弱依赖。
func (d ComponentDep) IsOptional() bool { return d.Optional }
