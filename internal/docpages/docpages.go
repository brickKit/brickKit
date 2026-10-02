// Package docpages 是 CLI 里自带的 BrickKit 文档：docs/en、docs/zh 两棵树编进二进制，
// brickkit docs 按页 ID 打印出来，报错建议也用页 ID 指过去——版本永远与正在跑的 CLI 一致，不联网也读得到。
//
// 页 ID 是页面在 docs/<语言>/ 下的路径，去掉 .md：04-shell/02-json-injection。一个目录的 README.md
// 用目录名指（04-shell），根上的 README.md 是 README。两种语言的树一一对应，所以页 ID 与语言无关。
package docpages

import (
	"errors"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	bkdocs "github.com/brickkit/brickkit/docs"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/version"
)

// 报错建议、帮助文本里指到的页。都在 docpages_test 里核对过两种语言都有。
const (
	PageComponentYAML = "11-reference/01-component-yaml-schema"
	PageBrickkitYAML  = "11-reference/02-brickkit-yaml-schema"
	PageDeployYAML    = "11-reference/03-deploy-yaml-schema"
	PageConfigDir     = "01-three-layers/05-config-directory"
	PageSecurity      = "06-architecture/08-security-and-signing"
	PageUpDownIssues  = "10-troubleshooting/01-up-down-issues"
	PageConfigSchema  = "11-reference/04-config-schema-spec"
	PageShellComplete = "00-intro/03-shell-completion"
)

// Referenced 是代码里指到的全部页（测试用）。
var Referenced = []string{
	PageComponentYAML, PageBrickkitYAML, PageDeployYAML, PageConfigDir,
	PageSecurity, PageUpDownIssues, PageConfigSchema, PageShellComplete,
}

// Repository 是 BrickKit 仓库的网页地址。
const Repository = "https://github.com/brickKit/brickKit"

// ErrNotFound：没有这一页。
var ErrNotFound = errors.New("no such page")

// Page 是一页：ID 与它的一级标题。
type Page struct {
	ID, Title string
}

// Langs 是有文档的语言。
func Langs() []string { return []string{"en", "zh"} }

// HasLang 判断 lang 有没有文档。
func HasLang(lang string) bool {
	for _, l := range Langs() {
		if l == lang {
			return true
		}
	}
	return false
}

// List 是 lang 的全部页，按 ID 排好序（目录的 README 排在目录里各页前面）。
func List(lang string) []Page {
	var out []Page
	_ = fs.WalkDir(bkdocs.FS, lang, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		body, _ := fs.ReadFile(bkdocs.FS, p)
		out = append(out, Page{ID: idOf(strings.TrimPrefix(p, lang+"/")), Title: title(string(body))})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Read 是 lang 里 id 那一页的原文。id 可以是 Normalize 认得的任何写法。
func Read(lang, id string) (string, error) {
	data, err := fs.ReadFile(bkdocs.FS, path.Join(lang, fileOf(Normalize(id))))
	if err != nil {
		return "", ErrNotFound
	}
	return string(data), nil
}

// Exists 判断 lang 里有没有 id 这一页。
func Exists(lang, id string) bool {
	_, err := Read(lang, id)
	return err == nil
}

var langPrefix = regexp.MustCompile(`^(?:\./)?docs/[a-z]{2,3}(?:-[a-z0-9]+)*/`)

// Normalize 把使用者可能抄来的写法变成页 ID：docs/en/04-shell/README.md、04-shell/、
// 04-shell/02-json-injection.md、文档里的相对链接拼出来的 04-shell/../06-architecture/x.md 都行。
func Normalize(arg string) string {
	s := strings.TrimSpace(arg)
	s, _, _ = strings.Cut(s, "#")
	s = langPrefix.ReplaceAllString(s, "")
	s = path.Clean("/" + s)[1:]
	s = strings.TrimSuffix(s, ".md")
	if s == "" {
		return "README"
	}
	return idOf(s + ".md")
}

// URL 是这一页在网上的地址，指向与正在跑的 CLI 同一个版本的 tag；开发版（不是 vX.Y.Z）指向 main。
func URL(lang, id string) string {
	return Repository + "/blob/" + ref() + "/docs/" + lang + "/" + fileOf(Normalize(id))
}

// Ref 是指向一页的两样东西，给消息的 %[1]s 与 %[2]s：打印它的命令，和它在网上的地址（CLI 当前的语言）。
func Ref(id string) []any {
	lang := string(i18n.Current())
	if !HasLang(lang) {
		lang = "en"
	}
	return []any{Command(id), URL(lang, id)}
}

// Command 是打印这一页的命令。
func Command(id string) string { return "brickkit docs " + id }

var release = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

func ref() string {
	if v := version.Display(); release.MatchString(v) {
		return v
	}
	return "main"
}

// idOf：04-shell/README.md → 04-shell；README.md → README；04-shell/02-x.md → 04-shell/02-x。
func idOf(rel string) string {
	rel = strings.TrimSuffix(rel, ".md")
	if dir, ok := strings.CutSuffix(rel, "/README"); ok {
		return dir
	}
	return rel
}

// fileOf 是 id 在语言树里的文件：目录名指它的 README.md。
func fileOf(id string) string {
	if id == "README" {
		return "README.md"
	}
	if _, err := fs.Stat(bkdocs.FS, path.Join("en", id+".md")); err == nil {
		return id + ".md"
	}
	if st, err := fs.Stat(bkdocs.FS, path.Join("en", id)); err == nil && st.IsDir() {
		return id + "/README.md"
	}
	return id + ".md"
}

func title(body string) string {
	for _, line := range strings.SplitN(body, "\n", 5) {
		if t, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(t)
		}
	}
	return ""
}
