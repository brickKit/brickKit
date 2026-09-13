// Package shell 计算外壳合并部署（servedBy）分组。
//
// 这是 Docker（internal/compose）与 K8s（internal/k8s）两个渲染器共用的
// 唯一一份校验与合并逻辑——两者过去对结构相似的问题（local: true）各自
// 独立实现分支，而 servedBy 明确要求"两边逻辑一致"，同一份逻辑写两遍
// 只会悄悄跑偏，所以单独收进这个包，两边渲染器只消费它的结果。
//
// servedBy 字段本身的语法、自引用、链式嵌套、跟 local: true 互斥已经在
// config.Validate 里静态查过（不需要依赖图就能查），这里只做需要依赖图 +
// cascade 结果才能查出来的部分：目标存不存在、有没有在跑、端口撞不撞车、
// 合并后的环境变量/labels 撞不撞值（servedBy 设计书 §5-§7）。
package shell

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// EnvVarServedMembers 是外壳容器上"当前实际收编了哪些成员"的保留变量名
// （设计书 §7）。它是精确匹配的保留变量，登记在
// internal/inject/reserved.go 与 market-server/internal/validator/reserved.go
// 的 reservedExact 里，任何组件的 configSchema 都不能声明出这个变量名。
const EnvVarServedMembers = "BRICKKIT_SERVED_MEMBERS"

// SourceServed 标记 BRICKKIT_SERVED_MEMBERS 这条变量的来源，
// 与 inject.SourceEndpoint 等常量同一用途（--verbose 输出、排障）。
const SourceServed = "外壳收编"

// Group 是一个外壳与它当前收编的成员。
type Group struct {
	Shell   resolver.Ref
	Members []Member
	// Env 是全部成员贡献的 *_ENDPOINT 类变量，已经和外壳自己的同类变量、
	// 以及成员相互之间做过合并与冲突检查（同名同值跳过，同名不同值报错）。
	Env []inject.Var
	// Labels 是全部成员贡献的透传标签，合并规则与 Env 相同。
	Labels map[string]string
}

// Member 是被外壳收编的一个组件。
type Member struct {
	Ref resolver.Ref
	// Port 是该组件自己 component.yaml 声明的 deployment.port。
	Port int
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
		byShell[target] = append(byShell[target], Member{Ref: ref, Port: node.Manifest.Deployment.Port})
	}

	sort.Slice(order, func(i, j int) bool { return order[i].String() < order[j].String() })

	groups := make([]Group, 0, len(order))
	for _, shellRef := range order {
		members := byShell[shellRef]
		sort.Slice(members, func(i, j int) bool { return members[i].Ref.String() < members[j].Ref.String() })

		if err := checkPortConflicts(shellRef, graph.Node(shellRef), members); err != nil {
			return nil, err
		}
		mergedEnv, mergedLabels, err := mergeGroup(shellRef, envByRef[shellRef], members, envByRef)
		if err != nil {
			return nil, err
		}
		groups = append(groups, Group{Shell: shellRef, Members: members, Env: mergedEnv, Labels: mergedLabels})
	}
	return groups, nil
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
				"错误：外壳 %s 上有两个组件都要用端口 %d", shellRef.String(), port).
				WithDetail("占用方", previous).
				WithDetail("占用方", owner).
				WithHint("这几个组件最终都跑在同一个外壳容器/Pod 里，端口必须互不相同")
		}
		claimed[port] = owner
		return nil
	}

	if shellNode != nil && shellNode.Manifest != nil {
		if err := claim(shellNode.Manifest.Deployment.Port, "外壳自己"); err != nil {
			return err
		}
	}
	for _, m := range members {
		if err := claim(m.Port, "组件 "+m.Ref.String()); err != nil {
			return err
		}
	}
	return nil
}

// mergeGroup 合并外壳自己的端点变量/透传标签与全部成员的端点变量/标签，
// 同名同值跳过，同名不同值报错（设计书 §6）。
func mergeGroup(
	shellRef resolver.Ref, shellComponent inject.Component, members []Member,
	envByRef map[resolver.Ref]inject.Component,
) ([]inject.Var, map[string]string, *clierr.Error) {
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

	labelValues := map[string]string{}
	labelOwner := map[string]resolver.Ref{}
	for key, value := range shellComponent.Labels {
		labelValues[key] = value
		labelOwner[key] = shellRef
	}

	for _, m := range members {
		mEnv := envByRef[m.Ref]
		for _, v := range mEnv.Env {
			if v.Source != inject.SourceEndpoint {
				continue
			}
			if existing, exists := envValues[v.Name]; exists {
				if existing == v.Value {
					continue
				}
				return nil, nil, endpointCollisionError(shellRef, envOwner[v.Name], m.Ref, v.Name, existing, v.Value)
			}
			envValues[v.Name] = v.Value
			envVars[v.Name] = v
			envOwner[v.Name] = m.Ref
		}
		for key, value := range mEnv.Labels {
			if existing, exists := labelValues[key]; exists {
				if existing == value {
					continue
				}
				return nil, nil, labelCollisionError(shellRef, labelOwner[key], m.Ref, key, existing, value)
			}
			labelValues[key] = value
			labelOwner[key] = m.Ref
		}
	}

	env := make([]inject.Var, 0, len(envVars))
	for _, v := range envVars {
		env = append(env, v)
	}
	sort.Slice(env, func(i, j int) bool { return env[i].Name < env[j].Name })

	var labels map[string]string
	if len(labelValues) > 0 {
		labels = labelValues
	}
	return env, labels, nil
}

func shellNotFoundError(member, target resolver.Ref) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, "错误：servedBy 指向的组件不存在").
		WithDetail("组件", member.String()).
		WithDetailf("servedBy 指向", "%s（当前项目的 components: 里没有这一条）", target.String()).
		WithHint("检查 servedBy 的值有没有写错组件 ID 或版本号")
}

func shellNotRunningError(member, target resolver.Ref) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, "错误：servedBy 指向的外壳当前没有在运行").
		WithDetail("组件", member.String()).
		WithDetail("servedBy 指向", target.String()).
		WithDetail("原因", "这个组件在跑，但它声明的外壳被禁用或没有启动，代码没有地方可以运行").
		WithHint(
			"确认外壳组件没有被 enabled: false 关掉",
			"或者去掉这个组件的 servedBy，让它照常独立部署",
		)
}

func endpointCollisionError(
	shellRef, firstOwner, secondOwner resolver.Ref, name, firstValue, secondValue string,
) *clierr.Error {
	return clierr.Newf(clierr.CodeConfigInvalid,
		"错误：外壳 %s 下两个成员对同一个环境变量给出了不同的值", shellRef.String()).
		WithDetail("变量名", name).
		WithDetailf(firstOwner.String(), "%s", firstValue).
		WithDetailf(secondOwner.String(), "%s", secondValue).
		WithDetail("原因", "这两个组件各自依赖同一个组件 ID 的不同精确版本——独立部署时互不冲突，"+
			"合并进同一个外壳的共享环境后，同一个变量名不可能同时指向两个地址").
		WithHint("让这两个成员依赖同一个精确版本，或者不要把它们放进同一个外壳")
}

func labelCollisionError(
	shellRef, firstOwner, secondOwner resolver.Ref, key, firstValue, secondValue string,
) *clierr.Error {
	return clierr.Newf(clierr.CodeConfigInvalid,
		"错误：外壳 %s 下两个成员对同一个透传标签给出了不同的值", shellRef.String()).
		WithDetail("标签键", key).
		WithDetailf(firstOwner.String(), "%s", firstValue).
		WithDetailf(secondOwner.String(), "%s", secondValue).
		WithHint("给其中一个成员改用不同的标签键，或者统一成同一个值")
}
