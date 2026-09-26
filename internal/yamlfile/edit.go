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
	seq := e.sequence(seqKey, false)
	if seq == nil {
		return false
	}
	for i, item := range seq.Content {
		if isEntry(item, id) {
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			return true
		}
		members := mappingValue(item, keyMembers)
		if members == nil || members.Kind != yaml.SequenceNode {
			continue
		}
		for j, member := range members.Content {
			if isEntry(member, id) {
				members.Content = append(members.Content[:j], members.Content[j+1:]...)
				if len(members.Content) == 0 {
					removeKey(item, keyMembers)
				}
				return true
			}
		}
	}
	return false
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
	seq := e.sequence(seqKey, false)
	if seq == nil {
		return nil
	}
	for _, item := range seq.Content {
		if isEntry(item, id) {
			return item
		}
		// 部署文件里外壳的成员是嵌在外壳条目 members 下面的完整条目（只嵌一层）；
		// id 在整个文件里唯一（部署文件校验保证），往下找一层不会找错
		if members := mappingValue(item, keyMembers); members != nil && members.Kind == yaml.SequenceNode {
			for _, member := range members.Content {
				if isEntry(member, id) {
					return member
				}
			}
		}
	}
	return nil
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
