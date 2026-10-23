package doccheck

// 本文件是局部的 AGENTS.md：组件或项目某个子目录里自己的那份说明（上层没有的约定、这个目录的小地图）。
// Codex、Claude Code 在那个目录里干活时会读到它（Claude Code 经由旁边那份 @AGENTS.md 的 CLAUDE.md），
// 它的链接与代码地图和根目录那份一样会过时，所以照样查；它没有固定小节，也没有维护区。

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// localAgents 是 root 下各子目录里的 AGENTS.md（相对 root、斜杠分隔、排好序）。
// 在 Git 工作区里按 Git 的看法列：被忽略的（依赖目录、生成物）与嵌套的另一个仓库都不算；不在 Git 里时走目录树，跳过隐藏目录。
// 不进 docs/（那里的文件按文档查），也不进放着 component.yaml 的子目录（那是另一个组件，归它自己查）。
func localAgents(root string) []string {
	candidates, err := gitrepo.ListFiles(root, ":(glob)**/"+docspec.FileAgents)
	if errors.Is(err, gitrepo.ErrNotRepo) {
		candidates = walkAgents(root)
	}
	var out []string
	for _, rel := range candidates {
		rel = path.Clean(rel)
		dir := path.Dir(rel)
		if path.Base(rel) != docspec.FileAgents || dir == "." || !exists(filepath.Join(root, filepath.FromSlash(rel))) {
			continue
		}
		if first, _, _ := strings.Cut(rel, "/"); first == docspec.DirDocs || insideComponent(root, dir) {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// walkAgents 是不在 Git 里时的 localAgents 候选：目录树里所有 AGENTS.md，隐藏目录不进。
func walkAgents(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case e.IsDir() && p != root && strings.HasPrefix(e.Name(), "."):
			return filepath.SkipDir
		case !e.IsDir() && e.Name() == docspec.FileAgents:
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// insideComponent 判断 root 下的目录 dir（斜杠分隔）或它与 root 之间的某一层放着 component.yaml。
func insideComponent(root, dir string) bool {
	for ; dir != "." && dir != "/"; dir = path.Dir(dir) {
		if exists(filepath.Join(root, filepath.FromSlash(dir), manifest.FileName)) {
			return true
		}
	}
	return false
}

// local 查一份局部的 AGENTS.md：链接（portable 时不能跑出 root）、占位词、代码地图（路径相对它所在的目录），
// 以及旁边的 CLAUDE.md——没有它，Claude Code 在那个目录里读不到这份说明。
func local(root, rel string, portable bool) []*clierr.Error {
	d, ok := read(root, rel)
	if !ok {
		return nil
	}
	out := links(root, d, portable)
	out = append(out, placeholders(d)...)
	out = append(out, codeMap(filepath.Join(root, filepath.FromSlash(path.Dir(rel))), d)...)
	claudeRel := path.Join(path.Dir(rel), docspec.FileClaude)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(claudeRel))); errors.Is(err, fs.ErrNotExist) {
		return append(out, warn(clierr.CodeDocFileMissing, i18n.T(msgid.DoccheckLocalClaudeMissing, claudeRel, docspec.ClaudeImport), claudeRel, 0))
	}
	return append(out, claude(root, claudeRel)...)
}
