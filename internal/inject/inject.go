// Package inject 计算每个组件的环境变量与资源配额（004 §5.6）。
//
// 它只产出"注入什么"，不负责"写到哪里、什么时候求值"——Docker、K8s、本地进程
// 各有一处唯一的判定（compose.envPlacement、k8s.envPlacement、compose.localValue），
// 这样同一套注入规则不会在不同目标里分叉。
//
// 三条贯穿全篇的规则：
//
//   - 变量名基于组件 ID（不带版本），变量值指向版本化服务名；
//   - 弱依赖没启动时**完全不注入**，绝不注入空值；
//   - 配置项与平台保留变量冲突时，警告并跳过，平台的值优先。
package inject

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// CLI 默认资源配额（004 §5.6.2）。
//
// **只有 requests 有默认值，limits 没有。** 没人写就不生成 limits——
// 这是刻意的，两个方向的失败代价完全不对称：
//
//	不设上限   组件用多少是多少。真出问题（内存泄漏）时节点变紧，
//	           kubelet 按 QoS 驱逐，运维看得见、查得出
//	设错上限   平台凭空猜一个数字去 kill 一个跑得好好的组件：
//	           内存超了直接 OOMKilled、CPU 超了被 CFS 限流成 p99 毛刺，
//	           而组件作者从没同意过那个数字
//
// 从前这里默认给 500m / 512Mi。那意味着**任何一个真的需要 600Mi 的组件，
// 都会被一个平台编出来的数字反复 OOMKill**，而配置里一个字都没写过它。
// 这与"平台提供工具，不替人做决定"（001 §12）直接冲突——限额是业务判断，
// 只有写下它的人知道那个数字对不对。
//
// requests 保留默认值，因为它的性质相反：它是**给调度器的提示**，
// 不设会让 Pod 掉进 BestEffort（节点一紧第一个被驱逐），
// 而给一个保守的小值只影响调度密度，不会杀任何东西。
const (
	DefaultRequestCPU    = "100m"
	DefaultRequestMemory = "128Mi"
)

// Var 是一条环境变量。
//
// Value 保留 configdir 的引用种类（字面量 / ${VAR} / file:// / existingSecret）：
// 何时求值、写到哪里由渲染器决定（附录 A6/A7）。
type Var struct {
	Name  string
	Value configdir.Value
	// Source 说明这条变量从哪来（SourcePlatform / SourceEndpoint / SourceConfig）。
	Source string
	// Secret 来自 configSchema 的 secret: true：平台从不按名字猜哪一条是密码。
	Secret bool
	// Key 是原始 configSchema 键。附录 A10 之后它与 Name 相同，保留给外壳 JSON 使用。
	Key string
	// Owner 是配置类变量所属组件的版本化服务名（K8s 据此给生成的 Secret 命名）。
	Owner string
}

// IsSecretRef 表示值是对外部已有 K8s Secret 的引用（{ existingSecret, key }）。
func (v Var) IsSecretRef() bool { return v.Value.Kind == configdir.KindSecretRef }

// Literal 把一个字符串包成字面量值。
func Literal(s string) configdir.Value { return configdir.Value{Kind: configdir.KindLiteral, Text: s} }

// 变量来源。只是代码里区分变量种类的标识，从不显示给使用者。
const (
	SourcePlatform = "platform"
	SourceEndpoint = "endpoint"
	SourceConfig   = "config"
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
	// Labels 是合并后的透传部署元数据；一个键都没有时是 nil。
	Labels map[string]string
}

// EnvMap 把环境变量表转成 名字 → 使用者写下的样子（字面量就是值本身），便于查询与测试。
func (c Component) EnvMap() map[string]string {
	out := make(map[string]string, len(c.Env))
	for _, v := range c.Env {
		out[v.Name] = v.Value.String()
	}
	return out
}

// Result 是整个项目的注入结果。
type Result struct {
	// Components 只包含本次实际启动的组件。
	Components []Component
	// Warnings 是保留变量冲突、未声明的配置键等不阻断的问题。
	Warnings []*clierr.Error
}

// Build 为本次启动的每个组件计算环境变量与资源配额。
func Build(p *project.Project, graph *resolver.Graph, states *cascade.Result) (*Result, error) {
	if p == nil || graph == nil || states == nil {
		return &Result{}, nil
	}

	result := &Result{}
	missing := map[resolver.Ref][]string{}
	for _, node := range graph.Nodes {
		if !states.IsRunning(node.Ref) {
			continue
		}
		component, warnings, lacks, err := buildComponent(p, node, graph, states)
		if err != nil {
			return nil, err
		}
		result.Components = append(result.Components, component)
		result.Warnings = append(result.Warnings, warnings...)
		if len(lacks) > 0 {
			missing[node.Ref] = lacks
		}
	}
	if err := missingRequiredError(p, missing); err != nil {
		return nil, err
	}
	return result, nil
}

// missingRequiredError 把"必填配置项没人给值"变成一条阻断错误。
//
// 组件作者写下 required 又不给默认值，说的正是"这一项我猜不出来"——跨项目服务的
// 地址就是典型。放行的后果是变量根本不出现，组件走进"未配置"分支，而使用者以为
// 配好了：不崩、不报警，只是那一路调用永远走不通。
func missingRequiredError(p *project.Project, missing map[resolver.Ref][]string) *clierr.Error {
	if len(missing) == 0 {
		return nil
	}
	refs := make([]resolver.Ref, 0, len(missing))
	for ref := range missing {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })

	err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.InjectRequiredConfigMissing))
	for _, ref := range refs {
		keys := missing[ref]
		sort.Strings(keys)
		for _, key := range keys {
			err = err.WithDetail(i18n.T(msgid.InjectLabelMissingConfig),
				i18n.T(msgid.InjectMissingConfigDetail, ref.String(), key))
		}
	}
	first := refs[0]
	return err.
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.InjectRequiredReasonDetail)).
		WithHint(
			i18n.T(msgid.InjectHintSetValue, configFileFor(p, first), missing[first][0]),
			i18n.T(msgid.InjectHintEnvVarValue),
		)
}

// configFileFor 是提示里让使用者去写值的那份文件：已有就指它，没有就指该建的那份。
func configFileFor(p *project.Project, ref resolver.Ref) string {
	if f := p.Config(ref.ID, ref.Version); f != nil {
		return f.Path
	}
	version := ""
	if len(p.Decl.Versions(ref.ID)) > 1 {
		version = ref.Version
	}
	return project.DirConfig + "/" + configdir.FileName(ref.ID, version)
}

// buildComponent 计算单个组件的注入结果。
func buildComponent(
	p *project.Project, node *resolver.Node, graph *resolver.Graph, states *cascade.Result,
) (Component, []*clierr.Error, []string, error) {
	m := node.Manifest
	service := manifest.ServiceName(node.Ref.ID, node.Ref.Version)
	builder := &envBuilder{componentID: node.Ref.ID, vars: map[string]Var{}}

	// 1. 平台通用变量
	builder.set(Var{Name: "COMPONENT_ID", Value: Literal(node.Ref.ID), Source: SourcePlatform})
	builder.set(Var{Name: "COMPONENT_VERSION", Value: Literal(node.Ref.Version), Source: SourcePlatform})

	// 2. 依赖地址（强依赖 + 正在启动的弱依赖）
	for _, dep := range append(append([]resolver.Ref{}, node.Requires...), node.Optional...) {
		if !states.IsRunning(dep) {
			// 弱依赖没启动 → 完全不注入（002 §3.4）；强依赖没启动时这个组件自己也不会启动
			continue
		}
		// 被外壳承载的成员没有自己的容器：地址指向外壳（提案 §8.8），端口仍是成员自己的
		host := dep
		if shell, hosted := states.HostOf(p, dep); hosted {
			host = shell
		}
		builder.addEndpoints(dep, host, graph.Node(dep))
	}

	// 3. 组件自身配置：config/ 目录 + vars + schema 默认值（提案 §7.5）
	var schema *manifest.ConfigSchema
	if m != nil {
		schema = m.ConfigSchema
	}
	resolved, err := configdir.Resolve(p.ConfigInput(node.Ref.ID, node.Ref.Version, schema))
	if err != nil {
		return Component{}, nil, nil, err
	}
	warnings := append([]*clierr.Error{}, resolved.Warnings...)
	for _, r := range resolved.Values {
		if pattern, hit := staticReserved(r.Key); hit {
			warnings = append(warnings, reservedConflictWarning(node.Ref.ID, r.Key, pattern))
			continue
		}
		builder.set(Var{
			Name: r.Key, Value: r.Value, Source: SourceConfig,
			Secret: r.Secret, Key: r.Key, Owner: service,
		})
	}

	entry := p.DeployEntry(node.Ref.ID, node.Ref.Version)
	component := Component{
		Ref:       node.Ref,
		Service:   service,
		Env:       builder.sorted(),
		Resources: mergeResources(manifestResources(m), entry.Resources),
		// 部署文件逐键覆盖 component.yaml（004 §5.6.2）
		Labels: manifest.MergeLabels(manifestLabels(m), entry.Labels),
	}
	return component, warnings, resolved.Missing, nil
}

// ============================================================
// 环境变量表
// ============================================================

type envBuilder struct {
	componentID string
	vars        map[string]Var
}

func (b *envBuilder) set(v Var) { b.vars[v.Name] = v }

// addEndpoints 注入依赖组件的主端口与额外端口地址。变量名按依赖 ref 算，主机名用 host
// （依赖本身，或承载它的外壳），端口始终是依赖自己声明的端口。
func (b *envBuilder) addEndpoints(ref, host resolver.Ref, node *resolver.Node) {
	if node == nil || node.Manifest == nil {
		return
	}
	service := manifest.ServiceName(host.ID, host.Version)
	prefix := manifest.EnvPrefix(ref.ID)

	b.set(Var{
		Name:   manifest.EndpointEnvVar(ref.ID),
		Value:  Literal(endpoint(service, node.Manifest.Deployment.Port)),
		Source: SourceEndpoint,
	})
	for _, extra := range node.Manifest.Deployment.ExtraPorts {
		b.set(Var{
			Name:   prefix + "_" + strings.ToUpper(extra.Name) + "_ENDPOINT",
			Value:  Literal(endpoint(service, extra.Port)),
			Source: SourceEndpoint,
		})
	}
}

func endpoint(service string, port int) string {
	return fmt.Sprintf("http://%s:%d", service, port)
}

// sorted 返回按变量名排序的环境变量表：map 的遍历顺序是随机的，
// 直接输出会让生成的部署文件每次都不一样。
func (b *envBuilder) sorted() []Var {
	out := make([]Var, 0, len(b.vars))
	for _, v := range b.vars {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ============================================================
// 资源配额合并（004 §5.6.2）
// ============================================================

// mergeResources 按 brickkit.yaml > component.yaml > CLI 默认值 合并配额。
//
// 逐字段合并而不是整块覆盖：使用者常常只想调大内存，
// 不该因此把组件推荐的 CPU 配额一起丢掉。
func mergeResources(recommended, override *manifest.Resources) manifest.Resources {
	defaults := &manifest.ResourceSpec{CPU: DefaultRequestCPU, Memory: DefaultRequestMemory}

	return manifest.Resources{
		Requests: mergeSpec(defaults, specOf(recommended, true), specOf(override, true)),
		// limits 没有兜底那一层：两边都没写就是**不设上限**，
		// 生成物里连 limits 这一段都不会出现
		Limits: mergeSpec(specOf(recommended, false), specOf(override, false)),
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
//
// 一层都没写出东西时返回 nil，而不是一个两个字段都空的结构体——
// 渲染器据此判断"这一段要不要生成"。limits 全靠它：没人写 limits 时，
// compose 的 deploy.resources.limits 与 K8s 的 resources.limits
// **整段不出现**，而不是出现一个空对象。
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
	if out.CPU == "" && out.Memory == "" {
		return nil
	}
	return out
}

func manifestResources(m *manifest.Manifest) *manifest.Resources {
	if m == nil {
		return nil
	}
	return m.Deployment.Resources
}

func manifestLabels(m *manifest.Manifest) map[string]string {
	if m == nil {
		return nil
	}
	return m.Deployment.Labels
}
