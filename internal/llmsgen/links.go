package llmsgen

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// 链接目标出现的三种位置：[文字](目标 "可选标题")、行首的引用式定义 [名]: 目标、HTML 的 href / src。
// 每个正则的第 2 个分组是目标。
var (
	linkForms = []*regexp.Regexp{
		regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`),
		regexp.MustCompile(`^(\s*\[[^\]]+\]:\s*)(\S+)`),
		regexp.MustCompile(`\b(href|src)="([^"]+)"`),
	}
	inlineCode = regexp.MustCompile("`[^`]*`")
)

// fences 是 Markdown 代码块的两种围栏（三个反引号、三个波浪号）。
var fences = []string{"\x60\x60\x60", "~~~"}

// fenceOf 是这一行开头的围栏；不是围栏时为空。
func fenceOf(line string) string {
	t := strings.TrimSpace(line)
	for _, f := range fences {
		if strings.HasPrefix(t, f) {
			return f
		}
	}
	return ""
}

// RewriteLinks 把 pagePath（仓库相对路径）里的相对链接改写成相对 bundleDir 的写法。
// 外部链接、页内锚点、代码块与行内代码里的内容不动。
func RewriteLinks(body, pagePath, bundleDir string) string {
	return mapLinks(body, func(target string) string { return rewriteTarget(target, pagePath, bundleDir) })
}

// mapLinks 对代码块与行内代码之外的每个链接目标调用 fn，用它的返回值替换目标。
func mapLinks(body string, fn func(target string) string) string {
	lines := strings.Split(body, "\n")
	open := "" // 正在其中的代码块的围栏：只有同一种围栏才能把它关上
	for i, line := range lines {
		if f := fenceOf(line); f != "" && (open == "" || f == open) {
			if open == "" {
				open = f
			} else {
				open = ""
			}
			continue
		}
		if open != "" {
			continue
		}
		lines[i] = mapLine(line, fn)
	}
	return strings.Join(lines, "\n")
}

func mapLine(line string, fn func(string) string) string {
	codes := inlineCode.FindAllStringIndex(line, -1)
	inCode := func(at int) bool {
		for _, c := range codes {
			if at >= c[0] && at < c[1] {
				return true
			}
		}
		return false
	}
	var spans [][2]int
	for _, re := range linkForms {
		for _, m := range re.FindAllStringSubmatchIndex(line, -1) {
			if !inCode(m[0]) {
				spans = append(spans, [2]int{m[4], m[5]})
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		if sp[0] < last { // 两种写法重叠（不会发生，保险起见只认先出现的那个）
			continue
		}
		b.WriteString(line[last:sp[0]])
		b.WriteString(fn(line[sp[0]:sp[1]]))
		last = sp[1]
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
