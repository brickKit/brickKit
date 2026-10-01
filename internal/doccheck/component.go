package doccheck

import (
	"os"
	"slices"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/mdtext"
	"github.com/brickkit/brickkit/internal/msgid"
)

// requiredFiles 是组件必须有的文档。
var requiredFiles = []string{docspec.FileBrickkit, docspec.FileAgents, docspec.FileClaude, docspec.FileReadme}

// translatedKinds 是根目录下会有译本的文档（CLAUDE.md 只有一行，不翻译）。
var translatedKinds = []string{docspec.FileBrickkit, docspec.FileAgents, docspec.FileReadme}

// Component 检查组件目录 dir 的文档（m 是它的 component.yaml；为 nil 时不核对清单里的事实）。
func Component(dir string, m *manifest.Manifest) []*clierr.Error {
	var out []*clierr.Error
	for _, f := range requiredFiles {
		if !exists(filepath.Join(dir, f)) {
			out = append(out, warn(clierr.CodeDocFileMissing, i18n.T(msgid.DoccheckFileMissing, f), f, 0))
		}
	}
	out = append(out, claude(dir)...)
	out = append(out, versionLinks(dir)...)
	for _, rel := range docFiles(dir) {
		d, _ := read(dir, rel)
		base, lang, ok := docspec.SplitTranslation(path.Base(rel))
		if !ok {
			out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNotALanguage, path.Base(rel)), rel, 0))
			continue
		}
		primaryRel := path.Join(path.Dir(rel), base)
		mine := d
		if primaryRel == docspec.FileAgents {
			mine = withoutBlock(d) // 维护段是平台写的，不归作者查
		}
		out = append(out, placeholders(mine)...)
		switch primaryRel {
		case docspec.FileBrickkit:
			out = append(out, brickkit(d, m, lang)...)
		case docspec.FileAgents:
			out = append(out, links(dir, mine, true)...)
			if lang == "" {
				out = append(out, sections(d, docspec.KindComponentAgents)...)
				out = append(out, codeMap(dir, d)...)
				out = append(out, block(d)...)
			}
		case docspec.FileReadme:
			out = append(out, links(dir, d, true)...)
			if lang == "" {
				out = append(out, sections(d, docspec.KindReadme)...)
			}
		default:
			out = append(out, links(dir, d, true)...)
		}
		if lang != "" {
			out = append(out, translation(dir, d, primaryRel)...)
		}
	}
	return out
}

// docFiles 是要查的 Markdown：根目录的 BRICKKIT / AGENTS / README 及其译本（含后缀写错了的），
// 加上 docs/ 下的全部文件。docs/ 里名字带点、却不是合法译本的文件，只在同目录里有别的译本时才算写错了后缀
// （v1.2-notes.md 这种名字本身就带点）。根目录其他 Markdown（CHANGELOG.md……）不归文档规范管。
func docFiles(dir string) []string {
	var out []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		for _, k := range translatedKinds {
			if e.Name() == k || strings.HasPrefix(e.Name(), strings.TrimSuffix(k, ".md")+".") {
				out = append(out, e.Name())
				break
			}
		}
	}
	tree := docsTree(dir)
	translatedDirs := map[string]bool{}
	for _, rel := range tree {
		if _, lang, ok := docspec.SplitTranslation(path.Base(rel)); ok && lang != "" {
			translatedDirs[path.Dir(rel)] = true
		}
	}
	for _, rel := range tree {
		if _, _, ok := docspec.SplitTranslation(path.Base(rel)); ok || translatedDirs[path.Dir(rel)] {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// codeMap 核对代码地图里每条路径都在：只看那一节表格里的行内代码，含 /、不含空白、* 与 ://。
func codeMap(dir string, d doc) []*clierr.Error {
	body, start := section(d, docspec.CodeMap)
	if start == 0 {
		return nil
	}
	var out []*clierr.Error
	for i, line := range strings.Split(body, "\n") {
		for _, c := range mdtext.TableCells(line) {
			for _, p := range mdtext.InlineCode(c) {
				// 以 / 开头的是路由（`/healthz`），不是仓库里的路径
				if !strings.Contains(p, "/") || strings.HasPrefix(p, "/") || strings.ContainsAny(p, " \t*") || strings.Contains(p, "://") {
					continue
				}
				if !exists(filepath.Join(dir, filepath.FromSlash(strings.TrimSuffix(p, "/")))) {
					out = append(out, warn(clierr.CodeDocPathMissing, i18n.T(msgid.DoccheckPathMissing, p), d.rel, start+1+i))
				}
			}
		}
	}
	return out
}

// brickkit 查 BRICKKIT.md（及译本）：小节、不许相对链接、component.yaml 的事实都提到了。
func brickkit(d doc, m *manifest.Manifest, lang string) []*clierr.Error {
	var out []*clierr.Error
	// 认不出标题的语言（ja……）的译本只查与原文对不对得上（translation），六节与清单事实都查不了
	if lang != "" && !slices.Contains(docspec.HeadingLangs(), lang) {
		m = nil
	} else {
		out = sections(d, docspec.KindBrickkit)
	}
	for _, l := range mdtext.Links(d.body) {
		if isRelative(l.Target) {
			out = append(out, warn(clierr.CodeDocLinkNotPortable, i18n.T(msgid.DoccheckBrickkitRelativeLink, l.Target), d.rel, l.Line))
		}
	}
	if m == nil {
		return out
	}
	headingLang := lang
	if headingLang == "" {
		headingLang = "en"
	}
	mention := func(s docspec.Section, fact string, msg msgid.ID) {
		body, line := section(d, s)
		if line == 0 || mentions(body, fact) {
			return // 缺整节已经报过了
		}
		out = append(out, warn(clierr.CodeDocOutOfStep, i18n.T(msg, fact, docspec.Heading(s, headingLang)), d.rel, line))
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

// translation 查一份译本：有原文、二级小节数一样、两边互相链接（checkLinks 为假时不查最后一条：
// BRICKKIT.md 根本不放相对链接）。
func translation(dir string, d doc, primaryRel string) []*clierr.Error {
	p, ok := read(dir, primaryRel)
	if !ok {
		return []*clierr.Error{warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNoPrimary, primaryRel), d.rel, 0)}
	}
	var out []*clierr.Error
	if a, b := len(mdtext.Sections(p.body)), len(mdtext.Sections(d.body)); a != b {
		out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckSectionCount, b, a, primaryRel), d.rel, 0))
	}
	return out
}

// versionLinks 查有译本的文件：原文与每份译本开头都要链到这组里的每个其他语言版本
// （BRICKKIT*.md 除外：它根本不放相对链接）。
func versionLinks(dir string) []*clierr.Error {
	groups := map[string][]string{} // 原文路径 → 这组里实际存在的文件（原文在前）
	var order []string
	for _, rel := range docFiles(dir) {
		base, _, ok := docspec.SplitTranslation(path.Base(rel))
		if !ok {
			continue
		}
		primary := path.Join(path.Dir(rel), base)
		if primary == docspec.FileBrickkit {
			continue
		}
		if _, seen := groups[primary]; !seen {
			order = append(order, primary)
		}
		groups[primary] = append(groups[primary], rel)
	}
	var out []*clierr.Error
	for _, primary := range order {
		members := groups[primary]
		if len(members) < 2 {
			continue
		}
		for _, rel := range members {
			d, ok := read(dir, rel)
			if !ok {
				continue
			}
			for _, other := range members {
				if other != rel && !linksTo(d, path.Base(other)) {
					out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNotLinked, path.Base(rel), path.Base(other)), rel, 0))
				}
			}
		}
	}
	return out
}

// mentions 判断 body 里有没有把 fact 当作一个完整的名字提到：前后不能紧挨着名字里会出现的字符，
// 所以 erp/xy 不算提到 erp/x、DB_HOST 不算提到 DB；erp/x@1.0.0、`erp/x`、"erp/x:" 都算。
func mentions(body, fact string) bool {
	for i := 0; ; {
		j := strings.Index(body[i:], fact)
		if j < 0 {
			return false
		}
		at, end := i+j, i+j+len(fact)
		before := at == 0 || !nameByte(body[at-1]) && body[at-1] != '/' && body[at-1] != '.'
		after := end == len(body) || !nameByte(body[end])
		if before && after {
			return true
		}
		i = at + 1
	}
}

func nameByte(b byte) bool {
	return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
