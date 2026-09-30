package suggest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSimilar(t *testing.T) {
	ids := []string{"erp/api", "erp/auth", "crm/api", "infra/cache"}
	assert.Equal(t, []string{"crm/api", "erp/api"}, Similar("api", ids))
	assert.Equal(t, []string{"erp/api"}, Similar("erp/apj", ids))
	assert.Equal(t, []string{"infra/cache"}, Similar("cache", ids))
	assert.Empty(t, Similar("zzz", ids))
	assert.NotContains(t, Similar("erp/api", ids), "erp/api", "完全一样的不算建议")
}

// ID 本身写对了（错的是版本之类）：不给候选——提示"你是不是想写 crm/api"只会误导。
func TestHintSaysNothingWhenTheIDItselfExists(t *testing.T) {
	ids := []string{"erp/api", "crm/api"}
	_, ok := Hint("erp/api@9.9.9", ids)
	assert.False(t, ok)
	h, ok := Hint("erp/apj", ids)
	assert.True(t, ok)
	assert.Contains(t, h, "erp/api")
}
