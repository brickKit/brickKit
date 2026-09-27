package yamlfile

// 本文件是三层文件共用的"原地编辑器"：restore 改 deploy.yaml 的 mode，
// add / remove 往 brickkit.yaml 与部署文件里增删条目，都走这里。
//
// 在节点层改，不经过结构体重新序列化，所以注释、字段顺序、`${VAR}` / `$var:`
// 全部原样——这些文件是人要读、要 review 的，编辑一次不该让它面目全非。

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// keyID 是列表条目用来互相区分的键：brickkit.yaml 与部署文件的组件条目都是 `- id: …`。
const keyID = "id"

// keyVersion 是 brickkit.yaml 组件条目的版本键：同一个 ID 的多行靠它区分。
const keyVersion = "version"

// keyMembers 是部署文件里外壳条目下嵌套成员条目的键。
const keyMembers = "members"

// editFilePerm 是编辑后写回的权限：与这些文件 init 时的权限一致。
const editFilePerm = 0o644

// Edit 是一份 YAML 文件的原地编辑器。
type Edit struct {
	path     string
	doc      *yaml.Node // 文档节点（承载文件头部注释）
	root     *yaml.Node // 顶层映射
	original []byte     // 读入时的原文，用于还原空行排版
}

// OpenEdit 打开 path 准备编辑。文件不存在时返回满足 fs.ErrNotExist 的错误。
func OpenEdit(path string) (*Edit, error) {
	data, err := Read(path)
	if err != nil {
		return nil, err
	}
	// 先走一遍与解析相同的检查：不是合法 YAML、多文档、顶层不是映射，报错口径一致
	if _, err := Document(data, path, false); err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err // Document 已经放行，这里不会走到
	}
	return &Edit{path: path, doc: &doc, root: doc.Content[0], original: data}, nil
}

// SetField 把序列 seqKey 里 `id: <id>` 那一条的 field 设成 value。条目不存在时返回 false。
func (e *Edit) SetField(seqKey, id, field, value string) bool {
	item := e.entry(seqKey, id)
	if item == nil {
		return false
	}
	if node := mappingValue(item, field); node != nil {
		// 就地改标量：挂在值上的行尾注释因此留得住
		*node = yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!str", Value: value,
			HeadComment: node.HeadComment, LineComment: node.LineComment, FootComment: node.FootComment,
			Line: node.Line, Column: node.Column,
		}
		return true
	}
	item.Content = append(item.Content, scalar(field), scalar(value))
	return true
}

// DeleteField 删掉 `id: <id>` 那一条的 field。条目或字段不存在时返回 false。
//
// 删字段与写成别的值不是一回事：mode 不写才是"跟着上层走"。
func (e *Edit) DeleteField(seqKey, id, field string) bool {
	item := e.entry(seqKey, id)
	if item == nil {
		return false
	}
	for i := 0; i+1 < len(item.Content); i += 2 {
		if item.Content[i].Value != field {
			continue
		}
		// 键上方的注释常常是一段小节说明，不属于这一个键：挪给下一个键；它是最后一个键时挂在
		// 前一个键的下方（挂在前一个值上的话，值是 labels 这种块结构时 yaml.v3 会把注释甩进
		// 下一个条目、打乱排版）。行尾注释属于这个键本身，随它一起删
		if head := item.Content[i].HeadComment; head != "" {
			switch {
			case i+2 < len(item.Content):
				next := item.Content[i+2]
				next.HeadComment = joinComments(head, next.HeadComment)
			case i >= 2:
				prev := item.Content[i-2]
				prev.FootComment = joinComments(prev.FootComment, head)
			}
		}
		item.Content = append(item.Content[:i], item.Content[i+2:]...)
		return true
	}
	return false
}

// joinComments 把两段注释接在一起（任一为空时返回另一段）。
func joinComments(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n" + b
}

// AppendEntry 在序列 seqKey 末尾追加 `- id: <id>`。已存在时返回 false，不重复写入。
func (e *Edit) AppendEntry(seqKey, id string) bool {
	if e.entry(seqKey, id) != nil {
		return false
	}
	seq := e.sequence(seqKey, true)
	// `components: []` 是流式空序列，加入条目后要切回块式，否则会写成一行
	seq.Style = 0
	seq.Content = append(seq.Content, &yaml.Node{
		Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{scalar(keyID), scalar(id)},
	})
	return true
}

// RemoveEntry 删除序列 seqKey 里 `id: <id>` 那一条（含嵌在外壳条目 members 下面的成员条目，
// 与 entry 的查找范围一致）。最后一个成员删掉后 members 键一并去掉。不存在时返回 false。
func (e *Edit) RemoveEntry(seqKey, id string) bool {
	return e.RemoveWhere(seqKey, Selector{ID: id})
}

// RemoveWhere 删除 sel 选中的那一条（查找范围同 RemoveEntry）。不存在时返回 false。
func (e *Edit) RemoveWhere(seqKey string, sel Selector) bool {
	loc := e.find(seqKey, sel)
	if loc == nil {
		return false
	}
	// 条目上方的注释常常是一段小节说明（与 DeleteField 同一个理由）：条目删了，注释交给
	// 下一个条目；它是最后一条时挂在前一条下方
	head := loc.item().HeadComment
	index, seq := loc.index, loc.seq
	loc.remove()
	if head != "" {
		switch {
		case index < len(seq.Content):
			seq.Content[index].HeadComment = joinComments(head, seq.Content[index].HeadComment)
		case index > 0:
			seq.Content[index-1].FootComment = joinComments(seq.Content[index-1].FootComment, head)
		}
	}
	return true
}

// Field 是 AppendMapping 写入的一个键。Value 是 string，或 []string（写成流式列表
// `requiredBy: [erp/shell]`，与手写的样子一致）。
type Field struct {
	Key   string
	Value any
}

// AppendMapping 在序列 seqKey 末尾追加一条由 fields 组成的条目（第一个键应当是 id）。
// 同一个 id（有 version 键时连同版本）的条目已存在时返回 false，不重复写入。
func (e *Edit) AppendMapping(seqKey string, fields []Field) bool {
	var sel Selector
	item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, f := range fields {
		switch f.Key {
		case keyID:
			sel.ID, _ = f.Value.(string)
		case keyVersion:
			sel.Version, _ = f.Value.(string)
		}
		item.Content = append(item.Content, scalar(f.Key), valueNode(f.Value))
	}
	if e.find(seqKey, sel) != nil {
		return false
	}
	seq := e.sequence(seqKey, true)
	seq.Style = 0
	seq.Content = append(seq.Content, item)
	return true
}

// SetList 把 sel 选中条目的 field 设成流式列表；values 为空时删掉这个字段
// （requiredBy 没人了就不该留一个空列表）。条目不存在、或要删的字段本来就没有时返回 false。
func (e *Edit) SetList(seqKey string, sel Selector, field string, values []string) bool {
	loc := e.find(seqKey, sel)
	if loc == nil {
		return false
	}
	item := loc.item()
	for i := 0; i+1 < len(item.Content); i += 2 {
		if item.Content[i].Value != field {
			continue
		}
		if len(values) == 0 {
			item.Content = append(item.Content[:i], item.Content[i+2:]...)
			return true
		}
		old := item.Content[i+1]
		node := valueNode(values)
		node.LineComment, node.FootComment = old.LineComment, old.FootComment
		item.Content[i+1] = node
		return true
	}
	if len(values) == 0 {
		return false
	}
	item.Content = append(item.Content, scalar(field), valueNode(values))
	return true
}

// SetValue 把 sel 选中条目的 field 设成标量 value（没有这个字段就加上）。条目不存在时返回 false。
func (e *Edit) SetValue(seqKey string, sel Selector, field, value string) bool {
	loc := e.find(seqKey, sel)
	if loc == nil {
		return false
	}
	item := loc.item()
	if node := mappingValue(item, field); node != nil {
		*node = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value,
			HeadComment: node.HeadComment, LineComment: node.LineComment, FootComment: node.FootComment}
		return true
	}
	item.Content = append(item.Content, scalar(field), scalar(value))
	return true
}

// DeleteFieldWhere 删掉 sel 选中条目的 field。条目或字段不存在时返回 false。
func (e *Edit) DeleteFieldWhere(seqKey string, sel Selector, field string) bool {
	loc := e.find(seqKey, sel)
	if loc == nil {
		return false
	}
	item := loc.item()
	for i := 0; i+1 < len(item.Content); i += 2 {
		if item.Content[i].Value == field {
			item.Content = append(item.Content[:i], item.Content[i+2:]...)
			return true
		}
	}
	return false
}

// Lift 把嵌在外壳条目下面的 entryID 挪到顶层、紧跟在它的外壳后面（字段与注释跟着走）；
// 最后一个成员挪走后 members 键一并去掉。条目不在任何外壳下面时返回 false。
func (e *Edit) Lift(seqKey, entryID string) bool {
	loc := e.find(seqKey, Selector{ID: entryID})
	if loc == nil || loc.parent == nil {
		return false
	}
	parent := loc.parent
	item := loc.remove()
	seq := e.sequence(seqKey, false)
	for i, top := range seq.Content {
		if top == parent {
			rest := append([]*yaml.Node{}, seq.Content[i+1:]...)
			seq.Content = append(append(seq.Content[:i+1], item), rest...)
			return true
		}
	}
	seq.Content = append(seq.Content, item)
	return true
}

// RenameID 把 `id: <oldID>` 改成 newID（id@1.0.0 ↔ id），无论它在顶层还是嵌在外壳下面。
// 行尾注释留着。不存在时返回 false。
func (e *Edit) RenameID(seqKey, oldID, newID string) bool {
	loc := e.find(seqKey, Selector{ID: oldID})
	if loc == nil {
		return false
	}
	mappingValue(loc.item(), keyID).Value = newID
	return true
}

// Nest 把 entryID 那一条挪到顶层外壳条目 shellID 的 members 下面（字段与注释跟着走）；
// 条目还不存在时以裸条目 `- id: <entryID>` 加进去。外壳条目不在顶层时返回 false。
func (e *Edit) Nest(seqKey, shellID, entryID string) bool {
	_, shell, _ := e.topLevel(seqKey, shellID)
	if shell == nil {
		return false
	}
	var item *yaml.Node
	if loc := e.find(seqKey, Selector{ID: entryID}); loc != nil {
		if loc.parent == shell {
			return true
		}
		item = loc.remove()
	} else {
		item = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{scalar(keyID), scalar(entryID)}}
	}
	members := mappingValue(shell, keyMembers)
	if members == nil || members.Kind != yaml.SequenceNode {
		removeKey(shell, keyMembers)
		members = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		shell.Content = append(shell.Content, scalar(keyMembers), members)
	}
	members.Style = 0
	members.Content = append(members.Content, item)
	return true
}

// Unnest 把顶层外壳条目 shellID 的全部成员挪到顶层、紧跟在外壳后面（字段原样），
// 去掉 members 键，返回挪出来的条目 id。外壳不在或没有成员时返回空。
func (e *Edit) Unnest(seqKey, shellID string) []string {
	seq, shell, index := e.topLevel(seqKey, shellID)
	if shell == nil {
		return nil
	}
	members := mappingValue(shell, keyMembers)
	if members == nil || members.Kind != yaml.SequenceNode {
		return nil
	}
	removeKey(shell, keyMembers)
	var ids []string
	for _, m := range members.Content {
		if v := mappingValue(m, keyID); v != nil {
			ids = append(ids, v.Value)
		}
	}
	rest := append([]*yaml.Node{}, seq.Content[index+1:]...)
	seq.Content = append(append(seq.Content[:index+1], members.Content...), rest...)
	return ids
}

// valueNode 把 AppendMapping / SetList 的值做成节点：[]string 写成流式列表。
func valueNode(v any) *yaml.Node {
	if list, ok := v.([]string); ok {
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for _, s := range list {
			node.Content = append(node.Content, scalar(s))
		}
		return node
	}
	s, _ := v.(string)
	return scalar(s)
}

// removeKey 从映射节点里删掉一个键及其值。
func removeKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

// Save 把修改写回文件。
func (e *Edit) Save() error {
	name := filepath.Base(e.path)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(e.doc)
	if err == nil {
		err = enc.Close()
	}
	if err != nil {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerEncodeFailed, name)).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithCause(err)
	}
	if err := writeAtomic(e.path, restoreBlankLines(e.original, buf.Bytes())); err != nil {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.LayerWriteFailed, name)).
			WithDetail(i18n.T(msgid.LabelPath), e.path).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.ProblemHintCheckPermissions)).
			WithCause(err)
	}
	return nil
}

// entry 找到序列 seqKey 里 `id: <id>` 的映射节点，不存在时返回 nil。
func (e *Edit) entry(seqKey, id string) *yaml.Node {
	loc := e.find(seqKey, Selector{ID: id})
	if loc == nil {
		return nil
	}
	return loc.item()
}

// Selector 选中列表里的一条：ID 必须相同；Version 非空时条目的 version 字段也必须相同——
// brickkit.yaml 里同一个组件 ID 可以有多行（多版本共存），靠版本区分。部署文件的条目
// 只用 ID（`id@version` 本身就是整个 id）。
type Selector struct{ ID, Version string }

func (s Selector) matches(item *yaml.Node) bool {
	if !isEntry(item, s.ID) {
		return false
	}
	if s.Version == "" {
		return true
	}
	v := mappingValue(item, keyVersion)
	return v != nil && v.Value == s.Version
}

// location 是条目所在的位置：seq 是它所在的序列（顶层或某个外壳的 members），
// parent 是外壳条目（顶层条目时为 nil）。
type location struct {
	seq    *yaml.Node
	index  int
	parent *yaml.Node
}

func (l *location) item() *yaml.Node { return l.seq.Content[l.index] }

// remove 把条目从所在序列里拿掉；外壳的最后一个成员拿掉后 members 键一并去掉。
func (l *location) remove() *yaml.Node {
	item := l.item()
	l.seq.Content = append(l.seq.Content[:l.index], l.seq.Content[l.index+1:]...)
	if l.parent != nil && len(l.seq.Content) == 0 {
		removeKey(l.parent, keyMembers)
	}
	return item
}

// find 按 sel 找条目：先看顶层，再往外壳条目的 members 里找一层（部署文件里外壳的成员是
// 嵌在外壳条目下面的完整条目，只嵌一层；id 在整个文件里唯一，由部署文件校验保证）。
func (e *Edit) find(seqKey string, sel Selector) *location {
	seq := e.sequence(seqKey, false)
	if seq == nil {
		return nil
	}
	for i, item := range seq.Content {
		if sel.matches(item) {
			return &location{seq: seq, index: i}
		}
		if members := mappingValue(item, keyMembers); members != nil && members.Kind == yaml.SequenceNode {
			for j, member := range members.Content {
				if sel.matches(member) {
					return &location{seq: members, index: j, parent: item}
				}
			}
		}
	}
	return nil
}

// topLevel 找顶层的 `id: <id>` 条目及其下标。
func (e *Edit) topLevel(seqKey, id string) (*yaml.Node, *yaml.Node, int) {
	seq := e.sequence(seqKey, false)
	if seq == nil {
		return nil, nil, -1
	}
	for i, item := range seq.Content {
		if isEntry(item, id) {
			return seq, item, i
		}
	}
	return seq, nil, -1
}

// sequence 返回顶层键 seqKey 的序列节点。create 为 true 时在缺失（或写成 null）时创建。
func (e *Edit) sequence(seqKey string, create bool) *yaml.Node {
	for i := 0; i+1 < len(e.root.Content); i += 2 {
		if e.root.Content[i].Value != seqKey {
			continue
		}
		value := e.root.Content[i+1]
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
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	e.root.Content = append(e.root.Content, scalar(seqKey), seq)
	return seq
}

func isEntry(item *yaml.Node, id string) bool {
	if item.Kind != yaml.MappingNode {
		return false
	}
	v := mappingValue(item, keyID)
	return v != nil && v.Value == id
}

func scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }

// mappingValue 取映射节点中某个键的值节点，不存在时返回 nil。
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// topLevelKeyRe 匹配顶层键（顶格、无缩进）。
var topLevelKeyRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*):`)

// restoreBlankLines 按原文把顶层块之间的空行补回编码结果。
//
// yaml.v3 的节点模型里没有空行，往返一次就会把顶层块之间的留白全部压掉。
// 这里只做一件事：原文中某个顶层键前面有空行，编码结果里也补上一行空行
// （有头部注释时补在注释块之前）。
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

// writeAtomic 先写同目录下的临时文件再改名覆盖：写到一半失败（磁盘满、被打断）时，原文件
// 一个字节都不动——brickkit.yaml 与部署文件是使用者手写的，写坏一半比写不进去糟得多。
//
// path 是符号链接时写到它指向的文件（临时文件也建在那个目录里），链接本身不动——改名覆盖
// 链接会把它换成一份普通文件，悄悄与它指向的共享配置分叉。保留原文件的权限与属主；
// 原文件不存在时用 editFilePerm。改名前先落盘，免得断电后留下一个空文件。
func writeAtomic(path string, data []byte) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	perm := os.FileMode(editFilePerm)
	info, statErr := os.Stat(path)
	if statErr == nil {
		perm = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // 改名成功后它已不存在，删除是空操作
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	if statErr == nil {
		keepOwner(name, info)
	}
	return os.Rename(name, path)
}
