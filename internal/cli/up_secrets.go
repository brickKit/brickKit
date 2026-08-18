package cli

// 本文件是 `brickkit up` 的明文密钥告警（P5、35.17；006 §3.3、008）。
//
// 泄漏路径不是生成物，而是 **brickkit.yaml 本身**——那个文件是明确建议
// 提交进 Git 的（003 §1.2）。
//
// 一律是警告不是错误：本地开发写个 dev 密码很常见，阻断只会让人绕开 CLI。
// 并且**绝不打印值本身**，那等于把密钥又抄了一遍到终端和 CI 日志里。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// warnHardcodedPasswords 提醒 brickkit.yaml 里写了明文密码。
//
// 是警告不是错误：本地开发写个 dev 密码很常见，阻断只会让人绕开 CLI。
// 警告里**不打印密码本身**——那等于把它又抄了一遍到终端和 CI 日志里。
func warnHardcodedPasswords(opts *Options, cfg *config.Config) {
	var offenders []string
	for _, r := range cfg.Resources {
		if isHardcodedSecret(r) {
			offenders = append(offenders, r.ID)
		}
	}
	if len(offenders) == 0 {
		return
	}

	err := clierr.Warn(clierr.CodeConfigInvalid, "brickkit.yaml 中存在明文密码").
		WithDetail("资源", strings.Join(offenders, "、")).
		WithDetail("要求", "密码必须用 ${ENV_VAR} 引用（006 §3.3、008）").
		WithHint(
			"改成 password: ${POSTGRES_PASSWORD}，并把真实值放进 .env",
			".env 必须在 .gitignore 中",
		)
	renderWarnings(opts, []*clierr.Error{err})
}

// isHardcodedSecret 判断这条资源的密码是不是写死在 brickkit.yaml 里的。
//
// 判据是**原文**写没写 ${ENV_VAR}，不是解析后的值：解析器在读配置时就把
// ${VAR} 展开掉了（003 §5.4），拿展开后的值去判断，只会在变量**真的配了**
// 的时候把使用者骂一顿，而变量漏配时（占位符原样保留）反倒不吭声——
// 正好反了。空值表示没配密码（比如不需要密码的资源），不算问题。
func isHardcodedSecret(r config.Resource) bool {
	if r.PasswordFromEnv || strings.TrimSpace(r.Password) == "" {
		return false
	}
	return true
}

// ============================================================
// 35.17 config 里的明文密钥告警
// ============================================================

// secretishKey 判断一个配置项名字看不看得出是密钥。
//
// **判据刻意收窄，宁可漏报也不误报。** 一个见谁都喊的告警，两天之内就会被
// 所有人无视，那时它连真的密钥也保护不了——所以只认那些几乎不可能有别的
// 含义的词，而不是"包含 key 就算"（apiKey 是密钥，但 sortKey、cacheKey 不是）。
func secretishKey(name string) bool {
	lower := strings.ToLower(name)
	for _, word := range []string{
		"password", "passwd", "secret", "token", "credential",
		"privatekey", "private_key", "apikey", "api_key", "accesskey", "access_key",
	} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// warnConfigSecrets 提醒 component.config 里写了明文密钥（35.17）。
//
// 泄漏路径不是生成物，而是 **brickkit.yaml 本身**：那个文件是明确建议
// 提交进 Git 的（003 §1.2）。写在 resources[].password 里的密码有 P5 告警
// 兜着，写在 config 里的此前一声不吭。
//
// 与 P5 一样是警告不是错误：config 里放什么由使用者决定，
// 平台不该替他判断哪个值算密钥；但看着像密钥的东西必须说一声。
//
// **绝不打印值本身**——那等于把密钥又抄了一遍到终端和 CI 日志里。
func warnConfigSecrets(opts *Options, cfg *config.Config) {
	var offenders []string
	for _, c := range cfg.Components {
		for name := range c.Config {
			// 写成 ${ENV_VAR} 就是做对了。判据取的是**展开前**的原文
			// （config.ConfigFromEnv），理由见那个字段的说明
			if c.ConfigFromEnv[name] || !secretishKey(name) {
				continue
			}
			offenders = append(offenders, c.Ref()+" → "+name)
		}
	}
	if len(offenders) == 0 {
		return
	}
	sort.Strings(offenders)

	renderWarnings(opts, []*clierr.Error{
		clierr.Warn(clierr.CodeConfigInvalid, "brickkit.yaml 的 config 里可能写了明文密钥").
			WithDetail("配置项", strings.Join(offenders, "、")).
			WithDetail("为什么要紧", "brickkit.yaml 是建议提交进 Git 的（003 §1.2），"+
				"写在这里的密钥会跟着进版本库，而且历史里删不掉").
			WithHint(
				"改成 ${MY_TOKEN} 这样的引用，把真实值放进 .env",
				".env 必须在 .gitignore 中",
				"确实不是密钥的话可以忽略这条——判据只看名字，不看值",
			),
	})
}
