package i18nguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

// langBranchAllow 是生产代码里唯一允许点名某种语言的地方，各带理由。
var langBranchAllow = map[string]string{
	"internal/skills/install.go": "lockLangBeforeLangField：Lang 字段出现之前写下的 skills.lock，装的只可能是中文资产——关于旧文件的事实，不是语言分支",
}

// 生产代码（internal/i18n 之外）不按某一种具体语言分支：语言只在登记处出现。
func TestNoProductionCodeBranchesOnALanguage(t *testing.T) {
	for _, rel := range goFiles(t, []string{"internal", "cmd"}, false) {
		if strings.HasPrefix(rel, "internal/i18n/") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, rel), nil, 0)
		require.NoError(t, err, rel)
		ast.Inspect(f, func(n ast.Node) bool {
			var hit string
			switch v := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := v.X.(*ast.Ident); ok && id.Name == "i18n" && (v.Sel.Name == "ZH" || v.Sel.Name == "EN") {
					hit = "i18n." + v.Sel.Name
				}
			case *ast.CallExpr:
				if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Lang" {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "i18n" && len(v.Args) == 1 {
						if _, lit := v.Args[0].(*ast.BasicLit); lit {
							hit = "i18n.Lang(literal)"
						}
					}
				}
			}
			if hit != "" && langBranchAllow[rel] == "" {
				t.Errorf("%s 点名了一种具体语言（%s）：语言从 i18n 的登记处推出来，不在这里写死", rel, hit)
			}
			return true
		})
	}
}

// 白名单里的文件确实还点名着那种语言——白名单不许留下失效的条目。
func TestLangBranchAllowEntriesStillExist(t *testing.T) {
	for rel := range langBranchAllow {
		body, err := readFile(filepath.Join(repoRoot, rel))
		require.NoError(t, err, rel)
		assert.Contains(t, body, "lockLangBeforeLangField", rel)
	}
}

// 语言列表不写死在文案或代码里（"en|zh"、"en, zh"）：加一种语言后，这些地方不会跟着变。
// 列表一律从 i18n.LangNames() 拼出来。
func TestNoLanguageListWrittenOut(t *testing.T) {
	names := i18n.LangNames()
	var lists []string
	for _, sep := range []string{"|", ", ", "/", " | "} {
		lists = append(lists, strings.Join(names, sep))
	}
	check := func(where, text string) {
		for _, l := range lists {
			if strings.Contains(text, l) {
				t.Errorf("%s 写死了语言列表 %q：用 i18n.LangNames() 拼出来", where, l)
			}
		}
	}
	for _, lang := range i18n.SupportedLangs() {
		for key, text := range i18n.CatalogFor(lang) {
			check(string(lang)+" "+key, text)
		}
	}
	for _, rel := range goFiles(t, []string{"internal", "cmd"}, false) {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, rel), nil, 0)
		require.NoError(t, err, rel)
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				check(rel, lit.Value)
			}
			return true
		})
	}
}
