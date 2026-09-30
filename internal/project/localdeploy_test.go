package project

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// 个人文件的文件头按结构认：deploy.yaml 开头连续的注释行（到第一个不是注释的行为止）就是文件头，
// 换成个人文件的说明，用当前 CLI 的语言——这份文件是写给这个人看的。不拿文字去比：团队文件头是
// 哪种语言写的、译文后来改没改过、团队自己改没改过，都一样换掉；那句"团队文件，请提交它"放进
// 一个从不提交的文件里就是假话。
func TestLocalDeployContentReplacesTheOpeningCommentsWithThePersonalHeader(t *testing.T) {
	prev := i18n.Current()
	t.Cleanup(func() { i18n.SetCurrent(prev) })
	i18n.SetCurrent(i18n.SourceLang())
	personal := yamlcomment.Block("", i18n.T(msgid.ProjectSkeletonDeployLocalHeader))
	body := "target: docker\n\ncomponents: []  # kept\n# a note further down, kept\n"

	for _, lang := range i18n.SupportedLangs() {
		team := yamlcomment.Block("", i18n.CatalogFor(lang)[msgid.ProjectSkeletonDeployHeader]) + body
		assert.Equal(t, personal+body, string(LocalDeployContent([]byte(team))), lang)
	}
	for _, team := range []string{
		"# deploy.yaml header from an older catalog\n# second line\n" + body,
		"# our own header, written by the team\n" + body,
		body,
	} {
		assert.Equal(t, personal+body, string(LocalDeployContent([]byte(team))), team)
	}
}
