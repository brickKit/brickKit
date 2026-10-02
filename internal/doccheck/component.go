package doccheck

import (
	"path"
	"path/filepath"
	"slices"
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
	set := collect(dir, translatedKinds)
	for _, f := range set.files {
		d, _ := read(dir, f.rel)
		if f.primary == "" {
			out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNotALanguage, path.Base(f.rel)), f.rel, 0))
			continue
		}
		mine := d
		if f.primary == docspec.FileAgents {
			mine = withoutBlock(d) // 维护段是平台写的，不归作者查
		}
		out = append(out, placeholders(mine)...)
		switch f.primary {
		case docspec.FileBrickkit:
			out = append(out, brickkit(d, m, f.lang)...)
		case docspec.FileAgents:
			out = append(out, links(dir, mine, true)...)
			if f.lang == "" {
				out = append(out, sections(d, docspec.KindComponentAgents)...)
				out = append(out, codeMap(dir, d)...)
				out = append(out, block(d)...)
			}
		case docspec.FileReadme:
			out = append(out, links(dir, d, true)...)
			if f.lang == "" {
				out = append(out, sections(d, docspec.KindReadme)...)
			}
		default:
			out = append(out, links(dir, d, true)...)
		}
	}
	return append(out, set.translations(dir)...)
}

// codeMap 核对代码地图里每条路径都在：只看那一节表格里的行内代码，不含空白、* 与 ://。
//
// 第一张表（路径 → 管什么）的第一列按规范就是路径，顶层文件（`main.go`、`Dockerfile`）也查；
// 别的格子里只有含 / 的才算路径——不含的是函数、类型这类名字（`main.go` 的 `runMode`）。
func codeMap(dir string, d doc) []*clierr.Error {
	body, start := section(d, docspec.CodeMap)
	if start == 0 {
		return nil
	}
	var out []*clierr.Error
	table, inTable := 0, false
	for i, line := range strings.Split(body, "\n") {
		switch row := strings.HasPrefix(strings.TrimSpace(line), "|"); {
		case row:
			inTable = true
		case inTable:
			table, inTable = table+1, false
		}
		for col, c := range mdtext.TableCells(line) {
			pathColumn := table == 0 && col == 0
			for _, p := range mdtext.InlineCode(c) {
				// 以 / 开头的是路由（`/healthz`），不是仓库里的路径
				if (!pathColumn && !strings.Contains(p, "/")) || strings.HasPrefix(p, "/") || strings.ContainsAny(p, " \t*") || strings.Contains(p, "://") {
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
