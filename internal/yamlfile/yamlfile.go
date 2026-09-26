// Package yamlfile 是三层文件（brickkit.yaml / deploy*.yaml / config/*.yaml）共用的
// "读文件 → 解析 YAML → 顶层必须是映射 → 解码"流水线，以及它们统一的报错。
package yamlfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// Read 读取文件。文件不存在时原样返回满足 errors.Is(err, fs.ErrNotExist) 的错误——
// 缺了算不算错、该怎么提示，只有调用方知道（deploy.yaml 缺了要 init，vars.yaml 缺了无所谓）。
func Read(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerReadFailed, filepath.Base(path))).
		WithDetail(i18n.T(msgid.LabelPath), path).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.ProblemHintCheckPermissions)).
		WithCause(err)
}

// Document 把 data 解析成顶层映射节点。
//
// allowEmpty 为 true 时，空文件（或只有注释）返回 (nil, nil)：vars.yaml、组件配置文件
// 允许是空的；brickkit.yaml 与 deploy 文件不允许。
func Document(data []byte, source string, allowEmpty bool) (*yaml.Node, error) {
	name := filepath.Base(source)
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerNotValidYAML, name)).
			WithDetail(i18n.T(msgid.LabelFile), source).
			WithDetail(i18n.T(msgid.LabelReason), CleanError(err)).
			WithHint(i18n.T(msgid.ProblemHintCheckSyntax)).
			WithCause(err)
	}
	if root.Kind == 0 || len(root.Content) == 0 || root.Content[0].Tag == "!!null" {
		if allowEmpty {
			return nil, nil
		}
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerEmpty, name)).
			WithDetail(i18n.T(msgid.LabelFile), source)
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, name)).
			WithDetail(i18n.T(msgid.LabelFile), source).
			WithDetail(name, i18n.T(msgid.ProblemTopLevelMustBeMapping))
	}
	return doc, nil
}

// Decode 把 doc 解码进 out。类型不匹配逐条记进 p，返回 false。
func Decode(doc *yaml.Node, out any, p *clierr.ProblemSet) bool {
	err := doc.Decode(out)
	if err == nil {
		return true
	}
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		for _, msg := range typeErr.Errors {
			p.Add(i18n.T(msgid.ProblemLabelTypeMismatch), msg)
		}
	} else {
		p.Add(i18n.T(msgid.ProblemLabelParseFailed), CleanError(err))
	}
	return false
}

// Lookup 沿键路径取子节点，任何一段不存在都返回 nil。
func Lookup(node *yaml.Node, path ...string) *yaml.Node {
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
		current = next
	}
	return current
}

// RequireSequence 要求 node（存在且不是 null 时）是数组；field 是报错里的完整字段路径。
func RequireSequence(node *yaml.Node, field string, p *clierr.ProblemSet) {
	if node != nil && node.Tag != "!!null" && node.Kind != yaml.SequenceNode {
		p.Add(field, i18n.T(msgid.ProblemMustBeArray, yamlcheck.KindName(node)))
	}
}

// RequireMapping 要求 node（存在且不是 null 时）是映射；field 是报错里的完整字段路径。
func RequireMapping(node *yaml.Node, field string, p *clierr.ProblemSet) {
	if node != nil && node.Tag != "!!null" && node.Kind != yaml.MappingNode {
		p.Add(field, i18n.T(msgid.ProblemMustBeMapping, yamlcheck.KindName(node)))
	}
}

// Indexed 把数组下标拼进字段路径：components[2]。
func Indexed(field string, i int) string { return fmt.Sprintf("%s[%d]", field, i) }

// CleanError 去掉 yaml.v3 报错前面的 "yaml: "。
func CleanError(err error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), "yaml: "))
}
