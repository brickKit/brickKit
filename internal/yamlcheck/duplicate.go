package yamlcheck

import (
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// DuplicateKeys 报出同一个映射里写了两次的键，字段路径指向后写的那一次。
//
// yaml 库遇到重复键也拒绝解码，但它的报错混在 TypeError 里（"mapping key "x" already
// defined"），调用方只能把它当成"类型不匹配"报给人看——而重复键跟类型毫无关系。
// 所以在解码之前，跟未知字段一起，按字段路径单独报。整棵树都查，不看目标类型：
// vars、labels 这类自由映射里写重了，也一样是两个值只留一个的歧义。
func DuplicateKeys(doc *yaml.Node, add func(field, message string)) {
	walkDuplicates(doc, "", add)
}

func walkDuplicates(node *yaml.Node, path string, add func(field, message string)) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			walkDuplicates(child, path, add)
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			walkDuplicates(child, indexPath(path, i), add)
		}
	case yaml.MappingNode:
		first := map[string]int{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			field := joinPath(path, key.Value)
			if line, seen := first[key.Value]; seen {
				add(field, i18n.T(msgid.YamlcheckDuplicateKey, key.Line, line))
			} else {
				first[key.Value] = key.Line
			}
			walkDuplicates(value, field, add)
		}
	}
}
