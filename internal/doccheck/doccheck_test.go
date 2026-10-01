package doccheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
)

func TestMain(m *testing.M) {
	i18n.SetCurrent(i18n.EN)
	os.Exit(m.Run())
}

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

func messages(ws []*clierr.Error) string {
	var b strings.Builder
	for _, w := range ws {
		b.WriteString(w.Format())
	}
	return b.String()
}

func TestCleanComponentHasNoWarnings(t *testing.T) {
	dir, m := component(t, nil)
	ws := Component(dir, m)
	assert.Empty(t, ws, messages(ws))
}

func TestEveryWarningIsAWarning(t *testing.T) {
	dir, m := component(t, map[string]string{"README.md": ""})
	for _, w := range Component(dir, m) {
		assert.True(t, w.Warning, "documentation problems never block")
	}
}

func TestMissingFiles(t *testing.T) {
	dir, m := component(t, map[string]string{"CLAUDE.md": "", "README.md": ""})
	assert.Equal(t, []clierr.Code{clierr.CodeDocFileMissing, clierr.CodeDocFileMissing}, codes(Component(dir, m)))
}

func TestMissingSectionAndChineseHeadingsCount(t *testing.T) {
	zh := strings.NewReplacer("## Purpose", "## 组件定位", "## Before you deploy\n", "").Replace(goodBrickkit)
	dir, m := component(t, map[string]string{"BRICKKIT.md": zh})
	ws := Component(dir, m)
	require.Len(t, ws, 1, messages(ws))
	assert.Equal(t, clierr.CodeDocSectionMissing, ws[0].Code)
	assert.Contains(t, ws[0].Format(), "Before you deploy")
}

func TestCodeMapPathsChecked(t *testing.T) {
	agents := strings.Replace(goodAgents, "| `api/` | contract |", "| `api/` | contract |\n| `gone/x.go` | missing |", 1)
	agents += "\nOutside the map `nope/also.go` is not checked.\n"
	dir, m := component(t, map[string]string{"AGENTS.md": agents})
	ws := Component(dir, m)
	require.Equal(t, []clierr.Code{clierr.CodeDocPathMissing}, codes(ws))
	assert.Contains(t, ws[0].Format(), "gone/x.go")
	assert.Contains(t, ws[0].Format(), "10", "the line of the row")
}

func TestLinks(t *testing.T) {
	readme := goodReadme + "\n[gone](docs/gone.md)\n" + fence + "\n[in code](x.md)\n" + fence + "\n[up](../outside.md)\n[web](https://example.com) [anchor](#development)\n"
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
	all := messages(ws)
	for _, fact := range []string{"infra/cache", "DB_PASSWORD", "events/quote.json", "demo/member"} {
		assert.Contains(t, all, fact)
	}
}

func TestDependencyWithVersionCountsAsMentioned(t *testing.T) {
	doc := strings.Replace(goodBrickkit, "- demo/hello: the greeting.", "- `demo/hello@1.0.0`: the greeting.", 1)
	dir, m := component(t, map[string]string{"BRICKKIT.md": doc})
	assert.Empty(t, Component(dir, m))
}

func TestPlaceholders(t *testing.T) {
	doc := goodBrickkit + "\n<!-- TODO: fill -->\n" + fence + "\nTODO in code is fine\n" + fence + "\n`TODO` inline is fine\nTODOS and todo are words of their own\n待补\n"
	dir, m := component(t, map[string]string{"BRICKKIT.md": doc})
	assert.Equal(t, []clierr.Code{clierr.CodeDocPlaceholder, clierr.CodeDocPlaceholder}, codes(Component(dir, m)))
}

func TestOtherRootMarkdownIsNotChecked(t *testing.T) {
	dir, m := component(t, map[string]string{"CHANGELOG.md": "TODO\n[gone](nope.md)\n"})
	assert.Empty(t, Component(dir, m))
}

func TestTranslations(t *testing.T) {
	header := "# demo/quote\n\n[English](README.md) · [中文](README.zh.md)\n"
	paired := strings.Replace(goodReadme, "# demo/quote\n", header, 1)
	dir, m := component(t, map[string]string{"README.md": paired, "README.zh.md": paired})
	assert.Empty(t, Component(dir, m), "a pair in step that links both ways is clean")

	dir, m = component(t, map[string]string{"README.md": paired, "README.zh.md": paired + "\n## Extra\n"})
	assert.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(Component(dir, m)))

	dir, m = component(t, map[string]string{"README.zh.md": goodReadme})
	assert.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift, clierr.CodeDocTranslationDrift}, codes(Component(dir, m)),
		"neither links the other: each file is told which version it lacks")

	dir, m = component(t, map[string]string{"docs/design.zh.md": "# d\n"})
	assert.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(Component(dir, m)), "a translation without a primary")

	dir, m = component(t, map[string]string{"BRICKKIT.zh.md": strings.Replace(goodBrickkit, "## Purpose", "## 组件定位", 1)})
	assert.Empty(t, Component(dir, m), "BRICKKIT translations cannot link (no relative links): not required")
}

func TestTranslationSuffixNotALanguage(t *testing.T) {
	dir, m := component(t, map[string]string{"README.zh-CN.md": goodReadme})
	ws := Component(dir, m)
	require.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(ws))
	assert.Contains(t, ws[0].Format(), "README.zh-CN.md")

	dir, m = component(t, map[string]string{"docs/v1.2-notes.md": "# notes\n"})
	assert.Empty(t, Component(dir, m), "a dotted name with no translation around is just a name")
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

func TestProjectInWorkbenchLeavesDocsToComponent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("x"), 0o644))
	assert.Empty(t, Project(root), "a workbench's AGENTS.md is the component's: Component checks it")
}

// 维护区里的内容来自别的组件（它们的 metadata.description），项目改不了：占位词与链接不在维护区里查。
func TestProjectChecksSkipTheManagedBlock(t *testing.T) {
	root := t.TempDir()
	agents := "# P\n\n## Overview\nx\n\n## Conventions\nx\n\n## Where to look\nx\n\n## Pitfalls\nx\n\n" +
		"<!-- brickkit:managed:begin lang=en -->\n| a/b | 1.0.0 | A TODO list service, see [docs](docs/x.md) | BRICKKIT.md | — |\n<!-- brickkit:managed:end -->\n" +
		"\nTODO after the block counts again\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agents), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("@AGENTS.md\n"), 0o644))
	ws := Project(root)
	require.Equal(t, []clierr.Code{clierr.CodeDocPlaceholder}, codes(ws), messages(ws))
	assert.Contains(t, ws[0].Format(), "19", "line numbers still count the block's lines")
}

// 认不出标题的语言（ja）的译本，只查它与原文对不对得上，不按英文/中文标题查六节。
func TestTranslationInAnotherLanguageIsOnlyCheckedForStep(t *testing.T) {
	ja := "# demo/quote\n\n## 用途\nx\n\n## デプロイ前\nx\n\n## 依存\ndemo/hello\n\n## 設定\nQUOTE_SECRET\n\n## 契約\n`api/openapi.yaml`\n\n## シェル\nなし\n"
	dir, m := component(t, map[string]string{"BRICKKIT.ja.md": ja})
	ws := Component(dir, m)
	assert.Empty(t, ws, messages(ws))

	dir, m = component(t, map[string]string{"BRICKKIT.ja.md": ja + "\n## 余分\n"})
	assert.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(Component(dir, m)))
}

// "提到"要按整个词算：erp/xy 不算提到 erp/x，DB_HOST 不算提到 DB；erp/x@1.0.0、`erp/x` 算。
func TestMentionIsAWholeName(t *testing.T) {
	assert.True(t, mentions("- erp/x: orders", "erp/x"))
	assert.True(t, mentions("`erp/x@1.0.0` for orders", "erp/x"))
	assert.True(t, mentions("| DB | the database |", "DB"))
	assert.False(t, mentions("- erp/xy: orders", "erp/x"))
	assert.False(t, mentions("- myerp/x", "erp/x"))
	assert.False(t, mentions("DB_HOST is the host", "DB"))
	assert.True(t, mentions("see `api/openapi.yaml`.", "api/openapi.yaml"))
}

// 有译本的文件开头要链到它的每个语言版本，不只是原文与这一份译本。
func TestEveryLanguageVersionIsLinked(t *testing.T) {
	all := "# demo/quote\n\n[English](README.md) · [中文](README.zh.md) · [日本語](README.ja.md)\n"
	two := "# demo/quote\n\n[English](README.md) · [中文](README.zh.md)\n"
	body := func(head string) string { return strings.Replace(goodReadme, "# demo/quote\n", head, 1) }
	dir, m := component(t, map[string]string{"README.md": body(all), "README.zh.md": body(all), "README.ja.md": body(all)})
	assert.Empty(t, Component(dir, m))

	dir, m = component(t, map[string]string{"README.md": body(all), "README.zh.md": body(two), "README.ja.md": body(all)})
	ws := Component(dir, m)
	require.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(ws))
	assert.Contains(t, ws[0].Format(), "README.ja.md")
}

// 代码地图里以 / 开头的是路由（`/healthz`、`/api/v1/quote`），不是仓库里的路径。
// 路径表第一列本来就是路径：顶层文件（main.go、Dockerfile）不带 / 也要查；别的格子里不带 / 的照旧当成名字（函数、类型）。
func TestCodeMapPathColumnChecksTopLevelFiles(t *testing.T) {
	agents := strings.Replace(goodAgents, "| `api/` | contract |", "| `api/` | contract |\n| `Dockerfile` | the image, built from `runMode` |", 1)
	dir, m := component(t, map[string]string{"AGENTS.md": agents})
	got := Component(dir, m)
	require.Len(t, got, 1, "Dockerfile is in the path column and doesn't exist; runMode is a name, not a path")
	assert.Equal(t, clierr.CodeDocPathMissing, got[0].Code)
	assert.Contains(t, got[0].Message, "Dockerfile")
}

func TestCodeMapRoutesAreNotPaths(t *testing.T) {
	agents := strings.Replace(goodAgents, "| `api/` | contract |", "| `api/` | contract |\n| `main.go` | serves `/healthz` and `/api/v1/quote` |", 1)
	dir, m := component(t, map[string]string{"AGENTS.md": agents})
	assert.Empty(t, Component(dir, m))
}

// 维护段只在原文 AGENTS.md 里：数原文的小节时不算它的 "## BrickKit"，译本照作者自己写的那几节翻就对得上。
func TestAgentsTranslationDoesNotCountTheManagedBlock(t *testing.T) {
	header := "# demo/quote\n\n[English](AGENTS.md) · [中文](AGENTS.zh.md)\n"
	withBlock := strings.Replace(strings.Replace(goodAgents, "# demo/quote\n", header, 1),
		"<!-- brickkit:managed:begin lang=en -->\n", "<!-- brickkit:managed:begin lang=en -->\n## BrickKit\n\nRules.\n", 1)
	zh := strings.NewReplacer("## Code map", "## 代码地图", "## Build and test", "## 构建与测试", "## Design decisions", "## 设计取舍",
		"## Pitfalls", "## 易错点", "## Before changing code", "## 改代码前自查").Replace(header + goodAgents[len("# demo/quote\n"):])
	zh = zh[:strings.Index(zh, "<!-- brickkit:managed:begin")]
	dir, m := component(t, map[string]string{"AGENTS.md": withBlock, "AGENTS.zh.md": zh})
	ws := Component(dir, m)
	assert.Empty(t, ws, messages(ws))
}

// 项目根目录的译本与组件的一样查：链接、占位、小节数（不算维护段）、互链。
func TestProjectTranslationsAreChecked(t *testing.T) {
	root := t.TempDir()
	header := "# P\n\n[English](AGENTS.md) · [中文](AGENTS.zh.md)\n\n"
	agents := header + "## Overview\nx\n\n## Conventions\nx\n\n## Where to look\nx\n\n## Pitfalls\nx\n\n" +
		"<!-- brickkit:managed:begin lang=en -->\n## BrickKit\n\n## Components\n<!-- brickkit:managed:end -->\n"
	zh := header + "## 项目概述\nx\n\n## 项目约定\nx\n\n## 查找路由\nx\n\n## 易错点\nx\n"
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	write("AGENTS.md", agents)
	write("CLAUDE.md", "@AGENTS.md\n")
	write("AGENTS.zh.md", zh)
	ws := Project(root)
	assert.Empty(t, ws, messages(ws))

	write("AGENTS.zh.md", zh+"\nTODO\n[gone](docs/gone.md)\n\n## 多出来的\n")
	assert.ElementsMatch(t, []clierr.Code{clierr.CodeDocPlaceholder, clierr.CodeDocLinkBroken, clierr.CodeDocTranslationDrift}, codes(Project(root)))
}

// 项目的 docs/ 也查：坏链接、占位；链接可以指到项目里的任何地方（components/ 下的组件也是项目的一部分）。
func TestProjectDocsAreChecked(t *testing.T) {
	root := t.TempDir()
	agents := "# P\n\n## Overview\nx\n\n## Conventions\nx\n\n## Where to look\nx\n\n## Pitfalls\nx\n\n<!-- brickkit:managed:begin lang=en -->\n<!-- brickkit:managed:end -->\n"
	for rel, body := range map[string]string{
		"AGENTS.md": agents, "CLAUDE.md": "@AGENTS.md\n", "components/erp/api/AGENTS.md": "# api\n",
		"docs/conventions.md": "# C\n\n[api](../components/erp/api/AGENTS.md) [gone](decisions/x.md)\n\nTBD\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	assert.ElementsMatch(t, []clierr.Code{clierr.CodeDocLinkBroken, clierr.CodeDocPlaceholder}, codes(Project(root)))
}

// docs/<lang>/ 语言树：主语言（维护段的 lang=）那棵是原文，同一相对路径的文件互为语言版本——
// 每一页在每棵树里都有、小节数一样、开头互相链到。
func TestLanguageTrees(t *testing.T) {
	en := "# A\n\n[English](a.md) · [中文](../../zh/guide/a.md)\n\n## One\nx\n"
	zh := "# A\n\n[English](../../en/guide/a.md) · [中文](a.md)\n\n## 一\nx\n"
	dir, m := component(t, map[string]string{"docs/en/guide/a.md": en, "docs/zh/guide/a.md": zh})
	ws := Component(dir, m)
	assert.Empty(t, ws, messages(ws))

	dir, m = component(t, map[string]string{"docs/en/guide/a.md": en, "docs/zh/guide/a.md": zh + "\n## 二\n"})
	ws = Component(dir, m)
	require.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(ws))
	assert.Contains(t, ws[0].Format(), "docs/en/guide/a.md", "the section count names the primary")

	dir, m = component(t, map[string]string{"docs/en/guide/a.md": en, "docs/zh/guide/a.md": zh, "docs/en/b.md": "# B\n", "docs/zh/c.md": "# C\n"})
	ws = Component(dir, m)
	assert.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift, clierr.CodeDocTranslationDrift}, codes(ws), messages(ws))
	assert.Contains(t, messages(ws), "docs/zh/b.md", "a primary page missing from the zh tree")
	assert.Contains(t, messages(ws), "docs/en/c.md", "a zh page with no primary")

	dir, m = component(t, map[string]string{"docs/en/guide/a.md": "# A\n\n## One\nx\n", "docs/zh/guide/a.md": zh})
	ws = Component(dir, m)
	require.Equal(t, []clierr.Code{clierr.CodeDocTranslationDrift}, codes(ws))
	assert.Contains(t, ws[0].Format(), "docs/zh/guide/a.md", "the primary does not link its zh version")
}

// 只有主语言那棵在时才是语言树：docs/api/、docs/faq/ 的名字也像语言代码，但不是。
func TestLanguageTreesNeedThePrimaryLanguage(t *testing.T) {
	dir, m := component(t, map[string]string{"docs/api/a.md": "# A\n", "docs/faq/b.md": "# B\n"})
	ws := Component(dir, m)
	assert.Empty(t, ws, messages(ws))

	// 主语言是维护段记下的 lang=：zh 项目的 docs/zh/ 是原文树
	zhBlock := strings.Replace(goodAgents, "lang=en", "lang=zh", 1)
	dir, m = component(t, map[string]string{"AGENTS.md": zhBlock, "docs/zh/a.md": "# A\n", "docs/en/b.md": "# B\n"})
	ws = Component(dir, m)
	assert.Contains(t, messages(ws), "docs/en/a.md")
	assert.Contains(t, messages(ws), "docs/zh/b.md")
}
