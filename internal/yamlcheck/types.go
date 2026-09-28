package yamlcheck

import (
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// TypeMismatches 沿着 YAML 文档与目标类型同时下行，把解不进目标类型的值按字段路径报出来。
//
// yaml 库自己的报错是 "line 3: cannot unmarshal !!str `abc` into int"：有行号、没有字段。
// 一份写在一行里的 JSON（市场收到的 Manifest 就是）所有值都在第 1 行，行号什么也说明不了；
// 而调用方——CLI 的错误块、市场响应里的 {field, reason}——要的正是字段。
//
// 判断不另立规则：叶子值、自己实现了 UnmarshalYAML 的类型，都交给 yaml 库去解，
// 解不进就报；容器只看节点种类对不对。所以它报与不报，与真正解码成不成功一致。
// 不认识的键归 Walk 管，这里跳过。
func TypeMismatches(doc *yaml.Node, typ reflect.Type, add func(field, message string)) {
	checkType(doc, typ, "", add)
}

var unmarshalerType = reflect.TypeOf((*yaml.Unmarshaler)(nil)).Elem()

func checkType(node *yaml.Node, typ reflect.Type, path string, add func(field, message string)) {
	if node == nil {
		return
	}
	if node.Kind == yaml.AliasNode {
		checkType(node.Alias, typ, path, add)
		return
	}
	if isNullNode(node) {
		return // null 解出零值，对任何类型都成立
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if reflect.PointerTo(typ).Implements(unmarshalerType) {
		if err := node.Decode(reflect.New(typ).Interface()); err != nil {
			add(path, customMessage(err))
		}
		return
	}

	switch typ.Kind() {
	case reflect.Interface:
		// any：什么都解得进
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			add(path, mismatch(typ, node))
			return
		}
		known := knownFieldsOf(typ)
		for i := 0; i+1 < len(node.Content); i += 2 {
			if field, ok := known[node.Content[i].Value]; ok {
				checkType(node.Content[i+1], field.Type, joinPath(path, node.Content[i].Value), add)
			}
		}
	case reflect.Map:
		if node.Kind != yaml.MappingNode {
			add(path, mismatch(typ, node))
			return
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			checkType(node.Content[i+1], typ.Elem(), joinPath(path, node.Content[i].Value), add)
		}
	case reflect.Slice, reflect.Array:
		if node.Kind != yaml.SequenceNode {
			add(path, mismatch(typ, node))
			return
		}
		for i, item := range node.Content {
			checkType(item, typ.Elem(), indexPath(path, i), add)
		}
	default:
		if err := node.Decode(reflect.New(typ).Interface()); err != nil {
			add(path, mismatch(typ, node))
		}
	}
}

func isNullNode(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}

// mismatch 是"必须是整数（当前是 `abc`）"这样的一句话。
func mismatch(want reflect.Type, got *yaml.Node) string {
	actual := KindName(got)
	if got.Kind == yaml.ScalarNode {
		actual = "`" + got.Value + "`"
	}
	return i18n.T(msgid.YamlcheckTypeMismatch, wantName(want), actual)
}

func wantName(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.Bool:
		return i18n.T(msgid.YamlcheckWantBool)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return i18n.T(msgid.YamlcheckWantInteger)
	case reflect.Float32, reflect.Float64:
		return i18n.T(msgid.YamlcheckWantNumber)
	case reflect.Struct, reflect.Map:
		return i18n.T(msgid.YamlcheckWantMapping)
	case reflect.Slice, reflect.Array:
		return i18n.T(msgid.YamlcheckWantArray)
	default:
		return i18n.T(msgid.YamlcheckWantString)
	}
}

// yamlLinePrefix 是 yaml 库报错开头的 "yaml: line 3: "：字段路径已经说明了位置。
var yamlLinePrefix = regexp.MustCompile(`^(yaml: )?(unmarshal errors:\s*)?(line \d+: )?`)

// customMessage 用自己解析的类型自己的话说原因，只去掉 yaml 库加的前缀。
func customMessage(err error) string {
	return strings.TrimSpace(yamlLinePrefix.ReplaceAllString(err.Error(), ""))
}
