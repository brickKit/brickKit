package cli

// 本文件说清 AGENTS.md / CLAUDE.md 这次怎么了：init、skills update 共用。

import (
	"errors"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// renderAgentsResult 说清 AGENTS.md / CLAUDE.md 这次怎么了：换了旧版、追加了、或者因为是作者的文件而没动。
// 新建的文件已经列在计划或创建清单里，这里不重复。
func renderAgentsResult(opts *Options, res agentsmd.Result) {
	if res.LegacyReplaced {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliAgentsLegacyReplaced, docspec.FileAgents))
	}
	if res.BlockAppended {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliAgentsBlockAppended, docspec.FileAgents))
	}
	if res.ClaudeAppended {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliAgentsClaudeAppended, docspec.ClaudeImport, docspec.FileClaude))
	}
	if res.Problem != nil {
		opts.Printf("%s", opts.render(agentsBlockWarning(res.Problem)))
	}
	if res.AgentsMissing {
		opts.Printf("%s", opts.render(clierr.Warn(clierr.CodeAgentsBlockMissing, i18n.T(msgid.CliAgentsMissing, docspec.FileAgents, docspec.FileComponents)).
			WithHint(i18n.T(msgid.CliAgentsHintSkillsUpdate))))
	}
	if res.ClaudeMissingImport {
		opts.Printf("%s", opts.render(clierr.Warn(clierr.CodeClaudeImportMissing, i18n.T(msgid.CliAgentsClaudeMissingImport, docspec.FileClaude, docspec.ClaudeImport)).
			WithHint(i18n.T(msgid.CliAgentsHintSkillsUpdate))))
	}
}

// agentsBlockWarning 是"AGENTS.md 里没有可用的维护段"：原因跟着语言说；标记坏了时建议改标记
// （skills update 修不了坏掉的标记），没有维护段时建议 skills update 追加。
func agentsBlockWarning(problem error) *clierr.Error {
	hint := i18n.T(msgid.CliAgentsHintSkillsUpdate)
	if errors.Is(problem, agentsmd.ErrMalformed) {
		hint = i18n.T(msgid.CliAgentsHintFixMarkers)
	}
	return clierr.Warn(clierr.CodeAgentsBlockMissing, i18n.T(msgid.CliAgentsBlockMissing, docspec.FileAgents, docspec.FileComponents)).
		WithDetail(i18n.T(msgid.LabelReason), agentsmd.Reason(problem)).
		WithHint(hint)
}

// renderComponentsCreated 说这次建了 COMPONENTS.md：add / remove / upgrade、skills update 在它不在时建它
// （CLI 升级后的第一次就是这样从 AGENTS.md 里搬出来的）。init 的创建清单里已经有它，不用这一句。
func renderComponentsCreated(opts *Options, res agentsmd.Result) {
	if res.ComponentsCreated {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliAgentsComponentsCreated, docspec.FileComponents, docspec.FileAgents))
	}
}

// renderProjectMapObsolete 说项目根还留着旧版的项目地图 BRICKKIT.md：组件表现在在 COMPONENTS.md。
// 从不替人删——那份文件里可能有他自己写的内容。
func renderProjectMapObsolete(opts *Options) {
	opts.Printf("%s", opts.render(clierr.Warn(clierr.CodeProjectMapObsolete, i18n.T(msgid.CliInitProjectMapObsolete, docspec.FileBrickkit, docspec.FileComponents)).
		WithHint(i18n.T(msgid.CliInitHintMoveProjectMap, docspec.FileAgents, docspec.FileBrickkit))))
}
