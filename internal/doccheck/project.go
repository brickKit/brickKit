package doccheck

import (
	"path"
	"path/filepath"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Project 检查项目的文档：根目录的 AGENTS.md（四节、维护区、链接、占位）、CLAUDE.md、README.md，
// 它们的译本，docs/ 下的全部文件（链接、占位、译本对不对得上），以及有没有旧版的项目地图。
// 项目文档的链接可以指到项目里任何地方（components/ 下的组件也是项目的一部分），只要目标在。
// 工作台（根目录也有 component.yaml）的这些文件同时是组件的文档，由 Component 查，这里什么都不报。
func Project(root string) []*clierr.Error {
	if exists(filepath.Join(root, manifest.FileName)) {
		return nil
	}
	var out []*clierr.Error
	for _, f := range []string{docspec.FileAgents, docspec.FileClaude} {
		if !exists(filepath.Join(root, f)) {
			out = append(out, warn(clierr.CodeDocFileMissing, i18n.T(msgid.DoccheckFileMissing, f), f, 0))
		}
	}
	out = append(out, claude(root)...)
	set := collect(root, projectTranslatedKinds)
	for _, f := range set.files {
		d, _ := read(root, f.rel)
		if f.primary == "" {
			out = append(out, warn(clierr.CodeDocTranslationDrift, i18n.T(msgid.DoccheckNotALanguage, path.Base(f.rel)), f.rel, 0))
			continue
		}
		mine := d
		if f.primary == docspec.FileAgents {
			mine = withoutBlock(d)
			if f.lang == "" {
				out = append(out, sections(d, docspec.KindProjectAgents)...)
				out = append(out, block(d)...)
			}
		}
		out = append(out, links(root, mine, false)...)
		out = append(out, placeholders(mine)...)
	}
	out = append(out, set.translations(root)...)
	if d, ok := read(root, docspec.FileBrickkit); ok && agentsmd.IsOldProjectMap([]byte(d.body)) {
		out = append(out, warn(clierr.CodeProjectMapObsolete, i18n.T(msgid.CliInitProjectMapObsolete, docspec.FileBrickkit, docspec.FileAgents), d.rel, 0).
			WithHint(i18n.T(msgid.CliInitHintMoveProjectMap, docspec.FileAgents, docspec.FileBrickkit)))
	}
	return out
}

// projectTranslatedKinds 是项目根目录下会有译本的文档（项目没有 BRICKKIT.md；CLAUDE.md 只有一行，不翻译）。
var projectTranslatedKinds = []string{docspec.FileAgents, docspec.FileReadme}
