package inject

import (
	"strings"
	"unicode"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 平台保留变量（004 §5.6.1）。
//
// 市场发布时也会校验同一套规则（007 §18.1），但那一侧看不到
// `{envPrefix}_*`——envPrefix 是使用者在 brickkit.yaml 里定的。
// 所以注入时必须再防一次：这是最后一道闸。
var (
	reservedExact  = []string{"COMPONENT_ID", "COMPONENT_VERSION"}
	reservedSuffix = []string{"_ENDPOINT"}
	reservedPrefix = []string{"DATABASE_", "REDIS_", "MQ_", "STORAGE_", "SEARCH_", "SMTP_"}
)

// matchReserved 判断环境变量名是否命中保留模式，返回命中的模式。
func (b *envBuilder) matchReserved(name string) (string, bool) {
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
	for _, prefix := range reservedPrefix {
		if strings.HasPrefix(name, prefix) {
			return prefix + "*", true
		}
	}
	for _, prefix := range b.reservedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return prefix + "*", true
		}
	}
	return "", false
}

// reservedConflictWarning 生成保留变量冲突的警告（004 §5.6.1 的输出样例）。
//
// 是警告不是错误：报错阻断意味着一个配置项名字写错，整个项目就起不来。
func reservedConflictWarning(componentID, configKey, envVar, pattern string) *clierr.Error {
	return clierr.Warn(clierr.CodeConfigConflict,
		"配置冲突：组件 "+componentID+" 的配置项已被忽略").
		WithDetail("组件", componentID).
		WithDetail("配置项", configKey).
		WithDetail("环境变量名", envVar).
		WithDetail("冲突的保留模式", pattern).
		WithDetail("处理", "该配置项已被忽略，平台注入的值优先").
		WithHint(
			"修改 configSchema 中的配置项名称，避开平台保留变量",
			"例如改为 custom"+strings.ToUpper(configKey[:1])+configKey[1:],
		)
}

// EnvVarName 把配置项名称转成环境变量名（004 §5.6）。
//
//	defaultPageSize → DEFAULT_PAGE_SIZE
//	enableV2Api     → ENABLE_V2_API
//	kebab-case-key  → KEBAB_CASE_KEY
//
// 必须与市场侧（market-server 的 validator.EnvVarName）算法一致：
// 两边不一致会出现"发布时说没冲突、注入时却冲突"的怪事。
func EnvVarName(key string) string {
	var b strings.Builder
	runes := []rune(key)

	for i, r := range runes {
		switch {
		case r == '-' || r == '.' || r == ' ':
			b.WriteRune('_')
		case unicode.IsUpper(r):
			// 小写或数字后面紧跟大写，说明是 camelCase 的词边界
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
