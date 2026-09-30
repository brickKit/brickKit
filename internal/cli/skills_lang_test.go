package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 要的语言还没有技能译本、装的是源语言那一套时，要明说——不能悄悄换成英文。
func TestSkillsLanguageFallbackIsSaid(t *testing.T) {
	var out bytes.Buffer
	opts := &Options{Stdout: &out}
	renderSkillsLangFallback(opts, "xx", i18n.SourceLang())
	assert.Contains(t, out.String(), i18n.T(msgid.CliSkillsLangFallback, "xx", string(i18n.SourceLang())))

	out.Reset()
	renderSkillsLangFallback(opts, i18n.ZH, i18n.ZH)
	renderSkillsLangFallback(opts, "", i18n.SourceLang())
	assert.Empty(t, out.String(), "装的就是要的语言（或没指定语言）时不多说")
}
