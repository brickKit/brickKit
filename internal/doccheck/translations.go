package doccheck

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/mdtext"
	"github.com/brickkit/brickkit/internal/msgid"
)

// file 是一份要查的文档，以及它属于哪一组语言版本。
type file struct {
	rel     string // 相对检查根目录，斜杠分隔
	primary string // 这组的原文；自己就是原文时等于 rel；名字像译本、后缀却不是语言代码时为空
	lang    string // 译本的语言；原文为空
}

// docSet 是一个根目录（组件或项目）里归文档规范管的全部 Markdown。
//
// 译本有两种放法，同一套检查：
//   - 同目录加后缀：README.zh.md 是 README.md 的译本，docs/design.zh.md 是 docs/design.md 的；
//   - 语言树：docs/ 下以语言代码命名的子目录有两个以上、其中一个是主语言时，docs/<主语言>/ 是原文树，
//     其余每棵 docs/<lang>/ 是它的译本树，同一相对路径的文件互为语言版本。
//
// 主语言是 AGENTS.md 维护段记下的 lang=（没有就是 en）：只靠"目录名像语言代码"认语言树，
// docs/api/ 与 docs/faq/ 这样的目录会被误认。
type docSet struct {
	files []file
	// mainTree 与 trees：用语言树时原文树的语言与译本树的语言；不用时都为空。
	mainTree string
	trees    []string
}

// collect 列出 root 下要查的文档：根目录里 kinds 这几种文件及其译本（含后缀写错了的），加上 docs/ 下的全部文件。
// 根目录别的 Markdown（CHANGELOG.md……）不归文档规范管。
func collect(root string, kinds []string) docSet {
	var s docSet
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		for _, k := range kinds {
			if e.Name() == k || strings.HasPrefix(e.Name(), strings.TrimSuffix(k, ".md")+".") {
				s.files = append(s.files, suffixed(e.Name()))
				break
			}
		}
	}

	s.mainTree, s.trees = languageTrees(root)
	tree := docsTree(root)
	// docs/ 里名字带点、却不是合法译本的文件，只在同目录里有别的译本时才算写错了后缀（v1.2-notes.md 这种名字本身就带点）
	translatedDirs := map[string]bool{}
	for _, rel := range tree {
		if _, lang, ok := docspec.SplitTranslation(path.Base(rel)); ok && lang != "" {
			translatedDirs[path.Dir(rel)] = true
		}
	}
	for _, rel := range tree {
		if f, ok := s.inTree(rel); ok {
			s.files = append(s.files, f)
			continue
		}
		f := suffixed(rel)
		if f.primary != "" || translatedDirs[path.Dir(rel)] {
			if f.primary == "" {
				f = file{rel: rel} // 写错了后缀：照样查，再点出来
			}
			s.files = append(s.files, f)
			continue
		}
		s.files = append(s.files, file{rel: rel, primary: rel})
	}
	sort.Slice(s.files, func(i, j int) bool { return s.files[i].rel < s.files[j].rel })
	return s
}

// suffixed 按同目录后缀的放法认 rel：README.zh.md → 原文 README.md、zh。
func suffixed(rel string) file {
	base, lang, ok := docspec.SplitTranslation(path.Base(rel))
	if !ok {
		return file{rel: rel}
	}
	return file{rel: rel, primary: path.Join(path.Dir(rel), base), lang: lang}
}

// inTree 按语言树认 rel：docs/zh/a/b.md → 原文 docs/<主语言>/a/b.md、zh。不在任何一棵语言树里时 ok 为 false。
func (s docSet) inTree(rel string) (file, bool) {
	if s.mainTree == "" {
		return file{}, false
	}
	rest, ok := strings.CutPrefix(rel, docspec.DirDocs+"/")
	if !ok {
		return file{}, false
	}
	lang, sub, ok := strings.Cut(rest, "/")
	if !ok {
		return file{}, false
	}
	primary := path.Join(docspec.DirDocs, s.mainTree, sub)
	switch {
	case lang == s.mainTree:
		return file{rel: rel, primary: rel}, true
	case slices.Contains(s.trees, lang):
		return file{rel: rel, primary: primary, lang: lang}, true
	}
	return file{}, false
}

// languageTrees 是 docs/ 下的语言树：主语言那棵与其余各棵（排好序）。不到两棵、或主语言那棵不在时都为空。
func languageTrees(root string) (string, []string) {
	main := primaryLang(root)
	entries, _ := os.ReadDir(filepath.Join(root, docspec.DirDocs))
	var others []string
	found := false
	for _, e := range entries {
		switch {
		case !e.IsDir() || !docspec.ValidLang(e.Name()):
		case e.Name() == main:
			found = true
		default:
			others = append(others, e.Name())
		}
	}
	if !found || len(others) == 0 {
		return "", nil
	}
	return main, others
}

// primaryLang 是 root 的主语言：AGENTS.md 维护段记下的 lang=，没有维护段就是 en。
func primaryLang(root string) string {
	if d, ok := read(root, docspec.FileAgents); ok {
		if b, err := agentsmd.Find(d.body); err == nil {
			return b.Lang
		}
	}
	return "en"
}

// translations 查每组语言版本：译本有原文、二级小节数与原文一样，语言树里原文在每棵译本树都有，
// 有译本的文件开头链到这组里的每个其他版本（BRICKKIT*.md 除外：它根本不放相对链接）。
// 小节都不算 brickkit 维护段：维护段只在原文 AGENTS.md 里，译本没有它。
func (s docSet) translations(root string) []*clierr.Error {
	var out []*clierr.Error
	groups := map[string][]string{} // 原文 → 这组里实际存在的文件（原文在前）
	var order []string
	for _, f := range s.files {
		if f.primary == "" {
			continue
		}
		if _, seen := groups[f.primary]; !seen {
			order = append(order, f.primary)
		}
		groups[f.primary] = append(groups[f.primary], f.rel)
		if f.lang == "" {
			continue
		}
		d, _ := read(root, f.rel)
		p, ok := read(root, f.primary)
		if !ok {
			out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNoPrimary, f.primary), f.rel, 0))
			continue
		}
		if a, b := countSections(p), countSections(d); a != b {
			out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckSectionCount, b, a, f.primary), f.rel, 0))
		}
	}
	for _, primary := range order {
		members := groups[primary]
		// 语言树里的原文要在每棵译本树里都有：选了语言树，就是整棵文档都双语
		if members[0] == primary && s.isMainTree(primary) {
			for _, lang := range s.trees {
				counterpart := path.Join(docspec.DirDocs, lang, strings.TrimPrefix(primary, docspec.DirDocs+"/"+s.mainTree+"/"))
				if !slices.Contains(members, counterpart) {
					out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNoCounterpart, lang, counterpart), primary, 0))
				}
			}
		}
		if len(members) < 2 || path.Base(primary) == docspec.FileBrickkit {
			continue
		}
		for _, rel := range members {
			d, ok := read(root, rel)
			if !ok {
				continue
			}
			for _, other := range members {
				if other != rel && !linksTo(d, other) {
					out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNotLinked, rel, other), rel, 0))
				}
			}
		}
	}
	return out
}

func (s docSet) isMainTree(rel string) bool {
	return s.mainTree != "" && strings.HasPrefix(rel, docspec.DirDocs+"/"+s.mainTree+"/")
}

// countSections 是 d 里作者自己写的二级小节数（不含维护段里的）。
func countSections(d doc) int { return len(mdtext.Sections(withoutBlock(d).body)) }

// linksTo 判断 d 里有没有一条相对链接指向 rel（rel 与链接都相对检查根目录解析）。
func linksTo(d doc, rel string) bool {
	for _, l := range mdtext.Links(d.body) {
		if !isRelative(l.Target) {
			continue
		}
		file, _, _ := strings.Cut(l.Target, "#")
		if file != "" && path.Clean(path.Join(path.Dir(d.rel), file)) == rel {
			return true
		}
	}
	return false
}
