package inject

import (
	"strings"
	"unicode"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
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
			warnings = append(warnings, reservedConflictWarning(m.Metadata.ID, key, name, pattern, nil))
		}
	}
	return warnings
}

// reservedConflictWarning 生成保留变量冲突的警告（004 §5.6.1 的输出样例）。
//
// 是警告不是错误：报错阻断意味着一个配置项名字写错，整个项目就起不来。
//
// extraPrefixes 是使用者在 brickkit.yaml 里为某个资源绑定定的 envPrefix（004 §5.2 的
// `{envPrefix}_*`）——lint 检查独立组件仓库时看不到它（那时还没有项目，传 nil）；
// up 注入时能看到（传 b.reservedPrefixes）。renameSuggestion 本身只核对平台内置的静态
// 规则，是因为它与市场发布时校验的是同一份、必须给出同一个答案（见 TestSuggestionMatchesCLI），
// 而市场在组件发布时同样看不到任何项目的 envPrefix——这是两处永久性的、结构性的盲区，不是没修全。
// 但 up 注入现场是唯一真正拥有完整信息的地方：这里再核对一遍 extraPrefixes，能做到就该做到，
// 不然会出现"建议换了个名字、重跑还是同一条警告"的怪事——`renameSuggestion` 存在的
// 唯一理由就是防止这个。
func reservedConflictWarning(componentID, configKey, envVar, pattern string, extraPrefixes []string) *clierr.Error {
	suggestion := renameSuggestion(configKey, pattern)
	if hasAnyPrefix(EnvVarName(suggestion), extraPrefixes) {
		// 静态规则躲开了，但撞上了这个项目自己定的 {envPrefix}_*——
		// 再包一层 custom 前缀：与 renameSuggestion 前缀分支同样的手法，
		// 对候选名字整体加前缀，不会引入新的后缀类冲突（见该分支的注释）。
		suggestion = "custom" + strings.ToUpper(suggestion[:1]) + suggestion[1:]
	}
	return clierr.Warn(clierr.CodeConfigConflict,
		i18n.T(msgid.InjectReservedConflict, componentID)).
		WithDetail(i18n.T(msgid.LabelComponent), componentID).
		WithDetail(i18n.T(msgid.LabelConfigKey), configKey).
		WithDetail(i18n.T(msgid.InjectLabelEnvVarName), envVar).
		WithDetail(i18n.T(msgid.InjectLabelReservedPattern), pattern).
		WithDetail(i18n.T(msgid.InjectLabelHandling), i18n.T(msgid.InjectReservedHandlingDetail)).
		WithHint(
			i18n.T(msgid.InjectHintRenameConfigKey),
			i18n.T(msgid.InjectHintRenameExample, suggestion),
		)
}

// renameSuggestion 给出一个**真的避得开**这条模式的新名字。
//
// 从前一律建议加 custom 前缀。那对 `DATABASE_*` 这类**前缀**模式有效，
// 对 `*_ENDPOINT` 这类**后缀**模式却完全无效：`customNotifierEndpoint`
// 照样以 _ENDPOINT 结尾，改完再跑还是同一条警告。
// 一条照着做不管用的建议，比不给建议更浪费时间。
//
// 换掉后缀本身也可能撞上**另一种**保留模式：`redisEndpoint` 换成
// `redisBaseUrl` 之后，`REDIS_BASE_URL` 又落进了 `REDIS_*` 这个前缀模式——
// 剩下的词根（redis / database / storage / smtp / mq / search）恰好
// 就是某个资源类型的前缀词，跟 `*_ENDPOINT` 是两条独立的规则，换后缀躲不开
// 前缀。所以候选名字算出来之后要再核对一遍：还撞的话，在词根前面也加上
// custom，两条规则一起避开。
func renameSuggestion(configKey, pattern string) string {
	if !strings.HasPrefix(pattern, "*") {
		return "custom" + strings.ToUpper(configKey[:1]) + configKey[1:]
	}
	// 后缀模式：得换掉结尾。Endpoint → BaseUrl 是最自然的同义替换
	suffix := strings.TrimPrefix(pattern, "*_")
	camel := strings.ToUpper(suffix[:1]) + strings.ToLower(suffix[1:])
	trimmed := strings.TrimSuffix(configKey, camel)
	if trimmed == configKey || trimmed == "" {
		return configKey + "Value"
	}
	if candidate := trimmed + "BaseUrl"; !stillReserved(candidate) {
		return candidate
	}
	return "custom" + strings.ToUpper(trimmed[:1]) + trimmed[1:] + "BaseUrl"
}

// stillReserved 判断按建议改名之后的候选名字是否仍然撞上静态保留模式。
func stillReserved(candidate string) bool {
	_, hit := staticReserved(EnvVarName(candidate))
	return hit
}

// hasAnyPrefix 判断 name 是否以 prefixes 中的任意一个开头。
func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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
