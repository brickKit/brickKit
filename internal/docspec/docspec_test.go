package docspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequiredSections(t *testing.T) {
	assert.Equal(t, []Section{Purpose, BeforeDeploy, Dependencies, Configuration, Contracts, ShellDecl}, Required(KindBrickkit))
	assert.Equal(t, []Section{CodeMap, BuildTest, Decisions, Pitfalls, BeforeChanging}, Required(KindComponentAgents))
	assert.Equal(t, []Section{Overview, Conventions, WhereToLook, Pitfalls}, Required(KindProjectAgents))
	assert.Equal(t, []Section{UseIt, Documentation, Development}, Required(KindReadme))
}

func TestHeadingsInBothLanguages(t *testing.T) {
	assert.Equal(t, "Before you deploy", Heading(BeforeDeploy, "en"))
	assert.Equal(t, "部署前准备", Heading(BeforeDeploy, "zh"))
	assert.Equal(t, "Code map", Heading(CodeMap, "ja"), "an unknown language falls back to English")
	for _, k := range []Kind{KindBrickkit, KindComponentAgents, KindProjectAgents, KindReadme} {
		for _, s := range Required(k) {
			for _, l := range HeadingLangs() {
				assert.NotEmpty(t, headings[s][l], "section %d lacks a %s heading", s, l)
			}
		}
	}
}

func TestMatchesNumberedAndDecorated(t *testing.T) {
	for _, h := range []string{"Purpose", "1. Purpose", "Purpose  ", "Purpose {#purpose}", "purpose", "组件定位", "2、组件定位", "3) 组件定位"} {
		assert.True(t, Matches(Purpose, h), h)
	}
	assert.False(t, Matches(Purpose, "Purposes"))
	assert.False(t, Matches(Purpose, "Dependencies"))
}

func TestLanguageCodes(t *testing.T) {
	for _, c := range []string{"zh", "en", "ja", "pt-br", "zh-hant"} {
		assert.True(t, ValidLang(c), c)
	}
	for _, c := range []string{"", "zh-CN", "Chinese", "draft", "x"} {
		assert.False(t, ValidLang(c), c)
	}
}

func TestSplitTranslation(t *testing.T) {
	cases := []struct {
		in, base, lang string
		ok             bool
	}{
		{"README.md", "README.md", "", true},
		{"README.zh.md", "README.md", "zh", true},
		{"design.pt-br.md", "design.md", "pt-br", true},
		{"README.zh-CN.md", "", "", false},
		{"v1.2-notes.md", "", "", false},
		{"notes.txt", "", "", false},
	}
	for _, c := range cases {
		base, lang, ok := SplitTranslation(c.in)
		assert.Equal(t, c.ok, ok, c.in)
		if c.ok {
			assert.Equal(t, c.base, base, c.in)
			assert.Equal(t, c.lang, lang, c.in)
		}
	}
	assert.Equal(t, "BRICKKIT.zh.md", TranslationName("BRICKKIT.md", "zh"))
	assert.Equal(t, "BRICKKIT.md", TranslationName("BRICKKIT.md", ""))
}
