package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stepColumn 匹配"下一步"里带命令的一行：两格缩进、命令、至少两格空白、说明。
var stepColumn = regexp.MustCompile(`^  (\S.*?\S)( {2,})(\S.*)$`)

// descriptionColumns 返回"下一步："之后每条带说明的命令行里，说明从第几列开始。
func descriptionColumns(t *testing.T, stdout string) []int {
	t.Helper()
	_, after, ok := strings.Cut(stdout, "Next steps:\n")
	require.True(t, ok, stdout)
	var cols []int
	for _, line := range strings.Split(after, "\n") {
		if m := stepColumn.FindStringSubmatchIndex(line); m != nil {
			cols = append(cols, m[6])
		}
	}
	return cols
}

// "下一步"的说明对成一列：命令长短不一（cd 后面是用户给的目录名），靠文案里数空格对不齐。
func TestNextStepsDescriptionsLineUp(t *testing.T) {
	for _, args := range [][]string{
		{"new", "shop/orders", "--path", "a-rather-long-directory-name"},
		{"new", "shop/orders"},
		{"init", "my-shop", "--no-skills"},
	} {
		r := runIn(t, t.TempDir(), args...)
		cols := descriptionColumns(t, r.stdout)
		require.GreaterOrEqual(t, len(cols), 2, "%v:\n%s", args, r.stdout)
		for _, c := range cols {
			assert.Equal(t, cols[0], c, "%v: descriptions should start in one column:\n%s", args, r.stdout)
		}
	}
}
