# Documentation Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One entry per reader — `AGENTS.md` with a doc map and a code map for local development, `llms.txt` with
"read everything / answer a question" routes and generated ≤100 KB bundles for web AIs, README navigation for GitHub
readers — with relative links everywhere hand-written and guards that keep all of it current.

**Architecture:** A Go generator (`internal/llmsgen` + `cmd/gen-llms`, the `gen-msgid` pattern) builds `llms/<lang>/`
bundles and the bundle list inside `llms*.txt`; `make check-llms` (in `make lint`) fails when they are stale, and a
versioned `.githooks/pre-commit` regenerates and stages them on commit. Hand-written entry files switch to
repository-relative links, guarded by check-docs (raw prefix once) and check-docs-bilingual (relative links resolve).
`AGENTS*.md` gain a code map guarded by a Go test in `tests/docfields`.

**Tech Stack:** Go 1.22 (generator, guards), Python 3 (existing `scripts/check-*.py`), POSIX sh (hook), Make.

**Spec:** `docs/superpowers/specs/2026-09-30-doc-navigation-design.md`

## Global Constraints

- Web prefix, stated once per entry file: `https://raw.githubusercontent.com/brickKit/brickKit/main/`
- Bundle budget: **100,000 bytes per file**, headers included; never truncate — fail loudly.
- Bundle files: `llms/<lang>/00-core.md`, then `01.md` … `NN.md` (two digits); languages `en` (`AGENTS.md`,
  `llms.txt`) and `zh` (`AGENTS.zh.md`, `llms.zh.txt`).
- Core pages (relative to `docs/<lang>/`, after `AGENTS*.md`): `00-intro/01-what-is-brickkit.md`,
  `00-intro/02-quick-start.md`, `00-intro/04-core-concepts.md`, `00-intro/06-fractal-architecture.md`,
  `01-three-layers/01-overview.md`, `01-three-layers/08-resolution-priority.md`, `01-three-layers/09-field-reference.md`.
- Every page appears exactly once across `00…NN`; a page is never split.
- Links inside bundles are rewritten **relative to the bundle's own directory** (`../../docs/en/…`) — they resolve on
  GitHub and via the raw base, and still name the file. (Refines spec §7 "repository-relative": a
  `docs/en/…` link inside `llms/en/` would resolve to `llms/en/docs/en/…` on GitHub.)
- Generated `llms/` is excluded from the doc scanners (check-docs, check-cli-docs, check-doc-tree, `tests/docfields`);
  `check-llms` proves it equals a fresh generation.
- Bundle-list markers in `llms*.txt`: `<!-- llms:bundles:begin -->` / `<!-- llms:bundles:end -->`.
- Code map: `## §10 Code map` (en) / `## §10 代码地图` (zh); doc map: `## §9 Doc map` / `## §9 文档地图`.
- Hand-written content links only into `docs/`, `AGENTS*`, `llms*`, `CONTRIBUTING*`, `README*` (maintainer rule).
- Commit after each task, trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`; run
  `.tools/bin/golangci-lint run ./...` and `make -k lint` before each commit; heavy test runs under
  `systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0`.

## Review Focus

1. **A bundle page whose links point outside `docs/`** (`../../AGENTS.md`, `../../CONTRIBUTING.md`, `../../README.md#install`) — rewritten relative to `llms/<lang>/` and still resolving. (Task 1, `TestRewriteLinks` cases.)
2. **Links inside fenced code blocks and inline code** (the component-doc spec page shows Markdown in a fence) — left untouched. (Task 1, `TestRewriteLinksSkipsCode`.)
3. **A part count crossing a digit boundary or shrinking** (a doc deleted, `llms/en/09.md` no longer produced) — the stale file is deleted by `generate` and reported by `-check`. (Task 1, `TestCheckReportsStaleExtraFile`.)
4. **Committing a new, untracked doc page** — the hook refuses rather than bundling a page the commit doesn't contain. (Task 5, untracked case.)
5. **`llms.txt` without its markers** (someone deletes them) — the generator fails loudly instead of appending a second list. (Task 1, `TestUpdateIndexNeedsMarkers`.)

---

### Task 1: The bundle generator library (`internal/llmsgen`)

**Files:**
- Create: `internal/llmsgen/llmsgen.go`, `internal/llmsgen/links.go`, `internal/llmsgen/llmsgen_test.go`

**Interfaces:**
- Produces:
  - `type Lang struct { Code, Agents, Index string }`; `var Langs = []Lang{{"en", "AGENTS.md", "llms.txt"}, {"zh", "AGENTS.zh.md", "llms.zh.txt"}}`
  - `type Options struct { Budget int; Core []string }`; `func DefaultOptions() Options`
  - `type Output struct { Path string; Content []byte }` (Path repository-relative, `/`-separated)
  - `func Generate(root string, opt Options) ([]Output, error)` — all bundle files for both languages plus the updated `llms*.txt`
  - `func Write(root string, outs []Output) error` — writes outputs, deletes stale `llms/<lang>/*.md`
  - `func Check(root string, outs []Output) []string` — problems: differing file, missing file, stale extra file
  - `func RewriteLinks(body, pagePath, bundleDir string) string`
  - `const RawBase = "https://raw.githubusercontent.com/brickKit/brickKit/main/"`

- [ ] **Step 1: Write the failing tests** — `internal/llmsgen/llmsgen_test.go`:

```go
package llmsgen

import (
	"os"
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
		"AGENTS.md":    "# Agents\n\nSee [the docs](docs/en/README.md).\n",
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

func page(n int) string { return "# P\n\n" + strings.Repeat("x", n) + "\n" }

func both(rel string, n int) map[string]string {
	return map[string]string{"docs/en/" + rel: page(n), "docs/zh/" + rel: page(n)}
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
	assert.Equal(t, []string{"AGENTS.md", "docs/en/00-intro/01-a.md",
		"docs/en/README.md", "docs/en/00-intro/README.md", "docs/en/00-intro/02-b.md",
		"docs/en/01-x/README.md", "docs/en/01-x/01-c.md"}, all)
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
	assert.Contains(t, err.Error(), "docs/en/01-x/01-big.md")
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

	require.NoError(t, os.Remove(filepath.Join(root, "docs/en/01-x/02-d.md")))
	require.NoError(t, os.Remove(filepath.Join(root, "docs/zh/01-x/02-d.md")))
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
		"sibling":        {"docs/en/02-g/03-x.md", "[a](04-y.md)", "[a](../../docs/en/02-g/04-y.md)"},
		"parent+anchor":  {"docs/en/02-g/03-x.md", "[a](../01-t/04-d.md#x)", "[a](../../docs/en/01-t/04-d.md#x)"},
		"outside docs":   {"docs/en/00-intro/02-q.md", "[i](../../../README.md#install)", "[i](../../README.md#install)"},
		"from AGENTS":    {"AGENTS.md", "[d](docs/en/README.md)", "[d](../../docs/en/README.md)"},
		"same-page":      {"docs/en/a.md", "[s](#top)", "[s](#top)"},
		"external":       {"docs/en/a.md", "[e](https://example.com/x.md)", "[e](https://example.com/x.md)"},
		"two on a line":  {"docs/en/a.md", "[a](b.md) and [c](d.md)", "[a](../../docs/en/b.md) and [c](../../docs/en/d.md)"},
	}
	for name, c := range cases {
		assert.Equal(t, c.want, RewriteLinks(c.in, c.page, "llms/en"), name)
	}
}

// 代码块与行内代码里长得像链接的东西不动。
func TestRewriteLinksSkipsCode(t *testing.T) {
	fence := strings.Repeat("`", 3)
	in := fence + "markdown\n[a](b.md)\n" + fence + "\nUse `[x](y.md)` here and [z](w.md).\n"
	want := fence + "markdown\n[a](b.md)\n" + fence + "\nUse `[x](y.md)` here and [z](../../docs/en/w.md).\n"
	assert.Equal(t, want, RewriteLinks(in, "docs/en/p.md", "llms/en"))
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/llmsgen/ -count=1`
Expected: FAIL — `undefined: Generate` (package does not compile).

- [ ] **Step 3: Implement** — `internal/llmsgen/links.go`:

```go
package llmsgen

import (
	"path"
	"regexp"
	"strings"
)

var (
	mdLink     = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	inlineCode = regexp.MustCompile("`[^`]*`")
)

// RewriteLinks 把 pagePath（仓库相对路径）里的相对链接改写成相对 bundleDir 的写法。
// 外部链接、页内锚点、代码块与行内代码里的内容不动。
func RewriteLinks(body, pagePath, bundleDir string) string {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = rewriteLine(line, pagePath, bundleDir)
	}
	return strings.Join(lines, "\n")
}

func rewriteLine(line, pagePath, bundleDir string) string {
	codes := inlineCode.FindAllStringIndex(line, -1)
	inCode := func(at int) bool {
		for _, c := range codes {
			if at >= c[0] && at < c[1] {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	last := 0
	for _, m := range mdLink.FindAllStringSubmatchIndex(line, -1) {
		if inCode(m[0]) {
			continue
		}
		target := line[m[4]:m[5]]
		b.WriteString(line[last:m[4]])
		b.WriteString(rewriteTarget(target, pagePath, bundleDir))
		last = m[5]
	}
	b.WriteString(line[last:])
	return b.String()
}

func rewriteTarget(target, pagePath, bundleDir string) string {
	if target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") ||
		strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return target
	}
	file, frag, hasFrag := strings.Cut(target, "#")
	repoPath := path.Clean(path.Join(path.Dir(pagePath), file))
	rel := relative(bundleDir, repoPath)
	if hasFrag {
		return rel + "#" + frag
	}
	return rel
}

// relative 是从 fromDir 到 to 的相对路径（两者都是仓库相对、/ 分隔）。
func relative(fromDir, to string) string {
	from := strings.Split(path.Clean(fromDir), "/")
	dst := strings.Split(to, "/")
	i := 0
	for i < len(from) && i < len(dst)-1 && from[i] == dst[i] {
		i++
	}
	parts := make([]string, 0, len(from)-i+len(dst)-i)
	for range from[i:] {
		parts = append(parts, "..")
	}
	parts = append(parts, dst[i:]...)
	return strings.Join(parts, "/")
}
```

`internal/llmsgen/llmsgen.go`:

```go
// Package llmsgen 生成给网页端 AI 读的文档合集：llms/<lang>/00-core.md 是核心合集，
// 01.md … NN.md 按阅读顺序装下其余每一页（每份不超过预算，一页不拆，每页只出现一次），
// 并把合集清单写进 llms.txt / llms.zh.txt 的标记之间。生成的文件提交进仓库，
// make check-llms 拦住"改了文档忘了重新生成"。
package llmsgen

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// RawBase 是网页上读仓库文件的前缀：合集是给网页 AI 顺着读的，导航用绝对网址。
	RawBase     = "https://raw.githubusercontent.com/brickKit/brickKit/main/"
	markerBegin = "<!-- llms:bundles:begin -->"
	markerEnd   = "<!-- llms:bundles:end -->"
)

// Lang 是一种语言的文档树与它的两份入口文件。
type Lang struct{ Code, Agents, Index string }

var Langs = []Lang{{"en", "AGENTS.md", "llms.txt"}, {"zh", "AGENTS.zh.md", "llms.zh.txt"}}

// Options：每份的字节预算，以及核心合集在 AGENTS 之后收哪几页（相对 docs/<lang>/）。
type Options struct {
	Budget int
	Core   []string
}

func DefaultOptions() Options {
	return Options{Budget: 100_000, Core: []string{
		"00-intro/01-what-is-brickkit.md", "00-intro/02-quick-start.md", "00-intro/04-core-concepts.md",
		"00-intro/06-fractal-architecture.md", "01-three-layers/01-overview.md",
		"01-three-layers/08-resolution-priority.md", "01-three-layers/09-field-reference.md",
	}}
}

// Output 是一份要写出的文件（仓库相对路径）。
type Output struct {
	Path    string
	Content []byte
}

type page struct{ path, body string }

// Generate 算出两种语言的全部合集与更新后的 llms*.txt。
func Generate(root string, opt Options) ([]Output, error) {
	var outs []Output
	for _, l := range Langs {
		bundles, err := generateLang(root, l, opt)
		if err != nil {
			return nil, err
		}
		outs = append(outs, bundles...)
		idx, err := updateIndex(root, l, bundles)
		if err != nil {
			return nil, err
		}
		outs = append(outs, idx)
	}
	return outs, nil
}

func generateLang(root string, l Lang, opt Options) ([]Output, error) {
	dir := "llms/" + l.Code
	all, err := docPages(root, l.Code)
	if err != nil {
		return nil, err
	}
	agents, err := os.ReadFile(filepath.Join(root, l.Agents))
	if err != nil {
		return nil, err
	}
	coreSet := map[string]bool{}
	core := []page{{l.Agents, string(agents)}}
	for _, rel := range opt.Core {
		p := "docs/" + l.Code + "/" + rel
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return nil, fmt.Errorf("core page %s: %w", p, err)
		}
		core = append(core, page{p, string(body)})
		coreSet[p] = true
	}
	var rest []page
	for _, p := range all {
		if !coreSet[p.path] {
			rest = append(rest, p)
		}
	}

	// 先按预算装箱（用两位数的占位页码与"下一份"一行算大小，真实的只会更短），再按真实份数渲染
	var parts [][]page
	var cur []page
	for _, p := range rest {
		if len(render(l, dir, 99, 99, []page{p}, false)) > opt.Budget {
			return nil, fmt.Errorf("%s alone is %d bytes, over the %d-byte bundle budget: split the page",
				p.path, len(render(l, dir, 99, 99, []page{p}, false)), opt.Budget)
		}
		if cur != nil && len(render(l, dir, 99, 99, append(append([]page{}, cur...), p), false)) > opt.Budget {
			parts, cur = append(parts, cur), nil
		}
		cur = append(cur, p)
	}
	if cur != nil {
		parts = append(parts, cur)
	}

	coreOut := render(l, dir, 0, len(parts), core, true)
	if len(coreOut) > opt.Budget {
		return nil, fmt.Errorf("%s/00-core.md would be %d bytes, over the %d-byte budget: drop a core page",
			dir, len(coreOut), opt.Budget)
	}
	outs := []Output{{dir + "/00-core.md", []byte(coreOut)}}
	for i, ps := range parts {
		outs = append(outs, Output{fmt.Sprintf("%s/%02d.md", dir, i+1), []byte(render(l, dir, i+1, len(parts), ps, false))})
	}
	return outs, nil
}

// docPages 按阅读顺序列出 docs/<lang>/ 下的每一页：README.md 在前，其余按名字（带编号即顺序）。
func docPages(root, lang string) ([]page, error) {
	var out []page
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		sort.SliceStable(entries, func(i, j int) bool {
			a, b := entries[i].Name(), entries[j].Name()
			if (a == "README.md") != (b == "README.md") {
				return a == "README.md"
			}
			return a < b
		})
		for _, e := range entries {
			p := rel + "/" + e.Name()
			switch {
			case e.IsDir():
				if err := walk(p); err != nil {
					return err
				}
			case strings.HasSuffix(e.Name(), ".md"):
				body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
				if err != nil {
					return err
				}
				out = append(out, page{p, string(body)})
			}
		}
		return nil
	}
	return out, walk("docs/" + lang)
}

type texts struct{ lang, core, part, contains, paths, next, last, rest string }

var textsByLang = map[string]texts{
	"en": {"English", "core — read this first", "part %d of %d", "Contains", "Paths below are relative to the repository root",
		"Next", "This is the last part.", "The rest of the documentation, every page once, in reading order"},
	"zh": {"中文", "核心合集——先读这一份", "第 %d 份，共 %d 份", "包含", "下面的路径都相对仓库根目录",
		"下一份", "这是最后一份。", "其余全部文档，每页一次，按阅读顺序"},
}

func render(l Lang, dir string, n, total int, pages []page, core bool) string {
	t := textsByLang[l.Code]
	var b strings.Builder
	if core {
		fmt.Fprintf(&b, "# BrickKit — %s (%s)\n\n", t.core, t.lang)
	} else {
		fmt.Fprintf(&b, "# BrickKit — "+t.part+" (%s)\n\n", n, total, t.lang)
	}
	names := make([]string, len(pages))
	for i, p := range pages {
		names[i] = p.path
	}
	fmt.Fprintf(&b, "%s: %s\n\n", t.contains, strings.Join(names, ", "))
	fmt.Fprintf(&b, "%s: %s\n\n", t.paths, RawBase)
	switch {
	case core && total > 0:
		fmt.Fprintf(&b, "%s: %s: %s%s/01.md … %02d.md\n", t.next, t.rest, RawBase, dir, total)
	case !core && n < total:
		fmt.Fprintf(&b, "%s: %s%s/%02d.md\n", t.next, RawBase, dir, n+1)
	default:
		fmt.Fprintf(&b, "%s\n", t.last)
	}
	for _, p := range pages {
		body := RewriteLinks(p.body, p.path, dir)
		fmt.Fprintf(&b, "\n---\n\n> File: %s\n\n%s", p.path, strings.TrimRight(body, "\n")+"\n")
	}
	return b.String()
}

// updateIndex 把合集清单写进 llms*.txt 的标记之间。
func updateIndex(root string, l Lang, bundles []Output) (Output, error) {
	raw, err := os.ReadFile(filepath.Join(root, l.Index))
	if err != nil {
		return Output{}, err
	}
	s := string(raw)
	i, j := strings.Index(s, markerBegin), strings.Index(s, markerEnd)
	if i < 0 || j < i {
		return Output{}, fmt.Errorf("%s: the bundle list markers %s … %s are missing", l.Index, markerBegin, markerEnd)
	}
	var b strings.Builder
	b.WriteString(markerBegin + "\n")
	for _, o := range bundles {
		first, last := contains(o.Content)
		fmt.Fprintf(&b, "- [%s](%s): %s … %s\n", o.Path, o.Path, first, last)
	}
	out := s[:i] + b.String() + s[j:]
	return Output{l.Index, []byte(out)}, nil
}

// contains 取合集头部"包含："一行里的第一页与最后一页。
func contains(content []byte) (first, last string) {
	line, _, _ := bytes.Cut(bytes.SplitN(content, []byte("\n\n"), 3)[1], []byte("\n"))
	_, list, _ := strings.Cut(string(line), ": ")
	names := strings.Split(list, ", ")
	return names[0], names[len(names)-1]
}

// Write 写出全部文件，并删掉 llms/<lang>/ 下这次没产出的旧合集。
func Write(root string, outs []Output) error {
	for _, o := range outs {
		p := filepath.Join(root, filepath.FromSlash(o.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, o.Content, 0o644); err != nil {
			return err
		}
	}
	for _, stale := range staleFiles(root, outs) {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(stale))); err != nil {
			return err
		}
	}
	return nil
}

// Check 列出与生成结果不一致的地方：内容不同、缺文件、多出来的旧合集。
func Check(root string, outs []Output) []string {
	var problems []string
	for _, o := range outs {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(o.Path)))
		switch {
		case err != nil:
			problems = append(problems, o.Path+": missing")
		case !bytes.Equal(got, o.Content):
			problems = append(problems, o.Path+": differs from a fresh generation")
		}
	}
	for _, stale := range staleFiles(root, outs) {
		problems = append(problems, stale+": no longer generated")
	}
	return problems
}

func staleFiles(root string, outs []Output) []string {
	want := map[string]bool{}
	for _, o := range outs {
		want[o.Path] = true
	}
	var stale []string
	for _, l := range Langs {
		entries, _ := os.ReadDir(filepath.Join(root, "llms", l.Code))
		for _, e := range entries {
			p := path.Join("llms", l.Code, e.Name())
			if strings.HasSuffix(e.Name(), ".md") && !want[p] {
				stale = append(stale, p)
			}
		}
	}
	sort.Strings(stale)
	return stale
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/llmsgen/ -count=1`
Expected: PASS (all seven tests).

- [ ] **Step 5: Commit**

```bash
git add internal/llmsgen
git commit -m "feat(llmsgen): documentation bundles under a byte budget, one page once, links still naming their files"
```

---

### Task 2: `cmd/gen-llms`, Make targets, first generation, scanners skip `llms/`

**Files:**
- Create: `cmd/gen-llms/main.go`, `internal/llmsgen/current_test.go`, `.gitattributes` (or modify if present)
- Modify: `Makefile`, `llms.txt`, `llms.zh.txt` (insert the empty marker pair), `scripts/check-docs.py`,
  `scripts/check-cli-docs.py`, `scripts/check-doc-tree.py`, `tests/docfields/docfields_test.go`,
  `tests/docfields/skeleton_test.go` (add `llms/` to their exclusions)

**Interfaces:**
- Consumes: `llmsgen.Generate`, `llmsgen.Write`, `llmsgen.Check`, `llmsgen.DefaultOptions`.
- Produces: `make generate-llms`, `make check-llms` (in `lint`), committed `llms/en/*.md`, `llms/zh/*.md`.

- [ ] **Step 1: The currency test** — `internal/llmsgen/current_test.go`:

```go
package llmsgen

import (
	"strings"
	"testing"
)

// 签入的合集就是这份文档树生成的那一份：改了文档要跑 make generate-llms（或装好提交钩子）。
func TestGeneratedBundlesAreCurrent(t *testing.T) {
	outs, err := Generate("../..", DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if problems := Check("../..", outs); len(problems) > 0 {
		t.Fatalf("llms/ is stale — run make generate-llms:\n%s", strings.Join(problems, "\n"))
	}
}
```

Run: `go test ./internal/llmsgen/ -run TestGeneratedBundlesAreCurrent -count=1` — Expected: FAIL (markers missing in
`llms.txt`).

- [ ] **Step 2: The command** — `cmd/gen-llms/main.go`:

```go
// cmd/gen-llms：生成 llms/<lang>/ 下的文档合集，并更新 llms.txt / llms.zh.txt 里的合集清单。
// 在仓库根目录运行；-check 只比较、不写，与生成结果不一致时以非零退出。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/brickkit/brickkit/internal/llmsgen"
)

func main() {
	check := flag.Bool("check", false, "compare instead of writing; exit 1 when llms/ is stale")
	flag.Parse()
	outs, err := llmsgen.Generate(".", llmsgen.DefaultOptions())
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-llms:", err)
		os.Exit(1)
	}
	if *check {
		if problems := llmsgen.Check(".", outs); len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "gen-llms:", p)
			}
			fmt.Fprintln(os.Stderr, "gen-llms: run make generate-llms")
			os.Exit(1)
		}
		return
	}
	if err := llmsgen.Write(".", outs); err != nil {
		fmt.Fprintln(os.Stderr, "gen-llms:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Makefile** — next to `generate-msgid`:

```make
# llms/<lang>/ 的文档合集与 llms*.txt 里的合集清单是生成的：改了 docs/ 或 AGENTS*.md 之后跑 generate-llms
# （make hooks 装好的提交钩子会自动跑）；check-llms 在 lint 里拦住"忘了跑"。
.PHONY: generate-llms
generate-llms: ## 重新生成 llms/ 下的文档合集与 llms*.txt 的合集清单（改了文档之后跑）
	@$(GO) run ./cmd/gen-llms

.PHONY: check-llms
check-llms: ## 检查签入的 llms/ 合集就是当前文档生成的那一份
	@$(GO) test ./internal/llmsgen/ -count=1
```

and add `check-llms` to the `lint:` prerequisites (after `check-msgid`), extending its `##` description with
`+ 文档合集`.

- [ ] **Step 4: Markers** — in `llms.txt`, insert right after the "If the question you were asked is in Chinese …"
paragraph:

```
## Read everything

<!-- llms:bundles:begin -->
<!-- llms:bundles:end -->
```

and in `llms.zh.txt` after its opening paragraphs:

```
## 读完全部

<!-- llms:bundles:begin -->
<!-- llms:bundles:end -->
```

(Task 3 writes the prose around them.)

- [ ] **Step 5: Exclusions** — generated bundles duplicate the docs and are proven by `check-llms`:
  - `scripts/check-docs.py`, `scripts/check-cli-docs.py`, `scripts/check-doc-tree.py`: add `"llms/"` to
    `EXCLUDED_PREFIXES`.
  - `tests/docfields/docfields_test.go`: `hasAnyPrefix(rel, "archive/", "docs/superpowers/", ".superpowers/", "llms/")`;
    `tests/docfields/skeleton_test.go`: add `"llms/"` to its prefix list.
  - `.gitattributes`: `llms/** linguist-generated=true` (GitHub collapses them in diffs).

- [ ] **Step 6: Generate and verify**

Run: `make generate-llms && ls llms/en llms/zh && wc -c llms/en/* | tail -3 && make check-llms`
Expected: `00-core.md` plus `01.md`…`NN.md` in each language, none over 100000 bytes, `check-llms` ok.
Then `make -k lint` → 0.

- [ ] **Step 7: Commit**

```bash
git add cmd/gen-llms internal/llmsgen Makefile llms llms.txt llms.zh.txt .gitattributes scripts tests/docfields
git commit -m "feat: make generate-llms builds the documentation bundles; check-llms keeps them current"
```

---

### Task 3: Relative links in the entry files, two routes in `llms*.txt`, the raw-prefix guard

**Files:**
- Modify: `llms.txt`, `llms.zh.txt`, `AGENTS.md`, `AGENTS.zh.md` (top line + §9 Doc map),
  `scripts/check-docs-bilingual.py`, `scripts/check-docs.py`

**Interfaces:**
- Consumes: the markers and bundles from Task 2.
- Produces: `§9 Doc map` / `§9 文档地图` in `AGENTS*.md`; check-docs rule `check_raw_prefix_once`.

- [ ] **Step 1: The raw-prefix guard first (RED)** — in `scripts/check-docs.py`:

```python
# 手写的入口文件只用相对路径；网页上的前缀只在开头说一次（本地的 AI 要的是能直接打开的路径）
RAW_PREFIX = "https://raw.githubusercontent.com/brickKit/brickKit/main/"
RAW_ONCE_FILES = ("AGENTS.md", "AGENTS.zh.md", "llms.txt", "llms.zh.txt")


def check_raw_prefix_once():
    """⑤ 入口文件里网页前缀只出现一次。"""
    bad = []
    for path in RAW_ONCE_FILES:
        n = sum(line.count(RAW_PREFIX) for line in read_lines(path) or [])
        if n != 1:
            bad.append((path, 0, f"网页前缀出现了 {n} 次，应当只在开头说明一次、其余一律写相对路径"))
    return bad
```

wire it in `main()` as `failed |= report("入口文件的网页前缀", check_raw_prefix_once())`, add ⑤ to the module
docstring, and add to `self_check()`:

```python
    if (RAW_PREFIX + "a " + RAW_PREFIX + "b").count(RAW_PREFIX) != 2:
        problems.append("网页前缀的计数坏了")
```

Run: `python3 scripts/check-docs.py` — Expected: `❌ 入口文件的网页前缀：4 处` (97, 97, 19, 19 occurrences).

- [ ] **Step 2: `llms.txt` / `llms.zh.txt`** — replace every `](https://raw.githubusercontent.com/brickKit/brickKit/main/`
with `](`; rewrite the opening to:

```
# BrickKit

> <existing summary paragraph, unchanged>

Repository: https://github.com/brickKit/brickKit | License: Apache-2.0

Paths below are relative to the repository root. On the web, prefix them with
https://raw.githubusercontent.com/brickKit/brickKit/main/ . **If the question you were asked is in Chinese, read
[`llms.zh.txt`](llms.zh.txt) instead** — the same kind of index, written for the Chinese documentation tree.

## Read everything

These files are the whole documentation, every page once, in reading order; each stays under 100 KB so one fetch
reads it. `00-core` alone — AGENTS.md plus the core pages — is enough to understand and discuss BrickKit.

<!-- llms:bundles:begin -->
…generated…
<!-- llms:bundles:end -->

## Answer a question

Find the page whose description below fits the question and fetch just that page. For where a feature lives in the
code, read the code map in [AGENTS.md](AGENTS.md) (§10).
```

`llms.zh.txt` the same in Chinese (标题「读完全部」「回答一个问题」; its own summary; "问题是英文的话，读
[`llms.txt`](llms.txt)"). Keep every page entry and its description.

- [ ] **Step 3: `AGENTS.md` / `AGENTS.zh.md` top and §9** — after the opening blockquote add:

```
> Paths in this file are relative to the repository root. On the web, prefix them with
> https://raw.githubusercontent.com/brickKit/brickKit/main/ . To read all the documentation in a few fetches, start
> with [`llms/en/00-core.md`](llms/en/00-core.md) and follow its "Next" line.
```

(zh: 「这份文件里的路径都相对仓库根目录；在网页上读，前面加 … 。想用最少的抓取读完全部文档，从
[`llms/zh/00-core.md`](llms/zh/00-core.md) 开始，顺着"下一份"往下读。」)

Replace `## §9 Documentation index` and its 19 raw links with:

```
## §9 Doc map

To find out → read (the page-by-page list with a line per page is [`llms.txt`](llms.txt)):

| To find out | Read |
| --- | --- |
| The platform in one read | [`llms/en/00-core.md`](llms/en/00-core.md): this file plus the core pages |
| Getting it running | [`docs/en/00-intro/02-quick-start.md`](docs/en/00-intro/02-quick-start.md) |
| Words and concepts | [`docs/en/00-intro/04-core-concepts.md`](docs/en/00-intro/04-core-concepts.md) |
| The three files, every field | [`docs/en/01-three-layers/README.md`](docs/en/01-three-layers/README.md), [`docs/en/01-three-layers/09-field-reference.md`](docs/en/01-three-layers/09-field-reference.md), [`docs/en/11-reference/README.md`](docs/en/11-reference/README.md) |
| Where a config value comes from | [`docs/en/01-three-layers/08-resolution-priority.md`](docs/en/01-three-layers/08-resolution-priority.md) |
| Running a project: init, add, local, up, upgrade … | [`docs/en/02-project-guide/README.md`](docs/en/02-project-guide/README.md) |
| Working on one component inside a project | [`docs/en/02-project-guide/04-focus-run.md`](docs/en/02-project-guide/04-focus-run.md) |
| Writing a component | [`docs/en/03-component-guide/README.md`](docs/en/03-component-guide/README.md) |
| Shells | [`docs/en/04-shell/README.md`](docs/en/04-shell/README.md) |
| Database migrations | [`docs/en/05-migration/README.md`](docs/en/05-migration/README.md) |
| Architecture, principles, the env contract, error codes | [`docs/en/06-architecture/README.md`](docs/en/06-architecture/README.md), [`docs/en/06-architecture/05-design-principles.md`](docs/en/06-architecture/05-design-principles.md), [`docs/en/06-architecture/03-env-injection-contract.md`](docs/en/06-architecture/03-env-injection-contract.md), [`docs/en/06-architecture/09-error-codes.md`](docs/en/06-architecture/09-error-codes.md) |
| Every command and flag | [`docs/en/07-cli-reference/README.md`](docs/en/07-cli-reference/README.md) |
| How an AI works with BrickKit | [`docs/en/08-ai-guide/README.md`](docs/en/08-ai-guide/README.md) |
| Recommended practices | [`docs/en/09-patterns/README.md`](docs/en/09-patterns/README.md) |
| Something failed | [`docs/en/10-troubleshooting/README.md`](docs/en/10-troubleshooting/README.md) |
| Building, testing, conventions for contributors | [`CONTRIBUTING.md`](CONTRIBUTING.md) |
| Where a feature lives in the code | §10 below |
```

`AGENTS.zh.md`: `## §9 文档地图`, the same rows in Chinese pointing at `docs/zh/…`, `llms.zh.txt`,
`CONTRIBUTING.zh.md`, `llms/zh/00-core.md`. Delete the old trailing line "The page-by-page index is …" (folded into
the table's lead-in).

- [ ] **Step 4: `scripts/check-docs-bilingual.py`** — the llms/AGENTS link check resolves relative links now:

```python
REL_LINK = re.compile(r"\[([^\]]+)\]\(([^)\s#]+)(#[^)]*)?\)")


def check_llms_txt_links():
    """入口文件（llms 两份、AGENTS 两份）里的每条相对链接都要指向真实存在的文件或目录。"""
    bad = []
    for name in RAW_LINK_FILES:
        text = open(os.path.join(ROOT, name), encoding="utf-8").read()
        for m in REL_LINK.finditer(text):
            target = m.group(2)
            if "://" in target or target.startswith("mailto:"):
                continue
            from urllib.parse import unquote
            local = os.path.join(ROOT, unquote(target))
            exists = os.path.isdir(local) if target.endswith("/") else os.path.isfile(local)
            if not exists:
                bad.append(f"{name} 链接 {target} 指向的{'目录' if target.endswith('/') else '文件'}不存在")
    return bad
```

and in `self_check()` replace the raw-link count with:

```python
    for name in LLMS_TXT_FILES:
        text = open(os.path.join(ROOT, name), encoding="utf-8").read()
        count = sum(1 for m in REL_LINK.finditer(text) if "://" not in m.group(2))
        if count < 5:
            print(f"❌ 自检失败：{name} 里只解析出 {count} 条相对链接——正则多半坏了，而不是链接真的这么少。")
            sys.exit(2)
```

Update the module docstring's ③ ("每一条相对链接指向的文件必须真实存在").

- [ ] **Step 5: Verify** — `make generate-llms` (AGENTS changed → bundles change), then
`python3 scripts/check-docs.py` (⑤ passes), `python3 scripts/check-docs-bilingual.py`, `make -k lint` → 0. Mutation:
append a second raw prefix to `AGENTS.md`, see ⑤ fail, revert.

- [ ] **Step 6: Commit**

```bash
git add llms.txt llms.zh.txt AGENTS.md AGENTS.zh.md llms scripts
git commit -m "docs: entry files use relative paths, llms.txt routes readers, AGENTS gets a doc map"
```

---

### Task 4: The code map in `AGENTS*.md`, guarded

**Files:**
- Modify: `AGENTS.md`, `AGENTS.zh.md` (new `## §10 Code map` / `## §10 代码地图` after §9)
- Create: `tests/docfields/codemap_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks except the §9 position.
- Produces: `func codeMapSection(text, heading string) (string, bool)`, `func missingPackages(section string, pkgs []string) []string`,
  `func codeMapPaths(section string) []string` (test helpers).

- [ ] **Step 1: The guard (RED)** — `tests/docfields/codemap_test.go`:

```go
package docfields_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AGENTS 的代码地图（§10）要让本地开发的 AI 从功能直达代码：internal/ 与 cmd/ 下每个包都在两份地图里，
// 地图里写的每个路径都真实存在。包加了、删了、挪了，地图不跟着改，这里就失败。
var codeMaps = map[string]string{"AGENTS.md": "## §10 Code map", "AGENTS.zh.md": "## §10 代码地图"}

var codePath = regexp.MustCompile("`((?:internal|cmd|tools|scripts|tests|market-server|schemas|\\.githooks|llms)/[^`]*|install\\.sh)`")

func codeMapSection(text, heading string) (string, bool) {
	i := strings.Index(text, "\n"+heading+"\n")
	if i < 0 {
		return "", false
	}
	rest := text[i+len(heading)+2:]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return rest, true
}

func missingPackages(section string, pkgs []string) []string {
	var out []string
	for _, p := range pkgs {
		if !strings.Contains(section, "`"+p+"/`") {
			out = append(out, p)
		}
	}
	return out
}

func codeMapPaths(section string) []string {
	var out []string
	for _, m := range codePath.FindAllStringSubmatch(section, -1) {
		out = append(out, m[1])
	}
	return out
}

func packageDirs(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, top := range []string{"internal", "cmd"} {
		entries, err := os.ReadDir(filepath.Join(repoRoot, top))
		require.NoError(t, err)
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, top+"/"+e.Name())
			}
		}
	}
	return out
}

func TestCodeMapCoversEveryPackage(t *testing.T) {
	pkgs := packageDirs(t)
	for file, heading := range codeMaps {
		text, err := os.ReadFile(filepath.Join(repoRoot, file))
		require.NoError(t, err)
		section, ok := codeMapSection(string(text), heading)
		require.True(t, ok, "%s has no %q section", file, heading)
		assert.Empty(t, missingPackages(section, pkgs), "%s: packages missing from the code map", file)
		for _, p := range codeMapPaths(section) {
			if strings.ContainsAny(p, "*?") {
				matches, _ := filepath.Glob(filepath.Join(repoRoot, filepath.FromSlash(p)))
				assert.NotEmpty(t, matches, "%s: %s matches nothing", file, p)
				continue
			}
			_, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(strings.TrimSuffix(p, "/"))))
			assert.NoError(t, err, "%s: %s does not exist", file, p)
		}
	}
}

// 检测器自己要会失败：少一个包、写一个不存在的路径，都要被认出来。
func TestCodeMapGuardCatchesDrift(t *testing.T) {
	section := "| `internal/cli/` | x |\n| `internal/gone/` | y |\n"
	assert.Equal(t, []string{"internal/cascade"}, missingPackages(section, []string{"internal/cli", "internal/cascade"}))
	assert.Equal(t, []string{"internal/cli/", "internal/gone/"}, codeMapPaths(section))
	_, ok := codeMapSection("## §9 x\nbody\n", "## §10 Code map")
	assert.False(t, ok)
}
```

Run: `go test ./tests/docfields/ -run 'TestCodeMap' -count=1` — Expected: FAIL (`AGENTS.md has no "## §10 Code map" section`).

- [ ] **Step 2: `AGENTS.md` §10** — insert after §9:

```
## §10 Code map

One Go module, `github.com/brickkit/brickkit`. The CLI starts in `cmd/brickkit/`; every command is one file,
`internal/cli/<command>.go`.

### Packages

| Package | Owns |
| --- | --- |
| `internal/cli/` | The command tree: one file per command, flags, output; errors shown relative to where you stand (`shown.go`); TAB completion candidates (`complete.go`) |
| `internal/project/` | Loading a project (brickkit.yaml + deploy file + config/) into one `Project`; finding the root upward (`findroot.go`); consistency checks; the project map `BRICKKIT.md` |
| `internal/projfile/` | `brickkit.yaml`: sources, components, versions, `kind: shell` |
| `internal/deployfile/` | Deploy files (`deploy.yaml`, `deploy.local.yaml`, `-f`): fields, validation, `focus`, the local-change diff |
| `internal/configdir/` | `config/`: per-component env files, `vars.yaml`, value resolution, skeletons, migration on upgrade |
| `internal/manifest/` | `component.yaml`: parsing, validation, the scaffolds `brickkit new` writes |
| `internal/resolver/` | Dependency resolution into a graph; start order |
| `internal/cascade/` | What runs this time: `mode`, "follows the layer above", focus reachability, shell hosting |
| `internal/inject/` | Each component's environment variables: dependency addresses, config, resources |
| `internal/shell/` | Shell grouping and the members' JSON config |
| `internal/compose/` | Rendering `compose.yaml` for docker / podman |
| `internal/k8s/` | Rendering Kubernetes manifests |
| `internal/deploy/` | Naming rules and file headers shared by both targets |
| `internal/engine/` | Docker, Podman and kubectl: detection and invocation |
| `internal/procsup/` | Supervising `mode: local` processes in the foreground |
| `internal/runcmd/` | Working out how to start a component from its source |
| `internal/sessionlock/` | One foreground local session per project |
| `internal/install/` | What `add` / `remove` / `upgrade` change in the three layers |
| `internal/source/` | Install sources (local / git / market), the manifest and artifact caches, the bare-repository cache, offline versions |
| `internal/gitrepo/` | Read-only git queries (status, submodules) |
| `internal/workspace/` | Component source under `components/`: archive, activate, deletion risk |
| `internal/release/` | `brickkit release`: checks, tag, push, rollback |
| `internal/market/` | Market client: login and publish |
| `internal/security/` | Component signatures: signing and verification |
| `internal/skills/` | AI-assistant skills installed into user projects (`assets/`) |
| `internal/clierr/` | The error type, error codes and how errors render |
| `internal/i18n/` | Message catalogs (`locales/en.yaml`, `locales/zh.yaml`) and language resolution |
| `internal/msgid/` | Message keys (`messages_gen.go` is generated) |
| `internal/logging/` | JSON log lines on stderr |
| `internal/suggest/` | "Did you mean" suggestions |
| `internal/envref/` | `${VAR}` references |
| `internal/yamlfile/` | The read / parse / decode pipeline the three layers share |
| `internal/yamlcheck/` | Misspelled or unknown YAML fields |
| `internal/yamlcomment/` | Comment blocks in generated YAML, `.env`, `.gitignore` |
| `internal/schemagen/` | JSON Schemas generated from the Go structs into `schemas/` |
| `internal/userconfig/` | Machine-level preferences (the CLI's language) |
| `internal/version/` | Version and capability constants |
| `internal/llmsgen/` | The documentation bundles in `llms/` and the bundle list in `llms*.txt` |
| `cmd/brickkit/` | The CLI's `main` |
| `cmd/gen-msgid/` | Generates `internal/msgid/messages_gen.go` |
| `cmd/gen-schemas/` | Generates `schemas/*.json` |
| `cmd/gen-llms/` | Generates `llms/` |

Elsewhere: `market-server/` (the optional component market, its own Go module), `tools/i18n/` (one-off i18n
migration scripts), `scripts/` (lint checks, install checks, release), `install.sh`, `.githooks/` (the commit hook).

### Features → code

| Feature | Start here | Then |
| --- | --- | --- |
| `up` | `internal/cli/up.go` (`up_local.go`, `up_k8s.go`, `up_upgrade.go`) | `cascade`, `inject`, `compose`, `k8s`, `engine`, `procsup` |
| `down`, `status` | `internal/cli/down.go`, `internal/cli/status.go`, `internal/cli/lifecycle.go` | `engine`, `sessionlock` |
| `add`, `remove`, `upgrade` | `internal/cli/add.go`, `internal/cli/remove.go`, `internal/cli/upgrade.go`, `internal/cli/install_apply.go` | `install`, `configdir`, `source` |
| `fetch` | `internal/cli/fetch.go`, `internal/cli/artifacts.go` | `source` |
| `build` | `internal/cli/build.go` | `source`, `engine`, `gitrepo` |
| `lint` | `internal/cli/lint.go`, `internal/cli/lint_config.go` | `project`, `yamlcheck` |
| `graph`, `deps` | `internal/cli/graph.go`, `internal/cli/deps.go`, `internal/cli/topology.go` | `resolver`, `cascade` |
| `sync`, `restore` | `internal/cli/sync.go`, `internal/cli/restore.go`, `internal/cli/restore_check.go` | `workspace` |
| `local` | `internal/cli/local.go` | `deployfile` |
| `init`, `new` | `internal/cli/init.go`, `internal/cli/new.go`, `internal/cli/hooks.go` | `project`, `manifest`, `skills` |
| `release`, `publish`, `login`, `logout` | `internal/cli/release.go`, `internal/cli/publish*.go`, `internal/cli/login.go`, `internal/cli/logout.go` | `release`, `market`, `security` |
| `skills`, `lang`, `version` | `internal/cli/skills.go`, `internal/cli/lang.go`, `internal/cli/version.go` | `skills`, `i18n`, `userconfig` |
| Finding the project upward | `internal/project/findroot.go`, `internal/cli/root.go` | |
| Focus run | `internal/cli/focus.go` | `internal/cascade/cascade.go`, `internal/deployfile/focus.go` |
| One `components/` (nested copies) | `internal/cli/nested.go` | `internal/project/nested.go` |
| TAB completion | `internal/cli/complete.go` | `internal/source/cached.go`, `install.sh` |
| "Did you mean" | `internal/cli/didyoumean.go` | `suggest` |
| How errors look | `internal/clierr/clierr.go`, `internal/cli/shown.go` | |
| Adding a message | `internal/i18n/locales/en.yaml`, `internal/i18n/locales/zh.yaml`, then `make generate-msgid` | `msgid` |
| Documentation bundles | `cmd/gen-llms/`, `internal/llmsgen/` | `.githooks/pre-commit` |

### Tests and checks

- Unit tests sit next to the code (`*_test.go`); CLI tests run commands in-process against `internal/cli/testdata/`.
- `tests/components/`: real components (Go, Python, nginx) used as fixtures; `tests/checklist/`: the regression
  list `make test-all` runs; `tests/docfields/`: guards that the docs match the code.
- `make lint` (every static check, `scripts/check-*.py`) and `make test-all`; `make hooks` once per clone.
```

`AGENTS.zh.md`: `## §10 代码地图`, the same paths, Chinese descriptions (take them from each package's `// Package`
comment), subsections 「包」「功能 → 代码」「测试与检查」.

- [ ] **Step 3: Run** — `go test ./tests/docfields/ -run 'TestCodeMap' -count=1` → PASS. Mutation: delete the
`internal/suggest/` row from `AGENTS.zh.md` → FAIL naming it; restore. `make generate-llms` (AGENTS feeds 00-core),
check `wc -c llms/*/00-core.md` < 100000, `make -k lint` → 0.

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md AGENTS.zh.md tests/docfields/codemap_test.go llms llms.txt llms.zh.txt
git commit -m "docs(agents): a code map from every feature to its files, guarded against drift"
```

---

### Task 5: The versioned pre-commit hook

**Files:**
- Create: `.githooks/pre-commit`, `scripts/check-githooks.sh`
- Modify: `Makefile` (`hooks`, `check-githooks` in `lint`), `CONTRIBUTING.md`, `CONTRIBUTING.zh.md`

**Interfaces:**
- Consumes: `cmd/gen-llms` (Task 2).
- Produces: `make hooks`.

- [ ] **Step 1: The check (RED)** — `scripts/check-githooks.sh` (bash, `set -euo pipefail`, `ok`/`bad` counters like
`scripts/check-install-sh.sh`). In a temp dir: `git init`, copy `.githooks/pre-commit`, `git config core.hooksPath
.githooks`, a fake `go` on `PATH` (`$tmp/bin/go`: `#!/bin/sh` → `mkdir -p llms/en && date +%s%N > llms/en/00-core.md
&& echo ran >> "$tmp/go-calls"`), `PATH="$tmp/bin:$PATH"`, commit an initial `docs/en/a.md` + `llms.txt`. Cases:
  1. change `docs/en/a.md`, `git add`, `git commit -qm x` → exit 0, `go-calls` has one line, `git show --name-only HEAD`
     lists `llms/en/00-core.md`;
  2. change a non-doc file only → commit ok, no new `go-calls` line;
  3. stage a change to `docs/en/a.md`, then change it again unstaged → commit exits non-zero, output names
     `docs/en/a.md`;
  4. add an untracked `docs/en/new.md` and commit another staged doc change → refused, output names `docs/en/new.md`;
  5. `PATH` without `go` (fake bin removed), staged doc change → commit ok, output contains `make lint`.

Run: `bash scripts/check-githooks.sh` — Expected: FAIL (`.githooks/pre-commit` missing).

- [ ] **Step 2: The hook** — `.githooks/pre-commit`:

```sh
#!/bin/sh
# 提交前让 llms/ 下的文档合集跟上这次提交里的文档（make hooks 启用本钩子）。
# 生成器读的是工作区：这些文件还有没暂存的改动、或有没跟踪的新文档时，拒绝提交——
# 那样生成的合集与提交的内容对不上。没有 go 时只提醒，make lint 的 check-llms 兜底。
set -e
paths='docs AGENTS.md AGENTS.zh.md llms.txt llms.zh.txt'
# shellcheck disable=SC2086
[ -n "$(git diff --cached --name-only -- $paths)" ] || exit 0
# shellcheck disable=SC2086
dirty=$(git diff --name-only -- $paths; git ls-files --others --exclude-standard -- docs)
if [ -n "$dirty" ]; then
	echo "❌ 这些文档还有没暂存的改动或没跟踪的新文件，合集会与这次提交对不上："
	echo "$dirty" | sed 's/^/   /'
	echo "   把它们整个 git add 进来（或 git stash 掉），再提交；确实要分开提交就用 --no-verify，之后跑 make generate-llms"
	exit 1
fi
if ! command -v go >/dev/null 2>&1; then
	echo "⚠️  找不到 go，没有重新生成 llms/ 合集；make lint（check-llms）会指出它过期了"
	exit 0
fi
go run ./cmd/gen-llms
git add llms llms.txt llms.zh.txt
```

`chmod +x .githooks/pre-commit`.

- [ ] **Step 3: Makefile**

```make
.PHONY: hooks
hooks: ## 启用仓库自带的提交钩子（.githooks/：提交时自动更新 llms/ 合集），每个克隆一次
	@git config core.hooksPath .githooks && echo "✅ 已启用 .githooks/（git config core.hooksPath .githooks）"

.PHONY: check-githooks
check-githooks: ## 真跑提交钩子：文档改动会重新生成并暂存合集，半暂存或未跟踪的文档会被拒绝
	@bash scripts/check-githooks.sh
```

add `check-githooks` to `lint:`. `scripts/check-githooks.sh` starts with the same shellcheck block as
`scripts/check-install-sh.sh`: when `shellcheck` is on `PATH`, run `shellcheck --shell=sh .githooks/pre-commit &&
shellcheck scripts/check-githooks.sh`; when it isn't, print the same loud skip line.

- [ ] **Step 4: CONTRIBUTING** — in "Building and running the tests": `make hooks` once per clone; what the hook does,
when it refuses, `make generate-llms` by hand. Chinese mirror in `CONTRIBUTING.zh.md`.

- [ ] **Step 5: Run** — `bash scripts/check-githooks.sh` → all ok; `make hooks`; remove the stray
`.git/hooks/pre-commit` in this clone (after confirming it is the brickkit-managed one pointing at a deleted binary);
`make -k lint` → 0.

- [ ] **Step 6: Commit**

```bash
git add .githooks scripts/check-githooks.sh Makefile CONTRIBUTING.md CONTRIBUTING.zh.md
git commit -m "feat: a versioned pre-commit hook regenerates the documentation bundles; make hooks enables it"
```

---

### Task 6: README navigation, the AI routes, and the AI-guide page

**Files:**
- Modify: `README.md`, `README.zh.md`, `docs/{en,zh}/08-ai-guide/README.md`, `llms.txt`, `llms.zh.txt`
- Create: `docs/en/08-ai-guide/05-reading-brickkit.md`, `docs/zh/08-ai-guide/05-reading-brickkit.md`

- [ ] **Step 1: README navigation line** — right after the closing `</div>` of the introduction (before `## The
three layers`):

```
**Docs:** [Quick start](docs/en/00-intro/02-quick-start.md) · [The three layers](docs/en/01-three-layers/README.md) ·
[Running a project](docs/en/02-project-guide/README.md) · [Writing components](docs/en/03-component-guide/README.md) ·
[CLI reference](docs/en/07-cli-reference/README.md) · [Troubleshooting](docs/en/10-troubleshooting/README.md) ·
[All docs](docs/en/README.md) · [For AI](llms.txt)
```

`README.zh.md`: 「**文档：**快速开始 · 三层文件 · 运行项目 · 编写组件 · 命令参考 · 排障 · 全部文档 · 给 AI」 pointing at
`docs/zh/…` and `llms.zh.txt`.

- [ ] **Step 2: The AI box** — replace its body with three routes (absolute raw URLs kept here: an AI reading the
GitHub HTML page doesn't know the raw prefix):

```
> Decide the user's language first (this README is always English; which README GitHub shows says nothing about it),
> then pick the route:
>
> | Task | English | Chinese |
> | --- | --- | --- |
> | Understand or evaluate BrickKit, or read everything | <https://raw.githubusercontent.com/brickKit/brickKit/main/llms/en/00-core.md>, then follow each file's "Next" line | <https://raw.githubusercontent.com/brickKit/brickKit/main/llms/zh/00-core.md> |
> | Answer one question | <https://raw.githubusercontent.com/brickKit/brickKit/main/llms.txt>: pick the page by its description | <https://raw.githubusercontent.com/brickKit/brickKit/main/llms.zh.txt> |
> | Develop in a local clone | `AGENTS.md`: the doc map (§9) and the code map (§10) | `AGENTS.zh.md` |
>
> Every file stays under 100 KB, so one fetch reads it whole.
```

Chinese mirror in `README.zh.md`. In "Where to go next", the AI sentence becomes: "**If you are an AI**, start from
[`llms.txt`](llms.txt): it routes a question to one page, or lists the bundles that hold everything."

- [ ] **Step 3: The AI-guide page** — `docs/en/08-ai-guide/05-reading-brickkit.md` ("Reading BrickKit itself"):
  who it is for (an AI asked about BrickKit, or developing it); three routes as in the AI box; how the bundles are cut
  (≤100 KB, reading order, a page never split, `> File:` markers, "Next" line); a clone: `AGENTS.md` §9 doc map and
  §10 code map, `CONTRIBUTING.md`; how bundles stay current (`make generate-llms`, `make hooks`, `check-llms`). Chinese
  page written independently. Add row 05 to both `08-ai-guide/README.md` tables and an entry in `llms*.txt`
  (`- [docs/en/08-ai-guide/05-reading-brickkit.md](docs/en/08-ai-guide/05-reading-brickkit.md): reading BrickKit
  itself — the bundles, llms.txt routing, AGENTS maps`).

- [ ] **Step 4: Verify** — `make generate-llms`; `make -k lint` → 0 (check-docs links and anchors,
check-docs-bilingual mirror, raw-prefix guard: README is not in `RAW_ONCE_FILES`).

- [ ] **Step 5: Commit**

```bash
git add README.md README.zh.md docs llms llms.txt llms.zh.txt
git commit -m "docs: README navigation and AI routes; how to read BrickKit itself"
```

---

### Task 7: Full gates and review

- [ ] **Step 1:** Checklist rows in `tests/checklist/清单.tsv` (continue the numbering):
  `boundary.31	文档合集每份不超过预算、每页只出现一次	.	./internal/llmsgen	TestGeneratePacksPagesInReadingOrderOnce`,
  `error.24	文档合集超预算时报错点名、不截断	.	./internal/llmsgen	TestGenerateFailsLoudlyOverBudget`,
  `error.25	代码地图漏了包或写了不存在的路径	.	./tests/docfields	TestCodeMapCoversEveryPackage`.
- [ ] **Step 2:** `systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 make -k lint; echo $?` → 0;
  `systemd-run --user --scope -q -p MemoryMax=10G -p MemorySwapMax=0 make -k test-all; echo $?` → 0.
- [ ] **Step 3:** Commit `test: checklist rows for documentation bundles and the code map`.
- [ ] **Step 4:** Whole-plan review on the most capable model with this plan's Review Focus; one fix pass, each fix
  with a failing test first; report rulings and deferred minors.
