package i18nguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid/msgidgen"
)

var positionalVerbRE = regexp.MustCompile(`%[-+# 0]*\d*(?:\.\d+)?\[(\d+)\][a-zA-Z]`)

// 调用处传的参数个数，正好是源语言文案用到的最大位置：少传，运行时冒出 %!s(BADINDEX)
// （一个都不传时 T 原样返回，%[1]s 直接显示给使用者）；多传，那个参数永远不会出现。
// 只查写成 i18n.T(msgid.X, …) / TN / Count 的调用；ID 放在变量里、或参数用 args... 展开的，查不了。
// 源语言留空的 key（cobra.*）由 internal/i18n 的 TestKeysWithoutSourceTextUseOnlyTheArgumentsPassed 管。
func TestCallsPassTheArgumentsTheirMessageUses(t *testing.T) {
	uses := map[string]int{} // Go 常量名 → 文案用到的最大位置
	for key, text := range i18n.CatalogFor(i18n.SourceLang()) {
		if text == "" {
			continue
		}
		n := 0
		for _, m := range positionalVerbRE.FindAllStringSubmatch(text, -1) {
			if i, _ := strconv.Atoi(m[1]); i > n {
				n = i
			}
		}
		uses[msgidgen.GoName(string(key))] = n
	}

	checked := 0
	for _, rel := range goFiles(t, []string{"internal", "cmd"}, false) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(repoRoot, rel), nil, 0)
		require.NoError(t, err, rel)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || call.Ellipsis.IsValid() || len(call.Args) == 0 {
				return true
			}
			fn := qualified(call.Fun, "i18n")
			id := qualified(call.Args[0], "msgid")
			if fn == nil || id == nil {
				return true
			}
			want, known := uses[id.Sel.Name]
			if !known {
				return true
			}
			var got int
			switch fn.Sel.Name {
			case "T":
				got = len(call.Args) - 1
			case "TN":
				got = len(call.Args) - 2
			case "Count":
				got = 1
			default:
				return true
			}
			checked++
			if got != want {
				t.Errorf("%s: i18n.%s(msgid.%s, …) passes %d argument(s); the %s text uses %d",
					fset.Position(call.Pos()), fn.Sel.Name, id.Sel.Name, got, i18n.SourceLang(), want)
			}
			return true
		})
	}
	require.Greater(t, checked, 1000, "只认出 %d 处调用——解析坏了，结论不可信", checked)
}

// qualified 在 e 是 pkg.Name 这种写法时返回它，否则返回 nil。
func qualified(e ast.Expr, pkg string) *ast.SelectorExpr {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg {
		return sel
	}
	return nil
}
