package agentsmd

import (
	"embed"
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
)

//go:embed templates
var templates embed.FS

// Row 是组件表的一行。单元格都是写进 Git 的事实：只来自 brickkit.yaml 和那个版本自己，
// 不来自"这台机器上有没有本地源码、是哪个安装源给的"——那会让同事之间来回改写。
type Row struct {
	ID, Version string
	Does        string // metadata.description
	Docs        string // "BRICKKIT.md +zh"，没有文档时 "—"
	Home        string // metadata.repository，没有时 "—"
}

// Content 决定维护区里有什么。
type Content struct {
	// Lang 是要用的语言；文件里已有维护区时以它记的为准，除非 ForceLang（skills update --lang）。
	Lang      string
	ForceLang bool
	// Title 是新建 AGENTS.md 时的一级标题（项目名或组件 ID）；空时用目录名。
	Title string
	// Component：这里是组件仓库（有 component.yaml），放组件作者要守的规则。
	Component bool
	// Project：这里有 brickkit.yaml，放平台规则（纯项目时）与组件表。
	Project bool
	Rows    []Row
}

// tmpl 是 lang 的模板；这种语言还没有模板时用英文那份（维护区照样记着 lang）。
func tmpl(lang, name string) string {
	data, err := templates.ReadFile("templates/" + lang + "/" + name)
	if err != nil {
		data, _ = templates.ReadFile("templates/en/" + name)
	}
	return string(data)
}

// Render 渲染整段维护区（含两端标记）。
// 组件仓库放组件规则；纯项目放项目规则与组件表；两者都有（工作台）时放组件规则加组件表那一节。
func Render(c Content) string {
	var b strings.Builder
	b.WriteString("<!-- brickkit:managed:begin lang=" + c.Lang + " -->\n")
	b.WriteString(tmpl(c.Lang, "note.md")) // 维护区的说明与组件表的表头都在模板里，跟着维护区的语言
	b.WriteString("\n")
	if c.Component {
		b.WriteString(tmpl(c.Lang, "component.md"))
	}
	if c.Project {
		project := tmpl(c.Lang, "project.md")
		if c.Component { // 工作台：平台规则已经有了组件那份，只要组件表那一节
			if i := strings.Index(project, "\n## "); i >= 0 {
				project = project[i+1:]
			}
			b.WriteString("\n")
		}
		b.WriteString(project)
		b.WriteString("\n")
		b.WriteString(tmpl(c.Lang, "table.md"))
		for _, r := range c.Rows {
			b.WriteString("| " + strings.Join([]string{cell(r.ID), cell(r.Version), cell(r.Does), cell(r.Docs), cell(r.Home)}, " | ") + " |\n")
		}
	}
	b.WriteString(endMarker + "\n")
	return b.String()
}

// cell 把一段文字放进表格单元格：竖线转义、换行并成空格、空的写 "—"。
func cell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "—"
	}
	return strings.ReplaceAll(s, "|", `\|`)
}

// DocsCell 是"文档"一列：有原文写 BRICKKIT.md，再加每个译本的语言（+zh）；都没有时 "—"。
func DocsCell(hasPrimary bool, langs []string) string {
	var parts []string
	if hasPrimary {
		parts = append(parts, docspec.FileBrickkit)
	}
	for _, l := range langs {
		parts = append(parts, "+"+l)
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " ")
}
