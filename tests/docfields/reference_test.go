// 本文件守着 docs/{en,zh}/11-reference/ 下三份字段参考文档的**完整性**：
// 01-component-yaml-schema.md 对着 manifest.Manifest，02-brickkit-yaml-schema.md
// 对着 projfile.File，03-deploy-yaml-schema.md 对着 deployfile.File（deploy.yaml 与
// deploy.local.yaml 是同一个结构）。结构体里每一个 YAML 字段，参考文档里都得有一行
// 讲它；文档里讲的每一个字段，结构体里都得真的存在。
//
// # 为什么要有它
//
// docfields_test.go 只查"文档写了结构体不认识的字段"（正向，且只扫 AGENTS/README
// 那几段骨架）。"结构体新增了字段、参考文档忘了写"这个方向没有任何东西看着——
// 字段参考文档就是详尽版，这条测试守的正是那个方向。
//
// 真相来源仍然是结构体本身（反射），不是又抄一份字段清单。所以结构体标签必须说
// 真话：自己实现了 UnmarshalYAML 的类型（如 manifest.ComponentDep）要把"不是作者
// 能写的键"标成 yaml:"-"，否则反射读出来的字段名会撒谎。
package docfields_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/projfile"
)

// fieldPaths 是从结构体反射出来的字段路径。
type fieldPaths struct {
	// leaves 是必须有文档的字段：标量、标量数组、map、any。
	leaves map[string]bool
	// nodes 是容器路径（结构体、数组元素、map 值）：文档里提到它们合法，但不强求。
	nodes map[string]bool
	// recursive 是递归超过上限、展开被截停的类型。
	recursive []string
}

// opaqueTypes 是"原样保留的值"：对写文件的人来说它就是一个值，和 string 一样是叶子，
// 不往里展开。
var opaqueTypes = map[reflect.Type]string{
	// deploy.yaml 的 vars 值按原文保留（VER: 1.10 是 "1.10"，不是 1.1），所以存成
	// YAML 节点再解释。它的 Content / Alias 字段又指回 Node——展开它就是一条没有尽头的路。
	reflect.TypeOf(yaml.Node{}): "原样保留的 YAML 值",
}

// maxSameTypeOnPath 是同一个结构体类型在一条展开链上最多出现的次数。
//
// 按**类型**展开没有"数据有多深"这个自然终点：类型引用了自己，展开就永远停不下来
// （真出过：vars 的 yaml.Node，一次测试吃掉 12 GB 内存，连编辑器一起被系统杀掉）。
// 合理范围内的嵌套照常展开；超出上限的那条分支停下，并把类型记进 recursive——
// 测试要求它为空，所以递归类型会被点名，而不是被无声截断。
// 类型的种类有限，每种最多出现这么多次，展开链的长度就有上限，一定会结束。
const maxSameTypeOnPath = 3

func structPaths(typ reflect.Type) fieldPaths {
	out := fieldPaths{leaves: map[string]bool{}, nodes: map[string]bool{}}
	collectPaths(typ, "", map[reflect.Type]int{}, &out)
	return out
}

// expandable 判断一个元素类型要不要继续往里展开：结构体才展开，原样保留的值不展开。
func expandable(typ reflect.Type) bool {
	_, opaque := opaqueTypes[typ]
	return typ.Kind() == reflect.Struct && !opaque
}

// derefType 一路解指针。Go 允许自指的指针类型（type T *T），那样永远解不到头；
// 遇到解过的类型就停下，返回的类型仍是指针——只有指针成环时才会这样。
func derefType(typ reflect.Type) reflect.Type {
	seen := map[reflect.Type]bool{}
	for typ.Kind() == reflect.Pointer && !seen[typ] {
		seen[typ] = true
		typ = typ.Elem()
	}
	return typ
}

func joinFieldPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func collectPaths(typ reflect.Type, path string, onPath map[reflect.Type]int, out *fieldPaths) {
	typ = derefType(typ)
	if _, opaque := opaqueTypes[typ]; opaque {
		out.leaves[path] = true
		return
	}

	switch typ.Kind() {
	case reflect.Pointer: // 解完指针还是指针：指针成环
		out.recursive = appendOnce(out.recursive, typ.String())

	case reflect.Struct:
		if onPath[typ] >= maxSameTypeOnPath {
			out.recursive = appendOnce(out.recursive, typ.String())
			return
		}
		onPath[typ]++
		defer func() { onPath[typ]-- }()

		if path != "" {
			out.nodes[path] = true
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			name := strings.Split(field.Tag.Get("yaml"), ",")[0]
			switch name {
			case "-":
				continue
			case "":
				name = strings.ToLower(field.Name)
			}
			collectPaths(field.Type, joinFieldPath(path, name), onPath, out)
		}

	case reflect.Slice:
		if elem := derefType(typ.Elem()); expandable(elem) || elem.Kind() == reflect.Pointer {
			out.nodes[path] = true
			collectPaths(elem, path+"[]", onPath, out)
			return
		}
		out.leaves[path] = true

	case reflect.Map:
		if elem := derefType(typ.Elem()); expandable(elem) || elem.Kind() == reflect.Pointer {
			out.nodes[path] = true
			collectPaths(elem, path+".<key>", onPath, out)
			return
		}
		out.leaves[path] = true

	default:
		out.leaves[path] = true
	}
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

var (
	backtickToken = regexp.MustCompile("`([^`]+)`")
	pathLikeToken = regexp.MustCompile(`^\.?[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*|\[\]|\.<key>)*$`)
)

// documentedPaths 从一份参考文档里抽出它声称讲过的字段路径：
// 表格每一行的**第一列**，加上各级标题里的反引号内容。
//
// 一格里写了 `a.b.cpu` / `.memory` 时，以 "." 开头的那个按上一个路径的兄弟处理。
func documentedPaths(markdown string) []string {
	var out []string

	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)

		var cell string
		switch {
		case strings.HasPrefix(trimmed, "#"):
			cell = trimmed
		case strings.HasPrefix(trimmed, "|"):
			cell = firstTableCell(trimmed)
		default:
			continue
		}

		previous := ""
		for _, match := range backtickToken.FindAllStringSubmatch(cell, -1) {
			token := strings.TrimSpace(match[1])
			if !pathLikeToken.MatchString(token) {
				continue
			}
			if strings.HasPrefix(token, ".") {
				if previous == "" {
					continue
				}
				token = previous[:strings.LastIndex(previous, ".")] + token
			}
			out = append(out, token)
			previous = token
		}
	}
	return out
}

func firstTableCell(row string) string {
	row = strings.TrimPrefix(row, "|")
	if i := strings.Index(row, "|"); i >= 0 {
		return row[:i]
	}
	return row
}

// referenceDrift 比较结构体与文档：missing 是文档没讲的字段，phantom 是文档讲了但结构体没有的。
func referenceDrift(documented []string, fields fieldPaths) (missing, phantom []string) {
	seen := map[string]bool{}
	for _, path := range documented {
		seen[path] = true
	}

	for leaf := range fields.leaves {
		if !seen[leaf] {
			missing = append(missing, leaf)
		}
	}
	for path := range seen {
		if !fields.leaves[path] && !fields.nodes[path] {
			phantom = append(phantom, path)
		}
	}
	sort.Strings(missing)
	sort.Strings(phantom)
	return missing, phantom
}

// 每份字段参考文档，对着它负责的那个结构体。
var referenceDocs = []struct {
	file string
	typ  reflect.Type
}{
	{"01-component-yaml-schema.md", reflect.TypeOf(manifest.Manifest{})},
	{"02-brickkit-yaml-schema.md", reflect.TypeOf(projfile.File{})},
	{"03-deploy-yaml-schema.md", reflect.TypeOf(deployfile.File{})},
}

// minFields 是反射与文档解析两侧的下限。它只用来发现"解析坏了、几乎什么都没取到"，
// 不是字段数的约定：三层重构后 brickkit.yaml 只剩十几个字段，下限按最瘦的那个定。
const minFields = 10

// 反射这一侧先单独验：它不依赖文档，文档还没写时也必须是绿的。
// 反射出来的字段太少，说明 structPaths 坏了，下面那条测试的结论就不可信。
func TestReferenceStructsReflectTheirFields(t *testing.T) {
	for _, ref := range referenceDocs {
		fields := structPaths(ref.typ)
		require.GreaterOrEqual(t, len(fields.leaves), minFields,
			"%s 只反射出 %d 个字段——structPaths 坏了", ref.file, len(fields.leaves))
	}
}

// 字段参考文档必须覆盖结构体里的每一个字段，且不多讲结构体没有的。
func TestYAMLReferencesCoverEveryField(t *testing.T) {
	for _, ref := range referenceDocs {
		fields := structPaths(ref.typ)

		for _, lang := range []string{"en", "zh"} {
			rel := filepath.Join("docs", lang, "11-reference", ref.file)
			body, err := os.ReadFile(filepath.Join(repoRoot, rel))
			require.NoError(t, err)

			documented := documentedPaths(string(body))
			require.GreaterOrEqual(t, len(documented), minFields,
				"%s 只抽出 %d 个字段路径——documentedPaths 坏了，这条测试的结论不可信", rel, len(documented))

			missing, phantom := referenceDrift(documented, fields)
			for _, path := range missing {
				t.Errorf("%s：结构体有字段 %s，参考文档没有任何一行讲它\n"+
					"   在对应的表格里补一行（第一列写完整路径，用反引号包住）", rel, path)
			}
			for _, path := range phantom {
				t.Errorf("%s：参考文档讲了 %s，结构体里没有这个字段\n"+
					"   字段被删了/改名了，或者这一格第一列的路径写错了", rel, path)
			}
		}
	}
}

// 检测器自己要能两个方向都抓得到——否则上面那条测试的"全绿"没有意义。
func TestReferenceDriftDetectorCatchesBothDirections(t *testing.T) {
	fields := fieldPaths{
		leaves: map[string]bool{"metadata.id": true, "metadata.vendor": true, "deployment.resources.requests.cpu": true, "deployment.resources.requests.memory": true},
		nodes:  map[string]bool{"metadata": true, "deployment.resources": true, "deployment.resources.requests": true},
	}

	markdown := "## `metadata`\n" +
		"\n" +
		"| Field | Type |\n" +
		"| --- | --- |\n" +
		"| `metadata.id` | string |\n" +
		"| `metadata.vendr` | string |\n" +
		"| `deployment.resources.requests.cpu` / `.memory` | string |\n"

	missing, phantom := referenceDrift(documentedPaths(markdown), fields)
	require.Equal(t, []string{"metadata.vendor"}, missing, "结构体有、文档没写的字段必须被报出来")
	require.Equal(t, []string{"metadata.vendr"}, phantom, "文档写了、结构体没有的字段必须被报出来")
}
