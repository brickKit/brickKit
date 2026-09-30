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
