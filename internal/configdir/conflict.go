package configdir

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// 冲突标记：升级时 brickkit 把同一个键写两遍，每行行尾注释带
// "brickkit:conflict <side> <version>"。YAML 解析器遇到重复键会拒绝，所以冲突不可能被忽略；
// 这里认出标记，是为了把冷冰冰的"mapping key already defined"换成能直接照做的提示。
const (
	ConflictTag  = "brickkit:conflict"
	SideCurrent  = "current"
	SideProposed = "proposed"
)

// ConflictLine 是重复键的其中一行。Side 为空表示这一行不是 brickkit 写的（使用者手滑重复了）。
type ConflictLine struct {
	Line    int
	Value   string
	Side    string
	Version string
}

// ConflictKey 是一个出现了不止一次的键。
type ConflictKey struct {
	Key   string
	Lines []ConflictLine
}

// FileConflict 是一份文件里的全部冲突。
type FileConflict struct {
	Path string
	Keys []ConflictKey
}

// ConflictError 汇总一个或多个文件的冲突，一次全部报出。
type ConflictError struct {
	Files []FileConflict
}

func (e *ConflictError) Error() string { return e.Render().Error() }

// Merge 把另一组冲突并进来。
func (e *ConflictError) Merge(other *ConflictError) {
	if other != nil {
		e.Files = append(e.Files, other.Files...)
	}
}

// Render 把冲突变成给使用者看的错误。
func (e *ConflictError) Render() *clierr.Error {
	err := clierr.New(clierr.CodeConfigConflict, i18n.T(msgid.ConfigdirConflictTitle))
	for _, file := range e.Files {
		for _, key := range file.Keys {
			err = err.WithDetail(i18n.T(msgid.LabelFile), file.Path).
				WithDetail(i18n.T(msgid.LabelConfigKey), key.Key)
			for _, line := range key.Lines {
				err = err.WithDetail(i18n.T(msgid.ConfigdirConflictLineLabel, line.Line), lineText(line))
			}
		}
	}
	edit := i18n.T(msgid.ConfigdirConflictHintEditPlain)
	if e.hasMarker() {
		edit = i18n.T(msgid.ConfigdirConflictHintEditMarked)
	}
	return err.
		WithHint(edit, i18n.T(msgid.ConfigdirConflictHintNoFormat)).
		WithTip(i18n.T(msgid.ConfigdirConflictTipEditor))
}

// hasMarker 报告是否有 brickkit 写的冲突标记（带说明注释）；手滑重复的键没有那段注释可删。
func (e *ConflictError) hasMarker() bool {
	for _, file := range e.Files {
		for _, key := range file.Keys {
			for _, line := range key.Lines {
				if line.Side != "" {
					return true
				}
			}
		}
	}
	return false
}

func lineText(l ConflictLine) string {
	switch l.Side {
	case SideCurrent:
		return i18n.T(msgid.ConfigdirConflictCurrent, l.Value, l.Version)
	case SideProposed:
		return i18n.T(msgid.ConfigdirConflictProposed, l.Value, l.Version)
	default:
		return l.Value
	}
}

// findConflicts 找出顶层映射里出现不止一次的键；没有时返回 nil。
func findConflicts(doc *yaml.Node, source string) *ConflictError {
	lines := map[string][]ConflictLine{}
	var order []string
	for i := 0; i+1 < len(doc.Content); i += 2 {
		keyNode, valueNode := doc.Content[i], doc.Content[i+1]
		if _, seen := lines[keyNode.Value]; !seen {
			order = append(order, keyNode.Value)
		}
		side, version := parseMarker(valueNode.LineComment)
		if side == "" {
			side, version = parseMarker(keyNode.LineComment)
		}
		lines[keyNode.Value] = append(lines[keyNode.Value], ConflictLine{
			Line: keyNode.Line, Value: displayValue(valueNode), Side: side, Version: version,
		})
	}
	var keys []ConflictKey
	for _, key := range order {
		if len(lines[key]) > 1 {
			keys = append(keys, ConflictKey{Key: key, Lines: lines[key]})
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return &ConflictError{Files: []FileConflict{{Path: source, Keys: keys}}}
}

func parseMarker(comment string) (side, version string) {
	i := strings.Index(comment, ConflictTag)
	if i < 0 {
		return "", ""
	}
	fields := strings.Fields(comment[i+len(ConflictTag):])
	if len(fields) < 2 || (fields[0] != SideCurrent && fields[0] != SideProposed) {
		return "", ""
	}
	return fields[0], fields[1]
}

func displayValue(v *yaml.Node) string {
	if v.Kind == yaml.ScalarNode {
		return v.Value
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ScalarYAML 把一个值写成能放在 "KEY: " 后面的单行 YAML。
// 多行字符串写成 JSON 双引号形式（它同时是合法的 YAML 双引号标量），保证一行一个键。
func ScalarYAML(v any) string {
	if s, ok := v.(string); ok && strings.ContainsAny(s, "\n\r") {
		data, _ := json.Marshal(s)
		return string(data)
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimRight(string(data), "\n")
}

// ConflictBlock 生成一处冲突：说明注释 + 同一个键的两行（旧值在前，新建议值在后）。
// currentYAML / proposedYAML 用 ScalarYAML 生成。
func ConflictBlock(key, currentYAML, currentVersion, proposedYAML, proposedVersion string) string {
	var b strings.Builder
	b.WriteString(yamlcomment.Block("", i18n.T(msgid.ConfigdirConflictBlockNote, proposedVersion)))
	fmt.Fprintf(&b, "%s: %s  # %s %s %s\n", key, currentYAML, ConflictTag, SideCurrent, currentVersion)
	fmt.Fprintf(&b, "%s: %s  # %s %s %s\n", key, proposedYAML, ConflictTag, SideProposed, proposedVersion)
	return b.String()
}
