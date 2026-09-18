// 本文件守着 docs/{en,zh}/architecture/component-yaml-reference.md 的**完整性**：
// 结构体里每一个 YAML 字段，参考文档里都得有一行讲它；文档里讲的每一个字段，
// 结构体里都得真的存在。
//
// # 为什么要有它
//
// docfields_test.go 只查"文档写了结构体不认识的字段"（正向，且只扫 AGENTS/README
// 那几段骨架）。"结构体新增了字段、参考文档忘了写"这个方向没有任何东西看着——
// 那组完整性检查随 design/ 归档一并撤掉了，当时的判据是"等 docs/architecture
// 长出详尽的参考文档，就在那里重新引入"。参考文档已经有了，这条测试就是那个
// 重新引入。
//
// 真相来源仍然是结构体本身（反射），不是又抄一份字段清单。
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

	"github.com/brickkit/brickkit/internal/manifest"
)

// fieldPaths 是从结构体反射出来的字段路径。
type fieldPaths struct {
	// leaves 是必须有文档的字段：标量、标量数组、map、any。
	leaves map[string]bool
	// nodes 是容器路径（结构体、数组元素、map 值）：文档里提到它们合法，但不强求。
	nodes map[string]bool
}

func structPaths(typ reflect.Type) fieldPaths {
	out := fieldPaths{leaves: map[string]bool{}, nodes: map[string]bool{}}
	collectPaths(typ, "", &out)
	return out
}

func derefType(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Pointer {
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

func collectPaths(typ reflect.Type, path string, out *fieldPaths) {
	typ = derefType(typ)

	switch typ.Kind() {
	case reflect.Struct:
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
			collectPaths(field.Type, joinFieldPath(path, name), out)
		}

	case reflect.Slice:
		if elem := derefType(typ.Elem()); elem.Kind() == reflect.Struct {
			out.nodes[path] = true
			collectPaths(elem, path+"[]", out)
			return
		}
		out.leaves[path] = true

	case reflect.Map:
		if elem := derefType(typ.Elem()); elem.Kind() == reflect.Struct {
			out.nodes[path] = true
			collectPaths(elem, path+".<key>", out)
			return
		}
		out.leaves[path] = true

	default:
		out.leaves[path] = true
	}
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

// component.yaml 的参考文档必须覆盖结构体里的每一个字段，且不多讲结构体没有的。
func TestComponentYAMLReferenceCoversEveryField(t *testing.T) {
	fields := structPaths(reflect.TypeOf(manifest.Manifest{}))
	require.GreaterOrEqual(t, len(fields.leaves), 25,
		"只反射出 %d 个字段——structPaths 坏了，这条测试的结论不可信", len(fields.leaves))

	for _, lang := range []string{"en", "zh"} {
		rel := filepath.Join("docs", lang, "architecture", "component-yaml-reference.md")
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		require.NoError(t, err)

		documented := documentedPaths(string(body))
		require.GreaterOrEqual(t, len(documented), 25,
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
