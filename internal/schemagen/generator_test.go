package schemagen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type inner struct {
	Name string `yaml:"name"`
	Size int    `yaml:"size,omitempty"`
}

type sample struct {
	ID       string            `yaml:"id"`
	Enabled  bool              `yaml:"enabled"`
	Count    *int              `yaml:"count,omitempty"`
	Ratio    float64           `yaml:"ratio,omitempty"`
	Tags     []string          `yaml:"tags,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
	Anything any               `yaml:"anything,omitempty"`
	Nested   inner             `yaml:"nested"`
	Items    []inner           `yaml:"items,omitempty"`
	ByName   map[string]inner  `yaml:"byName,omitempty"`
	NoTag    string
	Skipped  string `yaml:"-"`
	private  string //nolint:unused // 未导出字段必须被跳过；故意不去读它
}

func generate(t *testing.T, v any) schema {
	t.Helper()
	s, err := newGenerator(nil).typeSchema(reflect.TypeOf(v))
	require.NoError(t, err)
	return s
}

func props(s schema) map[string]any { return s["properties"].(map[string]any) }

// 必填的字段是纯类型；不必填的（omitempty、bool、指针）是"类型 + null"的联合形式，
// 见 TestOptionalFieldsAreNullableAndRequiredFieldsAreNot。这里先钉住各种 Go 类型映射成什么。
func TestScalarAndContainerMapping(t *testing.T) {
	s := generate(t, sample{})
	p := props(s)

	assert.Equal(t, schema{"type": "string"}, p["id"])
	assert.Equal(t, schema{"type": []string{"boolean", "null"}}, p["enabled"])
	assert.Equal(t, schema{"type": []string{"integer", "null"}}, p["count"], "指针与它指向的类型同形，只多允许 null")
	assert.Equal(t, schema{"type": []string{"number", "null"}}, p["ratio"])
	assert.Equal(t,
		schema{"type": []string{"array", "null"}, "items": schema{"type": "string"}}, p["tags"],
		"数组的元素不因为数组可以是 null 而可以是 null")
	assert.Equal(t,
		schema{"type": []string{"object", "null"}, "additionalProperties": schema{"type": "string"}}, p["labels"])
	assert.Equal(t, schema{}, p["anything"], "any 不加约束（本来就接受 null）")
	assert.Equal(t, []string{"array", "null"}, p["items"].(schema)["type"])
	assert.Equal(t, []string{"object", "null"}, p["byName"].(schema)["type"])
	assert.Equal(t, schema{"type": "string"}, p["notag"], "没写 yaml 名时用小写字段名，与 yamlcheck 一致")
}

func TestStructsRejectUnknownKeysAndSkipHiddenFields(t *testing.T) {
	s := generate(t, sample{})
	assert.Equal(t, false, s["additionalProperties"])
	assert.NotContains(t, props(s), "-")
	assert.NotContains(t, props(s), "Skipped")
	assert.NotContains(t, props(s), "private")

	nested := props(s)["nested"].(schema)
	assert.Equal(t, false, nested["additionalProperties"])
}

// required：没有 omitempty、不是 bool、不是指针。
func TestRequiredRule(t *testing.T) {
	s := generate(t, sample{})
	assert.Equal(t, []string{"id", "nested", "notag"}, s["required"],
		"bool（enabled）与指针虽然没写 omitempty 也不算必填；按字段名排序")
	assert.Equal(t, []string{"name"}, props(s)["nested"].(schema)["required"])
}

func TestRequiredKeyIsOmittedWhenEmpty(t *testing.T) {
	type allOptional struct {
		A string `yaml:"a,omitempty"`
	}
	assert.NotContains(t, generate(t, allOptional{}), "required")
}

func TestJSONSchemaTagKeywords(t *testing.T) {
	type tagged struct {
		Mode string   `yaml:"mode" jsonschema:"enum=fast|slow"`
		Ver  string   `yaml:"ver" jsonschema:"pattern=^[0-9]+[.][0-9]+$"`
		Port int      `yaml:"port" jsonschema:"minimum=1,maximum=65535"`
		Opt  string   `yaml:"opt" jsonschema:"optional"`
		Both string   `yaml:"both" jsonschema:"enum=a|b,optional"`
		Skip []string `yaml:"skip,omitempty"`
	}
	s := generate(t, tagged{})
	p := props(s)

	assert.Equal(t, []any{"fast", "slow"}, p["mode"].(schema)["enum"])
	assert.Equal(t, "^[0-9]+[.][0-9]+$", p["ver"].(schema)["pattern"])
	assert.EqualValues(t, 1, p["port"].(schema)["minimum"])
	assert.EqualValues(t, 65535, p["port"].(schema)["maximum"])
	assert.Equal(t, []string{"mode", "port", "ver"}, s["required"], "optional 把字段挪出 required")

	// 挪出 required 的字段同时变成可空的：enum 里也要有 null，否则 type 允许 null、enum 却拒绝它
	assert.Equal(t, schema{"type": []string{"string", "null"}, "enum": []any{"a", "b", nil}}, p["both"])
	assert.Equal(t, schema{"type": []string{"string", "null"}}, p["opt"])
}

func TestJSONSchemaTagErrorsAreLoud(t *testing.T) {
	cases := map[string]any{
		"不认识的关键字": struct {
			A string `yaml:"a" jsonschema:"enumm=x"`
		}{},
		"缺 =": struct {
			A string `yaml:"a" jsonschema:"pattern"`
		}{},
		"enum 用在数字": struct {
			A int `yaml:"a" jsonschema:"enum=1|2"`
		}{},
		"范围用在字符串": struct {
			A string `yaml:"a" jsonschema:"minimum=1"`
		}{},
		"数字解析失败": struct {
			A int `yaml:"a" jsonschema:"minimum=abc"`
		}{},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newGenerator(nil).typeSchema(reflect.TypeOf(v))
			require.Error(t, err)
		})
	}
}

type withUnmarshaler struct{ Raw string }

func (w *withUnmarshaler) UnmarshalYAML(value *yaml.Node) error { return value.Decode(&w.Raw) }

func TestTypesWithCustomUnmarshalerNeedAnOverride(t *testing.T) {
	type holder struct {
		V withUnmarshaler `yaml:"v"`
	}

	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(holder{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UnmarshalYAML")
	assert.Contains(t, err.Error(), "withUnmarshaler")

	overrides := map[reflect.Type]func() schema{
		reflect.TypeOf(withUnmarshaler{}): func() schema { return schema{"type": "string"} },
	}
	s, err := newGenerator(overrides).typeSchema(reflect.TypeOf(holder{}))
	require.NoError(t, err)
	assert.Equal(t, schema{"type": "string"}, props(s)["v"])
}

func TestOverrideReturnsAFreshMapEveryTime(t *testing.T) {
	overrides := map[reflect.Type]func() schema{
		reflect.TypeOf(inner{}): func() schema { return schema{"type": "string"} },
	}
	g := newGenerator(overrides)
	a, err := g.typeSchema(reflect.TypeOf(inner{}))
	require.NoError(t, err)
	a["mutated"] = true
	b, err := g.typeSchema(reflect.TypeOf(inner{}))
	require.NoError(t, err)
	assert.NotContains(t, b, "mutated")
}

type recursive struct {
	Next *recursive `yaml:"next,omitempty"`
}

func TestRecursiveTypesAreRejected(t *testing.T) {
	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(recursive{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "递归")
}

func TestUnsupportedTypesAreRejected(t *testing.T) {
	type badKey struct {
		M map[int]string `yaml:"m"`
	}
	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(badKey{}))
	require.Error(t, err)

	type badKind struct {
		C chan int `yaml:"c"`
	}
	_, err = newGenerator(nil).typeSchema(reflect.TypeOf(badKind{}))
	require.Error(t, err)
}

func TestDocumentEnvelopeAndFormatting(t *testing.T) {
	type doc struct {
		Expr string `yaml:"expr" jsonschema:"pattern=^<[a-z]+>&$"`
	}
	out, err := newGenerator(nil).document(reflect.TypeOf(doc{}), "示例")
	require.NoError(t, err)

	text := string(out)
	assert.True(t, strings.HasSuffix(text, "}\n"), "以换行结尾")
	assert.Contains(t, text, "\n  \"$schema\": \"http://json-schema.org/draft-07/schema#\"")
	assert.Contains(t, text, "\"title\": \"示例\"")
	assert.Contains(t, text, "^<[a-z]+>&$", "不做 HTML 转义：<、>、& 原样输出")

	var back map[string]any
	require.NoError(t, json.Unmarshal(out, &back), "输出是合法 JSON")
}

func TestDocumentIsDeterministic(t *testing.T) {
	first, err := newGenerator(nil).document(reflect.TypeOf(sample{}), "t")
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		again, err := newGenerator(nil).document(reflect.TypeOf(sample{}), "t")
		require.NoError(t, err)
		assert.Equal(t, string(first), string(again))
	}
}

// ---- 以下是在设计书 §4.2 / §4.3 的规则上补的用例：把上面没走到的分支与"错在哪"钉住 ----

func TestNumericKindsMapToJSONSchemaTypes(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{int(0), "integer"}, {int8(0), "integer"}, {int16(0), "integer"}, {int32(0), "integer"}, {int64(0), "integer"},
		{uint(0), "integer"}, {uint8(0), "integer"}, {uint16(0), "integer"}, {uint32(0), "integer"}, {uint64(0), "integer"},
		{float32(0), "number"}, {float64(0), "number"},
	}
	for _, c := range cases {
		assert.Equal(t, schema{"type": c.want}, generate(t, c.v), "%T", c.v)
	}
}

func TestArraysPointersAndMapsOfStructsKeepTheirShape(t *testing.T) {
	innerSchema := generate(t, inner{})
	assert.Equal(t, []string{"name"}, innerSchema["required"])

	assert.Equal(t, schema{"type": "array", "items": schema{"type": "string"}}, generate(t, [3]string{}),
		"数组与 slice 同形")
	assert.Equal(t, schema{"type": "array", "items": innerSchema}, generate(t, []*inner{}),
		"元素是指向 struct 的指针时，items 就是那个 struct 的 schema")

	p := &inner{}
	assert.Equal(t, innerSchema, generate(t, p), "指向 struct 的指针与 struct 同形")
	assert.Equal(t, innerSchema, generate(t, &p), "多层指针也一样")

	assert.Equal(t, schema{"type": "object", "additionalProperties": innerSchema}, generate(t, map[string]inner{}),
		"map 的值是 struct 时，值的 schema 挂在 additionalProperties 上；map 这一层本身不封闭（键是使用者自己定的）")

	s := generate(t, sample{})
	assert.Equal(t, innerSchema, props(s)["items"].(schema)["items"])
	assert.Equal(t, innerSchema, props(s)["byName"].(schema)["additionalProperties"])
}

func TestEmptyStructIsAClosedEmptyObject(t *testing.T) {
	assert.Equal(t,
		schema{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		generate(t, struct{}{}))
}

// 同一个类型出现在几个并列的字段里不是递归。
func TestSharedTypesAreNotMistakenForRecursion(t *testing.T) {
	type twice struct {
		A inner `yaml:"a"`
		B inner `yaml:"b"`
	}
	s := generate(t, twice{})
	assert.Equal(t, props(s)["a"], props(s)["b"])
}

type ping struct {
	Pong *pong `yaml:"pong,omitempty"`
}

type pong struct {
	Ping *ping `yaml:"ping,omitempty"`
}

type tree struct {
	Kids []tree `yaml:"kids,omitempty"`
}

func TestIndirectRecursionIsRejectedToo(t *testing.T) {
	for name, v := range map[string]any{"两个类型互相引用": ping{}, "经由 slice 引用自己": tree{}} {
		t.Run(name, func(t *testing.T) {
			_, err := newGenerator(nil).typeSchema(reflect.TypeOf(v))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "递归")
		})
	}
}

// 生成器出错之后还能继续用：visiting 不能留着上一次失败时的脏状态。
func TestGeneratorIsReusableAfterAnError(t *testing.T) {
	g := newGenerator(nil)
	_, err := g.typeSchema(reflect.TypeOf(recursive{}))
	require.Error(t, err)

	s, err := g.typeSchema(reflect.TypeOf(inner{}))
	require.NoError(t, err)
	assert.Equal(t, "object", s["type"])
}

// 出错的那条路径也要把 visiting 释放掉：同一个生成器再遇到同一个出错的类型，报的还是原来的错，
// 而不是被上一次留下的"正在展开"标记误报成"递归"。生产代码靠 defer 释放；
// 把释放只放在成功路径上，这条会红（上面那条只钉了"出错之后还能生成别的类型"，钉不住这一点）。
func TestVisitingIsReleasedOnTheErrorPathToo(t *testing.T) {
	cases := map[string]any{
		"字段的类型不支持": struct {
			C chan int `yaml:"c"`
		}{},
		"slice 的元素类型不支持": struct {
			L []chan int `yaml:"l"`
		}{},
		"map 的键不是 string": struct {
			M map[int]string `yaml:"m"`
		}{},
		"jsonschema tag 写错（错在子节点都生成完之后）": struct {
			A string `yaml:"a" jsonschema:"enumm=x"`
		}{},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			g := newGenerator(nil)
			typ := reflect.TypeOf(v)

			_, first := g.typeSchema(typ)
			require.Error(t, first)
			require.NotContains(t, first.Error(), "递归", "第一次的错本来就不该是递归")

			_, second := g.typeSchema(typ)
			require.Error(t, second)
			assert.Equal(t, first.Error(), second.Error(), "同一个生成器、同一个类型：报同一个错")
			assert.NotContains(t, second.Error(), "递归")
		})
	}
}

func TestUnsupportedTypesAreRejectedAtEveryDepth(t *testing.T) {
	type inSlice struct {
		L []chan int `yaml:"l"`
	}
	type inMap struct {
		M map[string]chan int `yaml:"m"`
	}
	type inPointer struct {
		P *chan int `yaml:"p"`
	}
	type inArray struct {
		A [2]chan int `yaml:"a"`
	}
	for name, v := range map[string]any{
		"chan":                   make(chan int),
		"func":                   func() {},
		"uintptr":                uintptr(0),
		"complex":                complex128(0),
		"slice 的元素":              []chan int{},
		"数组的元素":                  [2]chan int{},
		"map 的值":                 map[string]chan int{},
		"map 的键不是 string（uint8）": map[uint8]string{},
		"struct 字段里的 slice":      inSlice{},
		"struct 字段里的 map":        inMap{},
		"struct 字段里的指针":          inPointer{},
		"struct 字段里的数组":          inArray{},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newGenerator(nil).typeSchema(reflect.TypeOf(v))
			require.Error(t, err)
		})
	}

	// 出错时要能定位到是哪个类型的哪个字段
	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(inSlice{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inSlice.L")
	assert.Contains(t, err.Error(), "chan int")
}

// jsonschema tag 写错了：每一种错法都要报错，并且说清楚错在哪、在哪个类型的哪个字段。
func TestJSONSchemaTagErrorsSayWhatIsWrongAndWhere(t *testing.T) {
	str, num := reflect.TypeOf(""), reflect.TypeOf(0)
	cases := []struct {
		name   string
		typ    reflect.Type
		tag    string
		reason string
	}{
		{"不认识的关键字", str, "enumm=x", "enumm"},
		{"好关键字后面跟着坏关键字", str, "enum=a|b,enumm=x", "enumm"},
		{"缺 =", str, "pattern", "缺少 ="},
		{"pattern 里写了逗号：会被当成下一个关键字，而不是悄悄截断", str, "pattern=^a{1,3}$", "缺少 ="},
		{"enum 用在数字上", num, "enum=1|2", "enum 只能用在 string"},
		{"pattern 用在数字上", num, "pattern=^x$", "pattern 只能用在 string"},
		{"minimum 用在字符串上", str, "minimum=1", "minimum 只能用在 integer / number"},
		{"maximum 用在字符串上", str, "maximum=1", "maximum 只能用在 integer / number"},
		{"minimum 不是数字", num, "minimum=abc", "不是数字"},
		{"maximum 不是数字", num, "maximum=abc", "不是数字"},
		{"数字超出 float64 的范围", num, "maximum=1e999", "不是数字"},
		{"NaN 能被 ParseFloat 读进来，却写不进 JSON", num, "minimum=NaN", "不是有限的数字"},
		{"+Inf 同理", num, "maximum=+Inf", "不是有限的数字"},
		{"-Inf 同理", num, "minimum=-Inf", "不是有限的数字"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{
				Name: "A", Type: c.typ, Tag: reflect.StructTag(`yaml:"a" jsonschema:"` + c.tag + `"`),
			}})
			_, err := newGenerator(nil).typeSchema(typ)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.reason)
			assert.Contains(t, err.Error(), "A：", "错误要带着字段名")
		})
	}
}

func TestJSONSchemaTagErrorsNameTheTypeAndTheField(t *testing.T) {
	type badTag struct {
		Mode string `yaml:"mode" jsonschema:"enumm=x"`
	}
	type outer struct {
		In badTag `yaml:"in"`
	}

	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(badTag{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "badTag.Mode")

	_, err = newGenerator(nil).typeSchema(reflect.TypeOf(outer{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outer.In", "嵌套时，外层的类型与字段也在路径里")
	assert.Contains(t, err.Error(), "badTag.Mode")
}

func TestJSONSchemaTagRangeAcceptsFractionsAndNegatives(t *testing.T) {
	type ranged struct {
		Ratio  float64 `yaml:"ratio" jsonschema:"minimum=-0.5,maximum=1.5"`
		Offset *int    `yaml:"offset,omitempty" jsonschema:"minimum=-10"`
	}
	p := props(generate(t, ranged{}))
	assert.EqualValues(t, -0.5, p["ratio"].(schema)["minimum"])
	assert.EqualValues(t, 1.5, p["ratio"].(schema)["maximum"])
	assert.EqualValues(t, -10, p["offset"].(schema)["minimum"], "指针字段的约束写在它指向的类型的 schema 上")
}

// 必填只豁免 bool 与指针（以及 omitempty / optional），与字段是标量、slice、map 还是 any 无关：
// 没写 omitempty 的 slice / map / any 照样必填。sample 里这几个字段都自带 omitempty，
// 单看它测不到这两半规则，所以单独来一遍。
func TestRequiredExemptsOnlyBoolAndPointerFields(t *testing.T) {
	type noOmitempty struct {
		Flag      bool              `yaml:"flag"`
		Ptr       *int              `yaml:"ptr"`
		PtrStruct *inner            `yaml:"ptrStruct"`
		Must      int               `yaml:"must"`
		Tags      []string          `yaml:"tags"`
		Labels    map[string]string `yaml:"labels"`
		Anything  any               `yaml:"anything"`
	}
	assert.Equal(t, []string{"anything", "labels", "must", "tags"}, generate(t, noOmitempty{})["required"])
}

// 覆盖表命中之后，字段上的 jsonschema tag 仍然生效；并且每用一次就重新调用一次构造函数，
// 一个字段的 tag 不会漏进另一个同类型的字段。
func TestTagsApplyToOverriddenTypesWithoutLeaking(t *testing.T) {
	type holder struct {
		Mode  withUnmarshaler             `yaml:"mode" jsonschema:"enum=a|b"`
		Other withUnmarshaler             `yaml:"other"`
		Ptr   *withUnmarshaler            `yaml:"ptr,omitempty"`
		List  []withUnmarshaler           `yaml:"list,omitempty"`
		Map   map[string]*withUnmarshaler `yaml:"map,omitempty"`
	}
	calls := 0
	overrides := map[reflect.Type]func() schema{
		reflect.TypeOf(withUnmarshaler{}): func() schema { calls++; return schema{"type": "string"} },
	}
	s, err := newGenerator(overrides).typeSchema(reflect.TypeOf(holder{}))
	require.NoError(t, err)
	p := props(s)

	assert.Equal(t, schema{"type": "string", "enum": []any{"a", "b"}}, p["mode"])
	assert.Equal(t, schema{"type": "string"}, p["other"], "没写 tag 的同类型字段不受影响")
	assert.Equal(t, schema{"type": []string{"string", "null"}}, p["ptr"], "指向覆盖类型的指针也命中覆盖表；指针字段不必填，所以可空")
	assert.Equal(t,
		schema{"type": []string{"array", "null"}, "items": schema{"type": "string"}}, p["list"])
	assert.Equal(t,
		schema{"type": []string{"object", "null"}, "additionalProperties": schema{"type": "string"}}, p["map"])
	assert.Equal(t, 5, calls, "五处用到，构造函数就被调用五次")
}

func TestDocumentReturnsGenerationErrors(t *testing.T) {
	out, err := newGenerator(nil).document(reflect.TypeOf(recursive{}), "t")
	require.Error(t, err)
	assert.Nil(t, out)
}

// 覆盖表里手写的 schema 是唯一可能让 JSON 编码失败的来源；失败要报出来，不能吞掉。
func TestDocumentReportsSchemasThatCannotBeEncoded(t *testing.T) {
	overrides := map[reflect.Type]func() schema{
		reflect.TypeOf(inner{}): func() schema { return schema{"type": "string", "broken": make(chan int)} },
	}
	out, err := newGenerator(overrides).document(reflect.TypeOf(inner{}), "t")
	require.Error(t, err)
	assert.Nil(t, out)
}

// 逐字节钉住输出的样子：键按字母序、缩进 2 空格、整数写成 65535 而不是 65535.0、末尾一个换行；
// 不必填的 port 写成 ["integer", "null"]，必填的 name 仍是纯 "string" 加 enum。
func TestDocumentExactBytes(t *testing.T) {
	type tiny struct {
		Name string `yaml:"name" jsonschema:"enum=a|b"`
		Port int    `yaml:"port,omitempty" jsonschema:"minimum=1,maximum=65535"`
	}
	out, err := newGenerator(nil).document(reflect.TypeOf(tiny{}), "tiny")
	require.NoError(t, err)

	want := `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "additionalProperties": false,
  "properties": {
    "name": {
      "enum": [
        "a",
        "b"
      ],
      "type": "string"
    },
    "port": {
      "maximum": 65535,
      "minimum": 1,
      "type": [
        "integer",
        "null"
      ]
    }
  },
  "required": [
    "name"
  ],
  "title": "tiny",
  "type": "object"
}
`
	assert.Equal(t, want, string(out))
}

// ---- 修复轮 1：守卫补全、递归检测覆盖所有类型、tag 与 yaml 形状里的静默接受改成报错 ----

// 旧式签名（UnmarshalYAML(func(any) error) error），指针接收者。yaml.v3 照样会调用它。
type oldStyleUnmarshaler struct{ Raw string }

func (o *oldStyleUnmarshaler) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshal(&o.Raw)
}

// 新式签名，值接收者：*T 的方法集里也有它，yaml.v3 一样会调用。
type valueReceiverUnmarshaler struct{ Raw string }

func (valueReceiverUnmarshaler) UnmarshalYAML(*yaml.Node) error { return nil }

// encoding.TextUnmarshaler：yaml.v3 会把标量交给 UnmarshalText，而不是按字段反射。
type textDecoded struct{ Raw string }

func (x *textDecoded) UnmarshalText(b []byte) error { x.Raw = string(b); return nil }

// 自定义解码的类型：每一种都必须在覆盖表里，否则报错并说清是哪种机制；进了覆盖表就放行。
func TestEveryCustomDecodingMechanismNeedsAnOverride(t *testing.T) {
	cases := []struct {
		name      string
		typ       reflect.Type
		mechanism string
	}{
		{"新式 UnmarshalYAML（指针接收者）", reflect.TypeOf(withUnmarshaler{}), "UnmarshalYAML"},
		{"新式 UnmarshalYAML（值接收者）", reflect.TypeOf(valueReceiverUnmarshaler{}), "UnmarshalYAML"},
		{"旧式 UnmarshalYAML", reflect.TypeOf(oldStyleUnmarshaler{}), "UnmarshalYAML"},
		{"encoding.TextUnmarshaler", reflect.TypeOf(textDecoded{}), "TextUnmarshaler"},
		{"time.Time（它是 TextUnmarshaler）", reflect.TypeOf(time.Time{}), "TextUnmarshaler"},
		{"time.Duration（yaml.v3 会把 5s 解析成时长）", reflect.TypeOf(time.Duration(0)), "特殊解码"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			holder := reflect.StructOf([]reflect.StructField{{
				Name: "V", Type: c.typ, Tag: `yaml:"v"`,
			}})

			_, err := newGenerator(nil).typeSchema(holder)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.mechanism, "要说清是哪种机制")
			assert.Contains(t, err.Error(), c.typ.String(), "要点名是哪个类型")
			assert.Contains(t, err.Error(), "V：", "要点名是哪个字段")

			// 指针、slice、map 里装着它也一样
			for name, wrapped := range map[string]reflect.Type{
				"指针":    reflect.PointerTo(c.typ),
				"slice": reflect.SliceOf(c.typ),
				"map":   reflect.MapOf(reflect.TypeOf(""), c.typ),
			} {
				_, err := newGenerator(nil).typeSchema(wrapped)
				require.Error(t, err, name)
				assert.Contains(t, err.Error(), c.mechanism, name)
			}

			overrides := map[reflect.Type]func() schema{
				c.typ: func() schema { return schema{"type": "string"} },
			}
			s, err := newGenerator(overrides).typeSchema(holder)
			require.NoError(t, err, "进了覆盖表就放行")
			assert.Equal(t, schema{"type": "string"}, props(s)["v"])
		})
	}
}

// 名字叫 UnmarshalYAML、签名却不是 yaml.v3 认的那两种：宁可多报一次让人确认，也不放过。
type oddSignature struct{ Raw string }

func (o *oddSignature) UnmarshalYAML(n int) {}

func TestUnmarshalYAMLByNameIsEnoughToNeedAnOverride(t *testing.T) {
	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(oddSignature{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UnmarshalYAML")
}

// 只要类型没有自定义解码，就不该被守卫误伤：time.Duration 之外的整数、普通 struct、具名标量都照常生成。
func TestPlainTypesAreNotMistakenForCustomDecoding(t *testing.T) {
	type mode string
	type plain struct {
		D  int64 `yaml:"d"`
		M  mode  `yaml:"m"`
		In inner `yaml:"in"`
	}
	s := generate(t, plain{})
	assert.Equal(t, schema{"type": "integer"}, props(s)["d"])
	assert.Equal(t, schema{"type": "string"}, props(s)["m"])
	assert.Equal(t, "object", props(s)["in"].(schema)["type"])
}

// time.Duration：yaml.v3 会把 timeout: 5s 解析成时长，反射却只会看到一个整数——
// 照实生成就是一份把 5s 标成红线的 schema。要么在覆盖表里交代，要么报错。
func TestDurationFieldsAreRejectedUnlessOverridden(t *testing.T) {
	type settings struct {
		Timeout time.Duration `yaml:"timeout"`
	}
	type withRetries struct {
		Timeout time.Duration   `yaml:"timeout"`
		Retries []time.Duration `yaml:"retries,omitempty"`
	}

	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(settings{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "settings.Timeout", "要点名类型与字段")
	assert.Contains(t, err.Error(), "time.Duration")

	// 装在 slice 里也一样
	_, err = newGenerator(nil).typeSchema(reflect.TypeOf([]time.Duration{}))
	require.Error(t, err)

	overrides := map[reflect.Type]func() schema{
		reflect.TypeOf(time.Duration(0)): func() schema { return schema{"type": "string", "pattern": "^[0-9]+[smh]$"} },
	}
	s, err := newGenerator(overrides).typeSchema(reflect.TypeOf(withRetries{}))
	require.NoError(t, err)
	assert.Equal(t, schema{"type": "string", "pattern": "^[0-9]+[smh]$"}, props(s)["timeout"])
	assert.Equal(t,
		schema{"type": []string{"array", "null"}, "items": schema{"type": "string", "pattern": "^[0-9]+[smh]$"}},
		props(s)["retries"])

	// 不是 time.Duration 的整数不受影响
	assert.Equal(t, schema{"type": "integer"}, generate(t, int64(0)))
}

// 具名的 slice / map 也能递归；以前只有 struct 检查，这两种会把栈撑爆。
type nestedMap map[string]nestedMap

type nestedSlice []nestedSlice

type mapOfSlices map[string][]mapOfSlices

func TestRecursionThroughNamedContainersIsRejected(t *testing.T) {
	type viaField struct {
		M nestedMap `yaml:"m"`
	}
	for name, v := range map[string]any{
		"具名 map 的值是自己":               nestedMap{},
		"具名 slice 的元素是自己":            nestedSlice{},
		"map 的值是 slice，slice 的元素是自己": mapOfSlices{},
		"藏在 struct 字段里":              viaField{},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newGenerator(nil).typeSchema(reflect.TypeOf(v))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "递归")
		})
	}
}

// 同一个具名容器类型并列出现、或者出错之后再用，都不算递归。
func TestNamedContainersUsedTwiceAreNotMistakenForRecursion(t *testing.T) {
	type names []string
	type twice struct {
		A names            `yaml:"a"`
		B names            `yaml:"b"`
		C map[string]names `yaml:"c"`
	}
	g := newGenerator(nil)
	_, err := g.typeSchema(reflect.TypeOf(nestedMap{}))
	require.Error(t, err)

	s, err := g.typeSchema(reflect.TypeOf(twice{}))
	require.NoError(t, err)
	assert.Equal(t, props(s)["a"], props(s)["b"])
}

// 重复的关键字：后写的会悄悄盖掉先写的，所以一律报错。
func TestRepeatedTagKeywordsAreRejected(t *testing.T) {
	str, num := reflect.TypeOf(""), reflect.TypeOf(0)
	cases := []struct {
		name string
		typ  reflect.Type
		tag  string
	}{
		{"minimum 写了两次", num, "minimum=1,minimum=2"},
		{"maximum 写了两次", num, "maximum=1,maximum=2"},
		{"enum 写了两次", str, "enum=a,enum=b"},
		{"pattern 写了两次", str, "pattern=^a$,pattern=^b$"},
		{"optional 写了两次", str, "optional,optional"},
		{"中间隔着别的关键字也算重复", str, "enum=a|b,optional,enum=c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{
				Name: "A", Type: c.typ, Tag: reflect.StructTag(`yaml:"a" jsonschema:"` + c.tag + `"`),
			}})
			_, err := newGenerator(nil).typeSchema(typ)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "重复")
			assert.Contains(t, err.Error(), "A：", "错误要带着字段名")
		})
	}
}

// 空的 enum 取值：`enum=` 没写取值、`enum=a||b` 中间漏了一个，都是手滑，不该生成一个只允许空串的 schema。
func TestEmptyEnumValuesAreRejected(t *testing.T) {
	for name, tag := range map[string]string{
		"enum 完全没写取值": "enum=",
		"中间漏了一个":      "enum=a||b",
		"末尾多了个 |":     "enum=a|b|",
		"开头多了个 |":     "enum=|a",
	} {
		t.Run(name, func(t *testing.T) {
			typ := reflect.StructOf([]reflect.StructField{{
				Name: "A", Type: reflect.TypeOf(""), Tag: reflect.StructTag(`yaml:"a" jsonschema:"` + tag + `"`),
			}})
			_, err := newGenerator(nil).typeSchema(typ)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "enum")
			assert.Contains(t, err.Error(), "为空")
			assert.Contains(t, err.Error(), "A：", "错误要带着字段名")
		})
	}
}

type inlined struct {
	A string `yaml:"a"`
}

// ,inline：yaml.v3 把被内联的字段的键摊平到外层，生成器却会生成一层嵌套对象——两边对不上，所以报错。
func TestInlineFieldsAreRejected(t *testing.T) {
	type flattenedStruct struct {
		Inner inlined `yaml:",inline"`
		B     string  `yaml:"b"`
	}
	type flattenedNamed struct {
		Inner inlined `yaml:"inner,inline"`
	}
	type flattenedMap struct {
		Extra map[string]any `yaml:",inline,omitempty"`
	}
	for name, v := range map[string]any{
		"内联一个 struct":     flattenedStruct{},
		"名字之后还带 inline":   flattenedNamed{},
		"内联一个 map（收纳未知键）": flattenedMap{},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newGenerator(nil).typeSchema(reflect.TypeOf(v))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "inline")
			assert.Contains(t, err.Error(), reflect.TypeOf(v).Name()+".", "要点名类型与字段")
		})
	}

	// 只要没写 inline 选项就照常生成：名字里带 inline 字样、或者别的选项，都不受影响
	type notInlined struct {
		Inlined inlined  `yaml:"inlineThing,omitempty"`
		Flow    []string `yaml:"flow,flow"`
	}
	s := generate(t, notInlined{})
	assert.Contains(t, props(s), "inlineThing")
}

// ---- 修复轮 2：不必填的字段允许写成显式的 null ----
//
// 一个小节下面的条目全被注释掉时（`dependencies:` 后面只剩注释），YAML 把它读成 null，CLI 当作没写。
// schema 若拒绝它，就是编辑器对一份 CLI 认可的文件画红线。必填的字段不行：`port:` 写成 null 会解码成 0，
// 校验器报"缺失"，schema 同样该拒绝。

func TestOptionalFieldsAreNullableAndRequiredFieldsAreNot(t *testing.T) {
	type shapes struct {
		Req      string            `yaml:"req"`
		ReqList  []string          `yaml:"reqList"`
		ReqAny   any               `yaml:"reqAny"`
		Flag     bool              `yaml:"flag"`
		Ptr      *int              `yaml:"ptr"`
		PtrObj   *inner            `yaml:"ptrObj"`
		Omit     string            `yaml:"omit,omitempty"`
		Float    float64           `yaml:"float,omitempty"`
		Tagged   int               `yaml:"tagged" jsonschema:"optional"`
		List     []string          `yaml:"list,omitempty"`
		Map      map[string]string `yaml:"map,omitempty"`
		Nested   inner             `yaml:"nested,omitempty"`
		Anything any               `yaml:"anything,omitempty"`
	}
	p := props(generate(t, shapes{}))

	// 必填：纯类型，不允许 null
	assert.Equal(t, schema{"type": "string"}, p["req"])
	assert.Equal(t, schema{"type": "array", "items": schema{"type": "string"}}, p["reqList"], "必填的数组也不允许 null")
	assert.Equal(t, schema{}, p["reqAny"], "any 没有类型可加：{} 本来就接受任何值，包括 null")

	// 不必填的四种来源：bool、指针、omitempty、optional 标记
	assert.Equal(t, schema{"type": []string{"boolean", "null"}}, p["flag"], "bool")
	assert.Equal(t, schema{"type": []string{"integer", "null"}}, p["ptr"], "指针")
	assert.Equal(t, schema{"type": []string{"string", "null"}}, p["omit"], "omitempty")
	assert.Equal(t, schema{"type": []string{"number", "null"}}, p["float"], "omitempty")
	assert.Equal(t, schema{"type": []string{"integer", "null"}}, p["tagged"], "jsonschema:\"optional\"")
	assert.Equal(t, schema{"type": []string{"array", "null"}, "items": schema{"type": "string"}}, p["list"])
	assert.Equal(t,
		schema{"type": []string{"object", "null"}, "additionalProperties": schema{"type": "string"}}, p["map"])
	assert.Equal(t, schema{}, p["anything"], "any + omitempty 仍是 {}")

	// 可空的对象保留自己的全部约束：null 只是多允许了一种取值，不是放松了对象内部的规则
	for _, name := range []string{"ptrObj", "nested"} {
		node := p[name].(schema)
		assert.Equal(t, []string{"object", "null"}, node["type"], name)
		assert.Equal(t, false, node["additionalProperties"], name)
		assert.Equal(t, []string{"name"}, node["required"], name)
	}
}

func TestNullableEnumIncludesNull(t *testing.T) {
	type modes struct {
		Must string `yaml:"must" jsonschema:"enum=fast|slow"`
		Opt  string `yaml:"opt,omitempty" jsonschema:"enum=fast|slow"`
	}
	p := props(generate(t, modes{}))
	assert.Equal(t, schema{"type": "string", "enum": []any{"fast", "slow"}}, p["must"], "必填：enum 里没有 null")
	assert.Equal(t,
		schema{"type": []string{"string", "null"}, "enum": []any{"fast", "slow", nil}}, p["opt"],
		"type 允许 null 而 enum 不列它，等于没允许：两处要一起")

	// 落到文件里是 JSON 的 null，不是字符串 "null"
	out, err := newGenerator(nil).document(reflect.TypeOf(modes{}), "t")
	require.NoError(t, err)
	assert.Contains(t, string(out), "\"slow\",\n        null\n")
}

// 覆盖表里的 schema 可能没有 type，而是 oneOf：null 是多出来的一支。
func TestNullableOverrideWithOneOfGainsANullBranch(t *testing.T) {
	alternatives := make([]any, 2, 8) // 多留容量：原地 append 会写进这个共享的底层数组
	alternatives[0] = schema{"type": "string"}
	alternatives[1] = schema{"type": "object"}
	overrides := map[reflect.Type]func() schema{
		reflect.TypeOf(withUnmarshaler{}): func() schema { return schema{"oneOf": alternatives} },
	}
	type holder struct {
		Must withUnmarshaler   `yaml:"must"`
		Opt  *withUnmarshaler  `yaml:"opt,omitempty"`
		List []withUnmarshaler `yaml:"list,omitempty"`
	}
	s, err := newGenerator(overrides).typeSchema(reflect.TypeOf(holder{}))
	require.NoError(t, err)
	p := props(s)

	assert.Equal(t, schema{"oneOf": []any{schema{"type": "string"}, schema{"type": "object"}}}, p["must"],
		"必填：没有 null 那一支")
	assert.Equal(t,
		schema{"oneOf": []any{schema{"type": "string"}, schema{"type": "object"}, schema{"type": "null"}}}, p["opt"])
	assert.Equal(t,
		schema{"type": []string{"array", "null"}, "items": schema{"oneOf": []any{schema{"type": "string"}, schema{"type": "object"}}}},
		p["list"], "数组可以是 null，它的元素（覆盖表里的类型）仍然不可以")

	assert.Len(t, alternatives, 2)
	assert.Nil(t, alternatives[:3][2], "没有原地改覆盖表返回的切片：下一次用到它的字段不该看到 null 那一支")
}

// 生成器只知道怎么给 type / enum / oneOf 加上 null。覆盖表里的 schema 用了别的组合关键字、
// 又落在一个不必填的字段上时，报错而不是悄悄生成一个拒绝 null 的 schema。
func TestNullableOverridesWithUnknownShapesAreRejected(t *testing.T) {
	cases := map[string]schema{
		"anyOf":      {"anyOf": []any{schema{"type": "string"}}},
		"allOf":      {"allOf": []any{schema{"type": "string"}}},
		"not":        {"not": schema{"type": "integer"}},
		"const":      {"const": "x"},
		"$ref":       {"$ref": "#/definitions/x"},
		"联合形式的 type": {"type": []string{"string", "integer"}},
	}
	for name, node := range cases {
		t.Run(name, func(t *testing.T) {
			overrides := map[reflect.Type]func() schema{
				reflect.TypeOf(withUnmarshaler{}): func() schema {
					fresh := schema{}
					for k, v := range node {
						fresh[k] = v
					}
					return fresh
				},
			}
			type optional struct {
				V *withUnmarshaler `yaml:"v,omitempty"`
			}
			type required struct {
				V withUnmarshaler `yaml:"v"`
			}

			_, err := newGenerator(overrides).typeSchema(reflect.TypeOf(optional{}))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "optional.V", "要点名类型与字段")
			assert.Contains(t, err.Error(), "null")

			_, err = newGenerator(overrides).typeSchema(reflect.TypeOf(required{}))
			assert.NoError(t, err, "必填的字段不需要可空，用什么形状都行")
		})
	}
}
