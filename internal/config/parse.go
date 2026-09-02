package config

import (
	"os"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// envVarRe 匹配 ${ENV_VAR} 引用（003 §5.4、附录 D.1）。
// 变量名遵循 shell 惯例：字母或下划线开头，后接字母、数字、下划线。
var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv 把字符串中的 ${ENV_VAR} 替换为环境变量的值。
//
// 环境变量不存在时**保留原样**（不替换成空字符串），
// 这样使用者能在生成的部署文件里一眼看出漏配了哪个变量（开发计划 5.3）。
func ExpandEnv(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		return match
	})
}

// ParseConfigFile 读取并解析 brickkit.yaml。
func ParseConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, clierr.New(clierr.CodeProjectMissing, "错误：项目配置文件不存在").
			WithDetail("路径", path).
			WithHint(
				"在项目目录中执行 brickkit init <项目名称> 初始化项目",
				"或用 --config 指定正确的配置文件路径",
			).WithCause(err)
	case err != nil:
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：读取项目配置文件失败").
			WithDetail("路径", path).
			WithDetail("原因", err.Error()).
			WithHint("检查文件权限").
			WithCause(err)
	}
	return ParseConfig(data, path)
}

// ParseConfig 解析并校验 brickkit.yaml。source 用于错误提示。
//
// 解析分四步：
//  1. YAML 语法解析（语法错误带行号）
//  2. ${ENV_VAR} 展开（只作用于值，不动 key）
//  3. 结构形状检查（该是数组的字段必须是数组）+ 未知字段检查（拼错的键）
//  4. 字段级语义校验（一次报出全部问题）
func ParseConfig(data []byte, source string) (*Config, error) {
	if source == "" {
		source = DefaultConfigFile
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：项目配置文件不是合法的 YAML").
			WithDetail("文件", source).
			WithDetail("原因", cleanYAMLError(err)).
			WithHint("按报错行号检查缩进与语法").
			WithCause(err)
	}
	if root.Kind == 0 || len(root.Content) == 0 {
		return nil, clierr.New(clierr.CodeConfigInvalid, "错误：项目配置文件内容为空").
			WithDetail("文件", source).
			WithHint("参考 003 项目配置规范 §2 编写 brickkit.yaml")
	}

	doc := root.Content[0]
	// 展开**前**先记下哪些密码写的是 ${ENV_VAR}：展开之后就分不出
	// "使用者写死了密码" 与 "使用者写了引用、而这个变量恰好有值" 了
	cfgRefs := configEnvRefs(doc)
	envRefs := passwordEnvRefs(doc)
	expandEnvNode(doc)

	shape := newConfigProblems(source)
	checkConfigShapes(doc, shape)
	// 拼错的字段与形状问题一起报：两者都是"这份配置根本读不对"，
	// 分两轮报会让人改完一处又撞下一处
	yamlcheck.Walk(doc, reflect.TypeOf(Config{}), shape)
	if shape.Len() > 0 {
		return nil, shape.Err()
	}

	var c Config
	if err := doc.Decode(&c); err != nil {
		p := newConfigProblems(source)
		var typeErr *yaml.TypeError
		if te, ok := err.(*yaml.TypeError); ok {
			typeErr = te
			for _, msg := range typeErr.Errors {
				p.Add("类型不匹配", msg)
			}
		} else {
			p.Add("解析失败", cleanYAMLError(err))
		}
		return nil, p.Err()
	}
	c.Source = source
	for i := range c.Resources {
		if i < len(envRefs) {
			c.Resources[i].PasswordFromEnv = envRefs[i]
		}
	}
	for i := range c.Components {
		if i < len(cfgRefs) {
			c.Components[i].ConfigFromEnv = cfgRefs[i]
		}
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// newConfigProblems 创建 brickkit.yaml 校验用的问题收集器。
func newConfigProblems(source string) *clierr.ProblemSet {
	if source == "" {
		source = DefaultConfigFile
	}
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, "错误："+DefaultConfigFile+" 校验失败").
		WithSource("文件", source).
		WithHint(
			"参考 003 项目配置规范 §2 的完整结构",
			"参考附录 D.1 的完整字段参考",
		)
}

func cleanYAMLError(err error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), "yaml: "))
}

// passwordEnvRefs 按 resources 的顺序返回"这条资源的 password 写的是 ${ENV_VAR} 吗"。
//
// 必须在展开**之前**取：展开之后 `${POSTGRES_PASSWORD}` 与一个写死的密码
// 长得一模一样，P5 的明文密码告警会在使用者**做对了**的时候误报
// （变量真的配了才会被展开成明文），而在变量漏配时反倒不吭声。
func passwordEnvRefs(doc *yaml.Node) []bool {
	resources := lookupNode(doc, "resources")
	if resources == nil || resources.Kind != yaml.SequenceNode {
		return nil
	}

	out := make([]bool, 0, len(resources.Content))
	for _, item := range resources.Content {
		password := lookupNode(item, "password")
		out = append(out, password != nil && strings.Contains(password.Value, "${"))
	}
	return out
}

// configEnvRefs 按 components 的顺序返回"这个组件的哪些 config 项写的是 ${ENV_VAR}"。
//
// 与 passwordEnvRefs 同一个道理，也必须在展开**之前**取。
func configEnvRefs(doc *yaml.Node) []map[string]bool {
	components := lookupNode(doc, "components")
	if components == nil || components.Kind != yaml.SequenceNode {
		return nil
	}

	out := make([]map[string]bool, 0, len(components.Content))
	for _, item := range components.Content {
		refs := map[string]bool{}
		if cfg := lookupNode(item, "config"); cfg != nil && cfg.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(cfg.Content); i += 2 {
				key, value := cfg.Content[i], cfg.Content[i+1]
				refs[key.Value] = strings.Contains(value.Value, "${")
			}
		}
		out = append(out, refs)
	}
	return out
}

// expandEnvNode 递归展开节点中的 ${ENV_VAR}。
//
// 只展开**值**：映射的 key 不展开，避免环境变量影响配置结构。
func expandEnvNode(node *yaml.Node) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			expandEnvNode(node.Content[i+1])
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			expandEnvNode(item)
		}
	case yaml.ScalarNode:
		if node.Tag == "!!str" {
			node.Value = ExpandEnv(node.Value)
		}
	}
}

// configSequenceFields 是必须写成数组的字段（003 §2）。
var configSequenceFields = [][]string{
	{"sources"},
	{"components"},
	{"resources"},
}

// checkConfigShapes 在解码前检查节点形状，保证错误提示能指出具体字段。
func checkConfigShapes(doc *yaml.Node, p *clierr.ProblemSet) {
	if doc.Kind != yaml.MappingNode {
		p.Add(DefaultConfigFile, "顶层必须是一个 YAML 映射（key: value 结构）")
		return
	}

	for _, path := range configSequenceFields {
		node := lookupNode(doc, path...)
		if node == nil || node.Tag == "!!null" {
			continue
		}
		if node.Kind != yaml.SequenceNode {
			p.Addf(strings.Join(path, "."), "必须是数组格式（当前是 %s）", nodeKind(node))
		}
	}

	// resources[i].bindings 也必须是数组。
	if resources := lookupNode(doc, "resources"); resources != nil && resources.Kind == yaml.SequenceNode {
		for i, item := range resources.Content {
			if item.Kind != yaml.MappingNode {
				p.Addf(indexed("resources", i), "必须是映射")
				continue
			}
			bindings := lookupNode(item, "bindings")
			if bindings != nil && bindings.Tag != "!!null" && bindings.Kind != yaml.SequenceNode {
				p.Addf(indexed("resources", i)+".bindings", "必须是数组格式（当前是 %s）", nodeKind(bindings))
			}
		}
	}

	// components[i].config 与 components[i].resources 必须是映射。
	if components := lookupNode(doc, "components"); components != nil && components.Kind == yaml.SequenceNode {
		for i, item := range components.Content {
			if item.Kind != yaml.MappingNode {
				p.Addf(indexed("components", i), "必须是映射（至少包含 id 与 version）")
				continue
			}
			for _, key := range []string{"config", "resources", "labels"} {
				node := lookupNode(item, key)
				if node != nil && node.Tag != "!!null" && node.Kind != yaml.MappingNode {
					p.Addf(indexed("components", i)+"."+key, "必须是映射（当前是 %s）", nodeKind(node))
				}
			}
			// labels 的每个值都得是字符串——`traefik.enable: true` 少的
			// 那对引号在这里报（003 §4.11）。
			if labels := lookupNode(item, "labels"); labels != nil && labels.Kind == yaml.MappingNode {
				yamlcheck.CheckStringValues(labels, indexed("components", i)+".labels", p.Add)
			}
		}
	}
}

func lookupNode(node *yaml.Node, path ...string) *yaml.Node {
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

func nodeKind(node *yaml.Node) string {
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
