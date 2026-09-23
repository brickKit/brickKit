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
		field := indexed("components", i)
		validateEntry(p, field, c.ID, c.Mode, seen)
		for j, m := range c.Members {
			validateEntry(p, field+".members["+itoa(j)+"]", m.ID, m.Mode, seen)
		}
	}

	return p.Err()
}

// validateEntry 校验一个条目（顶层组件或嵌套成员，二者字段名相同）的 id/mode——
// ComponentOverride 与 MemberOverride 是两个不同的 Go 类型（schemagen 的反射生成器
// 不支持自引用递归类型），但它们要校验的规则完全一样，所以这里只取 id/mode 两个
// 值，不取整个结构体。
func validateEntry(p *clierr.ProblemSet, field, id, mode string, seen map[string]bool) {
	switch {
	case id == "":
		p.Missing(field + ".id")
	case seen[id]:
		p.Add(field+".id", i18n.T(msgid.OverrideDuplicateComponent, id))
	default:
		seen[id] = true
	}

	switch mode {
	case "", config.ModeEnabled, config.ModeDisable, config.ModeDebug, config.ModeLocal:
	default:
		p.Add(field+".mode", i18n.T(msgid.ConfigModeInvalid, mode))
	}
}
