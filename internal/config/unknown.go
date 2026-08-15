package config

import (
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
)

// checkUnknownFields 找出 brickkit.yaml 里**拼错或多余的字段**。
//
// # 为什么值得单独做这件事
//
// yaml.v3 默认把不认识的键**静默丢掉**。于是把 username 写成 user 时，
// CLI 一声不吭，等到组件启动才报：
//
//	缺少数据库连接配置：DATABASE_USER（这些变量由平台按资源绑定注入）
//
// ——一句把**配置笔误**指向**平台**的错误。这是真实装配时踩到的，
// 而且是最费时间的一类：使用者会去查注入引擎、查资源绑定、查组件代码，
// 唯独不会想到自己少打了三个字母。
//
// 直接用 yaml.Decoder 的 KnownFields 也能挡住，但它只会说
// "field user not found in type config.Resource"——没有行号、没有中文、
// 更不会告诉你**你想写的大概是哪个字段**。所以自己走一遍。
func checkUnknownFields(doc *yaml.Node, p *clierr.ProblemSet) {
	walkFields(doc, reflect.TypeOf(Config{}), "", p)
}

// walkFields 沿着 YAML 节点与 Go 类型同时下行。
func walkFields(node *yaml.Node, typ reflect.Type, path string, p *clierr.ProblemSet) {
	if node == nil {
		return
	}
	if node.Kind == yaml.AliasNode {
		walkFields(node.Alias, typ, path, p)
		return
	}
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == nil {
		return
	}

	switch typ.Kind() {
	case reflect.Struct:
		walkStruct(node, typ, path, p)
	case reflect.Slice, reflect.Array:
		if node.Kind != yaml.SequenceNode {
			return
		}
		for i, item := range node.Content {
			walkFields(item, typ.Elem(), indexPath(path, i), p)
		}
	}
	// map / 标量不往下走：map 的键是使用者自己定的（如 component.config），
	// 那里出现什么都合法
}

func walkStruct(node *yaml.Node, typ reflect.Type, path string, p *clierr.ProblemSet) {
	if node.Kind != yaml.MappingNode {
		return
	}

	known := knownFieldsOf(typ)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valueNode := node.Content[i], node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}

		field, ok := known[keyNode.Value]
		if !ok {
			p.Add(joinPath(path, keyNode.Value), unknownFieldMessage(keyNode, known))
			continue
		}
		walkFields(valueNode, field.Type, joinPath(path, keyNode.Value), p)
	}
}

// unknownFieldMessage 组织一条能直接用的提示。
//
// 关键是那句"是不是想写 X"：user → username 这类笔误，只要把正确的字段名
// 摆在眼前，就不必再去翻设计书。
func unknownFieldMessage(keyNode *yaml.Node, known map[string]reflect.StructField) string {
	msg := "未知字段（第 " + strconv.Itoa(keyNode.Line) + " 行）"
	if guess := closestField(keyNode.Value, known); guess != "" {
		return msg + "，是不是想写 " + guess + "？"
	}

	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	sort.Strings(names)
	return msg + "。这一层可用的字段：" + strings.Join(names, "、")
}

// closestField 猜使用者想写的是哪个字段。
//
// 只在**足够接近**时才给建议：乱猜一个八竿子打不着的字段名，
// 比不给建议更误导人。
//
// 两条规则，前缀优先：
//
//	前缀    user → username、depend → dependencies。少打了后半截是最常见的写法
//	编辑距离 usrname → username。打字打错了
//
// 前缀单独列一条，是因为编辑距离对它无能为力：user 与 username 差 4 个字符，
// 按距离早就被筛掉了，而它恰恰是真实装配时踩到的那一个。
func closestField(input string, known map[string]reflect.StructField) string {
	lowered := strings.ToLower(input)

	// 前缀匹配。要求至少 3 个字符：一两个字母的前缀能命中一大片，
	// 挑出来的多半不是使用者想要的
	if len(lowered) >= minPrefixLength {
		best := ""
		for name := range known {
			candidate := strings.ToLower(name)
			if !strings.HasPrefix(candidate, lowered) && !strings.HasPrefix(lowered, candidate) {
				continue
			}
			// 命中多个时取最短的：user 同时是 username 的前缀，
			// 短的那个通常就是使用者少打了后半截的那个
			if best == "" || len(name) < len(best) {
				best = name
			}
		}
		if best != "" {
			return best
		}
	}

	best, bestDistance := "", 0
	for name := range known {
		distance := editDistance(lowered, strings.ToLower(name))
		// 允许的差距随字段名长度放宽，但最多两处改动
		limit := 2
		if len(name) <= 4 {
			limit = 1
		}
		if distance > limit {
			continue
		}
		if best == "" || distance < bestDistance {
			best, bestDistance = name, distance
		}
	}
	return best
}

// minPrefixLength 是做前缀猜测所需的最少字符数。
const minPrefixLength = 3

// editDistance 是标准的 Levenshtein 距离。
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// knownFieldsOf 列出结构体在 YAML 里认识的键。
func knownFieldsOf(typ reflect.Type) map[string]reflect.StructField {
	out := map[string]reflect.StructField{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		switch name {
		case "-":
			// 显式排除的字段（如 Config.Source）不该出现在配置里
			continue
		case "":
			name = strings.ToLower(field.Name)
		}
		out[name] = field
	}
	return out
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func indexPath(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}
