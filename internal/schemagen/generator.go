// Package schemagen 从 Manifest（component.yaml）与 Config（brickkit.yaml）的 Go 结构体
// 反射生成 JSON Schema（draft-07），给编辑器做字段补全、类型提示与未知字段红线。
//
// # 为什么生成而不是手写
//
// 手写的 schema 是仓库里从没有过的一种"容易悄悄过期"的东西——改了结构体，schema 不会
// 报错，只会让编辑器给出过时的提示。这与 tests/docfields 已经在防的是同一类问题，
// 所以同一个办法：生成，再用测试把生成物与签入的文件钉死（见 schemas_test.go）。
//
// # 约束从哪来
//
// 三样东西，各管各的：
//
//   - 反射：字段名（与 CLI 拒绝未知字段用的是同一份，见 yamlcheck.KnownFields）、类型、
//     必填与否。必填 = yaml tag 没有 omitempty，且不是 bool、不是指针，且没写
//     jsonschema:"optional"。
//   - `jsonschema` struct tag：封闭取值的约束——enum、pattern、minimum、maximum。
//     关键字之间用 `,` 分隔，enum 的取值之间用 `|` 分隔；取值与 pattern 里不能出现
//     `,` 与 `|`（也就不必在 struct tag 里转义反斜杠，正则写成 [.] 而不是 \.）。
//     不认识的关键字、重复的关键字、空的 enum 取值都直接报错，写错了不会悄悄不生效。
//   - 覆盖表：yaml.v3 不按字段反射来解码的类型（目前表里只有 manifest.ComponentDep）。
//     有三种：有 UnmarshalYAML 方法的（新旧两种签名、指针或值接收者都算）、实现了
//     encoding.TextUnmarshaler 的（time.Time 就是）、以及 time.Duration（yaml.v3 特判，
//     接受 5s 这样的写法）。反射看不出它们接受哪些写法——ComponentDep 既能写成字符串也能写成
//     映射——只能手写。遇到这样的类型却不在表里，生成器报错并说清是哪种机制，
//     而不是生成一份悄悄错误的 schema。
//
// # 生成器不去猜的形状
//
// 另有两处，反射看到的与 yaml.v3 实际解码的对不上，同样直接报错而不是猜：yaml tag 里的
// inline 选项（yaml.v3 把被内联字段的键摊平到外层，生成器只会生成一层嵌套对象），以及递归
// 类型——结构体自己引用自己，或者经由具名 slice / map 绕回来，展开永远不会终止。
//
// # 需要人记得的两处
//
// tag 里抄了一份"取值范围"，覆盖表里手写了一份"ComponentDep 的形状"——它们都是校验代码之外的
// 另一份真相，都由 schemas_test.go 拿真实的校验器核对：tag 的取值靠约束测试逐项核对；覆盖表靠
// "有自定义解码的类型都得在表里"，加上版本 pattern、id 必填、optional 可缺省、映射写法认的键集合
// 这几项与真实解析器的对照。核对不到的是新增的写法本身（比如给 ComponentDep 加第三种形式）——
// 那要改 UnmarshalYAML 的人自己想起来这里。
package schemagen

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// tagName 是承载约束的 struct tag 名。
const tagName = "jsonschema"

// schema 是一个 JSON Schema 节点。用 map 而不是结构体：encoding/json 按键排序输出 map，
// 同一份输入每次生成的字节都相同，签入仓库才有稳定的 diff。
type schema = map[string]any

var (
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	durationType        = reflect.TypeOf(time.Duration(0))
)

// customDecoding 说出 yaml.v3 会绕开字段反射、自己解码这个类型的原因；没有这回事就返回空串。
//
// 三种，都是 yaml.v3 真的会走的路（decode.go 的 prepare / scalar）：
//
//   - UnmarshalYAML：新旧两种签名它都调用。所以按方法名判断，而不是按 yaml.Unmarshaler
//     接口——接口只认新式签名，旧式的 UnmarshalYAML(func(any) error) error 会漏掉。
//     看的是 *T 的方法集，指针接收者与值接收者的都在里面。名字对、签名对不上的也算：
//     宁可多报一次让人确认，也不放过。
//   - encoding.TextUnmarshaler：标量会被交给它的 UnmarshalText。time.Time 就是这种。
//   - time.Duration：yaml.v3 特判，把 5s 解析成时长；它没有任何方法，反射只会看到整数。
func customDecoding(t reflect.Type) string {
	pt := reflect.PointerTo(t)
	if _, ok := pt.MethodByName("UnmarshalYAML"); ok {
		return "有自定义的 UnmarshalYAML 方法（yaml.v3 会调用它来解码，反射看不出它接受哪些写法）"
	}
	if pt.Implements(textUnmarshalerType) {
		return "实现了 encoding.TextUnmarshaler（yaml.v3 会把标量交给它的 UnmarshalText，反射看不出它接受哪些写法）"
	}
	if t == durationType {
		return "会被 yaml.v3 特殊解码（它接受 5s 这样的写法，反射却只会看到一个整数）"
	}
	return ""
}

type generator struct {
	overrides map[reflect.Type]func() schema
	visiting  map[reflect.Type]bool
}

func newGenerator(overrides map[reflect.Type]func() schema) *generator {
	return &generator{overrides: overrides, visiting: map[reflect.Type]bool{}}
}

// document 生成一份完整的 schema 文档：节点 + $schema + title，缩进 2 空格，末尾换行。
func (g *generator) document(t reflect.Type, title string) ([]byte, error) {
	root, err := g.typeSchema(t)
	if err != nil {
		return nil, err
	}
	root["$schema"] = "http://json-schema.org/draft-07/schema#"
	root["title"] = title

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// 不做 HTML 转义：pattern 里的 < > & 要原样出现在文件里，编辑器读到的才是原来的正则
	enc.SetEscapeHTML(false)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (g *generator) typeSchema(t reflect.Type) (schema, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if build, ok := g.overrides[t]; ok {
		return build(), nil
	}
	if how := customDecoding(t); how != "" {
		return nil, fmt.Errorf("%s %s，需要在覆盖表里手写它的 schema", t, how)
	}

	// 递归检测放在这里而不是 structSchema 里：环可以完全由具名的 slice / map 构成
	// （type tree []tree、type nested map[string]nested），一个 struct 都不经过。
	// 放在覆盖表与守卫之后：被它们接管的类型不会往下展开，谈不上递归。
	if g.visiting[t] {
		return nil, fmt.Errorf("递归类型 %s 不支持", t)
	}
	g.visiting[t] = true
	defer delete(g.visiting, t)

	switch t.Kind() {
	case reflect.String:
		return schema{"type": "string"}, nil
	case reflect.Bool:
		return schema{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return schema{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return schema{"type": "number"}, nil
	case reflect.Interface:
		return schema{}, nil
	case reflect.Slice, reflect.Array:
		items, err := g.typeSchema(t.Elem())
		if err != nil {
			return nil, err
		}
		return schema{"type": "array", "items": items}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("%s：map 的键必须是 string", t)
		}
		values, err := g.typeSchema(t.Elem())
		if err != nil {
			return nil, err
		}
		return schema{"type": "object", "additionalProperties": values}, nil
	case reflect.Struct:
		return g.structSchema(t)
	}
	return nil, fmt.Errorf("不支持的类型 %s", t)
}

func (g *generator) structSchema(t reflect.Type) (schema, error) {
	known := yamlcheck.KnownFields(t)
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	sort.Strings(names)

	properties := map[string]any{}
	var required []string
	for _, name := range names {
		field := known[name]
		if hasYAMLOption(field, "inline") {
			return nil, fmt.Errorf("%s.%s：yaml 的 inline 选项会把这个字段的键摊平到外层，"+
				"生成器却会生成一层嵌套对象，两边对不上，不支持", t.Name(), field.Name)
		}
		node, err := g.typeSchema(field.Type)
		if err != nil {
			return nil, fmt.Errorf("%s.%s：%w", t.Name(), field.Name, err)
		}
		optional, err := applyTag(node, field.Tag.Get(tagName))
		if err != nil {
			return nil, fmt.Errorf("%s.%s：%w", t.Name(), field.Name, err)
		}
		properties[name] = node

		if !hasYAMLOption(field, "omitempty") && !optional &&
			field.Type.Kind() != reflect.Bool && field.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}

	out := schema{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out, nil
}

// hasYAMLOption 判断字段的 yaml tag 是否带某个选项（omitempty、inline、flow……）。
func hasYAMLOption(field reflect.StructField, option string) bool {
	options := strings.Split(field.Tag.Get("yaml"), ",")
	for _, opt := range options[1:] {
		if opt == option {
			return true
		}
	}
	return false
}

// applyTag 把 `jsonschema:"..."` 里的约束写进字段的 schema 节点，返回该字段是否被标成 optional。
func applyTag(node schema, tag string) (optional bool, err error) {
	if tag == "" {
		return false, nil
	}
	kind, _ := node["type"].(string)

	seen := map[string]bool{}
	for _, item := range strings.Split(tag, ",") {
		key, value, found := strings.Cut(item, "=")
		// 重复的关键字：后写的悄悄盖掉先写的，写的人却以为两条都生效了
		if seen[key] {
			return false, fmt.Errorf("%s 约束 %q 重复出现", tagName, key)
		}
		seen[key] = true

		if item == "optional" {
			optional = true
			continue
		}
		if !found {
			return false, fmt.Errorf("%s 约束 %q 缺少 =", tagName, item)
		}
		switch key {
		case "enum":
			if kind != "string" {
				return false, fmt.Errorf("enum 只能用在 string 字段上（这个字段是 %q）", kind)
			}
			values := strings.Split(value, "|")
			list := make([]any, len(values))
			for i, v := range values {
				// enum= 没写取值、enum=a||b 漏了一个：多半是手滑，别生成一个只允许空串的 schema
				if v == "" {
					return false, fmt.Errorf("enum 的取值不能为空（%q）", value)
				}
				list[i] = v
			}
			node["enum"] = list
		case "pattern":
			if kind != "string" {
				return false, fmt.Errorf("pattern 只能用在 string 字段上（这个字段是 %q）", kind)
			}
			node["pattern"] = value
		case "minimum", "maximum":
			if kind != "integer" && kind != "number" {
				return false, fmt.Errorf("%s 只能用在 integer / number 字段上（这个字段是 %q）", key, kind)
			}
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return false, fmt.Errorf("%s=%q 不是数字", key, value)
			}
			// ParseFloat 认 NaN / Inf，但 JSON 写不下它们。放过去的话，typeSchema 照常返回，
			// 直到 document 编码时才报一句不带字段名的 "unsupported value"
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return false, fmt.Errorf("%s=%q 不是有限的数字，JSON Schema 写不下它", key, value)
			}
			node[key] = n
		default:
			return false, fmt.Errorf("不认识的 %s 约束 %q", tagName, key)
		}
	}
	return optional, nil
}
