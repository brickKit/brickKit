package schemagen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

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

func TestScalarAndContainerMapping(t *testing.T) {
	s := generate(t, sample{})
	p := props(s)

	assert.Equal(t, schema{"type": "string"}, p["id"])
	assert.Equal(t, schema{"type": "boolean"}, p["enabled"])
	assert.Equal(t, schema{"type": "integer"}, p["count"], "指针与它指向的类型同形")
	assert.Equal(t, schema{"type": "number"}, p["ratio"])
	assert.Equal(t, schema{"type": "array", "items": schema{"type": "string"}}, p["tags"])
	assert.Equal(t, schema{"type": "object", "additionalProperties": schema{"type": "string"}}, p["labels"])
	assert.Equal(t, schema{}, p["anything"], "any 不加约束")
	assert.Equal(t, "array", p["items"].(schema)["type"])
	assert.Equal(t, "object", p["byName"].(schema)["type"])
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

// bool 与指针不算必填，与它们写没写 omitempty 无关。sample 里的指针字段自带 omitempty，
// 测不到指针这一半规则，所以单独来一遍。
func TestBoolAndPointerFieldsAreNeverRequired(t *testing.T) {
	type noOmitempty struct {
		Flag      bool   `yaml:"flag"`
		Ptr       *int   `yaml:"ptr"`
		PtrStruct *inner `yaml:"ptrStruct"`
		Must      int    `yaml:"must"`
	}
	assert.Equal(t, []string{"must"}, generate(t, noOmitempty{})["required"])
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
	assert.Equal(t, schema{"type": "string"}, p["ptr"], "指向覆盖类型的指针也命中覆盖表")
	assert.Equal(t, schema{"type": "array", "items": schema{"type": "string"}}, p["list"])
	assert.Equal(t, schema{"type": "object", "additionalProperties": schema{"type": "string"}}, p["map"])
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

// 逐字节钉住输出的样子：键按字母序、缩进 2 空格、整数写成 65535 而不是 65535.0、末尾一个换行。
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
      "type": "integer"
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
