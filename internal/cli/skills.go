package cli

// 本文件实现 brickkit skills：查看与刷新装进项目的 AI 助手技能。
//
// 它是本仓库第一条带子命令的命令。不带子命令时等于 status——
// 只读是安全的默认，敲错了不会改任何东西。

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/version"
)

// newSkillsCommand 实现 brickkit skills。
func newSkillsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Short:   "查看与刷新装进项目的 AI 助手技能",
		GroupID: groupProject,
		Long: `管理装进本项目的 AI 助手技能（.claude/skills/、AGENTS.md）。

这些文件由 brickkit init 装入，跟着项目提交、团队共享。它们描述的是
**当前这个 CLI 版本**的行为，所以 CLI 升级后需要刷新一次。

  brickkit skills          看每个文件的状态（只读，等同 status）
  brickkit skills status   同上
  brickkit skills update   缺的装上、旧的刷新

**手改过的文件绝不覆盖。** update 会把它们列出来并跳过。想放弃本地
修改，删掉那个文件再执行一次 update——刻意不提供 --force：删文件这个
动作本身已经足够明确，而多一个开关就多一条误伤路径。

不碰你的 CLAUDE.md：那是你自己的流程文件。`,
		Example: `  brickkit skills          看装了什么、有没有过期
  brickkit skills update   刷新到当前 CLI 版本`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsStatus(opts)
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:     "status",
			Short:   "查看每个技能文件的状态（只读）",
			Args:    cobra.NoArgs,
			Example: `  brickkit skills status`,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runSkillsStatus(opts)
			},
		},
		&cobra.Command{
			Use:     "update",
			Short:   "缺的装上、旧的刷新；手改过的跳过",
			Args:    cobra.NoArgs,
			Example: `  brickkit skills update`,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runSkillsUpdate(opts)
			},
		},
	)
	return cmd
}

// skillsInstaller 构造 Installer，并先确认这儿真是个 BrickKit 项目。
//
// 不确认的话，在随便一个目录里敲 skills update 会默默建出 .claude/ 与
// AGENTS.md——在别人家里留下文件，比报个错糟糕得多。
func skillsInstaller(opts *Options) (skills.Installer, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	if _, err := os.Stat(layout.ConfigPath()); err != nil {
		return skills.Installer{}, clierr.New(clierr.CodeProjectMissing,
			"错误：当前目录不是 BrickKit 项目").
			WithDetail("找不到", layout.ConfigName()).
			WithHint("先执行 brickkit init <项目名称>",
				"或用 --config 指定配置文件")
	}
	return skills.Installer{
		Root:     layout.Root,
		LockPath: layout.SkillsLockPath(),
		Version:  version.Version,
	}, nil
}

func runSkillsStatus(opts *Options) error {
	in, err := skillsInstaller(opts)
	if err != nil {
		return err
	}
	list, err := in.Status()
	if err != nil {
		return wrapSkillsError(err)
	}

	t := newTable("文件", "状态")
	stale := 0
	for _, s := range list {
		state := string(s.State)
		switch s.State {
		case skills.StateOutdated:
			state = state + "（" + s.FromVersion + " → " + version.Version + "）"
			stale++
		case skills.StateMissing:
			stale++
		case skills.StateModified, skills.StateUntracked:
			state = state + "，update 会跳过"
		}
		t.add(s.Target, state)
	}
	opts.Printf("%s", t.render("   "))
	if stale > 0 {
		opts.Printf("\n有 %d 个文件需要刷新：brickkit skills update\n", stale)
	}
	return nil
}

func runSkillsUpdate(opts *Options) error {
	in, err := skillsInstaller(opts)
	if err != nil {
		return err
	}
	res, err := in.Apply()
	if err != nil {
		return wrapSkillsError(err)
	}

	if len(res.Written) == 0 && len(res.Skipped) == 0 {
		opts.Printf("✅ AI 助手技能已是最新（CLI %s）\n", version.Display())
		return nil
	}

	opts.Printf("✅ AI 助手技能已更新\n")
	if len(res.Written) > 0 {
		opts.Printf("   已写入 %d 个：\n", len(res.Written))
		for _, w := range res.Written {
			opts.Printf("     %s\n", w)
		}
	}
	if len(res.Skipped) > 0 {
		opts.Printf("   已跳过 %d 个：\n", len(res.Skipped))
		for _, s := range res.Skipped {
			opts.Printf("     %s（%s）\n", s.Target, s.State)
		}
		opts.Printf("\n提示：想放弃本地修改，删掉那个文件后重新执行 brickkit skills update\n")
	}
	logging.Info("AI 助手技能已刷新",
		"written", len(res.Written), "skipped", len(res.Skipped))
	return nil
}

func wrapSkillsError(cause error) error {
	return clierr.New(clierr.CodeInternal, "错误：读写 AI 助手技能失败").
		WithDetail("原因", cause.Error()).
		WithHint("检查目录权限与磁盘空间").
		WithCause(cause)
}
