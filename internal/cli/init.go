package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/version"
)

// newInitCommand 实现 brickkit init（004 §3.2）。
func newInitCommand(opts *Options) *cobra.Command {
	var noSkills bool
	cmd := &cobra.Command{
		Use:     "init <项目名称>",
		Short:   "初始化项目，生成 brickkit.yaml 骨架",
		GroupID: groupProject,
		Long: `在当前目录初始化一个 BrickKit 项目。

行为（004 §3.2）：
  1. 创建项目目录结构（.brickkit/、components/）
  2. 生成 brickkit.yaml 骨架
  3. 追加 .gitignore 规则（003 §11）
  4. 装入 AI 助手技能（.claude/skills/、AGENTS.md）

项目名称必须显式指定：只能包含小写字母、数字与中划线，
且以字母或数字开头结尾（用于 K8s namespace 与 Docker Network 命名）。

装入的技能只写「照常识会猜错」的东西——保留变量、健康检查禁令、
启停跟着上层走——参数一律不复刻，指向 --help。它们跟着项目提交、
团队共享；CLI 升级后用 brickkit skills update 刷新。
不想要就加 --no-skills。

不会碰你的 CLAUDE.md：那是你自己的流程文件。`,
		Example: `  brickkit init my-project
  brickkit init my-project --config brickkit.prod.yaml   初始化指定环境的配置
  brickkit init my-project --no-skills                   不装 AI 助手技能`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定项目名称：brickkit init <项目名称>").
					WithExit(clierr.ExitUsage)
			}
			return runInit(opts, args[0], noSkills)
		},
	}
	cmd.Flags().BoolVar(&noSkills, "no-skills", false,
		"不装 AI 助手技能（.claude/skills/、AGENTS.md）")
	return cmd
}

func runInit(opts *Options, project string, noSkills bool) error {
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

	if !noSkills {
		if err := installSkills(opts, layout); err != nil {
			return err
		}
	}

	opts.Printf("\n")
	opts.Printf("下一步：\n")
	opts.Printf("  brickkit add --local               把 components/ 下的组件全加进来\n")
	opts.Printf("  brickkit add people/basic@1.0.0    从安装源添加组件\n")
	opts.Printf("  brickkit up                        一键启动\n")
	return nil
}

// installSkills 装入 AI 助手技能，并把跳过的文件说清楚。
//
// 装不上是**错误**而不是静默跳过：init 说了它会装，那就得装上或者说明为什么没装。
// 但错误里要讲明项目本身已经建好了——否则人会以为整个 init 都白跑了。
func installSkills(opts *Options, layout config.Layout) error {
	in := skills.Installer{
		Root:     layout.Root,
		LockPath: layout.SkillsLockPath(),
		Version:  version.Version,
	}
	res, err := in.Apply()
	if err != nil {
		return clierr.New(clierr.CodeInternal, "错误：AI 助手技能装入失败").
			WithDetail("原因", err.Error()).
			WithHint(
				"项目本身已经初始化完成，只是技能没装上",
				"修好权限后执行 brickkit skills update 补装",
				"也可以不要它们：技能不影响 brickkit 的任何功能",
			).
			WithCause(err)
	}

	if len(res.Written) > 0 {
		opts.Printf("   📁 %-21s%s\n", ".claude/skills/", "AI 助手技能（4 个）")
		opts.Printf("   📁 %-21s%s\n", "AGENTS.md", "AI 助手项目导读")
	}
	// 跳过的必须说出来。默默不装，用户会以为装了、然后奇怪它为什么没效果。
	for _, s := range res.Skipped {
		opts.Printf("   ⏭  %-21s%s\n", s.Target, "已存在，未改动（"+string(s.State)+"）")
	}
	logging.Info("AI 助手技能已装入",
		"written", len(res.Written), "skipped", len(res.Skipped))
	return nil
}
