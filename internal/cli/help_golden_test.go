package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

var updateHelp = flag.Bool("update-help", false, "重新生成 testdata/help 下的 --help 基线")

// P10 的脚手架：重构目录结构前后，每条命令的 --help 在每种语言下必须一字不差。
// 基线在 P10 开始前生成一次；之后任何一处不同都是回归，而不是"更新一下基线"。
func TestHelpGolden(t *testing.T) {
	// 构建命令树会设置进程级的当前语言；不复位的话，后面不经过命令树的测试会读到最后一轮的中文
	t.Cleanup(func() { i18n.SetCurrent(i18n.EN) })
	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		t.Setenv(i18n.EnvLang, string(lang))
		var paths [][]string
		var walk func(c *cobra.Command, path []string)
		walk = func(c *cobra.Command, path []string) {
			paths = append(paths, path)
			for _, sub := range c.Commands() {
				walk(sub, append(append([]string(nil), path...), sub.Name()))
			}
		}
		root := NewRootCommand(NewOptions())
		// cobra 在执行时才给英文命令树补上 help 与 completion；这里先补上，两种语言列出的路径才一样
		root.InitDefaultHelpCmd()
		root.InitDefaultCompletionCmd()
		walk(root, nil)
		for _, path := range paths {
			r := runIn(t, t.TempDir(), append(append([]string(nil), path...), "--help")...)
			name := strings.Join(append([]string{"root"}, path...), "_") + ".txt"
			file := filepath.Join("testdata", "help", string(lang), name)
			if *updateHelp {
				require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
				require.NoError(t, os.WriteFile(file, []byte(r.stdout), 0o644))
				continue
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err, "没有基线：%s", file)
			require.Equal(t, string(want), r.stdout, "%s %v --help 变了", lang, path)
		}
	}
}
