package i18n

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var updateValues = flag.Bool("update-values", false, "重新生成 testdata/values 下的文案基线")

// P10 的脚手架：搬目录、改 key 名都不许改任何一条文案。按值排序比较——key 在 Task 7 会改名，值不能变。
func TestCatalogValuesGolden(t *testing.T) {
	for _, lang := range []Lang{EN, ZH} {
		values := make([]string, 0)
		for _, v := range CatalogFor(lang) {
			values = append(values, strings.ReplaceAll(v, "\n", `\n`))
		}
		sort.Strings(values)
		got := strings.Join(values, "\n") + "\n"
		file := filepath.Join("testdata", "values", string(lang)+".txt")
		if *updateValues {
			require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
			require.NoError(t, os.WriteFile(file, []byte(got), 0o644))
			continue
		}
		want, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, string(want), got, "%s 的文案变了", lang)
	}
}
