package llmsgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture 造一棵最小的两语言文档树：每页大小可控，好测打包。
func fixture(t *testing.T, pages map[string]string) string {
	t.Helper()
	root := t.TempDir()
	base := map[string]string{
		"AGENTS.md":    "# Agents\n\nSee [the docs](" + en("README.md") + ").\n",
		"AGENTS.zh.md": "# Agents\n",
		"llms.txt":     "# x\n\n<!-- llms:bundles:begin -->\n<!-- llms:bundles:end -->\n",
		"llms.zh.txt":  "# x\n\n<!-- llms:bundles:begin -->\n<!-- llms:bundles:end -->\n",
	}
	for k, v := range pages {
		base[k] = v
	}
	for rel, body := range base {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	return root
}

// en 拼出英文文档树里的路径（写成拼接，免得夹具里的假路径被当成指向真实文档的引用）。
func en(rel string) string { return "docs/" + "en/" + rel }

func pageBody(n int) string { return "# P\n\n" + strings.Repeat("x", n) + "\n" }

func both(rel string, n int) map[string]string {
	return map[string]string{"docs/en/" + rel: pageBody(n), "docs/zh/" + rel: pageBody(n)}
}

func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func find(outs []Output, path string) (Output, bool) {
	for _, o := range outs {
		if o.Path == path {
			return o, true
		}
	}
	return Output{}, false
}

func smallOpts() Options { return Options{Budget: 3000, Core: []string{"00-intro/01-a.md"}} }

// 按阅读顺序装满预算就换下一份；一页不拆；每页只出现一次；核心页不再出现在后面。
func TestGeneratePacksPagesInReadingOrderOnce(t *testing.T) {
	root := fixture(t, merge(both("README.md", 100), both("00-intro/README.md", 100), both("00-intro/01-a.md", 100),
		both("00-intro/02-b.md", 1200), both("01-x/README.md", 1200), both("01-x/01-c.md", 1200)))
	outs, err := Generate(root, smallOpts())
	require.NoError(t, err)

	var all []string
	for _, name := range []string{"llms/en/00-core.md", "llms/en/01.md", "llms/en/02.md"} {
		o, ok := find(outs, name)
		require.True(t, ok, name)
		assert.LessOrEqual(t, len(o.Content), 3000, name)
		for _, line := range strings.Split(string(o.Content), "\n") {
			if strings.HasPrefix(line, "> File: ") {
				all = append(all, strings.TrimPrefix(line, "> File: "))
			}
		}
	}
	assert.Equal(t, []string{"AGENTS.md", en("00-intro/01-a.md"),
		en("README.md"), en("00-intro/README.md"), en("00-intro/02-b.md"),
		en("01-x/README.md"), en("01-x/01-c.md")}, all)
	_, extra := find(outs, "llms/en/03.md")
	assert.False(t, extra)
}

// 每份开头：第几份/共几份、下一份的网址；最后一份说自己是最后一份。
func TestBundleHeaders(t *testing.T) {
	root := fixture(t, merge(both("00-intro/01-a.md", 100), both("01-x/01-c.md", 2500), both("01-x/02-d.md", 2500)))
	outs, err := Generate(root, smallOpts())
	require.NoError(t, err)
	first, _ := find(outs, "llms/en/01.md")
	assert.Contains(t, string(first.Content), "part 1 of 2")
	assert.Contains(t, string(first.Content), "Next: "+RawBase+"llms/en/02.md")
	last, _ := find(outs, "llms/en/02.md")
	assert.Contains(t, string(last.Content), "This is the last part.")
	core, _ := find(outs, "llms/en/00-core.md")
	assert.Contains(t, string(core.Content), RawBase+"llms/en/01.md")
}

// 预算放不下核心合集、或者单独一页就超预算：报错点名，不截断。
func TestGenerateFailsLoudlyOverBudget(t *testing.T) {
	root := fixture(t, merge(both("00-intro/01-a.md", 5000)))
	_, err := Generate(root, smallOpts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "00-core.md")

	root = fixture(t, merge(both("00-intro/01-a.md", 100), both("01-x/01-big.md", 5000)))
	_, err = Generate(root, smallOpts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), en("01-x/01-big.md"))
}

// llms.txt 的合集清单写在两个标记之间；标记没了就报错，不另追加一份。
func TestUpdateIndexNeedsMarkers(t *testing.T) {
	root := fixture(t, merge(both("00-intro/01-a.md", 100), both("01-x/01-c.md", 100)))
	outs, err := Generate(root, smallOpts())
	require.NoError(t, err)
	idx, _ := find(outs, "llms.txt")
	assert.Contains(t, string(idx.Content), "- [llms/en/00-core.md](llms/en/00-core.md)")
	assert.Contains(t, string(idx.Content), "- [llms/en/01.md](llms/en/01.md)")

	require.NoError(t, os.WriteFile(filepath.Join(root, "llms.txt"), []byte("# x\n"), 0o644))
	_, err = Generate(root, smallOpts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llms.txt")
}

// 同样的输入，逐字节相同的输出；Write 之后 Check 无问题；删掉一页后，多出来的那份旧文件被 Check 报出、被 Write 删掉。
func TestCheckReportsStaleExtraFile(t *testing.T) {
	root := fixture(t, merge(both("00-intro/01-a.md", 100), both("01-x/01-c.md", 2500), both("01-x/02-d.md", 2500)))
	outs, err := Generate(root, smallOpts())
	require.NoError(t, err)
	again, err := Generate(root, smallOpts())
	require.NoError(t, err)
	assert.Equal(t, outs, again)
	require.NoError(t, Write(root, outs))
	assert.Empty(t, Check(root, outs))

	require.NoError(t, os.Remove(filepath.Join(root, en("01-x/02-d.md"))))
	require.NoError(t, os.Remove(filepath.Join(root, "docs/"+"zh/01-x/02-d.md")))
	outs, err = Generate(root, smallOpts())
	require.NoError(t, err)
	problems := Check(root, outs)
	assert.Contains(t, strings.Join(problems, "\n"), "llms/en/02.md")
	require.NoError(t, Write(root, outs))
	assert.NoFileExists(t, filepath.Join(root, "llms/en/02.md"))
	assert.Empty(t, Check(root, outs))
}

// 页内相对链接改写成相对合集目录的写法：GitHub 上点得开，也看得出是哪个文件。
func TestRewriteLinks(t *testing.T) {
	cases := map[string]struct{ page, in, want string }{
		"sibling":       {en("02-g/03-x.md"), "[a](04-y.md)", "[a](../../" + en("02-g/04-y.md") + ")"},
		"parent+anchor": {en("02-g/03-x.md"), "[a](../01-t/04-d.md#x)", "[a](../../" + en("01-t/04-d.md") + "#x)"},
		"outside docs":  {en("00-intro/02-q.md"), "[i](../../../README.md#install)", "[i](../../README.md#install)"},
		"from AGENTS":   {"AGENTS.md", "[d](" + en("README.md") + ")", "[d](../../" + en("README.md") + ")"},
		"same-page":     {en("a.md"), "[s](#top)", "[s](#top)"},
		"external":      {en("a.md"), "[e](https://example.com/x.md)", "[e](https://example.com/x.md)"},
		"two on a line": {en("a.md"), "[a](b.md) and [c](d.md)", "[a](../../" + en("b.md") + ") and [c](../../" + en("d.md") + ")"},
	}
	for name, c := range cases {
		assert.Equal(t, c.want, RewriteLinks(c.in, c.page, "llms/en"), name)
	}
}

// 代码块与行内代码里长得像链接的东西不动。
func TestRewriteLinksSkipsCode(t *testing.T) {
	fence := strings.Repeat("`", 3)
	in := fence + "markdown\n[a](b.md)\n" + fence + "\nUse `[x](y.md)` here and [z](w.md).\n"
	want := fence + "markdown\n[a](b.md)\n" + fence + "\nUse `[x](y.md)` here and [z](../../" + en("w.md") + ").\n"
	assert.Equal(t, want, RewriteLinks(in, en("p.md"), "llms/en"))
}

// 预算是硬上限：试装时按"最后一份"量，会漏掉真实的"下一份"那一行（Final review I3）。
// 在一段页面大小里逐字节扫，真实产出的每一份都不得超过预算。
func TestEveryOutputStaysWithinTheBudget(t *testing.T) {
	for size := 1200; size <= 1500; size++ {
		root := fixture(t, merge(both("00-intro/01-a.md", 100), both("01-x/01-c.md", size),
			both("01-x/02-d.md", size), both("01-x/03-e.md", size)))
		outs, err := Generate(root, smallOpts())
		require.NoError(t, err)
		for _, o := range outs {
			if strings.HasPrefix(o.Path, "llms/") {
				require.LessOrEqual(t, len(o.Content), 3000, "%s with pages of %d bytes", o.Path, size)
			}
		}
	}
}

// 合集开头要说清楚两种路径：File 与 Contains 里的是相对仓库根，页内链接是相对这份合集自己（Final review）。
func TestBundleHeaderExplainsBothKindsOfPaths(t *testing.T) {
	root := fixture(t, merge(both("00-intro/01-a.md", 100), both("01-x/01-c.md", 100)))
	outs, err := Generate(root, smallOpts())
	require.NoError(t, err)
	en, _ := find(outs, "llms/en/01.md")
	assert.Contains(t, string(en.Content), "links inside pages are relative to this file")
	zh, _ := find(outs, "llms/zh/01.md")
	assert.Contains(t, string(zh.Content), "页内链接相对这份合集自己")
}

// 只有一页的分卷，清单里只写那一页，不写成"X … X"。
func TestIndexNamesASinglePageOnce(t *testing.T) {
	root := fixture(t, merge(both("00-intro/01-a.md", 100), both("01-x/01-c.md", 2500), both("01-x/02-d.md", 2500)))
	outs, err := Generate(root, smallOpts())
	require.NoError(t, err)
	idx, _ := find(outs, "llms.txt")
	assert.Contains(t, string(idx.Content), "- [llms/en/01.md](llms/en/01.md): "+en("01-x/01-c.md")+"\n")
}

// 其他几种链接写法也要改写：带标题的、引用式定义、HTML 的 href / src；~~~ 围起来的代码块不动。
func TestRewriteLinksOtherForms(t *testing.T) {
	page := en("02-g/03-x.md")
	cases := map[string]struct{ in, want string }{
		"title":       {`[a](04-y.md "Y")`, `[a](../../` + en("02-g/04-y.md") + ` "Y")`},
		"reference":   {"[y]: 04-y.md#top", "[y]: ../../" + en("02-g/04-y.md") + "#top"},
		"html href":   {`<a href="04-y.md">y</a>`, `<a href="../../` + en("02-g/04-y.md") + `">y</a>`},
		"html src":    {`<img src="../img/a.png">`, `<img src="../../` + en("img/a.png") + `">`},
		"html extern": {`<a href="https://x.io/a.md">x</a>`, `<a href="https://x.io/a.md">x</a>`},
		"tilde fence": {"~~~\n[a](b.md)\n~~~", "~~~\n[a](b.md)\n~~~"},
		"mixed fence": {strings.Repeat("`", 3) + "\n~~~\n[a](b.md)\n" + strings.Repeat("`", 3), strings.Repeat("`", 3) + "\n~~~\n[a](b.md)\n" + strings.Repeat("`", 3)},
	}
	for name, c := range cases {
		assert.Equal(t, c.want, RewriteLinks(c.in, page, "llms/en"), name)
	}
}

// 被 git 忽略的 .md（放在文档树里的本地笔记）不进合集：合集是要发布出去的。
func TestIgnoredPagesStayOut(t *testing.T) {
	root := fixture(t, merge(both("00-intro/01-a.md", 100), both("01-x/01-c.md", 100), both("01-x/notes.md", 100)))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("notes.md\n"), 0o644))
	cmd := exec.Command("git", "init", "-q", root)
	require.NoError(t, cmd.Run())
	outs, err := Generate(root, smallOpts())
	require.NoError(t, err)
	for _, o := range outs {
		assert.NotContains(t, string(o.Content), "notes.md", o.Path)
	}
}
