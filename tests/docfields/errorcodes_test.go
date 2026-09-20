// 本文件守着 docs/{en,zh}/06-architecture/10-error-codes.md（错误码参考）与 CLI 真实的
// 错误码、报错标题**不脱节**。
//
// # 为什么要有它
//
// 错误码是**对外契约**：每条终止命令的错误都在 stderr 的 JSON 日志里带一个稳定的
// error_code，CI 脚本据此判断该重试还是该报警——NETWORK_UNREACHABLE 值得重试，
// CONFIG_INVALID 重试多少次都一样。这份契约原本写在设计书 004 §10.2.1，并由
// clierr.TestEveryErrorCodeIsDocumented 守着；设计书归档时那条测试跟着撤了，
// 契约就没有了活文档的家——新增一个码、改掉一句报错，都不会让任何东西失败，
// 只会让照着文档写脚本、或照着文档排障的人对不上号。
//
// # 判据（比被撤掉的那条更强）
//
// 旧的只查"码的字符串出现在文档里"。这里查三件事：
//
//  1. 每个 clierr.Code* 常量，en/zh 两份文档里各有一节（### CODE）——没有豁免表，
//     连目前没有命令会产生的 NOT_IMPLEMENTED 也要写明这一点；反过来，文档里的
//     每一节都必须是真实存在的码。
//  2. 文档里引用的每个报错标题（表格第一列的反引号内容），在 Go 源码里必须真实存在。
//     错误码是粗粒度的类别（CONFIG_INVALID 背后有七十多种情形），排障要靠标题认出
//     具体是哪一种——标题被改了措辞，文档就成了一条永远搜不到的死线索。
//  3. 匹配规则的检测器自己有自检：解析坏了会直接失败，而不是给出一个漂亮的全绿。
//
// 真相来源是源码本身：错误码常量从 clierr.go 里取，标题从 go/ast 抽出所有
// clierr.New / Newf / Warn / NewProblemSet 调用的第二个参数，不是又抄一份清单。
package docfields_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

// titlePlaceholder 是报错标题里"这里会填进一个变量"的占位符：
// 源码里的 %s / %d，或 "a" + x + "b" 拼接里非字面量的那段；文档里的 <组件> 这类写法。
const titlePlaceholder = "<…>"

var (
	// codeConst 匹配 clierr.go 里的错误码常量：CodeXxx Code = "XXX_YYY"。
	codeConst = regexp.MustCompile(`(?m)^\s*Code\w+\s+Code\s*=\s*"([A-Z][A-Z_]*)"`)
	// formatVerb 匹配格式串里的动词：%s %d %v %q %-10s 等。
	formatVerb = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0-9.]*[a-zA-Z]`)
	// docPlaceholder 匹配文档标题里的占位写法：<组件>、<component>。
	docPlaceholder = regexp.MustCompile(`<[^<>]*>`)
	// codeHeading 匹配错误码参考里的一节：### DEPENDENCY_MISSING。
	codeHeading = regexp.MustCompile(`^### ([A-Z][A-Z_]+)\s*$`)
	// backticked 匹配一格里的反引号内容。
	backticked = regexp.MustCompile("`([^`]+)`")
)

// clierrCodes 取出 clierr.go 里所有的错误码值。
func clierrCodes(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot, "internal", "clierr", "clierr.go"))
	require.NoError(t, err)
	var out []string
	for _, m := range codeConst.FindAllStringSubmatch(string(body), -1) {
		out = append(out, m[1])
	}
	return out
}

// msgidKeys 从 internal/msgid 源码里取出常量名 → key 字符串值的映射
// （msgid.ProjectMissing -> "project.missing"），用来把 i18n.T(msgid.X)
// 调用还原成它实际查到的文案。跟 clierrCodes() 是同一个手法：真相来自
// 源码本身，不是又抄一份清单。
func msgidKeys(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(repoRoot, "internal", "msgid")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	re := regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*"([^"]+)"`)
	out := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		for _, m := range re.FindAllStringSubmatch(string(body), -1) {
			out[m[1]] = m[2]
		}
	}
	return out
}

// i18nCallTitle 把 i18n.T(msgid.Name, ...) 形状的调用还原成它在 zh 目录里
// 的实际文案。错误码文档现在两侧（docs/en 与 docs/zh）都还照抄同一句
// 中文原文（子项目 3 才会改成两侧各自的真实语言），所以这里统一按 zh
// 目录解析，跟文档现状对齐；子项目 3 改变文档约定之后，这里要跟着改成
// 按文档所在的语言分别解析。
func i18nCallTitle(call *ast.CallExpr, msgidToKey map[string]string, zhCatalog map[string]string) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "i18n" || sel.Sel.Name != "T" || len(call.Args) < 1 {
		return "", false
	}
	idSel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	idPkg, ok := idSel.X.(*ast.Ident)
	if !ok || idPkg.Name != "msgid" {
		return "", false
	}
	key, ok := msgidToKey[idSel.Sel.Name]
	if !ok {
		return "", false
	}
	text, ok := zhCatalog[key]
	return text, ok
}

// titleLiteral 取出一个报错标题表达式里的字面量：字符串字面量直接取；
// "a" + x + "b" 这种拼接，把非字面量的那段记成占位符。取不出返回 false。
func titleLiteral(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		left, lok := titleLiteral(v.X)
		right, rok := titleLiteral(v.Y)
		if !lok && !rok {
			return "", false
		}
		if !lok {
			left = titlePlaceholder
		}
		if !rok {
			right = titlePlaceholder
		}
		return left + right, true
	}
	return "", false
}

// sourceTitles 扫 internal/ 与 cmd/ 下所有非测试文件，抽出
// clierr.New / Newf / Warn / NewProblemSet 调用的第二个参数（报错标题），
// 格式串里的动词统一换成占位符。
func sourceTitles(t *testing.T) []string {
	t.Helper()
	msgidToKey := msgidKeys(t)
	zhCatalog := i18n.CatalogFor(i18n.ZH)
	fset := token.NewFileSet()
	var out []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err, path)
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "clierr" {
					return true
				}
				switch sel.Sel.Name {
				case "New", "Newf", "Warn", "NewProblemSet":
				default:
					return true
				}
				title, ok := titleLiteral(call.Args[1])
				if !ok {
					if inner, isCall := call.Args[1].(*ast.CallExpr); isCall {
						title, ok = i18nCallTitle(inner, msgidToKey, zhCatalog)
					}
				}
				if ok {
					// 源码标题里本来就带尖括号的（"init <项目名称>"）也归一成占位符，
					// 与文档那一侧的归一化对称。
					title = formatVerb.ReplaceAllString(title, titlePlaceholder)
					out = append(out, docPlaceholder.ReplaceAllString(title, titlePlaceholder))
				}
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}
	return out
}

// minFixedRunes 是一个源码标题能进匹配池所需的"固定文字"最少字数（不含开头的
// "错误："与占位符）。"错误：<…>" 这种几乎全是变量的标题，会把任何文档标题都放行。
const minFixedRunes = 3

// compileTitlePatterns 把源码标题编译成匹配模式：占位符匹配任意非空内容。
func compileTitlePatterns(titles []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, title := range titles {
		fixed := strings.ReplaceAll(title, titlePlaceholder, "")
		fixed = strings.TrimPrefix(strings.TrimSpace(fixed), "错误：")
		fixed = strings.Join(strings.Fields(fixed), "")
		if utf8.RuneCountInString(fixed) < minFixedRunes {
			continue
		}
		pattern := regexp.QuoteMeta(title)
		pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta(titlePlaceholder), ".+")
		out = append(out, regexp.MustCompile("^"+pattern+"$"))
	}
	return out
}

// titleExists 报告文档里写的标题，是否对得上源码里的某个标题。
func titleExists(docTitle string, patterns []*regexp.Regexp) bool {
	normalized := docPlaceholder.ReplaceAllString(docTitle, titlePlaceholder)
	for _, re := range patterns {
		if re.MatchString(normalized) {
			return true
		}
	}
	return false
}

// errorCodeDoc 是从一份错误码参考文档里抽出的东西。
type errorCodeDoc struct {
	codes  []string // 各节标题：### CODE
	titles []string // 第一节之后所有表格第一列里的反引号内容
}

// parseErrorCodeDoc 抽出文档里的错误码小节与报错标题。第一个 "### CODE" 之前的
// 内容是导读（退出码表之类），它的表格不算标题。
func parseErrorCodeDoc(markdown string) errorCodeDoc {
	var doc errorCodeDoc
	inCodes := false
	for _, line := range strings.Split(markdown, "\n") {
		if m := codeHeading.FindStringSubmatch(line); m != nil {
			inCodes = true
			doc.codes = append(doc.codes, m[1])
			continue
		}
		if !inCodes || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		for _, m := range backticked.FindAllStringSubmatch(firstTableCell(strings.TrimSpace(line)), -1) {
			doc.titles = append(doc.titles, m[1])
		}
	}
	return doc
}

func errorCodesDocPath(lang string) string {
	return filepath.Join("docs", lang, "06-architecture", "10-error-codes.md")
}

// 错误码参考必须为每个错误码写一节，且不多写不存在的码。
func TestErrorCodesDocCoversEveryCode(t *testing.T) {
	codes := clierrCodes(t)
	require.GreaterOrEqual(t, len(codes), 20,
		"clierr.go 只抽出 %d 个错误码——codeConst 坏了，这条测试的结论不可信", len(codes))

	for _, lang := range []string{"en", "zh"} {
		rel := errorCodesDocPath(lang)
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		require.NoError(t, err, "%s 不存在：错误码是对外契约，需要一份活文档", rel)
		doc := parseErrorCodeDoc(string(body))

		missing, phantom := nameDrift(codes, doc.codes)
		for _, code := range missing {
			t.Errorf("%s：clierr 里有错误码 %s，文档里没有 \"### %s\" 这一节\n"+
				"   新增错误码必须写进错误码参考；暂时不会被产生的码也要写明这一点", rel, code, code)
		}
		for _, code := range phantom {
			t.Errorf("%s：文档里有一节 %s，clierr 里没有这个错误码\n"+
				"   错误码只增不改——它被改名/删掉了，还是这一节的标题写错了？", rel, code)
		}
	}
}

// 文档里引用的每个报错标题，必须真实存在于 Go 源码。
func TestErrorCodesDocTitlesExistInSource(t *testing.T) {
	patterns := compileTitlePatterns(sourceTitles(t))
	require.GreaterOrEqual(t, len(patterns), 100,
		"只从源码里编出 %d 个标题模式——sourceTitles 或 compileTitlePatterns 坏了，这条测试的结论不可信", len(patterns))

	for _, lang := range []string{"en", "zh"} {
		rel := errorCodesDocPath(lang)
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		require.NoError(t, err, "%s 不存在", rel)
		doc := parseErrorCodeDoc(string(body))
		require.GreaterOrEqual(t, len(doc.titles), 40,
			"%s 只抽出 %d 个报错标题——parseErrorCodeDoc 坏了，这条测试的结论不可信", rel, len(doc.titles))

		for _, title := range doc.titles {
			if !titleExists(title, patterns) {
				t.Errorf("%s：文档引用的报错标题 `%s` 在源码里找不到\n"+
					"   CLI 改了措辞而文档没跟上，还是文档里抄错了？带变量的部分用 <组件> 这样的占位符写", rel, title)
			}
		}
	}
}

// 标题匹配自己要能认得出对的、拦得住错的，也不能被"几乎全是变量"的标题放水。
func TestErrorTitleMatcher(t *testing.T) {
	patterns := compileTitlePatterns([]string{
		"错误：强依赖 <…> 被禁用",
		"错误：<…> 校验失败",
		"错误：<…>",     // 全是变量：不能进匹配池
		"错误：<…>失败", // 固定文字不足三个字：不能进匹配池
	})
	require.Len(t, patterns, 2, "几乎全是变量的标题不该进匹配池")

	require.True(t, titleExists("错误：强依赖 <组件> 被禁用", patterns), "文档占位符要能对上源码的变量")
	require.True(t, titleExists("错误：brickkit.yaml 校验失败", patterns), "文档写了具体值也要能对上源码的变量")
	require.False(t, titleExists("错误：强依赖 <组件> 被停用", patterns), "措辞改了必须被拦下")
	require.False(t, titleExists("错误：随便写点什么", patterns), "全变量的标题不能放行任意文档标题")

	doc := parseErrorCodeDoc("| 退出码 | 含义 |\n| `0` | 成功 |\n\n### A_B\n\n| 标题 | 原因 |\n| --- | --- |\n| `错误：x` | y |\n\n### C\n")
	require.Equal(t, []string{"A_B"}, doc.codes, "单字母的小节标题不是错误码")
	require.Equal(t, []string{"错误：x"}, doc.titles, "第一节之前的表格不算报错标题")
}

func TestFormatVerbMatchesPositionalVerbs(t *testing.T) {
	assert.Equal(t, titlePlaceholder+" 已启动", formatVerb.ReplaceAllString("%[1]s 已启动", titlePlaceholder))
	assert.Equal(t, titlePlaceholder+" 个", formatVerb.ReplaceAllString("%d 个", titlePlaceholder))
}

func TestI18nCallTitleResolvesKnownMessage(t *testing.T) {
	msgidToKey := msgidKeys(t)
	zhCatalog := i18n.CatalogFor(i18n.ZH)

	fset := token.NewFileSet()
	expr, err := parser.ParseExprFrom(fset, "", `i18n.T(msgid.ProjectMissing)`, 0)
	require.NoError(t, err)
	call, ok := expr.(*ast.CallExpr)
	require.True(t, ok)

	title, ok := i18nCallTitle(call, msgidToKey, zhCatalog)
	require.True(t, ok)
	assert.Equal(t, "错误：项目配置文件不存在", title)
}

func TestI18nCallTitleRejectsUnrelatedCalls(t *testing.T) {
	msgidToKey := msgidKeys(t)
	zhCatalog := i18n.CatalogFor(i18n.ZH)

	fset := token.NewFileSet()
	expr, err := parser.ParseExprFrom(fset, "", `fmt.Sprintf("x")`, 0)
	require.NoError(t, err)
	call, ok := expr.(*ast.CallExpr)
	require.True(t, ok)

	_, ok = i18nCallTitle(call, msgidToKey, zhCatalog)
	assert.False(t, ok, "不是 i18n.T(msgid.X) 形状的调用要直接放行返回 false，不能误判")
}
