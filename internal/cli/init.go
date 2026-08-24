package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
)

// newInitCommand 实现 brickkit init（004 §3.2）。
func newInitCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "init <项目名称>",
		Short:   "初始化项目，生成 brickkit.yaml 骨架",
		GroupID: groupProject,
		Long: `在当前目录初始化一个 BrickKit 项目。

行为（004 §3.2）：
  1. 创建项目目录结构（.brickkit/、components/）
  2. 生成 brickkit.yaml 骨架
  3. 追加 .gitignore 规则（003 §11）

项目名称必须显式指定：只能包含小写字母、数字与中划线，
且以字母或数字开头结尾（用于 K8s namespace 与 Docker Network 命名）。`,
		Example: `  brickkit init my-project
  brickkit init my-project --config brickkit.prod.yaml   初始化指定环境的配置`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定项目名称：brickkit init <项目名称>").
					WithExit(clierr.ExitUsage)
			}
			return runInit(opts, args[0])
		},
	}
	return cmd
}

func runInit(opts *Options, project string) error {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	result, err := config.InitProject(layout, project)
	if err != nil {
		return err
	}

	logging.Info("项目已初始化",
		"project", result.ProjectName,
		"config", layout.ConfigPath(),
		"gitignore_updated", result.GitignoreUpdated,
	)

	// 输出格式与 004 §3.2 / 009 / 011 中的样例逐字一致。
	opts.Printf("✅ 项目已初始化：%s\n", result.ProjectName)
	opts.Printf("   📁 %-21s%s\n", result.ConfigName, "项目配置")
	// components/ 一直都在建，只是从前没说过；现在它还是默认安装源，更该点出来
	opts.Printf("   📁 %-21s%s\n", config.DirComponents+"/", "组件源码（已配为本地安装源 local-dev）")
	opts.Printf("   📁 %-21s%s\n", config.DirBrickkit+"/", "CLI 工作目录")
	opts.Printf("\n")
	opts.Printf("下一步：\n")
	opts.Printf("  brickkit add --local               把 components/ 下的组件全加进来\n")
	opts.Printf("  brickkit add people/basic@1.0.0    从安装源添加组件\n")
	opts.Printf("  brickkit up                        一键启动\n")
	return nil
}
