// Package mdtext 是几处都要用的 Markdown 扫描：代码块围栏、行内代码、链接、二级小节、表格单元格。
// 文档合集生成器（llmsgen）改写链接、lint 的文档检查（doccheck）核对链接与小节，都走这里，
// 免得两份对"什么算代码块"的理解各写一套、慢慢分叉。
package mdtext

import (
	"regexp"
	"sort"
	"strings"
)

// 链接目标出现的三种位置：[文字](目标 "可选标题")、行首的引用式定义 [名]: 目标、HTML 的 href / src。
// 每个正则的第 2 个分组是目标。
var (
	linkForms = []*regexp.Regexp{
		regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`),
		regexp.MustCompile(`^(\s*\[[^\]]+\]:\s*)(\S+)`),
		regexp.MustCompile(`\b(href|src)="([^"]+)"`),
	}
	inlineCode = regexp.MustCompile("`[^`]*`")
)

// fences 是 Markdown 代码块的两种围栏（三个反引号、三个波浪号）。
var fences = []string{"\x60\x60\x60", "~~~"}

// FenceOf 是这一行开头的围栏；不是围栏时为空。
func FenceOf(line string) string {
	t := strings.TrimSpace(line)
	for _, f := range fences {
		if strings.HasPrefix(t, f) {
			return f
		}
	}
	return ""
}

// eachOutside 对代码块之外的每一行调用 fn（i 从 0 起）；围栏行本身与块内的行都不给。
func eachOutside(body string, fn func(i int, line string)) {
	open := "" // 正在其中的代码块的围栏：只有同一种围栏才能把它关上
	for i, line := range strings.Split(body, "\n") {
		if f := FenceOf(line); f != "" && (open == "" || f == open) {
			if open == "" {
				open = f
			} else {
				open = ""
			}
			continue
		}
		if open == "" {
			fn(i, line)
		}
	}
}

// MapLinks 对代码块与行内代码之外的每个链接目标调用 fn，用它的返回值替换目标。
func MapLinks(body string, fn func(target string) string) string {
	lines := strings.Split(body, "\n")
	eachOutside(body, func(i int, line string) { lines[i] = mapLine(line, fn) })
	return strings.Join(lines, "\n")
}

// Link 是一处链接：所在行号（从 1 起）与目标。
type Link struct {
	Line   int
	Target string
}

// Links 列出代码之外的全部链接，按出现顺序。
func Links(body string) []Link {
	var out []Link
	eachOutside(body, func(i int, line string) {
		mapLine(line, func(target string) string {
			out = append(out, Link{Line: i + 1, Target: target})
			return target
		})
	})
	return out
}

func mapLine(line string, fn func(string) string) string {
	codes := inlineCode.FindAllStringIndex(line, -1)
	inCode := func(at int) bool {
		for _, c := range codes {
			if at >= c[0] && at < c[1] {
				return true
			}
		}
		return false
	}
	var spans [][2]int
	for _, re := range linkForms {
		for _, m := range re.FindAllStringSubmatchIndex(line, -1) {
			if !inCode(m[0]) {
				spans = append(spans, [2]int{m[4], m[5]})
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		if sp[0] < last { // 两种写法重叠（不会发生，保险起见只认先出现的那个）
			continue
		}
		b.WriteString(line[last:sp[0]])
		b.WriteString(fn(line[sp[0]:sp[1]]))
		last = sp[1]
	}
	b.WriteString(line[last:])
	return b.String()
}

// Section 是一个二级小节："## " 开头的那一行（代码块外）到下一个二级标题之前。
type Section struct {
	Heading string // 去掉 "## " 与首尾空白
	Line    int    // 标题所在行，从 1 起
	Body    string // 标题之后、下一个二级标题之前的原文（含代码块）
}

// Sections 列出代码块之外的全部二级小节。
func Sections(body string) []Section {
	lines := strings.Split(body, "\n")
	var heads []int
	eachOutside(body, func(i int, line string) {
		if strings.HasPrefix(line, "## ") {
			heads = append(heads, i)
		}
	})
	out := make([]Section, 0, len(heads))
	for k, h := range heads {
		end := len(lines)
		if k+1 < len(heads) {
			end = heads[k+1]
		}
		out = append(out, Section{
			Heading: strings.TrimSpace(strings.TrimPrefix(lines[h], "## ")),
			Line:    h + 1,
			Body:    strings.Join(lines[h+1:end], "\n"),
		})
	}
	return out
}

// Line 是一行正文：行号（从 1 起）与去掉行内代码后的文字。
type Line struct {
	N    int
	Text string
}

// ProseLines 是代码块之外的每一行，行内代码已删掉——查占位词这类"正文里写了什么"用它。
func ProseLines(body string) []Line {
	var out []Line
	eachOutside(body, func(i int, line string) {
		out = append(out, Line{N: i + 1, Text: inlineCode.ReplaceAllString(line, "")})
	})
	return out
}

// InlineCode 是一行里每段行内代码的内容（不含反引号），按出现顺序。
func InlineCode(line string) []string {
	var out []string
	for _, m := range inlineCode.FindAllString(line, -1) {
		out = append(out, strings.Trim(m, "`"))
	}
	return out
}

// TableCells 是表格行的各个单元格（去掉首尾空白）；不是表格行、或是分隔行（|---|）时为 nil。
func TableCells(line string) []string {
	t := strings.TrimSpace(line)
	if len(t) < 2 || !strings.HasPrefix(t, "|") || !strings.HasSuffix(t, "|") {
		return nil
	}
	cells := splitCells(t[1 : len(t)-1])
	sep := true
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
		if strings.Trim(cells[i], ":-") != "" {
			sep = false
		}
	}
	if sep {
		return nil
	}
	return cells
}

// splitCells 按没转义的竖线切开（\| 是单元格里的一个竖线字符，不是分隔符）。
func splitCells(row string) []string {
	var cells []string
	start := 0
	for i := 0; i < len(row); i++ {
		switch {
		case row[i] == '\\' && i+1 < len(row):
			i++
		case row[i] == '|':
			cells = append(cells, row[start:i])
			start = i + 1
		}
	}
	return append(cells, row[start:])
}
