// Package override 实现 override.yaml——本地、可选、默认 gitignore 的部署覆盖文件
// （override.yaml 设计书）。它从不改动 brickkit.yaml，也从不参与 brickkit.yaml 自身的
// 校验：两份文件各自独立解析，跨文件的规则（target 降级方向、k8s 下 local/debug 不许
// 出现、baseline 漂移）单独在 CheckAgainst/Drift 里判断（本包另一个文件）。
package override

// Override 是 override.yaml 的完整结构（设计书 §8）。
type Override struct {
	// Target 覆盖 brickkit.yaml 的 deploy.target，只能是降级方向
	// （设计书 §5.2，跨文件规则见 CheckAgainst）。不写就跟着 deploy.target 走。
	Target string `yaml:"target,omitempty" jsonschema:"enum=docker|podman|k8s"`
	// TargetBaseline 记录上一次确认这份覆盖时 deploy.target 的取值（设计书 §7）。
	// Target 一旦写了，这个字段总会有值——deploy.target 本身是必填字段，
	// 不存在"没写"这回事，跟组件级 Baseline 不是同一种"缺省"。
	TargetBaseline string `yaml:"targetBaseline,omitempty"`
	// Components 是这份覆盖列出的组件条目，穷举式：项目当前有的每个组件都该有
	// 一行（哪怕只是裸 `- id: X`），生成/刷新逻辑负责维持这一点（brickkit override
	// 命令，另一个任务）。
	Components []ComponentOverride `yaml:"components,omitempty"`

	// Source 是该文件的来源路径，只用于错误提示，不参与结构比较。
	Source string `yaml:"-"`
}

// ComponentOverride 是 override.yaml 里一个顶层组件的条目。
//
// 没有 Version 字段——精确到版本的实例是级联自动算出来的：外壳没提供的版本
// 实例强制独立部署，override.yaml 从不代表这件事（设计书 §6.1 最后一条）。
type ComponentOverride struct {
	// ID 是组件 ID（不带版本），必填。
	ID string `yaml:"id"`
	// Mode 覆盖这个组件在 brickkit.yaml 里的 mode。debug 只能在这里出现——
	// brickkit.yaml 自身从今往后拒绝这个取值（设计书 §4）。
	Mode string `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|debug|local"`
	// LocalPort 覆盖 brickkit.yaml 建议的默认端口，或者在 Mode 是 debug 时
	// 提供它唯一的落脚点——debug 从不出现在 brickkit.yaml，它的 localPort
	// 没有别的地方可以写（设计书 §8 结尾）。
	LocalPort int `yaml:"localPort,omitempty"`
	// Baseline 记录上一次确认这份覆盖时 brickkit.yaml 对这个组件声明的 mode
	// （设计书 §7）。只在 brickkit.yaml 当时**已经**显式写了取值时才落笔——
	// 多数组件在 brickkit.yaml 里没有 mode，这个字段因此常常留空，这是刻意的
	// 取舍（设计书 §7 的"Consequence, accepted deliberately"）。
	Baseline string `yaml:"baseline,omitempty"`
	// Members 是被这个组件（作为外壳）合并部署的成员——嵌套纯粹是给人看的
	// 分组，apply 时会整个展平（设计书 §8："避免重复声明我属于外壳 X"）。
	Members []MemberOverride `yaml:"members,omitempty"`
}

// MemberOverride 是嵌套在某个外壳条目下的一个成员，字段跟 ComponentOverride
// 完全一样，只是没有自己的 Members：servedBy 只有"成员声明属于哪个外壳"这一层
// 关系（设计书 §6.1："a member declares servedBy... not the reverse"），平台里
// 不存在"外壳的外壳"，override.yaml 的嵌套因此天然只有一层——这不是绕开限制的
// 权宜写法，是这份文件真实要表达的关系形状本身就是两层，不是无限递归的一层。
type MemberOverride struct {
	ID        string `yaml:"id"`
	Mode      string `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|debug|local"`
	LocalPort int    `yaml:"localPort,omitempty"`
	Baseline  string `yaml:"baseline,omitempty"`
}
