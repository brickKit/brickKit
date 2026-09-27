package deployfile

// 本文件是 brickkit local refresh 的"本地修改摘要"（提案 §6.6）：旧的 deploy.local.yaml
// 与重新从 deploy.yaml 复制出来的新文件逐值对比，列出旧文件里与新文件不同、或新文件没有的每一个值，
// 让使用者照着把自己的本地改动合并回去。
//
// 比的是数据而不是文本：注释、键的顺序、引号写法都不算改动；新文件多出来的值也不算——
// 那是团队的标准配置，不是使用者的本地修改。CLI 只列出来，不替人合并（完整替换，附录 A1）。

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// 摘要里的两个固定作用域：顶层字段与公共变量。其余作用域是组件条目的 id。
const (
	ScopeDeploy = "deploy"
	ScopeVars   = "vars"
)

// LocalChange 是旧本地文件里的一处本地修改。
type LocalChange struct {
	// Scope 是 ScopeDeploy、ScopeVars，或组件条目的 id（外壳下面的成员也按自己的条目 id）。
	Scope string
	// Field 是作用域里的字段路径（嵌套映射用 . 连接）；为空表示整个条目在新文件里没有了。
	Field string
	// Old 是旧文件里的值（单行写法）。
	Old string
	// New 是新文件里的值；nil 表示新文件没有这个字段。
	New *string
}

// flatValue 是拍平后的一个值。
type flatValue struct {
	scope, field, value string
}

// DiffLocal 列出 old 里与 fresh 不同或 fresh 没有的值，按 old 里的出现顺序。
func DiffLocal(old, fresh []byte) ([]LocalChange, error) {
	oldValues, oldEntries, err := flatten(old)
	if err != nil {
		return nil, err
	}
	newValues, newEntries, err := flatten(fresh)
	if err != nil {
		return nil, err
	}
	index := map[[2]string]string{}
	for _, v := range newValues {
		index[[2]string{v.scope, v.field}] = v.value
	}
	present := map[string]bool{}
	for _, id := range newEntries {
		present[id] = true
	}

	var changes []LocalChange
	gone := map[string]bool{}
	for _, id := range oldEntries {
		if !present[id] {
			gone[id] = true
		}
	}
	reported := map[string]bool{}
	for _, v := range oldValues {
		if gone[v.scope] {
			if !reported[v.scope] {
				reported[v.scope] = true
				changes = append(changes, LocalChange{Scope: v.scope})
			}
			continue
		}
		now, ok := index[[2]string{v.scope, v.field}]
		switch {
		case !ok:
			changes = append(changes, LocalChange{Scope: v.scope, Field: v.field, Old: v.value})
		case now != v.value:
			changes = append(changes, LocalChange{Scope: v.scope, Field: v.field, Old: v.value, New: &now})
		}
	}
	// 没有任何字段的条目（只有 id）整个不见了，也要说
	for _, id := range oldEntries {
		if gone[id] && !reported[id] {
			changes = append(changes, LocalChange{Scope: id})
		}
	}
	return changes, nil
}

// flatten 把一份部署文件拍平成值的列表（文件顺序），外加文件里全部条目 id（含成员）。
func flatten(data []byte) ([]flatValue, []string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, nil
	}
	root := doc.Content[0]
	var values []flatValue
	var entries []string
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i].Value, root.Content[i+1]
		switch key {
		case "components":
			flattenEntries(value, &values, &entries)
		case "vars":
			flattenMap(ScopeVars, "", value, &values)
		default:
			flattenMap(ScopeDeploy, key, value, &values)
		}
	}
	return values, entries, nil
}

func flattenEntries(list *yaml.Node, values *[]flatValue, entries *[]string) {
	if list.Kind != yaml.SequenceNode {
		return
	}
	for _, item := range list.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		id := mapValue(item, "id")
		if id == "" {
			continue
		}
		*entries = append(*entries, id)
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, value := item.Content[i].Value, item.Content[i+1]
			switch key {
			case "id":
			case "members":
				flattenEntries(value, values, entries)
			default:
				flattenMap(id, key, value, values)
			}
		}
	}
}

// flattenMap 把 node 放到 scope 下的 path 处：映射逐层展开（路径用 . 连接），其余写成一行。
// path 为空时 node 本身必须是映射（vars 的顶层）。
func flattenMap(scope, path string, node *yaml.Node, values *[]flatValue) {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			sub := node.Content[i].Value
			if path != "" {
				sub = path + "." + sub
			}
			flattenMap(scope, sub, node.Content[i+1], values)
		}
		return
	}
	if path == "" {
		return
	}
	*values = append(*values, flatValue{scope: scope, field: path, value: oneLine(node)})
}

func mapValue(m *yaml.Node, key string) string {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1].Value
		}
	}
	return ""
}

// oneLine 把值写成一行：标量用它的文本（引号不算差别），列表与映射写 JSON。
func oneLine(node *yaml.Node) string {
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	var v any
	if err := node.Decode(&v); err != nil {
		return strings.TrimSpace(node.Value)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(node.Value)
	}
	return string(data)
}
