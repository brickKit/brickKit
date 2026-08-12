package manifest

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
)

// FileName 是 Manifest 的固定文件名（002 §2.1）。
const FileName = "component.yaml"

// ParseFile 读取并解析一个 component.yaml。
func ParseFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误："+FileName+" 不存在").
			WithDetail("路径", path).
			WithHint(
				"确认组件目录中包含 component.yaml",
				"确认 --path / 安装源指向的目录正确",
			).WithCause(err)
	case err != nil:
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误：读取 "+FileName+" 失败").
			WithDetail("路径", path).
			WithDetail("原因", err.Error()).
			WithHint("检查文件权限").
			WithCause(err)
	}
	return Parse(data, path)
}

// Parse 解析并校验 component.yaml。source 用于错误提示（文件路径或安装源描述）。
//
// 解析分三步：
//  1. YAML 语法解析（语法错误带行号）
//  2. 结构形状检查（该是数组的字段必须是数组，给出精确字段名）
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
		return nil, clierr.New(clierr.CodeManifestInvalid, "错误："+FileName+" 内容为空").
			WithDetail("文件", source).
			WithHint("参考 002 组件规范 §2.2 编写 component.yaml")
	}

	doc := root.Content[0]
	shape := newProblems(source)
	checkShapes(doc, shape)
	if shape.Len() > 0 {
		return nil, shape.Err()
	}

	var m Manifest
	if err := doc.Decode(&m); err != nil {
		return nil, decodeError(source, err)
	}
	m.Source = source

	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func syntaxError(source string, cause error) error {
	return clierr.New(clierr.CodeManifestInvalid, "错误："+FileName+" 不是合法的 YAML").
		WithDetail("文件", source).
		WithDetail("原因", cleanYAMLError(cause)).
		WithHint("按报错行号检查缩进与语法").
		WithCause(cause)
}

func decodeError(source string, cause error) error {
	p := newProblems(source)
	var typeErr *yaml.TypeError
	if ok := asTypeError(cause, &typeErr); ok {
		for _, msg := range typeErr.Errors {
			p.Add("类型不匹配", msg)
		}
	} else {
		p.Add("解析失败", cleanYAMLError(cause))
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
	{"dependencies", "resources"},
	{"deployment", "extraPorts"},
	{"migration", "command"},
	{"configSchema", "required"},
}

// checkShapes 在结构体解码前检查节点形状，
// 这样才能给出"migration.command 必须是数组格式"这类精确提示，
// 而不是把 yaml 库的 "cannot unmarshal !!str into []string" 抛给用户。
func checkShapes(doc *yaml.Node, p *clierr.ProblemSet) {
	if doc.Kind != yaml.MappingNode {
		p.Add(FileName, "顶层必须是一个 YAML 映射（key: value 结构）")
		return
	}

	for _, path := range sequenceFields {
		node := lookup(doc, path...)
		if node == nil || isNull(node) {
			continue
		}
		if node.Kind != yaml.SequenceNode {
			p.Addf(strings.Join(path, "."), "必须是数组格式（当前是 %s）", nodeKindName(node))
		}
	}

	// artifacts[i].files 同样必须是数组。
	if artifacts := lookup(doc, "artifacts"); artifacts != nil && artifacts.Kind == yaml.SequenceNode {
		for i, item := range artifacts.Content {
			if item.Kind != yaml.MappingNode {
				p.Addf(fmt.Sprintf("artifacts[%d]", i), "必须是映射（包含 type 与 files）")
				continue
			}
			files := lookup(item, "files")
			if files != nil && !isNull(files) && files.Kind != yaml.SequenceNode {
				p.Addf(fmt.Sprintf("artifacts[%d].files", i), "必须是数组格式（当前是 %s）", nodeKindName(files))
			}
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

func nodeKindName(node *yaml.Node) string {
	switch node.Kind {
	case yaml.ScalarNode:
		return "标量"
	case yaml.MappingNode:
		return "映射"
	case yaml.SequenceNode:
		return "数组"
	case yaml.AliasNode:
		return "别名"
	default:
		return "未知类型"
	}
}

// ============================================================
// 依赖项的两种写法
// ============================================================

// UnmarshalYAML 支持组件依赖的两种写法（002 §3.2）：
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
		if err := value.Decode(&raw); err != nil {
			return err
		}
		d.Ref = raw.ID
		d.Optional = raw.Optional
	default:
		return fmt.Errorf("依赖项必须是 <组件ID>@<版本> 字符串或含 id 字段的映射")
	}

	if id, version, found := strings.Cut(d.Ref, "@"); found {
		d.ID, d.Version = id, version
	} else {
		d.ID, d.Version = d.Ref, ""
	}
	return nil
}
