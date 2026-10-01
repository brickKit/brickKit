package manifest

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"

	"errors"
)

// FileName 是 Manifest 的固定文件名。
const FileName = "component.yaml"

// ParseFile 读取并解析一个 component.yaml。
func ParseFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestFileMissing, FileName)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithHint(
				i18n.T(msgid.ManifestHintCheckDirHasFile),
				i18n.T(msgid.ManifestHintCheckPathFlag),
			).WithCause(err)
	case err != nil:
		return nil, clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestReadFailed, FileName)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.ProblemHintCheckPermissions)).
			WithCause(err)
	}
	return Parse(data, path)
}

// Parse 解析并校验 component.yaml。source 用于错误提示（文件路径或安装源描述）。
//
// 解析分三步：
//  1. YAML 语法解析（语法错误带行号）
//  2. 结构形状检查（该是数组的字段必须是数组，给出精确字段名）
//     + 未知字段检查（拼错的键，见 yamlcheck.Walk）
//  3. 字段级语义校验（一次报出全部问题）
func Parse(data []byte, source string) (*Manifest, error) {
	if source == "" {
		source = FileName
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, syntaxError(source, err)
	}
	if root.Kind == 0 || len(root.Content) == 0 {
		return nil, clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestEmpty, FileName)).
			WithDetail(i18n.T(msgid.LabelFile), source).
			WithHint(i18n.T(msgid.ManifestHintFieldReference))
	}

	doc := root.Content[0]
	shape := newProblems(source)
	checkShapes(doc, shape)
	// 与形状问题一起报：两者都是"这份 Manifest 根本读不对"，
	// 分两轮报会让人改完一处又撞下一处
	walkUnknownFields(doc, shape)
	checkTypes(doc, shape)
	if shape.Len() > 0 {
		return nil, shape.Err()
	}

	var m Manifest
	if err := doc.Decode(&m); err != nil {
		return nil, decodeError(source, err)
	}
	m.Source = source
	keepNumericDefaultText(doc, &m)

	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// keepNumericDefaultText 把 configSchema 里写成数字的 default 换回原文（Number）。
//
// 环境变量是文本：`default: 1.10` 注入的应当是 "1.10"，跟同一个值写在 config/ 里一样；
// 经过 yaml 的 float64 就成了 "1.1"（`007` 成了 "7"）。不在 ConfigProperty 上写 UnmarshalYAML：
// 自己解析的类型 yamlcheck 不往里查，configSchema 里的拼写错误就再也报不出来了。
func keepNumericDefaultText(doc *yaml.Node, m *Manifest) {
	if m.ConfigSchema == nil {
		return
	}
	props := mappingValue(mappingValue(doc, "configSchema"), "properties")
	if props == nil {
		return
	}
	for i := 0; i+1 < len(props.Content); i += 2 {
		key := props.Content[i].Value
		d := mappingValue(props.Content[i+1], "default")
		prop, ok := m.ConfigSchema.Properties[key]
		if !ok || d == nil || d.Kind != yaml.ScalarNode || (d.ShortTag() != "!!int" && d.ShortTag() != "!!float") {
			continue
		}
		prop.Default = Number(d.Value)
		m.ConfigSchema.Properties[key] = prop
	}
}

// mappingValue 是映射节点里 key 对应的值节点；不是映射或没有这个键时为 nil。
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// walkUnknownFields 做未知字段检查，并给"依赖项里另写 version:"补一句该怎么写。
//
// 这是最自然的写错法（别的生态里版本几乎都是独立的键），而通用的"这一层可用的
// 字段：id、optional"只告诉作者它不认识，没告诉作者版本去了哪儿。
func walkUnknownFields(doc *yaml.Node, shape *clierr.ProblemSet) {
	// clierr:nohint 只用来收集问题（取 Items），从不渲染给人看，所以标题留空、也不进目录
	found := clierr.NewProblemSet(clierr.CodeManifestInvalid, "")
	yamlcheck.Walk(doc, reflect.TypeOf(Manifest{}), found)
	for _, problem := range found.Items() {
		reason := problem.Reason
		if strings.HasPrefix(problem.Field, "dependencies.components[") &&
			strings.HasSuffix(problem.Field, "].version") {
			reason += i18n.T(msgid.ManifestUnknownVersionKeySuffix)
		}
		shape.Add(problem.Field, reason)
	}
}

// checkTypes 把解不进字段类型的值按字段路径报出来（yamlcheck.TypeMismatches）。
// 形状检查已经报过的字段（及其下层）不再重复。
func checkTypes(doc *yaml.Node, shape *clierr.ProblemSet) {
	var reported []string
	for _, item := range shape.Items() {
		if item.Field == FileName {
			return // 整份文档就不对（顶层不是映射），下面每个字段都无从谈起
		}
		reported = append(reported, item.Field)
	}
	yamlcheck.TypeMismatches(doc, reflect.TypeOf(Manifest{}), func(field, message string) {
		for _, r := range reported {
			if field == r || strings.HasPrefix(field, r+".") || strings.HasPrefix(field, r+"[") {
				return
			}
		}
		shape.Add(field, message)
	})
}

func syntaxError(source string, cause error) error {
	return clierr.New(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestNotValidYAML, FileName)).
		WithDetail(i18n.T(msgid.LabelFile), source).
		WithDetail(i18n.T(msgid.LabelReason), cleanYAMLError(cause)).
		WithHint(i18n.T(msgid.ProblemHintCheckSyntax)).
		WithCause(cause)
}

func decodeError(source string, cause error) error {
	p := newProblems(source)
	var typeErr *yaml.TypeError
	if ok := asTypeError(cause, &typeErr); ok {
		for _, msg := range typeErr.Errors {
			p.Add(i18n.T(msgid.ProblemLabelTypeMismatch), msg)
		}
	} else {
		p.Add(i18n.T(msgid.ProblemLabelParseFailed), cleanYAMLError(cause))
	}
	return p.Err()
}

func asTypeError(err error, target **yaml.TypeError) bool {
	if te, ok := err.(*yaml.TypeError); ok {
		*target = te
		return true
	}
	return false
}

// cleanYAMLError 去掉 yaml 库的 "yaml: " 前缀，保留行号信息。
func cleanYAMLError(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "yaml: ")
	return strings.TrimSpace(msg)
}

// ============================================================
// 结构形状检查
// ============================================================

// sequenceFields 是必须写成数组的字段路径（点号表示嵌套）。
var sequenceFields = [][]string{
	{"tags"},
	{"artifacts"},
	{"dependencies", "components"},
	{"deployment", "extraPorts"},
	{"migration", "command"},
	{"configSchema", "required"},
}

// checkShapes 在结构体解码前检查节点形状，
// 这样才能给出"migration.command 必须是数组格式"这类精确提示，
// 而不是把 yaml 库的 "cannot unmarshal !!str into []string" 抛给用户。
func checkShapes(doc *yaml.Node, p *clierr.ProblemSet) {
	if doc.Kind != yaml.MappingNode {
		p.Add(FileName, i18n.T(msgid.ProblemTopLevelMustBeMapping))
		return
	}

	for _, path := range sequenceFields {
		node := lookup(doc, path...)
		if node == nil || isNull(node) {
			continue
		}
		if node.Kind != yaml.SequenceNode {
			p.Add(strings.Join(path, "."), i18n.T(msgid.ProblemMustBeArray, yamlcheck.KindName(node)))
		}
	}

	// artifacts[i].files 同样必须是数组。
	if artifacts := lookup(doc, "artifacts"); artifacts != nil && artifacts.Kind == yaml.SequenceNode {
		for i, item := range artifacts.Content {
			if item.Kind != yaml.MappingNode {
				p.Add(fmt.Sprintf("artifacts[%d]", i), i18n.T(msgid.ManifestArtifactMustBeMapping))
				continue
			}
			files := lookup(item, "files")
			if files != nil && !isNull(files) && files.Kind != yaml.SequenceNode {
				p.Add(fmt.Sprintf("artifacts[%d].files", i), i18n.T(msgid.ProblemMustBeArray, yamlcheck.KindName(files)))
			}
		}
	}

	// deployment.labels 必须是映射，而且每个值都得是字符串
	// ——`traefik.enable: true` 少的那对引号在这里报。
	if labels := lookup(doc, "deployment", "labels"); labels != nil && !isNull(labels) {
		if labels.Kind != yaml.MappingNode {
			p.Add("deployment.labels", i18n.T(msgid.ProblemMustBeMapping, yamlcheck.KindName(labels)))
		} else {
			yamlcheck.CheckStringValues(labels, "deployment.labels", p.Add)
		}
	}
}

// lookup 按路径逐层查找映射中的值节点，未找到返回 nil。
func lookup(node *yaml.Node, path ...string) *yaml.Node {
	current := node
	for _, key := range path {
		if current == nil || current.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(current.Content); i += 2 {
			if current.Content[i].Value == key {
				next = current.Content[i+1]
				break
			}
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

func isNull(node *yaml.Node) bool {
	return node.Tag == "!!null"
}

// ============================================================
// 依赖项的两种写法
// ============================================================

// UnmarshalYAML 支持组件依赖的两种写法：
//
//   - department/tree@1.0.0              # 标量：强依赖
//   - id: infra/redis-event-bus@1.0.0    # 映射：可带 optional
//     optional: true
func (d *ComponentDep) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		d.Ref = value.Value
	case yaml.MappingNode:
		var raw struct {
			ID       string `yaml:"id"`
			Optional bool   `yaml:"optional"`
		}
		// 自己解析的类型，类型检查只能整个交给这里：键写错了什么，由这里说清楚
		var problems []string
		yamlcheck.TypeMismatches(value, reflect.TypeOf(raw), func(field, message string) {
			problems = append(problems, field+": "+message)
		})
		if len(problems) > 0 {
			return errors.New(strings.Join(problems, "; "))
		}
		if err := value.Decode(&raw); err != nil {
			return err
		}
		d.Ref = raw.ID
		d.Optional = raw.Optional
	default:
		return errors.New(i18n.T(msgid.ManifestDependencyBadShape))
	}

	d.ID, d.Version, _ = SplitRef(d.Ref)
	return nil
}
