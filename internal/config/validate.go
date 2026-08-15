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
}

// validateNetworkPolicy 只查配置本身说得通不通（P26）。
//
// "有 expose: true 的组件却没说 ingress controller 在哪"要等到生成清单时才查得了——
// 那时才知道有哪些组件、谁对外暴露，见 k8s.checkIngressController。
func (c *Config) validateNetworkPolicy(p *clierr.ProblemSet) {
	np := c.Deploy.NetworkPolicy
	if np == nil || np.IngressController == nil {
		return
	}

	if np.IngressController.Namespace == "" {
		// 空命名空间会生成 `kubernetes.io/metadata.name: ""`，那是一条谁也匹配不上的
		// 规则——策略照样 apply 成功，ingress controller 却照样被挡在外面
		p.Missing("deploy.networkPolicy.ingressController.namespace")
	}
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
