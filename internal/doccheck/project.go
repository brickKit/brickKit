package doccheck

import (
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Project 检查项目根的 AGENTS.md（四节、维护区、链接、占位）、CLAUDE.md、README.md 的链接，以及有没有旧版的项目地图。
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
	if d, ok := read(root, docspec.FileAgents); ok {
		out = append(out, sections(d, docspec.KindProjectAgents)...)
		mine := withoutBlock(d)
		out = append(out, links(root, mine, false)...)
		out = append(out, placeholders(mine)...)
		out = append(out, block(d)...)
	}
	out = append(out, claude(root)...)
	if d, ok := read(root, docspec.FileReadme); ok {
		out = append(out, links(root, d, false)...)
	}
	if d, ok := read(root, docspec.FileBrickkit); ok && strings.Contains(d.body, "<!-- brickkit:managed:begin") {
		out = append(out, warn(clierr.CodeProjectMapObsolete, i18n.T(msgid.CliInitProjectMapObsolete, docspec.FileBrickkit, docspec.FileAgents), d.rel, 0).
			WithHint(i18n.T(msgid.CliInitHintMoveProjectMap, docspec.FileAgents, docspec.FileBrickkit)))
	}
	return out
}
