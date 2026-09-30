package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/userconfig"
)

func TestParseLangAcceptsKnownValues(t *testing.T) {
	l, ok := ParseLang("en")
	assert.True(t, ok)
	assert.Equal(t, EN, l)

	l, ok = ParseLang(" ZH ")
	assert.True(t, ok)
	assert.Equal(t, ZH, l, "大小写与前后空白都要容错")
}

func TestParseLangRejectsUnknownValue(t *testing.T) {
	_, ok := ParseLang(notALanguage)
	assert.False(t, ok)
}

// notALanguage 是一个永远不会被登记的语言代码。别用真实的代码（fr、de……）当"不支持"的例子：
// 哪天真的加了这种语言，这些测试就会无缘无故变红。
const notALanguage = "not-a-language"

// LangNames 就是 SupportedLangs 的字符串形式——列表本身由 registry 决定，这里不写死。
func TestLangNamesMirrorSupportedLangs(t *testing.T) {
	langs := SupportedLangs()
	require.Len(t, LangNames(), len(langs))
	for i, l := range langs {
		assert.Equal(t, string(l), LangNames()[i])
	}
}

func TestResolveDefaultsToEnglishWhenNothingConfigured(t *testing.T) {
	t.Setenv(EnvLang, "")
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())

	lang, source := Resolve()
	assert.Equal(t, EN, lang)
	assert.Equal(t, SourceDefault, source)
}

func TestResolveEnvVarTakesPriorityOverConfig(t *testing.T) {
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())
	require.NoError(t, userconfig.Save(&userconfig.Config{Lang: "zh"}))
	t.Setenv(EnvLang, "en")

	lang, source := Resolve()
	assert.Equal(t, EN, lang)
	assert.Equal(t, SourceEnv, source)
}

func TestResolveFallsBackToConfigWhenEnvUnset(t *testing.T) {
	t.Setenv(EnvLang, "")
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())
	require.NoError(t, userconfig.Save(&userconfig.Config{Lang: "zh"}))

	lang, source := Resolve()
	assert.Equal(t, ZH, lang)
	assert.Equal(t, SourceConfig, source)
}

func TestResolveIgnoresUnsupportedEnvValueAndFallsThrough(t *testing.T) {
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())
	require.NoError(t, userconfig.Save(&userconfig.Config{Lang: "zh"}))
	t.Setenv(EnvLang, notALanguage)

	lang, source := Resolve()
	assert.Equal(t, ZH, lang, "不认识的 BRICKKIT_LANG 值当作没设置，往下一层找，不阻断启动")
	assert.Equal(t, SourceConfig, source)
}
