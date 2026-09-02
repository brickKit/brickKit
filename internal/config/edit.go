package config

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
)

// Edit 是 brickkit.yaml 的原地编辑器（供 brickkit add / remove 使用）。
//
// 它在 YAML 节点层改写，**不经过 Config 结构体**，因此：
//
//	注释、字段顺序都保留；
//	`${ENV_VAR}` 保持原样，绝不会把展开后的密钥写回文件（003 §5.4）。
//
// yaml.v3 的节点往返不保留顶层块之间的空行，Save 会按原文把它们补回来
// （见 restoreBlankLines）：brickkit.yaml 是人要读的文件，排版不能越编辑越挤。
type Edit struct {
	path string
	doc  *yaml.Node // 文档节点（承载文件头部注释）
	root *yaml.Node // 顶层映射
	// original 是读入时的原文，用于还原空行排版。
	original []byte
}

// OpenEdit 打开配置文件准备编辑。
func OpenEdit(path string) (*Edit, error) {
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, clierr.New(clierr.CodeProjectMissing, "错误：项目未初始化").
			WithDetail("路径", path).
			WithHint(
				"在项目根目录执行本命令",
				"或先执行 brickkit init <项目名称> 初始化项目",
			).WithCause(err)
	case err != nil:
		return nil, wrapIOError("读取配置", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：brickkit.yaml 不是合法的 YAML").
			WithDetail("文件", path).
			WithDetail("原因", cleanYAMLError(err)).
			WithCause(err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：brickkit.yaml 结构不合法").
			WithDetail("文件", path).
			WithHint("顶层必须是键值映射（project / deploy / components ...）")
	}
	return &Edit{path: path, doc: &doc, root: doc.Content[0], original: data}, nil
}

// HasComponent 判断配置中是否已有该组件版本。
func (e *Edit) HasComponent(id, version string) bool {
	return e.findComponent(id, version) >= 0
}

// AddComponent 追加一个组件条目。已存在时返回 false，不重复写入。
//
// 只写 id 与 version：add 自动添加的组件**不写 enabled 字段**（004 §3.3 关键规则），
// 只有用户手写 enabled: true 才是"钉住"的显式意图。
func (e *Edit) AddComponent(id, version string) bool {
	if e.HasComponent(id, version) {
		return false
	}
	seq := e.componentsNode(true)
	// `components: []` 是流式空序列，加入条目后要切回块式，否则会写成一行
	seq.Style = 0
	seq.Tag = "!!seq"
	seq.Content = append(seq.Content, componentNode(id, version))
	return true
}

// RemoveComponent 删除一个组件条目。不存在时返回 false。
func (e *Edit) RemoveComponent(id, version string) bool {
	i := e.findComponent(id, version)
	if i < 0 {
		return false
	}
	seq := e.componentsNode(false)
	seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
	return true
}

// keyEnabled 是组件条目里那个启停字段的键名。
const keyEnabled = "enabled"

// SetComponentEnabled 把某个组件条目的 enabled 设成给定值。条目不存在时返回 false。
//
// 在节点层改而不是重新序列化整个结构体：注释、字段顺序、`${ENV_VAR}` 全部原样
// （与 AddComponent / RemoveComponent 同一个理由）。
func (e *Edit) SetComponentEnabled(id, version string, enabled bool) bool {
	item := e.componentItem(id, version)
	if item == nil {
		return false
	}
	if node := mappingValue(item, keyEnabled); node != nil {
		// 就地改标量：这样行尾注释（yaml.Node 挂在键或值上的 LineComment）留得住
		node.Kind = yaml.ScalarNode
		node.Tag = "!!bool"
		node.Value = strconv.FormatBool(enabled)
		node.Style = 0
		node.Content = nil
		node.Alias = nil
		return true
	}
	item.Content = append(item.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: keyEnabled},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(enabled)})
	return true
}

// ClearComponentEnabled 删掉某个组件条目的 enabled 字段。
//
// 条目不存在、或本来就没写 enabled 时返回 false。
//
// 删字段与写 enabled: false **不是一回事**：不写才是默认（跟着上层走），
// 写了才是"钉住"的显式意图（004 §3.3）。还原时必须能表达"回到不写"。
func (e *Edit) ClearComponentEnabled(id, version string) bool {
	item := e.componentItem(id, version)
	if item == nil {
		return false
	}
	for i := 0; i+1 < len(item.Content); i += 2 {
		if item.Content[i].Value == keyEnabled {
			item.Content = append(item.Content[:i], item.Content[i+2:]...)
			return true
		}
	}
	return false
}

// componentItem 找到某个组件条目的映射节点，不存在时返回 nil。
func (e *Edit) componentItem(id, version string) *yaml.Node {
	i := e.findComponent(id, version)
	if i < 0 {
		return nil
	}
	return e.componentsNode(false).Content[i]
}

// HasComponentID 判断配置中还有没有该组件 ID 的**任何**版本。
//
// 资源绑定按组件 ID 记（`bindings[].componentId` 不带版本，003 §5.3），
// 所以多版本共存时移除其中一个版本，那条绑定还得留着给剩下的版本用。
func (e *Edit) HasComponentID(id string) bool {
	seq := e.componentsNode(false)
	if seq == nil {
		return false
	}
	for _, item := range seq.Content {
		if item.Kind == yaml.MappingNode && scalarField(item, "id") == id {
			return true
		}
	}
	return false
}

// RemoveBindings 删除所有指向该组件的资源绑定，返回受影响的资源 ID。
//
// # 为什么 remove 必须做这件事
//
// 组件没了，指着它的绑定就是一条谁也用不上的配置。留着它不是"无害的残留"：
// 使用者会看到一条 `remove` 报成功，然后**下一条命令就跑不了**——
// 那条绑定会在解析阶段被当成问题拦下来，而错误信息说的是资源配置，
// 与他刚做的事对不上号。
//
// 这与 012 §2.20 的立场一致：remove 就是彻底移除，连归档目录里的源码
// 都要一并删掉，没道理单单留下一行会绊人的配置。
func (e *Edit) RemoveBindings(componentID string) []string {
	resources := e.resourcesNode()
	if resources == nil {
		return nil
	}

	var affected []string
	for _, resource := range resources.Content {
		if resource.Kind != yaml.MappingNode {
			continue
		}
		bindings := mappingValue(resource, "bindings")
		if bindings == nil || bindings.Kind != yaml.SequenceNode {
			continue
		}

		kept := make([]*yaml.Node, 0, len(bindings.Content))
		removed := false
		for _, binding := range bindings.Content {
			if binding.Kind == yaml.MappingNode && scalarField(binding, "componentId") == componentID {
				removed = true
				continue
			}
			kept = append(kept, binding)
		}
		if !removed {
			continue
		}
		// 清空后保留 `bindings: []` 而不是删掉这个键：资源本身还在，
		// 一个空的绑定列表恰好说明"这台库现在没人用"，比键凭空消失更好读
		bindings.Content = kept
		affected = append(affected, scalarField(resource, "id"))
	}
	return affected
}

// Save 把修改写回文件。
func (e *Edit) Save() error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(e.doc); err != nil {
		return clierr.New(clierr.CodeConfigInvalid, "错误：生成 brickkit.yaml 失败").
			WithDetail("原因", err.Error()).
			WithCause(err)
	}
	if err := enc.Close(); err != nil {
		return clierr.New(clierr.CodeConfigInvalid, "错误：生成 brickkit.yaml 失败").
			WithDetail("原因", err.Error()).
			WithCause(err)
	}
	if err := os.WriteFile(e.path, restoreBlankLines(e.original, buf.Bytes()), filePerm); err != nil {
		return wrapIOError("写入配置", e.path, err)
	}
	return nil
}

// topLevelKeyRe 匹配顶层键（顶格、无缩进）。
var topLevelKeyRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*):`)

// restoreBlankLines 按原文把顶层块之间的空行补回编码结果。
//
// yaml.v3 的节点模型里没有空行，往返一次就会把 `project` / `deploy` / `components`
// 之间的呼吸感全部压掉。这里只做一件事：原文中某个顶层键前面有空行，
// 编码结果里也补上一行空行（有头部注释时补在注释块之前）。
func restoreBlankLines(original, encoded []byte) []byte {
	spaced := keysPrecededByBlankLine(original)
	if len(spaced) == 0 {
		return encoded
	}

	lines := strings.Split(string(encoded), "\n")
	out := make([]string, 0, len(lines)+len(spaced))
	for _, line := range lines {
		m := topLevelKeyRe.FindStringSubmatch(line)
		if m != nil && spaced[m[1]] {
			// 回退到该键的注释块开头
			insert := len(out)
			for insert > 0 && strings.HasPrefix(strings.TrimSpace(out[insert-1]), "#") {
				insert--
			}
			if insert > 0 && strings.TrimSpace(out[insert-1]) != "" {
				out = append(out, "")
				copy(out[insert+1:], out[insert:])
				out[insert] = ""
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

// keysPrecededByBlankLine 找出原文中前面隔了空行的顶层键。
func keysPrecededByBlankLine(original []byte) map[string]bool {
	lines := strings.Split(string(original), "\n")
	spaced := map[string]bool{}

	for i, line := range lines {
		m := topLevelKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// 越过该键上方连续的注释行
		j := i - 1
		for j >= 0 && strings.HasPrefix(strings.TrimSpace(lines[j]), "#") {
			j--
		}
		if j >= 0 && strings.TrimSpace(lines[j]) == "" {
			spaced[m[1]] = true
		}
	}
	return spaced
}

// findComponent 返回该组件版本在 components 序列中的下标，不存在时返回 -1。
func (e *Edit) findComponent(id, version string) int {
	seq := e.componentsNode(false)
	if seq == nil {
		return -1
	}
	for i, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if scalarField(item, "id") == id && scalarField(item, "version") == version {
			return i
		}
	}
	return -1
}

// componentsNode 返回 components 序列节点。create 为 true 时在缺失的情况下创建。
func (e *Edit) componentsNode(create bool) *yaml.Node {
	for i := 0; i+1 < len(e.root.Content); i += 2 {
		if e.root.Content[i].Value != "components" {
			continue
		}
		value := e.root.Content[i+1]
		// `components:`（null）等价于空列表
		if value.Kind != yaml.SequenceNode {
			if !create {
				return nil
			}
			*value = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		}
		return value
	}
	if !create {
		return nil
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "components"}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	e.root.Content = append(e.root.Content, key, seq)
	return seq
}

// resourcesNode 返回 resources 序列节点；缺失或不是序列时返回 nil。
//
// 与 componentsNode 不同，它从不创建：编辑器不会凭空给使用者加资源声明。
func (e *Edit) resourcesNode() *yaml.Node {
	for i := 0; i+1 < len(e.root.Content); i += 2 {
		if e.root.Content[i].Value != "resources" {
			continue
		}
		if value := e.root.Content[i+1]; value.Kind == yaml.SequenceNode {
			return value
		}
		return nil
	}
	return nil
}

// componentNode 构造 `- id: <id>\n  version: <version>` 条目。
func componentNode(id, version string) *yaml.Node {
	return &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "id"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: id},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "version"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: version},
		},
	}
}

// scalarField 取映射节点中某个键的标量值。
func scalarField(node *yaml.Node, key string) string {
	if value := mappingValue(node, key); value != nil {
		return value.Value
	}
	return ""
}

// mappingValue 取映射节点中某个键的值节点，不存在时返回 nil。
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
