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
}

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

	for _, node := range graph.Nodes {
		if !states.IsRunning(node.Ref) {
			continue
		}

		component, warnings := buildComponent(node, graph, states, entries[node.Ref], bindings[node.Ref.ID])
		result.Components = append(result.Components, component)
		result.Warnings = append(result.Warnings, warnings...)
	}
	return result, nil
}

// buildComponent 计算单个组件的注入结果。
func buildComponent(
	node *resolver.Node, graph *resolver.Graph, states *cascade.Result,
	entry config.Component, bindings []boundResource,
) (Component, []*clierr.Error) {
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
	warnings := builder.addConfig(m, entry)

	component := Component{
		Ref:       node.Ref,
		Service:   manifest.ServiceName(node.Ref.ID, node.Ref.Version),
		Env:       builder.sorted(),
		Resources: mergeResources(manifestResources(m), entry.Resources),
	}
	return component, warnings
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

// endpoint 拼出依赖地址。容器网络内直接用服务名寻址（005 §4）。
func endpoint(service string, port int) string {
	return fmt.Sprintf("http://%s:%d", service, port)
}

// addConfig 注入组件自身配置，返回保留变量冲突的警告。
//
// 默认值来自**本次要装的这个版本**的 configSchema，因此升级时：
// 新增的配置项自动用新默认值；被删掉的配置项即使 brickkit.yaml 里还留着覆盖
// 也不会注入（静默忽略，不打扰使用者）。
func (b *envBuilder) addConfig(m *manifest.Manifest, entry config.Component) []*clierr.Error {
	if m == nil || m.ConfigSchema == nil {
		return nil
	}

	var warnings []*clierr.Error
	for _, key := range sortedConfigKeys(m.ConfigSchema.Properties) {
		property := m.ConfigSchema.Properties[key]

		value, source := property.Default, SourceConfig
		if override, ok := entry.Config[key]; ok {
			value, source = override, SourceOverride
		}
		if value == nil {
			// 既没有默认值也没有覆盖：不注入空值，让组件自己走"未配置"分支
			continue
		}

		name := EnvVarName(key)
		if pattern, hit := b.matchReserved(name); hit {
			warnings = append(warnings, reservedConflictWarning(b.componentID, key, name, pattern))
			continue
		}
		b.set(Var{Name: name, Value: formatValue(value), Source: source})
	}
	return warnings
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
	var pairs [][2]string
	switch r.Kind {
	case config.ResourceKindDatabase:
		pairs = [][2]string{
			{"DATABASE_HOST", r.Host}, {"DATABASE_PORT", portOf(r)},
			{"DATABASE_NAME", bound.binding.Database},
			{"DATABASE_USER", r.Username}, {"DATABASE_PASSWORD", r.Password},
		}
	case config.ResourceKindCache:
		pairs = [][2]string{
			{"REDIS_HOST", r.Host}, {"REDIS_PORT", portOf(r)}, {"REDIS_PASSWORD", r.Password},
		}
	case config.ResourceKindMQ:
		pairs = [][2]string{
			{"MQ_HOST", r.Host}, {"MQ_PORT", portOf(r)},
			{"MQ_USER", r.Username}, {"MQ_PASSWORD", r.Password},
			{"MQ_VHOST", bound.binding.Database},
		}
	case config.ResourceKindStorage:
		pairs = [][2]string{
			{"STORAGE_ENDPOINT", r.Host}, {"STORAGE_BUCKET", bound.binding.Database},
			{"STORAGE_ACCESS_KEY", r.Username}, {"STORAGE_SECRET_KEY", r.Password},
		}
	case config.ResourceKindSearch:
		pairs = [][2]string{
			{"SEARCH_HOST", r.Host}, {"SEARCH_PORT", portOf(r)},
			{"SEARCH_INDEX", bound.binding.Database},
		}
	case config.ResourceKindSMTP:
		pairs = [][2]string{
			{"SMTP_HOST", r.Host}, {"SMTP_PORT", portOf(r)},
			{"SMTP_USER", r.Username}, {"SMTP_PASSWORD", r.Password},
		}
	}

	out := make([]Var, 0, len(pairs))
	for _, pair := range pairs {
		if pair[1] == "" {
			// 没配的字段不注入空值：组件据此判断"这项没提供"
			continue
		}
		out = append(out, Var{Name: prefix + pair[0], Value: pair[1], Source: SourceResource})
	}
	return out
}

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
