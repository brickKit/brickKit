package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/msgid"
)

func TestCurrentDefaultsToEnglish(t *testing.T) {
	assert.Equal(t, EN, Current())
}

func TestSetCurrentChangesLookupLanguage(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)

	SetCurrent(ZH)
	assert.Equal(t, "建议：", T(msgid.HintLabelMulti))

	SetCurrent(EN)
	assert.Equal(t, "Suggestions:", T(msgid.HintLabelMulti))
}

func TestTInterpolatesPositionalArgs(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)

	SetCurrent(EN)
	assert.Equal(t, "Path: brickkit.yaml", T(msgid.DetailLine, "Path", "brickkit.yaml"))

	SetCurrent(ZH)
	assert.Equal(t, "路径：brickkit.yaml", T(msgid.DetailLine, "路径", "brickkit.yaml"))
}

func TestCatalogForReturnsIndependentSnapshot(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)
	SetCurrent(ZH)

	snap := CatalogFor(ZH)
	snap[msgid.HintLabelMulti] = "被改了"
	assert.Equal(t, "建议：", T(msgid.HintLabelMulti), "CatalogFor 返回的必须是拷贝，不能让调用方改到真的目录")
}

// TestCatalogParity 是双语完整性的门槛：msgid 里声明的每个 key，en/zh
// 两份目录都必须有，缺一个就是半成品——不允许运行时才发现某句话是空字符串。
func TestCatalogParity(t *testing.T) {
	for key := range en {
		if _, ok := zh[key]; !ok {
			t.Errorf("zh 目录缺少 key：%s", key)
		}
	}
	for key := range zh {
		if _, ok := en[key]; !ok {
			t.Errorf("en 目录缺少 key：%s", key)
		}
	}
}
