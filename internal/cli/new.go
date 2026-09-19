package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// newNewCommand 实现 brickkit new：生成一个新组件的最小骨架。
//
// 只生成一份能通过 Parse + Validate 的 component.yaml（外加可选的契约占位
// 文件），不生成 Dockerfile、不生成任何源码——平台语言无关，不替组件作者
// 选语言；起步代码由 docs/{en,zh}/04-go-component-template.md 这类"带读一个
// 真实组件"的文档承担。
func newNewCommand(opts *Options) *cobra.Command {
	var path string
	var contract string
	cmd := &cobra.Command{
		Use:     "new <scope>/<name>",
		Short:   "生成一个新组件的最小骨架",
		GroupID: groupComponent,
		Long: `生成一份能通过 brickkit up --dry-run 校验的 component.yaml 骨架。

默认写到 components/<scope>/<name>/ —— 本地安装源本来就按这个布局扫描
（<scope>/<name>/component.yaml），brickkit add --repo 克隆源码也放在这里，
不需要另外配置就能 brickkit add --local 把它加进项目。

--path 写到别的目录，给独立成一个 Git 仓库的组件用（一个组件一个仓库）：
这时目录本身就是组件仓库根，不再套 <scope>/<name> 这层。

生成之后不会自动 brickkit add：写进 brickkit.yaml 是一次单独、可审阅的
动作。也不会装 AI 助手技能——项目根已经有了，独立仓库场景请自己执行
brickkit skills update。`,
		Example: `  brickkit new demo/widget                          写到 components/demo/widget/
  brickkit new demo/widget --contract openapi       顺带生成一份 OpenAPI 契约占位文件
  brickkit new demo/widget --contract proto         顺带生成一份 proto 契约占位文件
  brickkit new demo/widget --path ../widget-repo    写到别的目录（独立仓库场景）`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(opts, args[0], path, contract)
		},
	}
	cmd.Flags().StringVar(&path, "path", "",
		"写到哪个目录（默认 components/<scope>/<name>/，写了这个就不再套那一层）")
	cmd.Flags().StringVar(&contract, "contract", "",
		"契约占位格式：openapi 或 proto（默认不生成）")
	return cmd
}

func runNew(opts *Options, id, path, contract string) error {
	files, err := manifest.Scaffold(id, manifest.ScaffoldOptions{Contract: contract})
	if err != nil {
		return err
	}

	rel := path
	if rel == "" {
		rel = filepath.Join(config.DirComponents, id)
	}
	dir := filepath.Join(opts.WorkDir, rel)

	if _, statErr := os.Stat(dir); statErr == nil {
		return clierr.New(clierr.CodeConfigInvalid, "错误：目标目录已存在").
			WithDetail("目录", dir).
			WithHint(
				"如果是误操作，请先删除或重命名该目录",
				"想写到别的地方，用 --path 指定",
			).
			WithExit(clierr.ExitUsage)
	}

	for _, f := range files {
		full := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return clierr.New(clierr.CodeInternal, "错误：创建目录失败").
				WithDetail("目录", filepath.Dir(full)).WithCause(err)
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			return clierr.New(clierr.CodeInternal, "错误：写入文件失败").
				WithDetail("文件", full).WithCause(err)
		}
	}

	opts.Printf("✅ 已生成组件骨架：%s\n", id)
	for _, f := range files {
		opts.Printf("   📄 %s\n", filepath.Join(rel, f.Path))
	}
	opts.Printf("\n")
	opts.Printf("下一步：\n")
	opts.Printf("  改完骨架里的 TODO\n")
	opts.Printf("  brickkit add --local               把它加进 brickkit.yaml（本地安装源里能扫到它的话）\n")
	opts.Printf("  brickkit up --dry-run               校验能不能通过\n")
	return nil
}
