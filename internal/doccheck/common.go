// Package doccheck 是 brickkit lint 的文档检查：只查程序能确定的事——文件在不在、小节齐不齐、
// 路径与链接在不在、component.yaml 的事实有没有提、占位词、译本对不对得上。写得好不好是评审的事。
// 全部是警告：从不拦下 up 与 release，只有 lint --strict 把它们算成失败。
package doccheck

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/agentsmd"
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

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// sections 检查 kind 的每个固定小节都在（任何认得的语言的标题都算）。
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

// section 是 d 里匹配 s 的那一节的正文与标题所在行；没有这一节时行号为 0。
func section(d doc, s docspec.Section) (string, int) {
	for _, h := range mdtext.Sections(d.body) {
		if docspec.Matches(s, h.Heading) {
			return h.Body, h.Line
		}
	}
	return "", 0
}

// placeholders 查代码之外的占位词，每行最多报一次。
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

// isRelative：不是外部地址、页内锚点、mailto 的链接目标。
func isRelative(target string) bool {
	return target != "" && !strings.HasPrefix(target, "#") && !strings.Contains(target, "://") && !strings.HasPrefix(target, "mailto:")
}

// links 检查相对链接：目标要存在；portable 为真（组件里）时不能跑出根目录。
func links(root string, d doc, portable bool) []*clierr.Error {
	var out []*clierr.Error
	for _, l := range mdtext.Links(d.body) {
		if !isRelative(l.Target) {
			continue
		}
		file, _, _ := strings.Cut(l.Target, "#")
		if file == "" {
			continue
		}
		rel := path.Clean(path.Join(path.Dir(d.rel), file))
		if portable && (rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(file, "/")) {
			out = append(out, warn(clierr.CodeDocLinkNotPortable, i18n.T(msgid.DoccheckLinkLeaves, l.Target), d.rel, l.Line))
			continue
		}
		if !exists(filepath.Join(root, filepath.FromSlash(rel))) {
			out = append(out, warn(clierr.CodeDocLinkBroken, i18n.T(msgid.DoccheckLinkBroken, l.Target), d.rel, l.Line))
		}
	}
	return out
}

// claude 查 CLAUDE.md 有没有 @AGENTS.md。
func claude(root string) []*clierr.Error {
	d, ok := read(root, docspec.FileClaude)
	if !ok || agentsmd.HasClaudeImport(d.body) {
		return nil
	}
	return []*clierr.Error{warn(clierr.CodeClaudeImportMissing, i18n.T(msgid.DoccheckClaudeImport, docspec.ClaudeImport), d.rel, 0)}
}

// block 查 AGENTS.md 末尾的维护区。
func block(d doc) []*clierr.Error {
	_, err := agentsmd.Find(d.body)
	if err == nil {
		return nil
	}
	hint := i18n.T(msgid.CliAgentsHintSkillsUpdate)
	if errors.Is(err, agentsmd.ErrMalformed) {
		hint = i18n.T(msgid.CliAgentsHintFixMarkers)
	}
	return []*clierr.Error{warn(clierr.CodeAgentsBlockMissing, i18n.T(msgid.DoccheckAgentsBlock, agentsmd.Reason(err)), d.rel, 0).WithHint(hint)}
}

// withoutBlock 是 d 去掉 brickkit 维护段之后、作者自己写的那部分：维护段的每一行换成空行（行号照旧）。
// 维护段里的内容来自别的组件（它们的 metadata.description、平台的模板），项目改不了，占位词与链接不该在那里查。
func withoutBlock(d doc) doc {
	b, err := agentsmd.Find(d.body)
	if err != nil {
		return d
	}
	blank := strings.Repeat("\n", strings.Count(d.body[b.Start:b.End], "\n"))
	return doc{rel: d.rel, body: d.body[:b.Start] + blank + d.body[b.End:]}
}

// docsTree 是 root/docs 下的全部 Markdown（斜杠分隔、排好序）。
func docsTree(root string) []string {
	var out []string
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
