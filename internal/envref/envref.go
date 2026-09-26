// Package envref 识别并展开值里的 ${VAR} 引用。
//
// 三层文件都用同一种写法引用进程环境变量；"是不是引用、引用了谁、要不要现在展开"
// 在这一处定义，免得 brickkit.yaml、deploy 文件、config/ 各长一份略有出入的正则。
package envref

import (
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// refRe 匹配 ${NAME}：字母或下划线开头，后接字母、数字、下划线（shell 惯例）。
var refRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Has 报告 s 里是否含有 ${VAR} 引用。
func Has(s string) bool { return refRe.MatchString(s) }

// Names 按出现顺序返回 s 里引用到的变量名，去重。
func Names(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range refRe.FindAllStringSubmatch(s, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Expand 用 lookup 替换 s 里的 ${VAR}。lookup 找不到的引用**原样保留**：
// 生成物里留着 ${VAR} 一眼就能看出漏配了哪个，换成空串则无从查起。
func Expand(s string, lookup func(string) (string, bool)) string {
	if !Has(s) {
		return s
	}
	return refRe.ReplaceAllStringFunc(s, func(match string) string {
		if v, ok := lookup(match[2 : len(match)-1]); ok {
			return v
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
