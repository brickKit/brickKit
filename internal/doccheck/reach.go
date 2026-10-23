package doccheck

// 本文件查 docs/ 里有没有走不到的文件：AI 从 AGENTS.md 的路由与代码地图往下找，没被引用到的文件它不会知道存在。

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/mdtext"
	"github.com/brickkit/brickkit/internal/msgid"
)

// unreachable 报 docs/ 下从 entries（根目录的 AGENTS.md、README.md 与它们的译本，各份局部的 AGENTS.md）走不到的文件。
//
// 一份 Markdown 引用别的文件有两种写法，都算：相对链接；行内代码里写出的路径（相对它自己所在的目录，或相对 root），
// 只要指向一个真实存在的文件或目录。引用一个目录就到达它下面的全部文件（查找路由常写 `docs/decisions/`，AI 会去列那个目录）。
// 到达的 Markdown 再顺着它的引用往下走。译本按原文算：到达任何一个语言版本，这一份就到达了；没到达时只报原文一次。
func unreachable(root string, set docSet, entries []string) []*clierr.Error {
	primaryOf := map[string]string{} // docs/ 下的文件 → 它的原文
	var inDocs []string
	for _, f := range set.files {
		if strings.HasPrefix(f.rel, docspec.DirDocs+"/") && f.primary != "" {
			primaryOf[f.rel] = f.primary
			inDocs = append(inDocs, f.rel)
		}
	}
	reached := map[string]bool{} // 原文
	visited := map[string]bool{}
	found := map[string]kind{} // 路径是什么：同一个路径在几十份文档里被写到，只 stat 一次
	queue := append([]string(nil), entries...)
	reach := func(rel string) {
		if p, ok := primaryOf[rel]; ok {
			reached[p] = true
		}
		if strings.HasSuffix(rel, ".md") && !visited[rel] {
			queue = append(queue, rel)
		}
	}
	for len(queue) > 0 {
		rel := queue[0]
		queue = queue[1:]
		if visited[rel] {
			continue
		}
		visited[rel] = true
		d, ok := read(root, rel)
		if !ok {
			continue
		}
		for _, target := range references(root, withoutBlock(d), found) {
			if found[target] == kindDir {
				for _, f := range inDocs {
					if strings.HasPrefix(f, target+"/") {
						reach(f)
					}
				}
				continue
			}
			reach(target)
		}
	}
	var out []*clierr.Error
	for _, f := range set.files {
		if p, ok := primaryOf[f.rel]; ok && p == f.rel && !reached[p] {
			out = append(out, warn(clierr.CodeDocUnreachable, i18n.T(msgid.DoccheckUnreachable, f.rel), f.rel, 0).
				WithHint(i18n.T(msgid.DoccheckHintUnreachable, docspec.FileAgents)))
		}
	}
	return out
}

// kind 是 references 查到的一个路径：不存在、文件、目录。
type kind int

const (
	kindMissing kind = iota + 1
	kindFile
	kindDir
)

// references 是 d 引用到的、root 里真实存在的文件与目录（相对 root、斜杠分隔；root 本身不算）。found 缓存每个路径查到的 kind。
func references(root string, d doc, found map[string]kind) []string {
	var out []string
	add := func(rel string) {
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			return // root 本身，或跑出了 root
		}
		k, seen := found[rel]
		if !seen {
			k = kindMissing
			if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
				k = kindFile
				if info.IsDir() {
					k = kindDir
				}
			}
			found[rel] = k
		}
		if k != kindMissing {
			out = append(out, rel)
		}
	}
	base := path.Dir(d.rel)
	for _, l := range mdtext.Links(d.body) {
		if !isRelative(l.Target) {
			continue
		}
		if file, _, _ := strings.Cut(l.Target, "#"); file != "" && !strings.HasPrefix(file, "/") {
			add(path.Clean(path.Join(base, file)))
		}
	}
	for _, c := range mdtext.CodeSpans(d.body) {
		p := strings.TrimSuffix(c.Target, "/")
		// 能走到的只有目录与 Markdown：既不含 / 也不以 .md 结尾的（函数名、命令、`main.go`）不用查。
		// 路由（`/healthz`）、带空白或通配的都不是路径；`.` 指的是构建上下文这类东西，不是"整个仓库都引用了"
		if p == "" || p == "." || p == ".." || (!strings.Contains(c.Target, "/") && !strings.HasSuffix(p, ".md")) ||
			strings.HasPrefix(p, "/") || strings.ContainsAny(p, " \t*") || strings.Contains(p, "://") {
			continue
		}
		add(path.Clean(path.Join(base, p)))
		if base != "." {
			add(path.Clean(p))
		}
	}
	return out
}

// entryDocs 是 unreachable 的起点：根目录的 AGENTS.md、README.md 及它们的译本（在 set 里），加上 locals。
func entryDocs(set docSet, locals []string) []string {
	var out []string
	for _, f := range set.files {
		if f.primary == docspec.FileAgents || f.primary == docspec.FileReadme {
			out = append(out, f.rel)
		}
	}
	return append(out, locals...)
}
