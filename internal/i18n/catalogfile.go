package i18n

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

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
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, map[string]string{}, nil
		}
		return nil, nil, &CatalogError{File: file, Line: 1, Reason: err.Error()}
	}
	// 只读第一份文档就等于悄悄截掉 --- 之后的所有条目
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, nil, &CatalogError{File: file, Line: 1, Reason: err.Error()}
		}
		line := extra.Line
		if len(extra.Content) > 0 {
			line = extra.Content[0].Line
		}
		return nil, nil, &CatalogError{File: file, Line: line, Reason: "a second YAML document starts here: remove the stray --- line above it"}
	}
	if len(doc.Content) == 0 {
		return nil, map[string]string{}, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, &CatalogError{File: file, Line: root.Line, Reason: "the file must be a mapping of key: text"}
	}
	lines := strings.Split(string(data), "\n")
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
		if v.Style == yaml.DoubleQuotedStyle && !quotedOnOneLine(lines, v) {
			return nil, nil, &CatalogError{File: file, Line: k.Line, Key: k.Value, Reason: `a double-quoted text must stay on one line (YAML folds a line break into a space); write a multi-line text as a literal block (|), or use \n`}
		}
		if v.Style == yaml.LiteralStyle && !blockIndentedByTwo(lines, k, v) {
			return nil, nil, &CatalogError{File: file, Line: k.Line, Key: k.Value, Reason: "indent a literal block by two spaces (YAML takes the first line's indentation for the whole block, so extra spaces vanish); for a text that starts with spaces, write the indentation indicator: |2"}
		}
		keys = append(keys, k.Value)
		texts[k.Value] = v.Value
	}
	return keys, texts, nil
}

// quotedOnOneLine 报告双引号纯量的收尾引号是否与开头在同一行。
// yaml.v3 的列号按字符计，所以按 rune 取那一行。
func quotedOnOneLine(lines []string, v *yaml.Node) bool {
	line := []rune(lines[v.Line-1])
	for i := v.Column; i < len(line); i++ { // line[v.Column-1] 是开头的引号
		switch line[i] {
		case '\\':
			i++
		case '"':
			return true
		}
	}
	return false
}

// blockIndentedByTwo 报告字面块的内容是否比 key 多缩进两格。写了缩进标记（|2）的块，
// 缩进由标记说了算，不查。
func blockIndentedByTwo(lines []string, k, v *yaml.Node) bool {
	header := []rune(lines[v.Line-1])[v.Column-1:]
	for _, r := range header {
		if r == ' ' || r == '#' {
			break
		}
		if r >= '1' && r <= '9' {
			return true
		}
	}
	keyIndent := k.Column - 1
	for _, l := range lines[v.Line:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		return indent <= keyIndent || indent == keyIndent+2 // 缩进不超过 key 的是下一条：空块
	}
	return true
}
