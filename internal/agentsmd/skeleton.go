package agentsmd

import (
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// ClaudeContent 是 CLAUDE.md 的全部内容。
const ClaudeContent = docspec.ClaudeImport + "\n"

// hints 是骨架里每节的填写提示（CLI 当前语言：骨架是"现在生成一份"，语言就是此刻的语言）。
var hints = map[docspec.Section]msgid.ID{
	docspec.CodeMap:        msgid.AgentsmdHintCodeMap,
	docspec.BuildTest:      msgid.AgentsmdHintBuildTest,
	docspec.Decisions:      msgid.AgentsmdHintDecisions,
	docspec.Pitfalls:       msgid.AgentsmdHintPitfalls,
	docspec.BeforeChanging: msgid.AgentsmdHintBeforeChanging,
	docspec.Overview:       msgid.AgentsmdHintOverview,
	docspec.Conventions:    msgid.AgentsmdHintConventions,
	docspec.WhereToLook:    msgid.AgentsmdHintWhereToLook,
}

// Skeleton 是一份新的 AGENTS.md：标题、一句指路、作者要写的各节（带 TODO 提示）、末尾的维护区。
// 组件仓库是组件那五节，否则是项目那四节；标题用 c.Lang 的说法。
func Skeleton(title string, c Content) string {
	kind, intro := docspec.KindProjectAgents, msgid.AgentsmdIntroProject
	if c.Component {
		kind, intro = docspec.KindComponentAgents, msgid.AgentsmdIntroComponent
	}
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	b.WriteString(i18n.T(intro) + "\n\n")
	for _, s := range docspec.Required(kind) {
		b.WriteString("## " + docspec.Heading(s, c.Lang) + "\n\n")
		hint := hints[s]
		if s == docspec.Pitfalls && !c.Component {
			hint = msgid.AgentsmdHintProjectPitfalls
		}
		b.WriteString("<!-- TODO: " + i18n.T(hint) + " -->\n\n")
	}
	b.WriteString(Render(c))
	return b.String()
}
