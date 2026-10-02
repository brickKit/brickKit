package yamlfile

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// keyRequiredBy 是 brickkit.yaml 里兼容版本那一行的键：有它的不是默认版本。
const keyRequiredBy = "requiredBy"

// EntryKey 是一个组件条目排序时看的三样东西，原样取自条目的字段。
type EntryKey struct {
	// ID 是 id 字段；部署文件里可能带版本（id@version）。
	ID string
	// Version 是 version 字段；只有 brickkit.yaml 的条目有。
	Version string
	// RequiredBy 表示条目写了非空的 requiredBy。
	RequiredBy bool
}

// SortEntries 把序列 seqKey 的条目按 less 排好，外壳条目下面的 members 各自也排好。
//
// 稳定排序，条目整个搬：字段、行尾注释、写在条目上方的注释都跟着它走。只有一处例外——
// 列表最后一条下方的注释留在列表末尾：那多半是"还可以再加什么"的说明，不属于恰好排在最后的那一条。
// 序列里有不是映射的条目（手写坏了）时原样不动，交给校验去报。
func (e *Edit) SortEntries(seqKey string, less func(a, b EntryKey) bool) {
	seq := e.sequence(seqKey, false)
	if seq == nil {
		return
	}
	sortSequence(seq, less)
	for _, item := range seq.Content {
		if members := mappingValue(item, keyMembers); members != nil && members.Kind == yaml.SequenceNode {
			sortSequence(members, less)
		}
	}
}

func sortSequence(seq *yaml.Node, less func(a, b EntryKey) bool) {
	n := len(seq.Content)
	if n < 2 {
		return
	}
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			return
		}
	}
	last := seq.Content[n-1]
	sort.SliceStable(seq.Content, func(i, j int) bool {
		return less(entryKey(seq.Content[i]), entryKey(seq.Content[j]))
	})
	if now := seq.Content[n-1]; now != last {
		moveTrailingComment(last, now)
	}
}

func entryKey(item *yaml.Node) EntryKey {
	var k EntryKey
	if v := mappingValue(item, keyID); v != nil {
		k.ID = v.Value
	}
	if v := mappingValue(item, keyVersion); v != nil {
		k.Version = v.Value
	}
	if v := mappingValue(item, keyRequiredBy); v != nil {
		k.RequiredBy = len(v.Content) > 0
	}
	return k
}

// moveTrailingComment 把 from 末尾下方的注释挪到 to 的末尾。
func moveTrailingComment(from, to *yaml.Node) {
	holder := trailingCommentHolder(from)
	if holder == nil || len(to.Content) < 2 {
		return
	}
	comment := holder.FootComment
	holder.FootComment = ""
	key := to.Content[len(to.Content)-2]
	key.FootComment = joinComments(key.FootComment, comment)
}

// trailingCommentHolder 找到挂着"节点末尾下方那段注释"的节点。yaml.v3 把它挂在最后一个键上；
// 最后一个值本身是映射或序列时，挂在更里面的最后一个键（或最后一个元素）上。没有时返回 nil。
func trailingCommentHolder(n *yaml.Node) *yaml.Node {
	switch n.Kind {
	case yaml.MappingNode:
		if len(n.Content) < 2 {
			return nil
		}
		key, value := n.Content[len(n.Content)-2], n.Content[len(n.Content)-1]
		if key.FootComment != "" {
			return key
		}
		return trailingCommentHolder(value)
	case yaml.SequenceNode:
		if len(n.Content) == 0 {
			return nil
		}
		last := n.Content[len(n.Content)-1]
		if last.FootComment != "" {
			return last
		}
		return trailingCommentHolder(last)
	}
	return nil
}
