package manifest

// 本文件是 brickkit new 写的四份文档：BRICKKIT.md（随版本给使用方）、AGENTS.md + CLAUDE.md（给开发它的 AI）、
// README.md（给 GitHub 上的人）。标题取自 docspec，语言是此刻的 CLI 语言；要作者填的地方写成
// <!-- TODO: … --> 注释，brickkit lint 会一条条点出来，直到填完。

import (
	"strings"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

func todo(id msgid.ID, args ...any) string { return "<!-- TODO: " + i18n.T(id, args...) + " -->" }

func docHeading(s docspec.Section) string {
	return "## " + docspec.Heading(s, string(i18n.Current())) + "\n\n"
}

// brickkitDoc 是 BRICKKIT.md 的骨架：六节。不放相对链接——它在使用方项目里是脱离仓库单独读的。
func brickkitDoc(id, contractPath string, shell bool) string {
	var b strings.Builder
	b.WriteString("# " + id + "\n\n")
	b.WriteString(docHeading(docspec.Purpose) + todo(msgid.ManifestDocHintPurpose) + "\n\n")
	b.WriteString(docHeading(docspec.BeforeDeploy) + todo(msgid.ManifestDocHintBeforeDeploy) + "\n\n")
	b.WriteString(docHeading(docspec.Dependencies) + todo(msgid.ManifestDocHintDependencies) + "\n\n")
	b.WriteString(docHeading(docspec.Configuration))
	// 两列：键与它的含义。类型、默认值、是否必填是 component.yaml 的事实，这里再写一遍就是两处要同步（lint 只查必填键被提到）
	b.WriteString("| " + i18n.T(msgid.ManifestDocColVariable) + " | " + i18n.T(msgid.ManifestDocColMeaning) + " |\n|---|---|\n")
	b.WriteString("| " + todo(msgid.ManifestDocConfigKeyTodo) + " | " + todo(msgid.ManifestDocConfigMeaningTodo) + " |\n\n")
	b.WriteString(docHeading(docspec.Contracts))
	if contractPath != "" {
		b.WriteString("- `" + contractPath + "`: " + todo(msgid.ManifestDocHintContractFile) + "\n\n")
	} else {
		b.WriteString(todo(msgid.ManifestDocHintContracts) + "\n\n")
	}
	b.WriteString(docHeading(docspec.ShellDecl))
	if shell {
		b.WriteString(i18n.T(msgid.ManifestDocShellMembers) + "\n\n- `" + scaffoldPlaceholderMember + "` " + todo(msgid.ManifestDocShellMembersTodo) + "\n")
	} else {
		b.WriteString(i18n.T(msgid.ManifestDocNotShell) + "\n")
	}
	return b.String()
}

// readmeDoc 是 README.md 的骨架：三节，主要用来指路。
func readmeDoc(id, contractPath string) string {
	fence := strings.Repeat("`", 3)
	link := func(path string) string { return "[" + path + "](" + path + ")" }
	var b strings.Builder
	b.WriteString("# " + id + "\n\n" + todo(msgid.ManifestReadmeHintOneLine) + "\n\n")
	b.WriteString(docHeading(docspec.UseIt))
	b.WriteString(fence + "bash\nbrickkit add " + id + "@0.1.0\nbrickkit up\n" + fence + "\n\n")
	b.WriteString(i18n.T(msgid.ManifestReadmePrepareFirst, docspec.FileBrickkit) + "\n\n")
	b.WriteString(docHeading(docspec.Documentation))
	b.WriteString("| " + i18n.T(msgid.ManifestReadmeColQuestion) + " | " + i18n.T(msgid.ManifestReadmeColRead) + " |\n|---|---|\n")
	b.WriteString("| " + i18n.T(msgid.ManifestReadmeRowUsage) + " | " + link(docspec.FileBrickkit) + " |\n")
	if contractPath != "" {
		b.WriteString("| " + i18n.T(msgid.ManifestReadmeRowContracts) + " | " + link(contractPath) + " |\n")
	}
	b.WriteString("| " + i18n.T(msgid.ManifestReadmeRowDependencies, docspec.FileBrickkit) + " | " + link(FileName) + " |\n")
	b.WriteString("| " + i18n.T(msgid.ManifestReadmeRowDevelop) + " | " + link(docspec.FileAgents) + " |\n\n")
	b.WriteString(docHeading(docspec.Development) + i18n.T(msgid.ManifestReadmeDevelopment, docspec.FileAgents) + "\n")
	return b.String()
}

// docFiles 是 new 写的四份文档，按这个顺序。
func docFiles(id, contractPath string, shell bool) []ScaffoldFile {
	c := agentsmd.Content{Lang: string(i18n.Current()), Component: true, Title: id}
	return []ScaffoldFile{
		{Path: docspec.FileBrickkit, Content: []byte(brickkitDoc(id, contractPath, shell))},
		{Path: docspec.FileAgents, Content: []byte(agentsmd.Skeleton(id, c))},
		{Path: docspec.FileClaude, Content: []byte(agentsmd.ClaudeContent)},
		{Path: docspec.FileReadme, Content: []byte(readmeDoc(id, contractPath))},
	}
}
