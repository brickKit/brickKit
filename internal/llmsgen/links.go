package llmsgen

import (
	"path"
	"strings"

	"github.com/brickkit/brickkit/internal/mdtext"
)

// RewriteLinks 把 pagePath（仓库相对路径）里的相对链接改写成相对 bundleDir 的写法。
// 外部链接、页内锚点、代码块与行内代码里的内容不动。
func RewriteLinks(body, pagePath, bundleDir string) string {
	return mdtext.MapLinks(body, func(target string) string { return rewriteTarget(target, pagePath, bundleDir) })
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
