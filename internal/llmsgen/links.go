package llmsgen

import (
	"path"
	"regexp"
	"strings"
)

var (
	mdLink     = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	inlineCode = regexp.MustCompile("`[^`]*`")
)

// fence 是 Markdown 代码块的开头与结尾（三个反引号）。
const fence = "\x60\x60\x60"

// RewriteLinks 把 pagePath（仓库相对路径）里的相对链接改写成相对 bundleDir 的写法。
// 外部链接、页内锚点、代码块与行内代码里的内容不动。
func RewriteLinks(body, pagePath, bundleDir string) string {
	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), fence) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = rewriteLine(line, pagePath, bundleDir)
	}
	return strings.Join(lines, "\n")
}

func rewriteLine(line, pagePath, bundleDir string) string {
	codes := inlineCode.FindAllStringIndex(line, -1)
	inCode := func(at int) bool {
		for _, c := range codes {
			if at >= c[0] && at < c[1] {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	last := 0
	for _, m := range mdLink.FindAllStringSubmatchIndex(line, -1) {
		if inCode(m[0]) {
			continue
		}
		target := line[m[4]:m[5]]
		b.WriteString(line[last:m[4]])
		b.WriteString(rewriteTarget(target, pagePath, bundleDir))
		last = m[5]
	}
	b.WriteString(line[last:])
	return b.String()
}

func rewriteTarget(target, pagePath, bundleDir string) string {
	if target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") ||
		strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return target
	}
	file, frag, hasFrag := strings.Cut(target, "#")
	repoPath := path.Clean(path.Join(path.Dir(pagePath), file))
	rel := relative(bundleDir, repoPath)
	if hasFrag {
		return rel + "#" + frag
	}
	return rel
}

// relative 是从 fromDir 到 to 的相对路径（两者都是仓库相对、/ 分隔）。
func relative(fromDir, to string) string {
	from := strings.Split(path.Clean(fromDir), "/")
	dst := strings.Split(to, "/")
	i := 0
	for i < len(from) && i < len(dst)-1 && from[i] == dst[i] {
		i++
	}
	parts := make([]string, 0, len(from)-i+len(dst)-i)
	for range from[i:] {
		parts = append(parts, "..")
	}
	parts = append(parts, dst[i:]...)
	return strings.Join(parts, "/")
}
