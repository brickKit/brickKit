package config

import (
	"fmt"
	"regexp"

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
		c.validateExternal(p, field, item)
		validateReplicas(p, field, item)
	}
}

// validateExternal 校验 `external:`（P39）。
//
// 这里拦下的都是"写了不会报错、但也不会生效"的组合——那是最难查的一类问题：
// 使用者改了配置、命令成功了、行为却没变，于是开始怀疑平台。
func (c *Config) validateExternal(p *clierr.ProblemSet, field string, item Component) {
	if item.External == nil {
		return
	}
	ext := field + ".external"

	switch {
	case item.External.Project == "":
		p.Add(ext+".project", "必须写明是哪个项目部署了它——"+
			"平台要用它推导跨项目地址（K8s 命名空间 / Docker 网络）")
	case item.External.Project == c.Project:
		p.Addf(ext+".project", "不能指向本项目（%s）：那等于说"+
			`"我不部署它，但它由我部署"。`+
			"它若真该由本项目部署，去掉 external 即可", c.Project)
	default:
		if err := ValidateProjectName(item.External.Project); err != nil {
			p.Addf(ext+".project", "%s", clierr.As(err).Message)
		}
	}

	// 含义相反：local 是"在我的 IDE 里跑"，external 是"在别人那儿跑"
	if item.Local {
		p.Add(ext, "不能与 local 同时声明：一个说组件在本机调试，"+
			"另一个说组件在别的项目里跑，平台无从选择")
	}
	// expose 是"把我部署的这个暴露出去"；它不由本项目部署，
	// 硬生成会得到一个指向不存在的 Service 的 Ingress（表现为 503，
	// 而人会去查那个组件，查不出任何问题）
	if item.Expose {
		p.Add(ext, "不能与 expose 同时声明：它的入口该由部署它的那个项目决定")
	}
	// 它读的是**对方项目**那份 brickkit.yaml 的配置，在这边写不会有任何效果
	if len(item.Config) > 0 {
		p.Add(ext, "不能与 config 同时声明：它跑在别的项目里，"+
			"读的是那边的配置——写在这里不会生效，而看上去像生效了")
	}
	if item.Resources != nil {
		p.Add(ext, "不能与 resources 同时声明：资源配额由部署它的那个项目决定")
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
	declared := make(map[string]bool, len(c.Components))
	for _, item := range c.Components {
		declared[item.ID] = true
	}

	seen := make(map[string]int)
	for i, r := range c.Resources {
		field := indexed("resources", i)

		if r.Kind == "" {
			p.Missing(field + ".kind")
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
			switch {
			case b.ComponentID == "":
				p.Missing(bField + ".componentId")
			case !declared[b.ComponentID]:
				p.Addf(bField+".componentId", "组件 %s 未在 components 中声明", b.ComponentID)
			}
			if b.EnvPrefix != "" && !envPrefixRe.MatchString(b.EnvPrefix) {
				p.Addf(bField+".envPrefix", "必须是大写字母开头的大写字母、数字与下划线（会拼进环境变量名，如 %s_DATABASE_HOST）", b.EnvPrefix)
			}
		}
	}
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
	// 下面两条只在 >= 1 时才有意义：0 已经报过一次，再报只是噪音
	if item.IsExternal() {
		p.Add(name, "不能与 external 同时声明：副本数由部署它的那个项目决定，"+
			"写在这里不会生效——而看上去像生效了")
	}
	if item.Local {
		p.Add(name, "不能与 local 同时声明：local 是这个组件在你的 IDE 里跑，"+
			"那里只有一个进程")
	}
}
