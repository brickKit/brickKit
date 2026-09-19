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
	"github.com/brickkit/brickkit/internal/resolver"
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
		WithDetail("要求", "密码必须用 ${ENV_VAR} 引用").
		WithHint(
			"改成 password: ${POSTGRES_PASSWORD}，并把真实值放进 .env",
			".env 必须在 .gitignore 中",
		)
	renderWarnings(opts, []*clierr.Error{err})
}

// isHardcodedSecret 判断这条资源的密码是不是写死在 brickkit.yaml 里的。
//
// 判据是**原文**写没写 ${ENV_VAR}：config.deferredRefs 让 password 解析时不展开，
// 值本身就是原文，不需要再额外记一份"展开前的样子"。空值表示没配密码
// （比如不需要密码的资源），不算问题。
func isHardcodedSecret(r config.Resource) bool {
	if isEnvRef(r.Password) || strings.TrimSpace(r.Password) == "" {
		return false
	}
	return true
}

// isEnvRef 判断一个配置值写的是不是 ${ENV_VAR} 引用。
//
// config.deferredRefs 让这两处字段解析时不展开，所以值本身就是原文——
// 不再需要"展开前先记下"的补丁字段。
func isEnvRef(v any) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, "${")
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

// declaredSecretKeys 返回组件在 configSchema 里声明了 secret: true 的配置项。
//
// 名字启发式（secretishKey）宁可漏报也不误报，只配拿来发警告；
// 组件作者亲口声明的事实比它准得多，所以两者取并集。
// graph 里没有这个组件（还没 add、Manifest 读不到）时返回 nil，退回按名字判断。
func declaredSecretKeys(graph *resolver.Graph, c config.Component) map[string]bool {
	if graph == nil {
		return nil
	}
	node := graph.Node(resolver.Ref{ID: c.ID, Version: c.Version})
	if node == nil || node.Manifest == nil || node.Manifest.ConfigSchema == nil {
		return nil
	}
	out := map[string]bool{}
	for key, property := range node.Manifest.ConfigSchema.Properties {
		if property.Secret {
			out[key] = true
		}
	}
	return out
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
// 判据是：组件声明了 secret: true，或名字长得像密钥（只看名字，不看值）。
//
// **绝不打印值本身**——那等于把密钥又抄了一遍到终端和 CI 日志里。
func warnConfigSecrets(opts *Options, cfg *config.Config, graph *resolver.Graph) {
	var offenders []string
	for _, c := range cfg.Components {
		declared := declaredSecretKeys(graph, c)
		for name, value := range c.Config {
			// 写成 ${ENV_VAR} 就是做对了，existingSecret 引用也是——解析时这两处
			// 都不展开/不改写（config.deferredRefs），所以值本身就是原文/原形状
			_, _, isExistingSecretRef := config.ExistingSecretRef(value)
			if isEnvRef(value) || isExistingSecretRef || (!declared[name] && !secretishKey(name)) {
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
			WithDetail("为什么要紧", "brickkit.yaml 是建议提交进 Git 的，"+
				"写在这里的密钥会跟着进版本库，而且历史里删不掉").
			WithHint(
				"改成 ${MY_TOKEN} 这样的引用，把真实值放进 .env",
				".env 必须在 .gitignore 中",
				"确实不是密钥的话可以忽略这条——判据只看名字，不看值",
			),
	})
}

// warnExistingSecretConfigIssues 检查 component.config 里写了 existingSecret 形状的两类问题：
//
//	① 配置项没有声明 secret: true——这个形状不会生效（见 inject.addConfig），
//	   使用者会以为自己配好了，实际这条变量根本不会被注入；
//	② deploy.target 是 docker——existingSecret 是 K8s 专属概念，Docker 没有
//	   "引用外部已建好的 Secret"这回事，同样不会生效。
//
// 与 declaredSecretKeys 用同一份"这个组件的 configSchema 怎么说"的读取方式：graph 里
// 没有这个组件（还没 add、Manifest 读不到）时什么都不查，等 add 完、下一次 up 自然查得到。
func warnExistingSecretConfigIssues(opts *Options, cfg *config.Config, graph *resolver.Graph) {
	var notDeclaredSecret, dockerOnly []string
	for _, c := range cfg.Components {
		declared := declaredSecretKeys(graph, c)
		for name, value := range c.Config {
			if _, _, ok := config.ExistingSecretRef(value); !ok {
				continue
			}
			ref := c.Ref() + " → " + name
			if !declared[name] {
				notDeclaredSecret = append(notDeclaredSecret, ref)
			}
			if cfg.Deploy.Target != config.TargetK8s {
				dockerOnly = append(dockerOnly, ref)
			}
		}
	}

	if len(notDeclaredSecret) > 0 {
		sort.Strings(notDeclaredSecret)
		renderWarnings(opts, []*clierr.Error{
			clierr.Warn(clierr.CodeConfigInvalid, "existingSecret 写法不会生效：配置项没有声明 secret: true").
				WithDetail("配置项", strings.Join(notDeclaredSecret, "、")).
				WithHint("给对应的 configSchema.properties.<key> 加一行 secret: true，或者把这个值改回普通写法"),
		})
	}
	if len(dockerOnly) > 0 {
		sort.Strings(dockerOnly)
		renderWarnings(opts, []*clierr.Error{
			clierr.Warn(clierr.CodeConfigInvalid, "existingSecret 只在 K8s 生效，当前是 docker 目标").
				WithDetail("配置项", strings.Join(dockerOnly, "、")).
				WithHint("Docker 下没有\"引用外部已建好的 Secret\"这个概念，请直接给这个配置项写字面值或 ${VAR} 引用"),
		})
	}
}
