package cli

// 本文件实现 brickkit skills：查看与刷新装进项目的 AI 助手技能。
//
// 它是本仓库第一条带子命令的命令。不带子命令时等于 status——
// 只读是安全的默认，敲错了不会改任何东西。

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/version"
)

// newSkillsCommand 实现 brickkit skills。
func newSkillsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Short:   i18n.T(msgid.CliSkillsShort),
		GroupID: groupProject,
		Long:    i18n.T(msgid.CliSkillsLong),
		Example: i18n.T(msgid.CliSkillsExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsStatus(opts)
		},
	}
	var lang string
	updateCmd := &cobra.Command{
		Use:     "update",
		Short:   i18n.T(msgid.CliSkillsShort3),
		Args:    cobra.NoArgs,
		Example: "  brickkit skills update\n  brickkit skills update --lang zh",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsUpdate(opts, lang)
		},
	}
	updateCmd.Flags().StringVar(&lang, "lang", "", i18n.T(msgid.CliSkillsLangFlag))

	cmd.AddCommand(
		&cobra.Command{
			Use:     "status",
			Short:   i18n.T(msgid.CliSkillsShort2),
			Args:    cobra.NoArgs,
			Example: `  brickkit skills status`,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runSkillsStatus(opts)
			},
		},
		updateCmd,
	)
	return cmd
}

// detectScope 判断当前目录是 BrickKit 项目（有 brickkit.yaml）还是独立组件仓库
// （有 component.yaml、没有 brickkit.yaml）；两者都没有时返回错误。
//
// 两者都有时按项目算：那是 brickkit skills 一直以来的行为，lint 沿用同一条规则，
// 不新发明一条。返回的 Layout 无论成败都有效——调用方走哪一支都要用它定位文件。
// 出错时返回的 Scope 只是占位，没有含义：调用方必须先检查 err。
func detectScope(opts *Options) (skills.Scope, config.Layout, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	if _, err := os.Stat(layout.ConfigPath()); err == nil {
		return skills.ScopeProject, layout, nil
	}
	if _, err := os.Stat(filepath.Join(layout.Root, manifest.FileName)); err == nil {
		return skills.ScopeComponent, layout, nil
	}
	return skills.ScopeProject, layout, clierr.New(clierr.CodeProjectMissing,
		i18n.T(msgid.CliSkillsErrorTheCurrentDirectoryIs)).
		WithDetail(i18n.T(msgid.CliSkillsNotFound), i18n.T(msgid.CliSkillsAndNoEither, layout.ConfigName(), manifest.FileName)).
		WithHint(i18n.T(msgid.CliSkillsProjectRunBrickkitInitProject),
			i18n.T(msgid.CliSkillsComponentRepositoryRunItIn, manifest.FileName))
}

// skillsInstaller 构造 Installer，并先确认这儿是它管得着的地方：
// BrickKit 项目（有 brickkit.yaml），或独立的组件仓库（有 component.yaml、没有 brickkit.yaml）。
//
// 不确认的话，在随便一个目录里敲 skills update 会默默建出 .claude/ 与
// AGENTS.md——在别人家里留下文件，比报个错糟糕得多。
// langOverride 为空字符串时不指定语言（Installer 沿用项目已记录的语言）；
// 否则必须是受支持的语言，供 `skills update --lang` 用。
func skillsInstaller(opts *Options, langOverride string) (skills.Installer, error) {
	scope, layout, err := detectScope(opts)
	if err != nil {
		return skills.Installer{}, err
	}
	in := skills.Installer{
		Root:     layout.Root,
		LockPath: layout.SkillsLockPath(),
		Version:  version.Version,
		Scope:    scope,
	}
	if langOverride != "" {
		lang, ok := i18n.ParseLang(langOverride)
		if !ok {
			return skills.Installer{}, clierr.Newf(clierr.CodeInvalidArgument,
				i18n.T(msgid.LangInvalidValue, langOverride, langNamesJoined())).
				WithExit(clierr.ExitUsage)
		}
		in.Lang = lang
	}
	return in, nil
}

// renderSkillsScope 在组件仓库模式下说一句"为什么只有一个文件"。
func renderSkillsScope(opts *Options, in skills.Installer) {
	if in.Scope == skills.ScopeComponent {
		opts.Printf("%s\n", i18n.T(msgid.CliSkillsComponentRepositoryHasNoOnly, manifest.FileName, config.NewLayout(opts.WorkDir, opts.ConfigPath).ConfigName()))
	}
}

func runSkillsStatus(opts *Options) error {
	in, err := skillsInstaller(opts, "")
	if err != nil {
		return err
	}
	list, err := in.Status()
	if err != nil {
		return wrapSkillsError(err)
	}
	lang, err := in.ResolvedLang()
	if err != nil {
		return wrapSkillsError(err)
	}

	renderSkillsScope(opts, in)
	opts.Printf("%s\n", i18n.T(msgid.CliSkillsLanguageLine, string(lang)))
	t := newTable(i18n.T(msgid.LabelFile), i18n.T(msgid.CliSkillsStatus))
	stale := 0
	for _, s := range list {
		state := s.State.Label()
		switch s.State {
		case skills.StateOutdated:
			state = i18n.T(msgid.CliSkillsMsg, state, s.FromVersion, version.Version)
			stale++
		case skills.StateMissing:
			stale++
		case skills.StateModified, skills.StateUntracked:
			state = i18n.T(msgid.CliSkillsUpdateWillSkipIt, state)
		}
		t.add(s.Target, state)
	}
	opts.Printf("%s", t.render("   "))
	if stale > 0 {
		opts.Printf("\n%s\n", i18n.TN(msgid.CliSkillsFilesNeedRefreshing, stale, stale))
	}
	return nil
}

func runSkillsUpdate(opts *Options, lang string) error {
	in, err := skillsInstaller(opts, lang)
	if err != nil {
		return err
	}
	res, err := in.Apply()
	if err != nil {
		return wrapSkillsError(err)
	}

	renderSkillsScope(opts, in)
	if lang != "" {
		// 只在显式 --lang 时才提；裸的 update 每次都印会很吵，且不带信息量——
		// 项目的语言本来就没变。
		opts.Printf("%s\n", i18n.T(msgid.CliSkillsLanguageLine, string(in.Lang)))
	}
	if len(res.Written) == 0 && len(res.Skipped) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliSkillsAiAssistantSkillsAreUp, version.Display()))
		return nil
	}

	opts.Printf("%s\n", i18n.T(msgid.CliSkillsAiAssistantSkillsUpdated))
	if len(res.Written) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliSkillsWrote, len(res.Written)))
		for _, w := range res.Written {
			opts.Printf("     %s\n", w)
		}
	}
	if len(res.Skipped) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliSkillsSkipped, len(res.Skipped)))
		for _, s := range res.Skipped {
			opts.Printf("%s\n", i18n.T(msgid.CliSkillsMsg2, s.Target, s.State.Label()))
		}
		opts.Printf("\n%s\n", i18n.T(msgid.CliSkillsNoteToDiscardLocalEdits))
	}
	logging.Info(i18n.T(msgid.LogSkillsRefreshed),
		"written", len(res.Written), "skipped", len(res.Skipped))
	return nil
}

func wrapSkillsError(cause error) error {
	return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliSkillsErrorFailedToReadOr)).
		WithDetail(i18n.T(msgid.LabelReason), cause.Error()).
		WithHint(i18n.T(msgid.HintCheckDiskAccess)).
		WithCause(cause)
}
