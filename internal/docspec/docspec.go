// Package docspec 是组件与项目文档规范本身，写成数据：有哪些文件、每种文件要哪几节、
// 各节在每种语言里叫什么、怎样的文件名算译本、哪些词算占位。
//
// 生成骨架（manifest、agentsmd）与检查（doccheck）都从这里取，标题因此只有一份。
// 标题是文档内容的一部分，跟着那份文档的语言走，不随 CLI 的显示语言变——所以写在这里，
// 不进 i18n 目录。
package docspec

import (
	"path"
	"regexp"
	"strings"
)

// 文档文件名。
const (
	FileBrickkit = "BRICKKIT.md"
	FileAgents   = "AGENTS.md"
	FileClaude   = "CLAUDE.md"
	FileReadme   = "README.md"
	// ClaudeImport 是 CLAUDE.md 的全部内容：Claude Code 读 CLAUDE.md，其他工具读 AGENTS.md，内容只写一份。
	ClaudeImport = "@AGENTS.md"
	DirDocs      = "docs"
	// MaxTranslations 是一个组件随版本发布的 BRICKKIT.md 译本上限。
	MaxTranslations = 16
)

// Kind 是一种有固定小节的文档。
type Kind int

const (
	KindBrickkit Kind = iota
	KindComponentAgents
	KindProjectAgents
	KindReadme
)

// Section 是一个固定小节。
type Section int

const (
	Purpose Section = iota
	BeforeDeploy
	Dependencies
	Configuration
	Contracts
	ShellDecl
	CodeMap
	BuildTest
	Decisions
	Pitfalls
	BeforeChanging
	Overview
	Conventions
	WhereToLook
	UseIt
	Documentation
	Development
)

var required = map[Kind][]Section{
	KindBrickkit:        {Purpose, BeforeDeploy, Dependencies, Configuration, Contracts, ShellDecl},
	KindComponentAgents: {CodeMap, BuildTest, Decisions, Pitfalls, BeforeChanging},
	KindProjectAgents:   {Overview, Conventions, WhereToLook, Pitfalls},
	KindReadme:          {UseIt, Documentation, Development},
}

// Required 是这种文档必须有的小节，按它们在文档里的顺序。
func Required(k Kind) []Section { return append([]Section(nil), required[k]...) }

// headings 是每节在每种语言里的标题。加一种语言 = 给每一节加一列。
var headings = map[Section]map[string]string{
	Purpose:        {"en": "Purpose", "zh": "组件定位"},
	BeforeDeploy:   {"en": "Before you deploy", "zh": "部署前准备"},
	Dependencies:   {"en": "Dependencies", "zh": "依赖说明"},
	Configuration:  {"en": "Configuration", "zh": "配置指南"},
	Contracts:      {"en": "Contracts", "zh": "契约索引"},
	ShellDecl:      {"en": "Shell declaration", "zh": "外壳声明"},
	CodeMap:        {"en": "Code map", "zh": "代码地图"},
	BuildTest:      {"en": "Build and test", "zh": "构建与测试"},
	Decisions:      {"en": "Design decisions", "zh": "设计取舍"},
	Pitfalls:       {"en": "Pitfalls", "zh": "易错点"},
	BeforeChanging: {"en": "Before changing code", "zh": "改代码前自查"},
	Overview:       {"en": "Overview", "zh": "项目概述"},
	Conventions:    {"en": "Conventions", "zh": "项目约定"},
	WhereToLook:    {"en": "Where to look", "zh": "查找路由"},
	UseIt:          {"en": "Use it in a project", "zh": "在项目里使用"},
	Documentation:  {"en": "Documentation", "zh": "文档"},
	Development:    {"en": "Development", "zh": "开发"},
}

// HeadingLangs 是认得出标题的语言。
func HeadingLangs() []string { return []string{"en", "zh"} }

// Heading 是这一节在 lang 里的标题；不认识的语言给英文。
func Heading(s Section, lang string) string {
	if h, ok := headings[s][lang]; ok {
		return h
	}
	return headings[s]["en"]
}

// numbering 是标题前可有可无的编号（"1. "、"2、"、"3) "），decoration 是标题后的锚点（" {#x}"）。
var (
	numbering  = regexp.MustCompile(`^\d+(\.\d+)*[.、)]?\s*`)
	decoration = regexp.MustCompile(`\s*\{#[^}]*\}$`)
)

// Matches 判断一个二级标题（已去掉 "## "）是不是这一节，任何认得的语言都算。
func Matches(s Section, heading string) bool {
	h := strings.TrimSpace(heading)
	h = strings.TrimSpace(decoration.ReplaceAllString(numbering.ReplaceAllString(h, ""), ""))
	for _, name := range headings[s] {
		if strings.EqualFold(h, name) {
			return true
		}
	}
	return false
}

var langCode = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)

// ValidLang 判断 code 是不是合法的语言代码（小写 BCP 47）。
func ValidLang(code string) bool { return langCode.MatchString(code) }

// SplitTranslation 把 Markdown 文件名拆成原文名与语言：README.zh.md → README.md、zh；
// README.md → README.md、""。名字里还有点、而最后一个点后的那段不是语言代码时（README.zh-CN.md、
// v1.2-notes.md）ok 为 false：它可能是写错了后缀的译本，要不要点出来由检查方决定。
func SplitTranslation(name string) (base, lang string, ok bool) {
	if path.Ext(name) != ".md" {
		return "", "", false
	}
	stem := strings.TrimSuffix(name, ".md")
	dot := strings.LastIndex(stem, ".")
	if dot < 0 {
		return name, "", true
	}
	suffix := stem[dot+1:]
	if !ValidLang(suffix) {
		return "", "", false
	}
	return stem[:dot] + ".md", suffix, true
}

// TranslationName 是 base 的 lang 译本的文件名；lang 为空时就是 base。
func TranslationName(base, lang string) string {
	if lang == "" {
		return base
	}
	return strings.TrimSuffix(base, ".md") + "." + lang + ".md"
}

// PlaceholderWords 是正文里不该留下的占位词（代码里的不算）。单独成词才算：中文的前后挨着汉字
// 就是正常的话（"事后补上"），所以"待补充""待填写"这种常见的写法要各自列出来。
var PlaceholderWords = []string{"TODO", "TBD", "FIXME", "待补", "待补充", "后补", "待填", "待填写"}
