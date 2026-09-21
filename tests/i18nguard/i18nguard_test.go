// Package i18nguard 守住"CLI 消息全部走目录"这条验收标准，让它不靠人工扫描。
//
// 两条守卫都用 go/parser 看真正的字符串字面量（注释、文档不算）：
//
//  1. 生产代码里不许再写死带中文的字符串——用户看得见的文字一律走
//     i18n.T(msgid.X)，翻译只改 catalog_*.go。
//  2. 测试里不许写"中文短语的否定断言"（NotContains / NotEqual / NotRegexp）：
//     默认语言是英文，这种断言对英文输出永远成立，等于检查悄悄消失了。
//
// 两条都有一份显式的白名单，每一项都要写清理由；加白名单是有意识的决定，不是顺手。
package i18nguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoRoot 是仓库根目录（本包在 tests/i18nguard/ 下）。
const repoRoot = "../.."

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef) {
			return true
		}
	}
	return false
}

// goFiles 列出 root 下（相对仓库根）所有 .go 文件；test 决定要测试文件还是生产文件。
func goFiles(t *testing.T, dirs []string, test bool) []string {
	t.Helper()
	var out []string
	for _, dir := range dirs {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") != test {
				return nil
			}
			rel, _ := filepath.Rel(repoRoot, path)
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		require.NoError(t, err)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// 守卫 1：生产代码里没有写死的中文字符串
// ---------------------------------------------------------------------------

// hardcodedAllow 是允许含中文字符串的位置。key 是相对仓库根的路径；
// value 为空表示整个文件放行，否则只放行这几个顶层声明（函数名 / 变量名）。
var hardcodedAllow = map[string]struct {
	decls  []string
	reason string
}{
	"internal/i18n/catalog_zh.go": {reason: "中文消息目录本身"},
	"internal/i18n/catalog_en.go": {reason: "英文目录里也有作为示例出现的中文（如 zh 路径说明）"},
	"internal/cli/root.go": {
		decls:  []string{"usageTemplate", "localize"},
		reason: "cobra 的中文用法模板与 help/completion 中文化，只在语言为 zh 时才套用；英文用 cobra 自带默认文案",
	},
}

// hardcodedSkipDirs 是整目录不检查的开发者工具：它们只在 make generate-schemas 时
// 由维护者运行，报错读者不是使用者。
var hardcodedSkipDirs = []string{"internal/schemagen/", "cmd/gen-schemas/"}

func TestNoHardcodedChineseInProductionCode(t *testing.T) {
	var offenders []string
	for _, rel := range goFiles(t, []string{"internal", "cmd"}, false) {
		skip := false
		for _, d := range hardcodedSkipDirs {
			if strings.HasPrefix(rel, d) {
				skip = true
			}
		}
		if skip {
			continue
		}
		allow, hasAllow := hardcodedAllow[rel]
		if hasAllow && len(allow.decls) == 0 {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(repoRoot, rel), nil, 0)
		require.NoError(t, err, rel)

		for _, decl := range f.Decls {
			name := declName(decl)
			if hasAllow && contains(allow.decls, name) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if v, err := strconv.Unquote(lit.Value); err == nil && hasCJK(v) {
					offenders = append(offenders, fset.Position(lit.Pos()).String()+"  "+abbreviate(v))
				}
				return true
			})
		}
	}
	assert.Empty(t, offenders, "生产代码里有写死的中文字符串。用户看得见的文字要放进 internal/msgid + "+
		"internal/i18n 两份目录，代码里写 i18n.T(msgid.X)（不能在包级 var/const 里调，要包成函数）。"+
		"确有理由保留就加进 hardcodedAllow 并写清理由。\n%s", strings.Join(offenders, "\n"))
}

// ---------------------------------------------------------------------------
// 守卫 2：测试里没有"中文短语的否定断言"
// ---------------------------------------------------------------------------

// negativeAllow 是允许出现中文需要的位置：key 是 "相对路径|短语"。
var negativeAllow = map[string]string{
	"internal/cli/log_lang_test.go|命令开始执行": "有意的：默认（英文）日志里不该出现中文那句，检查的正是'没有中文'",
	"internal/market/market_test.go|内部堆栈":  "服务端返回的 details.cause 夹具本身是中文，检查的是'不外泄给使用者'",
}

var negativeFuncs = map[string]bool{"NotContains": true, "NotEqual": true, "NotRegexp": true, "NotSubset": true}

func TestNoChineseNegativeAssertionsInTests(t *testing.T) {
	var offenders []string
	for _, rel := range goFiles(t, []string{"internal", "cmd", "tests"}, true) {
		if strings.HasPrefix(rel, "internal/schemagen/") || strings.HasPrefix(rel, "cmd/gen-schemas/") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(repoRoot, rel), nil, 0)
		require.NoError(t, err, rel)

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !negativeFuncs[sel.Sel.Name] {
				return true
			}
			// assert.NotContains(t, s, needle, msg...)：needle 是第 3 个参数
			if len(call.Args) < 3 {
				return true
			}
			lit, ok := call.Args[2].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil || !hasCJK(v) {
				return true
			}
			if _, allowed := negativeAllow[rel+"|"+v]; allowed {
				return true
			}
			offenders = append(offenders, fset.Position(lit.Pos()).String()+"  "+abbreviate(v))
			return true
		})
	}
	assert.Empty(t, offenders, "这些否定断言用了中文短语。默认语言是英文，它们对英文输出永远成立，"+
		"等于检查悄悄消失了。请改成对应的英文措辞；确有理由再加进 negativeAllow 并写清理由。\n%s",
		strings.Join(offenders, "\n"))
}

// 白名单不能悄悄烂掉：里面列的位置必须真的存在，否则就该删掉这一项。
func TestNegativeAllowlistEntriesStillExist(t *testing.T) {
	for key := range negativeAllow {
		rel, phrase, _ := strings.Cut(key, "|")
		body, err := readFile(filepath.Join(repoRoot, rel))
		require.NoError(t, err, key)
		assert.Contains(t, body, phrase, "白名单项 %q 已经不存在，请删掉", key)
	}
}

func declName(d ast.Decl) string {
	switch x := d.(type) {
	case *ast.FuncDecl:
		return x.Name.Name
	case *ast.GenDecl:
		for _, spec := range x.Specs {
			if v, ok := spec.(*ast.ValueSpec); ok && len(v.Names) > 0 {
				return v.Names[0].Name
			}
		}
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func abbreviate(s string) string {
	s = strings.ReplaceAll(s, "\n", "⏎")
	if r := []rune(s); len(r) > 50 {
		return string(r[:50]) + "…"
	}
	return s
}
