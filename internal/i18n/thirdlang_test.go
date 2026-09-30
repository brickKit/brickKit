package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/msgid"
)

// 证明"加一种语言 = 一份目录 + 一行登记"：拿一份与英文相同 key 的目录临时登记成 xx，
// T 能取到它的文案，语言列表里也有它。
func TestAThirdLanguageNeedsOnlyACatalogAndARegistryLine(t *testing.T) {
	data, err := localeFS.ReadFile("locales/en.yaml")
	require.NoError(t, err)
	_, parsed, err := parseCatalog(data, "locales/xx.yaml")
	require.NoError(t, err)
	texts := map[msgid.ID]string{}
	for k, v := range parsed {
		texts[msgid.ID(k)] = v
	}
	texts[msgid.LabelReason] = "XX-Reason"

	restore := registerForTest(Language{Code: "xx"}, texts)
	defer restore()
	prev := Current()
	defer SetCurrent(prev)

	l, ok := ParseLang("xx")
	require.True(t, ok)
	SetCurrent(l)
	assert.Equal(t, "XX-Reason", T(msgid.LabelReason))
	assert.Contains(t, LangNames(), "xx")
}
