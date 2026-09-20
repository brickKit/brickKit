package schemagen

// 本文件把两份真实的 schema（component.yaml 与 brickkit.yaml）钉在三样东西上：
//
//   - 签入仓库的 schemas/*.json——防漂移；
//   - 真实的校验器（manifest.Parse / config.ParseConfig）——schema 不能比校验器更严：
//     编辑器里为一份 CLI 接受的文件画红线，比没有 schema 更糟；
//   - 结构体本身（yamlcheck.KnownFields、覆盖表）——schema 认识的键与 CLI 认识的键是同一份。
//
// 约束测试里的取值都是先拿真实校验器验过的：表里写的"合法/非法"不是凭印象，是校验器的实际反应。
// 某个 tag 与校验器对不上时，改 tag，不要放宽这里的测试。
//
// 已知的"schema 比 CLI 更严"共三类，每一类都是有意保留的，不是疏忽。下面的
// TestKnownStricterThanTheCLICasesAreReal 与 TestUnknownKeysInConfigSchemaPropertiesAreWarnedNotRejected
// 用真实的解析器把它们各钉了一遍：CLI 哪天改了行为，那里会红，说明这份清单过期了。
//
//  1. `${VAR}` 写进有封闭取值或 pattern 的字段（brickkit.yaml 的 deploy.target、sources[].type、
//     resources[].kind、components[].version）：ParseConfig 先展开环境变量再校验，schema 检查的是字面文本。
//     文档里没有这种写法；多环境的做法是每个环境一份自足的 brickkit.yaml。
//  2. yaml.v3 的宽容解码，schema 不跟着放宽：不加引号的非字符串标量写进字符串字段（`password: 123456`、
//     `project: 2024`）被 yaml.v3 照字面转成字符串，schema 的 type: string 标红——加引号才是对的 YAML 写法，
//     放宽会把类型提示的价值整个抹掉；列表里的 null 元素（一行没写完的 `-`）在有的位置被悄悄丢掉
//     （dependencies.components、tags），schema 同样指出来——那几乎总是笔误；map 里的 null 值
//     （`deploy.ingressAnnotations: {k: null}`、`installer.publicKeys`、`allowFrom[].podSelector`）CLI 接受，
//     schema 要求值是字符串；bool 字段写 `yes` / `on` 被读成 true，schema 只认 true / false；
//     整数字段写小数（`resources[].port: 5432.5`）被悄悄截断成 5432，schema 的 integer 不认小数。
//  3. configSchema 里属性声明的多余键（`format: uri`、拼错的 `defualt`）：yamlcheck.Walk 不往 map 的值里下钻，
//     manifest.Parse 不拒绝，CLI 只在 PropertyKeyWarnings（lint / publish / add --local）里警告。这些键不会生效，
//     所以 schema 在这里保持封闭：编辑器标红与 CLI 的警告说的是同一件事。

import (
	"encoding"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// 两份合法基准：字段尽量铺满，好让"删掉某个必填字段 / 改成某个值"都有落脚点。
// 先跑 TestBaselinesAreValid——基准自己不合法，后面所有断言都没有意义。
const baselineComponent = `apiVersion: brickkit/v1
kind: Component
metadata:
  id: demo/hello
  name: Hello
  version: 1.0.0
  description: 基准
artifacts:
  - type: api-contract
    files: [openapi.yaml]
dependencies:
  components:
    - demo/other@1.0.0
    - id: demo/weak@1.0.0
      optional: true
  resources:
    - kind: database
      engine: postgresql
configSchema:
  type: object
  properties:
    pageSize:
      type: integer
      default: 20
    tags:
      type: array
      items: {}
deployment:
  type: container
  image: registry.example.com/demo/hello:1.0.0
  port: 8080
  extraPorts:
    - name: grpc
      port: 9090
migration:
  command: ["./migrate"]
healthCheck:
  type: http
  path: /healthz
`

// allowTo 的目标位置必须写 namespace 或 cidr 二选一——即使已经写了 resource：
// validateEgress 不因为有 resource 就放过"缺少目标位置"。
const baselineProject = `project: demo
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    ingressController:
      namespace: ingress-nginx
    allowFrom:
      - name: prometheus
        namespace: monitoring
    egress:
      enabled: true
      allowTo:
        - name: db
          resource: main-db
          namespace: databases
sources:
  - id: local-dev
    type: local
    path: ./components
components:
  - id: demo/hello
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    bindings:
      - componentId: demo/hello
        database: hello
`

// document 是一份要检查的 YAML 文档：它的基准、生成函数与真实的解析入口。
type document struct {
	baseline string
	generate func() ([]byte, error)
	parse    func(data []byte) error
}

var documents = map[string]document{
	"component": {
		baseline: baselineComponent,
		generate: Component,
		parse:    func(data []byte) error { _, err := manifest.Parse(data, "component.yaml"); return err },
	},
	"project": {
		baseline: baselineProject,
		generate: Project,
		parse:    func(data []byte) error { _, err := config.ParseConfig(data, "brickkit.yaml"); return err },
	},
}

// ---- 辅助：改写 YAML、读校验错误 ----

// problemFields 取出一个校验错误里所有明细的键（字段路径）。
// 第一个键是来源标签"文件"，不是字段；它不会与任何字段路径相等，留着无妨。
func problemFields(err error) []string {
	if err == nil {
		return nil
	}
	var out []string
	for _, d := range clierr.As(err).Details {
		out = append(out, d.Key)
	}
	return out
}

// mutate 解析 base，把 path 指向的位置改成 value（remove 时删掉那个键），再编码回 YAML。
// path 的元素：string 表示映射的键，int 表示数组下标。
func mutate(t *testing.T, base string, path []any, value any, remove bool) []byte {
	t.Helper()
	var root any
	require.NoError(t, yaml.Unmarshal([]byte(base), &root))
	set(t, &root, path, value, remove)
	out, err := yaml.Marshal(root)
	require.NoError(t, err)
	return out
}

func set(t *testing.T, node *any, path []any, value any, remove bool) {
	t.Helper()
	if len(path) == 0 {
		*node = value
		return
	}
	switch key := path[0].(type) {
	case string:
		m := (*node).(map[string]any)
		if len(path) == 1 && remove {
			delete(m, key)
			return
		}
		child := m[key]
		set(t, &child, path[1:], value, remove)
		m[key] = child
	case int:
		s := (*node).([]any)
		if len(path) == 1 && remove {
			// 从数组里删一项会让后面的元素挪位，下标从此指向别的东西；把它改成 null 又不是"删除"。
			// 不猜：要去掉一个元素就改写整个数组。
			panic(fmt.Sprintf("删除路径不能以数组下标结尾（下标 %d）：要去掉一个元素就改写整个数组", key))
		}
		child := s[key]
		set(t, &child, path[1:], value, remove)
		s[key] = child
	}
}

// valueAt 读出 path 指向的值；路径走不通时返回 false。
func valueAt(root any, path []any) (any, bool) {
	node := root
	for _, step := range path {
		switch key := step.(type) {
		case string:
			m, ok := node.(map[string]any)
			if !ok {
				return nil, false
			}
			if node, ok = m[key]; !ok {
				return nil, false
			}
		case int:
			s, ok := node.([]any)
			if !ok || key >= len(s) {
				return nil, false
			}
			node = s[key]
		}
	}
	return node, true
}

// fieldPath 把数据路径写成校验器报错时用的字段名：[]any{"deployment", "extraPorts", 0, "port"}
// → deployment.extraPorts[0].port。
func fieldPath(path []any) string {
	var b strings.Builder
	for _, step := range path {
		switch key := step.(type) {
		case string:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(key)
		case int:
			fmt.Fprintf(&b, "[%d]", key)
		}
	}
	return b.String()
}

// fieldRelation 是校验器报的字段与被改动的字段（被删掉，或被写成 null）之间的关系。
type fieldRelation int

const (
	unrelated              fieldRelation = iota
	reportedOnAncestor                   // 报在被改字段的祖先上
	reportedOnFieldOrBelow               // 报在被改字段本身，或它的后代上
)

// relationOf 在校验器报的所有字段里，找与 changed 关系最近的一个。
//
// 它比"必须报在这个字段本身"宽松，要知道宽到哪里：报在被改字段的**祖先**上也算相关。这一支是为映射写法的
// 依赖专门留的——删掉（或写成 null）`dependencies.components[1].id` 之后，Ref 是空的，校验器只能指着依赖项
// 本身说"缺失"，报的是 `dependencies.components[1]`，不是 `.id`。黄金表里除了这一行，没有哪一行靠祖先这一支
// 才算相关：其余各行校验器报的都是被改的字段本身或它的后代（删掉整个 metadata 会报 metadata.id、
// metadata.name……）。这一点由 ancestorOnly 与用到它的测试保证，不只是这句注释——将来某一行只能靠祖先才
// 对得上，必须来这里登记。
//
// 它拦得住"失败其实是别的字段引起的"；拦不住"报在同一个祖先上的另一个字段"，这张表里没有那种情形。
func relationOf(reported []string, changed string) fieldRelation {
	under := func(child, parent string) bool {
		return strings.HasPrefix(child, parent+".") || strings.HasPrefix(child, parent+"[")
	}
	best := unrelated
	for _, field := range reported {
		switch {
		case field == changed || under(field, changed):
			return reportedOnFieldOrBelow
		case under(changed, field):
			best = reportedOnAncestor
		}
	}
	return best
}

// ancestorOnly 登记黄金表里校验器只报在被改字段祖先上的那些行（键是被改字段的路径）。
// 每一项都会被核对是不是还成立（TestRequiredFieldsAreReallyRequiredByTheValidators 的末尾）：
// 校验器改成直接报在那个字段上，或者黄金表里没了这一行，测试都会失败，名单不会悄悄过期。
var ancestorOnly = map[string]bool{
	"dependencies.components[1].id": true, // 映射写法的依赖：见 relationOf
}

// ---- 辅助：读 schema、按路径定位节点 ----
//
// 路径的写法：从根往下，属性用 /，数组的元素用 []，map 的值用 {}，oneOf 的第 n 支用 #oneOf[n]。
// 根是空串。例：deployment/extraPorts[]/port、configSchema/properties{}/type、
// dependencies/components[]#oneOf[1]/id。

// schemaRoot 是根节点的路径。
const schemaRoot = ""

func joinSchemaPath(path, name string) string {
	if path == schemaRoot {
		return name
	}
	return path + "/" + name
}

// walkSchema 走遍一份 schema 的每个节点，把它的路径与内容交给 visit。
func walkSchema(node map[string]any, path string, visit func(path string, node map[string]any)) {
	visit(path, node)
	if props, ok := node["properties"].(map[string]any); ok {
		for name, child := range props {
			walkSchema(child.(map[string]any), joinSchemaPath(path, name), visit)
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		walkSchema(items, path+"[]", visit)
	}
	// struct 节点的 additionalProperties 是 false（布尔），只有 map 节点才是一个 schema
	if values, ok := node["additionalProperties"].(map[string]any); ok {
		walkSchema(values, path+"{}", visit)
	}
	if alternatives, ok := node["oneOf"].([]any); ok {
		for i, alternative := range alternatives {
			walkSchema(alternative.(map[string]any), fmt.Sprintf("%s#oneOf[%d]", path, i), visit)
		}
	}
}

// loadSchema 生成一份 schema 并解回 JSON 对象——测试看的是落到文件里的形状（数字是 float64、
// 列表是 []any），不是生成器内存里的 map。
func loadSchema(t *testing.T, doc string) map[string]any {
	t.Helper()
	data, err := documents[doc].generate()
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	return root
}

// indexSchema 把一份 schema 的每个节点按路径列成表。
func indexSchema(t *testing.T, doc string) map[string]map[string]any {
	t.Helper()
	index := map[string]map[string]any{}
	walkSchema(loadSchema(t, doc), schemaRoot, func(path string, node map[string]any) {
		index[path] = node
	})
	return index
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// requiredOf 读出一个节点的 required 列表（没有时为 nil）。
func requiredOf(node map[string]any) []string {
	var out []string
	list, _ := node["required"].([]any)
	for _, item := range list {
		out = append(out, item.(string))
	}
	return out
}

func anySlice[T any](items []T) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

// ---- 基准自检 ----

func TestBaselinesAreValid(t *testing.T) {
	for name, d := range documents {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, d.parse([]byte(d.baseline)), "基准自己不合法，后面所有断言都没有意义")
		})
	}
}

// ---- 块一：防漂移 ----

func TestCheckedInSchemasAreUpToDate(t *testing.T) {
	files, err := Files()
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join("..", "..", "schemas", name))
		require.NoError(t, err, "缺 schemas/%s：先跑 make generate-schemas", name)
		assert.Equal(t, string(want), string(got),
			"schemas/%s 与结构体生成的不一致：改了 manifest / config 的结构体或 jsonschema tag 之后，要跑 make generate-schemas 并提交结果", name)
	}

	// schemas/ 里全是生成物：Files() 不再产出的文件（改名、删掉的 schema）留在那里，
	// 编辑器还会一直读它，而它再也没有人更新。
	entries, err := os.ReadDir(filepath.Join("..", "..", "schemas"))
	require.NoError(t, err)
	for _, entry := range entries {
		_, generated := files[entry.Name()]
		assert.True(t, generated, "schemas/%s 不是任何一份生成物：schemas/ 只放 make generate-schemas 写出来的文件，删掉它", entry.Name())
	}
}

// 文件名是使用者编辑器里 $schema 注释指向的公开地址（.../schemas/component.schema.json），
// 改名等于让所有已经配好的编辑器悄悄失效，所以这里写死字面值而不是引用常量。
func TestFilesNamesBothSchemas(t *testing.T) {
	files, err := Files()
	require.NoError(t, err)
	assert.Equal(t, []string{"brickkit.schema.json", "component.schema.json"}, sortedKeys(files))
}

// ---- 块二：必填集合（钉规则 + 校验器核对）----

type requiredCase struct {
	doc        string
	schemaPath string
	required   []string
	dataPath   []any // 这个 schema 节点在基准 YAML 里的位置
}

// requiredGolden 是手写的"schema 路径 → 必填字段"黄金表，每一项都逐条对照
// manifest.Validate / config.Validate 核实过，并且下面会从合法基准里把每个字段真删一遍，
// 看真实的解析是不是真的失败。
//
// 生成结果里出现表里没有的必填集合也会失败：新增类型的作者得回来这里核对它的必填规则。
var requiredGolden = []requiredCase{
	{"component", schemaRoot, []string{"apiVersion", "deployment", "healthCheck", "kind", "metadata"}, nil},
	{"component", "metadata", []string{"description", "id", "name", "version"}, []any{"metadata"}},
	{"component", "artifacts[]", []string{"files", "type"}, []any{"artifacts", 0}},
	{"component", "dependencies/resources[]", []string{"engine", "kind"}, []any{"dependencies", "resources", 0}},
	{"component", "dependencies/components[]#oneOf[1]", []string{"id"}, []any{"dependencies", "components", 1}}, // 映射写法
	{"component", "configSchema/properties{}", []string{"type"}, []any{"configSchema", "properties", "pageSize"}},
	{"component", "deployment", []string{"image", "port", "type"}, []any{"deployment"}},
	{"component", "deployment/extraPorts[]", []string{"name", "port"}, []any{"deployment", "extraPorts", 0}},
	{"component", "healthCheck", []string{"type"}, []any{"healthCheck"}},
	{"component", "migration", []string{"command"}, []any{"migration"}},

	{"project", schemaRoot, []string{"deploy", "project"}, nil},
	{"project", "deploy", []string{"target"}, []any{"deploy"}},
	{"project", "deploy/networkPolicy/ingressController", []string{"namespace"}, []any{"deploy", "networkPolicy", "ingressController"}},
	{"project", "deploy/networkPolicy/allowFrom[]", []string{"name", "namespace"}, []any{"deploy", "networkPolicy", "allowFrom", 0}},
	{"project", "deploy/networkPolicy/egress/allowTo[]", []string{"name"}, []any{"deploy", "networkPolicy", "egress", "allowTo", 0}},
	{"project", "sources[]", []string{"id", "type"}, []any{"sources", 0}},
	{"project", "components[]", []string{"id", "version"}, []any{"components", 0}},
	{"project", "resources[]", []string{"engine", "host", "id", "kind", "port"}, []any{"resources", 0}},
	{"project", "resources[]/bindings[]", []string{"componentId"}, []any{"resources", 0, "bindings", 0}},
}

// 生成结果里的每个必填集合都在黄金表里，黄金表里的每一行也都对得上生成结果。
func TestRequiredFieldsMatchGoldenTable(t *testing.T) {
	for _, doc := range sortedKeys(documents) {
		t.Run(doc, func(t *testing.T) {
			got := map[string][]string{}
			walkSchema(loadSchema(t, doc), schemaRoot, func(path string, node map[string]any) {
				if required := requiredOf(node); len(required) > 0 {
					got[path] = required
				}
			})

			want := map[string][]string{}
			for _, c := range requiredGolden {
				if c.doc == doc {
					want[c.schemaPath] = c.required
				}
			}
			assert.Equal(t, want, got,
				"生成结果里的必填集合与手写的黄金表对不上：改了结构体的 omitempty / 新增了类型，"+
					"就要回来核对它们的必填规则是不是真的与校验器一致，再改这张表")
		})
	}
}

// 黄金表里的每个必填字段，从合法基准里删掉、或者写成显式的 null，真实的解析都必须失败——
// schema 说必填（并且不许 null）而校验器并不要求，就是 schema 比校验器更严。
// 写成 null 这一半是为了 makeNullable：必填字段不能被它误伤，`port:` 留空会解码成 0，校验器报缺失。
func TestRequiredFieldsAreReallyRequiredByTheValidators(t *testing.T) {
	modes := []struct {
		name   string
		remove bool
	}{{"删掉", true}, {"写成 null", false}}

	// 每一行、每一种改法，校验器报的字段与被改字段的关系；ancestorOnly 的过期检查要用
	relations := map[string]fieldRelation{}

	for _, c := range requiredGolden {
		d := documents[c.doc]
		for _, field := range c.required {
			path := append(append([]any(nil), c.dataPath...), field)
			changed := fieldPath(path)
			for _, mode := range modes {
				t.Run(c.doc+"/"+changed+"/"+mode.name, func(t *testing.T) {
					err := d.parse(mutate(t, d.baseline, path, nil, mode.remove))
					require.Error(t, err, "%s %s之后校验器居然通过了：它不是必填，schema 里不该列它",
						changed, mode.name)

					relation := relationOf(problemFields(err), changed)
					relations[changed+"/"+mode.name] = relation
					switch relation {
					case unrelated:
						assert.Fail(t, "校验器确实失败了，但报的字段与被改的字段不相干——失败可能是别的原因，这一行没有证明什么",
							"报的字段：%v；被改的：%s", problemFields(err), changed)
					case reportedOnAncestor:
						assert.True(t, ancestorOnly[changed],
							"校验器只报在了 %s 的祖先上（%v）：要么这一行真的只能这样，登记进 ancestorOnly，"+
								"要么是别的字段引起的失败", changed, problemFields(err))
					}
				})
			}
		}
	}

	// ancestorOnly 里的每一项也得真的"只靠祖先字段相关"：校验器哪天改成直接报在那个字段上，
	// 或者黄金表里已经没有这一行了，名单就过期了——与 conditionallyRequired 的做法对齐，
	// 不然名单只会越攒越多，登记过的例外永远没人回头核对。
	for _, changed := range sortedKeys(ancestorOnly) {
		for _, mode := range modes {
			relation, seen := relations[changed+"/"+mode.name]
			require.True(t, seen, "ancestorOnly 里的 %s（%s）不在黄金表里了：字段改名或删除之后，这里要跟着删",
				changed, mode.name)
			assert.Equal(t, reportedOnAncestor, relation,
				"ancestorOnly 里的 %s（%s）：校验器现在直接报在这个字段上（或它的后代上），不再只靠祖先——"+
					"这一项过期了，从 ancestorOnly 里删掉", changed, mode.name)
		}
	}
}

// bool 字段虽然没写 omitempty 也不是必填（规则里唯一的类型例外）：schema 里声明了它，
// 但不列进 required，并且真实的校验器确实不要求它。
func TestBoolFieldsAreDeclaredButNotRequired(t *testing.T) {
	cases := []struct {
		doc        string
		schemaPath string
		dataPath   []any          // 这个对象在基准 YAML 里的位置
		field      string         // 它的 bool 字段
		setup      map[string]any // 基准里没有这个对象时，先按这个内容造一个
	}{
		{"project", "deploy/networkPolicy", []any{"deploy", "networkPolicy"}, "enabled", nil},
		{"project", "deploy/networkPolicy/egress", []any{"deploy", "networkPolicy", "egress"}, "enabled", nil},
		{"project", "deploy/serviceAccount", []any{"deploy", "serviceAccount"}, "enabled", map[string]any{"enabled": true}},
		{"component", "dependencies/components[]#oneOf[1]", []any{"dependencies", "components", 1}, "optional", nil},
	}
	for _, c := range cases {
		t.Run(c.doc+"/"+c.schemaPath, func(t *testing.T) {
			d := documents[c.doc]
			node, found := indexSchema(t, c.doc)[c.schemaPath]
			require.True(t, found, "schema 里没有 %s", c.schemaPath)

			properties, _ := node["properties"].(map[string]any)
			property, _ := properties[c.field].(map[string]any)
			require.NotNil(t, property, "%s 里没有声明 %s", c.schemaPath, c.field)
			assert.Equal(t, []any{"boolean", "null"}, property["type"], "bool 字段不必填，所以可空")
			assert.NotContains(t, requiredOf(node), c.field, "bool 字段不是必填")

			base := d.baseline
			if c.setup != nil {
				base = string(mutate(t, base, c.dataPath, c.setup, false))
			}
			require.NoError(t, d.parse([]byte(base)))
			removed := mutate(t, base, append(append([]any(nil), c.dataPath...), c.field), nil, true)
			assert.NoError(t, d.parse(removed), "%s 删掉 %s 之后，校验器不该拒绝", fieldPath(c.dataPath), c.field)
		})
	}
}

// ItemDef 没写 omitempty，可是校验器从不检查 items.type（AGENTS §9.12：items 只是说明书），
// 所以它靠 jsonschema:"optional" 挪出了必填。
func TestConfigItemsTypeIsOptional(t *testing.T) {
	node, ok := indexSchema(t, "component")["configSchema/properties{}/items"]
	require.True(t, ok)
	assert.Contains(t, node["properties"], "type", "items.type 仍然声明着")
	assert.Empty(t, requiredOf(node), "……但不是必填")

	// 真实的校验器：基准里 tags.items 就是一个没有 type 的空映射，照样通过
	var root any
	require.NoError(t, yaml.Unmarshal([]byte(baselineComponent), &root))
	items, found := valueAt(root, []any{"configSchema", "properties", "tags", "items"})
	require.True(t, found)
	assert.Empty(t, items, "基准里的 tags.items 应该是空映射，这一行才证明了 items 不需要 type")
	assert.NoError(t, documents["component"].parse([]byte(baselineComponent)))
}

// ---- 块三：约束测试 ----

type constraintCase struct {
	name string
	doc  string // "component" | "project"
	// schemaPath 指向 schema 里该字段的节点（用来读出 tag 生成的 enum / pattern / 范围）。
	schemaPath string
	// dataPath 是该字段在基准 YAML 里的位置；errField 是校验器报错时用的字段名。
	dataPath []any
	errField string
	// valid / invalid：拿去改基准里那个字段的值。
	//
	// valid 是校验器接受的取值（enum 行里就是 enum 的全集，且必须与 schema 的 enum 一模一样：
	// 少一个是 schema 比校验器更严，多一个是 schema 比校验器更松）。取值尽量取自校验器自己导出的
	// 常量 / 列表，这样改了常量而没改 tag，这里会红。
	valid, invalid []any
}

func constraintCases() []constraintCase {
	ports := []any{0, -1, 65536}
	kinds := anySlice(manifest.ResourceKinds)
	badKinds := []any{"redis", "Database", ""}

	return []constraintCase{
		{
			name: "deploy.target", doc: "project", schemaPath: "deploy/target",
			dataPath: []any{"deploy", "target"}, errField: "deploy.target",
			valid:   []any{config.TargetDocker, config.TargetK8s},
			invalid: []any{"swarm", "Docker", ""},
		},
		{
			name: "sources[0].type", doc: "project", schemaPath: "sources[]/type",
			dataPath: []any{"sources", 0, "type"}, errField: "sources[0].type",
			valid:   []any{config.SourceTypeMarket, config.SourceTypeGit, config.SourceTypeLocal},
			invalid: []any{"svn", "LOCAL", ""},
		},
		{
			name: "apiVersion", doc: "component", schemaPath: "apiVersion",
			dataPath: []any{"apiVersion"}, errField: "apiVersion",
			valid:   []any{manifest.APIVersion},
			invalid: []any{"brickkit/v2", "v1", ""},
		},
		{
			name: "kind", doc: "component", schemaPath: "kind",
			dataPath: []any{"kind"}, errField: "kind",
			valid:   []any{manifest.Kind},
			invalid: []any{"component", "Service", ""},
		},
		{
			name: "metadata.version", doc: "component", schemaPath: "metadata/version",
			dataPath: []any{"metadata", "version"}, errField: "metadata.version",
			valid:   []any{"1.0.0", "10.20.30", "0.0.0"},
			invalid: []any{"1.0", "^1.0.0", "~1.0.0", "1.0.0-beta", "v1.0.0", "1.0.0.0", " 1.0.0", ""},
		},
		{
			// 与 metadata.version 同一条 pattern：brickkit.yaml 里 components[].version 是使用者最常敲版本号的地方。
			name: "components[0].version", doc: "project", schemaPath: "components[]/version",
			dataPath: []any{"components", 0, "version"}, errField: "components[0].version",
			valid:   []any{"1.0.0", "10.20.30", "0.0.0"},
			invalid: []any{"^1.0.0", "~1.0.0", "1.0", "latest", "1.0.0-beta", ""},
		},
		{
			name: "deployment.type", doc: "component", schemaPath: "deployment/type",
			dataPath: []any{"deployment", "type"}, errField: "deployment.type",
			valid:   []any{manifest.DeploymentTypeContainer},
			invalid: []any{"docker", "static", ""},
		},
		{
			name: "deployment.port", doc: "component", schemaPath: "deployment/port",
			dataPath: []any{"deployment", "port"}, errField: "deployment.port",
			valid:   []any{1, 8080, 65535},
			invalid: ports,
		},
		{
			// 8080 不在这里：它是基准里的主端口，extraPorts 不许与它相同（报在同一个字段上），
			// 校验器拒绝它的原因与范围无关，放进 valid 会让这一行证明不了任何事。
			name: "deployment.extraPorts[0].port", doc: "component", schemaPath: "deployment/extraPorts[]/port",
			dataPath: []any{"deployment", "extraPorts", 0, "port"}, errField: "deployment.extraPorts[0].port",
			valid:   []any{1, 9091, 65535},
			invalid: ports,
		},
		{
			name: "healthCheck.type", doc: "component", schemaPath: "healthCheck/type",
			dataPath: []any{"healthCheck", "type"}, errField: "healthCheck.type",
			valid:   []any{manifest.HealthCheckHTTP, manifest.HealthCheckTCP, manifest.HealthCheckNone},
			invalid: []any{"ftp", "HTTP", ""},
		},
		{
			name: "dependencies.resources[0].kind", doc: "component", schemaPath: "dependencies/resources[]/kind",
			dataPath: []any{"dependencies", "resources", 0, "kind"}, errField: "dependencies.resources[0].kind",
			valid: kinds, invalid: badKinds,
		},
		{
			// 改 kind 之后 bindings[0].database 会因为"这种 kind 没有 database 这一格"而报错——
			// 那是别的字段的问题，只看 errField。
			name: "resources[0].kind", doc: "project", schemaPath: "resources[]/kind",
			dataPath: []any{"resources", 0, "kind"}, errField: "resources[0].kind",
			valid: kinds, invalid: badKinds,
		},
		{
			name: "configSchema.properties.pageSize.type", doc: "component", schemaPath: "configSchema/properties{}/type",
			dataPath: []any{"configSchema", "properties", "pageSize", "type"}, errField: "configSchema.properties.pageSize.type",
			valid:   []any{"string", "integer", "number", "boolean", "array", "object"},
			invalid: []any{"int", "str", "float", ""},
		},
		{
			// 覆盖表里字符串写法的 pattern。ID 部分 schema 故意比校验器松（只要求非空、不含 @ 与空格，
			// 大小写、连字符等 componentIDRe 的细则不在 schema 里），所以这里不放 ID 写错的取值。
			// 校验器把"没写版本""版本不精确"都报在依赖项本身（dependencies.components[0]）上，
			// 因此它们都能放进 invalid。
			name: "dependencies.components[0]", doc: "component", schemaPath: "dependencies/components[]#oneOf[0]",
			dataPath: []any{"dependencies", "components", 0}, errField: "dependencies.components[0]",
			valid: []any{"demo/other@1.0.0", "demo/other@10.2.3"},
			invalid: []any{
				"demo/other@^1.0.0", "demo/other@~1.0.0", "demo/other@latest", "demo/other@>=1.0.0",
				"demo/other@1.0", "demo/other@1.0.0-rc1", "demo/other@", "demo/other", "",
			},
		},
		{
			// 映射写法：同一个 pattern 挂在 id 上，校验器把问题报在依赖项本身（不带 .id）。
			name: "dependencies.components[1].id", doc: "component", schemaPath: "dependencies/components[]#oneOf[1]/id",
			dataPath: []any{"dependencies", "components", 1, "id"}, errField: "dependencies.components[1]",
			valid:   []any{"demo/weak@1.0.0", "demo/weak@10.2.3"},
			invalid: []any{"demo/weak@^1.0.0", "demo/weak@latest", "demo/weak@1.0", "demo/weak@", "demo/weak", ""},
		},
	}
}

func toFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	require.Failf(t, "不是数字", "%v（%T）", v, v)
	return 0
}

func TestConstraintsAgreeWithValidators(t *testing.T) {
	for _, c := range constraintCases() {
		t.Run(c.doc+"/"+c.name, func(t *testing.T) {
			d := documents[c.doc]
			node, found := indexSchema(t, c.doc)[c.schemaPath]
			require.True(t, found, "schema 里没有 %s", c.schemaPath)

			enum, hasEnum := node["enum"].([]any)
			pattern, hasPattern := node["pattern"].(string)
			minimum, hasMinimum := node["minimum"].(float64)
			maximum, hasMaximum := node["maximum"].(float64)
			require.True(t, hasEnum || hasPattern || hasMinimum || hasMaximum,
				"%s 处的 schema 没有任何约束：tag 被删了，或者路径写错了", c.schemaPath)

			// 一、schema 自己：合法的都放行，非法的都拦下，多一个少一个都算错。
			if hasEnum {
				assert.ElementsMatch(t, c.valid, enum,
					"schema 的 enum 与校验器接受的取值不一致：少了是比校验器更严，多了是比校验器更松")
				for _, bad := range c.invalid {
					assert.NotContains(t, enum, bad, "schema 的 enum 放行了校验器拒绝的 %q", bad)
				}
			}
			if hasPattern {
				re, err := regexp.Compile(pattern)
				require.NoError(t, err, "pattern 不是合法的正则")
				for _, good := range c.valid {
					assert.Regexp(t, re, good, "pattern 拦下了校验器接受的 %v：schema 比校验器更严", good)
				}
				for _, bad := range c.invalid {
					assert.NotRegexp(t, re, bad, "pattern 放行了校验器拒绝的 %v：schema 比校验器更松", bad)
				}
			}
			if hasMinimum || hasMaximum {
				inRange := func(v any) bool {
					n := toFloat(t, v)
					return (!hasMinimum || n >= minimum) && (!hasMaximum || n <= maximum)
				}
				for _, good := range c.valid {
					assert.True(t, inRange(good), "范围 [%v, %v] 拦下了校验器接受的 %v：schema 比校验器更严", minimum, maximum, good)
				}
				for _, bad := range c.invalid {
					assert.False(t, inRange(bad), "范围 [%v, %v] 放行了校验器拒绝的 %v：schema 比校验器更松", minimum, maximum, bad)
				}
			}

			// 基准里现在的取值本身也要满足这条约束，否则上面的"合法"没有落脚点
			var root any
			require.NoError(t, yaml.Unmarshal([]byte(d.baseline), &root))
			current, found := valueAt(root, c.dataPath)
			require.True(t, found, "基准里没有 %s", c.errField)
			switch {
			case hasEnum:
				assert.Contains(t, enum, current, "基准里的 %s 不在 enum 里", c.errField)
			case hasPattern:
				assert.Regexp(t, pattern, current, "基准里的 %s 不匹配 pattern", c.errField)
			default:
				n := toFloat(t, current)
				assert.True(t, (!hasMinimum || n >= minimum) && (!hasMaximum || n <= maximum),
					"基准里的 %s 不在范围里", c.errField)
			}

			// 二、合法的取值不被校验器拒绝：别的字段因为这次改动而报错（如 sources[0].type 改成 market
			// 之后缺 url）不算，只看以 errField 为键的明细。
			for _, good := range c.valid {
				err := d.parse(mutate(t, d.baseline, c.dataPath, good, false))
				assert.NotContains(t, problemFields(err), c.errField,
					"校验器拒绝了 %v，而 schema 放行它——这不是 schema 更松，是取值表写错了", good)
			}

			// 三、非法的取值被校验器拒绝。
			for _, bad := range c.invalid {
				err := d.parse(mutate(t, d.baseline, c.dataPath, bad, false))
				assert.Contains(t, problemFields(err), c.errField,
					"校验器放行了 %v：tag 比校验器更严，或者这个取值不该放进 invalid", bad)
			}
		})
	}
}

// 每个带约束（enum / pattern / 范围）的 schema 节点都要在约束表里有一行：新加一个 jsonschema tag
// 却忘了拿校验器核对，这里会红。反过来，表里的行也必须指向真有约束的节点。
func TestEveryConstrainedSchemaNodeHasAConstraintCase(t *testing.T) {
	want := map[string]bool{}
	for _, c := range constraintCases() {
		want[c.doc+":"+c.schemaPath] = true
	}

	got := map[string]bool{}
	for _, doc := range sortedKeys(documents) {
		walkSchema(loadSchema(t, doc), schemaRoot, func(path string, node map[string]any) {
			for _, keyword := range []string{"enum", "pattern", "minimum", "maximum"} {
				if _, ok := node[keyword]; ok {
					got[doc+":"+path] = true
				}
			}
		})
	}
	assert.Equal(t, sortedKeys(want), sortedKeys(got),
		"schema 里带约束的节点与约束表的行对不上：新增的 jsonschema tag 要在 constraintCases 里加一行去核对校验器")
}

// ---- 结构性的自检 ----

// 有自定义解码的类型，反射看不出它接受哪些写法，得在覆盖表里手写。生成器自己遇到就会报错，
// 这里再查一遍是为了让报错指向"覆盖表"，而不是一个生成失败的堆栈；反过来，覆盖表里的类型
// 如果已经没有自定义解码了（有人删了 ComponentDep.UnmarshalYAML），那份手写 schema 就成了过期的。
func TestEveryUnmarshalerTypeIsCoveredByAnOverride(t *testing.T) {
	yamlUnmarshaler := reflect.TypeOf((*yaml.Unmarshaler)(nil)).Elem()
	textUnmarshaler := reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

	found := map[reflect.Type]bool{}
	seen := map[reflect.Type]bool{}
	var visit func(reflect.Type)
	visit = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if seen[typ] {
			return
		}
		seen[typ] = true

		if pt := reflect.PointerTo(typ); pt.Implements(yamlUnmarshaler) || pt.Implements(textUnmarshaler) {
			found[typ] = true
			return // 自定义解码的类型不往下展开：它的内部结构不是 yaml.v3 看到的形状
		}
		switch typ.Kind() {
		case reflect.Struct:
			for _, field := range yamlcheck.KnownFields(typ) {
				visit(field.Type)
			}
		case reflect.Slice, reflect.Array, reflect.Map:
			visit(typ.Elem())
		}
	}
	visit(reflect.TypeOf(manifest.Manifest{}))
	visit(reflect.TypeOf(config.Config{}))

	require.NotEmpty(t, found, "manifest.ComponentDep 就是这样的类型；一个都找不到说明这个遍历坏了")

	table := overrides()
	for typ := range found {
		_, covered := table[typ]
		assert.True(t, covered, "%s 有自定义的解码逻辑，反射看不出它接受哪些写法：得在 overrides() 里手写它的 schema", typ)
	}
	for typ := range table {
		assert.True(t, found[typ], "覆盖表里的 %s 已经没有自定义解码逻辑了：这份手写的 schema 是过期的，删掉它", typ)
	}
}

// mapValueStructs 是"经由 map 的值才够得着的 struct 类型"，目前恰好两个，都在 configSchema.properties.<键> 之下：
// ConfigProperty，以及它里面的 ItemDef。
//
// yamlcheck.Walk 不往 map 的值里下钻（map 的键是使用者自己定的，那里写什么都合法），所以 manifest.Parse 对这两个
// 类型里的多余键一声不吭——`format: uri`、拼错的 `defualt` 被静默丢掉；CLI 改在 PropertyKeyWarnings 里警告
// （lint / publish / add --local）。schema 在这里仍然封闭，这是**有意的**"比 Parse 更严"：那些键不会生效，
// 编辑器标红与 CLI 的警告说的是同一件事（TestUnknownKeysInConfigSchemaPropertiesAreWarnedNotRejected 用真实的
// 解析器把这一点钉住了）。
//
// 其余 struct 节点的 additionalProperties: false 是在镜像 CLI 的"未知字段"拒绝；这两个不是，所以单列。
// 新出现一个经由 map 的值才够得着的 struct，checkNode 会失败，逼着加它的人来这里决定：也这样封闭，还是放开。
var mapValueStructs = map[reflect.Type]bool{
	reflect.TypeOf(manifest.ConfigProperty{}): true,
	reflect.TypeOf(manifest.ItemDef{}):        true,
}

// schema 里每个 struct 节点的 properties 键集合必须等于 yamlcheck.KnownFields 给出的键集合——
// CLI 拒绝未知字段用的是同一份。覆盖表里的类型除外（那是手写的）。
// 顺带核对两种节点的封闭性：struct 是 additionalProperties: false，map 不是（键是使用者自己定的）。
// 经由 map 的值才够得着的 struct 是个例外，见 mapValueStructs。
func TestPropertyNamesMatchWhatTheCLIAccepts(t *testing.T) {
	roots := map[string]reflect.Type{
		"component": reflect.TypeOf(manifest.Manifest{}),
		"project":   reflect.TypeOf(config.Config{}),
	}
	reached := map[reflect.Type]bool{}
	for _, doc := range sortedKeys(roots) {
		t.Run(doc, func(t *testing.T) {
			checkNode(t, roots[doc], loadSchema(t, doc), schemaRoot, false, reached)
		})
	}
	assert.Equal(t, mapValueStructs, reached,
		"mapValueStructs 里登记的类型与真正经由 map 的值才够得着的类型不一致：登记过期了，或者多了一个要交代的")
}

// 覆盖表里的 ComponentDep 是手写的，checkNode 不去比它；但映射写法认哪些键，CLI 用的也是
// yamlcheck.KnownFields（id 与 optional，Version 与 Ref 标了 "-"）。两边对得上，才不会出现
// 编辑器放行而 CLI 报"未知字段"（或者反过来）。
func TestComponentDepOverrideKnowsTheSameKeysAsTheCLI(t *testing.T) {
	mapping, ok := indexSchema(t, "component")["dependencies/components[]#oneOf[1]"]
	require.True(t, ok)
	properties, _ := mapping["properties"].(map[string]any)
	assert.ElementsMatch(t,
		sortedKeys(yamlcheck.KnownFields(reflect.TypeOf(manifest.ComponentDep{}))), sortedKeys(properties))
	assert.Equal(t, false, mapping["additionalProperties"], "映射写法要拒绝未知键（CLI 就是这样）")
}

// checkNode 沿着 Go 类型与 schema 节点同时下行。belowMapValue 表示当前节点在某个 map 的值之下
// （yamlcheck.Walk 走不到那里）；reached 收集在那里遇到的 struct 类型。
func checkNode(t *testing.T, typ reflect.Type, node map[string]any, path string, belowMapValue bool, reached map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if _, overridden := overrides()[typ]; overridden {
		return
	}
	where := fmt.Sprintf("%q（%s）", path, typ)

	switch typ.Kind() {
	case reflect.Struct:
		known := yamlcheck.KnownFields(typ)
		properties, _ := node["properties"].(map[string]any)
		assert.ElementsMatch(t, sortedKeys(known), sortedKeys(properties),
			"%s：schema 认识的键与 CLI 认识的键不一致", where)
		// 两种情形下 schema 都是封闭的，但理由不同：一般的 struct 节点是在镜像 CLI 拒绝未知字段；
		// 经由 map 的值才够得着的 struct，CLI 并不拒绝（yamlcheck.Walk 不下钻），封闭是 schema 有意为之
		assert.Equal(t, false, node["additionalProperties"], "%s：struct 节点要拒绝未知字段", where)
		if belowMapValue {
			reached[typ] = true
			assert.True(t, mapValueStructs[typ],
				"%s：这个 struct 经由 map 的值才够得着，yamlcheck.Walk 不下钻到这里，CLI 不拒绝里面的多余键。"+
					"schema 在这里封闭是一个决定：要在 mapValueStructs 里登记并写清理由", where)
		}
		for name, field := range known {
			if child, ok := properties[name].(map[string]any); ok {
				checkNode(t, field.Type, child, joinSchemaPath(path, name), belowMapValue, reached)
			}
		}
	case reflect.Slice, reflect.Array:
		items, ok := node["items"].(map[string]any)
		require.True(t, ok, "%s：数组节点没有 items", where)
		checkNode(t, typ.Elem(), items, path+"[]", belowMapValue, reached)
	case reflect.Map:
		values, ok := node["additionalProperties"].(map[string]any)
		require.True(t, ok, "%s：map 节点的 additionalProperties 应该是值的 schema，而不是 false", where)
		checkNode(t, typ.Elem(), values, path+"{}", true, reached)
	}
}

// ---- 修复轮 2：可空性、configSchema 的例外、已知"更严"的清单 ----

// acceptsNull 按 JSON Schema 的语义判断一个节点是否接受 null，只认生成器会写出来的那几个关键字
// （type、enum、oneOf）：type 缺省（{}）接受任何值；有 type 就得列着 "null"；有 enum 就得含 null；
// oneOf 有一支接受就行。
func acceptsNull(node map[string]any) bool {
	if branches, ok := node["oneOf"].([]any); ok {
		for _, branch := range branches {
			if acceptsNull(branch.(map[string]any)) {
				return true
			}
		}
		return false
	}
	switch kind := node["type"].(type) {
	case string:
		if kind != "null" {
			return false
		}
	case []any:
		if !slices.Contains(kind, any("null")) {
			return false
		}
	}
	if values, ok := node["enum"].([]any); ok && !slices.Contains(values, nil) {
		return false
	}
	return true
}

// 整份 schema 通用：一个属性接受 null，当且仅当它不在父节点的 required 里。
// 手写的覆盖表（ComponentDep 的映射写法）也在这份遍历里，所以它的 optional 忘了写 null 会红。
//
// 没有允许名单：Go 的 any 是 {}，本来就接受 null；今天没有必填的 any。哪天有了，这里会红，
// 由加它的人决定该怎么办，而不是悄悄放过。
func TestNullabilityMatchesRequiredness(t *testing.T) {
	for _, doc := range sortedKeys(documents) {
		t.Run(doc, func(t *testing.T) {
			checked := 0
			walkSchema(loadSchema(t, doc), schemaRoot, func(path string, node map[string]any) {
				properties, _ := node["properties"].(map[string]any)
				required := requiredOf(node)
				for name, child := range properties {
					checked++
					assert.Equal(t, !slices.Contains(required, name), acceptsNull(child.(map[string]any)),
						"%s：必填的属性不接受 null，不必填的接受", joinSchemaPath(path, name))
				}
			})
			assert.Positive(t, checked, "一个属性都没检查到，遍历坏了")
		})
	}
}

func TestAcceptsNullFollowsJSONSchemaSemantics(t *testing.T) {
	for name, tc := range map[string]struct {
		node map[string]any
		want bool
	}{
		"没有 type（any）":          {map[string]any{}, true},
		"type: string":          {map[string]any{"type": "string"}, false},
		"type: null":            {map[string]any{"type": "null"}, true},
		"type: [string, null]":  {map[string]any{"type": []any{"string", "null"}}, true},
		"type: [string, other]": {map[string]any{"type": []any{"string", "integer"}}, false},
		"type 允许 null，enum 不列它": {map[string]any{"type": []any{"string", "null"}, "enum": []any{"a"}}, false},
		"type 与 enum 都允许 null":  {map[string]any{"type": []any{"string", "null"}, "enum": []any{"a", nil}}, true},
		"oneOf 没有 null 那一支": {map[string]any{"oneOf": []any{
			map[string]any{"type": "string"}, map[string]any{"type": "object"}}}, false},
		"oneOf 有 null 那一支": {map[string]any{"oneOf": []any{
			map[string]any{"type": "string"}, map[string]any{"type": "null"}}}, true},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, acceptsNull(tc.node))
		})
	}
}

// optionalPropertyPaths 沿着 schema 与基准 YAML 同时下行，把"基准里已经存在其父对象"的每个不必填属性的
// 数据路径交给 visit。父对象不在基准里的属性够不着，这里不管；oneOf 的节点（覆盖表手写的 ComponentDep）
// 不进去，它们单独核对。
func optionalPropertyPaths(node map[string]any, data any, path []any, visit func(path []any)) {
	child := func(step any) []any { return append(append([]any(nil), path...), step) }

	switch value := data.(type) {
	case map[string]any:
		if properties, ok := node["properties"].(map[string]any); ok {
			required := requiredOf(node)
			for _, name := range sortedKeys(properties) {
				if !slices.Contains(required, name) {
					visit(child(name))
				}
				if present, ok := value[name]; ok {
					optionalPropertyPaths(properties[name].(map[string]any), present, child(name), visit)
				}
			}
		}
		if values, ok := node["additionalProperties"].(map[string]any); ok {
			for _, key := range sortedKeys(value) {
				optionalPropertyPaths(values, value[key], child(key), visit)
			}
		}
	case []any:
		if items, ok := node["items"].(map[string]any); ok {
			for i, item := range value {
				optionalPropertyPaths(items, item, child(i), visit)
			}
		}
	}
}

// conditionallyRequired：yaml tag 写了 omitempty，可是校验器在基准的取值下要求它——写成 null 等于没写，
// 校验器拒绝。这是"校验器比 schema 更严"，是允许的方向；登记在这里，并且要求它们真的被拒绝，名单才不会过期。
var conditionallyRequired = map[string]string{
	"component:healthCheck.path": "healthCheck.type 是 http 时必填（validateHealthCheck）",
	"project:sources[0].path":    "sources[].type 是 local 时必填（validateSources）",
	"project:deploy.networkPolicy.egress.allowTo[0].namespace": "allowTo 的 namespace 与 cidr 必须写一个，" +
		"基准里写的是 namespace（validateEgress）",
}

// 不必填的属性写成显式的 null，真实的解析器放行：这是 schema 允许 null 的依据。
// 遍历基准里已经存在父对象的全部不必填属性（不是抽几个来看），使用者最常踩到的几个另外点名，
// 确认遍历真的走到了它们。
func TestExplicitNullIsAcceptedForOptionalPropertiesByTheRealParsers(t *testing.T) {
	representatives := map[string][]string{
		"component": {
			"tags", "dependencies", "dependencies.components", "dependencies.resources", "configSchema",
			"configSchema.properties", "configSchema.required", "migration", "metadata.vendor",
			"deployment.labels", "deployment.resources", "healthCheck.startPeriodSeconds",
			"configSchema.properties.pageSize.default", "configSchema.properties.tags.items.type",
		},
		"project": {
			"sources", "components", "resources", "installer", "deploy.networkPolicy",
			"deploy.networkPolicy.egress", "deploy.networkPolicy.enabled", "deploy.serviceAccount",
			"components[0].config", "components[0].local", "resources[0].bindings", "resources[0].password",
		},
	}

	for _, doc := range sortedKeys(documents) {
		d := documents[doc]
		var root any
		require.NoError(t, yaml.Unmarshal([]byte(d.baseline), &root))

		var visited []string
		optionalPropertyPaths(loadSchema(t, doc), root, nil, func(path []any) {
			visited = append(visited, fieldPath(path))
			name := fieldPath(path)
			t.Run(doc+"/"+name, func(t *testing.T) {
				err := d.parse(mutate(t, d.baseline, path, nil, false))
				if why, conditional := conditionallyRequired[doc+":"+name]; conditional {
					assert.Error(t, err, "%s：%s。它写成 null 应该被校验器拒绝；不拒绝了就把它从 conditionallyRequired 里删掉", name, why)
					return
				}
				assert.NoError(t, err, "%s 写成 null：schema 允许，校验器却拒绝——schema 比校验器更严", name)
			})
		})

		for _, want := range representatives[doc] {
			assert.Contains(t, visited, want, "遍历没有走到 %s:%s", doc, want)
		}
	}

	// 覆盖表手写的映射写法不在上面的遍历里
	t.Run("component/dependencies.components[1].optional", func(t *testing.T) {
		d := documents["component"]
		path := []any{"dependencies", "components", 1, "optional"}
		assert.NoError(t, d.parse(mutate(t, d.baseline, path, nil, false)))
	})

	// 名单里的每一项都得真的存在：字段改名、条件改了之后，这里要跟着删
	for key := range conditionallyRequired {
		doc, name, _ := strings.Cut(key, ":")
		found := false
		var root any
		require.NoError(t, yaml.Unmarshal([]byte(documents[doc].baseline), &root))
		optionalPropertyPaths(loadSchema(t, doc), root, nil, func(path []any) { found = found || fieldPath(path) == name })
		assert.True(t, found, "conditionallyRequired 里的 %s 已经不是基准里的不必填属性了", key)
	}
}

// ---- configSchema 属性声明里的多余键：CLI 只警告，schema 封闭（已知例外三）----

func TestUnknownKeysInConfigSchemaPropertiesAreWarnedNotRejected(t *testing.T) {
	cases := []struct {
		name string
		path []any  // 在基准里插入这个多余的键
		key  string // 警告里应该点名的字段
	}{
		{"属性声明里的 format（照着 JSON Schema 习惯写的）",
			[]any{"configSchema", "properties", "pageSize", "format"}, "configSchema.properties.pageSize.format"},
		{"属性声明里拼错的 defualt",
			[]any{"configSchema", "properties", "pageSize", "defualt"}, "configSchema.properties.pageSize.defualt"},
		{"items 里的多余键",
			[]any{"configSchema", "properties", "tags", "items", "minItems"}, "configSchema.properties.tags.items.minItems"},
	}
	index := indexSchema(t, "component")

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := mutate(t, baselineComponent, c.path, "x", false)

			// manifest.Parse 不拒绝：yamlcheck.Walk 不往 map 的值里下钻
			_, err := manifest.Parse(data, "component.yaml")
			require.NoError(t, err, "manifest.Parse 不该拒绝属性声明里的多余键")

			// PropertyKeyWarnings 警告，并且点名了这个键
			warnings := manifest.PropertyKeyWarnings(data, "component.yaml")
			require.Len(t, warnings, 1)
			assert.True(t, warnings[0].Warning, "是警告，不是错误")
			assert.Contains(t, problemFields(warnings[0]), c.key)
		})
	}

	// schema 在这里封闭：ConfigProperty 与 ItemDef 两个节点
	for _, path := range []string{"configSchema/properties{}", "configSchema/properties{}/items"} {
		assert.Equal(t, false, index[path]["additionalProperties"], path)
	}
}

// ---- 已知例外一、二：同样拿真实的解析器钉住 ----

func TestKnownStricterThanTheCLICasesAreReal(t *testing.T) {
	// 一、${VAR} 写进有封闭取值或 pattern 的字段：ParseConfig 先展开再校验，schema 检查字面文本
	t.Run("${VAR}", func(t *testing.T) {
		t.Setenv("BRICKKIT_SCHEMATEST_TARGET", "docker")
		t.Setenv("BRICKKIT_SCHEMATEST_VERSION", "1.0.0")
		t.Setenv("BRICKKIT_SCHEMATEST_SOURCE_TYPE", "local")
		t.Setenv("BRICKKIT_SCHEMATEST_KIND", "database")
		index := indexSchema(t, "project")
		d := documents["project"]

		for _, c := range []struct {
			path       []any
			schemaPath string
			reference  string
		}{
			{[]any{"deploy", "target"}, "deploy/target", "${BRICKKIT_SCHEMATEST_TARGET}"},
			{[]any{"components", 0, "version"}, "components[]/version", "${BRICKKIT_SCHEMATEST_VERSION}"},
			{[]any{"sources", 0, "type"}, "sources[]/type", "${BRICKKIT_SCHEMATEST_SOURCE_TYPE}"},
			{[]any{"resources", 0, "kind"}, "resources[]/kind", "${BRICKKIT_SCHEMATEST_KIND}"},
		} {
			require.NoError(t, d.parse(mutate(t, d.baseline, c.path, c.reference, false)),
				"ParseConfig 展开 %s 之后校验，放行", c.reference)

			node := index[c.schemaPath]
			require.NotNil(t, node, "schema 里没有 %s", c.schemaPath)
			enum, hasEnum := node["enum"].([]any)
			pattern, hasPattern := node["pattern"].(string)
			require.True(t, hasEnum || hasPattern, "%s 既没有 enum 也没有 pattern，这一行什么都证明不了", c.schemaPath)
			if hasEnum {
				assert.NotContains(t, enum, c.reference, "schema 的 enum 检查字面文本")
			}
			if hasPattern {
				assert.NotRegexp(t, pattern, c.reference, "schema 的 pattern 检查字面文本")
			}
		}
	})

	// 二、yaml.v3 的宽容解码：schema 不跟着放宽
	t.Run("不加引号的非字符串标量写进字符串字段", func(t *testing.T) {
		index := indexSchema(t, "project")
		d := documents["project"]
		for _, c := range []struct {
			path       []any
			schemaPath string
			value      any
		}{
			{[]any{"resources", 0, "password"}, "resources[]/password", 123456},
			{[]any{"project"}, "project", 2024},
		} {
			require.NoError(t, d.parse(mutate(t, d.baseline, c.path, c.value, false)),
				"yaml.v3 把 %v 照字面转成字符串", c.value)

			names := typeNames(index[c.schemaPath])
			assert.Contains(t, names, "string", c.schemaPath)
			for _, other := range []string{"integer", "number", "boolean"} {
				assert.NotContains(t, names, other, "%s 的 type 只认字符串：它是 schema 更严的地方", c.schemaPath)
			}
		}
	})

	t.Run("列表里的 null 元素", func(t *testing.T) {
		d := documents["component"]
		index := indexSchema(t, "component")

		// 在有的位置被悄悄丢掉：dependencies.components 的第一项、tags 的唯一一项
		require.NoError(t, d.parse(mutate(t, d.baseline, []any{"dependencies", "components", 0}, nil, false)))
		require.NoError(t, d.parse(mutate(t, d.baseline, []any{"tags"}, []any{nil}, false)))

		// schema 指出来：数组的元素不接受 null
		assert.False(t, acceptsNull(index["dependencies/components[]"]))
		assert.False(t, acceptsNull(index["tags[]"]))
	})

	// map 的键是使用者自己定的，值却必须是字符串：值写成 null（`key:` 后面什么都没写），
	// CLI 照样收下，schema 的 additionalProperties 是 type: string，标红。
	t.Run("map 里的 null 值", func(t *testing.T) {
		d := documents["project"]
		index := indexSchema(t, "project")

		for _, c := range []struct {
			field      string
			path       []any
			value      any
			schemaPath string // map 的值在 schema 里的位置
		}{
			{"deploy.ingressAnnotations", []any{"deploy", "ingressAnnotations"},
				map[string]any{"k": nil}, "deploy/ingressAnnotations{}"},
			{"installer.publicKeys", []any{"installer"},
				map[string]any{"publicKeys": map[string]any{"k": nil}}, "installer/publicKeys{}"},
			{"deploy.networkPolicy.allowFrom[0].podSelector", []any{"deploy", "networkPolicy", "allowFrom", 0, "podSelector"},
				map[string]any{"k": nil}, "deploy/networkPolicy/allowFrom[]/podSelector{}"},
		} {
			require.NoError(t, d.parse(mutate(t, d.baseline, c.path, c.value, false)),
				"%s 里有一个值是 null 的键：CLI 收下", c.field)

			node, found := index[c.schemaPath]
			require.True(t, found, "schema 里没有 %s", c.schemaPath)
			assert.False(t, acceptsNull(node), "%s 的值不接受 null——这是 schema 更严的地方", c.schemaPath)
			assert.Equal(t, []string{"string"}, typeNames(node), c.schemaPath)
		}
	})

	// yaml.v3 对 bool 目标有 YAML 1.1 的兼容：yes / on 读成 true。schema 只认 true / false。
	// mutate 会把字符串 "yes" 加上引号写出来（那就成了字符串，CLI 也拒绝），所以这里直接改文本。
	t.Run("bool 字段写 yes / on", func(t *testing.T) {
		projectEnabled := func(read func(*config.Config) bool) func([]byte) bool {
			return func(data []byte) bool {
				cfg, err := config.ParseConfig(data, "brickkit.yaml")
				require.NoError(t, err)
				return read(cfg)
			}
		}
		for _, c := range []struct {
			doc, field, schemaPath string
			old, new               string
			readTrue               func(data []byte) bool // 真实的解析器把它读成了什么
		}{
			{"project", "deploy.networkPolicy.enabled", "deploy/networkPolicy/enabled",
				"  networkPolicy:\n    enabled: true\n", "  networkPolicy:\n    enabled: yes\n",
				projectEnabled(func(cfg *config.Config) bool { return cfg.Deploy.NetworkPolicy.Enabled })},
			{"project", "deploy.networkPolicy.egress.enabled", "deploy/networkPolicy/egress/enabled",
				"    egress:\n      enabled: true\n", "    egress:\n      enabled: on\n",
				projectEnabled(func(cfg *config.Config) bool { return cfg.Deploy.NetworkPolicy.Egress.Enabled })},
			{"component", "dependencies.components[1].optional", "dependencies/components[]#oneOf[1]/optional",
				"      optional: true\n", "      optional: yes\n",
				func(data []byte) bool {
					m, err := manifest.Parse(data, "component.yaml")
					require.NoError(t, err)
					return m.Dependencies.Components[1].Optional
				}},
		} {
			d := documents[c.doc]
			require.Equal(t, 1, strings.Count(d.baseline, c.old), "基准里应该恰好有一处 %q", c.old)
			data := []byte(strings.Replace(d.baseline, c.old, c.new, 1))
			require.NoError(t, d.parse(data), "%s 写成 yes / on：yaml.v3 读成 true，CLI 收下", c.field)
			assert.True(t, c.readTrue(data), "%s 被读成了 true，不是被忽略", c.field)

			node, found := indexSchema(t, c.doc)[c.schemaPath]
			require.True(t, found, "schema 里没有 %s", c.schemaPath)
			names := typeNames(node)
			assert.Contains(t, names, "boolean", c.schemaPath)
			assert.NotContains(t, names, "string", "%s 只认 true / false：yes / on 是字符串，这是 schema 更严的地方", c.schemaPath)
		}
	})

	// 整数字段里写小数：yaml.v3 悄悄截断成整数，schema 的 integer 不认小数。
	t.Run("整数字段写小数", func(t *testing.T) {
		for _, c := range []struct {
			doc, field, schemaPath string
			old, new               string
		}{
			{"project", "resources[0].port", "resources[]/port", "    port: 5432\n", "    port: 5432.5\n"},
			{"component", "deployment.port", "deployment/port", "  port: 8080\n", "  port: 8080.5\n"},
		} {
			d := documents[c.doc]
			require.Equal(t, 1, strings.Count(d.baseline, c.old), "基准里应该恰好有一处 %q", c.old)
			data := []byte(strings.Replace(d.baseline, c.old, c.new, 1))
			require.NoError(t, d.parse(data), "%s 写成小数：yaml.v3 截断成整数，CLI 收下", c.field)

			node, found := indexSchema(t, c.doc)[c.schemaPath]
			require.True(t, found, "schema 里没有 %s", c.schemaPath)
			names := typeNames(node)
			assert.Contains(t, names, "integer", c.schemaPath)
			assert.NotContains(t, names, "number", "%s 是整数字段：5432.5 不是整数，这是 schema 更严的地方", c.schemaPath)
		}

		// 截断是真的：值落到了 5432，不是被拒绝
		cfg, err := config.ParseConfig([]byte(strings.Replace(baselineProject, "    port: 5432\n", "    port: 5432.5\n", 1)), "brickkit.yaml")
		require.NoError(t, err)
		assert.Equal(t, 5432, cfg.Resources[0].Port)
	})
}

// typeNames 读出一个节点的 type：单个类型名，或者联合形式里的每一个。
func typeNames(node map[string]any) []string {
	switch kind := node["type"].(type) {
	case string:
		return []string{kind}
	case []any:
		names := make([]string, len(kind))
		for i, name := range kind {
			names[i] = name.(string)
		}
		return names
	}
	return nil
}

// ---- 测试辅助自己的行为 ----

func TestMutateRefusesToRemoveAnArrayElement(t *testing.T) {
	assert.PanicsWithValue(t,
		"删除路径不能以数组下标结尾（下标 0）：要去掉一个元素就改写整个数组",
		func() { mutate(t, baselineComponent, []any{"artifacts", 0}, nil, true) })

	// 删映射的键（哪怕它在数组元素里）、把数组元素改写成别的值，都照常
	removed := string(mutate(t, baselineComponent, []any{"artifacts", 0, "type"}, nil, true))
	assert.NotContains(t, removed, "api-contract")
	replaced := string(mutate(t, baselineComponent, []any{"artifacts", 0}, "x", false))
	assert.Contains(t, replaced, "- x")
}

func TestRelationOfNamesHowFarTheReportedFieldIs(t *testing.T) {
	assert.Equal(t, reportedOnFieldOrBelow, relationOf([]string{"文件", "a.b"}, "a.b"), "同一个字段")
	assert.Equal(t, reportedOnFieldOrBelow, relationOf([]string{"metadata.id"}, "metadata"), "后代")
	assert.Equal(t, reportedOnFieldOrBelow, relationOf([]string{"list[0].id"}, "list[0]"), "数组元素的后代")
	assert.Equal(t, reportedOnAncestor, relationOf([]string{"dependencies.components[1]"}, "dependencies.components[1].id"), "祖先")
	assert.Equal(t, unrelated, relationOf([]string{"文件", "deployment.port"}, "deployment.image"), "同一层的别的字段")
	assert.Equal(t, unrelated, relationOf([]string{"deployment.portal"}, "deployment.port"), "前缀相同但不是同一个路径")
	assert.Equal(t, reportedOnFieldOrBelow, relationOf([]string{"a", "a.b"}, "a.b"), "祖先与本身同时报了，取更近的")
}
