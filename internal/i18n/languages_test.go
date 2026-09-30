package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/msgid"
)

// 语言只在 registry 一处登记：列表、解析、目录选择都从它推出来。
func TestRegistryDrivesEveryLanguageList(t *testing.T) {
	assert.Equal(t, SourceLang(), registry[0].Code, "第一项是源语言")
	assert.Equal(t, EN, SourceLang())
	codes := make([]Lang, len(registry))
	for i, l := range registry {
		codes[i] = l.Code
	}
	assert.Equal(t, codes, SupportedLangs())
	for _, l := range registry {
		got, ok := ParseLang(" " + string(l.Code) + " ")
		assert.True(t, ok)
		assert.Equal(t, l.Code, got)
		assert.NotEmpty(t, catalogFor(l.Code), "%s 有目录", l.Code)
	}
}

// 没登记的语言代码落回源语言的目录——与重构前的行为一致。
func TestUnregisteredLanguageUsesSourceCatalog(t *testing.T) {
	assert.Equal(t, catalogFor(SourceLang())[msgid.ErrorPrefix], catalogFor(Lang("xx"))[msgid.ErrorPrefix])
}
