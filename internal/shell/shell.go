// Package shell 计算外壳分组（提案 §8）。
//
// 这是 Docker（internal/compose）与 K8s（internal/k8s）两个渲染器共用的唯一一份外壳逻辑：
// 声明核对（check.go）、谁被哪个外壳承载、以及交给外壳的 BRICKKIT_SERVED_MEMBERS_CONFIG。
// 两边渲染器只消费它的结果，不各自再实现一遍——同一份规则写两遍迟早跑偏。
package shell

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// EnvVarServedMembers 是外壳容器上"这次实际承载了哪些成员"的保留变量：成员的版本化服务名，
// 逗号分隔。登记在 internal/inject/reserved.go 的精确匹配保留名里。
const EnvVarServedMembers = "BRICKKIT_SERVED_MEMBERS"

// EnvVarServedMembersConfig 是外壳容器上装着全部成员配置的保留变量（提案 §8.2）：一个 JSON 数组，
// 每个成员一个对象，config 里是 CLI 已经求好的值（提案 §8.3 场景 A）。它装着成员的密钥，
// 所以按密钥放置：Docker 写进 0600 的 env 文件、K8s 进生成的 Secret（附录 A7），
// 绝不明文出现在 compose.yaml 或 Deployment 里。
const EnvVarServedMembersConfig = "BRICKKIT_SERVED_MEMBERS_CONFIG"

// SourceServed 标记 BRICKKIT_SERVED_MEMBERS 这条变量的来源，
// 与 inject.SourceEndpoint 等常量同一用途：只是代码里用来区分变量种类的标识，
// 不会显示给使用者，所以取语言中立的英文值，不进消息目录。
const SourceServed = "served"

// Group 是一个外壳与它这次承载的成员。
type Group struct {
	Shell   resolver.Ref
	Members []Member
}

// Member 是被外壳承载的一个组件。
type Member struct {
	Ref resolver.Ref
	// Port / ExtraPorts 是该组件自己 component.yaml 声明的端口：外壳进程替它在这些端口上监听。
	Port       int
	ExtraPorts []manifest.ExtraPort
	// Config 是该成员独立运行时会拿到的那份环境（配置项与依赖地址，不含 COMPONENT_ID /
	// COMPONENT_VERSION——那两项是 JSON 的 componentId / version），值已经求好。
	Config map[string]string
}

// MemberLabels 返回一个 servedBy 成员自己声明的 labels（component.yaml 的
// deployment.labels 与 brickkit.yaml 覆盖合并后的结果），全空时返回 nil。
//
// 两个渲染器（compose / k8s）在判断"该不该警告 labels 本次不生效"时都调
// 这一个函数，不各写一份——判据必须与"这些 labels 不参与合并"（mergeGroup
// 的注释）是同一件事的两面。
func MemberLabels(m *manifest.Manifest, entry deployfile.Component) map[string]string {
	var manifestLabels map[string]string
	if m != nil {
		manifestLabels = m.Deployment.Labels
	}
	return manifest.MergeLabels(manifestLabels, entry.Labels)
}

// ServedMembers 返回这个外壳该写进 BRICKKIT_SERVED_MEMBERS 的值：当前
// 收编成员的版本化服务名，逗号分隔、按字典序排列。零个成员时返回空字符
// 串——这个空字符串本身就是"零个成员激活"的信号，外壳读到空字符串必须
// 一个模块都不初始化，不能当成"变量不存在"去回退成全部启动（这两种语义
// 不能合并处理，设计书 §7）。
func (g Group) ServedMembers() string {
	names := make([]string, 0, len(g.Members))
	for _, m := range g.Members {
		names = append(names, manifest.ServiceName(m.Ref.ID, m.Ref.Version))
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// servedMemberExtraPort 是 ServedMembersConfig JSON 里一个成员的额外端口。
// 独立于 manifest.ExtraPort：后者只有 yaml 标签，直接编码会得到大写字段名。
type servedMemberExtraPort struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// servedMemberConfigEntry 是 BRICKKIT_SERVED_MEMBERS_CONFIG JSON 数组里的一个元素（提案 §8.2）。
type servedMemberConfigEntry struct {
	ComponentID string                  `json:"componentId"`
	Version     string                  `json:"version"`
	HTTPPort    int                     `json:"httpPort"`
	ExtraPorts  []servedMemberExtraPort `json:"extraPorts"`
	Config      map[string]string       `json:"config"`
}

// ServedMembersConfig 返回 BRICKKIT_SERVED_MEMBERS_CONFIG 的值，按 componentId 排序。
// 零个成员时是 "[]" 而不是 "null"：外壳必须能分清"零个成员"与"变量不存在"。
func (g Group) ServedMembersConfig() string {
	entries := make([]servedMemberConfigEntry, 0, len(g.Members))
	for _, m := range g.Members {
		ports := make([]servedMemberExtraPort, 0, len(m.ExtraPorts))
		for _, p := range m.ExtraPorts {
			ports = append(ports, servedMemberExtraPort{Name: p.Name, Port: p.Port})
		}
		config := m.Config
		if config == nil {
			config = map[string]string{}
		}
		entries = append(entries, servedMemberConfigEntry{
			ComponentID: m.Ref.ID, Version: m.Ref.Version, HTTPPort: m.Port,
			ExtraPorts: ports, Config: config,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ComponentID < entries[j].ComponentID })

	// entries 只由 string / int / slice / map[string]string 组成，值也都校验过是合法 UTF-8
	// （memberConfig），编码不会失败，也不会改动任何字节。
	out, _ := json.Marshal(entries)
	return string(out)
}

// Apply 把外壳分组的两个保留变量写进外壳自己算好的环境变量（同名覆盖），按变量名排序返回。
// 成员的配置只经由 JSON 交给外壳，不摊进外壳的环境。两个渲染器都调这一个函数。
func Apply(shellEnv []inject.Var, g Group) []inject.Var {
	byName := make(map[string]inject.Var, len(shellEnv)+2)
	for _, v := range shellEnv {
		byName[v.Name] = v
	}
	byName[EnvVarServedMembers] = inject.Var{
		Name: EnvVarServedMembers, Value: inject.Literal(g.ServedMembers()), Source: SourceServed,
	}
	byName[EnvVarServedMembersConfig] = inject.Var{
		Name: EnvVarServedMembersConfig, Key: EnvVarServedMembersConfig,
		Value: inject.Literal(g.ServedMembersConfig()), Source: SourceServed, Secret: true,
		Owner: manifest.ServiceName(g.Shell.ID, g.Shell.Version),
	}

	out := make([]inject.Var, 0, len(byName))
	for _, v := range byName {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Resolve 计算本次生成里全部的外壳分组。
//
// 出错即返回（不是一次性收集全部问题）：与本包同类的其它生成期校验
// （compose.checkExposePorts、k8s.checkHostnameUnique、
// k8s.localNotSupported）都是这个约定——使用者改一处、重新 up、再看下
// 一个问题，跟本项目"生成器只报第一个问题"的既有习惯保持一致。
//
// lookup 是 ${VAR} 的取值来源（进程环境，其次 .env）：成员配置在这里就要求好值。
func Resolve(
	p *project.Project, graph *resolver.Graph, states *cascade.Result, env *inject.Result,
	lookup func(string) (string, bool),
) ([]Group, error) {
	if p == nil || graph == nil || states == nil || env == nil {
		return nil, nil
	}
	if err := Check(p, graph, states); err != nil {
		return nil, err
	}
	envByRef := make(map[resolver.Ref]inject.Component, len(env.Components))
	for _, c := range env.Components {
		envByRef[c.Ref] = c
	}

	byShell := map[resolver.Ref][]Member{}
	var order []resolver.Ref

	for _, ref := range states.Running() {
		// 外壳没跑、成员是裸进程：这个成员不属于任何 Group，交给调用方按普通组件 /
		// 裸进程生成——不是这个函数的错误分支
		target, ok := states.HostOf(p, ref)
		if !ok {
			continue
		}
		if graph.Node(target) == nil {
			return nil, shellNotFoundError(ref, target)
		}

		node := graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		config, err := memberConfig(p, ref, envByRef[ref], lookup)
		if err != nil {
			return nil, err
		}
		if _, seen := byShell[target]; !seen {
			order = append(order, target)
		}
		byShell[target] = append(byShell[target], Member{
			Ref: ref, Port: node.Manifest.Deployment.Port,
			ExtraPorts: node.Manifest.Deployment.ExtraPorts,
			Config:     config,
		})
	}

	sort.Slice(order, func(i, j int) bool { return order[i].String() < order[j].String() })

	groups := make([]Group, 0, len(order))
	for _, shellRef := range order {
		members := byShell[shellRef]
		sort.Slice(members, func(i, j int) bool { return members[i].Ref.String() < members[j].Ref.String() })

		if err := checkPortConflicts(shellRef, graph.Node(shellRef), members); err != nil {
			return nil, err
		}
		groups = append(groups, Group{Shell: shellRef, Members: members})
	}
	return groups, nil
}

// memberConfig 把成员算好的环境（配置项与依赖地址）求成最终的值（提案 §8.3 场景 A）。
//
//	字面量     原样
//	file://    读文件
//	${VAR}     严格展开：引用的每个变量都必须取得到，否则大声失败——字面的 ${VAR}
//	           留在 JSON 里，外壳拿到的就是一段错的值，而且要到运行时才露面
//	existingSecret  拒绝：值只在集群里，CLI 读不到，而 JSON 需要值
//
// 求好的值必须是合法 UTF-8：JSON 只能装文本，非法字节会被悄悄换成替换字符。
func memberConfig(
	p *project.Project, ref resolver.Ref, env inject.Component, lookup func(string) (string, bool),
) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range env.Env {
		if v.Source != inject.SourceConfig && v.Source != inject.SourceEndpoint {
			continue
		}
		value, err := memberValue(p, ref, v, lookup)
		if err != nil {
			return nil, err
		}
		if !utf8.ValidString(value) {
			return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellMemberInvalidUTF8, ref.String(), v.Name)).
				WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellMemberInvalidUTF8Reason)).
				WithHint(i18n.T(msgid.ShellHintInvalidUTF8))
		}
		out[v.Name] = value
	}
	return out, nil
}

func memberValue(
	p *project.Project, ref resolver.Ref, v inject.Var, lookup func(string) (string, bool),
) (string, error) {
	switch v.Value.Kind {
	case configdir.KindSecretRef:
		return "", clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellMemberExistingSecret, ref.String(), v.Name)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellMemberExistingSecretReason)).
			WithHint(i18n.T(msgid.ShellHintExistingSecret))
	case configdir.KindEnvTemplate:
		for _, name := range envref.Names(v.Value.Text) {
			if lookup == nil {
				lookup = func(string) (string, bool) { return "", false }
			}
			if _, ok := lookup(name); !ok {
				return "", clierr.New(clierr.CodeConfigInvalid,
					i18n.T(msgid.ShellMemberValueUnresolved, ref.String(), v.Name, name)).
					WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellMemberValueUnresolvedReason)).
					WithHint(i18n.T(msgid.ShellHintSetVariable, name))
			}
		}
	}
	value, err := configdir.Evaluate(v.Value, p.Layout.Root, lookup)
	if err != nil {
		return "", clierr.As(err).WithDetail(i18n.T(msgid.LabelComponent), ref.String()).
			WithDetail(i18n.T(msgid.ShellLabelVariableName), v.Name)
	}
	return value, nil
}

// checkPortConflicts 校验外壳自己的端口 + 全部成员的端口互不冲突——它们
// 最终都要在同一个容器/Pod 上监听。只查主端口（deployment.port），不查
// extraPorts：一个成员的额外端口撞车，后果是外壳进程自己在那个端口上
// bind 失败、当场崩溃退出——这本身就是一次响亮的失败，不是需要平台在
// 生成阶段replicate 一遍的静默失败，v1 不做这层校验（YAGNI）。
func checkPortConflicts(shellRef resolver.Ref, shellNode *resolver.Node, members []Member) *clierr.Error {
	claimed := map[int]string{}
	claim := func(port int, owner string) *clierr.Error {
		if port == 0 {
			return nil
		}
		if previous, taken := claimed[port]; taken && previous != owner {
			return clierr.Newf(clierr.CodePortConflict,
				i18n.T(msgid.ShellPortConflict, shellRef.String(), port)).
				WithDetail(i18n.T(msgid.ShellLabelPortOwner), previous).
				WithDetail(i18n.T(msgid.ShellLabelPortOwner), owner).
				WithHint(i18n.T(msgid.ShellHintPortsMustDiffer))
		}
		claimed[port] = owner
		return nil
	}

	if shellNode != nil && shellNode.Manifest != nil {
		if err := claim(shellNode.Manifest.Deployment.Port, i18n.T(msgid.ShellOwnerShellItself)); err != nil {
			return err
		}
	}
	for _, m := range members {
		if err := claim(m.Port, i18n.T(msgid.ShellOwnerComponent, m.Ref.String())); err != nil {
			return err
		}
	}
	return nil
}

func shellNotFoundError(member, target resolver.Ref) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellServedByTargetNotFound)).
		WithDetail(i18n.T(msgid.LabelComponent), member.String()).
		WithDetail(i18n.T(msgid.ShellLabelServedByTarget), i18n.T(msgid.ShellServedByTargetNotInProjectDetail, target.String())).
		WithHint(i18n.T(msgid.ShellHintCheckServedByValue))
}
