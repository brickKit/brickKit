package project

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// 文件头按生成它的语言认，与 CLI 现在说什么语言无关：中文 init 的项目，英文 CLI 下 local on 也要换掉那句"请提交它"。
func TestLocalDeployContentSwapsHeaderInItsOwnLanguage(t *testing.T) {
	body := "target: docker\n\ncomponents: []\n"
	for _, lang := range i18n.SupportedLangs() {
		catalog := i18n.CatalogFor(lang)
		team := yamlcomment.Block("", catalog[msgid.ProjectSkeletonDeployHeader]) + body
		want := yamlcomment.Block("", catalog[msgid.ProjectSkeletonDeployLocalHeader]) + body
		assert.Equal(t, want, string(LocalDeployContent([]byte(team))), lang)
	}
	edited := "# our own header\n" + body
	assert.Equal(t, edited, string(LocalDeployContent([]byte(edited))), "认不出的文件头原样保留")
}
