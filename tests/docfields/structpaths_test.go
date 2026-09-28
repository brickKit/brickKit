package docfields_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

// recursiveTree 引用了自己：按类型展开，这条路本来没有尽头。
type recursiveTree struct {
	Name     string          `yaml:"name"`
	Children []recursiveTree `yaml:"children"`
}

// 合理范围内的递归照常展开，超出范围就停下并点名——绝不能一直展开到内存耗尽。
func TestStructPathsStopsOnRecursiveType(t *testing.T) {
	got := structPaths(reflect.TypeOf(recursiveTree{}))

	for _, want := range []string{"name", "children[].name", "children[].children[].name"} {
		assert.True(t, got.leaves[want], "合理深度内的 %s 要照常展开", want)
	}
	assert.False(t, got.leaves["children[].children[].children[].name"], "超过上限的那一层不该再展开")
	assert.Equal(t, []string{"docfields_test.recursiveTree"}, got.recursive, "递归超限的类型要被点名，而不是无声截断")
}

// yaml.Node 是"原样保留的 YAML 值"：对用户来说它就是一个值，和 string 一样是叶子。
// 它的 Content / Alias 又指回 Node——展开它，就是上面那个没有尽头的环。
func TestStructPathsTreatsYAMLNodeAsLeaf(t *testing.T) {
	type withRawValues struct {
		Vars  map[string]yaml.Node `yaml:"vars"`
		Extra yaml.Node            `yaml:"extra"`
	}
	got := structPaths(reflect.TypeOf(withRawValues{}))

	assert.True(t, got.leaves["vars"], "map[string]yaml.Node 与 map[string]string 一样，整体是叶子")
	assert.True(t, got.leaves["extra"])
	assert.Len(t, got.leaves, 2, "yaml.Node 内部的字段不是用户能写的字段：%v", got.leaves)
	assert.Empty(t, got.recursive)
}

// 三份字段参考对着的结构体里，不允许有递归超限的类型：它的字段写不全。
func TestReferenceStructsHaveNoRecursiveTypes(t *testing.T) {
	for _, ref := range referenceDocs {
		assert.Empty(t, structPaths(ref.typ).recursive,
			"%s：这些类型引用了自己，字段路径没有尽头。原样保留的值加进 opaqueTypes，"+
				"不是用户能写的字段标 yaml:\"-\"", ref.file)
	}
}

// selfPointer 是 Go 允许的自指指针类型：一路解指针永远解不到头。
type selfPointer *selfPointer

// 自指指针不能让展开在解指针那一步原地死循环，同样要被点名。
func TestStructPathsStopsOnSelfReferentialPointer(t *testing.T) {
	type holder struct {
		P selfPointer `yaml:"p"`
	}
	got := structPaths(reflect.TypeOf(holder{}))
	assert.Equal(t, []string{"docfields_test.selfPointer"}, got.recursive)
}

// yaml:",inline" 的结构体，键摊平到外层——路径里不能多出一段字段名。
// deploy.yaml 的组件条目就是这样：Component 内联了 Entry。
type InlineEntry struct {
	ID   string `yaml:"id"`
	Mode string `yaml:"mode"`
}

func TestStructPathsFlattensInlineStructs(t *testing.T) {
	type component struct {
		InlineEntry `yaml:",inline"`
		Members     []InlineEntry `yaml:"members"`
	}
	type file struct {
		Components []component `yaml:"components"`
	}
	got := structPaths(reflect.TypeOf(file{}))

	for _, want := range []string{"components[].id", "components[].mode", "components[].members[].id"} {
		assert.True(t, got.leaves[want], "缺 %s：%v", want, got.leaves)
	}
	for path := range got.leaves {
		assert.NotContains(t, path, "inlineentry", "内联结构体的字段名不该出现在路径里：%s", path)
	}
}

// 结构体藏在多层容器里也要展开：map 的值是数组、数组的元素是数组、定长数组。
func TestStructPathsExpandsStructsInsideNestedContainers(t *testing.T) {
	type item struct {
		Name string `yaml:"name"`
	}
	type file struct {
		Groups map[string][]item `yaml:"groups"`
		Grid   [][]item          `yaml:"grid"`
		Pair   [2]item           `yaml:"pair"`
		Tags   []string          `yaml:"tags"`
	}
	got := structPaths(reflect.TypeOf(file{}))

	for _, want := range []string{"groups.<key>[].name", "grid[][].name", "pair[].name", "tags"} {
		assert.True(t, got.leaves[want], "缺 %s：%v", want, got.leaves)
	}
	assert.Len(t, got.leaves, 4, "%v", got.leaves)
}
