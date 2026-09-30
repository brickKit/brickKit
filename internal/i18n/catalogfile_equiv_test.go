package i18n

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 临时：YAML 目录与 Go map 目录逐 key、逐字节相同。Task 4 删掉 Go map 时一起删掉这个测试。
func TestYAMLCatalogsEqualGoMaps(t *testing.T) {
	for lang, goMap := range map[Lang]map[string]string{EN: en, ZH: zh} {
		data, err := os.ReadFile("locales/" + string(lang) + ".yaml")
		require.NoError(t, err)
		keys, texts, err := parseCatalog(data, "locales/"+string(lang)+".yaml")
		require.NoError(t, err)
		assert.Len(t, keys, len(goMap), lang)
		for k, v := range goMap {
			assert.Equal(t, v, texts[k], "%s %s", lang, k)
		}
	}
}
