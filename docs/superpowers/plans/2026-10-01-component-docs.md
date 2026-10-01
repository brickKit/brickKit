# Component and Project Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a project's `AGENTS.md` and each component's `BRICKKIT.md` / `AGENTS.md` / `README.md` the documented,
generated and mechanically checked way an AI understands and changes a BrickKit project — with `AGENTS.md` owned by the
author, the old project-level `BRICKKIT.md` gone, skills tracked inside their own files, multilingual component docs
cached and published, and `brickkit lint` warning on documentation drift.

**Architecture:** Four new packages carry the design:
- `internal/mdtext`: Markdown scanning, extracted from `llmsgen` (links, fences, sections, table cells).
- `internal/docspec`: the spec as data (file names, required sections per kind, heading names per language, language
  codes, translation names, placeholder words).
- `internal/agentsmd`: the CLI-maintained block inside `AGENTS.md`, its per-language templates, and the skeletons.
- `internal/doccheck`: the lint checks.

Existing packages change around them: `project` (rows, init), `skills` (markers instead of a lock), `manifest`
(scaffold, `metadata.repository`), `source` (cache every language), `cli` (init / new / skills / lint / publish), and
`market-server` (translations).

**Tech Stack:** Go 1.24 (module `github.com/brickkit/brickkit`), testify, embed, text/template; market-server (own
module, PostgreSQL + in-memory repo); Python lint scripts; Markdown docs in `docs/{en,zh}`.

**Spec:** `docs/superpowers/specs/2026-10-01-component-docs-design.md`

## Global Constraints

- Go code comments are Chinese, like the rest of the codebase; every user-visible string goes through
  `internal/i18n/locales/en.yaml` **and** `zh.yaml`, then `make generate-msgid`. No hard-coded user-facing text.
- Doc heading names (English / Chinese) are content of the documents, not CLI messages: they live in `internal/docspec`
  as data, and `internal/docspec/` is added to `hardcodedSkipDirs` in `tests/i18nguard/i18nguard_test.go` with the reason.
- Every new error code goes into the `clierr` const block **and** both `docs/{en,zh}/06-architecture/09-error-codes.md`
  (`tests/docfields` enforces it). Codes are never renamed once added.
- Required component files: `component.yaml`, `BRICKKIT.md`, `AGENTS.md`, `CLAUDE.md`, `README.md`. Optional: `docs/`,
  `CHANGELOG.md` (never checked).
- `BRICKKIT.md` six sections: `Purpose`/`组件定位`, `Before you deploy`/`部署前准备`, `Dependencies`/`依赖说明`,
  `Configuration`/`配置指南`, `Contracts`/`契约索引`, `Shell declaration`/`外壳声明`.
- Component `AGENTS.md` five sections: `Code map`/`代码地图`, `Build and test`/`构建与测试`, `Design decisions`/`设计取舍`,
  `Pitfalls`/`易错点`, `Before changing code`/`改代码前自查`.
- Project `AGENTS.md` four sections: `Overview`/`项目概述`, `Conventions`/`项目约定`, `Where to look`/`查找路由`,
  `Pitfalls`/`易错点`.
- Component `README.md` three sections: `Use it in a project`/`在项目里使用`, `Documentation`/`文档`, `Development`/`开发`.
- Managed block markers: `<!-- brickkit:managed:begin lang=<code> -->` … `<!-- brickkit:managed:end -->`, at the end of
  `AGENTS.md`. The CLI never writes a byte outside them.
- Skill marker, last line of each installed skill file: `<!-- brickkit:skill version=<v> sum=sha256:<hex> -->`.
- Language codes: `^[a-z]{2,3}(-[a-z0-9]{2,8})*$`. A translation is a sibling `<name>.<lang>.md`.
- Placeholder words: `TODO`, `TBD`, `FIXME`, `待补`, `后补`, `待填` (outside code).
- Docs: at most 16 translations, all docs together ≤ `4 × MaxDocBytes`.
- Documentation checks are warnings only; `up` and `release` never block on them.
- Docs pages: en and zh are written independently with the same structure; links only into `docs/{en,zh}`; never link
  specs, plans or `docs/superpowers`.
- `$SCRATCH` in commands is the session scratchpad directory (long test output goes there; read its tail).
- Before each commit: `make -k lint`; heavy tests run under
  `systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0`. Commit after each task, with the trailer
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Commit commands start with `git` (no `cd`). Work on `main`.

## Review Focus

1. **Markers inside a code fence, or CRLF line endings, in `AGENTS.md`.** An author documenting the markers in a
   ```` ``` ```` block must not have that example rewritten; a CRLF file must keep its bytes outside the block. Owned by
   Task 3 (`TestFindIgnoresMarkersInFences`, `TestReplaceKeepsCRLFOutside`).
2. **Malformed markers** (begin without end, two begins, end before begin). The CLI must not rewrite anything, and must
   warn (`AGENTS_BLOCK_MISSING` carries the reason). Owned by Task 3 (`TestFindRejectsMalformed`).
3. **Table cell content with `|` or a newline** in `metadata.description`. The component table must stay one row per
   component. Owned by Task 3 (`TestRenderEscapesCells`).
4. **Headings written as `## 1. Purpose`, `## Purpose  `, or `## Purpose {#purpose}`.** These must still count as the
   section. Owned by Task 2 (`TestMatchesNumberedAndDecorated`).
5. **Translation names that aren't codes** (`README.zh-CN.md`, `BRICKKIT.draft.md`). They must be reported, not
   silently ignored. Owned by Task 7 (`TestTranslationSuffixNotALanguage`).
6. **An editor adding or stripping the final newline of a skill file.** It must not flip a skill to "modified". Owned
   by Task 5 (`TestMarkerSumIgnoresTrailingNewlines`).

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/mdtext/mdtext.go` (new) | Fences, inline code, links (`Links`, `MapLinks`), level-2 `Sections`, `TableCells` |
| `internal/llmsgen/links.go` (modify) | Uses `mdtext.MapLinks`; keeps only `RewriteLinks` / `rewriteTarget` / `relative` |
| `internal/docspec/docspec.go` (new) | File names, `Kind`, `Section`, headings, `Required`, `Matches`, language codes, translations, placeholders |
| `internal/agentsmd/block.go` (new) | `Find` / `Replace` / `Append` of the managed block; `ParseRows` |
| `internal/agentsmd/render.go` (new) | `Content`, `Row`, `Render` (templates per language) |
| `internal/agentsmd/skeleton.go` (new) | `Skeleton` for project / component `AGENTS.md`; `ClaudeContent` |
| `internal/agentsmd/ensure.go` (new) | `Ensure(root, content, mode)`: create / append / rewrite / CLAUDE.md, legacy replacement |
| `internal/agentsmd/templates/{en,zh}/{project,component}.md` (new) | Block prose |
| `internal/project/agents.go` (new, replaces `projectdoc.go`) | `AgentsRows`, `AgentsContent`, `WriteAgentsBlock`, `ObsoleteProjectMap` |
| `internal/project/init.go`, `layout.go` (modify) | Plan/apply `AGENTS.md` + `CLAUDE.md`; no project `BRICKKIT.md`; `CachedDocLangs` |
| `internal/skills/marker.go` (new), `install.go`, `assets.go`, `lock.go` (modify) | Markers, states, legacy lock migration, language from the block; `AGENTS.md` asset removed |
| `internal/manifest/types.go`, `scaffold.go`, `docs_scaffold.go` (new) | `metadata.repository`; `new` writes six-section `BRICKKIT.md`, `AGENTS.md`, `CLAUDE.md`, `README.md` |
| `internal/source/{source,local,git,market,gitcache}.go` (modify) | `docFiles`: every `BRICKKIT*.md` cached |
| `internal/doccheck/{component,project,common}.go` (new) | The eleven checks |
| `internal/clierr/clierr.go` (modify) | Eleven codes |
| `internal/cli/{init,new,skills,lint,install_apply,publish}.go` (modify) | Wiring and output |
| `market-server/internal/{model,validator,service,repo,handler}` (modify) | Translations |
| `internal/skills/assets/{en,zh}/claude/skills/*` (modify/new) | `brickkit-component`, `brickkit-assemble`, new `brickkit-plan-change` |
| `docs/{en,zh}/…`, `AGENTS*.md`, `tests/checklist/清单.tsv`, `tests/components/*` | Documentation, regression rows, fixtures |

---

### Task 1: `internal/mdtext` — Markdown scanning shared by the generator and the checks

**Files:**
- Create: `internal/mdtext/mdtext.go`, `internal/mdtext/mdtext_test.go`
- Modify: `internal/llmsgen/links.go` (remove `linkForms`, `inlineCode`, `fences`, `fenceOf`, `mapLinks`, `mapLine`;
  call `mdtext.MapLinks`), `internal/llmsgen/current_test.go:33` (`mapLinks` → `mdtext.MapLinks`)

**Interfaces:**
- Produces:
  - `func MapLinks(body string, fn func(target string) string) string`
  - `type Link struct{ Line int; Target string }`
  - `func Links(body string) []Link`
  - `type Section struct{ Heading string; Line int; Body string }`
  - `func Sections(body string) []Section`
  - `func ProseLines(body string) []Line`
  - `type Line struct{ N int; Text string }`
  - `func InlineCode(line string) []string`
  - `func TableCells(line string) []string`

- [ ] **Step 1: Write the failing tests**

```go
package mdtext

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var fence = strings.Repeat("`", 3)

func TestLinksSkipsCodeAndFindsEveryForm(t *testing.T) {
	body := "See [a](a.md) and [b](b.md \"t\").\n" +
		fence + "\n[x](inside.md)\n" + fence + "\n" +
		"`[y](inline.md)` <a href=\"c.md\">c</a>\n" +
		"[ref]: d.md\n"
	var got []string
	for _, l := range Links(body) {
		got = append(got, l.Target)
	}
	assert.Equal(t, []string{"a.md", "b.md", "c.md", "d.md"}, got)
	assert.Equal(t, 1, Links(body)[0].Line)
}

func TestTildeFenceClosesOnlyWithTilde(t *testing.T) {
	body := "~~~\n" + fence + "\n[x](in.md)\n~~~\n[y](out.md)\n"
	ls := Links(body)
	assert.Len(t, ls, 1)
	assert.Equal(t, "out.md", ls[0].Target)
}

func TestSectionsLevelTwoOutsideFences(t *testing.T) {
	body := "# T\n\n## One\n\na\n" + fence + "\n## not a heading\n" + fence + "\n## Two\nb\n"
	s := Sections(body)
	assert.Len(t, s, 2)
	assert.Equal(t, "One", s[0].Heading)
	assert.Equal(t, 3, s[0].Line)
	assert.Contains(t, s[0].Body, "## not a heading")
	assert.Equal(t, "Two", s[1].Heading)
}

func TestProseLinesDropCode(t *testing.T) {
	body := "keep TODO\n" + fence + "\nTODO hidden\n" + fence + "\nalso `TODO` gone\n"
	var texts []string
	for _, l := range ProseLines(body) {
		texts = append(texts, strings.TrimSpace(l.Text))
	}
	assert.Equal(t, []string{"keep TODO", "", "also  gone"}, texts)
}

func TestTableCells(t *testing.T) {
	assert.Equal(t, []string{"`a/`", "Owns"}, TableCells("| `a/` | Owns |"))
	assert.Nil(t, TableCells("|---|---|"))
	assert.Nil(t, TableCells("plain"))
	assert.Equal(t, []string{"`x/y.go`"}, InlineCode("see `x/y.go` here")[:1])
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/mdtext/`
Expected: FAIL — `undefined: Links` (package has no source yet).

- [ ] **Step 3: Implement `internal/mdtext/mdtext.go`**

```go
// Package mdtext 是几处都要用的 Markdown 扫描：代码块围栏、行内代码、链接、二级小节、表格单元格。
// 文档合集生成器（llmsgen）改写链接、lint 的文档检查（doccheck）核对链接与小节，都走这里，
// 免得两份对"什么算代码块"的理解各写一套、慢慢分叉。
package mdtext

import (
	"regexp"
	"sort"
	"strings"
)

// 链接目标出现的三种位置：[文字](目标 "可选标题")、行首的引用式定义 [名]: 目标、HTML 的 href / src。
// 每个正则的第 2 个分组是目标。
var (
	linkForms = []*regexp.Regexp{
		regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`),
		regexp.MustCompile(`^(\s*\[[^\]]+\]:\s*)(\S+)`),
		regexp.MustCompile(`\b(href|src)="([^"]+)"`),
	}
	inlineCode = regexp.MustCompile("`[^`]*`")
)

// fences 是 Markdown 代码块的两种围栏（三个反引号、三个波浪号）。
var fences = []string{"\x60\x60\x60", "~~~"}

// FenceOf 是这一行开头的围栏；不是围栏时为空。
func FenceOf(line string) string {
	t := strings.TrimSpace(line)
	for _, f := range fences {
		if strings.HasPrefix(t, f) {
			return f
		}
	}
	return ""
}

// eachOutside 对代码块之外的每一行调用 fn（行号从 1 起）；围栏行本身与块内的行都不给。
func eachOutside(body string, fn func(i int, line string)) {
	open := "" // 正在其中的代码块的围栏：只有同一种围栏才能把它关上
	for i, line := range strings.Split(body, "\n") {
		if f := FenceOf(line); f != "" && (open == "" || f == open) {
			if open == "" {
				open = f
			} else {
				open = ""
			}
			continue
		}
		if open == "" {
			fn(i, line)
		}
	}
}

// MapLinks 对代码块与行内代码之外的每个链接目标调用 fn，用它的返回值替换目标。
func MapLinks(body string, fn func(target string) string) string {
	lines := strings.Split(body, "\n")
	eachOutside(body, func(i int, line string) { lines[i] = mapLine(line, fn) })
	return strings.Join(lines, "\n")
}

// Link 是一处链接：所在行号（从 1 起）与目标。
type Link struct {
	Line   int
	Target string
}

// Links 列出代码之外的全部链接，按出现顺序。
func Links(body string) []Link {
	var out []Link
	eachOutside(body, func(i int, line string) {
		mapLine(line, func(target string) string {
			out = append(out, Link{Line: i + 1, Target: target})
			return target
		})
	})
	return out
}

func mapLine(line string, fn func(string) string) string {
	codes := inlineCode.FindAllStringIndex(line, -1)
	inCode := func(at int) bool {
		for _, c := range codes {
			if at >= c[0] && at < c[1] {
				return true
			}
		}
		return false
	}
	var spans [][2]int
	for _, re := range linkForms {
		for _, m := range re.FindAllStringSubmatchIndex(line, -1) {
			if !inCode(m[0]) {
				spans = append(spans, [2]int{m[4], m[5]})
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		if sp[0] < last { // 两种写法重叠（不会发生，保险起见只认先出现的那个）
			continue
		}
		b.WriteString(line[last:sp[0]])
		b.WriteString(fn(line[sp[0]:sp[1]]))
		last = sp[1]
	}
	b.WriteString(line[last:])
	return b.String()
}

// Section 是一个二级小节："## " 开头的那一行（代码块外）到下一个二级标题之前。
type Section struct {
	Heading string // 去掉 "## " 与首尾空白
	Line    int    // 标题所在行，从 1 起
	Body    string // 标题之后、下一个二级标题之前的原文（含代码块）
}

// Sections 列出代码块之外的全部二级小节。
func Sections(body string) []Section {
	lines := strings.Split(body, "\n")
	var heads []int
	eachOutside(body, func(i int, line string) {
		if strings.HasPrefix(line, "## ") {
			heads = append(heads, i)
		}
	})
	out := make([]Section, 0, len(heads))
	for k, h := range heads {
		end := len(lines)
		if k+1 < len(heads) {
			end = heads[k+1]
		}
		out = append(out, Section{
			Heading: strings.TrimSpace(strings.TrimPrefix(lines[h], "## ")),
			Line:    h + 1,
			Body:    strings.Join(lines[h+1:end], "\n"),
		})
	}
	return out
}

// Line 是一行正文：行号（从 1 起）与去掉行内代码后的文字。
type Line struct {
	N    int
	Text string
}

// ProseLines 是代码块之外的每一行，行内代码已删掉——查占位词这类"正文里写了什么"用它。
func ProseLines(body string) []Line {
	var out []Line
	eachOutside(body, func(i int, line string) {
		out = append(out, Line{N: i + 1, Text: inlineCode.ReplaceAllString(line, "")})
	})
	return out
}

// InlineCode 是一行里每段行内代码的内容（不含反引号），按出现顺序。
func InlineCode(line string) []string {
	var out []string
	for _, m := range inlineCode.FindAllString(line, -1) {
		out = append(out, strings.Trim(m, "`"))
	}
	return out
}

// TableCells 是表格行的各个单元格（去掉首尾空白）；不是表格行、或是分隔行（|---|）时为 nil。
func TableCells(line string) []string {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "|") || !strings.HasSuffix(t, "|") || len(t) < 2 {
		return nil
	}
	cells := strings.Split(t[1:len(t)-1], "|")
	sep := true
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
		if strings.Trim(cells[i], ":-") != "" {
			sep = false
		}
	}
	if sep {
		return nil
	}
	return cells
}
```

Fix the test's expectation on `InlineCode`: it returns contents without backticks, so the last assertion is
`assert.Equal(t, []string{"x/y.go"}, InlineCode("see `x/y.go` here"))`. In `TestTableCells`, `TableCells` keeps
backticks inside cells (`"`a/`"`), as written.

- [ ] **Step 4: Point `llmsgen` at it**

In `internal/llmsgen/links.go`, delete `linkForms`, `inlineCode`, `fences`, `fenceOf`, `mapLinks` and `mapLine`. Make
`RewriteLinks` call `mdtext.MapLinks(body, func(target string) string { return rewriteTarget(target, pagePath, bundleDir) })`.
Drop the now-unused imports `regexp` and `sort`, and add `"github.com/brickkit/brickkit/internal/mdtext"`. In
`internal/llmsgen/current_test.go` replace `mapLinks(` with `mdtext.MapLinks(` and add the import. Add a row for
`internal/mdtext/` to both code maps (`AGENTS.md` §10 and `AGENTS.zh.md` §10, Packages table): "Markdown scanning
shared by the doc bundles and lint's doc checks: fences, links, sections, table cells". `tests/docfields/codemap_test.go`
fails until this row exists.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/mdtext/ ./internal/llmsgen/ ./tests/docfields/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mdtext internal/llmsgen AGENTS.md AGENTS.zh.md
git commit -m "refactor(mdtext): one Markdown scanner for the doc bundles and the doc checks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `internal/docspec` — the spec as data

**Files:**
- Create: `internal/docspec/docspec.go`, `internal/docspec/docspec_test.go`
- Modify: `tests/i18nguard/i18nguard_test.go` (`hardcodedSkipDirs` += `"internal/docspec/"`, comment: heading names are
  the documents' own content and follow the document's language, not the CLI's), both code maps.

**Interfaces:**
- Produces:
  - `const FileBrickkit = "BRICKKIT.md"`, `FileAgents = "AGENTS.md"`, `FileClaude = "CLAUDE.md"`,
    `FileReadme = "README.md"`, `ClaudeImport = "@AGENTS.md"`, `DirDocs = "docs"`
  - `type Kind int` with `KindBrickkit`, `KindComponentAgents`, `KindProjectAgents`, `KindReadme`
  - `type Section int` with `Purpose`, `BeforeDeploy`, `Dependencies`, `Configuration`, `Contracts`, `ShellDecl`,
    `CodeMap`, `BuildTest`, `Decisions`, `Pitfalls`, `BeforeChanging`, `Overview`, `Conventions`, `WhereToLook`,
    `UseIt`, `Documentation`, `Development`
  - `func Required(k Kind) []Section`
  - `func Heading(s Section, lang string) string`
  - `func Matches(s Section, heading string) bool`
  - `func HeadingLangs() []string`
  - `func ValidLang(code string) bool`
  - `func SplitTranslation(name string) (base, lang string, ok bool)`
  - `func TranslationName(base, lang string) string`
  - `var PlaceholderWords []string`
  - `const MaxTranslations = 16`

- [ ] **Step 1: Write the failing tests**

```go
package docspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequiredSections(t *testing.T) {
	assert.Equal(t, []Section{Purpose, BeforeDeploy, Dependencies, Configuration, Contracts, ShellDecl}, Required(KindBrickkit))
	assert.Equal(t, []Section{CodeMap, BuildTest, Decisions, Pitfalls, BeforeChanging}, Required(KindComponentAgents))
	assert.Equal(t, []Section{Overview, Conventions, WhereToLook, Pitfalls}, Required(KindProjectAgents))
	assert.Equal(t, []Section{UseIt, Documentation, Development}, Required(KindReadme))
}

func TestHeadingsInBothLanguages(t *testing.T) {
	assert.Equal(t, "Before you deploy", Heading(BeforeDeploy, "en"))
	assert.Equal(t, "部署前准备", Heading(BeforeDeploy, "zh"))
	assert.Equal(t, "Code map", Heading(CodeMap, "ja"), "an unknown language falls back to English")
	for _, k := range []Kind{KindBrickkit, KindComponentAgents, KindProjectAgents, KindReadme} {
		for _, s := range Required(k) {
			for _, l := range HeadingLangs() {
				assert.NotEmpty(t, headings[s][l], "section %d lacks a %s heading", s, l)
			}
		}
	}
}

func TestMatchesNumberedAndDecorated(t *testing.T) {
	for _, h := range []string{"Purpose", "1. Purpose", "Purpose  ", "Purpose {#purpose}", "组件定位", "2、组件定位"} {
		assert.True(t, Matches(Purpose, h), h)
	}
	assert.False(t, Matches(Purpose, "Purposes"))
	assert.False(t, Matches(Purpose, "Dependencies"))
}

func TestLanguageCodes(t *testing.T) {
	for _, c := range []string{"zh", "en", "ja", "pt-br", "zh-hant"} {
		assert.True(t, ValidLang(c), c)
	}
	for _, c := range []string{"", "zh-CN", "Chinese", "draft", "x"} {
		assert.False(t, ValidLang(c), c)
	}
}

func TestSplitTranslation(t *testing.T) {
	cases := []struct{ in, base, lang string; ok bool }{
		{"README.md", "README.md", "", true},
		{"README.zh.md", "README.md", "zh", true},
		{"design.pt-br.md", "design.md", "pt-br", true},
		{"README.zh-CN.md", "README.zh-CN.md", "", false},
		{"notes.txt", "", "", false},
	}
	for _, c := range cases {
		base, lang, ok := SplitTranslation(c.in)
		assert.Equal(t, c.ok, ok, c.in)
		if c.ok {
			assert.Equal(t, c.base, base, c.in)
			assert.Equal(t, c.lang, lang, c.in)
		}
	}
	assert.Equal(t, "BRICKKIT.zh.md", TranslationName("BRICKKIT.md", "zh"))
	assert.Equal(t, "BRICKKIT.md", TranslationName("BRICKKIT.md", ""))
}
```

`SplitTranslation("README.zh-CN.md")` returns `ok=false`: `README.zh-CN` has a dot-suffix `zh-CN` that isn't a code,
so the name is not a valid primary either. Task 7 reports such files.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/docspec/`
Expected: FAIL — `undefined: Required`.

- [ ] **Step 3: Implement `internal/docspec/docspec.go`**

```go
// Package docspec 是组件与项目文档规范本身，写成数据：有哪些文件、每种文件要哪几节、
// 各节在每种语言里叫什么、怎样的文件名算译本、哪些词算占位。
//
// 生成骨架（manifest、agentsmd）与检查（doccheck）都从这里取，标题因此只有一份。
// 标题是文档内容的一部分，跟着那份文档的语言走，不随 CLI 的显示语言变——所以写在这里，
// 不进 i18n 目录。
package docspec

import (
	"path"
	"regexp"
	"strings"
)

// 文档文件名。
const (
	FileBrickkit = "BRICKKIT.md"
	FileAgents   = "AGENTS.md"
	FileClaude   = "CLAUDE.md"
	FileReadme   = "README.md"
	// ClaudeImport 是 CLAUDE.md 的全部内容：Claude Code 读 CLAUDE.md，其他工具读 AGENTS.md，内容只写一份。
	ClaudeImport = "@AGENTS.md"
	DirDocs      = "docs"
	// MaxTranslations 是一个组件随版本发布的 BRICKKIT.md 译本上限。
	MaxTranslations = 16
)

// Kind 是一种有固定小节的文档。
type Kind int

const (
	KindBrickkit Kind = iota
	KindComponentAgents
	KindProjectAgents
	KindReadme
)

// Section 是一个固定小节。
type Section int

const (
	Purpose Section = iota
	BeforeDeploy
	Dependencies
	Configuration
	Contracts
	ShellDecl
	CodeMap
	BuildTest
	Decisions
	Pitfalls
	BeforeChanging
	Overview
	Conventions
	WhereToLook
	UseIt
	Documentation
	Development
)

var required = map[Kind][]Section{
	KindBrickkit:        {Purpose, BeforeDeploy, Dependencies, Configuration, Contracts, ShellDecl},
	KindComponentAgents: {CodeMap, BuildTest, Decisions, Pitfalls, BeforeChanging},
	KindProjectAgents:   {Overview, Conventions, WhereToLook, Pitfalls},
	KindReadme:          {UseIt, Documentation, Development},
}

// Required 是这种文档必须有的小节，按它们在文档里的顺序。
func Required(k Kind) []Section { return append([]Section(nil), required[k]...) }

// headings 是每节在每种语言里的标题。加一种语言 = 给每一节加一列。
var headings = map[Section]map[string]string{
	Purpose:        {"en": "Purpose", "zh": "组件定位"},
	BeforeDeploy:   {"en": "Before you deploy", "zh": "部署前准备"},
	Dependencies:   {"en": "Dependencies", "zh": "依赖说明"},
	Configuration:  {"en": "Configuration", "zh": "配置指南"},
	Contracts:      {"en": "Contracts", "zh": "契约索引"},
	ShellDecl:      {"en": "Shell declaration", "zh": "外壳声明"},
	CodeMap:        {"en": "Code map", "zh": "代码地图"},
	BuildTest:      {"en": "Build and test", "zh": "构建与测试"},
	Decisions:      {"en": "Design decisions", "zh": "设计取舍"},
	Pitfalls:       {"en": "Pitfalls", "zh": "易错点"},
	BeforeChanging: {"en": "Before changing code", "zh": "改代码前自查"},
	Overview:       {"en": "Overview", "zh": "项目概述"},
	Conventions:    {"en": "Conventions", "zh": "项目约定"},
	WhereToLook:    {"en": "Where to look", "zh": "查找路由"},
	UseIt:          {"en": "Use it in a project", "zh": "在项目里使用"},
	Documentation:  {"en": "Documentation", "zh": "文档"},
	Development:    {"en": "Development", "zh": "开发"},
}

// HeadingLangs 是认得出标题的语言。
func HeadingLangs() []string { return []string{"en", "zh"} }

// Heading 是这一节在 lang 里的标题；不认识的语言给英文。
func Heading(s Section, lang string) string {
	if h, ok := headings[s][lang]; ok {
		return h
	}
	return headings[s]["en"]
}

// numbering 是标题前可有可无的编号（"1. "、"2、"），decoration 是标题后的锚点（" {#x}"）。
var (
	numbering  = regexp.MustCompile(`^\d+(\.\d+)*[.、)]?\s*`)
	decoration = regexp.MustCompile(`\s*\{#[^}]*\}$`)
)

// Matches 判断一个二级标题（已去掉 "## "）是不是这一节，任何认得的语言都算。
func Matches(s Section, heading string) bool {
	h := strings.TrimSpace(heading)
	h = decoration.ReplaceAllString(numbering.ReplaceAllString(h, ""), "")
	for _, name := range headings[s] {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return true
		}
	}
	return false
}

var langCode = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)

// ValidLang 判断 code 是不是合法的语言代码（小写 BCP 47）。
func ValidLang(code string) bool { return langCode.MatchString(code) }

// SplitTranslation 把 Markdown 文件名拆成原文名与语言：README.zh.md → README.md、zh；
// README.md → README.md、""。最后一个点前的后缀不是语言代码时（README.zh-CN.md）ok 为 false：
// 这种名字既不是原文、也不是能认的译本，检查要把它点出来，而不是当成无关文件放过。
func SplitTranslation(name string) (base, lang string, ok bool) {
	if path.Ext(name) != ".md" {
		return "", "", false
	}
	stem := strings.TrimSuffix(name, ".md")
	dot := strings.LastIndex(stem, ".")
	if dot < 0 {
		return name, "", true
	}
	suffix := stem[dot+1:]
	if !ValidLang(suffix) {
		return name, "", false
	}
	return stem[:dot] + ".md", suffix, true
}

// TranslationName 是 base 的 lang 译本的文件名；lang 为空时就是 base。
func TranslationName(base, lang string) string {
	if lang == "" {
		return base
	}
	return strings.TrimSuffix(base, ".md") + "." + lang + ".md"
}

// PlaceholderWords 是正文里不该留下的占位词（代码里的不算）。
var PlaceholderWords = []string{"TODO", "TBD", "FIXME", "待补", "后补", "待填"}
```

Note on `SplitTranslation("design.pt-br.md")`: the stem `design.pt-br` splits at the last dot into `design` and
`pt-br`. A name with a dot that is not a translation, such as `v1.2-notes.md`, has the suffix `2-notes`, which is not a
code, so it returns `ok=false`. Task 7 reports such a name only when its base is one of the four doc file names, or the
file is under `docs/` and another file in that directory is a translation. Write that rule into Task 7's check, not
here.

- [ ] **Step 4: Run the tests, add the skip-dir and code-map rows**

Run: `go test ./internal/docspec/ ./tests/i18nguard/ ./tests/docfields/`
Expected: PASS (after adding `"internal/docspec/"` to `hardcodedSkipDirs` and the `internal/docspec/` row to both code
maps: "The component and project documentation spec as data: files, sections, heading names per language, translation
names").

- [ ] **Step 5: Commit**

```bash
git add internal/docspec tests/i18nguard/i18nguard_test.go AGENTS.md AGENTS.zh.md
git commit -m "feat(docspec): the documentation spec as data — files, sections, headings, translations

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `internal/agentsmd` — the managed block, its templates, skeletons and `Ensure`

**Files:**
- Create: `internal/agentsmd/block.go`, `render.go`, `skeleton.go`, `ensure.go`,
  `templates/en/project.md`, `templates/en/component.md`, `templates/zh/project.md`, `templates/zh/component.md`,
  `agentsmd_test.go`
- Modify: `internal/i18n/locales/en.yaml`, `zh.yaml` (skeleton hints), `make generate-msgid`; both code maps.

**Interfaces:**
- Consumes: `docspec.Required`, `docspec.Heading`, `docspec.FileAgents`, `docspec.FileClaude`, `docspec.ClaudeImport`
- Produces:
  - `type Block struct{ Lang string; Start, End int }` (byte offsets of the whole block incl. markers and trailing newline)
  - `func Find(doc string) (Block, error)` — `ErrNoBlock` when absent; `ErrMalformed` (wrapped with a reason) when broken
  - `func Replace(doc string, b Block, rendered string) string`
  - `func Append(doc, rendered string) string`
  - `type Row struct{ ID, Version, Does, Docs, Home string }`
  - `type Content struct{ Lang string; Component, Project bool; Rows []Row }`
  - `func Render(c Content) string`
  - `func ParseRows(doc string, b Block) map[string]Row` (key `id@version`)
  - `func Skeleton(title string, c Content) string`
  - `const ClaudeContent = "@AGENTS.md\n"`
  - `type Mode int` with `ModeInit`, `ModeRepair`, `ModeRewrite`
  - `type Result struct{ AgentsCreated, BlockAppended, BlockRewritten, LegacyReplaced, ClaudeCreated, ClaudeAppended bool; Problem string }`
  - `func Ensure(root string, c Content, mode Mode, legacyAgentsSum string) (Result, error)`
  - `func BlockLang(root string) (string, bool)` — the recorded language of `<root>/AGENTS.md`'s block

- [ ] **Step 1: Write the failing tests**

```go
package agentsmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fence = strings.Repeat("`", 3)

func block(lang, body string) string {
	return "<!-- brickkit:managed:begin lang=" + lang + " -->\n" + body + "<!-- brickkit:managed:end -->\n"
}

func TestFindReadsLang(t *testing.T) {
	doc := "# P\n\nmine\n\n" + block("zh", "x\n")
	b, err := Find(doc)
	require.NoError(t, err)
	assert.Equal(t, "zh", b.Lang)
	assert.Equal(t, "# P\n\nmine\n\n", doc[:b.Start])
	assert.Equal(t, len(doc), b.End)
}

func TestFindIgnoresMarkersInFences(t *testing.T) {
	doc := "# P\n" + fence + "\n" + block("en", "example\n") + fence + "\n"
	_, err := Find(doc)
	assert.ErrorIs(t, err, ErrNoBlock)
}

func TestFindRejectsMalformed(t *testing.T) {
	for name, doc := range map[string]string{
		"begin only":   "<!-- brickkit:managed:begin lang=en -->\nx\n",
		"two begins":   block("en", "<!-- brickkit:managed:begin lang=en -->\n"),
		"end first":    "<!-- brickkit:managed:end -->\n" + block("en", ""),
		"bad language": block("EN!", ""),
	} {
		_, err := Find(doc)
		assert.ErrorIs(t, err, ErrMalformed, name)
	}
}

func TestReplaceKeepsCRLFOutside(t *testing.T) {
	doc := "# P\r\n\r\nmine\r\n" + block("en", "old\n") + "tail\r\n"
	b, err := Find(doc)
	require.NoError(t, err)
	out := Replace(doc, b, block("en", "new\n"))
	assert.True(t, strings.HasPrefix(out, "# P\r\n\r\nmine\r\n"))
	assert.True(t, strings.HasSuffix(out, "tail\r\n"))
	assert.Contains(t, out, "new\n")
}

func TestRenderUsesRecordedLanguage(t *testing.T) {
	zh := Render(Content{Lang: "zh", Project: true})
	assert.Contains(t, zh, "lang=zh")
	assert.Contains(t, zh, "## 组件")
	en := Render(Content{Lang: "en", Project: true})
	assert.Contains(t, en, "## Components")
	assert.Contains(t, Render(Content{Lang: "ja", Project: true}), "lang=ja", "the recorded language stays even without a template")
}

func TestRenderEscapesCells(t *testing.T) {
	out := Render(Content{Lang: "en", Project: true, Rows: []Row{{ID: "a/b", Version: "1.0.0", Does: "x | y\nz", Docs: "BRICKKIT.md", Home: "—"}}})
	assert.Contains(t, out, "| a/b | 1.0.0 | x \\| y z | BRICKKIT.md | — |\n")
}

func TestRenderSixtyRowsStaysSmall(t *testing.T) {
	var rows []Row
	for i := 0; i < 60; i++ {
		rows = append(rows, Row{ID: "domain/component-name", Version: "1.0.26", Does: strings.Repeat("d", 40), Docs: "BRICKKIT.md +zh", Home: "https://git.example.com/domain-component-name"})
	}
	assert.Less(t, len(Render(Content{Lang: "en", Project: true, Rows: rows})), 12_000)
}

func TestParseRowsRoundTrip(t *testing.T) {
	doc := Render(Content{Lang: "en", Project: true, Rows: []Row{{ID: "a/b", Version: "1.0.0", Does: "does", Docs: "BRICKKIT.md", Home: "https://h"}}})
	b, err := Find(doc)
	require.NoError(t, err)
	assert.Equal(t, Row{ID: "a/b", Version: "1.0.0", Does: "does", Docs: "BRICKKIT.md", Home: "https://h"}, ParseRows(doc, b)["a/b@1.0.0"])
}

func TestSkeletonHasRequiredSectionsThenBlock(t *testing.T) {
	s := Skeleton("demo/quote", Content{Lang: "en", Component: true})
	for _, h := range []string{"## Code map", "## Build and test", "## Design decisions", "## Pitfalls", "## Before changing code"} {
		assert.Contains(t, s, h)
	}
	b, err := Find(s)
	require.NoError(t, err)
	assert.Greater(t, b.Start, strings.Index(s, "## Before changing code"))
	assert.Contains(t, s, "<!-- TODO:")
}

func TestEnsureModes(t *testing.T) {
	root := t.TempDir()
	c := Content{Lang: "en", Project: true}

	res, err := Ensure(root, c, ModeInit, "")
	require.NoError(t, err)
	assert.True(t, res.AgentsCreated)
	assert.True(t, res.ClaudeCreated)
	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	assert.Equal(t, ClaudeContent, string(claude))

	// 作者自己的文件：init 一个字节都不碰，只报问题；repair 追加
	mine := "# Mine\n\nnotes\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(mine), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("own\n"), 0o644))
	res, err = Ensure(root, c, ModeInit, "")
	require.NoError(t, err)
	assert.NotEmpty(t, res.Problem)
	got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.Equal(t, mine, string(got))

	res, err = Ensure(root, c, ModeRepair, "")
	require.NoError(t, err)
	assert.True(t, res.BlockAppended)
	assert.True(t, res.ClaudeAppended)
	got, _ = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(string(got), mine))
	claude, _ = os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	assert.Equal(t, "own\n@AGENTS.md\n", string(claude))

	// rewrite：只改块里面
	c.Rows = []Row{{ID: "a/b", Version: "1.0.0", Does: "d", Docs: "—", Home: "—"}}
	res, err = Ensure(root, c, ModeRewrite, "")
	require.NoError(t, err)
	assert.True(t, res.BlockRewritten)
	got, _ = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(string(got), mine))
	assert.Contains(t, string(got), "| a/b | 1.0.0 |")
}

func TestEnsureKeepsRecordedLanguage(t *testing.T) {
	root := t.TempDir()
	_, err := Ensure(root, Content{Lang: "zh", Project: true}, ModeInit, "")
	require.NoError(t, err)
	_, err = Ensure(root, Content{Lang: "en", Project: true}, ModeRewrite, "")
	require.NoError(t, err)
	lang, ok := BlockLang(root)
	assert.True(t, ok)
	assert.Equal(t, "zh", lang, "a teammate with another CLI language must not flip the committed file")
}

func TestEnsureReplacesLegacyAsset(t *testing.T) {
	root := t.TempDir()
	old := "# This project is assembled with BrickKit\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(old), 0o644))
	res, err := Ensure(root, Content{Lang: "en", Project: true}, ModeInit, sum([]byte(old)))
	require.NoError(t, err)
	assert.True(t, res.LegacyReplaced)
	got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.Contains(t, string(got), "## Overview")
}
```

`TestEnsureKeepsRecordedLanguage` pins the rule that an existing block's `lang` wins over `Content.Lang` in
`ModeRewrite` and `ModeRepair`. The only way to change it is `ModeRepair` with `Content.Lang` marked explicit. Model that
as a field `ForceLang bool` on `Content` (add it to the Produces list), set only by `skills update --lang`.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/agentsmd/`
Expected: FAIL — `undefined: Find`.

- [ ] **Step 3: Write the templates**

`internal/agentsmd/templates/en/project.md`:

```markdown
## BrickKit

This project is assembled with [BrickKit](https://github.com/brickKit/brickKit): each component describes itself in its own `component.yaml`; the `brickkit` CLI resolves the graph, generates the deployment files and exits. There is no registry, config server or gateway to look for.

- `brickkit.yaml` lists the components at exact versions (it is the lock file); `deploy.yaml` says how they run (`deploy.local.yaml` replaces it on your machine while local mode is on); `config/<scope>-<name>.yaml` holds each component's environment variables, `config/vars.yaml` the shared values.
- `brickkit add` / `remove` / `upgrade` keep the three layers in step: never hand-edit one and forget another.
- Config keys are the environment variable names the component's `configSchema` declares; secrets are `${VAR}` or `file://.secrets/…`, never plaintext.
- A health check checks only its own process. To work on one component, run `brickkit up` in its directory; `brickkit up --all` runs everything again.
- Task skills are in `.claude/skills/brickkit-*`; for flags ask `brickkit <command> --help`.

## Components

A component's documentation is `BRICKKIT.md` (translations `BRICKKIT.<lang>.md`) in its source directory when a local source holds this version, otherwise in `.brickkit/manifests/<id>/<version>/`; its contracts are in `.brickkit/artifacts/<service name>/` (the ID with `/` and `.` as `-`, then the version with `.` as `-`). To change a component, read its own `AGENTS.md`.
```

`internal/agentsmd/templates/en/component.md`:

```markdown
## BrickKit

This is a BrickKit component: `component.yaml` is all the platform reads. The rules it relies on:

- `configSchema` keys are the environment variable names the code reads. Never use a reserved name: `COMPONENT_ID`, `COMPONENT_VERSION`, `PORT`, `BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG`, or any `*_ENDPOINT`.
- Dependencies are exact versions. A dependency's address arrives as `<ID>_ENDPOINT`; an optional dependency that is absent has no variable at all, so read it with a fallback.
- `/healthz` checks only this process, never a dependency. The migration command runs from the same image and must fail on an argument it does not know.
- `BRICKKIT.md` travels to every project that uses this component and is read there without the repository: keep it in step with the code, with no relative links.
- Release: raise `metadata.version`, commit, push, `brickkit release`. `brickkit lint` checks the manifest and these docs.
- The full rules: `.claude/skills/brickkit-component/SKILL.md`; for flags ask `brickkit <command> --help`.
```

`internal/agentsmd/templates/zh/project.md`:

```markdown
## BrickKit

这个项目用 [BrickKit](https://github.com/brickKit/brickKit) 组装：每个组件在自己的 `component.yaml` 里描述自己；`brickkit` 命令行解出依赖图、生成部署文件后就退出。没有注册中心、配置中心、网关，不用去找。

- `brickkit.yaml` 列出组件和它们的精确版本（它就是锁文件）；`deploy.yaml` 写怎么运行（本地模式开着时，你机器上用 `deploy.local.yaml` 整份替换它）；`config/<scope>-<name>.yaml` 是每个组件的环境变量，`config/vars.yaml` 是共用的值。
- `brickkit add` / `remove` / `upgrade` 会让三层文件一起改：别手改一层、忘了另一层。
- 配置项的键就是组件 `configSchema` 声明的环境变量名；密钥写成 `${VAR}` 或 `file://.secrets/…`，不写明文。
- 健康检查只查自己这个进程。只想改一个组件时，在它的目录里 `brickkit up`；`brickkit up --all` 回到全部运行。
- 按任务分的技能在 `.claude/skills/brickkit-*`；参数问 `brickkit <命令> --help`。

## 组件

组件的文档是 `BRICKKIT.md`（译本 `BRICKKIT.<语言>.md`）：本地源里正好是这个版本时在源码目录里，否则在 `.brickkit/manifests/<id>/<version>/`；契约在 `.brickkit/artifacts/<服务名>/`（ID 里的 `/` 和 `.` 换成 `-`，再接上 `.` 换成 `-` 的版本）。要改某个组件，读它自己的 `AGENTS.md`。
```

`internal/agentsmd/templates/zh/component.md`:

```markdown
## BrickKit

这是一个 BrickKit 组件：平台只读 `component.yaml`。它依赖的规则：

- `configSchema` 的键就是代码读的环境变量名。不能用保留名：`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`，以及任何 `*_ENDPOINT`。
- 依赖写精确版本。依赖的地址以 `<ID>_ENDPOINT` 注入；缺席的可选依赖根本没有这个变量，读的时候要带兜底。
- `/healthz` 只查本进程，不查依赖。迁移命令用同一个镜像跑，遇到不认识的参数必须直接失败。
- `BRICKKIT.md` 会随版本进入每个使用它的项目，在那里是脱离仓库单独读的：跟代码一起改，不放相对链接。
- 发版：改 `metadata.version`，提交、推送，`brickkit release`。`brickkit lint` 会检查清单和这些文档。
- 完整规则：`.claude/skills/brickkit-component/SKILL.md`；参数问 `brickkit <命令> --help`。
```

The column headings of the component table are in each language's template code below (`tableHead`), so the
templates stay pure prose.

- [ ] **Step 4: Implement `block.go`, `render.go`, `skeleton.go`, `ensure.go`**

`internal/agentsmd/block.go`:

```go
// Package agentsmd 管 AGENTS.md 里由 CLI 维护的那一段：标记、语言、渲染、骨架、补全。
//
// AGENTS.md 是作者的 AI 导读（各家工具通用的那个文件），平台只占文件末尾一对标记之间的一段：
// 平台规则与组件表。标记之外 CLI 一个字节都不写；标记里记着这段用的语言，谁跑命令都按它渲染，
// 免得两个 CLI 语言不同的同事把提交进 Git 的文件改来改去。
package agentsmd

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/mdtext"
)

const (
	beginPrefix = "<!-- brickkit:managed:begin"
	endMarker   = "<!-- brickkit:managed:end -->"
)

var beginLine = regexp.MustCompile(`^<!-- brickkit:managed:begin lang=(\S+) -->\s*$`)

// ErrNoBlock：文件里没有维护区。ErrMalformed：标记残缺或重复，不敢改。
var (
	ErrNoBlock   = errors.New("no managed block")
	ErrMalformed = errors.New("malformed managed block")
)

// Block 是维护区在文件里的位置（字节偏移，含两端标记与结束标记后的换行）与它记的语言。
type Block struct {
	Lang       string
	Start, End int
}

// Find 找到维护区。代码块里的标记不算（作者可能在文档里举例）。
func Find(doc string) (Block, error) {
	var begins, ends []int // 行号
	lines := strings.SplitAfter(doc, "\n")
	inCode := map[int]bool{}
	open := ""
	for i, l := range lines {
		if f := mdtext.FenceOf(l); f != "" && (open == "" || f == open) {
			if open == "" {
				open = f
			} else {
				open = ""
			}
			inCode[i] = true
			continue
		}
		inCode[i] = open != ""
	}
	for i, l := range lines {
		if inCode[i] {
			continue
		}
		t := strings.TrimRight(l, "\r\n")
		switch {
		case strings.HasPrefix(t, beginPrefix):
			begins = append(begins, i)
		case strings.TrimSpace(t) == endMarker:
			ends = append(ends, i)
		}
	}
	switch {
	case len(begins) == 0 && len(ends) == 0:
		return Block{}, ErrNoBlock
	case len(begins) != 1 || len(ends) != 1:
		return Block{}, fmt.Errorf("%w: %d begin, %d end markers", ErrMalformed, len(begins), len(ends))
	case ends[0] < begins[0]:
		return Block{}, fmt.Errorf("%w: the end marker comes first", ErrMalformed)
	}
	m := beginLine.FindStringSubmatch(strings.TrimRight(lines[begins[0]], "\r\n"))
	if m == nil || !docspec.ValidLang(m[1]) {
		return Block{}, fmt.Errorf("%w: the begin marker has no valid lang=", ErrMalformed)
	}
	start := 0
	for _, l := range lines[:begins[0]] {
		start += len(l)
	}
	end := start
	for _, l := range lines[begins[0] : ends[0]+1] {
		end += len(l)
	}
	return Block{Lang: m[1], Start: start, End: end}, nil
}

// Replace 用 rendered 换掉维护区，其余字节原样保留。
func Replace(doc string, b Block, rendered string) string {
	return doc[:b.Start] + rendered + doc[b.End:]
}

// Append 把维护区接在文件末尾，前面空一行。
func Append(doc, rendered string) string {
	switch {
	case doc == "":
		return rendered
	case strings.HasSuffix(doc, "\n\n"):
		return doc + rendered
	case strings.HasSuffix(doc, "\n"):
		return doc + "\n" + rendered
	default:
		return doc + "\n\n" + rendered
	}
}

// ParseRows 读出现有维护区里组件表的行（键 id@version）。缓存里缺某个组件时，
// 新渲染的那一行沿用这里的单元格：新克隆的仓库还没取过组件，不能因此把已经写好的说明抹成"—"。
func ParseRows(doc string, b Block) map[string]Row {
	rows := map[string]Row{}
	for _, l := range strings.Split(doc[b.Start:b.End], "\n") {
		c := mdtext.TableCells(l)
		if len(c) != 5 || !strings.Contains(c[0], "/") {
			continue
		}
		rows[c[0]+"@"+c[1]] = Row{ID: c[0], Version: c[1], Does: c[2], Docs: c[3], Home: c[4]}
	}
	return rows
}
```

`internal/agentsmd/render.go`:

```go
package agentsmd

import (
	"embed"
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
)

//go:embed templates
var templates embed.FS

// Row 是组件表的一行。单元格都是写进 Git 的事实：只来自 brickkit.yaml 和那个版本自己，
// 不来自"这台机器上有没有本地源码、是哪个安装源给的"——那会让同事之间来回改写。
type Row struct {
	ID, Version string
	Does        string // metadata.description
	Docs        string // "BRICKKIT.md +zh"，没有文档时 "—"
	Home        string // metadata.repository，没有时 "—"
}

// Content 决定维护区里有什么。
type Content struct {
	// Lang 是要用的语言；文件里已有维护区时以它记的为准，除非 ForceLang。
	Lang      string
	ForceLang bool
	// Component：这里是组件仓库（有 component.yaml），放组件作者要守的规则。
	Component bool
	// Project：这里有 brickkit.yaml，放平台规则（纯项目时）与组件表。
	Project bool
	Rows    []Row
}

// tableHead 是组件表的表头，跟着维护区的语言。
var tableHead = map[string]string{
	"en": "| Component | Version | What it does | Docs | Home |\n|---|---|---|---|---|\n",
	"zh": "| 组件 | 版本 | 干什么 | 文档 | 主页 |\n|---|---|---|---|---|\n",
}

var managedNote = map[string]string{
	"en": "<!-- maintained by brickkit (init, add, remove, upgrade, skills update): edits between these markers are overwritten -->\n",
	"zh": "<!-- 由 brickkit 维护（init、add、remove、upgrade、skills update）：这对标记之间的改动会被覆盖 -->\n",
}

func tmpl(lang, name string) string {
	data, err := templates.ReadFile("templates/" + lang + "/" + name)
	if err != nil {
		data, _ = templates.ReadFile("templates/en/" + name)
	}
	return string(data)
}

func pick(m map[string]string, lang string) string {
	if v, ok := m[lang]; ok {
		return v
	}
	return m["en"]
}

// Render 渲染整段维护区（含两端标记）。
// 组件仓库放组件规则；纯项目放项目规则；两者都有（工作台）时放组件规则加组件表。
func Render(c Content) string {
	var b strings.Builder
	b.WriteString("<!-- brickkit:managed:begin lang=" + c.Lang + " -->\n")
	b.WriteString(pick(managedNote, c.Lang))
	b.WriteString("\n")
	if c.Component {
		b.WriteString(tmpl(c.Lang, "component.md"))
	}
	if c.Project {
		project := tmpl(c.Lang, "project.md")
		if c.Component { // 工作台：平台规则已经有了组件那份，只要组件表那一节
			if i := strings.Index(project, "\n## "); i >= 0 {
				project = project[i+1:]
			}
		}
		if !strings.HasSuffix(b.String(), "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString(project)
		b.WriteString("\n")
		b.WriteString(pick(tableHead, c.Lang))
		for _, r := range c.Rows {
			b.WriteString("| " + strings.Join([]string{cell(r.ID), cell(r.Version), cell(r.Does), cell(r.Docs), cell(r.Home)}, " | ") + " |\n")
		}
	}
	b.WriteString(endMarker + "\n")
	return b.String()
}

// cell 把一段文字放进表格单元格：竖线转义、换行并成空格。
func cell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "—"
	}
	return strings.ReplaceAll(s, "|", `\|`)
}

// DocsCell 是"文档"一列：有原文写 BRICKKIT.md，再加每个译本的语言（+zh）；都没有时 "—"。
func DocsCell(hasPrimary bool, langs []string) string {
	var parts []string
	if hasPrimary {
		parts = append(parts, docspec.FileBrickkit)
	}
	for _, l := range langs {
		parts = append(parts, "+"+l)
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " ")
}
```

Add `func DocsCell(hasPrimary bool, langs []string) string` to the Produces list. In `TestRenderEscapesCells`, the
newline in `Does` becomes a space via `strings.Fields`, so the expected cell is `x \| y z`.

`internal/agentsmd/skeleton.go`:

```go
package agentsmd

import (
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// ClaudeContent 是 CLAUDE.md 的全部内容。
const ClaudeContent = docspec.ClaudeImport + "\n"

// hints 是骨架里每节的填写提示（CLI 当前语言；骨架是"现在生成一份"，语言就是此刻的语言）。
var hints = map[docspec.Section]msgid.ID{
	docspec.CodeMap:        msgid.AgentsmdHintCodeMap,
	docspec.BuildTest:      msgid.AgentsmdHintBuildTest,
	docspec.Decisions:      msgid.AgentsmdHintDecisions,
	docspec.Pitfalls:       msgid.AgentsmdHintPitfalls,
	docspec.BeforeChanging: msgid.AgentsmdHintBeforeChanging,
	docspec.Overview:       msgid.AgentsmdHintOverview,
	docspec.Conventions:    msgid.AgentsmdHintConventions,
	docspec.WhereToLook:    msgid.AgentsmdHintWhereToLook,
}

// Skeleton 是一份新的 AGENTS.md：标题、一句指路、作者要写的各节（带 TODO 提示）、末尾的维护区。
// 有 component.yaml 时是组件那五节，否则是项目那四节。
func Skeleton(title string, c Content) string {
	kind, intro := docspec.KindProjectAgents, msgid.AgentsmdIntroProject
	if c.Component {
		kind, intro = docspec.KindComponentAgents, msgid.AgentsmdIntroComponent
	}
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	b.WriteString(i18n.T(intro) + "\n\n")
	for _, s := range docspec.Required(kind) {
		b.WriteString("## " + docspec.Heading(s, c.Lang) + "\n\n")
		b.WriteString("<!-- TODO: " + i18n.T(hints[s]) + " -->\n\n")
	}
	b.WriteString(Render(c))
	return b.String()
}
```

i18n keys (add to both catalogs, then `make generate-msgid`):

| Key | en | zh |
| --- | --- | --- |
| `agentsmd.intro_project` | `The AI guide to this project: what it is, the rules every component here follows, where to look. The component table at the end is maintained by brickkit.` | `这个项目的 AI 导读：项目是什么、这里每个组件都要守的规则、到哪里找东西。末尾的组件表由 brickkit 维护。` |
| `agentsmd.intro_component` | `The AI guide to developing this component. How to use it, its boundaries and contracts: BRICKKIT.md. Dependencies, configuration and deployment: component.yaml.` | `开发这个组件的 AI 导读。怎么用、边界和契约：BRICKKIT.md。依赖、配置、部署：component.yaml。` |
| `agentsmd.hint_code_map` | `two tables — "Path / Owns" (paths in backticks, directories ending in /) and "Feature / Start here / Then"; brickkit lint checks every path exists` | `两张表——"路径 / 管什么"（路径用反引号，目录以 / 结尾）与"功能 / 从这里开始 / 然后"；brickkit lint 会核对每条路径都在` |
| `agentsmd.hint_build_test` | `the exact commands to build, test, run locally and check the contract, and what success looks like` | `构建、测试、本地运行、契约检查的确切命令，以及成功时看到什么` |
| `agentsmd.hint_decisions` | `why it does not depend on some other component; alternatives rejected and why — longer reasoning goes in docs/ and is linked here` | `为什么不依赖某某；否决过哪些做法、为什么——写不下的放 docs/，这里链过去` |
| `agentsmd.hint_pitfalls` | `a table: Never / Symptom / Why — only what is specific to this one, never a copy of a rule written elsewhere` | `一张表：不许 / 症状 / 原因——只写这里特有的，不抄别处已经写了的规则` |
| `agentsmd.hint_before_changing` | `3 to 8 checks to run before changing code here` | `改这里的代码前要过的 3 到 8 条检查` |
| `agentsmd.hint_overview` | `what this project is, its domains, how its components group` | `这个项目是什么、分哪些领域、组件怎么分组` |
| `agentsmd.hint_conventions` | `the rules every component in this project follows: stack, port and schema registries, naming, review rules — components never repeat them` | `这个项目里每个组件都要守的规则：技术栈、端口与 schema 登记、命名、评审规则——组件里不再重复` |
| `agentsmd.hint_where_to_look` | `a table: what you are doing (the words you would search for) → the file to read first; last line: not here? the component table below, then the component's AGENTS.md` | `一张表：在做什么（会拿去搜的那几个词）→ 先读哪个文件；最后一行：这里没有？看下面的组件表，再看那个组件的 AGENTS.md` |

`internal/agentsmd/ensure.go`:

```go
package agentsmd

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
)

// Mode 是这次调用可以做到哪一步。
type Mode int

const (
	// ModeInit：缺的文件建出来；已有的文件一个字节都不动，只改写已有的维护区。
	ModeInit Mode = iota
	// ModeRepair：使用者明确要求（skills update）——还可以把维护区追加到没有它的 AGENTS.md、
	// 把 @AGENTS.md 追加到没有它的 CLAUDE.md。
	ModeRepair
	// ModeRewrite：只改写已有的维护区（add / remove / upgrade），别的都不碰。
	ModeRewrite
)

// Result 是做了什么，以及没能做、要告诉使用者的问题。
type Result struct {
	AgentsCreated, BlockAppended, BlockRewritten, LegacyReplaced bool
	ClaudeCreated, ClaudeAppended                                bool
	// Problem 非空：AGENTS.md 没有维护区（或标记坏了）、或 CLAUDE.md 没有 @AGENTS.md，这次没改。
	Problem string
	// ClaudeMissingImport：CLAUDE.md 在，但不引 AGENTS.md。
	ClaudeMissingImport bool
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// BlockLang 是 root/AGENTS.md 维护区记的语言；没有文件或没有维护区时 ok 为 false。
func BlockLang(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, docspec.FileAgents))
	if err != nil {
		return "", false
	}
	b, err := Find(string(data))
	if err != nil {
		return "", false
	}
	return b.Lang, true
}

// Ensure 让 root 下的 AGENTS.md 与 CLAUDE.md 符合 mode 允许的样子。
// legacyAgentsSum 是旧版 skills.lock 给 AGENTS.md 记的指纹：文件恰好是旧版 CLI 装的那份、没改过时，
// 它本来就是 CLI 的文件，整份换成新骨架。
func Ensure(root string, c Content, mode Mode, legacyAgentsSum string) (Result, error) {
	var res Result
	path := filepath.Join(root, docspec.FileAgents)
	title := filepath.Base(root)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if mode != ModeRewrite {
			if err := os.WriteFile(path, []byte(Skeleton(title, c)), 0o644); err != nil {
				return res, err
			}
			res.AgentsCreated = true
		}
	case err != nil:
		return res, err
	default:
		doc := string(data)
		b, findErr := Find(doc)
		switch {
		case findErr == nil:
			if !c.ForceLang {
				c.Lang = b.Lang
			}
			if updated := Replace(doc, b, Render(c)); updated != doc {
				if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
					return res, err
				}
				res.BlockRewritten = true
			}
		case errors.Is(findErr, ErrNoBlock) && legacyAgentsSum != "" && sum(data) == legacyAgentsSum && mode != ModeRewrite:
			if err := os.WriteFile(path, []byte(Skeleton(title, c)), 0o644); err != nil {
				return res, err
			}
			res.LegacyReplaced = true
		case errors.Is(findErr, ErrNoBlock) && mode == ModeRepair:
			if err := os.WriteFile(path, []byte(Append(doc, Render(c))), 0o644); err != nil {
				return res, err
			}
			res.BlockAppended = true
		default:
			res.Problem = findErr.Error()
		}
	}
	if mode == ModeRewrite {
		return res, nil
	}
	return res, ensureClaude(root, mode, &res)
}

func ensureClaude(root string, mode Mode, res *Result) error {
	path := filepath.Join(root, docspec.FileClaude)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		res.ClaudeCreated = true
		return os.WriteFile(path, []byte(ClaudeContent), 0o644)
	case err != nil:
		return err
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == docspec.ClaudeImport {
			return nil
		}
	}
	if mode != ModeRepair {
		res.ClaudeMissingImport = true
		return nil
	}
	res.ClaudeAppended = true
	return os.WriteFile(path, []byte(Append(string(data), ClaudeContent)[0:]), 0o644)
}
```

`Append(doc, ClaudeContent)` puts a blank line before `@AGENTS.md`. The test expects `"own\n@AGENTS.md\n"`, so for
CLAUDE.md append without the blank line: replace the last statement with
`content := string(data); if !strings.HasSuffix(content, "\n") { content += "\n" }; return os.WriteFile(path, []byte(content+ClaudeContent), 0o644)`.

- [ ] **Step 5: Run the tests**

Run: `make generate-msgid && go test ./internal/agentsmd/`
Expected: PASS. If `TestReplaceKeepsCRLFOutside` fails because `Find` saw `\r\n` endings on marker lines, the
`TrimRight(l, "\r\n")` handling is what fixes it — keep it.

- [ ] **Step 6: Code-map rows, lint, commit**

Add `internal/agentsmd/` to both code maps: "The CLI-maintained block at the end of `AGENTS.md` (platform rules,
component table, recorded language), the `AGENTS.md` / `CLAUDE.md` skeletons". Run `make -k lint`.

```bash
git add internal/agentsmd internal/i18n/locales internal/msgid AGENTS.md AGENTS.zh.md
git commit -m "feat(agentsmd): the managed block in AGENTS.md — markers with a language, templates, skeletons, Ensure

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: The project's `AGENTS.md` replaces the project-level `BRICKKIT.md`

**Files:**
- Create: `internal/project/agents.go`, `internal/project/agents_test.go`
- Delete: `internal/project/projectdoc.go` (and its test cases in `init_test.go`: `TestRenderProjectDocTables` and
  any `ProjectDoc*` assertions), `projectDocHead`, `managedBlock`, `hasManagedBlock`
- Modify: `internal/project/init.go` (plan/apply), `internal/project/layout.go` (`FileProjectDoc` removed,
  `ProjectAgentsPath`, `CachedDocLangs`), `internal/cli/init.go`, `internal/cli/install_apply.go`,
  `internal/manifest/types.go` (`Metadata.Repository`), `schemas/` (`make generate-schemas`), i18n catalogs, CLI tests in
  `internal/cli/init_test.go` and the install tests that assert on `BRICKKIT.md`

**Interfaces:**
- Consumes: `agentsmd.Content`, `agentsmd.Row`, `agentsmd.DocsCell`, `agentsmd.Ensure`, `agentsmd.ModeInit`,
  `agentsmd.ModeRewrite`, `agentsmd.Find`, `agentsmd.ParseRows`, `docspec.SplitTranslation`
- Produces:
  - `func (l Layout) AgentsPath() string`
  - `func (l Layout) CachedDocLangs(id, version string) (hasPrimary bool, langs []string)`
  - `func AgentsContent(l Layout, decl *projfile.File, lang string) agentsmd.Content`
  - `func WriteAgentsBlock(l Layout, p *Project) (agentsmd.Result, error)`
  - `func ObsoleteProjectMap(l Layout) bool`
  - `Metadata.Repository string \`yaml:"repository,omitempty"\``
  - `CompletePlan.Agents agentsmd.Result` (filled by Apply), `CompletePlan.ObsoleteMap bool`

- [ ] **Step 1: Write the failing tests**

`internal/project/agents_test.go`:

```go
package project_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
)

func cache(t *testing.T, l project.Layout, id, version, manifest string, docs ...string) {
	t.Helper()
	dir := l.CachedManifestDir(id, version)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.yaml"), []byte(manifest), 0o644))
	for _, d := range docs {
		require.NoError(t, os.WriteFile(filepath.Join(dir, d), []byte("# d\n"), 0o644))
	}
}

func TestAgentsRowsFromCache(t *testing.T) {
	l := project.NewLayout(t.TempDir())
	cache(t, l, "erp/sales", "1.0.26", "metadata: {id: erp/sales, version: 1.0.26, description: Sales orders, repository: https://git.example.com/erp-sales}\n",
		"BRICKKIT.md", "BRICKKIT.zh.md", "BRICKKIT.zh-CN.md")
	decl := &projfile.File{Project: "p", Components: []projfile.Component{{ID: "erp/sales", Version: "1.0.26"}}}
	c := project.AgentsContent(l, decl, "en")
	require.Len(t, c.Rows, 1)
	assert.Equal(t, agentsmd.Row{ID: "erp/sales", Version: "1.0.26", Does: "Sales orders", Docs: "BRICKKIT.md +zh", Home: "https://git.example.com/erp-sales"}, c.Rows[0])
	assert.True(t, c.Project)
	assert.False(t, c.Component)
}

func TestAgentsRowsKeepCellsWhenCacheMissing(t *testing.T) {
	root := t.TempDir()
	l := project.NewLayout(root)
	old := agentsmd.Render(agentsmd.Content{Lang: "en", Project: true, Rows: []agentsmd.Row{{ID: "a/b", Version: "1.0.0", Does: "kept", Docs: "BRICKKIT.md", Home: "https://h"}}})
	require.NoError(t, os.WriteFile(l.AgentsPath(), []byte("# P\n\n"+old), 0o644))
	decl := &projfile.File{Project: "p", Components: []projfile.Component{{ID: "a/b", Version: "1.0.0"}, {ID: "c/d", Version: "2.0.0"}}}
	c := project.AgentsContent(l, decl, "en")
	assert.Equal(t, "kept", c.Rows[0].Does)
	assert.Equal(t, "—", c.Rows[1].Does)
}

func TestObsoleteProjectMap(t *testing.T) {
	root := t.TempDir()
	l := project.NewLayout(root)
	assert.False(t, project.ObsoleteProjectMap(l))
	require.NoError(t, os.WriteFile(filepath.Join(root, "BRICKKIT.md"), []byte("# p\n<!-- brickkit:managed:begin -->\n<!-- brickkit:managed:end -->\n"), 0o644))
	assert.True(t, project.ObsoleteProjectMap(l))
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("x"), 0o644))
	assert.False(t, project.ObsoleteProjectMap(l), "in a component repository BRICKKIT.md is the component's own doc")
}
```

Add to `internal/project/init_test.go` (replacing the removed project-doc tests):

```go
func TestInitWritesAgentsAndClaudeNotProjectMap(t *testing.T) {
	root := t.TempDir()
	l := project.NewLayout(root)
	plan, err := project.PlanComplete(l, "shop")
	require.NoError(t, err)
	require.NoError(t, plan.Apply(l))
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(agents), "## Overview")
	assert.Contains(t, string(agents), "<!-- brickkit:managed:begin lang=")
	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, "@AGENTS.md\n", string(claude))
	assert.NoFileExists(t, filepath.Join(root, "BRICKKIT.md"))
}

func TestInitWorkbenchAgentsHasComponentSections(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("metadata: {id: demo/x, version: 0.1.0}\n"), 0o644))
	l := project.NewLayout(root)
	plan, err := project.PlanComplete(l, "x")
	require.NoError(t, err)
	require.NoError(t, plan.Apply(l))
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	assert.Contains(t, string(agents), "## Code map")
	assert.Contains(t, string(agents), "reserved name")
}
```

Use `i18n.SetCurrent(i18n.EN)` at the start of these tests if the package's tests do not already pin English (check
`internal/project`'s `TestMain`).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/project/`
Expected: FAIL — `undefined: project.AgentsContent`, `l.AgentsPath undefined`.

- [ ] **Step 3: Implement**

`internal/manifest/types.go` — `Metadata` gains, after `APIDocs`:

```go
	// Repository 是组件仓库或主页的地址，可选。项目 AGENTS.md 的组件表把它写进"主页"一列，
	// 网页上读项目仓库的人（那里没有 .brickkit/）也能点到每个组件。
	Repository string `yaml:"repository,omitempty"`
```

Run `make generate-schemas` (the JSON schema gains `metadata.repository`).

`internal/project/layout.go`: delete `FileProjectDoc` and `ProjectDocPath`; add:

```go
// AgentsPath 是项目根的 AGENTS.md：项目自己的 AI 导读，末尾一段由 CLI 维护。
func (l Layout) AgentsPath() string { return l.path(docspec.FileAgents) }

// CachedDocLangs 是缓存里这个组件版本带着的文档：有没有原文 BRICKKIT.md，以及各译本的语言（排好序）。
// 名字不是合法译本的文件（BRICKKIT.zh-CN.md）不算。
func (l Layout) CachedDocLangs(id, version string) (hasPrimary bool, langs []string) {
	entries, _ := os.ReadDir(l.CachedManifestDir(id, version))
	for _, e := range entries {
		base, lang, ok := docspec.SplitTranslation(e.Name())
		if !ok || base != docspec.FileBrickkit {
			continue
		}
		if lang == "" {
			hasPrimary = true
		} else {
			langs = append(langs, lang)
		}
	}
	sort.Strings(langs)
	return hasPrimary, langs
}
```

(`FileCachedDoc` stays: it is the primary file name inside the cache.)

`internal/project/agents.go`:

```go
package project

// 本文件是项目 AGENTS.md 末尾那段由 CLI 维护的内容：平台规则与组件表。
// 表里只写对所有人都一样的事实（brickkit.yaml 与组件那个版本自己说的），见 agentsmd。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/projfile"
)

// AgentsContent 算出 l 下 AGENTS.md 维护区该有的内容。lang 只在文件里还没有维护区时用。
// 缓存里缺某个组件版本时，那一行沿用现有维护区里的单元格。
func AgentsContent(l Layout, decl *projfile.File, lang string) agentsmd.Content {
	c := agentsmd.Content{Lang: lang, Project: true, Component: exists(filepath.Join(l.Root, manifest.FileName))}
	var previous map[string]agentsmd.Row
	if data, err := os.ReadFile(l.AgentsPath()); err == nil {
		if b, err := agentsmd.Find(string(data)); err == nil {
			previous = agentsmd.ParseRows(string(data), b)
		}
	}
	for _, comp := range decl.Components {
		row := agentsmd.Row{ID: comp.ID, Version: comp.Version, Does: "—", Docs: "—", Home: "—"}
		if m, err := manifest.ParseFile(l.CachedManifestPath(comp.ID, comp.Version)); err == nil {
			row.Does = orDash(m.Metadata.Description)
			row.Home = orDash(m.Metadata.Repository)
			row.Docs = agentsmd.DocsCell(l.CachedDocLangs(comp.ID, comp.Version))
		} else if old, ok := previous[comp.Ref()]; ok {
			row = old
		}
		c.Rows = append(c.Rows, row)
	}
	return c
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// WriteAgentsBlock 改写项目 AGENTS.md 已有的维护区（add / remove / upgrade 之后）。
// 没有文件、没有维护区都不建：那是 init / skills update 的事；结果里的 Problem 交给命令决定要不要说。
func WriteAgentsBlock(l Layout, p *Project) (agentsmd.Result, error) {
	return agentsmd.Ensure(l.Root, AgentsContent(l, p.Decl, ""), agentsmd.ModeRewrite, "")
}

// ObsoleteProjectMap 判断项目根有没有旧版的项目地图 BRICKKIT.md（带旧的维护区标记）。
// 组件仓库（有 component.yaml）里的 BRICKKIT.md 是组件自己的文档，不算。
func ObsoleteProjectMap(l Layout) bool {
	if exists(filepath.Join(l.Root, manifest.FileName)) {
		return false
	}
	data, err := os.ReadFile(filepath.Join(l.Root, docspec.FileBrickkit))
	if errors.Is(err, fs.ErrNotExist) || err != nil {
		return false
	}
	return strings.Contains(string(data), "<!-- brickkit:managed:begin")
}
```

`Ensure` reads the AGENTS file name from `docspec.FileAgents`, so `AgentsPath` and `Ensure` agree.

`internal/project/init.go`:
- Remove `ProjectDoc`, `ProjectDocUnmanaged` from `CompletePlan`; add `Agents agentsmd.Result` (what `Apply` did) and
  `ObsoleteMap bool`. Replace the `switch` at the end of `planComplete` with `plan.ObsoleteMap = ObsoleteProjectMap(l)`.
- In `Apply`, replace the `if p.ProjectDoc { … }` block with:

```go
	decl := &projfile.File{Project: p.Name}
	if parsed, err := projfile.ParseFile(l.DeclPath()); err == nil {
		decl = parsed
	}
	res, err := agentsmd.Ensure(l.Root, AgentsContent(l, decl, string(i18n.Current())), agentsmd.ModeInit, p.LegacyAgentsSum)
	if err != nil {
		return ioError(i18n.T(msgid.ActionWriteFile), l.AgentsPath(), err)
	}
	p.Agents = res
```

- Add `LegacyAgentsSum string` to `CompletePlan` (set by the CLI from the old lock in Task 5; empty until then).
- The workbench's `Skeleton` title is the directory name; for a component it should be the component ID. Make
  `Ensure` take the title through `Content`: add `Title string` to `agentsmd.Content` (default `filepath.Base(root)` when
  empty), and set it here to `p.Name` for a project, or to the manifest's `metadata.id` for a workbench (parse
  `component.yaml` with `manifest.ParseFile`; fall back to `p.Name`).

`internal/cli/init.go`:
- `runInitCreate`: replace the `FileProjectDoc` line with
  `opts.Printf("   📄 %-21s%s\n", docspec.FileAgents, i18n.T(msgid.CliInitAgentsGuide))` and
  `opts.Printf("   📄 %-21s%s\n", docspec.FileClaude, i18n.T(msgid.CliInitClaudeImport))`.
- `renderCompletePlan`: drop the `plan.ProjectDoc` lines. `runInitComplete`: replace the `ProjectDocUnmanaged` note
  with `renderAgentsResult(opts, plan.Agents)` and, when `plan.ObsoleteMap`, a warning
  `clierr.Warn(clierr.CodeProjectMapObsolete, i18n.T(msgid.CliInitProjectMapObsolete))` with the hint
  `i18n.T(msgid.CliInitHintMoveProjectMap)`. `CodeProjectMapObsolete` is added in Task 7: in this task, use
  `clierr.CodeConfigInvalid`, and Task 7 switches the code.
- `finishInit`: replace `project.WriteProjectDoc(layout, proj)` with `project.WriteAgentsBlock(layout, proj)`.
- `installSkills`: drop the `AGENTS.md` line (the file is no longer a skill asset — Task 5 removes the asset; in this
  task leave the asset in place but print nothing for it).
- New helper in `internal/cli/agents.go`:

```go
// renderAgentsResult 说清 AGENTS.md / CLAUDE.md 这次怎么了：建了、追加了、或者因为是作者的文件而没动。
func renderAgentsResult(opts *Options, res agentsmd.Result) {
	if res.LegacyReplaced {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliAgentsLegacyReplaced, docspec.FileAgents))
	}
	if res.BlockAppended {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliAgentsBlockAppended, docspec.FileAgents))
	}
	if res.ClaudeAppended {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliAgentsClaudeAppended, docspec.ClaudeImport, docspec.FileClaude))
	}
	if res.Problem != "" {
		opts.Printf("%s", opts.render(clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliAgentsBlockMissing, docspec.FileAgents)).
			WithDetail(i18n.T(msgid.LabelReason), res.Problem).
			WithHint(i18n.T(msgid.CliAgentsHintSkillsUpdate))))
	}
	if res.ClaudeMissingImport {
		opts.Printf("%s", opts.render(clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliAgentsClaudeMissingImport, docspec.FileClaude, docspec.ClaudeImport)).
			WithHint(i18n.T(msgid.CliAgentsHintSkillsUpdate))))
	}
}
```

(Task 7 switches these two warnings to `AGENTS_BLOCK_MISSING` / `CLAUDE_IMPORT_MISSING`.)

`internal/cli/install_apply.go:128-140`: replace both `project.WriteProjectDoc(layout, proj)` calls with
`project.WriteAgentsBlock(layout, proj)` (ignore the `Result` — no output on add/remove/upgrade), and change the
warning text key `CliInstallProjectDocFailed` to take `docspec.FileAgents`.

i18n keys (both catalogs):

| Key | en | zh |
| --- | --- | --- |
| `cli.init.agents_guide` | `the project's AI guide; the component table at its end is maintained by brickkit` | `项目的 AI 导读；末尾的组件表由 brickkit 维护` |
| `cli.init.claude_import` | `@AGENTS.md: Claude Code reads AGENTS.md through it` | `@AGENTS.md：Claude Code 通过它读 AGENTS.md` |
| `cli.init.project_map_obsolete` | `BRICKKIT.md here is the old project map: the component table now lives at the end of AGENTS.md` | `这里的 BRICKKIT.md 是旧版的项目地图：组件表现在在 AGENTS.md 末尾` |
| `cli.init.hint_move_project_map` | `move any notes of your own into AGENTS.md, then delete BRICKKIT.md (brickkit never deletes it for you)` | `把你自己写的内容挪进 AGENTS.md，再删掉 BRICKKIT.md（brickkit 不会替你删）` |
| `cli.agents.legacy_replaced` | `%[1]s: replaced the guide an earlier brickkit installed (it was never edited) with the new skeleton` | `%[1]s：之前 brickkit 装的那份导读没被改过，已换成新骨架` |
| `cli.agents.block_appended` | `%[1]s: appended the brickkit-maintained block at the end` | `%[1]s：已在末尾追加由 brickkit 维护的一段` |
| `cli.agents.claude_appended` | `appended %[1]s to %[2]s` | `已把 %[1]s 追加到 %[2]s` |
| `cli.agents.block_missing` | `%[1]s has no brickkit-maintained block, so its component table is not kept up to date` | `%[1]s 里没有由 brickkit 维护的一段，组件表不会自动更新` |
| `cli.agents.claude_missing_import` | `%[1]s does not contain %[2]s, so Claude Code does not read AGENTS.md` | `%[1]s 里没有 %[2]s，Claude Code 不会读 AGENTS.md` |
| `cli.agents.hint_skills_update` | `brickkit skills update adds it (an explicit request; nothing else changes your file)` | `brickkit skills update 会把它加上（明确要求才加；别的命令不改你的文件）` |

Remove the keys `project.doc.*` and `cli.init.project_doc*` that nothing uses any more (`make generate-msgid` then fails
the build wherever a stale reference remains — fix each).

- [ ] **Step 4: Update the CLI tests**

`grep -rn "BRICKKIT.md" internal/cli/*_test.go internal/project/*_test.go` lists every assertion on the old project map.
For each: an `init`/`add`/`remove`/`upgrade` test that checked the project `BRICKKIT.md` table now checks the same row in
`AGENTS.md` (e.g. `assert.Contains(t, readFile(t, filepath.Join(dir, "AGENTS.md")), "| demo/hello | 1.0.0 |")`); a test
that checked "an unmanaged BRICKKIT.md is left alone" becomes "an AGENTS.md without a block is left alone and warned
about". Component-repository tests that read the component's own `BRICKKIT.md` stay as they are.

- [ ] **Step 5: Run the tests**

Run: `make generate-msgid generate-schemas && systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 go test ./internal/project/ ./internal/cli/ ./internal/manifest/ ./tests/...`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

Run: `make -k lint` (code maps: replace the `internal/project/` row's "the project map `BRICKKIT.md`" with "the
project's `AGENTS.md` block").

```bash
git add -A internal schemas AGENTS.md AGENTS.zh.md
git commit -m "feat(project): the component table moves into AGENTS.md; init writes AGENTS.md and CLAUDE.md, not a project BRICKKIT.md

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Skills carry their own marker; no `skills.lock`; `AGENTS.md` leaves the skill assets

**Files:**
- Create: `internal/skills/marker.go`, `internal/skills/marker_test.go`
- Delete: `internal/skills/assets/en/AGENTS.md`, `internal/skills/assets/zh/AGENTS.md`
- Modify: `internal/skills/install.go`, `internal/skills/lock.go` (becomes read-only legacy: `LoadLegacyLock`),
  `internal/skills/*_test.go`, `internal/cli/skills.go`, `internal/cli/init.go` (`installSkills`, legacy sum),
  `internal/project/layout.go` (`SkillsLockPath` → `LegacySkillsLockPath`), i18n catalogs

**Interfaces:**
- Consumes: `agentsmd.BlockLang`, `agentsmd.Ensure`, `agentsmd.ModeRepair`, `project.AgentsContent`
- Produces:
  - `func Mark(content []byte, version string) []byte`
  - `func ReadMarker(content []byte) (body []byte, version, recordedSum string, ok bool)`
  - `Installer` fields: `Root`, `Version`, `Scope`, `Lang`, `LegacyLockPath`
  - `func (in Installer) LegacyAgentsSum() string`
  - `func (in Installer) Apply()` deletes the legacy lock after a successful run
  - `States`: unchanged names; `stateOf` reads markers

- [ ] **Step 1: Write the failing tests**

`internal/skills/marker_test.go`:

```go
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkRoundTrip(t *testing.T) {
	body := []byte("# skill\n\ntext\n")
	marked := Mark(body, "0.5.0")
	got, v, s, ok := ReadMarker(marked)
	require.True(t, ok)
	assert.Equal(t, body, got)
	assert.Equal(t, "0.5.0", v)
	assert.Equal(t, Sum(normalize(body)), s)
}

func TestMarkerSumIgnoresTrailingNewlines(t *testing.T) {
	marked := Mark([]byte("# skill\n"), "0.5.0")
	edited := append([]byte(nil), marked...)
	edited = edited[:len(edited)-1] // 编辑器去掉了最后的换行
	_, _, s, ok := ReadMarker(edited)
	require.True(t, ok)
	body, _, _, _ := ReadMarker(edited)
	assert.Equal(t, s, Sum(normalize(body)))
}

func TestStatesFromMarkers(t *testing.T) {
	in := Installer{Root: t.TempDir(), Version: "9.9.9"}
	a := AssetsFor(ScopeProject, "en")[0]
	want, _ := a.Content()
	target := filepath.Join(in.Root, a.Target)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))

	st, _ := in.stateOf(a)
	assert.Equal(t, StateMissing, st.State)

	require.NoError(t, os.WriteFile(target, Mark(want, "9.9.9"), 0o644))
	st, _ = in.stateOf(a)
	assert.Equal(t, StateCurrent, st.State)

	require.NoError(t, os.WriteFile(target, Mark([]byte("older text\n"), "0.1.0"), 0o644))
	st, _ = in.stateOf(a)
	assert.Equal(t, StateOutdated, st.State)
	assert.Equal(t, "0.1.0", st.FromVersion)

	edited := append(Mark([]byte("older text\n"), "0.1.0"), []byte("my note\n")...)
	require.NoError(t, os.WriteFile(target, edited, 0o644))
	st, _ = in.stateOf(a)
	assert.Equal(t, StateModified, st.State, "text after the marker counts as part of the body")

	require.NoError(t, os.WriteFile(target, []byte("no marker\n"), 0o644))
	st, _ = in.stateOf(a)
	assert.Equal(t, StateUntracked, st.State)
}

func TestFreshCloneUpdatesWithoutLock(t *testing.T) {
	in := Installer{Root: t.TempDir(), Version: "1.0.0"}
	_, err := in.Apply()
	require.NoError(t, err)
	// 同事新克隆：.brickkit/ 不在，但文件自己带着记录
	in2 := Installer{Root: in.Root, Version: "2.0.0"}
	for _, a := range AssetsFor(ScopeProject, "en") {
		st, err := in2.stateOf(a)
		require.NoError(t, err)
		assert.Contains(t, []State{StateCurrent, StateOutdated}, st.State, a.Target)
	}
}

func TestLegacyLockMigratesOnceThenGoesAway(t *testing.T) {
	root := t.TempDir()
	a := AssetsFor(ScopeProject, "en")[0]
	old := []byte("old installed text\n")
	target := filepath.Join(root, a.Target)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, old, 0o644))
	lockPath := filepath.Join(root, ".brickkit", "skills.lock")
	l := &Lock{Lang: "en"}
	l.Set(LockEntry{Path: a.Target, Version: "0.1.0", Sum: Sum(old)})
	require.NoError(t, l.Save(lockPath))

	in := Installer{Root: root, Version: "1.0.0", LegacyLockPath: lockPath}
	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, a.Target)
	assert.NoFileExists(t, lockPath)
}

func TestLangFromAgentsBlock(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"),
		[]byte("# P\n<!-- brickkit:managed:begin lang=zh -->\n<!-- brickkit:managed:end -->\n"), 0o644))
	in := Installer{Root: root, Version: "1.0.0"}
	lang, err := in.ResolvedLang()
	require.NoError(t, err)
	assert.Equal(t, "zh", string(lang))
}

func TestNoAgentsAsset(t *testing.T) {
	for _, l := range []string{"en", "zh"} {
		for _, a := range AssetsFor(ScopeProject, i18nLang(l)) {
			assert.NotEqual(t, "AGENTS.md", a.Target)
		}
	}
}
```

`i18nLang` is a test helper: `func i18nLang(s string) i18n.Lang { l, _ := i18n.ParseLang(s); return l }`. Rewrite
the existing tests in `install_test.go`, `lang_test.go` and `lock_test.go` that used `LockPath` and `AGENTS.md`:
- assertions on `AGENTS.md` move to a skill target (`.claude/skills/brickkit-assemble/SKILL.md`);
- assertions on lock entries become assertions on markers;
- `lock_test.go` keeps only what `LoadLegacyLock` still does.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/skills/`
Expected: FAIL — `undefined: Mark`.

- [ ] **Step 3: Implement `marker.go` and the new state machine**

```go
package skills

// 每份装进项目的技能文件最后一行带一条记录：哪个版本的 CLI 写的、写的时候正文的指纹。
// 记录跟着文件进 Git，同事新克隆下来也知道哪些是 CLI 的、有没有被改过——以前那份
// skills.lock 放在被忽略的 .brickkit/ 里，新克隆里根本没有，技能从此再也升不上去。

import (
	"bytes"
	"regexp"
)

var markerLine = regexp.MustCompile(`(?m)^<!-- brickkit:skill version=(\S+) sum=(sha256:[0-9a-f]+) -->\s*$`)

// normalize 去掉正文末尾的空行与换行：编辑器补上或去掉最后一个换行，不该让文件变成"已手改"。
func normalize(body []byte) []byte { return bytes.TrimRight(body, "\r\n") }

// Mark 返回带记录的内容：正文、一个换行、记录行。
func Mark(body []byte, version string) []byte {
	n := normalize(body)
	out := append(append([]byte(nil), n...), '\n', '\n')
	return append(out, []byte("<!-- brickkit:skill version="+version+" sum="+Sum(n)+" -->\n")...)
}

// ReadMarker 拆出记录：正文（记录行之外的全部内容，记录行之后的也算正文）、版本、记的指纹。
func ReadMarker(content []byte) (body []byte, version, recordedSum string, ok bool) {
	locs := markerLine.FindAllSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return content, "", "", false
	}
	m := locs[len(locs)-1]
	body = append(append([]byte(nil), content[:m[0]]...), content[m[1]:]...)
	return bytes.TrimRight(body, "\r\n"), string(content[m[2]:m[3]]), string(content[m[4]:m[5]]), true
}
```

In `TestMarkRoundTrip` the body returned is normalized (`"# skill\n\ntext"`); assert
`assert.Equal(t, normalize(body), got)` instead of `body`. Use that form.

`install.go`:
- Remove `LockPath`; add `LegacyLockPath string` (the old `.brickkit/skills.lock`, read once).
- `stateOf(a Asset) (FileStatus, error)` (no lock parameter):

```go
	disk, err := os.ReadFile(filepath.Join(in.Root, a.Target))
	if os.IsNotExist(err) { st.State = StateMissing; return st, nil }
	if err != nil { … unchanged … }
	body, version, recorded, ok := ReadMarker(disk)
	switch {
	case ok && bytes.Equal(normalize(body), normalize(want)):
		st.State = StateCurrent
	case ok && Sum(normalize(body)) != recorded:
		st.State = StateModified
	case ok:
		st.State, st.FromVersion = StateOutdated, version
	case in.legacy != nil:
		if e, found := in.legacy.Get(a.Target); found && e.Sum == Sum(disk) {
			st.State, st.FromVersion = StateOutdated, e.Version
		} else if found {
			st.State = StateModified
		} else {
			st.State = StateUntracked
		}
	case bytes.Equal(normalize(disk), normalize(want)):
		st.State = StateOutdated // 内容就是当前那份、只是没有记录：补上记录
	default:
		st.State = StateUntracked
	}
```

  `in.legacy` is the `*Lock` loaded from `LegacyLockPath` at the start of `Status`/`Apply` (nil when absent). Since
  `Installer` is a value type, load it into a local and pass it through. The cleanest way is to keep a private
  `stateOfWith(a, legacy *Lock)`, with `stateOf(a)` calling it with nil. The test calls `stateOf`.
- `Apply` writes `Mark(content, in.Version)` instead of `content`. It stops writing a lock; after the loop, if
  `LegacyLockPath` exists, it removes it.
- `resolveLang`: explicit `in.Lang` → `agentsmd.BlockLang(in.Root)` → the legacy lock's `Lang` → `i18n.Current()`, then
  `AssetLang`.
- `LegacyAgentsSum() string`: the legacy lock's entry for `AGENTS.md`, or "".

`lock.go`: keep `Lock`, `LockEntry`, `LoadLock` (rename to `LoadLegacyLock`, same body), `Set` and `Save` (`Save` is
used only by tests now — keep it, mark the comment "only for tests and the migration's fixtures"). Delete
`assets/{en,zh}/AGENTS.md`.

`internal/project/layout.go`: rename `SkillsLockPath` → `LegacySkillsLockPath` (comment: only read to migrate projects
from before skills carried their own marker).

`internal/cli/skills.go`:
- `skillsInstaller` sets `LegacyLockPath: layout.LegacySkillsLockPath()`.
- After `in.Apply()` in `runSkillsUpdate`, run the AGENTS handling:

```go
	content := agentsmd.Content{Lang: string(res.Lang), ForceLang: lang != "", Component: in.Scope == skills.ScopeComponent ||
		fileExists(filepath.Join(in.Root, manifest.FileName))}
	if decl, err := projfile.ParseFile(project.NewLayout(in.Root).DeclPath()); err == nil {
		content = project.AgentsContent(project.NewLayout(in.Root), decl, string(res.Lang))
		content.ForceLang = lang != ""
	}
	ares, err := agentsmd.Ensure(in.Root, content, agentsmd.ModeRepair, legacyAgentsSum)
	if err != nil {
		return wrapSkillsError(err)
	}
	renderAgentsResult(opts, ares)
	if project.ObsoleteProjectMap(project.NewLayout(in.Root)) {
		renderProjectMapObsolete(opts)
	}
```

  `legacyAgentsSum := in.LegacyAgentsSum()` must be read **before** `in.Apply()` (which deletes the lock). Move the
  obsolete-map warning out of `init.go` into a shared `renderProjectMapObsolete(opts)` in `internal/cli/agents.go`.
- `runSkillsStatus` adds one row for `AGENTS.md`: "block present (lang=zh)" / "no brickkit block — skills update appends
  it" / "missing — skills update creates it", via new keys `cli.skills.agents_block_ok`, `cli.skills.agents_block_none`,
  `cli.skills.agents_missing`.
- `renderSkillsScope` no longer says "only one file" (component repositories now also get `AGENTS.md`): reword the key
  `CliSkillsComponentRepositoryHasNoOnly` to say the component repository gets `brickkit-component` and its own
  `AGENTS.md`/`CLAUDE.md`.

`internal/cli/init.go`: `installSkills` sets `LegacyLockPath`; `finishInit` passes `in.LegacyAgentsSum()` into the plan
**before** `plan.Apply`. That means computing it in `runInitCreate` / `runInitComplete` right after `PlanComplete`:
`plan.LegacyAgentsSum = (skills.Installer{Root: layout.Root, LegacyLockPath: layout.LegacySkillsLockPath()}).LegacyAgentsSum()`.

- [ ] **Step 4: Run the tests**

Run: `make generate-msgid && systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 go test ./internal/skills/ ./internal/cli/ ./internal/project/`
Expected: PASS. CLI tests that asserted `skills.lock` exists after `init` now assert the marker line in
`.claude/skills/brickkit-assemble/SKILL.md`.

- [ ] **Step 5: Lint and commit**

```bash
git add -A internal
git commit -m "feat(skills): each skill file carries its own marker — fresh clones update; AGENTS.md is no longer a skill asset

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `brickkit new` writes the full component doc set

**Files:**
- Create: `internal/manifest/docs_scaffold.go`, `internal/manifest/docs_scaffold_test.go`
- Modify: `internal/manifest/scaffold.go` (delete `componentDoc`; `Scaffold` adds the four docs;
  `metadata.repository` commented in the manifest), `internal/cli/new.go` (unchanged output loop already lists every
  file; next-steps text), `internal/manifest/scaffold_test.go`, `internal/cli/new_test.go` (expected file lists),
  i18n catalogs (`manifest.doc.*` heading keys removed; new hint keys)

**Interfaces:**
- Consumes: `docspec.Heading`, `docspec.Required`, `agentsmd.Skeleton`, `agentsmd.ClaudeContent`, `agentsmd.Content`
- Produces: `Scaffold` returns, in order: `component.yaml`, `BRICKKIT.md`, `AGENTS.md`, `CLAUDE.md`, `README.md`,
  then the contract file if any.

- [ ] **Step 1: Write the failing test**

```go
package manifest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/i18n"
)

func TestScaffoldWritesTheDocSet(t *testing.T) {
	i18n.SetCurrent(i18n.EN)
	files, err := Scaffold("demo/quote", ScaffoldOptions{Contract: ContractOpenAPI})
	require.NoError(t, err)
	var paths []string
	byPath := map[string]string{}
	for _, f := range files {
		paths = append(paths, f.Path)
		byPath[f.Path] = string(f.Content)
	}
	assert.Equal(t, []string{"component.yaml", "BRICKKIT.md", "AGENTS.md", "CLAUDE.md", "README.md", "api/openapi.yaml"}, paths)

	doc := byPath["BRICKKIT.md"]
	for _, h := range []string{"## Purpose", "## Before you deploy", "## Dependencies", "## Configuration", "## Contracts", "## Shell declaration"} {
		assert.Contains(t, doc, h)
	}
	assert.Contains(t, doc, "`api/openapi.yaml`")
	assert.NotContains(t, doc, "](", "BRICKKIT.md is read alone in the cache: no relative links")
	assert.Contains(t, doc, "<!-- TODO:")

	_, err = agentsmd.Find(byPath["AGENTS.md"])
	assert.NoError(t, err)
	assert.Equal(t, "@AGENTS.md\n", byPath["CLAUDE.md"])
	readme := byPath["README.md"]
	for _, h := range []string{"## Use it in a project", "## Documentation", "## Development"} {
		assert.Contains(t, readme, h)
	}
	assert.Contains(t, readme, "brickkit add demo/quote@0.1.0")
	assert.Contains(t, byPath["component.yaml"], "# repository:")
}

func TestScaffoldShellDeclaresMembers(t *testing.T) {
	files, err := Scaffold("erp/shell", ScaffoldOptions{Shell: true})
	require.NoError(t, err)
	for _, f := range files {
		if f.Path == "BRICKKIT.md" {
			assert.True(t, strings.Contains(string(f.Content), scaffoldPlaceholderMember))
		}
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/manifest/ -run TestScaffold`
Expected: FAIL — the file list stops at `BRICKKIT.md`, and the `## Before you deploy` assertion fails.

- [ ] **Step 3: Implement `docs_scaffold.go`**

```go
package manifest

// 本文件是 brickkit new 写的四份文档：BRICKKIT.md（随版本给使用方）、AGENTS.md + CLAUDE.md（给开发它的 AI）、
// README.md（给 GitHub 上的人）。标题取自 docspec，语言是此刻的 CLI 语言；要作者填的地方写成
// <!-- TODO: … --> 注释，brickkit lint 会一条条点出来，直到填完。

import (
	"strings"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

func todo(id msgid.ID, args ...any) string { return "<!-- TODO: " + i18n.T(id, args...) + " -->" }

func heading(s docspec.Section) string { return "## " + docspec.Heading(s, string(i18n.Current())) + "\n\n" }

// brickkitDoc 是 BRICKKIT.md 的骨架：六节。不放相对链接——它在使用方项目里是脱离仓库单独读的。
func brickkitDoc(id, contractPath string, shell bool) string {
	var b strings.Builder
	b.WriteString("# " + id + "\n\n")
	b.WriteString(heading(docspec.Purpose) + todo(msgid.ManifestDocHintPurpose) + "\n\n")
	b.WriteString(heading(docspec.BeforeDeploy) + todo(msgid.ManifestDocHintBeforeDeploy) + "\n\n")
	b.WriteString(heading(docspec.Dependencies) + todo(msgid.ManifestDocHintDependencies) + "\n\n")
	b.WriteString(heading(docspec.Configuration))
	b.WriteString("| " + i18n.T(msgid.ManifestDocColVariable) + " | " + i18n.T(msgid.ManifestDocColRequired) + " | " + i18n.T(msgid.ManifestDocColMeaning) + " |\n|---|---|---|\n")
	b.WriteString("| " + todo(msgid.ManifestDocConfigKeyTodo) + " | | " + todo(msgid.ManifestDocConfigMeaningTodo) + " |\n\n")
	b.WriteString(heading(docspec.Contracts))
	if contractPath != "" {
		b.WriteString("- `" + contractPath + "`: " + todo(msgid.ManifestDocHintContractFile) + "\n\n")
	} else {
		b.WriteString(todo(msgid.ManifestDocHintContracts) + "\n\n")
	}
	b.WriteString(heading(docspec.ShellDecl))
	if shell {
		b.WriteString(i18n.T(msgid.ManifestDocShellMembers) + "\n\n- `" + scaffoldPlaceholderMember + "` " + todo(msgid.ManifestDocShellMembersTodo) + "\n")
	} else {
		b.WriteString(i18n.T(msgid.ManifestDocNotShell) + "\n")
	}
	return b.String()
}

// readmeDoc 是 README.md 的骨架：三节，主要用来指路。
func readmeDoc(id, contractPath string) string {
	var b strings.Builder
	b.WriteString("# " + id + "\n\n" + todo(msgid.ManifestReadmeHintOneLine) + "\n\n")
	b.WriteString(heading(docspec.UseIt))
	b.WriteString("\x60\x60\x60bash\nbrickkit add " + id + "@0.1.0\nbrickkit up\n\x60\x60\x60\n\n")
	b.WriteString(i18n.T(msgid.ManifestReadmePrepareFirst, docspec.FileBrickkit) + "\n\n")
	b.WriteString(heading(docspec.Documentation))
	b.WriteString("| " + i18n.T(msgid.ManifestReadmeColQuestion) + " | " + i18n.T(msgid.ManifestReadmeColRead) + " |\n|---|---|\n")
	b.WriteString("| " + i18n.T(msgid.ManifestReadmeRowUsage) + " | [" + docspec.FileBrickkit + "](" + docspec.FileBrickkit + ") |\n")
	if contractPath != "" {
		b.WriteString("| " + i18n.T(msgid.ManifestReadmeRowContracts) + " | [" + contractPath + "](" + contractPath + ") |\n")
	}
	b.WriteString("| " + i18n.T(msgid.ManifestReadmeRowDependencies) + " | [" + FileName + "](" + FileName + ") |\n")
	b.WriteString("| " + i18n.T(msgid.ManifestReadmeRowDevelop) + " | [" + docspec.FileAgents + "](" + docspec.FileAgents + ") |\n\n")
	b.WriteString(heading(docspec.Development) + i18n.T(msgid.ManifestReadmeDevelopment, docspec.FileAgents) + "\n")
	return b.String()
}

// docFiles 是 new 写的四份文档，按顺序。
func docFiles(id, contractPath string, shell bool) []ScaffoldFile {
	lang := string(i18n.Current())
	return []ScaffoldFile{
		{Path: docspec.FileBrickkit, Content: []byte(brickkitDoc(id, contractPath, shell))},
		{Path: docspec.FileAgents, Content: []byte(agentsmd.Skeleton(id, agentsmd.Content{Lang: lang, Component: true}))},
		{Path: docspec.FileClaude, Content: []byte(agentsmd.ClaudeContent)},
		{Path: docspec.FileReadme, Content: []byte(readmeDoc(id, contractPath))},
	}
}
```

`agentsmd.Skeleton` takes the title as its first argument, and `Content.Title` exists from Task 4, so pass the title
either way.

In `scaffold.go`:
- Replace `componentDoc(...)` and the line that inserts it with
  `files = append(files[:1], append(docFiles(id, contractPath, opts.Shell), files[1:]...)...)`.
- Delete `componentDoc`, and keep `FileDoc` as an alias: `const FileDoc = docspec.FileBrickkit`.
- In the manifest template, after `description: %s`, add a line `  # repository: https://… # %s` with the new key
  `manifest.scaffold.repository_comment`
  (en: `the repository or page of this component: AGENTS.md tables in projects that use it link here`; zh:
  `这个组件的仓库或主页：使用它的项目在 AGENTS.md 组件表里链到这里`).

i18n keys:

| Key | en | zh |
| --- | --- | --- |
| `manifest.doc.hint_purpose` | `one or two sentences on the problem it solves, then two short lists: Owns, and Does not own (each saying who owns it instead)` | `一两句话说解决什么问题，再两张短表：负责、不负责（每条写明归谁）` |
| `manifest.doc.hint_before_deploy` | `what must exist before brickkit up — a database with its schema and role, an account, a certificate — who prepares each and how to check it; or "Nothing beyond the configuration below."` | `brickkit up 之前要准备好的东西——数据库及其 schema 和角色、账号、证书——每样由谁准备、怎么确认好了；没有就写"除下面的配置外无需准备。"` |
| `manifest.doc.hint_dependencies` | `each dependency by ID (no version), what it is used for; for an optional one, what happens when it is absent` | `每条依赖写 ID（不写版本）、拿来做什么；可选依赖写缺席时会怎样` |
| `manifest.doc.hint_contract_file` | `the main interfaces it describes` | `它描述的主要接口` |
| `manifest.doc.hint_contracts` | `every file under artifacts with the main interfaces it describes; events published; events consumed` | `artifacts 下的每个文件与它描述的主要接口；发布的事件；消费的事件` |
| `manifest.readme.hint_one_line` | `the same sentence as metadata.description` | `与 metadata.description 同一句话` |
| `manifest.readme.prepare_first` | `Prepare first: see "Before you deploy" in %[1]s.` | `先做准备：见 %[1]s 的"部署前准备"。` |
| `manifest.readme.col_question` | `To find out` | `想知道` |
| `manifest.readme.col_read` | `Read` | `读` |
| `manifest.readme.row_usage` | `What it does and does not do, how to configure it, what to prepare` | `负责什么、不负责什么，怎么配置，要准备什么` |
| `manifest.readme.row_contracts` | `Its interfaces and events` | `接口与事件` |
| `manifest.readme.row_dependencies` | `What it depends on (explained in BRICKKIT.md)` | `依赖什么（BRICKKIT.md 里有说明）` |
| `manifest.readme.row_develop` | `How to develop it` | `怎么开发` |
| `manifest.readme.development` | `Clone it, run brickkit init for a workbench and brickkit up to run it with its dependencies; then read %[1]s.` | `克隆下来，brickkit init 建工作台，brickkit up 连同依赖一起跑起来；然后读 %[1]s。` |

Remove `manifest.doc.purpose`, `.dependencies`, `.configuration`, `.contracts`, `.shell`, `.purpose_todo`,
`.dependencies_todo`, `.contracts_todo` (headings now come from `docspec`); keep `col_*`, `config_*_todo`,
`shell_members`, `shell_members_todo`, `not_shell`.

`internal/cli/new.go`: the next-steps line `CliNewFinishTheTodosInThe` now reads (both catalogs) "Fill in the TODO
comments in the docs and component.yaml (brickkit lint lists every one left)" /
"把文档和 component.yaml 里的 TODO 注释填完（brickkit lint 会列出剩下的每一处）".

- [ ] **Step 4: Run the tests**

Run: `make generate-msgid && go test ./internal/manifest/ ./internal/cli/ -run 'Scaffold|New'`
Expected: PASS (update `new_test.go`'s expected output list of files).

- [ ] **Step 5: Lint and commit**

```bash
git add -A internal
git commit -m "feat(new): the skeleton carries BRICKKIT.md (six sections), AGENTS.md, CLAUDE.md and README.md

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: `internal/doccheck` — the documentation checks and their error codes

**Files:**
- Create: `internal/doccheck/common.go`, `component.go`, `project.go`, `doccheck_test.go`
- Modify: `internal/clierr/clierr.go` (eleven codes), `docs/{en,zh}/06-architecture/09-error-codes.md`, i18n catalogs,
  `internal/cli/agents.go` and `init.go` (switch to the new codes for the block / CLAUDE / obsolete-map warnings), both
  code maps

**Interfaces:**
- Consumes: `mdtext.*`, `docspec.*`, `agentsmd.Find`, `manifest.Manifest`
- Produces:
  - `func Component(dir string, m *manifest.Manifest) []*clierr.Error`
  - `func Project(root string) []*clierr.Error`
  - codes: `CodeDocFileMissing`, `CodeDocSectionMissing`, `CodeDocPathMissing`, `CodeDocLinkBroken`,
    `CodeDocLinkNotPortable`, `CodeDocOutOfStep`, `CodeDocPlaceholder`, `CodeDocTranslationDrift`,
    `CodeAgentsBlockMissing`, `CodeClaudeImportMissing`, `CodeProjectMapObsolete`

- [ ] **Step 1: Write the failing tests**

```go
package doccheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

var fence = strings.Repeat("`", 3)

const goodBrickkit = `# demo/quote

## Purpose
Quotes. Owns: quotes. Does not own: greetings (demo/hello).

## Before you deploy
Nothing beyond the configuration below.

## Dependencies
- demo/hello: the greeting.

## Configuration
| Variable | Required | Meaning |
|---|---|---|
| QUOTE_SECRET | Yes | signs quotes |

## Contracts
- ` + "`api/openapi.yaml`" + `: GET /api/v1/quote

## Shell declaration
Not a shell.
`

const goodAgents = `# demo/quote

Guide.

## Code map
| Path | Owns |
|---|---|
| ` + "`main.go`" + ` | entry |
| ` + "`api/`" + ` | contract |

## Build and test
go test ./...

## Design decisions
None.

## Pitfalls
| Never | Symptom | Why |
|---|---|---|

## Before changing code
1. Check the contract.

<!-- brickkit:managed:begin lang=en -->
<!-- brickkit:managed:end -->
`

const goodReadme = `# demo/quote

Quotes.

## Use it in a project
brickkit add demo/quote@0.1.0

## Documentation
[BRICKKIT.md](BRICKKIT.md)

## Development
See [AGENTS.md](AGENTS.md).
`

func component(t *testing.T, files map[string]string) (string, *manifest.Manifest) {
	t.Helper()
	dir := t.TempDir()
	base := map[string]string{
		"BRICKKIT.md": goodBrickkit, "AGENTS.md": goodAgents, "CLAUDE.md": "@AGENTS.md\n", "README.md": goodReadme,
		"main.go": "package main\n", "api/openapi.yaml": "openapi: 3.0.3\n",
	}
	for k, v := range files {
		if v == "" {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	for rel, body := range base {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	m := &manifest.Manifest{
		Metadata:     manifest.Metadata{ID: "demo/quote", Version: "0.1.0"},
		Artifacts:    []manifest.Artifact{{Type: "api-contract", Files: []string{"api/openapi.yaml"}}},
		Dependencies: &manifest.Dependencies{Components: []manifest.ComponentDep{{ID: "demo/hello", Version: "1.0.0"}}},
		ConfigSchema: &manifest.ConfigSchema{Required: []string{"QUOTE_SECRET"}},
	}
	return dir, m
}

func codes(ws []*clierr.Error) []clierr.Code {
	var out []clierr.Code
	for _, w := range ws {
		out = append(out, w.Code)
	}
	return out
}

func TestCleanComponentHasNoWarnings(t *testing.T) {
	dir, m := component(t, nil)
	assert.Empty(t, Component(dir, m))
}

func TestMissingFiles(t *testing.T) {
	dir, m := component(t, map[string]string{"CLAUDE.md": "", "README.md": ""})
	assert.Equal(t, []clierr.Code{clierr.CodeDocFileMissing, clierr.CodeDocFileMissing}, codes(Component(dir, m)))
}

func TestMissingSectionAndChineseHeadingsCount(t *testing.T) {
	zh := strings.NewReplacer("## Purpose", "## 组件定位", "## Before you deploy\n", "").Replace(goodBrickkit)
	dir, m := component(t, map[string]string{"BRICKKIT.md": zh})
	ws := Component(dir, m)
	require.Len(t, ws, 1)
	assert.Equal(t, clierr.CodeDocSectionMissing, ws[0].Code)
}

func TestCodeMapPathsChecked(t *testing.T) {
	agents := strings.Replace(goodAgents, "| `api/` | contract |", "| `api/` | contract |\n| `gone/x.go` | missing |", 1)
	agents += "\nOutside the map `nope/also.go` is not checked.\n"
	dir, m := component(t, map[string]string{"AGENTS.md": agents})
	ws := Component(dir, m)
	require.Equal(t, []clierr.Code{clierr.CodeDocPathMissing}, codes(ws))
}

func TestLinks(t *testing.T) {
	readme := goodReadme + "\n[gone](docs/gone.md)\n" + fence + "\n[in code](x.md)\n" + fence + "\n[up](../outside.md)\n"
	dir, m := component(t, map[string]string{"README.md": readme})
	assert.ElementsMatch(t, []clierr.Code{clierr.CodeDocLinkBroken, clierr.CodeDocLinkNotPortable}, codes(Component(dir, m)))
}

func TestBrickkitHasNoRelativeLinks(t *testing.T) {
	dir, m := component(t, map[string]string{"BRICKKIT.md": goodBrickkit + "\nSee [design](docs/design.md) and [site](https://x.example).\n"})
	assert.Equal(t, []clierr.Code{clierr.CodeDocLinkNotPortable}, codes(Component(dir, m)))
}

func TestOutOfStepWithManifest(t *testing.T) {
	dir, m := component(t, nil)
	m.Dependencies.Components = append(m.Dependencies.Components, manifest.ComponentDep{ID: "infra/cache", Version: "1.0.0", Optional: true})
	m.ConfigSchema.Required = append(m.ConfigSchema.Required, "DB_PASSWORD")
	m.Artifacts = append(m.Artifacts, manifest.Artifact{Type: "event-contract", Files: []string{"events/quote.json"}})
	m.Shell = &manifest.Shell{Members: []string{"demo/member@1.0.0"}}
	ws := Component(dir, m)
	assert.Equal(t, []clierr.Code{clierr.CodeDocOutOfStep, clierr.CodeDocOutOfStep, clierr.CodeDocOutOfStep, clierr.CodeDocOutOfStep}, codes(ws))
}

func TestDependencyWithVersionCountsAsMentioned(t *testing.T) {
	doc := strings.Replace(goodBrickkit, "- demo/hello: the greeting.", "- `demo/hello@1.0.0`: the greeting.", 1)
	dir, m := component(t, map[string]string{"BRICKKIT.md": doc})
	assert.Empty(t, Component(dir, m))
}

func TestPlaceholders(t *testing.T) {
	doc := goodBrickkit + "\n<!-- TODO: fill -->\n" + fence + "\nTODO in code is fine\n" + fence + "\n`TODO` inline is fine\n待补\n"
	dir, m := component(t, map[string]string{"BRICKKIT.md": doc})
	assert.Equal(t, []clierr.Code{clierr.CodeDocPlaceholder, clierr.CodeDocPlaceholder}, codes(Component(dir, m)))
}

func TestTranslations(t *testing.T) {
	zh := strings.Replace(goodReadme, "# demo/quote\n", "# demo/quote\n\n[English](README.md) · [中文](README.zh.md)\n", 1)
	en := strings.Replace(goodReadme, "# demo/quote\n", "# demo/quote\n\n[English](README.md) · [中文](README.zh.md)\n", 1)
	dir, m := component(t, map[string]string{"README.md": en, "README.zh.md": zh})
	assert.Empty(t, Component(dir, m), "a pair in step that links both ways is clean")

	dir, m = component(t, map[string]string{"README.md": en, "README.zh.md": zh + "\n## Extra\n"})
	assert.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(Component(dir, m)))

	dir, m = component(t, map[string]string{"README.zh.md": goodReadme})
	assert.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(Component(dir, m)), "the files don't link each other")

	dir, m = component(t, map[string]string{"docs/design.zh.md": "# d\n"})
	assert.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(Component(dir, m)), "a translation without a primary")
}

func TestTranslationSuffixNotALanguage(t *testing.T) {
	dir, m := component(t, map[string]string{"README.zh-CN.md": goodReadme})
	ws := Component(dir, m)
	require.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(ws))
	assert.Contains(t, ws[0].Format(), "README.zh-CN.md")
}

func TestProjectChecks(t *testing.T) {
	root := t.TempDir()
	assert.Equal(t, []clierr.Code{clierr.CodeDocFileMissing, clierr.CodeDocFileMissing}, codes(Project(root)))

	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# P\n\n## Overview\nx\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("hi\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "BRICKKIT.md"), []byte("<!-- brickkit:managed:begin -->\n<!-- brickkit:managed:end -->\n"), 0o644))
	assert.ElementsMatch(t, []clierr.Code{
		clierr.CodeDocSectionMissing, clierr.CodeDocSectionMissing, clierr.CodeDocSectionMissing,
		clierr.CodeAgentsBlockMissing, clierr.CodeClaudeImportMissing, clierr.CodeProjectMapObsolete,
	}, codes(Project(root)))
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/doccheck/`
Expected: FAIL — `undefined: Component`, `clierr.CodeDocFileMissing`.

- [ ] **Step 3: Add the codes**

In `internal/clierr/clierr.go`, after the lint block:

```go
	// 文档检查（brickkit lint，只作警告；--strict 下算失败）。规范见组件文档那一页。
	//
	// CodeDocFileMissing：必需的文档文件不在（BRICKKIT.md、AGENTS.md、CLAUDE.md、README.md）。
	CodeDocFileMissing Code = "DOC_FILE_MISSING"
	// CodeDocSectionMissing：文档缺了一个固定小节。
	CodeDocSectionMissing Code = "DOC_SECTION_MISSING"
	// CodeDocPathMissing：AGENTS.md 代码地图里写的路径不存在。
	CodeDocPathMissing Code = "DOC_PATH_MISSING"
	// CodeDocLinkBroken：相对链接指向的文件不存在。
	CodeDocLinkBroken Code = "DOC_LINK_BROKEN"
	// CodeDocLinkNotPortable：BRICKKIT.md 里有相对链接（缓存里没有仓库，链接是死的），
	// 或组件文档的相对链接跑出了组件目录（组件被别的项目用时那里什么都没有）。
	CodeDocLinkNotPortable Code = "DOC_LINK_NOT_PORTABLE"
	// CodeDocOutOfStep：component.yaml 里的事实在文档该提的地方没提（依赖、必填配置项、契约文件、外壳成员）。
	CodeDocOutOfStep Code = "DOC_OUT_OF_STEP"
	// CodeDocPlaceholder：正文里留着占位词（TODO、待补……）。
	CodeDocPlaceholder Code = "DOC_PLACEHOLDER"
	// CodeDocTranslationDrift：译本与原文对不上（没有原文、小节数不同、没互相链接、后缀不是语言代码）。
	CodeDocTranslationDrift Code = "DOC_TRANSLATION_DRIFT"
	// CodeAgentsBlockMissing：AGENTS.md 里没有由 brickkit 维护的一段（或标记坏了）。
	CodeAgentsBlockMissing Code = "AGENTS_BLOCK_MISSING"
	// CodeClaudeImportMissing：CLAUDE.md 里没有 @AGENTS.md。
	CodeClaudeImportMissing Code = "CLAUDE_IMPORT_MISSING"
	// CodeProjectMapObsolete：项目根还留着旧版的项目地图 BRICKKIT.md。
	CodeProjectMapObsolete Code = "PROJECT_MAP_OBSOLETE"
```

Add the eleven entries to both error-code pages, in a new "Documentation checks" section, each with what fires it and
what to do (en and zh, written independently).

- [ ] **Step 4: Implement the checks**

`internal/doccheck/common.go`:

```go
// Package doccheck 是 brickkit lint 的文档检查：只查程序能确定的事——文件在不在、小节齐不齐、
// 路径与链接在不在、component.yaml 的事实有没有提、占位词、译本对不对得上。写得好不好是评审的事。
// 全部是警告。
package doccheck

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/mdtext"
	"github.com/brickkit/brickkit/internal/msgid"
)

// doc 是一份读进来的文档。rel 是相对检查根目录的路径（斜杠分隔），给人看。
type doc struct {
	rel, body string
}

func warn(code clierr.Code, msg, rel string, line int) *clierr.Error {
	w := clierr.Warn(code, msg).WithDetail(i18n.T(msgid.LabelFile), rel)
	if line > 0 {
		w = w.WithDetail(i18n.T(msgid.DoccheckLabelLine), strconv.Itoa(line))
	}
	return w
}

func read(root, rel string) (doc, bool) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return doc{}, false
	}
	return doc{rel: rel, body: string(data)}, true
}

// sections 检查 kind 的每个固定小节都在。
func sections(d doc, kind docspec.Kind) []*clierr.Error {
	var out []*clierr.Error
	have := mdtext.Sections(d.body)
	for _, s := range docspec.Required(kind) {
		found := false
		for _, h := range have {
			if docspec.Matches(s, h.Heading) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, warn(clierr.CodeDocSectionMissing,
				i18n.T(msgid.DoccheckSectionMissing, docspec.Heading(s, "en"), docspec.Heading(s, "zh")), d.rel, 0))
		}
	}
	return out
}

// section 是 d 里匹配 s 的那一节的正文；没有时为空。
func section(d doc, s docspec.Section) (string, int) {
	for _, h := range mdtext.Sections(d.body) {
		if docspec.Matches(s, h.Heading) {
			return h.Body, h.Line
		}
	}
	return "", 0
}

// placeholders 查代码之外的占位词。
func placeholders(d doc) []*clierr.Error {
	var out []*clierr.Error
	for _, l := range mdtext.ProseLines(d.body) {
		for _, w := range docspec.PlaceholderWords {
			if containsWord(l.Text, w) {
				out = append(out, warn(clierr.CodeDocPlaceholder, i18n.T(msgid.DoccheckPlaceholder, w), d.rel, l.N))
				break
			}
		}
	}
	return out
}

// containsWord：英文占位词要成词（TODOS、todo 不算），中文的照字面。
func containsWord(text, w string) bool {
	if w[0] >= 0x80 {
		return strings.Contains(text, w)
	}
	for i := 0; ; {
		j := strings.Index(text[i:], w)
		if j < 0 {
			return false
		}
		at := i + j
		before := at == 0 || !isWordByte(text[at-1])
		after := at+len(w) == len(text) || !isWordByte(text[at+len(w)])
		if before && after {
			return true
		}
		i = at + len(w)
	}
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// links 检查相对链接：目标要存在；在组件里（portableRoot 非空）不能跑出组件目录。
func links(root string, d doc, portable bool) []*clierr.Error {
	var out []*clierr.Error
	for _, l := range mdtext.Links(d.body) {
		t := l.Target
		if t == "" || strings.HasPrefix(t, "#") || strings.Contains(t, "://") || strings.HasPrefix(t, "mailto:") {
			continue
		}
		file, _, _ := strings.Cut(t, "#")
		if file == "" {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(filepath.Dir(d.rel), file))
		if portable && (strings.HasPrefix(rel, "../") || rel == ".." || strings.HasPrefix(file, "/")) {
			out = append(out, warn(clierr.CodeDocLinkNotPortable, i18n.T(msgid.DoccheckLinkLeaves, t), d.rel, l.Line))
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			out = append(out, warn(clierr.CodeDocLinkBroken, i18n.T(msgid.DoccheckLinkBroken, t), d.rel, l.Line))
		}
	}
	return out
}

// mdFiles 是 root 下要查的 Markdown：根目录的 *.md，加上 docs/ 下的全部 *.md（斜杠分隔、排好序）。
func mdFiles(root string) []string {
	var out []string
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	_ = filepath.WalkDir(filepath.Join(root, docspec.DirDocs), func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".md") {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}
```

`internal/doccheck/component.go`:

```go
package doccheck

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/mdtext"
	"github.com/brickkit/brickkit/internal/msgid"
)

var requiredFiles = []string{docspec.FileBrickkit, docspec.FileAgents, docspec.FileClaude, docspec.FileReadme}

// Component 检查组件目录 dir 的文档（m 是它的 component.yaml）。
func Component(dir string, m *manifest.Manifest) []*clierr.Error {
	var out []*clierr.Error
	for _, f := range requiredFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			out = append(out, warn(clierr.CodeDocFileMissing, i18n.T(msgid.DoccheckFileMissing, f), f, 0))
		}
	}
	if d, ok := read(dir, docspec.FileClaude); ok && !hasImport(d.body) {
		out = append(out, warn(clierr.CodeClaudeImportMissing, i18n.T(msgid.DoccheckClaudeImport, docspec.ClaudeImport), d.rel, 0))
	}
	if d, ok := read(dir, docspec.FileAgents); ok {
		out = append(out, sections(d, docspec.KindComponentAgents)...)
		out = append(out, codeMap(dir, d)...)
		if _, err := agentsmd.Find(d.body); err != nil {
			out = append(out, warn(clierr.CodeAgentsBlockMissing, i18n.T(msgid.DoccheckAgentsBlock, err.Error()), d.rel, 0))
		}
	}
	for _, rel := range mdFiles(dir) {
		d, _ := read(dir, rel)
		base, lang, ok := docspec.SplitTranslation(path.Base(rel))
		if !ok {
			if isDocName(path.Base(rel)) || strings.HasPrefix(rel, docspec.DirDocs+"/") && strings.Count(path.Base(rel), ".") > 1 {
				out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNotALanguage, path.Base(rel)), rel, 0))
			}
			continue
		}
		out = append(out, placeholders(d)...)
		if base == docspec.FileBrickkit {
			out = append(out, brickkit(d, m, lang)...)
		} else {
			out = append(out, links(dir, d, true)...)
		}
		if base == docspec.FileReadme && lang == "" {
			out = append(out, sections(d, docspec.KindReadme)...)
		}
		if lang != "" {
			out = append(out, translation(dir, d, path.Join(path.Dir(rel), base), lang)...)
		}
	}
	return out
}

// isDocName：名字以四个必需文档之一开头（README.zh-CN.md 这种写错了后缀的）。
func isDocName(name string) bool {
	for _, f := range requiredFiles {
		if strings.HasPrefix(name, strings.TrimSuffix(f, ".md")+".") {
			return true
		}
	}
	return false
}

func hasImport(body string) bool {
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(l) == docspec.ClaudeImport {
			return true
		}
	}
	return false
}

// codeMap 核对代码地图里每条路径都在：只看那一节表格里的行内代码，含 /、不含空白、* 与 ://。
func codeMap(dir string, d doc) []*clierr.Error {
	body, start := section(d, docspec.CodeMap)
	if start == 0 {
		return nil
	}
	var out []*clierr.Error
	for i, line := range strings.Split(body, "\n") {
		for _, cellText := range mdtext.TableCells(line) {
			for _, p := range mdtext.InlineCode(cellText) {
				if !strings.Contains(p, "/") || strings.ContainsAny(p, " \t*") || strings.Contains(p, "://") {
					continue
				}
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(strings.TrimSuffix(p, "/")))); err != nil {
					out = append(out, warn(clierr.CodeDocPathMissing, i18n.T(msgid.DoccheckPathMissing, p), d.rel, start+1+i))
				}
			}
		}
	}
	return out
}

// brickkit 查 BRICKKIT.md（及译本）：小节、不许相对链接、component.yaml 的事实都提到了。
func brickkit(d doc, m *manifest.Manifest, lang string) []*clierr.Error {
	out := sections(d, docspec.KindBrickkit)
	for _, l := range mdtext.Links(d.body) {
		if !strings.Contains(l.Target, "://") && !strings.HasPrefix(l.Target, "#") && !strings.HasPrefix(l.Target, "mailto:") {
			out = append(out, warn(clierr.CodeDocLinkNotPortable, i18n.T(msgid.DoccheckBrickkitRelativeLink, l.Target), d.rel, l.Line))
		}
	}
	if m == nil {
		return out
	}
	mention := func(s docspec.Section, fact string, msg msgid.ID) {
		body, line := section(d, s)
		if line == 0 || strings.Contains(body, fact) {
			return // 缺整节已经报过了
		}
		out = append(out, warn(clierr.CodeDocOutOfStep, i18n.T(msg, fact, docspec.Heading(s, langOr(lang))), d.rel, line))
	}
	if m.Dependencies != nil {
		for _, dep := range m.Dependencies.Components {
			mention(docspec.Dependencies, dep.ID, msgid.DoccheckDependencyNotMentioned)
		}
	}
	if m.ConfigSchema != nil {
		for _, k := range m.ConfigSchema.Required {
			mention(docspec.Configuration, k, msgid.DoccheckRequiredKeyNotMentioned)
		}
	}
	for _, a := range m.Artifacts {
		for _, f := range a.Files {
			mention(docspec.Contracts, f, msgid.DoccheckArtifactNotMentioned)
		}
	}
	if m.Shell != nil {
		for _, ref := range m.Shell.Members {
			id, _, _ := manifest.SplitRef(ref)
			mention(docspec.ShellDecl, id, msgid.DoccheckMemberNotMentioned)
		}
	}
	return out
}

func langOr(lang string) string {
	if lang == "" {
		return "en"
	}
	return lang
}

// translation 查一份译本：有原文、二级小节数一样、两边互相链接。
func translation(dir string, d doc, primaryRel, lang string) []*clierr.Error {
	p, ok := read(dir, primaryRel)
	if !ok {
		return []*clierr.Error{warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNoPrimary, primaryRel), d.rel, 0)}
	}
	var out []*clierr.Error
	if a, b := len(mdtext.Sections(p.body)), len(mdtext.Sections(d.body)); a != b {
		out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckSectionCount, b, a, primaryRel), d.rel, 0))
	}
	if !linksTo(d, path.Base(primaryRel)) || !linksTo(p, path.Base(d.rel)) {
		out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNotLinked, path.Base(primaryRel), path.Base(d.rel)), d.rel, 0))
	}
	return out
}

func linksTo(d doc, name string) bool {
	for _, l := range mdtext.Links(d.body) {
		if path.Base(strings.SplitN(l.Target, "#", 2)[0]) == name {
			return true
		}
	}
	return false
}
```

In `TestTranslations`, the clean pair uses `README.md` → `README.zh.md` links and the reverse; `BRICKKIT.md` has no
links, so a `BRICKKIT.zh.md` cannot link its primary (relative links are forbidden there). Make `translation` skip the
mutual-link check when the base is `BRICKKIT.md`: add the parameter `checkLinks bool`, false for BRICKKIT.
`TestTranslations` needs `brickkit` to call `translation` too — move the `if lang != ""` translation call before the
base switch so both paths run it, passing `checkLinks: base != docspec.FileBrickkit`.

`internal/doccheck/project.go`:

```go
package doccheck

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Project 检查项目根的 AGENTS.md、CLAUDE.md、README.md 的链接，和有没有旧版的项目地图。
// 工作台（根目录也有 component.yaml）的这些文件同时是组件的文档，由 Component 查；这里只查项目那一份要求的。
func Project(root string) []*clierr.Error {
	var out []*clierr.Error
	workbench := exists(filepath.Join(root, manifest.FileName))
	for _, f := range []string{docspec.FileAgents, docspec.FileClaude} {
		if !exists(filepath.Join(root, f)) && !workbench {
			out = append(out, warn(clierr.CodeDocFileMissing, i18n.T(msgid.DoccheckFileMissing, f), f, 0))
		}
	}
	if workbench {
		return out
	}
	if d, ok := read(root, docspec.FileAgents); ok {
		out = append(out, sections(d, docspec.KindProjectAgents)...)
		out = append(out, links(root, d, false)...)
		out = append(out, placeholders(d)...)
		if _, err := agentsmd.Find(d.body); err != nil {
			out = append(out, warn(clierr.CodeAgentsBlockMissing, i18n.T(msgid.DoccheckAgentsBlock, err.Error()), d.rel, 0))
		}
	}
	if d, ok := read(root, docspec.FileClaude); ok && !hasImport(d.body) {
		out = append(out, warn(clierr.CodeClaudeImportMissing, i18n.T(msgid.DoccheckClaudeImport, docspec.ClaudeImport), d.rel, 0))
	}
	if d, ok := read(root, docspec.FileReadme); ok {
		out = append(out, links(root, d, false)...)
	}
	if d, ok := read(root, docspec.FileBrickkit); ok && strings.Contains(d.body, "<!-- brickkit:managed:begin") {
		out = append(out, warn(clierr.CodeProjectMapObsolete, i18n.T(msgid.CliInitProjectMapObsolete), d.rel, 0).
			WithHint(i18n.T(msgid.CliInitHintMoveProjectMap)))
	}
	return out
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
```

i18n keys (both catalogs; `%[n]` verbs as shown):

| Key | en | zh |
| --- | --- | --- |
| `doccheck.label_line` | `Line` | `行` |
| `doccheck.file_missing` | `%[1]s is missing` | `缺少 %[1]s` |
| `doccheck.section_missing` | `the "%[1]s" (%[2]s) section is missing` | `缺少"%[1]s"（%[2]s）一节` |
| `doccheck.path_missing` | `the code map names %[1]s, which does not exist` | `代码地图写的 %[1]s 不存在` |
| `doccheck.link_broken` | `the link %[1]s points at a file that does not exist` | `链接 %[1]s 指向的文件不存在` |
| `doccheck.link_leaves` | `the link %[1]s leaves the component directory: projects that use the component do not have it` | `链接 %[1]s 跑出了组件目录：使用这个组件的项目里没有它` |
| `doccheck.brickkit_relative_link` | `BRICKKIT.md is read alone in other projects' caches, so the relative link %[1]s is dead there: name the file as inline code, or use an absolute URL` | `BRICKKIT.md 在别的项目缓存里是单独读的，相对链接 %[1]s 在那里是死的：用行内代码写文件名，或用绝对地址` |
| `doccheck.dependency_not_mentioned` | `component.yaml depends on %[1]s, but "%[2]s" doesn't mention it` | `component.yaml 依赖 %[1]s，"%[2]s"一节没提到它` |
| `doccheck.required_key_not_mentioned` | `%[1]s is a required config key, but "%[2]s" doesn't explain it` | `%[1]s 是必填配置项，"%[2]s"一节没讲它` |
| `doccheck.artifact_not_mentioned` | `%[1]s is listed under artifacts, but "%[2]s" doesn't mention it` | `artifacts 里有 %[1]s，"%[2]s"一节没提到它` |
| `doccheck.member_not_mentioned` | `the shell compiles in %[1]s, but "%[2]s" doesn't list it` | `外壳编进了 %[1]s，"%[2]s"一节没列它` |
| `doccheck.placeholder` | `a placeholder (%[1]s) is still in the text` | `正文里还留着占位（%[1]s）` |
| `doccheck.not_a_language` | `%[1]s looks like a translation, but its suffix is not a language code (lowercase, like zh or pt-br)` | `%[1]s 看着像译本，但后缀不是语言代码（小写，如 zh、pt-br）` |
| `doccheck.no_primary` | `this translation has no primary file %[1]s` | `这份译本没有原文 %[1]s` |
| `doccheck.section_count` | `this translation has %[1]d sections, its primary %[3]s has %[2]d` | `这份译本有 %[1]d 节，原文 %[3]s 有 %[2]d 节` |
| `doccheck.not_linked` | `%[1]s and %[2]s should link each other near the top` | `%[1]s 与 %[2]s 应在开头互相链接` |
| `doccheck.claude_import` | `CLAUDE.md does not contain %[1]s, so Claude Code does not read AGENTS.md` | `CLAUDE.md 里没有 %[1]s，Claude Code 不会读 AGENTS.md` |
| `doccheck.agents_block` | `AGENTS.md has no usable brickkit block (%[1]s): brickkit skills update appends one` | `AGENTS.md 里没有可用的 brickkit 维护段（%[1]s）：brickkit skills update 会追加` |

Switch `renderAgentsResult` and the obsolete-map warning in `internal/cli` to `CodeAgentsBlockMissing`,
`CodeClaudeImportMissing`, `CodeProjectMapObsolete`.

- [ ] **Step 5: Run the tests**

Run: `make generate-msgid && go test ./internal/doccheck/ ./tests/docfields/ ./internal/cli/`
Expected: PASS. `tests/docfields` fails until both error-code pages list the eleven codes.

- [ ] **Step 6: Lint and commit**

Add `internal/doccheck/` to both code maps ("lint's documentation checks: files, sections, code-map paths, links,
manifest facts, placeholders, translations").

```bash
git add -A internal docs/en/06-architecture/09-error-codes.md docs/zh/06-architecture/09-error-codes.md AGENTS.md AGENTS.zh.md
git commit -m "feat(doccheck): documentation checks — sections, code-map paths, links, manifest facts, placeholders, translations

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: `brickkit lint` runs the documentation checks

**Files:**
- Modify: `internal/cli/lint.go`, `internal/cli/lint_test.go`, the CLI test helpers that create local components
  (`oneLocalSource` and friends in `internal/cli/*_test.go`), `internal/cli/testdata/**` (component directories gain
  complete docs; project roots gain `AGENTS.md` + `CLAUDE.md`)
- Create: `internal/cli/docs_fixture_test.go` (helper writing complete docs for a component directory)

**Interfaces:**
- Consumes: `doccheck.Component`, `doccheck.Project`, `manifest.ParseFile`
- Produces: lint output gains one entry per checked doc set: `📝 <dir>/ (docs)`, listing its warnings; the summary
  counts it as a file.

- [ ] **Step 1: Write the failing tests**

```go
func TestLintComponentRepositoryChecksDocs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "component.yaml"), minimalManifest("demo/quote", "0.1.0"))
	r := runIn(t, dir, "lint")
	require.Equal(t, clierr.ExitOK, r.code, "documentation problems are warnings")
	assert.Contains(t, r.stdout, "BRICKKIT.md is missing")
	assert.Contains(t, r.stdout, "AGENTS.md is missing")

	strict := runIn(t, dir, "lint", "--strict")
	assert.Equal(t, clierr.ExitError, strict.code)
}

func TestLintProjectChecksLocalComponentDocs(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	r := runIn(t, f.Dir, "lint")
	assert.Contains(t, r.stdout, "0 warnings", "fixtures carry complete docs")

	require.NoError(t, os.Remove(filepath.Join(f.Dir, "shared", "demo", "hello", "README.md")))
	r = runIn(t, f.Dir, "lint")
	assert.Contains(t, r.stdout, filepath.Join("shared", "demo", "hello")+string(filepath.Separator)+" (docs)")
	assert.Contains(t, r.stdout, "README.md is missing")
}
```

`minimalManifest` already exists in the CLI tests (if not, use the YAML that `TestLintComponent…` tests use today).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/cli/ -run 'TestLint(ComponentRepositoryChecksDocs|ProjectChecksLocalComponentDocs)'`
Expected: FAIL — no "BRICKKIT.md is missing" in the output.

- [ ] **Step 3: Implement**

In `internal/cli/lint.go`:

```go
// lintDocs 是一组文档的检查结果，在报告里占一行。
func lintDocs(opts *Options, dir string, warnings []*clierr.Error) lintFile {
	return lintFile{path: opts.display(dir) + string(filepath.Separator) + " " + i18n.T(msgid.CliLintDocsSuffix), warnings: warnings}
}

// componentDocs 查 dir 里那个组件的文档；component.yaml 读不了时跳过（它自己的错误已经报过）。
func componentDocs(opts *Options, dir string) (lintFile, bool) {
	m, err := manifest.ParseFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return lintFile{}, false
	}
	return lintDocs(opts, dir, doccheck.Component(dir, m)), true
}
```

- In `runLint`'s component branch, after `lintManifest`: `if f, ok := componentDocs(opts, layout.Root); ok { files = append(files, f) }`.
- In `lintProject`:
  - after the local-source loop, add `files = append(files, lintDocs(opts, layout.Root, doccheck.Project(layout.Root)))`;
  - for each `found` local component, add `componentDocs(opts, filepath.Dir(f.Path))`;
  - for a workbench (own `component.yaml` exists), add `componentDocs(opts, layout.Root)` once.
- Key `cli.lint.docs_suffix`: en `(docs)`, zh `（文档）`.
- The report already prints a lintFile's warnings under its path, and `--strict` already counts them.

`internal/cli/docs_fixture_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeCompleteDocs 给测试里的组件目录写一套能过文档检查的文档：测试关心的是别的东西，
// 不该被"缺 README"之类的警告搅乱"0 warnings"。
func writeCompleteDocs(t *testing.T, dir, id string) {
	t.Helper()
	files := map[string]string{
		"BRICKKIT.md": "# " + id + "\n\n## Purpose\nx\n\n## Before you deploy\nNothing.\n\n## Dependencies\nNone.\n\n" +
			"## Configuration\nNone.\n\n## Contracts\nNone.\n\n## Shell declaration\nNot a shell.\n",
		"AGENTS.md": "# " + id + "\n\n## Code map\nNone.\n\n## Build and test\nx\n\n## Design decisions\nx\n\n## Pitfalls\nx\n\n" +
			"## Before changing code\nx\n\n<!-- brickkit:managed:begin lang=en -->\n<!-- brickkit:managed:end -->\n",
		"CLAUDE.md": "@AGENTS.md\n",
		"README.md": "# " + id + "\n\n## Use it in a project\nx\n\n## Documentation\nx\n\n## Development\nx\n",
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
}
```

Call it from every helper that writes a component directory for a lint-checked project (`oneLocalSource` and any
sibling that writes `component.yaml` under a local source). A component whose manifest declares dependencies or
required keys gets those mentioned too: let `writeCompleteDocs` take the manifest's dependency IDs and required keys
(`deps, keys []string`) and write them into the Dependencies / Configuration sections. Update callers accordingly. For
project roots built by `newProjectFixtureAt`, `init` already writes `AGENTS.md`/`CLAUDE.md` when the fixture runs `init`.
Fixtures that write files by hand need both: add them in the helper.

For `internal/cli/testdata/**`, add the same four files to each component directory, and `AGENTS.md`/`CLAUDE.md` with
a block to each project root. Generate them once with a throwaway script that calls the same content, then commit the
files.

- [ ] **Step 4: Run the whole CLI suite**

Run: `make generate-msgid && systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 go test ./internal/cli/ > "$SCRATCH/cli.log" 2>&1; tail -30 "$SCRATCH/cli.log"`
Expected: PASS. Every failing "N files" count gains the docs entries. Update each count to what the new output shows,
after checking the extra entries are the doc sets you expect, not stray warnings.

- [ ] **Step 5: Lint and commit**

```bash
git add -A internal
git commit -m "feat(lint): documentation checks for component repositories, workbenches, projects and local components

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Multilingual `BRICKKIT.md` — cached from every source, published, served by the market

**Files:**
- Modify: `internal/source/source.go` (`docFetcher` → `docFiles`), `local.go`, `git.go`, `gitcache.go` (`files(ctx, tag, dir)`),
  `market.go`; their tests (`internal/source/*_test.go`); `internal/cli/publish.go`; `internal/manifest/scaffold.go`
  (`MaxDocsTotalBytes`); market-server `model/model.go`, `validator/validator.go`, `service/service.go`,
  `repo/{repo,memory,postgres}.go`, `repo/schema.sql`, `handler/versions.go`, their tests;
  `docs/{en,zh}/11-reference/06-market-api.md`

**Interfaces:**
- Consumes: `docspec.SplitTranslation`, `docspec.FileBrickkit`, `docspec.MaxTranslations`, `docspec.ValidLang`
- Produces:
  - `type docFetcher interface{ docFiles(ctx context.Context, componentID, version string) (map[string][]byte, error) }`
    (key = file name: `BRICKKIT.md`, `BRICKKIT.zh.md`)
  - `func (r *gitRepo) dir(ctx context.Context, tag, dir string) ([]string, error)` — names at a directory of a tag
  - `const MaxDocsTotalBytes = 4 * MaxDocBytes`
  - market: `PublishRequest.DocTranslations map[string]string \`json:"docTranslations,omitempty"\``,
    `Version.DocTranslations map[string]string \`json:"-"\``, `Version.DocLanguages []string \`json:"docLanguages,omitempty"\``,
    `func (s *Service) GetDoc(ctx, id, componentID, version, lang string) (string, error)`

- [ ] **Step 1: Write the failing tests**

In `internal/source/` (next to the existing doc cache tests; `gittest` remotes):

```go
func TestGitCachesEveryDocLanguage(t *testing.T) {
	remote := gittest.NewRemote(t, "demo-quote")
	remote.Tag("1.0.0", map[string]string{
		"component.yaml":    "apiVersion: brickkit/v1\nkind: Component\nmetadata: {id: demo/quote, name: q, version: 1.0.0, description: d}\ndeployment: {type: container, image: x, port: 8080}\nhealthCheck: {type: none}\n",
		"BRICKKIT.md":       "# en\n",
		"BRICKKIT.zh.md":    "# zh\n",
		"BRICKKIT.zh-CN.md": "# not a code\n",
	})
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(projfile.Source{Name: "g", Type: projfile.SourceTypeGit, BaseURL: gittest.BaseURL(filepath.Dir(remote.Dir()))}), Options{})
	_, err := c.Fetch(context.Background(), "demo/quote", "1.0.0")
	require.NoError(t, err)
	primary, langs := layout.CachedDocLangs("demo/quote", "1.0.0")
	assert.True(t, primary)
	assert.Equal(t, []string{"zh"}, langs)
}
```

Use the existing helper names in `internal/source` tests (`newProject`, `newClient`, `cfgWithSources`; the method that
fetches a manifest is whatever `add` calls — read `source.go` and use it). Write the same test for a local source
(component directory with `BRICKKIT.md` + `BRICKKIT.ja.md`) and for the market source against `httptest` (the doc
endpoint answers `?lang=zh`; the manifest envelope carries `"docLanguages":["zh"]`).

In `market-server/internal/service/service_test.go`:

```go
func TestPublishWithTranslationsAndGetByLanguage(t *testing.T) {
	svc, id := newServiceWithOwner(t)
	req := samplePublish("demo/quote", "1.0.0")
	req.Doc = "# en\n"
	req.DocTranslations = map[string]string{"zh": "# zh\n"}
	_, err := svc.Publish(context.Background(), id, req)
	require.NoError(t, err)
	got, err := svc.GetDoc(context.Background(), id, "demo/quote", "1.0.0", "zh")
	require.NoError(t, err)
	assert.Equal(t, "# zh\n", got)
	_, err = svc.GetDoc(context.Background(), id, "demo/quote", "1.0.0", "ja")
	assert.ErrorIs(t, err, ErrNotFound)
}
```

In `validator_test.go`: an invalid code (`"zh-CN"`), more than `MaxTranslations`, and a total over `MaxDocsTotalBytes`
each fail validation. Use the helper names those test files already use.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/source/ && (cd market-server && go test ./...)`
Expected: FAIL — only `BRICKKIT.md` is cached; `DocTranslations` undefined.

- [ ] **Step 3: Implement the client side**

- `gitcache.go`: add

```go
// names 列出 tag 里某个目录下的文件名（不递归）；目录为空串时是仓库根。
func (r *gitRepo) names(ctx context.Context, tag, dir string) ([]string, error) {
	spec := tag
	if dir != "" {
		spec = tag + ":" + dir
	}
	out, err := runGit(ctx, r.dir, "ls-tree", "--name-only", spec)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}
```

- `git.go`: `docFiles` lists `names(ctx, tag, subpath)`, keeps every name where
  `base, _, ok := docspec.SplitTranslation(n); ok && base == docspec.FileBrickkit`, and reads each with `fileAtVersion`.
- `local.go`: `docFiles` checks the version as `docBytes` did, then globs the directory the same way.
- `market.go`: `docFiles` fetches `/doc` (primary; 404 = none), then reads the manifest envelope's
  `data.docLanguages`. To avoid a second request, remember `docLanguages` in `rememberSignature`'s companion map, filled
  by `manifestBytes`. For each language it fetches `/doc?lang=<code>`.
- `source.go`: `cacheDoc` writes each returned file next to the cached manifest
  (`filepath.Join(c.layout.CachedManifestDir(id, version), name)`), keeping at most `docspec.MaxTranslations` translations
  and skipping invalid names. `Client.Doc` stays as is (primary only).
- `publish.go`: `loadDoc` also globs the component root for `BRICKKIT.<lang>.md`. Each must be valid UTF-8 and at most
  `MaxDocBytes`; there may be at most `docspec.MaxTranslations`; the total must be at most `manifest.MaxDocsTotalBytes`.
  Failures use the existing `CliPublishDocCannotBePublished` error with new reason keys `cli.publish.too_many_translations`
  (en: `%[1]d translations; at most %[2]d are published` / zh: `有 %[1]d 份译本；最多发布 %[2]d 份`) and
  `cli.publish.docs_total_too_large` (en: `the docs total %[1]d bytes; the limit is %[2]d` / zh:
  `文档合计 %[1]d 字节；上限 %[2]d`). It fills `req.DocTranslations`.

- [ ] **Step 4: Implement the market side**

- `model.go`: `PublishRequest.DocTranslations`; `Version.DocTranslations` (`json:"-"`); `Version.DocLanguages`
  (`json:"docLanguages,omitempty"`, filled from the map's sorted keys when the version is read); `MaxDocsTotalBytes =
  manifest.MaxDocsTotalBytes`.
- `validator.go`: each key `docspec.ValidLang`, ≤ `docspec.MaxTranslations` keys, each ≤ `MaxDocBytes`, total (primary +
  translations) ≤ `MaxDocsTotalBytes`; English reasons like the existing ones.
- `schema.sql`: `ALTER TABLE component_versions ADD COLUMN IF NOT EXISTS doc_translations JSONB NOT NULL DEFAULT '{}'::jsonb;`
  (append-only, as `schema_test.go` requires).
- `postgres.go` / `memory.go`: store and read `doc_translations` (JSON-encode the map).
- `service.go`: `Publish` copies `req.DocTranslations` into the version. `GetDoc(…, lang)`: lang "" → `v.Doc`; otherwise
  `v.DocTranslations[lang]`; missing → `ErrNotFound`.
- `handler/versions.go`: `doc` passes `r.URL.Query().Get("lang")`. The manifest handler's envelope `data` includes
  `docLanguages`.
- `docs/{en,zh}/11-reference/06-market-api.md`: document `?lang=`, `docTranslations` in the publish body, and
  `docLanguages` in the manifest response.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/source/ ./internal/cli/ && (cd market-server && go test ./...)`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

```bash
git add -A internal market-server docs/en/11-reference/06-market-api.md docs/zh/11-reference/06-market-api.md
git commit -m "feat(docs-i18n): BRICKKIT.<lang>.md cached from every source, published and served by the market

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: The skills installed into projects

**Files:**
- Modify: `internal/skills/assets/{en,zh}/claude/skills/brickkit-component/SKILL.md`,
  `internal/skills/assets/{en,zh}/claude/skills/brickkit-assemble/SKILL.md`,
  `internal/skills/assets/{en,zh}/claude/skills/brickkit-troubleshoot/SKILL.md` (the eleven codes)
- Create: `internal/skills/assets/{en,zh}/claude/skills/brickkit-plan-change/SKILL.md`
- Test: `internal/skills/content_test.go`, `internal/skills/skillmd_test.go` (existing guards: frontmatter, parity)

**Interfaces:**
- Consumes: the file names, sections and codes of Tasks 2–7.

- [ ] **Step 1: Write the failing test**

Add to `internal/skills/content_test.go`:

```go
func TestSkillsDescribeTheDocSpec(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		read := func(target string) string {
			for _, a := range AssetsFor(ScopeProject, lang) {
				if a.Target == target {
					b, _ := a.Content()
					return string(b)
				}
			}
			t.Fatalf("%s: no asset %s", lang, target)
			return ""
		}
		comp := read(".claude/skills/brickkit-component/SKILL.md")
		for _, h := range []string{docspec.Heading(docspec.BeforeDeploy, string(lang)), docspec.Heading(docspec.CodeMap, string(lang)), "BRICKKIT.zh.md", "DOC_OUT_OF_STEP"} {
			assert.Contains(t, comp, h, lang)
		}
		plan := read(".claude/skills/brickkit-plan-change/SKILL.md")
		for _, s := range []string{"brickkit deps", "brickkit lint", "AGENTS.md"} {
			assert.Contains(t, plan, s, lang)
		}
		assert.NotContains(t, read(".claude/skills/brickkit-assemble/SKILL.md"), "project map BRICKKIT.md")
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/skills/ -run TestSkillsDescribeTheDocSpec`
Expected: FAIL — no asset `brickkit-plan-change`.

- [ ] **Step 3: Write the content**

`brickkit-component` (both languages): replace rule 12 with a section **"The component's documents"**. It covers:
- the five required files and what each holds (one line each);
- the six `BRICKKIT.md` headings and the five `AGENTS.md` headings, exactly as `docspec` names them in that language;
- no relative links in `BRICKKIT.md`;
- the Code map format (tables, backticked paths, directories end in `/`);
- translations as siblings (`BRICKKIT.zh.md`), with the primary canonical, `AGENTS.md` not translated, and pairs linking
  each other;
- history only in Git;
- `brickkit lint` lists every doc warning, with the eleven codes, each with a one-line fix;
- "change the docs in the same commit as the code".

Mention `metadata.repository`.

`brickkit-assemble`: every mention of the project map becomes "the component table at the end of `AGENTS.md`
(maintained by `add` / `remove` / `upgrade`; edit outside the markers only)".

`brickkit-troubleshoot`: a table row per new code.

`brickkit-plan-change` (new; frontmatter `name: brickkit-plan-change`; description: "Use when the user brings a new
requirement, a feature or a change and you need to decide which component owns it, whether it is sound, and how to
plan it across components — 'add a feature', 'new requirement', 'where should this go', 'plan this change'."):

1. Find the owner: the "What it does" column of the table in `AGENTS.md`, then the candidates' `BRICKKIT.md`
   Purpose: Owns / Does not own. "Does not own" names the real owner.
2. Check it against what is written. Each point leads to a person's decision:
   - the owner's boundary: needing it to own something listed under "Does not own" is a boundary change;
   - project Conventions and `docs/decisions/`: a conflict with a recorded decision;
   - the dependency direction: no cycles (`brickkit deps <id>`), and only the directions the design allows;
   - the contracts: additive changes are a minor version, removing or changing meaning is a major version, and every
     consumer moves.
3. The outcome is one of:
   - fits one component;
   - needs a provider's contract first;
   - needs a new component (`brickkit new`);
   - conflicts: stop and put it to the person, quoting the file that says so.
4. Plan: providers before consumers (the order `brickkit deps` prints). Per component, in one commit: contract, code,
   `BRICKKIT.md` / `AGENTS.md`. Then the version (patch / minor / major), release, and `brickkit upgrade` in the project.
5. Test: a focus run, `brickkit up` in the component's directory.
6. Done: `brickkit lint` shows no doc warnings for what changed.

Both languages written independently, same structure.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/skills/ ./tests/...`
Expected: PASS (`skillmd_test.go` checks frontmatter and that both language trees have the same targets).

- [ ] **Step 5: Lint and commit**

```bash
git add -A internal/skills
git commit -m "feat(skills): component docs in brickkit-component, the table in AGENTS.md, a new brickkit-plan-change

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: BrickKit's documentation

**Files:** (each in `docs/en/` and `docs/zh/`, written independently with the same structure)
- Rewrite: `03-component-guide/08-component-doc-spec.md` → title "A component's documentation" / "组件的文档"
- Modify: `03-component-guide/01-component-anatomy.md`, `02-component-yaml-reference.md` (`metadata.repository`),
  `04-new-and-skeleton.md`, `05-local-dev-fractal.md`, `06-artifacts-and-contracts.md` (`event-contract`, Contracts
  section), `README.md` of the module
- Modify: `08-ai-guide/02-ai-dev-workflow.md`, `03-component-doc-spec.md`, `04-fractal-reading.md`,
  `01-ai-routing-table.md`, `README.md`; create `08-ai-guide/03-judging-a-requirement.md` and renumber `03`→`04`,
  `04`→`05`, `05`→`06` (follow the docs numbering convention: `git mv`, update every link, `llms.txt`/`llms.zh.txt`
  page list, `docs/{en,zh}/README.md`, the module README, `scripts/check-doc-tree.py` if it lists pages)
- Modify: `02-project-guide/01-init-and-project-creation.md` (AGENTS.md / CLAUDE.md, the block, the old map),
  `02-add-and-component-install.md`, `10-lint-and-checks.md` (doc checks), `00-intro/*` pages that mention the project
  `BRICKKIT.md`, `01-three-layers/01-overview.md` and `09-field-reference.md` (`metadata.repository`),
  `06-architecture/06-bare-repo-mechanism.md` and `07-cache-design.md` (translations cached), `07-cli-reference/README.md`
  (`init`, `new`, `skills`, `lint` outputs), `04-shell/05-shell-development.md`
- Modify: `AGENTS.md` / `AGENTS.zh.md` §3.1 ("Which components the project has and where their docs are" → "the
  component table at the end of `AGENTS.md`"), §3.3, §3.4, §5 (`skills` row), §9; `README.md` / `README.zh.md` where they
  mention the project map
- Modify: `tests/checklist/清单.tsv` — new rows (next free numbers in their groups):
  - `init` writes `AGENTS.md` + `CLAUDE.md` and no project `BRICKKIT.md`;
  - the block keeps its language for a teammate;
  - `lint` reports a missing `BRICKKIT.md` section as a warning;
  - a fresh clone updates skills without `skills.lock`;
  - `BRICKKIT.zh.md` is cached on `add`.
- Modify: `tests/components/*` — each fixture component gains `BRICKKIT.md`, `AGENTS.md`, `CLAUDE.md` (and its README
  follows the three sections), so `brickkit lint` in each is clean

**Interfaces:**
- Consumes: everything above. The commit hook regenerates `llms/`.

- [ ] **Step 1: Find every stale statement**

Run: `grep -rn "BRICKKIT.md" docs/en docs/zh AGENTS.md AGENTS.zh.md README.md README.zh.md | grep -v "manifests/" > "$SCRATCH/stale.txt"; wc -l "$SCRATCH/stale.txt"`
Expected: a list. Each line is either the component doc (keep it, but check it says six sections, translations, no
relative links) or the project map (rewrite it to the `AGENTS.md` block). Also grep `skills.lock` and `five sections` /
`五节`.

- [ ] **Step 2: Write `08-component-doc-spec.md` ("A component's documentation")**

Sections, in order:
1. **Who reads what**: the file table from spec §4.1.
2. **One fact, one home**: spec §3 table, with "explaining is not restating".
3. **`BRICKKIT.md`**: the six sections with what goes in each, the no-relative-links rule and why.
4. **`AGENTS.md`**: the five sections, the Code map format, the block at the end, "written once, not translated".
5. **`README.md`**: three sections; a router.
6. **`docs/`**: optional files, decisions numbered.
7. **More than one language**: spec §5.
8. **What `brickkit lint` checks**: the eleven codes in a table, plus `--strict` for a team gate.
9. **A complete example**: `demo/quote`'s `BRICKKIT.md`, `AGENTS.md`, `README.md`, each in full; the zh page's example
   is in Chinese.
10. **How it reaches users**: the cache paths with translations, the project table, the market.

The rule for concepts applies: say plainly what each thing is before what it buys.

- [ ] **Step 3: Write `08-ai-guide/03-judging-a-requirement.md`**

Spec §7.1–§7.3 as a page: understanding a project (the route), judging a requirement (owner, checks, outcomes), planning
and changing (order, per-component steps, focus run, done = lint clean), and what goes to a person. Link it from
`02-ai-dev-workflow.md` and the module README. Mention the `brickkit-plan-change` skill.

- [ ] **Step 4: Update the other pages and entry files listed above**

For `02-project-guide/01-init-and-project-creation.md`: replace the project map section with "The project's
`AGENTS.md`":
- the four author sections;
- the block and what is in it;
- the recorded language;
- which commands rewrite it, and that nothing writes outside it;
- `CLAUDE.md`;
- what happens to an old project map;
- the `init` output, regenerated by actually running `brickkit init` in a temp dir.

Do the same for every page that shows command output (`init`, `new`, `lint`, `skills status`): run the real command
and paste the real output.

- [ ] **Step 5: Fixtures under `tests/components/`**

For each directory, write the four docs following the spec, using the component's real `component.yaml` (dependencies,
required keys, artifacts mentioned). Then:

Run: `for d in tests/components/*/; do (cd "$d" && go run ../../../cmd/brickkit lint) | tail -1; done`
Expected: every line ends `0 warnings`.

- [ ] **Step 6: Checks and commit**

Run: `make -k lint && systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 make -k test-all > "$SCRATCH/all.log" 2>&1; tail -20 "$SCRATCH/all.log"`
Expected: all green; the regression checklist passes including the new rows.

```bash
git add -A docs AGENTS.md AGENTS.zh.md README.md README.zh.md llms.txt llms.zh.txt llms tests
git commit -m "docs: component and project documentation — the spec, judging a requirement, AGENTS.md, translations, lint checks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: End-to-end check on a copy of be-assembly-standard's shape

**Files:**
- Create: `internal/cli/component_docs_e2e_test.go`

**Interfaces:**
- Consumes: the CLI as a whole.

- [ ] **Step 1: Write the test**

One in-process scenario mirroring how the maintainer will use it:
1. `brickkit init shop`; `brickkit new erp/sales --contract proto`; `brickkit new mdm/customer`.
2. Fill `erp/sales`'s docs so that its `component.yaml` depends on `mdm/customer@0.1.0` and its `BRICKKIT.md` mentions
   it. Also add a `BRICKKIT.zh.md` linking nothing, with the same six sections.
3. `brickkit add --local`.

Then assert:
- the `AGENTS.md` table has both rows, with `BRICKKIT.md +zh` for `erp/sales`;
- `brickkit lint` (project) shows doc warnings only for the TODO placeholders left in `mdm/customer`;
- removing `mdm/customer`'s mention from `erp/sales`'s Dependencies section produces `DOC_OUT_OF_STEP` naming
  `mdm/customer`;
- running the same `add` with the CLI language switched to Chinese leaves `lang=en` and English headings in the block.

```go
func TestComponentDocsEndToEnd(t *testing.T) {
	root := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, root, "init", "shop", "--yes").code)
	shop := filepath.Join(root, "shop")
	require.Equal(t, clierr.ExitOK, runIn(t, shop, "new", "mdm/customer").code)
	require.Equal(t, clierr.ExitOK, runIn(t, shop, "new", "erp/sales", "--contract", "proto").code)
	sales := filepath.Join(shop, "components", "erp", "sales")
	appendTo(t, filepath.Join(sales, "component.yaml"), "\ndependencies:\n  components:\n    - mdm/customer@0.1.0\n")
	doc := readFile(t, filepath.Join(sales, "BRICKKIT.md"))
	doc = strings.Replace(doc, "## Dependencies\n\n", "## Dependencies\n\n- mdm/customer: who the customer is.\n", 1)
	writeFile(t, filepath.Join(sales, "BRICKKIT.md"), doc)
	writeFile(t, filepath.Join(sales, "BRICKKIT.zh.md"), zhSixSections("erp/sales"))
	require.Equal(t, clierr.ExitOK, runIn(t, shop, "add", "--local").code)

	agents := readFile(t, filepath.Join(shop, "AGENTS.md"))
	assert.Contains(t, agents, "| erp/sales | 0.1.0 |")
	assert.Contains(t, agents, "BRICKKIT.md +zh")

	r := runIn(t, shop, "lint")
	assert.NotContains(t, r.stdout, "DOC_OUT_OF_STEP")
	assert.Contains(t, r.stdout, "placeholder")

	writeFile(t, filepath.Join(sales, "BRICKKIT.md"), strings.Replace(doc, "- mdm/customer: who the customer is.\n", "", 1))
	r = runWithLogs(t, shop, "lint")
	assert.Contains(t, r.stdout+r.stderr, "mdm/customer")
	assert.Contains(t, r.stderr, "DOC_OUT_OF_STEP")
}
```

`zhSixSections(id)` writes `# id` and the six Chinese headings each followed by one line of text (no TODO).
`readFile`/`writeFile` are the existing CLI test helpers. If an exact helper name differs, use the existing one, and
record that in the ledger as a ruling.

- [ ] **Step 2: Run it**

Run: `go test ./internal/cli/ -run TestComponentDocsEndToEnd -v`
Expected: PASS. If it fails, the failure is a real integration bug in an earlier task. Fix it there with a unit test
first (systematic-debugging), then rerun.

- [ ] **Step 3: Full checks and commit**

Run: `make -k lint && systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 make -k test-all > "$SCRATCH/all.log" 2>&1; tail -5 "$SCRATCH/all.log"`
Expected: green.

```bash
git add internal/cli/component_docs_e2e_test.go
git commit -m "test(cli): component docs end to end — new, add, the AGENTS.md table, translations, lint

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
