package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/version"
)

// newInitCommand 实现 brickkit init（004 §3.2）。
func newInitCommand(opts *Options) *cobra.Command {
	var noSkills bool
	var hooksOnly bool
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

不会碰你的 CLAUDE.md：那是你自己的流程文件。

组件源码要跟项目一起进 Git 的话，还会装一个 pre-commit hook，
拦住"源码归档了、而 brickkit.yaml 没跟着提交"这个失误（004 §3.14）。
它只在项目根就是仓库根时自动装——嵌套在别人仓库里的项目，
用 brickkit init --hooks 显式补装。`,
		Example: `  brickkit init my-project
  brickkit init my-project --config brickkit.prod.yaml   初始化指定环境的配置
  brickkit init my-project --no-skills                   不装 AI 助手技能
  brickkit init --hooks                                  只补装 pre-commit hook`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if hooksOnly {
				if len(args) > 0 {
					return clierr.New(clierr.CodeInvalidArgument,
						"brickkit init --hooks 只安装 pre-commit hook，不需要项目名称").
						WithExit(clierr.ExitUsage)
				}
				return installCommitHook(opts, config.NewLayout(opts.WorkDir, opts.ConfigPath), true)
			}
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定项目名称：brickkit init <项目名称>").
					WithExit(clierr.ExitUsage)
			}
			return runInit(opts, args[0], noSkills)
		},
	}
	cmd.Flags().BoolVar(&noSkills, "no-skills", false,
		"不装 AI 助手技能（.claude/skills/、AGENTS.md）")
	cmd.Flags().BoolVar(&hooksOnly, "hooks", false,
		"只安装提交前检查用的 pre-commit hook（在已有项目里补装）")
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

	if err := installCommitHook(opts, layout, false); err != nil {
		return err
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

// installCommitHook 装提交前检查用的 pre-commit hook（004 §3.14）。
//
// # 为什么 init 顺带装时要多一条"项目根 == 仓库根"
//
// init 完全可能跑在一个**跟本项目无关的仓库**的子目录里——本仓库的
// 试用指南/playground/ 就是这样（它在 brickKit 自己的仓库里）。那时候
// 自动装等于往别人的 .git/hooks 里写东西，而那个人根本没要求过。
//
// 所以顺带装只服务最常见的那一种：项目根就是仓库根。嵌套的项目要装，
// 就自己显式说一句 brickkit init --hooks——那时 explicit 为真，装不上是错误。
func installCommitHook(opts *Options, layout config.Layout, explicit bool) error {
	repo, err := gitrepo.Open(layout.Root)
	if err != nil {
		if explicit {
			return clierr.New(clierr.CodeConfigInvalid, "错误：这里不是一个 git 仓库").
				WithHint(
					"pre-commit hook 只能装进 git 仓库",
					"先 git init，再执行 brickkit init --hooks",
				)
		}
		hookHint(opts)
		return nil
	}

	rel, ok := repo.Rel(layout.Root)
	if !ok {
		if explicit {
			return clierr.New(clierr.CodeConfigInvalid, "错误：项目根不在这个 git 仓库里").
				WithDetail("项目", layout.Root).
				WithDetail("仓库", repo.Root())
		}
		hookHint(opts)
		return nil
	}
	if !explicit && rel != "." {
		// 项目嵌在别人的仓库里：不替他决定往那个仓库写东西
		hookHint(opts)
		return nil
	}

	path, added, err := installHook(repo,
		hookProject{Dir: rel, Config: layout.ConfigName()}, brickkitBinPath(), version.Version)
	if err != nil {
		return err
	}

	display := path
	if p, ok := repo.Rel(path); ok {
		display = p
	}
	if !added && explicit {
		// 说"已经装过了"会误导：这一次**确实**重写了文件——版本戳与写死在
		// 里面的可执行文件绝对路径都跟着刷新了。升级 CLI 之后跑这条命令的人
		// 要的正是这个刷新，看到"已经装过了"却会以为什么都没发生。
		opts.Printf("✅ pre-commit hook 已刷新到当前版本（%s）：%s\n", version.Version, display)
		return nil
	}
	opts.Printf("   🪝 %-21s%s\n", display, "提交前检查组件结构（004 §3.14）")
	return nil
}

// hookHint 在没自动装上时说清怎么补装。
//
// 装不上是**常态而不是错误**：init 常常跑在 git init 之前，项目也常常嵌在
// 别人的仓库里。但不说一声，使用者就永远不知道有这道闸门。
func hookHint(opts *Options) {
	opts.Printf("   💡 %s\n",
		"组件源码要跟项目一起进 Git 的话：brickkit init --hooks 装上提交前检查")
}

// brickkitBinPath 返回当前可执行文件的绝对路径，取不到就回落到裸名字。
//
// 把绝对路径写进 hook 是必要的：GUI 客户端（VS Code 的源代码管理面板、
// macOS 上从 Finder 启动的客户端）的 PATH 常常不含 ~/.local/bin，
// 只写 "brickkit" 会让 hook 在那些地方一律走"找不到就放行"那一支。
func brickkitBinPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "brickkit"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}
