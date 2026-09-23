package override

import (
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// ParseOverrideFile 读取并解析 override.yaml。
//
// 文件不存在时返回 (nil, nil)——这份机制是选用的（设计书 §3），"没有 override.yaml"
// 与"brickkit.yaml 缺失"完全不是同一类事情，不能沿用 config.ParseConfigFile 那种
// "文件缺失就报错"的处理。
func ParseOverrideFile(path string) (*Override, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.OverrideReadFailed)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.ProblemHintCheckPermissions)).
			WithCause(err)
	}
	return ParseOverride(data, path)
}

// ParseOverride 解析并校验 override.yaml 的单文件规则。source 用于错误提示。
//
// 跟 config.ParseConfig 同一个四步骨架（语法 → 形状+未知字段 → 解码 → 语义校验），
// 少一步：override.yaml 没有任何字段合理地需要 ${VAR} 展开（target/mode 是枚举，
// localPort 是数字，baseline 只是取值的镜像），所以不做 config.ExpandEnv 那一遍。
func ParseOverride(data []byte, source string) (*Override, error) {
	if source == "" {
		source = "override.yaml"
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.OverrideNotValidYAML)).
			WithDetail(i18n.T(msgid.LabelFile), source).
			WithDetail(i18n.T(msgid.LabelReason), cleanYAMLError(err)).
			WithHint(i18n.T(msgid.ProblemHintCheckSyntax)).
			WithCause(err)
	}
	if root.Kind == 0 || len(root.Content) == 0 {
		// 空文件等价于"这份覆盖什么都没说"——不是错误，与文件不存在同一个
		// 结论（override.yaml 设计书 §2："要么对某件事完全不吭声，要么明说"，
		// 空文件是"对所有事都不吭声"的极端情形，合法）。
		return &Override{Source: source}, nil
	}

	doc := root.Content[0]

	shape := newOverrideProblems(source)
	checkOverrideShapes(doc, shape)
	yamlcheck.Walk(doc, reflect.TypeOf(Override{}), shape)
	if shape.Len() > 0 {
		return nil, shape.Err()
	}

	var o Override
	if err := doc.Decode(&o); err != nil {
		p := newOverrideProblems(source)
		if te, ok := err.(*yaml.TypeError); ok {
			for _, msg := range te.Errors {
				p.Add(i18n.T(msgid.ProblemLabelTypeMismatch), msg)
			}
		} else {
			p.Add(i18n.T(msgid.ProblemLabelParseFailed), cleanYAMLError(err))
		}
		return nil, p.Err()
	}
	o.Source = source

	if err := o.Validate(); err != nil {
		return nil, err
	}
	return &o, nil
}

func newOverrideProblems(source string) *clierr.ProblemSet {
	if source == "" {
		source = "override.yaml"
	}
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.OverrideValidationFailed)).
		WithSource(i18n.T(msgid.LabelFile), source)
}

func cleanYAMLError(err error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), "yaml: "))
}

// checkOverrideShapes 在解码前检查节点形状：components 与嵌套的 members 都必须是数组，
// 数组里的每一项都必须是映射——跟 config.checkConfigShapes 同一个用意（003 §2 那条规则
// 的 override.yaml 版本），只是这份文件的字段少得多，不需要 configSequenceFields 那张表。
func checkOverrideShapes(doc *yaml.Node, p *clierr.ProblemSet) {
	if doc.Kind != yaml.MappingNode {
		p.Add("override.yaml", i18n.T(msgid.ProblemTopLevelMustBeMapping))
		return
	}
	if components := lookupOverrideNode(doc, "components"); components != nil && components.Tag != "!!null" {
		checkComponentsShape(components, "components", p)
	}
}

func checkComponentsShape(node *yaml.Node, path string, p *clierr.ProblemSet) {
	if node.Kind != yaml.SequenceNode {
		p.Add(path, i18n.T(msgid.ProblemMustBeArray, yamlcheck.KindName(node)))
		return
	}
	for i, item := range node.Content {
		itemPath := indexed(path, i)
		if item.Kind != yaml.MappingNode {
			p.Add(itemPath, i18n.T(msgid.OverrideComponentMustBeMapping))
			continue
		}
		if members := lookupOverrideNode(item, "members"); members != nil && members.Tag != "!!null" {
			checkComponentsShape(members, itemPath+".members", p)
		}
	}
}

func lookupOverrideNode(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func indexed(field string, i int) string {
	return field + "[" + itoa(i) + "]"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
