package deployfile

// 本文件是 brickkit local refresh 的"本地修改摘要"（提案 §6.6）：列出旧的 deploy.local.yaml 里
// 使用者自己的每一处修改，让使用者照着把它们合并回重新从 deploy.yaml 复制出来的新文件。
//
// "本地修改"以基线为准：基线是上次复制时的团队文件（local on / refresh 存下的那份）。
// 旧本地文件与基线不同的值、基线有而旧本地文件删掉了的字段，才是使用者改的；与基线相同的值不是，
// 哪怕团队后来改了它——刷新后跟随团队的新值正是想要的。没有基线（更早的项目）、或者一个条目在基线里
// 没有（add 之后才出现）时，退回两方对比：旧文件里与新文件不同、或新文件没有的值。
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

// FieldPlacement 是条目所在位置这个"字段"：值是它嵌在哪个外壳条目下面，在顶层时为空。
// 把成员挪出外壳单独跑（调试它时最常见的做法）不改任何字段，只改位置——也是本地修改。
const FieldPlacement = "\x00placement"

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
	// Removed 表示这个字段是使用者在本地删掉的（基线里有、旧本地文件里没有）；Old 为空。
	Removed bool
}

// flatValue 是拍平后的一个值。
type flatValue struct {
	scope, field, value string
}

// DiffLocal 列出旧本地文件 old 里的本地修改（相对基线 base；base 为 nil 时相对新文件 fresh），
// 且只列与 fresh 不同的——与团队现在的值一样的修改，刷新之后本来就在。按 old 里的出现顺序，
// 本地删掉的字段排在后面。
func DiffLocal(base, old, fresh []byte) ([]LocalChange, error) {
	oldValues, oldEntries, err := flatten(old)
	if err != nil {
		return nil, err
	}
	newValues, newEntries, err := flatten(fresh)
	if err != nil {
		return nil, err
	}
	index := indexValues(newValues)
	present := setOf(newEntries)

	var baseValues []flatValue
	baseIndex := map[[2]string]string{}
	inBase := map[string]bool{}
	if base != nil {
		values, entries, err := flatten(base)
		if err != nil {
			return nil, err
		}
		baseValues, baseIndex, inBase = values, indexValues(values), setOf(entries)
		// 顶层与 vars 在任何一份部署文件里都有，也按基线比
		inBase[ScopeDeploy], inBase[ScopeVars] = true, true
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
		if inBase[v.scope] {
			if was, ok := baseIndex[[2]string{v.scope, v.field}]; ok && was == v.value {
				continue // 与复制时一样：不是本地修改
			}
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
	// 本地删掉的字段：基线里有，旧本地文件的同一个条目里没有，而团队现在还有它
	oldIndex := indexValues(oldValues)
	oldPresent := setOf(oldEntries)
	oldPresent[ScopeDeploy], oldPresent[ScopeVars] = true, true
	for _, v := range baseValues {
		key := [2]string{v.scope, v.field}
		if !oldPresent[v.scope] || gone[v.scope] {
			continue
		}
		if _, kept := oldIndex[key]; kept {
			continue
		}
		if now, ok := index[key]; ok {
			changes = append(changes, LocalChange{Scope: v.scope, Field: v.field, Removed: true, New: &now})
		}
	}
	return changes, nil
}

func indexValues(values []flatValue) map[[2]string]string {
	index := map[[2]string]string{}
	for _, v := range values {
		index[[2]string{v.scope, v.field}] = v.value
	}
	return index
}

func setOf(ids []string) map[string]bool {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return set
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
			flattenEntries(value, "", &values, &entries)
		case "vars":
			flattenMap(ScopeVars, "", value, &values)
		default:
			flattenMap(ScopeDeploy, key, value, &values)
		}
	}
	return values, entries, nil
}

func flattenEntries(list *yaml.Node, shell string, values *[]flatValue, entries *[]string) {
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
		*values = append(*values, flatValue{scope: id, field: FieldPlacement, value: shell})
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, value := item.Content[i].Value, item.Content[i+1]
			switch key {
			case "id":
			case "members":
				flattenEntries(value, id, values, entries)
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
