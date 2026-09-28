// Package errorhints 守着一条对使用者的承诺：**CLI 的每一条报错都告诉人下一步该做什么**。
//
// 报错只说"错了"不说"怎么办"，使用者就只能去搜、去猜、去翻源码。这条承诺曾经靠一次
// 154 处的人工全量审计成立——可审计只证明审计那天，之后每加一条报错，它都可能悄悄不再成立
// （三层重构之后复查，确实有几十处没有建议）。所以改成守卫：每次跑测试都重新证明一遍。
//
// # 判据
//
// 每一处 clierr.New / clierr.Newf / clierr.NewProblemSet 构造出的报错，必须带建议
// （WithHint 或 WithTip），以下两种写法都算：
//
//	return clierr.New(...).WithHint(...)          构造链上直接带
//	e := clierr.New(...) … e = e.WithHint(...)    同一个函数里对同一个变量补上
//
// 确实没有建议可给的报错，在构造处（同一行或上一行）写明豁免与理由：
//
//	// clierr:nohint <理由>
//
// 理由要说清为什么没有下一步可以建议，不能空着——豁免是一句要被人审阅的判断，不是开关。
//
// 警告（clierr.Warn）不在此列：警告不阻断，很多只是告知。
package errorhints

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// constructors 是会构造出一条报错的 clierr 函数。
var constructors = map[string]bool{"New": true, "Newf": true, "NewProblemSet": true}

// hintMethods 是给报错加上建议的方法。
var hintMethods = map[string]bool{"WithHint": true, "WithTip": true}

// exemption 是构造处的豁免注释；理由至少要有几个字。
var exemption = regexp.MustCompile(`clierr:nohint\s+(\S.{3,})`)

// site 是一处构造。
type site struct {
	pos     token.Position
	message string
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return root
}

// scan 返回 root 下（internal/ 与 cmd/，不含测试文件）全部构造处，以及其中缺建议的那些。
func scan(t *testing.T, root string) (all, missing []site) {
	t.Helper()
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			a, m := scanFile(t, path)
			all, missing = append(all, a...), append(missing, m...)
			return nil
		})
		require.NoError(t, err)
	}
	return all, missing
}

func scanFile(t *testing.T, path string) (all, missing []site) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	require.NoError(t, err)
	exempt := exemptLines(fset, file)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		parents := parentMap(fn.Body)
		hinted := hintedIdents(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isConstructor(call) {
				return true
			}
			pos := fset.Position(call.Pos())
			s := site{pos: pos, message: messageOf(call)}
			all = append(all, s)
			if exempt[pos.Line] || exempt[pos.Line-1] {
				return true
			}
			outer, chain := outermost(call, parents)
			if chainHasHint(chain) {
				return true
			}
			if name := assignedTo(outer, parents); name != "" && hinted[name] {
				return true
			}
			missing = append(missing, s)
			return true
		})
	}
	return all, missing
}

// exemptLines 是带合格豁免注释的行号。
func exemptLines(fset *token.FileSet, file *ast.File) map[int]bool {
	out := map[int]bool{}
	for _, group := range file.Comments {
		for _, c := range group.List {
			if exemption.MatchString(c.Text) {
				out[fset.Position(c.Pos()).Line] = true
			}
		}
	}
	return out
}

func isConstructor(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "clierr" && constructors[sel.Sel.Name]
}

// messageOf 取构造的第二个参数（文案）的源码写法，只用于失败时的输出。
func messageOf(call *ast.CallExpr) string {
	if len(call.Args) < 2 {
		return ""
	}
	var b strings.Builder
	ast.Inspect(call.Args[1], func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "msgid" {
				b.WriteString(sel.Sel.Name)
				return false
			}
		}
		return true
	})
	return b.String()
}

func parentMap(root ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

// outermost 沿着 x.M(...) 链往外走到头，返回最外层的表达式与链上的方法名。
func outermost(call *ast.CallExpr, parents map[ast.Node]ast.Node) (ast.Expr, []string) {
	var chain []string
	var expr ast.Expr = call
	for {
		sel, ok := parents[expr].(*ast.SelectorExpr)
		if !ok || sel.X != expr {
			return expr, chain
		}
		outer, ok := parents[sel].(*ast.CallExpr)
		if !ok || outer.Fun != sel {
			return expr, chain
		}
		chain = append(chain, sel.Sel.Name)
		expr = outer
	}
}

func chainHasHint(chain []string) bool {
	for _, m := range chain {
		if hintMethods[m] {
			return true
		}
	}
	return false
}

// assignedTo 返回构造链被赋给的变量名（x := … / x = … / var x = …），没有时为空。
func assignedTo(expr ast.Expr, parents map[ast.Node]ast.Node) string {
	switch p := parents[expr].(type) {
	case *ast.AssignStmt:
		for i, rhs := range p.Rhs {
			if rhs == expr && i < len(p.Lhs) {
				if id, ok := p.Lhs[i].(*ast.Ident); ok {
					return id.Name
				}
			}
		}
	case *ast.ValueSpec:
		for i, v := range p.Values {
			if v == expr && i < len(p.Names) {
				return p.Names[i].Name
			}
		}
	}
	return ""
}

// hintedIdents 是函数体里补过 WithHint / WithTip 的变量名：x.WithHint(...)，
// 或者以 x 起头的方法链上有它（x.WithDetail(...).WithHint(...)）。
func hintedIdents(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !hintMethods[sel.Sel.Name] {
			return true
		}
		if id := chainRoot(sel.X); id != "" {
			out[id] = true
		}
		return true
	})
	return out
}

// chainRoot 沿着 x.A(...).B(...) 往里走，返回起头的变量名；起头不是变量时为空。
func chainRoot(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.Name
		case *ast.CallExpr:
			sel, ok := e.Fun.(*ast.SelectorExpr)
			if !ok {
				return ""
			}
			expr = sel.X
		default:
			return ""
		}
	}
}

func TestEveryErrorCarriesASuggestion(t *testing.T) {
	root := repoRoot(t)
	all, missing := scan(t, root)
	require.Greater(t, len(all), 200, "自检：构造处一个都没找到——扫描坏了，结论不可信")

	lines := make([]string, 0, len(missing))
	for _, s := range missing {
		rel, _ := filepath.Rel(root, s.pos.Filename)
		lines = append(lines, rel+":"+strconv.Itoa(s.pos.Line)+"  "+s.message)
	}
	sort.Strings(lines)
	assert.Empty(t, lines,
		"这些报错没有告诉使用者下一步该做什么（共 %d 处）：\n   %s\n"+
			"   给它加上 WithHint；确实没有建议可给时，在构造处写 // clierr:nohint <理由>",
		len(lines), strings.Join(lines, "\n   "))
}

// 自检：判据本身没坏——该认的写法认得出，该拦的拦得住。
func TestScannerRecognisesTheShapes(t *testing.T) {
	dir := t.TempDir()
	src := `package x

import "github.com/brickkit/brickkit/internal/clierr"

func chain() error { return clierr.New("C", "m").WithDetail("k", "v").WithHint("do this") }

func later() error {
	e := clierr.New("C", "m")
	if true {
		e = e.WithHint("do that")
	}
	return e
}

func loop(refs []string) error {
	err := clierr.New("C", "m")
	for _, r := range refs {
		err = err.WithDetail("k", r)
	}
	return err.WithDetail("why", "v").WithHint("fix it")
}

func problems() error {
	p := clierr.NewProblemSet("C", "m")
	p.WithHint("see the reference")
	return p.Err()
}

func exempt() error {
	// clierr:nohint the caller always wraps this with its own hint
	return clierr.New("C", "m")
}

func bare() error { return clierr.New("C", "m").WithDetail("k", "v") }

func bareVar() error {
	e := clierr.Newf("C", "m %d", 1)
	return e
}

func emptyExemption() error {
	// clierr:nohint
	return clierr.New("C", "m")
}
`
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	all, missing := scanFile(t, path)
	assert.Len(t, all, 8)
	var got []int
	for _, s := range missing {
		got = append(got, s.pos.Line)
	}
	assert.Equal(t, []int{34, 37, 43}, got, "只有不带建议、也没有合格豁免的三处")
}
