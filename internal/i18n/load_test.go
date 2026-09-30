package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每种登记的语言都有一份读得通的目录文件。
func TestEveryRegisteredLanguageLoads(t *testing.T) {
	for _, l := range registry {
		c, err := loadCatalog(l.Code)
		require.NoError(t, err, l.Code)
		assert.NotEmpty(t, c, l.Code)
	}
}

// 启动预算：读一份目录不超过 10ms（Global Constraints）。基准的数字记进账本。
func BenchmarkLoadCatalog(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := loadCatalog(ZH); err != nil {
			b.Fatal(err)
		}
	}
}
