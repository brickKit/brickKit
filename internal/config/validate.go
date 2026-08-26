package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// envPrefixRe 是多资源绑定的环境变量前缀规则（003 §5.6）：
// 该值会直接拼进环境变量名（如 PRIMARY_DATABASE_HOST），必须是合法的变量名片段。
var envPrefixRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

const (
	// MinPort / MaxPort 是合法端口范围。
	MinPort = 1
	MaxPort = 65535
)

// Validate 校验 brickkit.yaml 的全部字段，一次返回所有问题。
func (c *Config) Validate() error {
	p := newConfigProblems(c.Source)

	c.validateProject(p)
	c.validateDeploy(p)
	c.validateSources(p)
	c.validateComponents(p)
	c.validateResources(p)
	c.validateResourceEnvCollisions(p)

	return p.Err()
}

func (c *Config) validateProject(p *clierr.ProblemSet) {
	if c.Project == "" {
		p.Missing("project")
		return
	}
	if reason := projectNameProblem(c.Project); reason != "" {
		p.Addf("project", "%s（%s）", reason, ProjectNameRule)
	}
}

func (c *Config) validateDeploy(p *clierr.ProblemSet) {
	switch c.Deploy.Target {
	case "":
		p.Missing("deploy.target")
	case TargetDocker, TargetK8s:
	default:
		p.Addf("deploy.target", "必须是 %s 或 %s（当前是 %s）", TargetDocker, TargetK8s, c.Deploy.Target)
	}

	switch c.Deploy.PodSecurity {
	case "", PodSecurityRestricted:
	default:
		p.Addf("deploy.podSecurity", "目前只支持 %s（当前是 %s）",
			PodSecurityRestricted, c.Deploy.PodSecurity)
	}
	if c.Deploy.Namespace != "" {
		if reason := projectNameProblem(c.Deploy.Namespace); reason != "" {
			p.Addf("deploy.namespace", "%s（K8s 命名空间遵循同样的规则）", reason)
		}
	}
	c.validateNetworkPolicy(p)
	c.validateEgress(p)
}

// validateNetworkPolicy 只查配置本身说得通不通（P26）。
//
// "有 expose: true 的组件却没说 ingress controller 在哪"要等到生成清单时才查得了——
// 那时才知道有哪些组件、谁对外暴露，见 k8s.checkIngressController。
func (c *Config) validateNetworkPolicy(p *clierr.ProblemSet) {
	np := c.Deploy.NetworkPolicy
	if np == nil {
		return
	}

	if np.IngressController != nil && np.IngressController.Namespace == "" {
		// 空命名空间会生成 `kubernetes.io/metadata.name: ""`，那是一条谁也匹配不上的
		// 规则——策略照样 apply 成功，ingress controller 却照样被挡在外面
		p.Missing("deploy.networkPolicy.ingressController.namespace")
	}

	for i, source := range np.AllowFrom {
		field := indexed("deploy.networkPolicy.allowFrom", i)
		// 名字先查：后面的报错要靠它点名是哪一条
		if source.Name == "" {
			p.Missing(field + ".name")
		}
		if source.Namespace == "" {
			// 同上：空命名空间生成的是一条谁也匹配不上的规则，
			// 策略 apply 成功而来源照样被挡，等于什么都没做
			p.Addf(field+".namespace", "缺失（%s 这条放行规则不知道该放行哪个命名空间）",
				sourceLabel(source.Name, i))
		}
		for _, port := range source.Ports {
			if port < 1 || port > 65535 {
				p.Addf(field+".ports", "端口 %d 不合法（应在 1–65535）", port)
			}
		}
	}
}

// validateEgress 查出站目标本身说得通不通（P37）。
//
// "有资源没被覆盖"要等到生成清单时才查得了——那时才知道哪些组件会跑、
// 各自绑了哪些资源，见 k8s.checkEgressCoverage。
func (c *Config) validateEgress(p *clierr.ProblemSet) {
	np := c.Deploy.NetworkPolicy
	if np == nil || np.Egress == nil {
		return
	}

	for i, target := range np.Egress.AllowTo {
		field := indexed("deploy.networkPolicy.egress.allowTo", i)
		if target.Name == "" {
			p.Missing(field + ".name")
		}
		label := sourceLabel(target.Name, i)

		switch {
		case target.Namespace != "" && target.CIDR != "":
			p.Addf(field, "%s 同时写了 namespace 与 cidr，只能写一个"+
				"（集群内目标用 namespace，集群外用 cidr）", label)
		case target.Namespace == "" && target.CIDR == "":
			p.Addf(field, "%s 缺少目标位置：集群内写 namespace，集群外写 cidr", label)
		}

		// 端口只有一个来源。两处各写一份早晚不一致，
		// 而不一致的表现是"策略看着对，组件连不上库"
		if target.Resource != "" && len(target.Ports) > 0 {
			p.Addf(field+".ports",
				"%s 已经写了 resource，端口由平台从 resources[].port 取，不要再写 ports", label)
		}
		for _, port := range target.Ports {
			if port < 1 || port > 65535 {
				p.Addf(field+".ports", "端口 %d 不合法（应在 1–65535）", port)
			}
		}
	}
}

// sourceLabel 在报错里指认某条 allowFrom：有名字就用名字，没有就用下标。
func sourceLabel(name string, index int) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("第 %d 条", index+1)
}

func (c *Config) validateSources(p *clierr.ProblemSet) {
	seen := make(map[string]int)
	for i, s := range c.Sources {
		field := indexed("sources", i)

		if s.ID == "" {
			p.Missing(field + ".id")
		} else if prev, ok := seen[s.ID]; ok {
			p.Addf(field+".id", "与 %s.id 重复（安装源 ID 必须唯一）", indexed("sources", prev))
		} else {
			seen[s.ID] = i
		}

		switch s.Type {
		case "":
			p.Missing(field + ".type")
		case SourceTypeMarket, SourceTypeGit:
			if s.URL == "" {
				p.Addf(field+".url", "缺失（必填字段，%s 类型的安装源必须声明 url）", s.Type)
			}
		case SourceTypeLocal:
			if s.Path == "" {
				p.Add(field+".path", "缺失（必填字段，local 类型的安装源必须声明 path）")
			}
		default:
			p.Addf(field+".type", "必须是 %s / %s / %s 之一（当前是 %s）",
				SourceTypeMarket, SourceTypeGit, SourceTypeLocal, s.Type)
		}
	}
}

func (c *Config) validateComponents(p *clierr.ProblemSet) {
	seen := make(map[string]int)
	localPorts := make(map[int]int)
	exposePorts := make(map[int]int)

	for i, item := range c.Components {
		field := indexed("components", i)

		switch {
		case item.ID == "":
			p.Missing(field + ".id")
		default:
			if reason := manifest.ComponentIDProblem(item.ID); reason != "" {
				p.Add(field+".id", reason)
			}
		}

		switch {
		case item.Version == "":
			p.Missing(field + ".version")
		case !manifest.IsExactVersion(item.Version):
			p.Addf(field+".version", "必须是精确版本 major.minor.patch，不接受 ^ 或 ~ 等范围约束（当前是 %s）", item.Version)
		}

		// 多版本共存是默认行为；同一 ID 同一版本重复才是错误（003 §4.8）。
		if item.ID != "" && item.Version != "" {
			if prev, ok := seen[item.Ref()]; ok {
				p.Addf(field, "与 %s 重复声明了 %s", indexed("components", prev), item.Ref())
			} else {
				seen[item.Ref()] = i
			}
		}

		c.validateComponentPorts(p, field, i, item, localPorts, exposePorts)
		validateReplicas(p, field, item)
	}
}

func (c *Config) validateComponentPorts(
	p *clierr.ProblemSet, field string, index int, item Component,
	localPorts, exposePorts map[int]int,
) {
	// ---- 本地调试（003 §4.4）----
	if item.LocalPort != 0 {
		switch {
		case !item.Local:
			p.Add(field+".localPort", "只在 local: true 时生效，请一并声明 local: true 或删除该字段")
		case item.LocalPort < MinPort || item.LocalPort > MaxPort:
			p.Addf(field+".localPort", "必须在 %d~%d 之间（当前是 %d）", MinPort, MaxPort, item.LocalPort)
		default:
			if prev, ok := localPorts[item.LocalPort]; ok {
				p.Addf(field+".localPort", "与 %s.localPort 冲突（宿主机端口 %d 已被占用）",
					indexed("components", prev), item.LocalPort)
			} else {
				localPorts[item.LocalPort] = index
			}
		}
	}

	// ---- 外部暴露（003 §4.5）----
	if item.ExposePort != 0 {
		switch {
		case !item.Expose:
			p.Add(field+".exposePort", "只在 expose: true 时生效，请一并声明 expose: true 或删除该字段")
		case item.ExposePort < MinPort || item.ExposePort > MaxPort:
			p.Addf(field+".exposePort", "必须在 %d~%d 之间（当前是 %d）", MinPort, MaxPort, item.ExposePort)
		default:
			if prev, ok := exposePorts[item.ExposePort]; ok {
				p.Addf(field+".exposePort", "与 %s.exposePort 冲突（宿主机端口 %d 已被占用）",
					indexed("components", prev), item.ExposePort)
			} else {
				exposePorts[item.ExposePort] = index
			}
		}
	}

	// hostname 仅在 K8s 环境下必填（Ingress 需要域名）。
	if item.TLSSecret != "" && !item.Expose {
		p.Add(field+".tlsSecret", "只有 expose: true 的组件才需要 TLS 证书")
	}
	if item.Expose && item.Hostname == "" && c.Deploy.Target == TargetK8s {
		p.Add(field+".hostname", "缺失（deploy.target: k8s 且 expose: true 时必填，Ingress 需要域名）")
	}
}

func (c *Config) validateResources(p *clierr.ProblemSet) {
	seen := make(map[string]int)
	for i, r := range c.Resources {
		field := indexed("resources", i)

		switch {
		case r.Kind == "":
			p.Missing(field + ".kind")
		case !manifest.IsKnownResourceKind(r.Kind):
			// 不认识的 kind 不能放过去：注入引擎对它无事可做，组件一个
			// 连接变量都拿不到，而 up 一路绿灯、部署文件看上去完全正常
			p.Addf(field+".kind", "不是平台认识的资源类型（当前是 %s）；可选：%s",
				r.Kind, manifest.ResourceKindsText())
		}
		if r.Engine == "" {
			p.Missing(field + ".engine")
		}
		if r.Host == "" {
			p.Missing(field + ".host")
		}

		if r.ID == "" {
			p.Missing(field + ".id")
		} else if prev, ok := seen[r.ID]; ok {
			p.Addf(field+".id", "与 %s.id 重复（资源 ID 必须项目内唯一）", indexed("resources", prev))
		} else {
			seen[r.ID] = i
		}

		switch {
		case r.Port == 0:
			p.Missing(field + ".port")
		case r.Port < MinPort || r.Port > MaxPort:
			p.Addf(field+".port", "必须在 %d~%d 之间（当前是 %d）", MinPort, MaxPort, r.Port)
		}

		for j, b := range r.Bindings {
			bField := fmt.Sprintf("%s.bindings[%d]", field, j)
			// 绑定指向一个 components 里没有的组件**不在这里报错**（见 DanglingBindings）。
			if b.ComponentID == "" {
				p.Missing(bField + ".componentId")
			}
			if b.EnvPrefix != "" && !envPrefixRe.MatchString(b.EnvPrefix) {
				p.Addf(bField+".envPrefix", "必须是大写字母开头的大写字母、数字与下划线（会拼进环境变量名，如 %s_DATABASE_HOST）", b.EnvPrefix)
			}
			validateBindingSlot(p, bField, r.Kind, b)
		}
	}
}

// bindingSlots 是"占哪一块"那四个互斥字段，按 YAML 里的名字。
var bindingSlots = []struct {
	name  string
	value func(Binding) string
}{
	{"database", func(b Binding) string { return b.Database }},
	{"vhost", func(b Binding) string { return b.Vhost }},
	{"bucket", func(b Binding) string { return b.Bucket }},
	{"index", func(b Binding) string { return b.Index }},
}

// validateBindingSlot 保证"占哪一块"用的是这种资源该用的那个名字，且只写一个。
//
// # 为什么要管这件事
//
// 四个字段填的是同一格（见 Binding.Slot）。不管的话有两种静默失效：
//
//	kind: mq 写成 database:     能跑，但读起来是错的——别人看到 `database: orders`
//	                            会以为那是个库名
//	kind: cache 写了 database:  redis 的连接变量里根本没有这一格，写了完全不生效，
//	                            而使用者以为自己指定了什么
//
// 后一种正是这个项目最在意的那类：写了、不报错、不生效（003 §3.2）。
func validateBindingSlot(p *clierr.ProblemSet, field, kind string, b Binding) {
	want := BindingSlotName(kind)

	var written []string
	for _, slot := range bindingSlots {
		if slot.value(b) != "" {
			written = append(written, slot.name)
		}
	}

	switch {
	case len(written) == 0:
		// 没写不是错误：database 名可以不指定（组件自己拼），
		// cache / smtp 本来就没有这一格
		return

	case len(written) > 1:
		p.Addf(field, "%s 填的是同一格（这个组件在资源里占哪一块），只能写一个",
			strings.Join(written, " 与 "))
		return

	case want == "":
		p.Addf(field+"."+written[0],
			"kind: %s 没有这一格——%s 的连接变量里不存在对应的项，写了不会生效（006 §5.2）。"+
				"要给组件传别的东西，用 configSchema 里的配置项（006 §2.1）",
			kind, kind)

	case written[0] != want:
		p.Addf(field+"."+written[0],
			"kind: %s 下这一格叫 %s，不是 %s（注入为 %s）",
			kind, want, written[0], slotEnvVar(kind))
	}
}

// slotEnvVar 是这一格注入成哪个环境变量，用于报错时把话说到底。
func slotEnvVar(kind string) string {
	switch kind {
	case ResourceKindDatabase:
		return "DATABASE_NAME"
	case ResourceKindMQ:
		return "MQ_VHOST"
	case ResourceKindStorage:
		return "STORAGE_BUCKET"
	case ResourceKindSearch:
		return "SEARCH_INDEX"
	default:
		return ""
	}
}

// envClaim 记下"这批连接变量先被谁占了"。
type envClaim struct {
	resourceID string
	// field 是先占者的字段路径，如 resources[0].bindings[1]。
	field string
}

// validateResourceEnvCollisions 拦下"两条绑定抢同一批连接变量"。
//
// # 后果不是"其中一条不生效"，是那个资源整个蒸发
//
// 注入引擎按变量名写表（inject.envBuilder.set），同名后写的直接覆盖先写的。
// 两个 postgresql 都绑给 people/basic、都没写 envPrefix 时，组件拿到的
// DATABASE_* 全部来自配置里靠后的那个——它以为自己连着主库，实际连的是归档库。
// K8s 侧更彻底：只生成靠后那个资源的 Secret，前一个在生成物里一处都不剩。
//
// # 为什么是报错，不是警告
//
// 与悬空绑定（见 DanglingBindings）划的是同一条线，只是落在了另一边：
// 悬空绑定的唯一后果是"那条绑定不生效，其余组件毫发无伤"——无害状态，
// 用致命错误去挡它代价不成比例。而这里组件**连到了错误的库**，
// 没有任何一种解读下使用者想要这个结果。
//
// 同类判例也都是报错，形状完全一致——都是"两个东西抢同一个位子"：
// components[].localPort 重复、components[].exposePort 重复、resources[].id 重复。
//
// # 判据：(组件ID, 生效前缀, kind) 三元组
//
//	不同 kind（database + cache）        DATABASE_* 与 REDIS_*，不冲突
//	一个写了 envPrefix、一个没写         PRIMARY_DATABASE_* 与 DATABASE_*，不冲突
//	                                     （而且是合理写法：默认库 + 附加库）
//	都没写，或都写同一个前缀             ❌ 抢同一批变量名
//
// 同一个资源里对同一组件写两条绑定（`database: a` 与 `database: b`）
// 是同一种事故的另一个入口，同一个判据一并抓住。
func (c *Config) validateResourceEnvCollisions(p *clierr.ProblemSet) {
	seen := map[string]envClaim{}

	for i, r := range c.Resources {
		// kind 不合法时 validateResources 已经报过；这里再报一次只是噪音，
		// 而且那种 kind 压根没有对应的变量前缀，谈不上碰撞
		if !manifest.IsKnownResourceKind(r.Kind) {
			continue
		}
		for j, b := range r.Bindings {
			if b.ComponentID == "" {
				continue
			}
			prefix := strings.ToUpper(b.EnvPrefix)
			key := b.ComponentID + "\x00" + prefix + "\x00" + r.Kind
			field := fmt.Sprintf("%s.bindings[%d]", indexed("resources", i), j)

			first, taken := seen[key]
			if !taken {
				seen[key] = envClaim{resourceID: r.ID, field: field}
				continue
			}
			p.Add(field, envCollisionMessage(first, r, b.ComponentID, prefix))
		}
	}
}

// envCollisionMessage 说清楚"谁和谁抢、抢的是哪几个变量、怎么办"。
//
// 变量名要具体列出来：只说"连接变量冲突"的话，使用者得自己去翻 006 §5.2
// 才知道两条绑定到底哪里重叠了。
func envCollisionMessage(first envClaim, r Resource, componentID, prefix string) string {
	// 拼法必须与注入引擎一致：那边是 strings.ToUpper(envPrefix) + "_" + 变量名。
	// 少一个下划线就会报出 MAINDATABASE_HOST 这种根本不存在的变量名——
	// 一条照着找也找不到的提示，比不给变量名更浪费时间
	vars := manifest.ResourceEnvPrefix(r.Kind)
	if prefix != "" {
		vars = prefix + "_" + vars
	}
	return fmt.Sprintf(
		"与 %s 抢同一批连接变量：组件 %s 同时绑定了 %s 与 %s（都是 %s，%s），"+
			"两者都注入 %s_HOST / %s_PORT / … —— 后者覆盖前者，"+
			"而组件不会察觉自己连错了地方。"+
			"给其中一个加 envPrefix 区分开（如 envPrefix: ARCHIVE，注入为 ARCHIVE_%s_HOST，003 §5.6）；"+
			"只需要一个的话删掉多余的那条绑定",
		first.field, componentID, first.resourceID, r.ID, r.Kind,
		envPrefixText(prefix), vars, vars, manifest.ResourceEnvPrefix(r.Kind))
}

func envPrefixText(prefix string) string {
	if prefix == "" {
		return "都没写 envPrefix"
	}
	return "envPrefix 都是 " + prefix
}

// DanglingBinding 是一条指向未声明组件的资源绑定。
type DanglingBinding struct {
	ResourceID  string
	ComponentID string
}

// DanglingBindings 找出指向 components 中不存在的组件的资源绑定。
//
// # 为什么这是警告而不是错误
//
// 悬空绑定的**唯一**后果是那条绑定不生效——没有组件会读它，注入引擎按组件 ID
// 归集绑定（inject.resourceBindings），归到一个不存在的组件上就是无人认领。
// 拿一个致命错误去挡一个无害状态，代价完全不成比例：
//
//   - `brickkit remove` 之后配置里必然残留这样一条（已由 Edit.RemoveBindings 清掉，
//     但手工编辑、`git revert` 一半、多人合并冲突都能再造出来）。阻断意味着
//     使用者在**任何**命令上都撞同一堵墙，而错误说的是资源配置，
//     与他刚做的事对不上号；
//   - 它还顺带禁掉了一种正常写法：先把资源与绑定声明好，再 `brickkit add` 组件。
//     那顺序完全说得通，却会在 add 之前就报错。
//
// 所以只在 `up` 时说一句"这条绑定没人用"，让使用者自己决定是删掉还是把组件加回来。
func (c *Config) DanglingBindings() []DanglingBinding {
	declared := make(map[string]bool, len(c.Components))
	for _, item := range c.Components {
		declared[item.ID] = true
	}

	var out []DanglingBinding
	for _, r := range c.Resources {
		for _, b := range r.Bindings {
			if b.ComponentID != "" && !declared[b.ComponentID] {
				out = append(out, DanglingBinding{ResourceID: r.ID, ComponentID: b.ComponentID})
			}
		}
	}
	return out
}

// indexed 生成 "field[i]" 形式的字段路径。
func indexed(field string, i int) string {
	return fmt.Sprintf("%s[%d]", field, i)
}

// validateReplicas 校验 `replicas`（005 §5.8，P35 的前置）。
func validateReplicas(p *clierr.ProblemSet, field string, item Component) {
	if item.Replicas == nil {
		return
	}
	name := field + ".replicas"

	if *item.Replicas < 1 {
		p.Addf(name, "必须 >= 1（当前是 %d）。"+
			"要关掉这个组件请用 enabled: false——它会走级联计算并提醒依赖方，"+
			"而 replicas: 0 绕过这一切：依赖方照常启动、照常拿到地址，"+
			"然后连一个不存在的后端", *item.Replicas)
		return
	}
	// 下面这条只在 >= 1 时才有意义：0 已经报过一次，再报只是噪音
	if item.Local {
		p.Add(name, "不能与 local 同时声明：local 是这个组件在你的 IDE 里跑，"+
			"那里只有一个进程")
	}
}
