package inject

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 平台保留变量（提案 §5.2）。
//
// 市场发布时拒绝撞上它们的配置项；注入时仍再防一次（警告并跳过那一项）——
// 组件可能来自不经过市场的安装源。
var (
	reservedExact  = []string{"COMPONENT_ID", "COMPONENT_VERSION", "BRICKKIT_SERVED_MEMBERS", "BRICKKIT_SERVED_MEMBERS_CONFIG", "PORT"}
	reservedSuffix = []string{"_ENDPOINT"}
)

// staticReserved 判断环境变量名是否命中保留模式中**不依赖项目配置**的那部分
// （精确匹配、*_ENDPOINT 后缀、资源类型前缀），返回命中的模式。
//
// 市场发布时校验的也是这一份（经 ReservedHits）。它单独成函数，是为了让不在
// 注入现场的调用方——brickkit lint 在组件仓库里检查 Manifest——也能用同一份判断，
// 而不是再抄一遍。
func staticReserved(name string) (string, bool) {
	for _, exact := range reservedExact {
		if name == exact {
			return exact, true
		}
	}
	for _, suffix := range reservedSuffix {
		if strings.HasSuffix(name, suffix) {
			return "*" + suffix, true
		}
	}
	return "", false
}

// ReservedHit 是 configSchema 里撞上保留变量的一个键，以及它撞上的模式。
type ReservedHit struct {
	Key     string
	Pattern string
}

// ReservedHits 列出一份 Manifest 的 configSchema 里撞上平台保留变量的键，按键排序。
//
// 判断只有这一份：up 注入与 lint 据此警告，市场发布时据此拒绝（AGENTS §5.2 的两道防线）。
func ReservedHits(m *manifest.Manifest) []ReservedHit {
	if m == nil || m.ConfigSchema == nil {
		return nil
	}
	keys := make([]string, 0, len(m.ConfigSchema.Properties))
	for key := range m.ConfigSchema.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var hits []ReservedHit
	for _, key := range keys {
		if pattern, hit := staticReserved(key); hit {
			hits = append(hits, ReservedHit{Key: key, Pattern: pattern})
		}
	}
	return hits
}

// ReservedKeyWarnings 检查一份 Manifest 的 configSchema 里有没有键撞上平台保留变量。
//
// 这是 up 注入时那条警告的离线版（brickkit lint 在组件仓库里用），规则同一份
// （staticReserved），措辞同一份（reservedConflictWarning）。configSchema 的键就是
// 环境变量名（附录 A10），不需要任何转换。是警告不是错误：一个配置项名字写错，
// 不该让整个项目起不来。
func ReservedKeyWarnings(m *manifest.Manifest) []*clierr.Error {
	var warnings []*clierr.Error
	for _, hit := range ReservedHits(m) {
		warnings = append(warnings, reservedConflictWarning(m.Metadata.ID, hit.Key, hit.Pattern))
	}
	return warnings
}

// reservedConflictWarning 生成保留变量冲突的警告：平台的值优先，这一项被跳过。
func reservedConflictWarning(componentID, key, pattern string) *clierr.Error {
	return clierr.Warn(clierr.CodeConfigConflict,
		i18n.T(msgid.InjectReservedConflict, componentID)).
		WithDetail(i18n.T(msgid.LabelComponent), componentID).
		WithDetail(i18n.T(msgid.LabelConfigKey), key).
		WithDetail(i18n.T(msgid.InjectLabelReservedPattern), pattern).
		WithDetail(i18n.T(msgid.InjectLabelHandling), i18n.T(msgid.InjectReservedHandlingDetail)).
		WithHint(
			i18n.T(msgid.InjectHintRenameConfigKey),
			i18n.T(msgid.InjectHintRenameExample, renameSuggestion(key, pattern)),
		)
}

// renameSuggestion 给出一个**真的避得开**这条模式的新名字：
// 后缀模式（*_ENDPOINT）换掉结尾——加前缀躲不开后缀；精确名字加 CUSTOM_ 前缀。
func renameSuggestion(key, pattern string) string {
	if suffix, isSuffix := strings.CutPrefix(pattern, "*"); isSuffix {
		if trimmed := strings.TrimSuffix(key, suffix); trimmed != key && trimmed != "" {
			return trimmed + "_BASE_URL"
		}
		return key + "_VALUE"
	}
	return "CUSTOM_" + key
}
