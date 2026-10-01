package agentsmd

import (
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
)

// ClaudeContent 是 CLAUDE.md 的全部内容。
const ClaudeContent = docspec.ClaudeImport + "\n"

// Skeleton 是一份新的 AGENTS.md：标题、一句指路、作者要写的各节（带 TODO 提示）、末尾的维护区。
// 组件仓库是组件那五节，否则是项目那四节。整份都用 c.Lang 写（templates/<lang>/skeleton-*.md），
// 与维护区同一种语言——旧文件被替换时，c.Lang 可能不是此刻的 CLI 语言。
func Skeleton(title string, c Content) string {
	name := "skeleton-project.md"
	if c.Component {
		name = "skeleton-component.md"
	}
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	b.WriteString(tmpl(c.Lang, name))
	b.WriteString(Render(c))
	return b.String()
}
