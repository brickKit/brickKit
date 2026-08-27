package validator

import (
	"strings"
	"unicode"

	"github.com/brickkit/market-server/internal/model"
)

// 平台保留变量规则（007 §18.1 / 004 §5.6.1）。
//
// 注意：`{envPrefix}_*`（多资源前缀）不在这里校验——envPrefix 是**使用者**
// 在 brickkit.yaml 里定的，发布时市场无从知道。那一层由 CLI 注入时的运行期
// 防御负责（004 §5.6.1：警告但跳过）。
var (
	// 精确匹配的保留变量。
	reservedExact = []string{"COMPONENT_ID", "COMPONENT_VERSION"}
	// 后缀匹配：依赖组件地址（含额外端口）。
	reservedSuffix = []string{"_ENDPOINT"}
	// 前缀匹配：各类资源连接信息。
	reservedPrefix = []string{"DATABASE_", "REDIS_", "MQ_", "STORAGE_", "SEARCH_", "SMTP_"}
)

// ReservedConflicts 找出 configSchema 中与平台保留变量冲突的配置项。
//
// 判定按 004 §5.6 的注入规则：配置项名称转成大写下划线后就是环境变量名，
// 因此冲突要在**转换之后**判断，而不是看原始的 camelCase 名字。
func ReservedConflicts(cs *model.ConfigSchema) []model.ReservedConflict {
	if cs == nil || len(cs.Properties) == 0 {
		return nil
	}

	var conflicts []model.ReservedConflict
	for _, key := range sortedKeys(cs.Properties) {
		envVar := EnvVarName(key)
		pattern, hit := matchReserved(envVar)
		if !hit {
			continue
		}
		conflicts = append(conflicts, model.ReservedConflict{
			ConfigKey:       key,
			EnvVarName:      envVar,
			ConflictPattern: pattern,
			Suggestion:      suggestion(key, pattern),
		})
	}
	return conflicts
}

// matchReserved 返回命中的保留模式。
func matchReserved(envVar string) (string, bool) {
	for _, exact := range reservedExact {
		if envVar == exact {
			return exact, true
		}
	}
	for _, suffix := range reservedSuffix {
		if strings.HasSuffix(envVar, suffix) {
			return "*" + suffix, true
		}
	}
	for _, prefix := range reservedPrefix {
		if strings.HasPrefix(envVar, prefix) {
			return prefix + "*", true
		}
	}
	return "", false
}

// EnvVarName 把配置项名称转成环境变量名（004 §5.6）：
//
//	departmentTreeEndpoint → DEPARTMENT_TREE_ENDPOINT
//	default-page-size      → DEFAULT_PAGE_SIZE
func EnvVarName(key string) string {
	var b strings.Builder
	runes := []rune(key)

	for i, r := range runes {
		switch {
		case r == '-' || r == '.' || r == ' ':
			b.WriteRune('_')
		case unicode.IsUpper(r):
			// 小写/数字后面紧跟大写，说明是 camelCase 的词边界
			if i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])) {
				b.WriteRune('_')
			}
			b.WriteRune(r)
		default:
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

// suggestion 给出一个**真的避得开**那条模式的新名字。
//
// 从前一律加 custom 前缀。那对 DATABASE_* 这类**前缀**模式有效，
// 对 *_ENDPOINT 这类**后缀**模式完全无效：customDepartmentTreeEndpoint
// 照样以 _ENDPOINT 结尾，发布者照着改、再发一次、被拒同一条——
// 一条照着做不管用的建议，比不给建议更浪费时间。
//
// ⚠️ 与 CLI 侧 inject.renameSuggestion **必须给出同一个答案**：
// 两处都会对同一个配置项名提建议（市场在发布时拒绝，CLI 在注入时警告），
// 说法不一致会让人以为自己改错了。两个 module 没法共享代码，
// 所以由 TestSuggestionMatchesCLI 钉住同一批用例。
func suggestion(key string, pattern string) string {
	return "请修改配置项名称，避免与平台保留变量冲突（如改为 " + renamed(key, pattern) + "）"
}

// renamed 按模式类型给出新名字：后缀模式要换掉结尾，前缀模式才加前缀。
func renamed(key, pattern string) string {
	if strings.HasPrefix(pattern, "*") {
		// 后缀模式：Endpoint → BaseUrl 是最自然的同义替换
		suffix := strings.TrimPrefix(pattern, "*_")
		camel := strings.ToUpper(suffix[:1]) + strings.ToLower(suffix[1:])
		if trimmed := strings.TrimSuffix(key, camel); trimmed != key && trimmed != "" {
			return trimmed + "BaseUrl"
		}
		return key + "Value"
	}
	return "custom" + strings.ToUpper(key[:1]) + key[1:]
}

// sortedKeys 让冲突列表的顺序稳定，便于测试与排查。
func sortedKeys(properties map[string]model.ConfigProperty) []string {
	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
