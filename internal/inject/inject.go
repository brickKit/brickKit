// Package inject 计算每个组件的环境变量与资源配额（004 §5.6、006 §5）。
//
// 它只产出"注入什么"，不负责"写到哪里"——Docker compose 与 K8s 清单
// 各自渲染（Step 12 / 13）。这样同一套注入规则不会在两种目标里分叉。
//
// 三条贯穿全篇的规则：
//
//   - 变量名基于组件 ID（不带版本），变量值指向版本化服务名；
//   - 弱依赖没启动时**完全不注入**，绝不注入空值；
//   - 使用者的 config 与平台保留变量冲突时，警告并跳过，平台的值优先。
package inject

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// CLI 默认资源配额（004 §5.6.2 第 4 条）。
const (
	DefaultRequestCPU    = "100m"
	DefaultRequestMemory = "128Mi"
	DefaultLimitCPU      = "500m"
	DefaultLimitMemory   = "512Mi"
)

// Var 是一条环境变量。
type Var struct {
	Name  string
	Value string
	// Source 说明这条变量从哪来，供 --verbose 输出与排障使用。
	Source string
	// ResourceID 是它来自哪个基础资源；只有 SourceResource 的变量有值。
	ResourceID string
	// SecretKey 非空表示这是一条敏感变量（密码 / 密钥），
	// 值就是它在 K8s Secret 里的 key（005 §5.6）。
	//
	// 由注入引擎标记而不是让渲染器按变量名猜：谁生成的谁最清楚哪一条是密码，
	// 靠 `strings.HasSuffix(name, "_PASSWORD")` 去猜，早晚会漏掉一种资源。
	SecretKey string
}

// IsSecret 表示这条变量是密码或密钥，不能明文写进部署清单。
func (v Var) IsSecret() bool { return v.SecretKey != "" }

// 变量来源。
const (
	SourcePlatform = "平台"
	SourceEndpoint = "依赖地址"
	SourceResource = "资源连接"
	SourceConfig   = "组件配置"
	SourceOverride = "配置覆盖"
)

// Component 是一个组件的注入结果。
type Component struct {
	Ref resolver.Ref
	// Service 是版本化服务名（002 §5.3）。
	Service string
	// Env 按变量名排序，保证生成的部署文件稳定可比对。
	Env []Var
	// Resources 是合并后的资源配额（004 §5.6.2）。
	Resources manifest.Resources
}

// EnvMap 把环境变量表转成 map，便于查询。
func (c Component) EnvMap() map[string]string {
	out := make(map[string]string, len(c.Env))
	for _, v := range c.Env {
		out[v.Name] = v.Value
	}
	return out
}

// Result 是整个项目的注入结果。
type Result struct {
	// Components 只包含本次实际启动的组件。
	Components []Component
	// Warnings 是保留变量冲突等不阻断的问题（⚠️，退出码 0）。
	Warnings []*clierr.Error
}

// Build 为本次启动的每个组件计算环境变量与资源配额。
func Build(cfg *config.Config, graph *resolver.Graph, states *cascade.Result) (*Result, error) {
	if graph == nil || states == nil {
		return &Result{}, nil
	}

	entries := configEntries(cfg)
	bindings := resourceBindings(cfg)
	result := &Result{}
	missing := map[string][]string{}

	for _, node := range graph.Nodes {
		if !states.IsRunning(node.Ref) {
			continue
		}

		component, warnings, lacks := buildComponent(
			node, graph, states, entries[node.Ref], bindings[node.Ref.ID])
		result.Components = append(result.Components, component)
		result.Warnings = append(result.Warnings, warnings...)
		if len(lacks) > 0 {
			missing[node.Ref.String()] = lacks
		}
	}
	if err := missingRequiredError(missing); err != nil {
		return nil, err
	}
	return result, nil
}

// missingRequiredError 把"必填配置项没人给值"变成一条阻断错误。
//
// # 为什么是阻断，不是警告
//
// 组件作者写下 required 又不给默认值，说的正是"这一项我猜不出来"。
// 最典型的就是**跨项目服务的地址**（003 §4.9）：那台服务归别的项目管，
// 平台推导不出它在哪，只能由项目填。
//
// 放行的后果是变量根本不出现，组件看到的是"未配置"——而使用者以为配好了。
// 这与漏绑数据库是同一类失败：不崩、不报警，只是那一路调用永远走不通。
func missingRequiredError(missing map[string][]string) *clierr.Error {
	if len(missing) == 0 {
		return nil
	}
	refs := make([]string, 0, len(missing))
	for ref := range missing {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	err := clierr.New(clierr.CodeConfigInvalid, "错误：必填的组件配置没有值")
	for _, ref := range refs {
		keys := missing[ref]
		sort.Strings(keys)
		for _, key := range keys {
			err = err.WithDetailf("缺少配置", "%s → %s（注入为 %s）", ref, key, EnvVarName(key))
		}
	}
	first := refs[0]
	firstKey := missing[first][0]
	return err.
		WithDetail("原因", "组件在 configSchema.required 里声明了它，又没有给默认值——"+
			"这一项平台推导不出来，只能由项目提供").
		WithHint(
			"在 brickkit.yaml 里给它一个值：\n"+
				"    components:\n"+
				"      - id: "+strings.SplitN(first, "@", 2)[0]+"\n"+
				"        config:\n"+
				"          "+firstKey+": <值>",
			"值里可以写 ${ENV_VAR}，真值放 .env（003 §4.6）",
		)
}

// buildComponent 计算单个组件的注入结果。
func buildComponent(
	node *resolver.Node, graph *resolver.Graph, states *cascade.Result,
	entry config.Component, bindings []boundResource,
) (Component, []*clierr.Error, []string) {
	m := node.Manifest
	builder := &envBuilder{
		componentID: node.Ref.ID,
		vars:        map[string]Var{},
		// 使用者定的资源前缀也是保留的：市场发布时看不到它们，
		// 只有读完 brickkit.yaml 才知道（004 §5.6.1）
		reservedPrefixes: envPrefixesOf(bindings),
	}

	// 1. 平台通用变量
	builder.set(Var{Name: "COMPONENT_ID", Value: node.Ref.ID, Source: SourcePlatform})
	builder.set(Var{Name: "COMPONENT_VERSION", Value: node.Ref.Version, Source: SourcePlatform})

	// 2. 依赖地址（强依赖 + 正在启动的弱依赖）
	for _, dep := range append(append([]resolver.Ref{}, node.Requires...), node.Optional...) {
		if !states.IsRunning(dep) {
			// 弱依赖没启动 → 完全不注入（002 §3.4）；
			// 强依赖没启动的情况下这个组件自己也不会启动，走不到这里
			continue
		}
		builder.addEndpoints(dep, graph.Node(dep))
	}

	// 3. 资源连接
	for _, bound := range bindings {
		for _, v := range resourceVars(bound) {
			builder.set(v)
		}
	}

	// 4. 组件自身配置（configSchema 默认值 + brickkit.yaml 覆盖）
	warnings, missing := builder.addConfig(m, entry)

	component := Component{
		Ref:       node.Ref,
		Service:   manifest.ServiceName(node.Ref.ID, node.Ref.Version),
		Env:       builder.sorted(),
		Resources: mergeResources(manifestResources(m), entry.Resources),
	}
	return component, warnings, missing
}

// ============================================================
// 环境变量表
// ============================================================

type envBuilder struct {
	componentID string
	vars        map[string]Var
	// reservedPrefixes 是使用者定义的资源前缀（PRIMARY_ / ARCHIVE_ …）。
	// 它们只有在读完 brickkit.yaml 之后才知道，市场发布时无从校验。
	reservedPrefixes []string
}

func (b *envBuilder) set(v Var) { b.vars[v.Name] = v }

// addEndpoints 注入依赖组件的主端口与额外端口地址。
func (b *envBuilder) addEndpoints(ref resolver.Ref, node *resolver.Node) {
	if node == nil || node.Manifest == nil {
		return
	}
	service := manifest.ServiceName(ref.ID, ref.Version)
	prefix := manifest.EnvPrefix(ref.ID)

	b.set(Var{
		Name:   manifest.EndpointEnvVar(ref.ID),
		Value:  endpoint(service, node.Manifest.Deployment.Port),
		Source: SourceEndpoint,
	})
	for _, extra := range node.Manifest.Deployment.ExtraPorts {
		b.set(Var{
			Name:   prefix + "_" + strings.ToUpper(extra.Name) + "_ENDPOINT",
			Value:  endpoint(service, extra.Port),
			Source: SourceEndpoint,
		})
	}
}

func endpoint(service string, port int) string {
	return fmt.Sprintf("http://%s:%d", service, port)
}

// addConfig 注入组件自身配置，返回保留变量冲突的警告。
//
// 默认值来自**本次要装的这个版本**的 configSchema，因此升级时：
// 新增的配置项自动用新默认值；被删掉的配置项即使 brickkit.yaml 里还留着覆盖
// 也不会注入（静默忽略，不打扰使用者）。
func (b *envBuilder) addConfig(m *manifest.Manifest, entry config.Component) ([]*clierr.Error, []string) {
	if m == nil {
		return nil, nil
	}
	if m.ConfigSchema == nil {
		// 组件根本没声明 configSchema，而项目写了 config —— 整块蒸发。
		// 这是最常撞的一种：先写个最小组件跑通，再想让它可配置，
		// 直觉是去 brickkit.yaml 加 config，而正确做法是先回 component.yaml
		// 加 configSchema。不说一句的话，没有任何一处会告诉他
		if len(entry.Config) > 0 {
			return []*clierr.Error{noConfigSchemaWarning(b.componentID, entry.Config)}, nil
		}
		return nil, nil
	}

	required := map[string]bool{}
	for _, name := range m.ConfigSchema.Required {
		required[name] = true
	}

	var warnings []*clierr.Error
	var missing []string
	for _, key := range sortedConfigKeys(m.ConfigSchema.Properties) {
		property := m.ConfigSchema.Properties[key]

		value, source := property.Default, SourceConfig
		if override, ok := entry.Config[key]; ok {
			value, source = override, SourceOverride
		}
		if value == nil {
			// 既没有默认值也没有覆盖。
			//
			// 声明了 required 的：这是**必须拦下来**的一种。组件作者写 required
			// 又不给默认值，说的正是"这一项我猜不出来，必须由项目告诉我"——
			// 跨项目服务的地址就是典型（003 §4.9）。不拦的话变量根本不会出现，
			// 组件看到的是"未配置"，而使用者以为自己已经配好了。
			if required[key] {
				missing = append(missing, key)
			}
			// 没声明 required 的：不注入空值，让组件自己走"未配置"分支
			continue
		}

		name := EnvVarName(key)
		if pattern, hit := b.matchReserved(name); hit {
			warnings = append(warnings, reservedConflictWarning(b.componentID, key, name, pattern))
			continue
		}
		b.set(Var{Name: name, Value: formatValue(value), Source: source})
	}

	warnings = append(warnings, b.unknownConfigWarnings(m.ConfigSchema, entry.Config)...)
	return warnings, missing
}

// unknownConfigWarnings 提醒"你写的这个配置项，组件的 configSchema 里没有"。
//
// # 为什么这不违反"configSchema 是说明书，不是安检机"
//
// 012 §2.12 拒绝的是**校验值**（类型、枚举、范围），理由的地基是那一句
// "两种方式都能让用户发现错误"——类型填错了，组件拿到 "abc" 去 int()
// 会崩，用户一定会发现。
//
// 对**键名**填错，这句话整个不成立：没有任何运行时失败可以兜底。
// 变量根本不出现，组件走进 os.environ.get(k, 默认值) 的默认分支，
// 一切正常运行——只是不按你配的运行。用户永远不会"在运行时发现问题"，
// 他只会某天疑惑为什么改了配置没效果。
//
// §2.12 担心的滑坡（type → enum → minimum → pattern）也不适用：
// 那些都是 JSON Schema 的**约束**，开一个口子就得追下去；
// 而"这个键在 properties 里有没有"不是约束，是 yamlcheck.Walk 对结构体
// 字段做的同一件事，一次检查，没有下一步。
//
// # 为什么不在解析 brickkit.yaml 时查
//
// 那时候 CLI 手上根本没有 Manifest（brickkit.yaml 可以在 add 之前就写好），
// 任何检查都只能瞎猜。003 §10.1 规则 10 把 `components[].config` 列为
// 未知字段检查的例外，正是这个原因——那条例外是对的，不用动。
// 检查放在这里：up 的时候 Manifest 已经读进来了。
//
// # 为什么是警告不是错误
//
// 与保留变量冲突（004 §5.6.1）同一条线：一个配置项名字写错就整个项目
// 起不来，代价不成比例。而且它还覆盖第三种情形——升级后新版本删掉了
// 某个配置项，而 brickkit.yaml 里还留着覆盖（002 §7.9）。那也是
// "你写的这行不起作用"，同样值得说一句，但绝不该阻断升级。
func (b *envBuilder) unknownConfigWarnings(
	schema *manifest.ConfigSchema, overrides map[string]any,
) []*clierr.Error {
	if len(overrides) == 0 {
		return nil
	}

	known := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		known = append(known, name)
	}
	sort.Strings(known)

	var out []*clierr.Error
	for _, key := range sortedOverrideKeys(overrides) {
		if _, declared := schema.Properties[key]; declared {
			continue
		}
		out = append(out, unknownConfigWarning(b.componentID, key, known))
	}
	return out
}

func sortedOverrideKeys(overrides map[string]any) []string {
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// unknownConfigWarning 是"这一项不会生效"的提醒。
//
// "是不是想写 X"用的是 yamlcheck 里那一份实现（前缀 + 编辑距离），
// 与未知字段提示同一段代码——两处不可能给出不同的答案。
func unknownConfigWarning(componentID, key string, known []string) *clierr.Error {
	w := clierr.Warn(clierr.CodeConfigInvalid,
		"config 里有配置项不会生效：组件 "+componentID+" 的 "+key).
		WithDetail("组件", componentID).
		WithDetail("配置项", key)

	reason := "组件的 configSchema 里没有这一项"
	if guess := yamlcheck.Closest(key, known); guess != "" {
		reason += "，是不是想写 " + guess + "？"
	}
	w = w.WithDetail("原因", reason).
		WithDetail("影响", "这一项不会被注入任何环境变量；组件会使用它自己的默认值")

	if len(known) > 0 {
		w = w.WithDetail("组件声明的配置项", strings.Join(known, "、"))
	}
	return w.WithTip("组件升级后删掉了这一项时也会看到这条——" +
		"那说明这行覆盖从此不起作用了，可以清掉（002 §7.9）")
}

// noConfigSchemaWarning 提醒"这个组件压根没有可配置项"。
func noConfigSchemaWarning(componentID string, overrides map[string]any) *clierr.Error {
	keys := sortedOverrideKeys(overrides)
	return clierr.Warn(clierr.CodeConfigInvalid,
		"config 整块不会生效：组件 "+componentID+" 没有声明 configSchema").
		WithDetail("组件", componentID).
		WithDetailf("被忽略的配置项", "%s（共 %d 项）", strings.Join(keys, "、"), len(keys)).
		WithDetail("影响", "一项都不会被注入任何环境变量").
		WithHint("要让它可配置，先在组件的 component.yaml 里加 configSchema（002 §6.5）").
		WithTip("平台只注入 configSchema 里声明过的配置项——" +
			"没有声明，就没有对应的环境变量")
}

// sorted 返回按变量名排序的环境变量表。
//
// map 的遍历顺序是随机的，直接输出会让生成的部署文件每次都不一样：
// git diff 全是噪音，也没法判断"这次到底改了什么"。
func (b *envBuilder) sorted() []Var {
	out := make([]Var, 0, len(b.vars))
	for _, v := range b.vars {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// formatValue 把配置值转成字符串。
//
// CLI **不校验类型**（004 §5.6 关键规则 4）：configSchema 是配置说明书，
// 不是安检机。使用者把 integer 填成字符串，就原样注入。
func formatValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		// YAML/JSON 里的整数常常被解析成 float64，别输出 20.000000
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	default:
		return fmt.Sprint(value)
	}
}

func sortedConfigKeys(properties map[string]manifest.ConfigProperty) []string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ============================================================
// 资源连接（006 §5）
// ============================================================

// boundResource 是"某个资源绑定到了某个组件"这件事。
type boundResource struct {
	resource config.Resource
	binding  config.Binding
}

// resourceBindings 按组件 ID 归集资源绑定。
func resourceBindings(cfg *config.Config) map[string][]boundResource {
	out := map[string][]boundResource{}
	if cfg == nil {
		return out
	}
	for _, resource := range cfg.Resources {
		for _, binding := range resource.Bindings {
			out[binding.ComponentID] = append(out[binding.ComponentID],
				boundResource{resource: resource, binding: binding})
		}
	}
	return out
}

// resourceVars 生成一个资源的连接变量（006 §5.2）。
func resourceVars(bound boundResource) []Var {
	prefix := ""
	if bound.binding.EnvPrefix != "" {
		prefix = strings.ToUpper(bound.binding.EnvPrefix) + "_"
	}

	r := bound.resource
	var pairs []resourceVar
	switch r.Kind {
	case config.ResourceKindDatabase:
		pairs = []resourceVar{
			{name: "DATABASE_HOST", value: r.Host}, {name: "DATABASE_PORT", value: portOf(r)},
			{name: "DATABASE_NAME", value: bound.binding.Database},
			{name: "DATABASE_USER", value: r.Username},
			{name: "DATABASE_PASSWORD", value: r.Password, secretKey: secretKeyPassword},
		}
	case config.ResourceKindCache:
		pairs = []resourceVar{
			{name: "REDIS_HOST", value: r.Host}, {name: "REDIS_PORT", value: portOf(r)},
			{name: "REDIS_PASSWORD", value: r.Password, secretKey: secretKeyPassword},
		}
	case config.ResourceKindMQ:
		pairs = []resourceVar{
			{name: "MQ_HOST", value: r.Host}, {name: "MQ_PORT", value: portOf(r)},
			{name: "MQ_USER", value: r.Username},
			{name: "MQ_PASSWORD", value: r.Password, secretKey: secretKeyPassword},
			{name: "MQ_VHOST", value: bound.binding.Database},
		}
	case config.ResourceKindStorage:
		pairs = []resourceVar{
			{name: "STORAGE_ENDPOINT", value: r.Host},
			{name: "STORAGE_BUCKET", value: bound.binding.Database},
			{name: "STORAGE_ACCESS_KEY", value: r.Username},
			{name: "STORAGE_SECRET_KEY", value: r.Password, secretKey: secretKeySecretKey},
		}
	case config.ResourceKindSearch:
		pairs = []resourceVar{
			{name: "SEARCH_HOST", value: r.Host}, {name: "SEARCH_PORT", value: portOf(r)},
			{name: "SEARCH_INDEX", value: bound.binding.Database},
		}
	case config.ResourceKindSMTP:
		pairs = []resourceVar{
			{name: "SMTP_HOST", value: r.Host}, {name: "SMTP_PORT", value: portOf(r)},
			{name: "SMTP_USER", value: r.Username},
			{name: "SMTP_PASSWORD", value: r.Password, secretKey: secretKeyPassword},
		}
	}

	out := make([]Var, 0, len(pairs))
	for _, pair := range pairs {
		if pair.value == "" {
			// 没配的字段不注入空值：组件据此判断"这项没提供"
			continue
		}
		out = append(out, Var{
			Name: prefix + pair.name, Value: pair.value, Source: SourceResource,
			ResourceID: r.ID, SecretKey: pair.secretKey,
		})
	}
	return out
}

// resourceVar 是资源连接变量的一条声明。
type resourceVar struct {
	name  string
	value string
	// secretKey 非空表示这是敏感字段，值是它在 K8s Secret 里的 key。
	secretKey string
}

// K8s Secret 中的 key（005 §5.6）。
const (
	secretKeyPassword  = "password"
	secretKeySecretKey = "secret-key"
)

func portOf(r config.Resource) string {
	if r.Port == 0 {
		return ""
	}
	return fmt.Sprint(r.Port)
}

// ============================================================
// 资源配额合并（004 §5.6.2）
// ============================================================

// mergeResources 按 brickkit.yaml > component.yaml > CLI 默认值 合并配额。
//
// 逐字段合并而不是整块覆盖：使用者常常只想调大内存，
// 不该因此把组件推荐的 CPU 配额一起丢掉。
func mergeResources(recommended, override *manifest.Resources) manifest.Resources {
	defaults := manifest.Resources{
		Requests: &manifest.ResourceSpec{CPU: DefaultRequestCPU, Memory: DefaultRequestMemory},
		Limits:   &manifest.ResourceSpec{CPU: DefaultLimitCPU, Memory: DefaultLimitMemory},
	}

	return manifest.Resources{
		Requests: mergeSpec(defaults.Requests, specOf(recommended, true), specOf(override, true)),
		Limits:   mergeSpec(defaults.Limits, specOf(recommended, false), specOf(override, false)),
	}
}

func specOf(r *manifest.Resources, requests bool) *manifest.ResourceSpec {
	if r == nil {
		return nil
	}
	if requests {
		return r.Requests
	}
	return r.Limits
}

// mergeSpec 按优先级从低到高叠加，空字符串表示"没写"。
func mergeSpec(layers ...*manifest.ResourceSpec) *manifest.ResourceSpec {
	out := &manifest.ResourceSpec{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		if layer.CPU != "" {
			out.CPU = layer.CPU
		}
		if layer.Memory != "" {
			out.Memory = layer.Memory
		}
	}
	return out
}

func manifestResources(m *manifest.Manifest) *manifest.Resources {
	if m == nil {
		return nil
	}
	return m.Deployment.Resources
}

// configEntries 按组件引用归集 brickkit.yaml 中的条目。
func configEntries(cfg *config.Config) map[resolver.Ref]config.Component {
	out := map[resolver.Ref]config.Component{}
	if cfg == nil {
		return out
	}
	for _, c := range cfg.Components {
		out[resolver.Ref{ID: c.ID, Version: c.Version}] = c
	}
	return out
}

// envPrefixesOf 收集该组件绑定的资源前缀（PRIMARY_ / ARCHIVE_ …）。
func envPrefixesOf(bindings []boundResource) []string {
	var out []string
	for _, bound := range bindings {
		if bound.binding.EnvPrefix != "" {
			out = append(out, strings.ToUpper(bound.binding.EnvPrefix)+"_")
		}
	}
	return out
}
