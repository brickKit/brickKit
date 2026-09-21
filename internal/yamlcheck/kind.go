package yamlcheck

import (
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// KindName 返回一个 YAML 节点种类的名字，用在"必须是数组格式（当前是 ××）"这类形状错误里。
//
// component.yaml 与 brickkit.yaml 的形状检查都要这句话，放在这里两边共用。
func KindName(node *yaml.Node) string {
	switch node.Kind {
	case yaml.ScalarNode:
		return i18n.T(msgid.YamlcheckKindScalar)
	case yaml.MappingNode:
		return i18n.T(msgid.YamlcheckKindMapping)
	case yaml.SequenceNode:
		return i18n.T(msgid.YamlcheckKindArray)
	case yaml.AliasNode:
		return i18n.T(msgid.YamlcheckKindAlias)
	default:
		return i18n.T(msgid.YamlcheckKindUnknown)
	}
}
