// 本文件守着文档里抄下来的"CLI 输出行"与消息目录**不脱节**——两种语言各查各的。
//
// # 为什么要有它
//
// 文档里嵌了大量"真实输出"。CLI 有了两种语言之后，这些输出就有了两份：docs/en 抄英文
// 目录里的文案，docs/zh 抄中文目录里的。check-guide-output.py 真的把 CLI 跑起来逐行
// 比对，但它只覆盖 03-guide 里那 34 个能在任何机器上确定性构造的场景；参考文档、架构
// 文档、排障文档里的输出块它管不着——而这些正是"改了一句文案，文档悄悄过期"最容易发生
// 的地方（真出过：`version` 的两行输出在 CLI 本地化之后，两棵文档树都还写着旧的措辞；
// 英文文档里一句 "use its IP" 与真实输出的 "write its IP" 差了一个词，没有任何东西发现）。
//
// # 判据
//
// 每个围栏块里、以 CLI 输出的行首符号（✅ ❌ ⚠️ 📦 …）开头的行，必须整行匹配**该文档所在
// 语言**的消息目录里的某一行文案（动词当通配符）。符号集合不是手抄的：目录里哪些文案
// 以符号开头，就把哪些符号算进来，再加上错误块渲染器自己加的 ❌ ⚠️ 💡。
//
// 只查带符号的行——那是每条消息的第一行，也是最不该写错的一行。明细行（`键：值`）、
// 建议编号行等没有符号的行不在这里查，它们靠错误码文档的标题守卫与教程输出核对覆盖。
//
// 这条守卫的能力边界：它只知道"这句话是不是目录里的某一句"，不知道"这是哪条命令的输出"——
// 对得上目录里任何一条文案就算过。所以它拦得住改了措辞、抄错了词的过期行，拦不住把
// A 命令的输出错贴到 B 命令下面；后者是 check-guide-output.py 真跑 CLI 逐行比对的活。
//
// 匹配之前，一行可以被拆成几种"候选"：整行、去掉行首符号的、按两个以上空格切开的每一列
// （CLI 会把名字与说明对齐成列）、在 " ✅ " 处切开的两半（进度行与结果拼在同一行）。
// 一个候选如果只是一个词（路径、组件名——那是数据，不是文案）就放行；否则必须匹配目录。
package docfields_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

// outputLineAllow 是允许"带符号却不匹配目录"的行：key 是 "语言|相对路径|那一行里的一段文字"。
// 每一项要写清理由；加进来是有意识的决定，不是顺手。
var outputLineAllow = map[string]string{
	"en|docs/en/06-architecture/09-cli-reference.md|语言已设为 zh":           "brickkit lang 的示例：刻意展示切到中文之后 CLI 真实说的话",
	"en|docs/en/06-architecture/09-cli-reference.md|不支持的语言：fr":          "同上",
	"zh|docs/zh/06-architecture/09-cli-reference.md|Language set to en": "brickkit lang 的示例：切回英文之后 CLI 真实说的话",
}

// templateVerb 匹配目录文案里的动词：%s、%[1]s、%-12[1]s、%5.1[2]f、%%。
var templateVerb = regexp.MustCompile(`%%|%[-+# 0]*\d*(?:\.\d+)?(?:\[\d+\])?[a-zA-Z]`)

// minTemplateLiteral 是一条文案能进匹配池所需的"固定文字"最小宽度：
// "%s（%s）" 这样几乎全是动词的文案会匹配任何东西，放进来就等于没有检查。
// 宽度按显示宽度算（一个汉字算 2）："添加 %s" 的固定文字只有两个字，但信息量不比
// 四个英文字母少。
const minTemplateLiteral = 4

func literalWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x4e00 && r <= 0x9fff:
			w += 2
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			w++
		}
	}
	return w
}

// compileLineTemplates 把目录里每条文案的每一行编译成正则（动词 → 非贪婪通配）。
// 模板与文档里的候选片段两侧都去掉行首符号再比：同一句话有的地方带 ✅ 前缀、有的地方
// 由渲染器加前缀，去掉之后才是同一个东西。
func compileLineTemplates(catalog map[string]string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, message := range catalog {
		for _, line := range strings.Split(message, "\n") {
			line = strings.TrimSpace(line)
			if m := leadingMark(line); m != "" {
				line = strings.TrimSpace(line[len(m):])
			}
			literal := strings.TrimSpace(templateVerb.ReplaceAllString(line, ""))
			if literalWidth(literal) < minTemplateLiteral {
				continue
			}
			var pattern strings.Builder
			pos := 0
			for _, loc := range templateVerb.FindAllStringIndex(line, -1) {
				pattern.WriteString(regexp.QuoteMeta(line[pos:loc[0]]))
				if line[loc[0]:loc[1]] == "%%" {
					pattern.WriteString("%")
				} else {
					pattern.WriteString(".*?")
				}
				pos = loc[1]
			}
			pattern.WriteString(regexp.QuoteMeta(line[pos:]))
			out = append(out, regexp.MustCompile("^"+pattern.String()+"$"))
		}
	}
	return out
}

// leadingMark 取出一行开头的输出符号（一个符号字符，可带变体选择符 U+FE0F）。
// 不是符号开头返回空串。
func leadingMark(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r < 0x2190 { // 字母、数字、ASCII 标点、拉丁扩展都在这之下
		return ""
	}
	// 箭头（→ ←）、制表符（├── │）是排版，CJK 文字与全角标点是文字：都不是"输出符号"
	if (r >= 0x2190 && r <= 0x21ff) || (r >= 0x2500 && r <= 0x257f) ||
		(r >= 0x3000 && r <= 0x303f) || (r >= 0x4e00 && r <= 0x9fff) || (r >= 0xff00 && r <= 0xffef) {
		return ""
	}
	if strings.HasPrefix(s[size:], "\ufe0f") {
		size += len("\ufe0f")
	}
	return s[:size]
}

// outputMarks 收集这门语言目录里所有作为行首符号的符号，再加上错误块渲染器自己拼的三个。
func outputMarks(catalog map[string]string) map[string]bool {
	marks := map[string]bool{"❌": true, "⚠️": true, "💡": true}
	for _, message := range catalog {
		for _, line := range strings.Split(message, "\n") {
			if m := leadingMark(strings.TrimLeft(line, " ")); m != "" {
				marks[m] = true
			}
		}
	}
	return marks
}

// promptOutputBlocks 认的是围栏块自己的第一行——一句 shell 提示符
// "$ brickkit <command>"——不是块里输出内容本身的文字。命中之后，提示符
// 之后、到围栏结束的每一非空行都当成待核对的候选行，即便它不带任何输出符号。
//
// 目前只有 brickkit override 的两个例子在里面（CliOverrideWritten、
// CliOverrideDriftNote 这两条目录文案本身就没有符号前缀，fencedOutputLines
// 光靠 leadingMark 永远选不到它们，这两行例子因此从来没被这条守卫查过——评审
// Minor #3）。
//
// 关键之处：选中的判据是提示符那一行的字面文字，不是输出内容本身的文字。
// 早先的写法反过来——从 CliOverrideWritten/CliOverrideDriftNote 的目录文案里
// 现取"第一个动词之前的固定前缀"当选中条件——看着更"自动跟随目录"，实际是
// 自我拆台：目录文案的用词一变，那个现取的前缀跟着变，doc 里那行原来的旧文字
// 立刻不再匹配新前缀，直接从候选集里消失，测试照样全绿——真正需要拦下的那种
// 漂移反而被静默放过，等于没查。提示符文字（"$ brickkit override"）是一句
// shell 命令，不随目录文案变，选中条件因此不会因为正是这次漂移而跟着失效。
//
// 不是把"任意一行文字都拿去跟目录比"整个打开——那会在全篇文档的大量普通说明
// 文字、以及本节自己的 `$ cat override.yaml` 文件内容块里制造海量假阳性。
// 只按这份显式名单认提示符，选中之后走的还是原来那套 conformsToCatalog，
// 不需要另外的判定逻辑。
var promptOutputBlocks = map[string]bool{
	"$ brickkit override": true,
}

// stripTree 去掉行首的缩进与树形符号（├── └── │）。
func stripTree(s string) string {
	s = strings.TrimLeft(s, " ")
	for _, p := range []string{"├── ", "└── ", "│   "} {
		s = strings.TrimPrefix(s, p)
	}
	return strings.TrimLeft(s, " ")
}

var columnGap = regexp.MustCompile(` {2,}`)

// stripMark 去掉一个片段开头的输出符号，以及列里表示"到"的箭头。
func stripMark(s string, marks map[string]bool) string {
	s = strings.TrimSpace(s)
	if m := leadingMark(s); m != "" && marks[m] {
		s = strings.TrimSpace(s[len(m):])
	}
	return strings.TrimSpace(strings.TrimPrefix(s, "→"))
}

// splitColumns 把一行按两个以上空格切成列（CLI 会把名字与说明对齐成列）。
func splitColumns(line string, marks map[string]bool) []string {
	var out []string
	for _, piece := range columnGap.Split(stripMark(line, marks), -1) {
		out = append(out, stripMark(piece, marks))
	}
	return out
}

// splitAtMarks 在行中间出现的"空格 + 输出符号 + 空格"处切开（进度行与结果拼在同一行：
// "🔍 Checking… ✅ All passed"），符号跟着右半边。
func splitAtMarks(line string, marks map[string]bool) []string {
	line = stripMark(line, marks)
	var out []string
	start := 0
	for i := 1; i < len(line); i++ {
		if line[i-1] != ' ' {
			continue
		}
		if m := leadingMark(line[i:]); m != "" && marks[m] && strings.HasPrefix(line[i+len(m):], " ") {
			out = append(out, stripMark(line[start:i], marks))
			start = i
		}
	}
	return append(out, stripMark(line[start:], marks))
}

// isDataOnly 报告一个片段是不是纯数据（一个词：路径、组件名、文件名）。
func isDataOnly(s string) bool {
	return s == "" || !strings.ContainsAny(s, " \t")
}

func matchesAny(s string, templates []*regexp.Regexp) bool {
	for _, re := range templates {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// conformsToCatalog 见文件头的"判据"：整行（或去符号后的整行）匹配目录；或者拆成列、
// 拆成两半之后，每个片段要么是数据、要么匹配目录；或者整行只是一个词。
func conformsToCatalog(line string, marks map[string]bool, templates []*regexp.Regexp) bool {
	line = stripTree(line)
	whole := stripMark(line, marks)
	if matchesAny(strings.TrimSpace(line), templates) || matchesAny(whole, templates) {
		return true
	}

	groups := [][]string{splitColumns(line, marks), splitAtMarks(line, marks)}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		all := true
		for _, piece := range group {
			if !isDataOnly(piece) && !matchesAny(piece, templates) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return isDataOnly(whole)
}

// docOutputLine 是文档里的一处输出行。
type docOutputLine struct {
	path string
	n    int
	text string
}

// fencedOutputLines 返回文件里所有围栏块内、以输出符号开头的行；再加上
// promptOutputBlocks 认下的那些块——首行是认得的 "$ brickkit ..." 提示符——
// 里提示符之后的每一非空行，即便它不带符号。
func fencedOutputLines(t *testing.T, rel string, marks map[string]bool) []docOutputLine {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot, rel))
	require.NoError(t, err)

	var out []docOutputLine
	inside := false
	firstLineOfBlock := false
	promptOutput := false
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "```") {
			inside = !inside
			firstLineOfBlock = inside
			promptOutput = false
			continue
		}
		if !inside {
			continue
		}
		if firstLineOfBlock {
			firstLineOfBlock = false
			if promptOutputBlocks[strings.TrimSpace(line)] {
				promptOutput = true
				continue // 提示符本身是命令，不是输出，不进候选
			}
		}
		rest := stripTree(line)
		if m := leadingMark(rest); m != "" && marks[m] {
			out = append(out, docOutputLine{path: rel, n: i + 1, text: line})
			continue
		}
		if promptOutput && strings.TrimSpace(line) != "" {
			out = append(out, docOutputLine{path: rel, n: i + 1, text: line})
		}
	}
	return out
}

func docFiles(t *testing.T, lang string) []string {
	t.Helper()
	var out []string
	root := filepath.Join(repoRoot, "docs", lang)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			rel, _ := filepath.Rel(repoRoot, path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(out)
	return out
}

func TestDocOutputLinesConformToCatalog(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		catalog := i18n.CatalogFor(lang)
		templates := compileLineTemplates(catalog)
		require.GreaterOrEqual(t, len(templates), 500,
			"%s：只编出 %d 个行模板——compileLineTemplates 坏了，这条测试的结论不可信", lang, len(templates))
		marks := outputMarks(catalog)
		require.GreaterOrEqual(t, len(marks), 8, "%s：只认出 %d 个输出符号——outputMarks 坏了", lang, len(marks))

		checked := 0
		var offenders []string
		for _, rel := range docFiles(t, string(lang)) {
			for _, ln := range fencedOutputLines(t, rel, marks) {
				checked++
				if conformsToCatalog(ln.text, marks, templates) {
					continue
				}
				allowed := false
				for key := range outputLineAllow {
					parts := strings.SplitN(key, "|", 3)
					if len(parts) == 3 && parts[0] == string(lang) && parts[1] == rel && strings.Contains(ln.text, parts[2]) {
						allowed = true
					}
				}
				if !allowed {
					offenders = append(offenders, rel+":"+strconv.Itoa(ln.n)+"  "+strings.TrimSpace(ln.text))
				}
			}
		}
		require.GreaterOrEqual(t, checked, 200,
			"%s：只查了 %d 行输出——fencedOutputLines 坏了，而不是文档里真的只有这么点", lang, checked)
		assert.Empty(t, offenders,
			"docs/%s 里这些输出行在 %s 消息目录里找不到对应文案。CLI 改了措辞而文档没跟上，"+
				"还是文档里抄错了？以真实输出为准改文档；确有理由（比如刻意缩写）再加进 outputLineAllow 并写清理由。\n%s",
			lang, lang, strings.Join(offenders, "\n"))
	}
}

// brickkit override 的两个例子块——"Wrote override.yaml"、"Drift: ..."——不带
// 任何输出符号（CliOverrideWritten、CliOverrideDriftNote 两条目录文案本身就没有
// 符号前缀）。fencedOutputLines 只认符号开头的行，这两行因此从没被这条守卫看过：
// 目录文案哪天改了措辞，这两块例子会悄悄过期而没有任何东西发现（评审 Minor #3）。
// 这条测试直接读真实文档，钉住这两行确实被选中、也确实与目录一致。
func TestOverrideCliReferenceExamplesAreChecked(t *testing.T) {
	cases := []struct {
		lang i18n.Lang
		path string
		want string
	}{
		{i18n.EN, "docs/en/06-architecture/09-cli-reference.md", "Wrote override.yaml"},
		{i18n.EN, "docs/en/06-architecture/09-cli-reference.md",
			`Drift: demo/hello — "demo/hello"'s mode in brickkit.yaml changed from "enabled" to "" since this override was last confirmed`},
		{i18n.ZH, "docs/zh/06-architecture/09-cli-reference.md", "已写入 override.yaml"},
		{i18n.ZH, "docs/zh/06-architecture/09-cli-reference.md",
			`漂移：demo/hello —— "demo/hello" 在 brickkit.yaml 里的 mode 从 "enabled" 变成了 ""（相对这份覆盖上次确认时）`},
	}
	for _, c := range cases {
		catalog := i18n.CatalogFor(c.lang)
		marks := outputMarks(catalog)
		lines := fencedOutputLines(t, c.path, marks)
		var found bool
		for _, ln := range lines {
			if strings.TrimSpace(ln.text) == c.want {
				found = true
			}
		}
		assert.True(t, found, "%s 里这一行该被这条守卫选中并核对：%q", c.path, c.want)
	}
}

// 匹配器自己要认得出对的、拦得住错的，不能是个永远放行的摆设。
func TestOutputLineMatcher(t *testing.T) {
	catalog := map[string]string{
		"a": "✅ Removed %[1]s",
		"b": "Suggestions:",
		"c": "%[1]s（%[2]s）", // 几乎全是动词：不能进匹配池
		"d": "Can start on their own: %[1]s (no dependencies)",
		"e": "📦 Moved %[1]s",
	}
	templates := compileLineTemplates(catalog)
	require.Len(t, templates, 4, "几乎全是动词的文案不该进匹配池")
	marks := outputMarks(catalog)
	require.True(t, marks["✅"] && marks["❌"] && marks["⚠️"] && marks["💡"])

	assert.True(t, conformsToCatalog("✅ Removed demo/hello@1.0.0", marks, templates), "带值的对得上")
	assert.True(t, conformsToCatalog("   └── ✅ Removed x", marks, templates), "树形前缀不影响")
	assert.False(t, conformsToCatalog("✅ Deleted demo/hello@1.0.0", marks, templates), "措辞改了必须被拦下")
	assert.True(t, conformsToCatalog("✅ brickkit.yaml", marks, templates), "只是一个路径的行是数据，放行")
	assert.True(t, conformsToCatalog("📦 components/a/    → components/.archived/a", marks, templates), "路径对路径的列放行")
	assert.False(t, conformsToCatalog("📦 the source went missing", marks, templates), "一句不在目录里的话不能放行")
	assert.True(t, conformsToCatalog("💡 Can start on their own: x (no dependencies)", marks, templates), "渲染器加的符号要认")
}
