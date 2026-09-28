package yamlcheck

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type tmItem struct {
	N int `yaml:"n"`
}

// tmCustom 自己解析：类型检查要把整个节点交给它，而不是替它往下走。
type tmCustom struct{ v string }

func (c *tmCustom) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return errors.New("must be written as a single value")
	}
	c.v = n.Value
	return nil
}

type tmTarget struct {
	Port   int               `yaml:"port"`
	On     bool              `yaml:"on"`
	Ratio  float64           `yaml:"ratio"`
	Name   string            `yaml:"name"`
	Items  []tmItem          `yaml:"items"`
	Labels map[string]string `yaml:"labels"`
	Any    any               `yaml:"any"`
	Ptr    *tmItem           `yaml:"ptr"`
	Custom tmCustom          `yaml:"custom"`
	Hidden string            `yaml:"-"`
}

func typeMismatches(t *testing.T, doc string) map[string]string {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &root))
	out := map[string]string{}
	TypeMismatches(root.Content[0], reflect.TypeOf(tmTarget{}), func(field, message string) {
		out[field] = message
	})
	return out
}

func TestTypeMismatchesNamesTheField(t *testing.T) {
	cases := map[string]string{
		`{"port": "abc"}`:                   "port",
		`on: maybe`:                         "on",
		`ratio: fast`:                       "ratio",
		`name: [a, b]`:                      "name",
		`items: [{n: x}]`:                   "items[0].n",
		`items: just-one`:                   "items",
		`labels: {a: [1]}`:                  "labels.a",
		`labels: [a]`:                       "labels",
		`ptr: {n: "x"}`:                     "ptr.n",
		`ptr: 3`:                            "ptr",
		`custom: [1]`:                       "custom",
		`{"items": [{"n": 1}, {"n": "y"}]}`: "items[1].n",
	}
	for doc, field := range cases {
		got := typeMismatches(t, doc)
		require.Len(t, got, 1, doc)
		assert.Contains(t, got, field, doc)
		assert.NotContains(t, got[field], "line ", "路径已经指明了位置，不再拖着 yaml 库的行号：%s", doc)
	}
}

func TestTypeMismatchesSaysWhatWasExpected(t *testing.T) {
	got := typeMismatches(t, `{"port": "abc"}`)
	assert.Contains(t, got["port"], "integer")
	assert.Contains(t, got["port"], "abc")

	got = typeMismatches(t, `items: just-one`)
	assert.Contains(t, got["items"], "array")

	got = typeMismatches(t, `custom: [1]`)
	assert.Contains(t, got["custom"], "single value", "自己解析的类型，原因用它自己的话")
}

func TestTypeMismatchesAcceptsWellTypedDocuments(t *testing.T) {
	assert.Empty(t, typeMismatches(t, `
port: 8080
on: true
ratio: 1.5
name: hello
items: [{n: 1}, {n: 2}]
labels: {a: b}
any: {whatever: [1, 2]}
ptr: {n: 3}
custom: plain
unknown: [1, 2]
`), "未知字段归 Walk 管，这里不重复报")
	assert.Empty(t, typeMismatches(t, `{port: null, items: null, ptr: null}`), "null 解出零值，不算不匹配")
}

// 与 yaml 库一致：它解不进的，这里一定报；它解得进的，这里一定不报。
// 类型检查只是把解码器的结论换成带字段路径的说法，不能自己另立一套规则。
func TestTypeMismatchesAgreesWithTheDecoder(t *testing.T) {
	docs := []string{
		`{"port": "abc"}`, `{"port": "8080"}`, `{port: 8080}`, `on: maybe`, `on: yes`, `on: true`,
		`ratio: fast`, `ratio: 2`, `name: [a, b]`, `name: 12`, `items: [{n: x}]`, `items: just-one`,
		`labels: {a: [1]}`, `labels: {a: 1}`, `ptr: 3`, `custom: [1]`, `custom: x`, `{port: null}`,
		`{"items": [{"n": 1}, {"n": "y"}]}`, `any: [1]`,
	}
	for _, doc := range docs {
		var root yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(doc), &root))
		decodeErr := root.Content[0].Decode(&tmTarget{})
		reported := typeMismatches(t, doc)
		assert.Equal(t, decodeErr != nil, len(reported) > 0, "%s: decode error %v, reported %v", doc, decodeErr, reported)
	}
}
