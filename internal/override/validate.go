package override

import (
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Validate 校验 override.yaml 的单文件级规则：target/mode 取值、组件条目的形状、
// 组件 ID 不重复。跨文件规则（target 降级方向、k8s 下 local/debug/target 不许
// 出现、baseline 漂移）不在这里——那些需要 brickkit.yaml 才能判断，见 CheckAgainst
// 与 Drift（本包另一个文件，设计书 §5.2、§4、§7）。
func (o *Override) Validate() error {
	p := newOverrideProblems(o.Source)

	switch o.Target {
	case "", config.TargetDocker, config.TargetPodman, config.TargetK8s:
	default:
		p.Add("target", i18n.T(msgid.OverrideTargetInvalid, o.Target))
	}

	seen := map[string]bool{}
	for i, c := range o.Components {
		o.validateComponent(p, indexed("components", i), c, seen)
	}

	return p.Err()
}

func (o *Override) validateComponent(p *clierr.ProblemSet, field string, c ComponentOverride, seen map[string]bool) {
	switch {
	case c.ID == "":
		p.Missing(field + ".id")
	case seen[c.ID]:
		p.Add(field+".id", i18n.T(msgid.OverrideDuplicateComponent, c.ID))
	default:
		seen[c.ID] = true
	}

	switch c.Mode {
	case "", config.ModeEnabled, config.ModeDisable, config.ModeDebug, config.ModeLocal:
	default:
		p.Add(field+".mode", i18n.T(msgid.ConfigModeInvalid, c.Mode))
	}

	for j, m := range c.Members {
		o.validateComponent(p, field+".members["+itoa(j)+"]", m, seen)
	}
}
