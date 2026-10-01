package project

// 本文件是项目 AGENTS.md 末尾那段由 CLI 维护的内容：平台规则与组件表。
// 表里只写对所有人都一样的事实（brickkit.yaml 与组件那个版本自己说的），见 agentsmd。

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/projfile"
)

// AgentsContent 算出 l 下 AGENTS.md 维护区该有的内容。lang 只在文件里还没有维护区时用。
// 缓存里缺某个组件版本时（新克隆的仓库），那一行沿用现有维护区里的单元格。
func AgentsContent(l Layout, decl *projfile.File, lang string) agentsmd.Content {
	c := agentsmd.Content{Lang: lang, Project: true, Title: decl.Project}
	if exists(filepath.Join(l.Root, manifest.FileName)) {
		c.Component = true // 工作台：组件仓库兼作项目
		if md, ok := readMetadata(filepath.Join(l.Root, manifest.FileName)); ok && md.ID != "" {
			c.Title = md.ID
		}
	}
	var previous map[string]agentsmd.Row
	if data, err := os.ReadFile(l.AgentsPath()); err == nil {
		if b, err := agentsmd.Find(string(data)); err == nil {
			previous = agentsmd.ParseRows(string(data), b)
		}
	}
	for _, comp := range decl.Components {
		row := agentsmd.Row{ID: comp.ID, Version: comp.Version, Does: "—", Docs: "—", Home: "—"}
		if md, ok := readMetadata(l.CachedManifestPath(comp.ID, comp.Version)); ok {
			row.Does = orDash(md.Description)
			row.Home = orDash(md.Repository)
			row.Docs = agentsmd.DocsCell(l.CachedDocLangs(comp.ID, comp.Version))
		} else if old, ok := previous[comp.Ref()]; ok {
			row = old
		}
		c.Rows = append(c.Rows, row)
	}
	return c
}

// readMetadata 只读 component.yaml 的 metadata，不做校验：组件表只要说明与主页两句话，
// 不该因为清单里一条与之无关的规则没过就把已有的说明抹掉。
func readMetadata(path string) (manifest.Metadata, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest.Metadata{}, false
	}
	var doc struct {
		Metadata manifest.Metadata `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return manifest.Metadata{}, false
	}
	return doc.Metadata, true
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// WriteAgentsBlock 改写项目 AGENTS.md 已有的维护区（add / remove / upgrade 之后）。
// 只要 brickkit.yaml：组件表里的事实都来自它和缓存，不必装载整个项目（装载会被配置冲突拦下）。
// 没有文件、没有维护区都不建也不报：那是 init / skills update 的事。
func WriteAgentsBlock(l Layout, decl *projfile.File) (agentsmd.Result, error) {
	return agentsmd.Ensure(l.Root, AgentsContent(l, decl, ""), agentsmd.ModeRewrite, "")
}

// ObsoleteProjectMap 判断项目根有没有旧版的项目地图 BRICKKIT.md（带维护区标记）。
// 组件仓库（有 component.yaml）里的 BRICKKIT.md 是组件自己的文档，不算。
func ObsoleteProjectMap(l Layout) bool {
	if exists(filepath.Join(l.Root, manifest.FileName)) {
		return false
	}
	data, err := os.ReadFile(filepath.Join(l.Root, docspec.FileBrickkit))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "<!-- brickkit:managed:begin")
}
