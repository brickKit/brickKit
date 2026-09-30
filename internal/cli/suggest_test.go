package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 组件 ID 写错：报错不变，多一条"你是不是想写"。一个都不猜着执行。
func TestMistypedIDsGetSuggestions(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"remove":  {[]string{"remove", "erp/apj"}, "erp/api"},
		"upgrade": {[]string{"upgrade", "erp/portl"}, "erp/portal"},
		"deps":    {[]string{"deps", "erp/workr"}, "erp/worker"},
		"focus":   {[]string{"up", "--dry-run", "--focus", "portal"}, "erp/portal"},
		"add":     {[]string{"add", "erp/workr"}, "erp/worker"},
	}
	for name, c := range cases {
		dir := focusFixture(t)
		r := runWithEngine(t, newFakeEngine(), dir, c.args...)
		require.NotEqual(t, clierr.ExitOK, r.code, name)
		assert.Contains(t, r.stderr, i18n.T(msgid.HintDidYouMean, c.want), name)
	}
}
