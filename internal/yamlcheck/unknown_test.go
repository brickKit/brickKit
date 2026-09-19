package yamlcheck

import (
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 这份结构体只服务本文件：把猜测规则钉在一个不会随业务变动的形状上。
type sample struct {
	Username  string `yaml:"username"`
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	ExtraName string `yaml:"extraName"`
}

// 距离太远就不猜——乱猜一个八竿子打不着的字段名，比不给建议更误导人。
func TestClosestFieldOnlyGuessesWhenClose(t *testing.T) {
	known := knownFieldsOf(reflect.TypeOf(sample{}))

	assert.Equal(t, "username", closestField("user", known), "前缀：少打了后半截")
	assert.Equal(t, "username", closestField("usrname", known), "编辑距离：打错一个字母")
	assert.Empty(t, closestField("completelyUnrelated", known))
}

// 前缀规则要求至少 3 个字符：一两个字母能命中一大片，挑出来的多半不是想要的。
func TestClosestFieldIgnoresVeryShortPrefixes(t *testing.T) {
	known := knownFieldsOf(reflect.TypeOf(sample{}))

	assert.Empty(t, closestField("h", known))
	assert.Empty(t, closestField("po", known))
	assert.Equal(t, "port", closestField("por", known), "三个字符起才猜")
}

// 命中多个前缀时取最短的：那通常就是使用者少打了后半截的那一个。
func TestClosestFieldPrefersTheShortestPrefixMatch(t *testing.T) {
	type both struct {
		Ext     string `yaml:"ext"`
		ExtName string `yaml:"extName"`
	}
	known := knownFieldsOf(reflect.TypeOf(both{}))

	assert.Equal(t, "ext", closestField("ex", known))
}

// yaml:"-" 的字段不该出现在"可用的字段"里——它压根不能写进 YAML。
func TestKnownFieldsSkipsExcluded(t *testing.T) {
	type withExcluded struct {
		Real   string `yaml:"real"`
		Hidden string `yaml:"-"`
		unexp  string //nolint:unused // 未导出字段同样不该出现
	}
	known := knownFieldsOf(reflect.TypeOf(withExcluded{}))

	assert.Contains(t, known, "real")
	assert.NotContains(t, known, "-")
	assert.NotContains(t, known, "hidden")
	assert.NotContains(t, known, "unexp")
}

// KnownFields 是导出的：internal/schemagen 拿它生成 JSON Schema 的 properties。
// 这条钉住导出它的理由：它给出的键集合，就是 Walk 放行的键集合，一个不多、一个不少，
// 编辑器里的红线与 CLI 报的"未知字段"才不会各说各话。
func TestKnownFieldsIsExactlyTheSetWalkAccepts(t *testing.T) {
	type shape struct {
		Tagged   string `yaml:"tagged,omitempty"`
		NoTag    string
		Hidden   string `yaml:"-"`
		internal string //nolint:unused // 未导出字段既不在 KnownFields 里，也不被 Walk 放行
	}
	typ := reflect.TypeOf(shape{})

	known := KnownFields(typ)
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	sort.Strings(names)
	require.Equal(t, []string{"notag", "tagged"}, names)

	problemsFor := func(key string) int {
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(key+": x\n"), &doc))
		p := clierr.NewProblemSet(clierr.CodeConfigInvalid, "错误")
		Walk(doc.Content[0], typ, p)
		return p.Len()
	}
	for _, name := range names {
		assert.Zero(t, problemsFor(name), "KnownFields 里的键 %q 必须被 Walk 放行", name)
	}
	for _, name := range []string{"-", "Hidden", "hidden", "internal", "Tagged", "typo"} {
		assert.Equal(t, 1, problemsFor(name), "不在 KnownFields 里的键 %q 必须被 Walk 拒绝", name)
	}
}
