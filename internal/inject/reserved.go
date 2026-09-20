package inject

import (
	"strings"
	"unicode"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 平台保留变量（004 §5.6.1）。
//
// 市场发布时也会校验同一套规则（007 §18.1），但那一侧看不到
// `{envPrefix}_*`——envPrefix 是使用者在 brickkit.yaml 里定的。
// 所以注入时必须再防一次：这是最后一道闸。
var (
	reservedExact  = []string{"COMPONENT_ID", "COMPONENT_VERSION", "BRICKKIT_SERVED_MEMBERS", "BRICKKIT_SERVED_MEMBERS_CONFIG"}
	reservedSuffix = []string{"_ENDPOINT"}
	reservedPrefix = []string{"DATABASE_", "REDIS_", "MQ_", "STORAGE_", "SEARCH_", "SMTP_"}
)

// staticReserved 判断环境变量名是否命中保留模式中**不依赖项目配置**的那部分
// （精确匹配、*_ENDPOINT 后缀、资源类型前缀），返回命中的模式。
//
// 与市场发布时校验的是同一套规则（007 §18.1）。它单独成函数，是为了让不在
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
	for _, prefix := range reservedPrefix {
		if strings.HasPrefix(name, prefix) {
			return prefix + "*", true
		}
	}
	return "", false
}

// matchReserved 判断环境变量名是否命中保留模式，返回命中的模式。
func (b *envBuilder) matchReserved(name string) (string, bool) {
	if pattern, hit := staticReserved(name); hit {
		return pattern, true
	}
	for _, prefix := range b.reservedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return prefix + "*", true
		}
	}
	return "", false
}

// ReservedKeyWarnings 检查一份 Manifest 的 configSchema 里有没有配置项名字撞上平台保留变量。
//
// 这是 up 注入时那条警告的离线版，也是它的**超集**：规则同一份（staticReserved），措辞同一份
// （reservedConflictWarning），但 up 只对有值的配置项才走到保留变量检查——有默认值，
// 或者被 brickkit.yaml 的 config 覆盖；既没默认值、又没被覆盖的键，up 根本不会检查它。
// 这里把 configSchema 里声明的每一个键都查一遍，与市场发布时同一个范围。
//
// 不含使用者在 brickkit.yaml 里定的 envPrefix——组件仓库里看不到它，市场发布时同样看不到，
// 所以那一半只能留给注入现场。
//
// 是警告不是错误，理由同 reservedConflictWarning：一个配置项名字写错，不该让整个项目起不来。
func ReservedKeyWarnings(m *manifest.Manifest) []*clierr.Error {
	if m == nil || m.ConfigSchema == nil {
		return nil
	}
	var warnings []*clierr.Error
	for _, key := range sortedConfigKeys(m.ConfigSchema.Properties) {
		name := EnvVarName(key)
		if pattern, hit := staticReserved(name); hit {
			warnings = append(warnings, reservedConflictWarning(m.Metadata.ID, key, name, pattern))
		}
	}
	return warnings
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
			"例如改为 "+renameSuggestion(configKey, pattern),
		)
}

// renameSuggestion 给出一个**真的避得开**这条模式的新名字。
//
// 从前一律建议加 custom 前缀。那对 `DATABASE_*` 这类**前缀**模式有效，
// 对 `*_ENDPOINT` 这类**后缀**模式却完全无效：`customNotifierEndpoint`
// 照样以 _ENDPOINT 结尾，改完再跑还是同一条警告。
// 一条照着做不管用的建议，比不给建议更浪费时间。
func renameSuggestion(configKey, pattern string) string {
	if strings.HasPrefix(pattern, "*") {
		// 后缀模式：得换掉结尾。Endpoint → BaseUrl 是最自然的同义替换
		suffix := strings.TrimPrefix(pattern, "*_")
		camel := strings.ToUpper(suffix[:1]) + strings.ToLower(suffix[1:])
		if trimmed := strings.TrimSuffix(configKey, camel); trimmed != configKey && trimmed != "" {
			return trimmed + "BaseUrl"
		}
		return configKey + "Value"
	}
	return "custom" + strings.ToUpper(configKey[:1]) + configKey[1:]
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
