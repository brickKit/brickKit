package k8s

// 本文件负责 ${VAR} 求值。
//
// 为什么 K8s 这边必须做、compose 那边不用做：
// `docker compose` 自己会读项目根目录的 .env 并替换 ${VAR}，所以 compose 文件里
// 原样留着占位符就行。kubectl **不做任何变量替换**——留着占位符的后果是把字面量
// "${POSTGRES_PASSWORD}" 当成密码部署上去，Pod 以认证失败反复重启，
// 而那份 YAML 看上去完全正常。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// expander 按 shell 的写法展开 ${VAR} 与 ${VAR:-默认值}。
//
// 展开不到的变量名会被记下来，最后一次性报错（而不是逐个打断），
// 与配置校验"缺失项一次全报"的做法一致。
type expander struct {
	lookup  func(name string) (string, bool)
	missing map[string]bool
}

func newExpander(lookup func(name string) (string, bool)) *expander {
	return &expander{lookup: lookup, missing: map[string]bool{}}
}

// value 展开一段文本里的全部 ${...}。
func (e *expander) value(raw string) string {
	if !strings.Contains(raw, "${") {
		return raw
	}

	var b strings.Builder
	for {
		start := strings.Index(raw, "${")
		if start < 0 {
			b.WriteString(raw)
			return b.String()
		}
		end := strings.Index(raw[start:], "}")
		if end < 0 {
			// 没有右括号：不是引用，原样保留
			b.WriteString(raw)
			return b.String()
		}
		end += start

		b.WriteString(raw[:start])
		b.WriteString(e.one(raw[start+2 : end]))
		raw = raw[end+1:]
	}
}

// one 展开一个 ${...} 里的内容。
func (e *expander) one(expr string) string {
	name, fallback, hasFallback := strings.Cut(expr, ":-")

	if value, ok := e.get(name); ok {
		return value
	}
	if hasFallback {
		return fallback
	}
	e.missing[name] = true
	// 原样返回只是为了让后续渲染能继续跑完；check() 会阻断这次生成
	return "${" + expr + "}"
}

func (e *expander) get(name string) (string, bool) {
	if e.lookup == nil {
		return "", false
	}
	return e.lookup(name)
}

// check 汇总所有展开不了的变量。
func (e *expander) check() error {
	if len(e.missing) == 0 {
		return nil
	}

	names := make([]string, 0, len(e.missing))
	for name := range e.missing {
		names = append(names, name)
	}
	sort.Strings(names)

	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.K8sEnvVarsUndefined)).
		WithDetail(i18n.T(msgid.K8sLabelMissingVars), strings.Join(names, i18n.T(msgid.ListSeparator))).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.K8sEnvVarsReasonDetail)).
		WithHint(
			i18n.T(msgid.K8sHintDefineEnvVars),
			i18n.T(msgid.K8sHintEnvVarDefault),
		)
}
