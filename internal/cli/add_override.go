package cli

// 本文件让 brickkit add 与 brickkit add --local 在真的往 brickkit.yaml 里写了新组件
// 之后，把 override.yaml 也带上（override.yaml 设计书 §9）。override.yaml 是穷举式的
// （AGENTS.md §7.1：项目当前每个组件都该有一行），新组件不出现在里面，使用者会以为
// 它没有覆盖开关，其实只是从来没人给它补线。

import (
	"os"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
)

// syncOverrideAfterAdd 在 add 成功写入新组件之后调用。
//
// override.yaml 不存在：什么都不做——这份机制完全可选，没有它是最常见、完全合法的
// 状态，add 不该替使用者决定要不要开始用它。
// override.yaml 存在、只有裸 id 默认行：直接重新生成，安全——没有真实覆盖可丢，
// 重新生成天然会让外壳嵌套跟着 brickkit.yaml 最新的 servedBy 关系对上，不需要手写
// "往哪一行插一条 id"的补丁逻辑（复用 generateOverride，跟 brickkit override 命令
// 自己刷新时完全同一条路径）。
// override.yaml 存在且有真实覆盖：一个字节都不碰，只打印提醒——静默重新生成有真的
// 丢失自定义值的风险（设计书 §9 自己的原话："couldn't safely handle the new
// component being a shell member without risking clobbering the user's own
// customizations"）。
func syncOverrideAfterAdd(opts *Options, layout config.Layout) error {
	if isNonDefaultConfigRun(opts, layout) {
		return nil
	}

	existing, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}

	if !isOverrideDefaultOnly(existing) {
		opts.Printf("%s\n", i18n.T(msgid.CliAddOverrideYamlNeedsUpdating))
		return nil
	}

	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	fresh := generateOverride(cfg, existing)
	data, err := renderOverride(fresh)
	if err != nil {
		return err
	}
	if err := os.WriteFile(layout.OverridePath(), data, 0o644); err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliOverrideWriteFailed)).
			WithDetail(i18n.T(msgid.LabelPath), layout.OverridePath()).WithCause(err)
	}
	opts.Printf("%s\n", i18n.T(msgid.CliAddOverrideYamlRefreshed))
	return nil
}

// isOverrideDefaultOnly 判断这份 override.yaml 除了穷举式的裸 id 行之外，有没有任何
// 真实覆盖——target、或者任何一个组件/成员的 mode、localPort、baseline。
func isOverrideDefaultOnly(ov *override.Override) bool {
	if ov == nil {
		return true
	}
	if ov.Target != "" || ov.TargetBaseline != "" {
		return false
	}
	for _, c := range ov.Components {
		if c.Mode != "" || c.LocalPort != 0 || c.Baseline != "" {
			return false
		}
		for _, m := range c.Members {
			if m.Mode != "" || m.LocalPort != 0 || m.Baseline != "" {
				return false
			}
		}
	}
	return true
}
