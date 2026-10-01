// Package agentsmd 管 AGENTS.md 里由 CLI 维护的那一段：标记、语言、渲染、骨架、补全。
//
// AGENTS.md 是作者的 AI 导读（各家工具通用的那个文件），平台只占文件末尾一对标记之间的一段：
// 平台规则与组件表。标记之外 CLI 一个字节都不写；标记里记着这段用的语言，谁跑命令都按它渲染，
// 免得两个 CLI 语言不同的同事把提交进 Git 的文件改来改去。
package agentsmd

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/mdtext"
	"github.com/brickkit/brickkit/internal/msgid"
)

const (
	beginPrefix = "<!-- brickkit:managed:begin"
	endMarker   = "<!-- brickkit:managed:end -->"
)

var beginLine = regexp.MustCompile(`^<!-- brickkit:managed:begin lang=(\S+) -->\s*$`)

// ErrNoBlock：文件里没有维护区。ErrMalformed：标记残缺、重复或没写语言，不敢改（具体是 *MalformedError）。
var (
	ErrNoBlock   = errors.New("no brickkit-maintained block")
	ErrMalformed = errors.New("malformed brickkit-maintained block")
)

// MalformedError 说清维护区的标记哪里不对：各有几个、在哪几行（从 1 起）。给人看的说法见 Reason。
type MalformedError struct {
	Begins, Ends int
	// EndFirst：结束标记在开始标记前面。BadLang：开始标记没写合法的 lang=。
	EndFirst, BadLang bool
	Lines             []int
}

func (e *MalformedError) Error() string {
	return fmt.Sprintf("%v: %d begin and %d end markers at lines %v", ErrMalformed, e.Begins, e.Ends, e.Lines)
}

// Is 让 errors.Is(err, ErrMalformed) 认得它。
func (e *MalformedError) Is(target error) bool { return target == ErrMalformed }

// Reason 是 Find 的错误给人看的说法，跟着 CLI 当前的语言。
func Reason(err error) string {
	var m *MalformedError
	switch {
	case errors.As(err, &m):
		lines := make([]string, len(m.Lines))
		for i, n := range m.Lines {
			lines[i] = strconv.Itoa(n)
		}
		at := strings.Join(lines, ", ")
		switch {
		case m.BadLang:
			return i18n.T(msgid.AgentsmdReasonBadLang, at)
		case m.EndFirst:
			return i18n.T(msgid.AgentsmdReasonEndFirst, at)
		default:
			return i18n.T(msgid.AgentsmdReasonMarkerCount, m.Begins, m.Ends, at)
		}
	case errors.Is(err, ErrNoBlock):
		return i18n.T(msgid.AgentsmdReasonNoBlock)
	default:
		return err.Error()
	}
}

// Block 是维护区在文件里的位置（字节偏移，含两端标记与结束标记那一行的换行）与它记的语言。
type Block struct {
	Lang       string
	Start, End int
}

// Find 找到维护区。代码块里的标记不算（作者可能在文档里举例）。
func Find(doc string) (Block, error) {
	lines := strings.SplitAfter(doc, "\n")
	code := mdtext.CodeLines(doc) // 与 SplitAfter 行数相同：都按 "\n" 切
	var begins, ends []int        // 行号（从 0 起）
	for i, l := range lines {
		if code[i] {
			continue
		}
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, beginPrefix):
			begins = append(begins, i)
		case t == endMarker:
			ends = append(ends, i)
		}
	}
	at := markerLines(begins, ends)
	switch {
	case len(begins) == 0 && len(ends) == 0:
		return Block{}, ErrNoBlock
	case len(begins) != 1 || len(ends) != 1:
		return Block{}, &MalformedError{Begins: len(begins), Ends: len(ends), Lines: at}
	case ends[0] < begins[0]:
		return Block{}, &MalformedError{Begins: 1, Ends: 1, EndFirst: true, Lines: at}
	}
	m := beginLine.FindStringSubmatch(strings.TrimSpace(lines[begins[0]]))
	if m == nil || !docspec.ValidLang(m[1]) {
		return Block{}, &MalformedError{Begins: 1, Ends: 1, BadLang: true, Lines: at}
	}
	start := 0
	for _, l := range lines[:begins[0]] {
		start += len(l)
	}
	end := start
	for _, l := range lines[begins[0] : ends[0]+1] {
		end += len(l)
	}
	return Block{Lang: m[1], Start: start, End: end}, nil
}

// Replace 用 rendered 换掉维护区，其余字节原样保留。
func Replace(doc string, b Block, rendered string) string {
	return doc[:b.Start] + rendered + doc[b.End:]
}

// Append 把 rendered 接在文件末尾，前面空一行。
func Append(doc, rendered string) string {
	switch {
	case doc == "":
		return rendered
	case strings.HasSuffix(doc, "\n\n"):
		return doc + rendered
	case strings.HasSuffix(doc, "\n"):
		return doc + "\n" + rendered
	default:
		return doc + "\n\n" + rendered
	}
}

// ParseRows 读出现有维护区里组件表的行（键 id@version，单元格已去掉转义）。缓存里缺某个组件时，
// 新渲染的那一行沿用这里的单元格：新克隆的仓库还没取过组件，不能因此把已经写好的说明抹成"—"。
func ParseRows(doc string, b Block) map[string]Row {
	rows := map[string]Row{}
	for _, l := range strings.Split(doc[b.Start:b.End], "\n") {
		c := mdtext.TableCells(l)
		if len(c) != 5 || !strings.Contains(c[0], "/") {
			continue
		}
		for i := range c {
			c[i] = strings.ReplaceAll(c[i], `\|`, "|")
		}
		rows[c[0]+"@"+c[1]] = Row{ID: c[0], Version: c[1], Does: c[2], Docs: c[3], Home: c[4]}
	}
	return rows
}

// markerLines 是全部标记所在的行号（从 1 起，排好序）。
func markerLines(begins, ends []int) []int {
	var out []int
	for _, i := range append(append([]int(nil), begins...), ends...) {
		out = append(out, i+1)
	}
	sort.Ints(out)
	return out
}
