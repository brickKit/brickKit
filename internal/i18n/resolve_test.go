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
	_, ok := ParseLang("fr")
	assert.False(t, ok)
}

func TestSupportedLangsAndNames(t *testing.T) {
	assert.Equal(t, []Lang{EN, ZH}, SupportedLangs())
	assert.Equal(t, []string{"en", "zh"}, LangNames())
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
	t.Setenv(EnvLang, "fr")

	lang, source := Resolve()
	assert.Equal(t, ZH, lang, "不认识的 BRICKKIT_LANG 值当作没设置，往下一层找，不阻断启动")
	assert.Equal(t, SourceConfig, source)
}
