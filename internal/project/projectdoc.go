package project

// 本文件是项目级 BRICKKIT.md（提案 §16.2.1 的实例层）：列出项目里的组件与各自文档、契约的位置，
// 让 AI 与人不翻源码就能按图索骥。
//
// 组件表由 CLI 维护，放在一对标记之间：add / remove / upgrade 每次改完 brickkit.yaml 都重写它，
// 表永远跟着项目走。标记之外的内容是使用者的，CLI 一个字都不碰；没有标记的 BRICKKIT.md
// （使用者自己写的、或组件仓库里组件自己的文档）整个不碰。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

const (
	managedBegin = "<!-- brickkit:managed:begin -->"
	managedEnd   = "<!-- brickkit:managed:end -->"
)

// projectDocHead 是 init 写出的文档开头（标记之外，写完就归使用者）。
func projectDocHead(name string) string {
	return "# " + name + "\n\n" +
		i18n.T(msgid.ProjectDocIntro) + "\n\n" +
		"## " + i18n.T(msgid.ProjectDocThreeFiles) + "\n\n" +
		"- " + i18n.T(msgid.ProjectDocDeclFile, FileDecl) + "\n" +
		"- " + i18n.T(msgid.ProjectDocDeployFiles, FileDeploy, FileDeployLocal) + "\n" +
		"- " + i18n.T(msgid.ProjectDocConfigDir, DirConfig+"/") + "\n\n"
}

// managedBlock 把 body 包进 CLI 维护区。
func managedBlock(body string) string {
	return managedBegin + "\n" +
		"<!-- " + i18n.T(msgid.ProjectDocManagedNote) + " -->\n" +
		body +
		managedEnd + "\n"
}

func hasManagedBlock(doc string) bool {
	begin := strings.Index(doc, managedBegin)
	return begin >= 0 && strings.Index(doc[begin:], managedEnd) > 0
}

// RenderProjectDoc 渲染 CLI 维护区（含两端标记）：组件表与本地源组件表。
// 只写盘上真有的文档与产物路径——写一条不存在的路径，比不写更误导。
func RenderProjectDoc(p *Project) string {
	var b strings.Builder
	b.WriteString("\n## " + i18n.T(msgid.ProjectDocComponents) + "\n\n")
	if len(p.Decl.Components) == 0 {
		b.WriteString(i18n.T(msgid.ProjectDocNoComponents) + "\n")
	} else {
		b.WriteString("| " + strings.Join([]string{
			i18n.T(msgid.ProjectDocColID), i18n.T(msgid.ProjectDocColVersion),
			i18n.T(msgid.ProjectDocColDoc), i18n.T(msgid.ProjectDocColContract),
		}, " | ") + " |\n|---|---|---|---|\n")
		for _, c := range p.Decl.Components {
			doc := p.relIfExists(p.Layout.CachedDocPath(c.ID, c.Version))
			contract := p.relIfExists(filepath.Join(p.Layout.ArtifactsDir(), manifest.ServiceName(c.ID, c.Version)))
			if contract != "—" {
				contract = strings.TrimSuffix(contract, "`") + "/`"
			}
			b.WriteString("| " + c.ID + " | " + c.Version + " | " + doc + " | " + contract + " |\n")
		}
	}

	var local [][2]string
	var seen []string
	for _, c := range p.Decl.Components {
		if slices.Contains(seen, c.ID) {
			continue
		}
		seen = append(seen, c.ID)
		if dir, ok := p.LocalRepo(c.ID); ok {
			local = append(local, [2]string{c.ID, dir})
		}
	}
	if len(local) > 0 {
		b.WriteString("\n## " + i18n.T(msgid.ProjectDocLocalComponents) + "\n\n")
		b.WriteString("| " + strings.Join([]string{
			i18n.T(msgid.ProjectDocColID), i18n.T(msgid.ProjectDocColSourceDir), i18n.T(msgid.ProjectDocColDoc),
		}, " | ") + " |\n|---|---|---|\n")
		for _, l := range local {
			src := strings.TrimSuffix(p.rel(l[1]), "`") + "/`"
			b.WriteString("| " + l[0] + " | " + src + " | " + p.relIfExists(filepath.Join(l[1], FileProjectDoc)) + " |\n")
		}
	}
	b.WriteString("\n")
	return managedBlock(b.String())
}

// rel 把路径写成项目根下的相对路径（斜杠分隔、反引号包起来）。
func (p *Project) rel(path string) string {
	root, _ := filepath.Abs(p.Layout.Root)
	abs, _ := filepath.Abs(path)
	if r, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(r, "..") {
		path = r
	}
	return "`" + filepath.ToSlash(path) + "`"
}

func (p *Project) relIfExists(path string) string {
	if !exists(path) {
		return "—"
	}
	return p.rel(path)
}

// WriteProjectDoc 重写项目 BRICKKIT.md 的 CLI 维护区。只改已有的、带维护区的文件：
// 生成它是 init 的事（使用者删掉了它，add 不该每次都再造一份出来）；没有维护区的文件
// （使用者自己写的，或组件仓库里组件自己的文档，§16.1.1）一个字都不动。
// 返回这次是否写了文件。
func WriteProjectDoc(l Layout, p *Project) (bool, error) {
	path := l.ProjectDocPath()
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, ioError(i18n.T(msgid.ConfigActionReadFile), path, err)
	}
	doc := string(data)
	begin := strings.Index(doc, managedBegin)
	if begin < 0 {
		return false, nil
	}
	end := strings.Index(doc[begin:], managedEnd)
	if end < 0 {
		return false, nil
	}
	end += begin + len(managedEnd)
	if end < len(doc) && doc[end] == '\n' {
		end++
	}
	updated := doc[:begin] + RenderProjectDoc(p) + doc[end:]
	if updated == doc {
		return false, nil
	}
	return true, writeFile(path, updated)
}

func writeFile(path, content string) error {
	if err := os.WriteFile(path, []byte(content), initFilePerm); err != nil {
		return ioError(i18n.T(msgid.ActionWriteFile), path, err)
	}
	return nil
}
