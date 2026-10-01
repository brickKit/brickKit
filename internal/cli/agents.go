package cli

// 本文件说清 AGENTS.md / CLAUDE.md 这次怎么了：init、skills update 共用。

import (
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
	if res.Problem != "" {
		opts.Printf("%s", opts.render(clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliAgentsBlockMissing, docspec.FileAgents)).
			WithDetail(i18n.T(msgid.LabelReason), res.Problem).
			WithHint(i18n.T(msgid.CliAgentsHintSkillsUpdate))))
	}
	if res.ClaudeMissingImport {
		opts.Printf("%s", opts.render(clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliAgentsClaudeMissingImport, docspec.FileClaude, docspec.ClaudeImport)).
			WithHint(i18n.T(msgid.CliAgentsHintSkillsUpdate))))
	}
}

// renderProjectMapObsolete 说项目根还留着旧版的项目地图 BRICKKIT.md：组件表现在在 AGENTS.md 末尾。
// 从不替人删——那份文件里可能有他自己写的内容。
func renderProjectMapObsolete(opts *Options) {
	opts.Printf("%s", opts.render(clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliInitProjectMapObsolete, docspec.FileBrickkit, docspec.FileAgents)).
		WithHint(i18n.T(msgid.CliInitHintMoveProjectMap, docspec.FileAgents, docspec.FileBrickkit))))
}
