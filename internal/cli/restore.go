package cli

// 本文件实现 brickkit restore（004 §3.14）：把 brickkit.yaml 的 enabled 与组件
// 源码结构还原到最后一次提交，以及供 pre-commit hook 调用的 --check。

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newRestoreCommand 实现 brickkit restore（004 §3.14）。
func newRestoreCommand(opts *Options) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:     "restore",
		Short:   "把 brickkit.yaml 的 enabled 与组件源码结构还原到最后一次提交",
		GroupID: groupProject,
		Long: `把 brickkit.yaml 里各组件的 enabled 还原成最后一次提交的值，再让源码结构跟着走（004 §3.14）。

给谁用：把 components/ 从 .gitignore 去掉、让组件源码跟项目一起进版本库的项目。
那种项目里 brickkit sync 移动目录会进项目的 diff，而"本地关掉几个顶层、
sync 归档、干完活忘了还原就提交"这件事会反复发生。

它只动 enabled 这一个字段，逐条动：

  - 工作区与最后一次提交都有的条目 → enabled 回到提交里的值（提交里没写就删掉字段）
  - 工作区新增的条目（刚 add 的、或改了版本号）→ 一个字不动
  - 提交里有而工作区没有的 → 绝不加回来（它不是 git revert）

其余改动一律不碰，所以刚 add 的组件不会被它吃掉。被覆盖的旧值会在动手前印出来。

--check 只检查、不改任何东西：即将提交的 yaml 与即将提交的目录结构自洽吗。
不自洽就非零退出——pre-commit hook 调的就是它（brickkit init --hooks 装）。`,
		Example: `  brickkit restore           还原 enabled 与源码结构
  brickkit restore --check   只检查这次提交自洽不自洽（退出码非零表示不自洽）`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if check {
				return runRestoreCheck(cmd.Context(), opts)
			}
			return runRestore(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false,
		"只检查即将提交的 yaml 与目录结构是否自洽，不改任何东西（供 pre-commit hook 调用）")
	return cmd
}

// runRestore 在 Task 5 落地。这里绝不返回 nil：一个说自己会还原、实际什么都不做的
// 命令，比一个报错的命令糟得多。
func runRestore(ctx context.Context, opts *Options) error {
	return clierr.New(clierr.CodeNotImplemented, "错误：brickkit restore 尚未实现").
		WithHint("当前版本只提供 brickkit restore --check")
}
