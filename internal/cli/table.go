package cli

// 本文件实现终端框线表格（004 §3.7 的 status 输出）。
//
// 难点只有一个：对齐。中文一个字占两格，ASCII 占一格，按 len() 或按 rune 数
// 算宽度都会让竖线歪掉。
//
// 因此这里**刻意回避宽度不确定的字符**：`●` `○` `◐` 这类符号在 Unicode 里
// 是"东亚宽度：模糊"，同一个字符在不同终端下可能占 1 格也可能占 2 格，
// 任何静态计算都对不齐。表格里只用汉字、全角括号与 ASCII——它们的宽度是确定的。
// 状态标记放在表格**外面**的小节标题里（✅ 运行中（2 个组件）），不参与对齐。

import (
	"strings"
	"unicode"
)

// 框线字符。
const (
	tableTopLeft     = "┌"
	tableTopMid      = "┬"
	tableTopRight    = "┐"
	tableMidLeft     = "├"
	tableMidMid      = "┼"
	tableMidRight    = "┤"
	tableBottomLeft  = "└"
	tableBottomMid   = "┴"
	tableBottomRight = "┘"
	tableHorizontal  = "─"
	tableVertical    = "│"
)

// table 是一张待渲染的表格。
type table struct {
	headers []string
	rows    [][]string
}

func newTable(headers ...string) *table { return &table{headers: headers} }

func (t *table) add(cells ...string) { t.rows = append(t.rows, cells) }

// render 渲染成多行文本（每行都带结尾换行）。
func (t *table) render(indent string) string {
	if len(t.rows) == 0 {
		return ""
	}

	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && displayWidth(cell) > widths[i] {
				widths[i] = displayWidth(cell)
			}
		}
	}

	var b strings.Builder
	b.WriteString(indent + border(widths, tableTopLeft, tableTopMid, tableTopRight))
	b.WriteString(indent + t.line(t.headers, widths))
	b.WriteString(indent + border(widths, tableMidLeft, tableMidMid, tableMidRight))
	for _, row := range t.rows {
		b.WriteString(indent + t.line(row, widths))
	}
	b.WriteString(indent + border(widths, tableBottomLeft, tableBottomMid, tableBottomRight))
	return b.String()
}

// line 渲染一行：`│ 单元格 │ 单元格 │`。
func (t *table) line(cells []string, widths []int) string {
	var b strings.Builder
	b.WriteString(tableVertical)
	for i, width := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		b.WriteString(" " + cell + strings.Repeat(" ", width-displayWidth(cell)) + " ")
		b.WriteString(tableVertical)
	}
	b.WriteString("\n")
	return b.String()
}

// border 渲染一条横线。
func border(widths []int, left, mid, right string) string {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		parts = append(parts, strings.Repeat(tableHorizontal, width+2))
	}
	return left + strings.Join(parts, mid) + right + "\n"
}

// displayWidth 返回一段文本在等宽终端里占几格。
//
// 只区分两类：东亚宽字符占 2 格，其余占 1 格。之所以够用，是因为表格内容
// 被限制在"汉字 + 全角括号 + ASCII"——宽度模糊的符号（● ○ ◐、emoji）
// 一律不进表格（见文件头说明）。
func displayWidth(text string) int {
	width := 0
	for _, r := range text {
		if isWide(r) {
			width += 2
			continue
		}
		width++
	}
	return width
}

// isWide 判断一个字符是否占两格。
//
// 覆盖实际会出现在表格里的区段：CJK 汉字、中日韩标点（含全角括号）、
// 全角字母数字、假名。刻意**不**包含"东亚宽度：模糊"的区段。
func isWide(r rune) bool {
	switch {
	case r < 0x1100:
		return false
	case unicode.Is(unicode.Han, r): // 汉字
		return true
	case r >= 0x3000 && r <= 0x303F: // 中日韩符号与标点（含 、。（））
		return true
	case r >= 0x3040 && r <= 0x30FF: // 假名
		return true
	case r >= 0xFF00 && r <= 0xFF60: // 全角字母数字与标点
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全角货币符号
		return true
	default:
		return false
	}
}
