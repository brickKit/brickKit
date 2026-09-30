// Package envref 识别并展开值里的 ${VAR} 引用。
//
// 三层文件都用同一种写法引用进程环境变量；"是不是引用、引用了谁、要不要现在展开"
// 在这一处定义，免得 brickkit.yaml、deploy 文件、config/ 各长一份略有出入的正则。
package envref

import (
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// refRe 匹配 ${NAME} 与 ${NAME:-默认值}：NAME 字母或下划线开头，后接字母、数字、下划线（shell 惯例）；
// 默认值是一段不含 $、{、} 的纯文本（可以为空）。这是三层文件里唯一的引用语法：docker compose
// 与 K8s 下的展开、shell 成员的 JSON、本机进程的环境、lint 的检查都按它认。
var refRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-[^${}]*)?\}`)

// Has 报告 s 里是否含有 ${VAR} 引用（带不带默认值都算）。
func Has(s string) bool { return refRe.MatchString(s) }

// Required 按出现顺序返回 s 里**必须有定义**的变量名，去重：带默认值的引用永远展得开，不在其中。
func Required(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range refRe.FindAllStringSubmatch(s, -1) {
		if m[2] != "" || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}

// Expand 用 lookup 替换 s 里的引用。取不到时用默认值；没有默认值的引用**原样保留**：
// 生成物里留着 ${VAR} 一眼就能看出漏配了哪个，换成空串则无从查起。
func Expand(s string, lookup func(string) (string, bool)) string {
	if !Has(s) {
		return s
	}
	return refRe.ReplaceAllStringFunc(s, func(match string) string {
		m := refRe.FindStringSubmatch(match)
		if v, ok := lookup(m[1]); ok {
			return v
		}
		if m[2] != "" {
			return m[2][len(":-"):]
		}
		return match
	})
}

// ExpandOS 用进程环境展开 s。
func ExpandOS(s string) string { return Expand(s, os.LookupEnv) }

// ExpandNode 递归展开 YAML 树里字符串标量的 ${VAR}：只动值、不动键。
//
// skip 对某条字段路径返回 true 时，整棵子树原样保留（那些值要留给渲染器决定何时求值）。
// 路径里映射用键名，数组下标一律写作 "*"。
func ExpandNode(node *yaml.Node, skip func(path []string) bool) {
	expandNode(node, nil, skip)
}

func expandNode(node *yaml.Node, path []string, skip func([]string) bool) {
	if node == nil || (skip != nil && skip(path)) {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			expandNode(child, path, skip)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			expandNode(node.Content[i+1], appendPath(path, node.Content[i].Value), skip)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			expandNode(item, appendPath(path, "*"), skip)
		}
	case yaml.ScalarNode:
		if node.Tag == "!!str" {
			node.Value = ExpandOS(node.Value)
		}
	}
}

func appendPath(path []string, segment string) []string {
	return append(append([]string(nil), path...), segment)
}

// EscapeLiterals 把 s 里不属于引用（${NAME}、${NAME:-默认值}）的每个 $ 写成 $$，引用本身原样保留。
//
// 给"模板原样交给别的程序展开"的场合用（docker compose）：brickkit 只把 ${NAME} 当引用，
// 那边却把 $5、$HOME、$$ 都当成自己的语法。不转义，同一个值在不同部署目标下到达容器时
// 就不一样了。
func EscapeLiterals(s string) string {
	var b strings.Builder
	last := 0
	for _, loc := range refRe.FindAllStringIndex(s, -1) {
		b.WriteString(strings.ReplaceAll(s[last:loc[0]], "$", "$$"))
		b.WriteString(s[loc[0]:loc[1]])
		last = loc[1]
	}
	b.WriteString(strings.ReplaceAll(s[last:], "$", "$$"))
	return b.String()
}

// Malformed 找出 s 里写得像引用（以 ${ 开头）、却不合引用语法的第一段，比如嵌套的默认值
// ${A:-${B}}、默认值里带 $、变量名以数字开头、缺右括号。frag 是从 ${ 到下一个 }（没有就到结尾）。
//
// 这样的文字不能悄悄当字面量处理：交给 docker compose 会被它按自己的语法展开，原样注入则让
// 组件拿到一段意料之外的文字——两种都是不报错的错误值。调用方拿到 bad 就该大声失败。
func Malformed(s string) (frag string, bad bool) {
	valid := refRe.FindAllStringIndex(s, -1)
	inValid := func(i int) bool {
		for _, loc := range valid {
			if i >= loc[0] && i < loc[1] {
				return true
			}
		}
		return false
	}
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '$' || s[i+1] != '{' || inValid(i) {
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			return s[i:], true
		}
		return s[i : i+end+1], true
	}
	return "", false
}
