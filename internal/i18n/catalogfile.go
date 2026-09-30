package i18n

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// CatalogError 指出目录文件里有问题的那一条。
type CatalogError struct {
	File   string
	Line   int
	Key    string
	Reason string
}

func (e *CatalogError) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Reason)
	}
	return fmt.Sprintf("%s:%d: %s: %s", e.File, e.Line, e.Key, e.Reason)
}

// parseCatalog 读一份目录文件：顶层是 key: 文案 的映射。
//
// 文案只接受双引号纯量与字面块（| |- |+）：其余写法会被 YAML 悄悄改写
// （裸写的 yes、首尾空格、折行），译者看到的与程序拿到的就不是同一句话。
// 同一个 key 出现两次是错误，不是"后写的赢"。
func parseCatalog(data []byte, file string) ([]string, map[string]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, &CatalogError{File: file, Line: 1, Reason: err.Error()}
	}
	if len(doc.Content) == 0 {
		return nil, map[string]string{}, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, &CatalogError{File: file, Line: root.Line, Reason: "the file must be a mapping of key: text"}
	}
	keys := make([]string, 0, len(root.Content)/2)
	texts := make(map[string]string, len(root.Content)/2)
	firstLine := map[string]int{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if first, dup := firstLine[k.Value]; dup {
			return nil, nil, &CatalogError{File: file, Line: k.Line, Key: k.Value, Reason: fmt.Sprintf("duplicate key (first on line %d)", first)}
		}
		firstLine[k.Value] = k.Line
		if v.Kind != yaml.ScalarNode || (v.Style != yaml.DoubleQuotedStyle && v.Style != yaml.LiteralStyle) {
			return nil, nil, &CatalogError{File: file, Line: k.Line, Key: k.Value, Reason: `write the text in double quotes ("…") or as a literal block (|)`}
		}
		keys = append(keys, k.Value)
		texts[k.Value] = v.Value
	}
	return keys, texts, nil
}
