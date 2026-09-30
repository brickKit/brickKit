package clierr

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MapText 改写给人看的每一段文字（标题、明细值、建议、提示），返回副本；原错误不变，
// 日志里用的还是它。
func TestMapTextRewritesEveryShownTextOnACopy(t *testing.T) {
	e := New(CodeConfigInvalid, "bad /p/x").WithDetail("File", "/p/deploy.yaml").
		WithHint("see /p/a").WithTip("try /p/b")
	got := e.MapText(func(s string) string { return strings.ReplaceAll(s, "/p/", "") })

	assert.Equal(t, "bad x", got.Message)
	assert.Equal(t, []Detail{{Key: "File", Value: "deploy.yaml"}}, got.Details)
	assert.Equal(t, []string{"see a"}, got.Hints)
	assert.Equal(t, []string{"try b"}, got.Tips)
	assert.Equal(t, CodeConfigInvalid, got.Code)
	assert.Equal(t, "/p/deploy.yaml", e.Details[0].Value, "the original is untouched")
	assert.Equal(t, "see /p/a", e.Hints[0])
}
