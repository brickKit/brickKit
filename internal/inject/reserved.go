package inject

import (
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// ReservedKeyWarnings 检查一份 Manifest 的 configSchema 里有没有键撞上平台保留变量。
//
// 这是 up 注入时那条警告的离线版（brickkit lint 在组件仓库里用），判断同一份
// （manifest.ReservedHitFor），措辞同一份（reservedConflictWarning）。是警告不是错误：
// 一个配置项名字写错，不该让整个项目起不来。市场发布时用同一个判断直接拒收。
func ReservedKeyWarnings(m *manifest.Manifest) []*clierr.Error {
	var warnings []*clierr.Error
	for _, hit := range m.ReservedConfigKeys() {
		warnings = append(warnings, reservedConflictWarning(m.Metadata.ID, hit))
	}
	return warnings
}

// reservedConflictWarning 生成保留变量冲突的警告：平台的值优先，这一项被跳过。
func reservedConflictWarning(componentID string, hit manifest.ReservedHit) *clierr.Error {
	return clierr.Warn(clierr.CodeConfigConflict,
		i18n.T(msgid.InjectReservedConflict, componentID)).
		WithDetail(i18n.T(msgid.LabelComponent), componentID).
		WithDetail(i18n.T(msgid.LabelConfigKey), hit.Key).
		WithDetail(i18n.T(msgid.InjectLabelReservedPattern), hit.Pattern).
		WithDetail(i18n.T(msgid.InjectLabelHandling), i18n.T(msgid.InjectReservedHandlingDetail)).
		WithHint(
			i18n.T(msgid.InjectHintRenameConfigKey),
			i18n.T(msgid.InjectHintRenameExample, hit.Suggestion),
		)
}
