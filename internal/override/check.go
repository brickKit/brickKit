package override

import (
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// CheckAgainst 校验 override.yaml 相对 brickkit.yaml 的跨文件规则（设计书 §5.2、§4、
// §7 的"悬空条目"）：target 只能降级、生效目标是 k8s 时不许再有 local/debug/target
// 覆盖、每个组件条目都必须能在 brickkit.yaml 里找到对应的组件。一次收集全部问题
// 报出，不遇错即返回——跟 config.Config.Validate() 同一个约定。
//
// ov 为 nil 时总是合法（没有覆盖，谈不上跟 brickkit.yaml 冲突）。
func CheckAgainst(cfg *config.Config, ov *Override) error {
	if ov == nil {
		return nil
	}

	p := clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.OverrideCheckFailed)).
		WithSource(i18n.T(msgid.LabelFile), ov.Source)

	effectiveTarget := cfg.Deploy.Target
	if ov.Target != "" {
		if !isDowngrade(cfg.Deploy.Target, ov.Target) {
			p.Add("target", i18n.T(msgid.OverrideTargetUpgradeRejected, cfg.Deploy.Target, ov.Target))
		}
		effectiveTarget = ov.Target
	}

	known := map[string]bool{}
	for _, c := range cfg.Components {
		known[c.ID] = true
	}

	for _, c := range ov.Components {
		checkEntry(p, c.ID, c.Mode, known, effectiveTarget)
		for _, m := range c.Members {
			checkEntry(p, m.ID, m.Mode, known, effectiveTarget)
		}
	}

	return p.Err()
}

// isDowngrade 判断从 from 切到 to 是不是设计书 §5.2 允许的方向：
// k8s → docker/podman 总是允许；docker/podman → k8s 总是拒绝；
// docker ↔ podman 双向不受限（结构上都只生成 compose 文件，谁都不缺对方需要的字段）。
func isDowngrade(from, to string) bool {
	if to == config.TargetK8s {
		return from == config.TargetK8s
	}
	return true
}

// checkEntry 校验一个条目（顶层组件或嵌套成员）是不是悬空、mode 是不是在生效目标是
// k8s 时被禁止——ComponentOverride 与 MemberOverride 是两个不同的 Go 类型
// （schemagen 的反射生成器不支持自引用递归类型，见 override.go 的类型定义），但
// 这两条规则要看的只是 id/mode 两个值，跟具体类型无关。
func checkEntry(p *clierr.ProblemSet, id, mode string, known map[string]bool, effectiveTarget string) {
	if !known[id] {
		p.Add(id, i18n.T(msgid.OverrideDanglingComponent, id))
	}
	if effectiveTarget == config.TargetK8s && (mode == config.ModeDebug || mode == config.ModeLocal) {
		p.Add(id+".mode", i18n.T(msgid.OverrideModeK8sForbidden, mode, id))
	}
}

// DriftNote 是一条非阻断的漂移提示（设计书 §7）：override.yaml 记录的 baseline
// 与 brickkit.yaml 的当前值对不上了，值得回头看看这份覆盖是否还合理，但从不
// 拦任何命令。
type DriftNote struct {
	// Field 是 "target" 或某个组件 ID，供调用方（brickkit override 的刷新提示、
	// 后续 lint 规则）拼自己的展示格式。
	Field string
	// Message 是面向使用者的一句话说明。
	Message string
}

// Drift 找出 override.yaml 记录的 baseline 与 brickkit.yaml 当前值不一致的地方。
// 从不返回 error——这是提示，不是校验失败。ov 为 nil 时返回空。
func Drift(cfg *config.Config, ov *Override) []DriftNote {
	if ov == nil {
		return nil
	}

	var notes []DriftNote
	if ov.TargetBaseline != "" && ov.TargetBaseline != cfg.Deploy.Target {
		notes = append(notes, DriftNote{
			Field:   "target",
			Message: i18n.T(msgid.OverrideTargetDrift, ov.TargetBaseline, cfg.Deploy.Target),
		})
	}

	current := map[string]string{}
	for _, c := range cfg.Components {
		current[c.ID] = c.Mode
	}
	for _, c := range ov.Components {
		notes = append(notes, driftNoteFor(c.ID, c.Baseline, current)...)
		for _, m := range c.Members {
			notes = append(notes, driftNoteFor(m.ID, m.Baseline, current)...)
		}
	}
	return notes
}

func driftNoteFor(id, baseline string, current map[string]string) []DriftNote {
	if baseline == "" || baseline == current[id] {
		return nil
	}
	return []DriftNote{{
		Field:   id,
		Message: i18n.T(msgid.OverrideComponentDrift, id, baseline, current[id]),
	}}
}
