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
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/mdtext"
)

const (
	beginPrefix = "<!-- brickkit:managed:begin"
	endMarker   = "<!-- brickkit:managed:end -->"
)

var beginLine = regexp.MustCompile(`^<!-- brickkit:managed:begin lang=(\S+) -->\s*$`)

// ErrNoBlock：文件里没有维护区。ErrMalformed：标记残缺、重复或没写语言，不敢改。
var (
	ErrNoBlock   = errors.New("no brickkit-maintained block")
	ErrMalformed = errors.New("malformed brickkit-maintained block")
)

// Block 是维护区在文件里的位置（字节偏移，含两端标记与结束标记那一行的换行）与它记的语言。
type Block struct {
	Lang       string
	Start, End int
}

// Find 找到维护区。代码块里的标记不算（作者可能在文档里举例）。
func Find(doc string) (Block, error) {
	lines := strings.SplitAfter(doc, "\n")
	var begins, ends []int // 行号（从 0 起）
	open := ""             // 正在其中的代码块的围栏
	for i, l := range lines {
		if f := mdtext.FenceOf(l); f != "" && (open == "" || f == open) {
			if open == "" {
				open = f
			} else {
				open = ""
			}
			continue
		}
		if open != "" {
			continue
		}
		t := strings.TrimRight(l, "\r\n")
		switch {
		case strings.HasPrefix(t, beginPrefix):
			begins = append(begins, i)
		case strings.TrimSpace(t) == endMarker:
			ends = append(ends, i)
		}
	}
	switch {
	case len(begins) == 0 && len(ends) == 0:
		return Block{}, ErrNoBlock
	case len(begins) != 1 || len(ends) != 1:
		return Block{}, fmt.Errorf("%w: %d begin and %d end markers", ErrMalformed, len(begins), len(ends))
	case ends[0] < begins[0]:
		return Block{}, fmt.Errorf("%w: the end marker comes before the begin marker", ErrMalformed)
	}
	m := beginLine.FindStringSubmatch(strings.TrimRight(lines[begins[0]], "\r\n"))
	if m == nil || !docspec.ValidLang(m[1]) {
		return Block{}, fmt.Errorf("%w: the begin marker has no valid lang=", ErrMalformed)
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
