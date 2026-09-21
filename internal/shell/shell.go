// Package shell 计算外壳合并部署（servedBy）分组。
//
// 这是 Docker（internal/compose）与 K8s（internal/k8s）两个渲染器共用的
// 唯一一份校验与合并逻辑——两者过去对结构相似的问题（mode: debug）各自
// 独立实现分支，而 servedBy 明确要求"两边逻辑一致"，同一份逻辑写两遍
// 只会悄悄跑偏，所以单独收进这个包，两边渲染器只消费它的结果。
//
// servedBy 字段本身的语法、自引用、链式嵌套、跟 mode: debug 互斥已经在
// config.Validate 里静态查过（不需要依赖图就能查），这里只做需要依赖图 +
// cascade 结果才能查出来的部分：目标存不存在、有没有在跑、端口撞不撞车、
// 合并后的环境变量撞不撞值（servedBy 设计书 §5-§7）。labels 不参与合并，
// 见 mergeGroup 的注释。
package shell

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
)

// EnvVarServedMembers 是外壳容器上"当前实际收编了哪些成员"的保留变量名
// （设计书 §7）。它是精确匹配的保留变量，登记在
// internal/inject/reserved.go 与 market-server/internal/validator/reserved.go
// 的 reservedExact 里，任何组件的 configSchema 都不能声明出这个变量名。
const EnvVarServedMembers = "BRICKKIT_SERVED_MEMBERS"

// EnvVarServedMembersConfig 是外壳容器上"当前实际收编的每个成员，
// componentId/version/端口，以及每一项 config 对应的环境变量名"的保留
// 变量名（brickKit 反馈：两个降低 servedBy 运维摩擦的架构提案，提案一；
// JSON 形状定稿见 docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md）。
// 跟 BRICKKIT_SERVED_MEMBERS（只有一份名字列表）互补：外壳实现者从这里
// 能拿到装配每个模块所需的索引信息，不需要再自己维护一份容易过期的手工
// JSON。**这份 JSON 不携带 config 的值本身**——每个值都在外壳环境里
// 独立成一条带组件 ID 前缀的变量（mergeGroup 负责合并），这里只给"这个
// key 对应哪个变量名"，避免密钥类 config 值（常以未展开的 ${VAR} 占位符
// 形式存在）被塞进这条 JSON 字符串内部、被 docker compose 自己的全文本
// ${VAR} 替换撑坏结构。
const EnvVarServedMembersConfig = "BRICKKIT_SERVED_MEMBERS_CONFIG"

// SourceServed 标记 BRICKKIT_SERVED_MEMBERS 这条变量的来源，
// 与 inject.SourceEndpoint 等常量同一用途：只是代码里用来区分变量种类的标识，
// 不会显示给使用者，所以取语言中立的英文值，不进消息目录。
const SourceServed = "served"

// Group 是一个外壳与它当前收编的成员。
type Group struct {
	Shell   resolver.Ref
	Members []Member
	// Env 是全部成员贡献的 *_ENDPOINT 类变量，已经和外壳自己的同类变量、
	// 以及成员相互之间做过合并与冲突检查（同名同值跳过，同名不同值报错）。
	Env []inject.Var
	// 注意：这里没有 Labels 字段——成员自己的 labels 不参与合并，
	// 只有外壳自己条目上的 labels 算数，见 mergeGroup 的注释。
}

// Member 是被外壳收编的一个组件。
type Member struct {
	Ref resolver.Ref
	// Port 是该组件自己 component.yaml 声明的 deployment.port。
	Port int
	// ExtraPorts 是该组件自己 component.yaml 声明的 deployment.extraPorts。
	ExtraPorts []manifest.ExtraPort
	// Config 是该组件合并后的自身配置：原始 configSchema key（驼峰形式，
	// 不是转换后的环境变量名）→ 值，来自 inject.Build 已经算好的结果
	// （inject.Var.Key），不重新计算。用于 BRICKKIT_SERVED_MEMBERS_CONFIG。
	Config map[string]string
}

// MemberLabels 返回一个 servedBy 成员自己声明的 labels（component.yaml 的
// deployment.labels 与 brickkit.yaml 覆盖合并后的结果），全空时返回 nil。
//
// 两个渲染器（compose / k8s）在判断"该不该警告 labels 本次不生效"时都调
// 这一个函数，不各写一份——判据必须与"这些 labels 不参与合并"（mergeGroup
// 的注释）是同一件事的两面。
func MemberLabels(m *manifest.Manifest, entry config.Component) map[string]string {
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

// servedMemberConfigEntry 是 BRICKKIT_SERVED_MEMBERS_CONFIG JSON 数组里的
// 一个元素。
//
// 刻意不包含的两类数据：
//   - configSchema 本身（字段类型、默认值定义）——外壳作者编译时就知道
//     自己模块的 schema，这部分不会过期，带上只是净增负载，不解决任何
//     实际问题（真正会过期的只有下面这些值）；
//   - 资源连接变量（DATABASE_* 等）——这些可能标了 inject.Var.SecretKey，
//     该走 K8s Secret（005 §5.6），混进这条明文 JSON 会绕开那层处理。
//     而且提案本身要的也只是"把已经算好的 config 数据交出来"。
//
// ConfigEnvVars 携带的是变量名，不是值——member.Ref.ID 与原始 config key
// 拼出来的那条独立环境变量（mergeGroup 已经把它并入 Group.Env）才是真正
// 的值所在，外壳读这份 JSON 拿变量名，再去自己的进程环境读值。这样即使
// 某个成员的 config 值本身还是未展开的 ${VAR} 占位符（密钥类配置的标准
// 写法），也不会被塞进这条 JSON 字符串内部——那正是 docker compose 自己
// 的全文本 ${VAR} 替换会撑坏 JSON 结构的根源，见
// docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md。
type servedMemberConfigEntry struct {
	ComponentID   string                  `json:"componentId"`
	Version       string                  `json:"version"`
	HTTPPort      int                     `json:"httpPort"`
	ExtraPorts    []servedMemberExtraPort `json:"extraPorts"`
	ConfigEnvVars map[string]string       `json:"configEnvVars"`
}

// ServedMembersConfig 返回这个外壳该写进 BRICKKIT_SERVED_MEMBERS_CONFIG 的
// JSON 值：每个当前收编成员的 componentId/version/端口/合并后 config，
// 按 componentId 字典序排列。零个成员时是 "[]"，不是 "null"——跟
// ServedMembers 的空字符串同一个精神：外壳读到它必须能区分"零个成员"和
// "变量不存在"，不能把前者误当成后者去回退成全部初始化。
func (g Group) ServedMembersConfig() string {
	entries := make([]servedMemberConfigEntry, 0, len(g.Members))
	for _, m := range g.Members {
		ports := make([]servedMemberExtraPort, 0, len(m.ExtraPorts))
		for _, p := range m.ExtraPorts {
			ports = append(ports, servedMemberExtraPort{Name: p.Name, Port: p.Port})
		}
		prefix := manifest.EnvPrefix(m.Ref.ID)
		configEnvVars := make(map[string]string, len(m.Config))
		for key := range m.Config {
			configEnvVars[key] = prefix + "_" + inject.EnvVarName(key)
		}
		entries = append(entries, servedMemberConfigEntry{
			ComponentID: m.Ref.ID, Version: m.Ref.Version, HTTPPort: m.Port,
			ExtraPorts: ports, ConfigEnvVars: configEnvVars,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ComponentID < entries[j].ComponentID })

	// entries 全部由 string/int/slice/map[string]string 拼成，没有 channel、
	// func 这类 json 编不了的类型，也没有自定义 MarshalJSON 会出岔子——
	// 这里的 err 结构性地不可能非 nil。
	out, _ := json.Marshal(entries)
	return string(out)
}

// Apply 把这个外壳分组合并进外壳自己已经算好的环境变量：先放外壳自己的，
// 再把 Group.Env 与 BRICKKIT_SERVED_MEMBERS 逐个 upsert 进去（同名覆盖，
// 不存在则追加），按变量名排序返回，保证生成物稳定可比对。
//
// 两个渲染器（compose / k8s）都调用这一个函数，不各写一份"怎么把
// Group 摊平进环境变量"——这正是这个包存在的理由。
func Apply(shellEnv []inject.Var, g Group) []inject.Var {
	byName := make(map[string]inject.Var, len(shellEnv)+len(g.Env)+1)
	for _, v := range shellEnv {
		byName[v.Name] = v
	}
	for _, v := range g.Env {
		byName[v.Name] = v
	}
	byName[EnvVarServedMembers] = inject.Var{
		Name: EnvVarServedMembers, Value: g.ServedMembers(), Source: SourceServed,
	}
	byName[EnvVarServedMembersConfig] = inject.Var{
		Name: EnvVarServedMembersConfig, Value: g.ServedMembersConfig(), Source: SourceServed,
	}

	out := make([]inject.Var, 0, len(byName))
	for _, v := range byName {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ParseRef 把 "id@version" 拆成 Ref。config.Validate 已经保证一个非空
// 的 servedBy 字符串必然是合法的 id@version（internal/config/validate.go
// 的 validateServedBy），但 ParseRef 本身不做这个假设，格式不对就返回
// false——调用方（Resolve、两个渲染器）据此决定要不要继续处理。
func ParseRef(raw string) (resolver.Ref, bool) {
	id, version, found := strings.Cut(raw, "@")
	if !found || id == "" || version == "" {
		return resolver.Ref{}, false
	}
	return resolver.Ref{ID: id, Version: version}, true
}

// Resolve 计算本次生成里全部的外壳分组。
//
// 出错即返回（不是一次性收集全部问题）：与本包同类的其它生成期校验
// （compose.checkExposePorts、k8s.checkHostnameUnique、
// k8s.localNotSupported）都是这个约定——使用者改一处、重新 up、再看下
// 一个问题，跟本项目"生成器只报第一个问题"的既有习惯保持一致。
func Resolve(
	cfg *config.Config, graph *resolver.Graph, states *cascade.Result, env *inject.Result,
) ([]Group, error) {
	if cfg == nil || graph == nil || states == nil || env == nil {
		return nil, nil
	}

	entries := make(map[resolver.Ref]config.Component, len(cfg.Components))
	for _, c := range cfg.Components {
		entries[resolver.Ref{ID: c.ID, Version: c.Version}] = c
	}
	envByRef := make(map[resolver.Ref]inject.Component, len(env.Components))
	for _, c := range env.Components {
		envByRef[c.Ref] = c
	}

	byShell := map[resolver.Ref][]Member{}
	var order []resolver.Ref

	for _, ref := range states.Running() {
		entry := entries[ref]
		if entry.ServedBy == "" {
			continue
		}
		target, ok := ParseRef(entry.ServedBy)
		if !ok {
			continue // config.Validate 已经挡过格式问题，这里只做防御
		}

		targetNode := graph.Node(target)
		switch {
		case targetNode == nil:
			return nil, shellNotFoundError(ref, target)
		case !states.IsRunning(target):
			return nil, shellNotRunningError(ref, target)
		}

		node := graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		if _, seen := byShell[target]; !seen {
			order = append(order, target)
		}
		byShell[target] = append(byShell[target], Member{
			Ref: ref, Port: node.Manifest.Deployment.Port,
			ExtraPorts: node.Manifest.Deployment.ExtraPorts,
			Config:     memberConfig(envByRef[ref]),
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
		mergedEnv, err := mergeGroup(shellRef, envByRef[shellRef], members, envByRef)
		if err != nil {
			return nil, err
		}
		groups = append(groups, Group{Shell: shellRef, Members: members, Env: mergedEnv})
	}
	return groups, nil
}

// memberConfig 从一个成员已经算好的注入结果里，挑出 SourceConfig/
// SourceOverride 这两类变量，还原成"原始 key → 值"，供
// BRICKKIT_SERVED_MEMBERS_CONFIG 使用。不重新跑一遍 addConfig 那套合并
// 逻辑——inject.Build 早就把默认值/覆盖值都算好了，这里只是换一种形状
// 把它交出去。
func memberConfig(env inject.Component) map[string]string {
	cfg := map[string]string{}
	for _, v := range env.Env {
		if v.Source == inject.SourceConfig || v.Source == inject.SourceOverride {
			cfg[v.Key] = v.Value
		}
	}
	return cfg
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

// mergeGroup 合并外壳自己的端点变量与全部成员的端点变量，同名同值跳过，
// 同名不同值报错（设计书 §6）。
//
// # labels 不在合并范围内
//
// 成员自己声明的 labels（不管是 component.yaml 的 deployment.labels 还是
// brickkit.yaml 的覆盖）一概不参与合并，只有外壳自己条目上的 labels 算数
// ——跟 expose/exposePort/hostname/replicas/resources/serviceAccountName
// 归同一类：这些字段描述的都是"我自己这个容器该怎么部署"，而 servedBy
// 成员没有自己的容器。
//
// 这不是从一开始就这样：最初 labels 跟 env 变量走的是同一套"同名同值跳过，
// 同名不同值报错"逻辑。但 env 变量在合并前已经先按 inject.SourceEndpoint
// 筛过一轮——天然排掉了 COMPONENT_ID/COMPONENT_VERSION 这类"语义上必然
// 因组件而异"的变量；labels 没有这层筛选，而 labels 里恰恰存在这样的键
// （典型例子：component.yaml 常见的 prometheus.io/port，值就是各自的
// 端口号）。真实的多组件收编场景里，这类键之间"同名不同值"是必然出现的
// 常态，却被套用了本该只用来拦真实冲突的规则，导致合并时几乎必然假阳性
// 报错。要不重新发明一份"哪些键语义上必然因组件而异"的名单（这本身就是
// 平台去解释具体 label 键的语义，跟"labels 只透传、平台不解释键值"的
// 立场直接冲突），要么就不再把它当一个可以合并的东西——选的是后者
// （brickKit 反馈：servedBy 的 labels 合并漏了排除规则）。
// servedUnsupportedFieldWarnings 会在成员声明了 labels 时给出提示。
func mergeGroup(
	shellRef resolver.Ref, shellComponent inject.Component, members []Member,
	envByRef map[resolver.Ref]inject.Component,
) ([]inject.Var, *clierr.Error) {
	envValues := map[string]string{}
	envVars := map[string]inject.Var{}
	envOwner := map[string]resolver.Ref{}
	for _, v := range shellComponent.Env {
		if v.Source != inject.SourceEndpoint {
			continue
		}
		envValues[v.Name] = v.Value
		envVars[v.Name] = v
		envOwner[v.Name] = shellRef
	}

	for _, m := range members {
		mEnv := envByRef[m.Ref]
		prefix := manifest.EnvPrefix(m.Ref.ID)
		for _, v := range mEnv.Env {
			switch v.Source {
			case inject.SourceEndpoint:
				if existing, exists := envValues[v.Name]; exists {
					if existing == v.Value {
						continue
					}
					return nil, endpointCollisionError(shellRef, envOwner[v.Name], m.Ref, v.Name, existing, v.Value)
				}
				envValues[v.Name] = v.Value
				envVars[v.Name] = v
				envOwner[v.Name] = m.Ref
			case inject.SourceConfig, inject.SourceOverride:
				// 每个成员自己的 config 值各自生成一条带组件 ID 前缀的
				// 独立变量（跟 *_ENDPOINT 用同一个前缀算法），${VAR} 占位符
				// 语义完全不变，交给 docker compose 自己展开——不摊平进
				// 不带前缀的共享键（那是被否决过的方案，两个模块用同一个
				// 通用 key 名会撞车），也不塞进 BRICKKIT_SERVED_MEMBERS_CONFIG
				// 的 JSON 内部（那正是密钥类 ${VAR} 占位符撑坏 JSON 的根源，
				// 见 docs/superpowers/specs/2026-09-16-servedby-config-env-vars-design.md）。
				name := prefix + "_" + v.Name
				if existing, exists := envValues[name]; exists {
					if existing == v.Value {
						continue
					}
					return nil, configVarCollisionError(shellRef, envOwner[name], m.Ref, name, existing, v.Value)
				}
				v.Name = name
				envValues[name] = v.Value
				envVars[name] = v
				envOwner[name] = m.Ref
			}
		}
	}

	env := make([]inject.Var, 0, len(envVars))
	for _, v := range envVars {
		env = append(env, v)
	}
	sort.Slice(env, func(i, j int) bool { return env[i].Name < env[j].Name })
	return env, nil
}

func shellNotFoundError(member, target resolver.Ref) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellServedByTargetNotFound)).
		WithDetail(i18n.T(msgid.LabelComponent), member.String()).
		WithDetail(i18n.T(msgid.ShellLabelServedByTarget), i18n.T(msgid.ShellServedByTargetNotInProjectDetail, target.String())).
		WithHint(i18n.T(msgid.ShellHintCheckServedByValue))
}

func shellNotRunningError(member, target resolver.Ref) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellServedByTargetNotRunning)).
		WithDetail(i18n.T(msgid.LabelComponent), member.String()).
		WithDetail(i18n.T(msgid.ShellLabelServedByTarget), target.String()).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellNotRunningReasonDetail)).
		WithHint(
			i18n.T(msgid.ShellHintCheckShellEnabled),
			i18n.T(msgid.ShellHintRemoveServedBy),
		)
}

func endpointCollisionError(
	shellRef, firstOwner, secondOwner resolver.Ref, name, firstValue, secondValue string,
) *clierr.Error {
	return clierr.Newf(clierr.CodeConfigInvalid,
		i18n.T(msgid.ShellEndpointCollision, shellRef.String())).
		WithDetail(i18n.T(msgid.ShellLabelVariableName), name).
		WithDetailf(firstOwner.String(), "%s", firstValue).
		WithDetailf(secondOwner.String(), "%s", secondValue).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellEndpointCollisionReasonDetail)).
		WithHint(i18n.T(msgid.ShellHintAlignVersions))
}

// configVarCollisionError 生成"两个成员的 config 值算出了同一个环境变量名，
// 但值不同"的错误——跟 endpointCollisionError 同一个报错形状，原因文案不同：
// 这条变量名由组件 ID 与 config 项名拼出来、不含版本号，最常见的成因是同一个
// 组件 ID 的两个不同版本被同一个外壳收编，且这一项 config 的值不一样。
func configVarCollisionError(
	shellRef, firstOwner, secondOwner resolver.Ref, name, firstValue, secondValue string,
) *clierr.Error {
	return clierr.Newf(clierr.CodeConfigInvalid,
		i18n.T(msgid.ShellConfigVarCollision, shellRef.String())).
		WithDetail(i18n.T(msgid.ShellLabelVariableName), name).
		WithDetailf(firstOwner.String(), "%s", firstValue).
		WithDetailf(secondOwner.String(), "%s", secondValue).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ShellConfigVarCollisionReasonDetail)).
		WithHint(i18n.T(msgid.ShellHintAlignConfigValues))
}
