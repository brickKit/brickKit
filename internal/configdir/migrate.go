package configdir

// 本文件是配置迁移（提案 §12.2、§12.3，附录 A4）：一份旧版本的配置文件 + 新旧 configSchema，
// 得到新版本的配置文件。upgrade 与"从归档恢复"（提案 §7.7）都用它。
//
// 只做键集合的机械对比，不猜重命名、不做类型转换：
//
//	使用者写过、新版本还有    原文照抄（$var:、${}、file://、引号一个字符都不动；写在它上方的注释跟着走）
//	使用者写过、新版本没了    不写进新文件（值留在归档里），报告里列出来
//	使用者没写（骨架里是注释）新版本的骨架行——跟随新默认值，附录 A4 让它不会有假冲突
//	新版本新增的键            骨架行（必填且无默认写 KEY: ""，up 会拦住）
//
// 冲突：使用者写过的键、开发者改了默认值、而使用者的值不等于旧默认值（等于的话能确认他没改过，
// 跟随新默认值，提案 §12.3 第一行）。怎么处理由 Choose 决定：两行重复键（大声失败，提案 §12.3）、保留使用者
// 的值、或者用新默认值。

import (
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/yamlcomment"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// Conflict 是一处"使用者写过、默认值也变了"的冲突。值都是单行 YAML 写法。
type Conflict struct {
	Key, UserYAML, OldDefault, NewDefault string
}

// Choice 是一处冲突的处理方式。
type Choice int

const (
	// ChooseDuplicate 写两行重复键和说明注释：不解决就启动不了（非交互时的兜底，附录 A4）。
	ChooseDuplicate Choice = iota
	// ChooseMine 保留使用者的值。
	ChooseMine
	// ChooseNew 用新版本的默认值（写成跟随默认的骨架行）。
	ChooseNew
)

// MigrateInput 是一次迁移的输入。
type MigrateInput struct {
	ID, FromVersion, ToVersion string
	// Old 是旧配置文件的原文；没有旧文件时为空。
	Old []byte
	// OldSchema 为 nil 表示不知道旧版本的 configSchema（归档恢复、缓存里没有它）：
	// 文件里的键都当作使用者写的，照抄，不判冲突。
	OldSchema, NewSchema *manifest.ConfigSchema
	// VarRefs 是新写的骨架行里要写成 $var: 引用的键（同 add 的提示）。
	VarRefs map[string]string
	// Choose 决定每一处冲突怎么处理。
	Choose func(Conflict) Choice
}

// MigrateReport 说清楚迁移做了什么（给 upgrade 的输出与 --dry-run 用）。
type MigrateReport struct {
	// Copied 是原样抄过去的键，Added 是新版本新增的键，Dropped 是新版本没有了的键，
	// Followed 是值等于旧默认值、于是跟随新默认值的键。
	Copied, Added, Dropped, Followed []string
	// Conflicts 是全部冲突；Resolved 是每一处冲突最后怎么处理的。
	Conflicts []Conflict
	Resolved  map[string]Choice
}

// writtenKey 是旧文件里使用者写下的一个键：原文（整段，块标量的续行也在内）、写在它上方的
// 注释（骨架自己生成的分节标题不算），与解析出的值。
type writtenKey struct {
	raw     string
	comment string
	value   any
}

// withComment 是这个键在新文件里的写法：使用者写在它上方的注释，接着 line。
func (w writtenKey) withComment(line string) string { return w.comment + line }

// Migrate 把旧配置文件迁移到新版本的 configSchema。
func Migrate(in MigrateInput) ([]byte, MigrateReport, error) {
	report := MigrateReport{Resolved: map[string]Choice{}}
	written, order, comments, err := readWritten(in.Old, schemaKeys(in.OldSchema, in.NewSchema))
	if err != nil {
		return nil, report, err
	}
	newProps := map[string]manifest.ConfigProperty{}
	if in.NewSchema != nil {
		newProps = in.NewSchema.Properties
	}
	for _, key := range order {
		if _, ok := newProps[key]; !ok {
			report.Dropped = append(report.Dropped, key)
		}
	}

	out := renderSkeletonWith(in.ID, in.ToVersion, in.NewSchema, comments.file, func(key string, prop manifest.ConfigProperty, required bool) string {
		skeleton := skeletonLine(key, prop, in.VarRefs[key], required)
		w, wrote := written[key]
		oldProp, known := schemaProp(in.OldSchema, key)
		// 旧版本必填、没默认值、还没填的占位 KEY: ""：不是使用者写下的值
		if wrote && known && isUnfilledPlaceholder(w.value, oldProp) {
			wrote = false
		}
		if !wrote {
			if in.OldSchema != nil && !known {
				report.Added = append(report.Added, key)
			}
			return comments.byKey[key] + skeleton
		}
		if !known || sameValue(oldProp.Default, prop.Default) {
			report.Copied = append(report.Copied, key)
			return w.withComment(w.raw)
		}
		if oldProp.Default != nil && sameValue(w.value, oldProp.Default) {
			report.Followed = append(report.Followed, key)
			return w.withComment(skeleton)
		}
		c := Conflict{Key: key, UserYAML: oneLine(w.value), OldDefault: yamlOrEmpty(oldProp.Default), NewDefault: yamlOrEmpty(prop.Default)}
		report.Conflicts = append(report.Conflicts, c)
		choice := ChooseDuplicate
		if in.Choose != nil {
			choice = in.Choose(c)
		}
		report.Resolved[key] = choice
		switch choice {
		case ChooseMine:
			return w.withComment(w.raw)
		case ChooseNew:
			return w.withComment(skeleton)
		default:
			return w.withComment(ConflictBlock(key, c.UserYAML, in.FromVersion, c.NewDefault, in.ToVersion))
		}
	})
	if comments.trailer != "" {
		out = append(out, []byte("\n"+comments.trailer)...)
	}
	return out, report, nil
}

// oldComments 是旧文件里使用者写的注释，按它说的对象分好：文件开头的一段（放回新文件头之后）、
// 每个键上方的（键名 → 注释，那个键在旧文件里写了值还是只是一条注释掉的骨架行都一样）、
// 最后一个键之后的（放回新文件末尾）。每段每行带换行，没有就是空串。
type oldComments struct {
	file    string
	byKey   map[string]string
	trailer string
}

// readWritten 读出旧文件里使用者写下的键（注释掉的骨架行不算）：每个键的原文取自源码行，
// 从它那一行到下一个键之前（去掉尾部的空行与整行注释）——块标量的续行因此一起带上。
// 使用者写的注释另由 scanComments 按行归属。
//
// keys 是新旧 configSchema 里的全部键：骨架里注释掉的"# KEY: …"行不是使用者的注释。
func readWritten(old []byte, keys map[string]bool) (written map[string]writtenKey, order []string, comments oldComments, err error) {
	written = map[string]writtenKey{}
	comments = oldComments{byKey: map[string]string{}}
	if len(strings.TrimSpace(string(old))) == 0 {
		return written, nil, comments, nil
	}
	doc, err := yamlfile.Document(old, "", true)
	if err != nil {
		return nil, nil, comments, err
	}
	if doc == nil {
		return written, nil, comments, nil
	}
	if conflict := findConflicts(doc, ""); conflict != nil {
		return nil, nil, comments, conflict
	}
	lines := strings.Split(string(old), "\n")
	spans := map[int]keySpan{}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		keyNode, valueNode := doc.Content[i], doc.Content[i+1]
		end := len(lines)
		if i+2 < len(doc.Content) {
			end = doc.Content[i+2].Line - 1
		}
		var value any
		if err := valueNode.Decode(&value); err != nil {
			return nil, nil, comments, err
		}
		raw := keyRaw(lines[keyNode.Line-1:end], keyNode.Value, value)
		written[keyNode.Value] = writtenKey{raw: raw, value: value}
		spans[keyNode.Line-1] = keySpan{key: keyNode.Value, lines: strings.Count(raw, "\n")}
		order = append(order, keyNode.Value)
	}
	comments = scanComments(lines, spans, keys)
	for key, c := range comments.byKey {
		if w, ok := written[key]; ok {
			w.comment = c
			written[key] = w
		}
	}
	return written, order, comments, nil
}

// keySpan 是旧文件里一个写了值的键：从哪一行起、占几行。
type keySpan struct {
	key   string
	lines int
}

// scanComments 逐行把使用者的注释归给它下面的那一项：写了值的键，或者一条注释掉的骨架行。
// 生成的文件头、说明、分节标题不是使用者的注释。第一项之前、与它隔着空行或生成行的注释是
// 文件开头的注释；最后一项之后的是文件末尾的注释。
func scanComments(lines []string, spans map[int]keySpan, keys map[string]bool) oldComments {
	out := oldComments{byKey: map[string]string{}}
	var pending []string
	detached := false // pending 与下面的东西之间隔着空行或生成行
	seenItem := false
	flushToFile := func() {
		if len(pending) > 0 {
			out.file += strings.Join(pending, "\n") + "\n"
			pending = nil
		}
	}
	attach := func(key string) {
		if !seenItem && detached {
			flushToFile()
		}
		if len(pending) > 0 {
			out.byKey[key] += strings.Join(pending, "\n") + "\n"
			pending = nil
		}
		detached, seenItem = false, true
	}
	header := generatedHeaderLines(lines, keys)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if span, ok := spans[i]; ok {
			attach(span.key)
			i += span.lines - 1
			continue
		}
		switch {
		case trimmed == "":
			if len(pending) > 0 {
				detached = true
			}
		case header[i] || yamlcomment.IsSection(line):
			if len(pending) > 0 {
				detached = true
			}
		case isSkeletonKeyLine(line, keys):
			key, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "# "), ":")
			attach(key)
		case strings.HasPrefix(trimmed, "#"):
			if detached && !seenItem {
				flushToFile()
			}
			detached = false
			pending = append(pending, line)
		}
	}
	if len(pending) > 0 {
		if seenItem {
			out.trailer = strings.Join(pending, "\n") + "\n"
		} else {
			flushToFile()
		}
	}
	return out
}

// keyRaw 是一个键的原文：去掉尾部的空行与整行注释（它们属于下一个键，或者只是排版）——
// 除非去掉之后值变了。保留式块标量（|+、>+）末尾的空行是值的一部分：那时逐行补回，
// 直到这段原文解析出来的值与原值一字不差。
func keyRaw(chunk []string, key string, value any) string {
	trimmed := len(chunk)
	for trimmed > 1 {
		last := strings.TrimSpace(chunk[trimmed-1])
		if last != "" && !strings.HasPrefix(last, "#") {
			break
		}
		trimmed--
	}
	for n := trimmed; n <= len(chunk); n++ {
		raw := strings.Join(chunk[:n], "\n") + "\n"
		var parsed map[string]any
		if yaml.Unmarshal([]byte(raw), &parsed) == nil && reflect.DeepEqual(parsed[key], value) {
			return raw
		}
	}
	return strings.Join(chunk, "\n") + "\n"
}

// isSkeletonKeyLine：这一行是骨架里注释掉的键行——"# KEY: 默认值  # 类型 | 说明"或"# KEY:  # 类型 | 说明"，
// KEY 是 configSchema 里的键，行尾的说明以一个 configSchema 类型开头。使用者自己写的"# KEY: 说明"没有那段行尾，
// 是注释，不是骨架行。
func isSkeletonKeyLine(line string, keys map[string]bool) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "# ")
	if !ok {
		return false
	}
	key, after, ok := strings.Cut(rest, ":")
	if !ok || !keys[key] {
		return false
	}
	idx := strings.LastIndex(after, "  # ")
	if idx < 0 {
		return false
	}
	desc := after[idx+len("  # "):]
	kind, _, _ := strings.Cut(desc, " ")
	return manifest.ValidConfigType(kind)
}

// schemaKeys 是几份 configSchema 里出现过的全部键。
func schemaKeys(schemas ...*manifest.ConfigSchema) map[string]bool {
	keys := map[string]bool{}
	for _, s := range schemas {
		if s == nil {
			continue
		}
		for key := range s.Properties {
			keys[key] = true
		}
	}
	return keys
}

// generatedHeaderLines 标出骨架生成的文件头：HeaderPrefix 那一行，加上紧跟在它下面、
// 一直到第一个空行（或第一个键）的注释行——说明文字有几行由当时的语言决定，所以按结构认，
// 不拿文字去比：译文改过、或文件是另一种语言写的，都认得出来。
func generatedHeaderLines(lines []string, keys map[string]bool) map[int]bool {
	out := map[int]bool{}
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), strings.TrimSpace(HeaderPrefix)) {
			continue
		}
		out[i] = true
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if !strings.HasPrefix(next, "#") || isSkeletonKeyLine(lines[i+1], keys) {
				break
			}
			i++
			out[i] = true
		}
	}
	return out
}

func schemaProp(s *manifest.ConfigSchema, key string) (manifest.ConfigProperty, bool) {
	if s == nil {
		return manifest.ConfigProperty{}, false
	}
	p, ok := s.Properties[key]
	return p, ok
}

// sameValue 比较两个值（YAML 解码出来的 int 与 JSON 解码出来的 float64 视为同一个数）。
func sameValue(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return ScalarYAML(normalize(a)) == ScalarYAML(normalize(b))
}

// normalize 把数值统一成 float64，其余原样。
func normalize(v any) any {
	var out any
	data, err := yaml.Marshal(v)
	if err != nil || yaml.Unmarshal(data, &out) != nil {
		return v
	}
	switch n := out.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	}
	return out
}

func yamlOrEmpty(v any) string {
	if v == nil {
		return `""`
	}
	return oneLine(v)
}

// oneLine 把值写成能放在 "KEY: " 后面的一行：标量走 YAML，列表 / 映射写 JSON 流式写法
// （它同时是合法的 YAML）——冲突块的两行重复键必须各占一行，才认得出是冲突。
func oneLine(v any) string { return defaultText(v) }

// isUnfilledPlaceholder：骨架给必填、无默认值的键写的 KEY: ""，使用者还没填。
func isUnfilledPlaceholder(v any, prop manifest.ConfigProperty) bool {
	s, ok := v.(string)
	return ok && s == "" && prop.Default == nil
}
