package configdir

import (
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// Entry 是配置文件里的一行。
type Entry struct {
	Key   string
	Value Value
	Line  int
}

// File 是一份扁平的配置文件（组件配置或 vars.yaml），保持文件里的顺序。
type File struct {
	Path    string
	Entries []Entry
}

// Lookup 取一个键的值。
func (f *File) Lookup(key string) (Value, bool) {
	if f == nil {
		return Value{}, false
	}
	for _, e := range f.Entries {
		if e.Key == key {
			return e.Value, true
		}
	}
	return Value{}, false
}

// Map 把条目转成 map。
func (f *File) Map() map[string]Value {
	out := map[string]Value{}
	if f == nil {
		return out
	}
	for _, e := range f.Entries {
		out[e.Key] = e.Value
	}
	return out
}

// Keys 按文件顺序返回全部键。
func (f *File) Keys() []string {
	if f == nil {
		return nil
	}
	out := make([]string, 0, len(f.Entries))
	for _, e := range f.Entries {
		out = append(out, e.Key)
	}
	return out
}

// ParseComponentFile 解析 config/<id>.yaml。有重复键时返回 *ConflictError（大声失败）。
func ParseComponentFile(data []byte, source string) (*File, error) {
	return parseFlat(data, source, false)
}

// ParseVarsFile 解析 config/vars.yaml：不允许 $var: 链式引用。
func ParseVarsFile(data []byte, source string) (*File, error) {
	return parseFlat(data, source, true)
}

// ParseVarsMap 按 vars.yaml 的同一套规则检查部署文件里的 vars:。
func ParseVarsMap(raw map[string]yaml.Node, source string) (map[string]Value, error) {
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	p := newProblems(source)
	out := make(map[string]Value, len(raw))
	for _, key := range keys {
		field := "vars." + key
		node := raw[key]
		v, err := ParseNode(&node)
		if err != nil {
			p.Add(field, err.Error())
			continue
		}
		if v.Kind == KindVarRef {
			p.Add(field, i18n.T(msgid.ConfigdirVarsNoChain))
			continue
		}
		out[key] = v
	}
	if err := p.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseFlat(data []byte, source string, isVars bool) (*File, error) {
	doc, err := yamlfile.Document(data, source, true)
	if err != nil {
		return nil, err
	}
	f := &File{Path: source}
	if doc == nil {
		return f, nil
	}
	// 冲突优先：文件处在冲突状态时，别的问题都等冲突解决之后再说
	if conflict := findConflicts(doc, source); conflict != nil {
		return nil, conflict
	}

	p := newProblems(source)
	for i := 0; i+1 < len(doc.Content); i += 2 {
		keyNode, valueNode := doc.Content[i], doc.Content[i+1]
		key := keyNode.Value
		if !IsValidName(key) {
			p.Add(key, i18n.T(msgid.ConfigdirKeyInvalid))
			continue
		}
		v, err := ParseNode(valueNode)
		if err != nil {
			p.Add(key, err.Error())
			continue
		}
		if isVars && v.Kind == KindVarRef {
			p.Add(key, i18n.T(msgid.ConfigdirVarsNoChain))
			continue
		}
		f.Entries = append(f.Entries, Entry{Key: key, Value: v, Line: keyNode.Line})
	}
	if err := p.Err(); err != nil {
		return nil, err
	}
	return f, nil
}

func newProblems(source string) *clierr.ProblemSet {
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, filepath.Base(source))).
		WithSource(i18n.T(msgid.LabelFile), source).WithHint(i18n.T(msgid.ConfigdirHintFieldReference))
}
