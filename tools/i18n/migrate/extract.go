package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Edit 是对源文件的一次替换：字节区间 [Start, End) 换成 Text。
// Text 里的 {{T}} 稍后换成 i18n.T(...) 调用，{{LAST}} 换成被包装的最后一个参数。
type Edit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// Entry 是一处待迁移的中文文案（一条 `+` 拼接链，或一个 Printf 格式串）。
type Entry struct {
	ID        int      `json:"id"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Ctx       string   `json:"ctx"`    // 所处上下文：format:Printf / arg:WithDetail / value / …
	Callee    string   `json:"callee"` // 所在调用的函数表达式
	ZH        string   `json:"zh"`     // 目录里该写的中文文案（动词已改成位置动词）
	Args      []string `json:"args"`   // 传给 i18n.T 的参数源码
	Edits     []Edit   `json:"edits"`
	Manual    string   `json:"manual"` // 非空表示工具不敢自动改，要人来处理（原因写在这里）
	Field     string   `json:"field"`  // 所在结构体字段名（Short / Long / Example 用来取名）
	ArgRanges [][2]int `json:"argRanges"`
	LastRange [2]int   `json:"lastRange"`
}

// formatCallees 是"第一个（或指定位置的）参数是格式串"的调用，值是格式串的下标。
var formatCallees = map[string]int{
	"Printf": 0, "Sprintf": 0, "Fprintf": 1, "Errorf": 0, "Newf": 1, "WithDetailf": 1, "Addf": 1,
}

// extractFiles 扫描这些 Go 文件，返回所有带中文的字符串字面量（拼接链合并为一条）。
func extractFiles(files []string) ([]Entry, error) {
	var entries []Entry
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return nil, err
		}
		x := &extractor{path: path, fset: fset, src: src, seen: map[token.Pos]bool{}, nextID: len(entries)}
		x.visit(f, nil)
		entries = append(entries, x.entries...)
	}
	return entries, nil
}

type extractor struct {
	path    string
	fset    *token.FileSet
	src     []byte
	seen    map[token.Pos]bool
	entries []Entry
	nextID  int
}

func (x *extractor) text(n ast.Node) string {
	return string(x.src[x.off(n.Pos()):x.off(n.End())])
}

func (x *extractor) off(p token.Pos) int { return x.fset.Position(p).Offset }

// visit 深度优先遍历，parents 是从根到 n 的祖先链。
func (x *extractor) visit(n ast.Node, parents []ast.Node) {
	if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if val, err := strconv.Unquote(lit.Value); err == nil && isCJK(val) {
			top, rest := chainTop(lit, parents)
			if !x.seen[top.Pos()] {
				x.seen[top.Pos()] = true
				x.nextID++
				x.entries = append(x.entries, x.buildEntry(x.nextID, top, rest))
			}
		}
	}
	children := append(parents, n)
	ast.Inspect(n, func(c ast.Node) bool {
		if c == nil || c == n {
			return true
		}
		x.visit(c, children)
		return false
	})
}

// chainTop 从字符串字面量往外爬到最大的 `+` 拼接链（穿过括号），返回链顶与链顶的祖先链。
func chainTop(lit ast.Node, parents []ast.Node) (ast.Node, []ast.Node) {
	top := lit
	i := len(parents) - 1
	for i >= 0 {
		switch p := parents[i].(type) {
		case *ast.BinaryExpr:
			if p.Op == token.ADD {
				top = p
				i--
				continue
			}
		case *ast.ParenExpr:
			top = p
			i--
			continue
		}
		break
	}
	return top, parents[:i+1]
}

// operand 是拼接链里的一个操作数：字符串字面量，或者一个运行时表达式。
type operand struct {
	lit  bool
	val  string // lit 时的字符串值
	expr string // 非 lit 时的表达式源码
	r    [2]int // 非 lit 时表达式在文件里的字节区间
}

func (x *extractor) flatten(n ast.Node, ops *[]operand) {
	switch v := n.(type) {
	case *ast.ParenExpr:
		x.flatten(v.X, ops)
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			x.flatten(v.X, ops)
			x.flatten(v.Y, ops)
			return
		}
		*ops = append(*ops, x.exprOperand(v))
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			s, _ := strconv.Unquote(v.Value)
			*ops = append(*ops, operand{lit: true, val: s})
			return
		}
		*ops = append(*ops, x.exprOperand(v))
	default:
		*ops = append(*ops, x.exprOperand(n))
	}
}

func (x *extractor) exprOperand(n ast.Node) operand {
	return operand{expr: x.text(n), r: [2]int{x.off(n.Pos()), x.off(n.End())}}
}

func (x *extractor) buildEntry(id int, top ast.Node, parents []ast.Node) Entry {
	e := Entry{ID: id, File: x.path, Line: x.fset.Position(top.Pos()).Line}

	var ops []operand
	x.flatten(top, &ops)
	hasExpr := false
	var lits strings.Builder
	for _, o := range ops {
		if o.lit {
			lits.WriteString(o.val)
		} else {
			hasExpr = true
		}
	}
	e.ZH = lits.String()

	var parent ast.Node
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	}
	inFunc := false
	for _, p := range parents {
		switch p.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			inFunc = true
		}
		// const 里不能调函数：i18n.T 不是常量表达式
		if g, ok := p.(*ast.GenDecl); ok && g.Tok == token.CONST {
			e.Manual = "const"
		}
	}

	switch p := parent.(type) {
	case *ast.CallExpr:
		argIdx := -1
		for i, a := range p.Args {
			if a == top {
				argIdx = i
			}
		}
		name := calleeName(p.Fun)
		e.Callee = x.text(p.Fun)
		if fi, isFmt := formatCallees[name]; isFmt && argIdx == fi {
			x.formatEntry(&e, p, top, name, fi, hasExpr)
			return e
		}
		e.Ctx = "arg:" + name
	case *ast.CaseClause:
		e.Ctx = "case"
		e.Manual = firstNonEmpty(e.Manual, "case-clause")
	case *ast.BinaryExpr:
		e.Ctx = "compare"
		e.Manual = firstNonEmpty(e.Manual, "compare")
	case *ast.KeyValueExpr:
		if p.Key == top {
			e.Manual = firstNonEmpty(e.Manual, "map-key")
		}
		if id, ok := p.Key.(*ast.Ident); ok {
			e.Field = id.Name
		}
		e.Ctx = "value"
	case *ast.ValueSpec:
		e.Ctx = "value"
		if !inFunc {
			e.Manual = firstNonEmpty(e.Manual, "package-level")
		}
	default:
		e.Ctx = "value"
	}
	if e.Manual == "" && !inFunc {
		e.Manual = "package-level"
	}

	if e.Manual == "" {
		x.plainEntry(&e, top, ops, hasExpr)
	}
	return e
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}

// plainEntry 处理"整条拼接链换成一次 i18n.T 调用"的普通情形。
// 运行时表达式操作数依次变成 %[1]s、%[2]s……并作为参数传给 i18n.T。
func (x *extractor) plainEntry(e *Entry, top ast.Node, ops []operand, hasExpr bool) {
	var sb strings.Builder
	var args []string
	for _, o := range ops {
		if o.lit {
			v := o.val
			if hasExpr {
				v = strings.ReplaceAll(v, "%", "%%")
			}
			sb.WriteString(v)
			continue
		}
		args = append(args, o.expr)
		e.ArgRanges = append(e.ArgRanges, o.r)
		sb.WriteString("%[" + strconv.Itoa(len(args)) + "]s")
	}
	e.ZH, e.Args = sb.String(), args

	// 只有纯字面量才把结尾的换行挪到目录外面（Printf 之外的场景），
	// 让目录文案不带换行、调用点保留 + "\n"。
	suffix := ""
	if !hasExpr {
		if t := strings.TrimRight(e.ZH, "\n"); t != e.ZH && t != "" {
			suffix = " + " + strconv.Quote(e.ZH[len(t):])
			e.ZH = t
		}
	}
	e.Edits = []Edit{{x.off(top.Pos()), x.off(top.End()), "{{T}}" + suffix}}
}

// formatEntry 处理"这条文案是 Printf 一类调用的格式串"的情形。
func (x *extractor) formatEntry(e *Entry, call *ast.CallExpr, top ast.Node, name string, fi int, hasExpr bool) {
	e.Ctx = "format:" + name
	if hasExpr {
		e.Manual = firstNonEmpty(e.Manual, "format-with-expr")
		return
	}
	if call.Ellipsis.IsValid() {
		e.Manual = firstNonEmpty(e.Manual, "ellipsis")
		return
	}
	fargs := call.Args[fi+1:]
	raw := e.ZH

	// Printf / Fprintf：头尾的换行留在调用点的格式串里，目录文案不带换行
	lead, trail := "", ""
	if name == "Printf" || name == "Fprintf" {
		t := strings.TrimLeft(raw, "\n")
		lead = raw[:len(raw)-len(t)]
		t2 := strings.TrimRight(t, "\n")
		trail = t[len(t2):]
		raw = t2
	}
	pos, count, ok := positional(raw)
	if !ok || count != len(fargs) {
		e.Manual = firstNonEmpty(e.Manual, fmt.Sprintf("verbs=%d args=%d ok=%v", count, len(fargs), ok))
		return
	}
	var argTexts []string
	for _, a := range fargs {
		argTexts = append(argTexts, x.text(a))
		e.ArgRanges = append(e.ArgRanges, [2]int{x.off(a.Pos()), x.off(a.End())})
	}
	e.Args = argTexts
	if count == 0 {
		pos = strings.ReplaceAll(raw, "%%", "%") // 零参数时目录文案不经过 Sprintf，%% 要还原成 %
	}
	e.ZH = pos

	lastEnd := x.off(top.End())
	if len(fargs) > 0 {
		lastEnd = x.off(fargs[len(fargs)-1].End())
	}
	switch name {
	case "Printf", "Fprintf":
		e.Edits = []Edit{{x.off(top.Pos()), lastEnd, strconv.Quote(lead+"%s"+trail) + ", {{T}}"}}
	case "Sprintf":
		e.Edits = []Edit{{x.off(call.Pos()), x.off(call.End()), "{{T}}"}}
	case "Errorf":
		x.errorfEntry(e, call, raw, argTexts)
	case "Newf", "WithDetailf", "Addf":
		// Newf → New、WithDetailf → WithDetail：格式串和参数并进 i18n.T
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			e.Manual = firstNonEmpty(e.Manual, "f-call-not-selector")
			return
		}
		e.Edits = []Edit{
			{x.off(sel.Sel.Pos()), x.off(sel.Sel.End()), strings.TrimSuffix(sel.Sel.Name, "f")},
			{x.off(top.Pos()), lastEnd, "{{T}}"},
		}
	}
}

// errorfEntry 处理 fmt.Errorf：没有 %w 的换成 errors.New(i18n.T(...))；
// 结尾恰好有一个 %w 的，文案挪进目录，%w 留在调用点包装原错误。
func (x *extractor) errorfEntry(e *Entry, call *ast.CallExpr, raw string, argTexts []string) {
	if !strings.Contains(raw, "%w") {
		e.Edits = []Edit{{x.off(call.Pos()), x.off(call.End()), "errors.New({{T}})"}}
		e.Ctx = "errorf-errors.New"
		return
	}
	if !strings.HasSuffix(raw, "%w") || strings.Count(raw, "%w") != 1 || len(argTexts) == 0 {
		e.Manual = firstNonEmpty(e.Manual, "errorf-%w")
		return
	}
	base := strings.TrimSuffix(raw, "%w")
	text, count, ok := positional(base)
	if !ok || count != len(argTexts)-1 {
		e.Manual = firstNonEmpty(e.Manual, "errorf-%w")
		return
	}
	if count == 0 {
		text = strings.ReplaceAll(base, "%%", "%")
	}
	e.ZH = text
	e.Args = argTexts[:len(argTexts)-1]
	e.LastRange = e.ArgRanges[len(e.ArgRanges)-1]
	e.ArgRanges = e.ArgRanges[:len(e.ArgRanges)-1]
	e.Edits = []Edit{{x.off(call.Pos()), x.off(call.End()), `fmt.Errorf("%s%w", {{T}}, {{LAST}})`}}
	e.Ctx = "errorf-w"
}

// writeEntries 把抽取结果写成 JSON，供 apply 读回。
func writeEntries(path string, entries []Entry) error {
	b, err := json.MarshalIndent(entries, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// writeListing 写给人看的清单：一行一条，方便对着写英文；工具不敢自动改的排在最后（MANUAL）。
func writeListing(path string, entries []Entry) error {
	var auto, manual strings.Builder
	for _, e := range entries {
		zh := strings.ReplaceAll(e.ZH, "\n", "⏎")
		if e.Manual != "" {
			fmt.Fprintf(&manual, "MANUAL\t%d\t%s:%d\t%s\t%s\t%s\n",
				e.ID, filepath.Base(e.File), e.Line, e.Manual, e.Callee, zh)
			continue
		}
		callee := strings.Join(strings.Fields(e.Callee), " ")
		if len(callee) > 28 {
			callee = "…" + callee[len(callee)-27:]
		}
		fmt.Fprintf(&auto, "%d\t%s:%d\t%s\t%s\t%s\n", e.ID, filepath.Base(e.File), e.Line, e.Ctx, callee, zh)
	}
	return os.WriteFile(path, []byte(auto.String()+manual.String()), 0o644)
}
