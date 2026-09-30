package manifest

import (
	"sort"
	"strings"
)

// 平台保留变量：平台自己要注入的环境变量名。configSchema 的键就是环境变量名
// ，撞上其中之一，组件就永远拿不到自己的那个值。
//
// 判断只有这一份：up 注入与 lint 据此警告并跳过那一项（组件可能来自不经过市场的安装源），
// 市场发布时据此拒收。它放在 manifest 包里，是因为它是关于 Manifest 的规则——市场借用
// 规则时不必把 CLI 的注入、解析、安装源一起链接进去。
var (
	reservedExact  = []string{"COMPONENT_ID", "COMPONENT_VERSION", "BRICKKIT_SERVED_MEMBERS", "BRICKKIT_SERVED_MEMBERS_CONFIG", "PORT"}
	reservedSuffix = []string{"_ENDPOINT"}
)

// ReservedHit 是撞上保留变量的一个配置项名、它撞上的模式，以及一个避得开的新名字。
type ReservedHit struct {
	Key        string
	Pattern    string
	Suggestion string
}

// ReservedHitFor 判断一个环境变量名是否撞上保留变量。
func ReservedHitFor(key string) (ReservedHit, bool) {
	for _, exact := range reservedExact {
		if key == exact {
			return ReservedHit{Key: key, Pattern: exact, Suggestion: renameSuggestion(key, exact)}, true
		}
	}
	for _, suffix := range reservedSuffix {
		if strings.HasSuffix(key, suffix) {
			pattern := "*" + suffix
			return ReservedHit{Key: key, Pattern: pattern, Suggestion: renameSuggestion(key, pattern)}, true
		}
	}
	return ReservedHit{}, false
}

// ReservedConfigKeys 列出 configSchema 里撞上保留变量的键，按键排序。
func (m *Manifest) ReservedConfigKeys() []ReservedHit {
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
		if hit, ok := ReservedHitFor(key); ok {
			hits = append(hits, hit)
		}
	}
	return hits
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
