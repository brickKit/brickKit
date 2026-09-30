package i18nguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 守卫 4：英文里，错误块的标题（clierr.New 等的第二个参数）、标签（WithDetail 的键）与建议（WithHint / WithTip）
// 都是一句话的开头，要大写。
// 句中片段（"not starting (…)"、"must be an array"）不在其中——它们接在别的文字后面。
//
// 以命令或标识符开头的照原样：`brickkit local on`、`configSchema is a spec sheet`……
var lowercaseStartAllowed = map[string]bool{
	"brickkit": true, "git": true, "docker": true, "kubectl": true, "cosign": true, "podman": true,
	"systemctl": true, "up": true, "restore": true, "configSchema": true, "publicKeyRef": true,
	"exposePort": true, "skipWaitFor": true, "type": true, "existingSecret": true, "config/%[1]s": true,
}

// sentenceStartMsgids 找出生产代码里当作标签或建议用的 msgid：
// .WithDetail(i18n.T(msgid.X), …) 的第一个参数，.WithHint / .WithTip 的全部参数。
func sentenceStartMsgids(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range goFiles(t, []string{"internal", "cmd"}, false) {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, rel), nil, 0)
		require.NoError(t, err, rel)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			var args []ast.Expr
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "clierr" {
				// clierr.New(code, title) 等：标题渲染成 "❌ <标题>"，是一句话的开头。
				switch sel.Sel.Name {
				case "New", "Newf", "Warn", "NewProblemSet":
					if len(call.Args) > 1 {
						args = call.Args[1:2]
					}
				}
			}
			switch sel.Sel.Name {
			case "WithDetail":
				if len(call.Args) > 0 {
					args = call.Args[:1]
				}
			case "WithHint", "WithTip":
				args = call.Args
			}
			for _, a := range args {
				if name := translatedMsgid(a); name != "" {
					out[name] = rel
				}
			}
			return true
		})
	}
	return out
}

// translatedMsgid 认出 i18n.T(msgid.X, …)，返回 X。
func translatedMsgid(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return ""
	}
	if fn, ok := call.Fun.(*ast.SelectorExpr); !ok || fn.Sel.Name != "T" {
		return ""
	}
	if id, ok := call.Args[0].(*ast.SelectorExpr); ok {
		if pkg, ok := id.X.(*ast.Ident); ok && pkg.Name == "msgid" {
			return id.Sel.Name
		}
	}
	return ""
}

// 大写开头是英文的规则，这个守卫只对英文目录有意义，所以点名 i18n.EN，不遍历登记处。
func TestEnglishLabelsAndHintsStartWithACapital(t *testing.T) {
	values := msgidValues(t)
	en := i18n.CatalogFor(i18n.EN)
	uses := sentenceStartMsgids(t)
	require.Greater(t, len(uses), 200, "只认出 %d 处标题/标签/建议——解析坏了，结论不可信", len(uses))

	for name, where := range uses {
		text, ok := en[msgid.ID(values[name])]
		if !ok || text == "" {
			continue
		}
		first, _ := utf8.DecodeRuneInString(text)
		if !unicode.IsLower(first) {
			continue
		}
		word := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ':' || r == ',' })[0]
		assert.True(t, lowercaseStartAllowed[word],
			"%s 把 msgid.%s 当标题、标签或建议用，英文却以小写开头：%q", where, name, abbreviate(text))
	}
}
