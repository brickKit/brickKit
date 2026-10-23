package agentsmd

// 本文件是项目的组件表 COMPONENTS.md：整份由 CLI 生成（作者不在里面写东西），
// 第一行记着它用的语言。维护区每次会话都加载，组件表随组件数变长，所以单独放一份，按需去读。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/mdtext"
)

var componentsLine = regexp.MustCompile(`^<!-- brickkit:components lang=(\S+) -->\s*$`)

// RenderComponents 渲染整份 COMPONENTS.md：语言标记、说明、组件表（一行一个 brickkit.yaml 里的版本）。
func RenderComponents(c Content) string {
	var b strings.Builder
	b.WriteString("<!-- brickkit:components lang=" + c.Lang + " -->\n")
	b.WriteString(tmpl(c.Lang, "components.md"))
	b.WriteString(tmpl(c.Lang, "table.md"))
	for _, r := range c.Rows {
		b.WriteString("| " + strings.Join([]string{cell(r.ID), cell(r.Version), cell(r.Does), cell(r.Docs), cell(r.Home)}, " | ") + " |\n")
	}
	return b.String()
}

// ComponentsLang 是 root/COMPONENTS.md 第一行记的语言；没有文件或第一行不是标记时 ok 为 false。
func ComponentsLang(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, docspec.FileComponents))
	if err != nil {
		return "", false
	}
	first, _, _ := strings.Cut(string(data), "\n")
	m := componentsLine.FindStringSubmatch(strings.TrimSpace(first))
	if m == nil || !docspec.ValidLang(m[1]) {
		return "", false
	}
	return m[1], true
}

// ParseRows 读出组件表的行（键 id@version，单元格已去掉转义）。缓存里缺某个组件时，
// 新渲染的那一行沿用这里的单元格：新克隆的仓库还没取过组件，不能因此把已经写好的说明抹成"—"。
func ParseRows(doc string) map[string]Row {
	rows := map[string]Row{}
	for _, l := range strings.Split(doc, "\n") {
		c := mdtext.TableCells(strings.TrimRight(l, "\r"))
		if len(c) != 5 || !strings.Contains(c[0], "/") {
			continue
		}
		for i := range c {
			c[i] = strings.ReplaceAll(c[i], `\|`, "|")
		}
		rows[c[0]+"@"+c[1]] = Row{ID: c[0], Version: c[1], Does: c[2], Docs: c[3], Home: c[4]}
	}
	return rows
}

// TableLang 是 root/COMPONENTS.md 该用的语言：AGENTS.md 维护区记的；没有维护区时沿用 COMPONENTS.md 自己记的
// （免得 CLI 语言不同的同事来回改写）；都没有时用此刻的 CLI 语言。
func TableLang(root string) string {
	if lang, ok := BlockLang(root); ok {
		return lang
	}
	if lang, ok := ComponentsLang(root); ok {
		return lang
	}
	return string(i18n.Current())
}

// ensureComponents 让 COMPONENTS.md 与 c 一致：没有就建，不一致就整份改写（每种模式都这样——它整份是 CLI 的）。
// 在 AGENTS.md 处理完之后调用，语言按 TableLang；skills update --lang（ForceLang）时用 c.Lang。
func ensureComponents(root string, c Content, res *Result) error {
	if !c.ForceLang {
		c.Lang = TableLang(root)
	}
	path := filepath.Join(root, docspec.FileComponents)
	want := RenderComponents(c)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		res.ComponentsCreated = true
	case err != nil:
		return err
	default:
		eol := lineEnding(string(data))
		if want = withEOL(want, eol); string(data) == want {
			return nil
		}
		res.ComponentsRewritten = true
	}
	return os.WriteFile(path, []byte(want), filePerm)
}
