package cli

// 本文件实现 brickkit local：管理个人本地部署文件 deploy.local.yaml（提案 §6.2–6.6，附录 A1、A16）。
//
//	on       开启本地模式；文件不存在时从 deploy.yaml 复制（团队文件头换成个人文件头，见 project.LocalDeployContent），已存在时沿用（off 不删它）
//	off      关闭本地模式：只停止读取，文件留着
//	status   开关、文件、以及（开着时）与 brickkit.yaml 是否一致
//	refresh  备份旧文件、重新复制，列出旧文件里的本地修改——CLI 不合并，完整替换（附录 A1）
//
// 开关记在 .brickkit/local-mode（附录 A16）。不带子命令时等于 status：只读是安全的默认。

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

func newLocalCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "local",
		Short:   i18n.T(msgid.CliLocalShort),
		GroupID: groupLifecycle,
		Long:    i18n.T(msgid.CliLocalLong),
		Example: i18n.T(msgid.CliLocalExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocalStatus(opts)
		},
	}
	sub := func(use, short string, run func(*Options) error) *cobra.Command {
		return &cobra.Command{
			Use:     use,
			Short:   short,
			Args:    cobra.NoArgs,
			Example: "  brickkit local " + use,
			RunE: func(cmd *cobra.Command, args []string) error {
				return run(opts)
			},
		}
	}
	cmd.AddCommand(
		sub("on", i18n.T(msgid.CliLocalOnShort), runLocalOn),
		sub("off", i18n.T(msgid.CliLocalOffShort), runLocalOff),
		sub("status", i18n.T(msgid.CliLocalStatusShort), runLocalStatus),
		sub("refresh", i18n.T(msgid.CliLocalRefreshShort), runLocalRefresh),
	)
	return cmd
}

// localLayout 确认当前目录是项目：本地文件是 deploy.yaml 的副本，没有 deploy.yaml 无从谈起。
func localLayout(opts *Options) (project.Layout, error) {
	l := project.NewLayout(opts.WorkDir)
	if _, err := os.Stat(l.DeployPath()); err != nil {
		return l, clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.CliLocalNoDeployFile, project.FileDeploy)).
			WithDetail(i18n.T(msgid.LabelPath), l.DeployPath()).
			WithHint(i18n.T(msgid.CliLocalHintInit))
	}
	return l, nil
}

func runLocalOn(opts *Options) error {
	l, err := localLayout(opts)
	if err != nil {
		return err
	}
	on, err := project.LocalModeOn(l)
	if err != nil {
		return err
	}
	copied := false
	if !fileExists(l.DeployLocalPath()) {
		if err := copyDeployToLocal(l); err != nil {
			return err
		}
		copied = true
	}
	if on && !copied {
		opts.Printf("%s\n", i18n.T(msgid.CliLocalAlreadyOn, project.FileDeployLocal))
		return nil
	}
	if err := project.SetLocalMode(l, true); err != nil {
		return localSwitchError(l, err)
	}
	opts.Printf("%s\n", i18n.T(msgid.CliLocalOn, project.FileDeployLocal))
	if copied {
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalCopied, project.FileDeployLocal, project.FileDeploy))
	} else {
		// 附录 A16：off 留下了文件，再开时沿用——那里面是使用者的本地修改，绝不覆盖
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalReused, project.FileDeployLocal))
		opts.Printf("   💡 %s\n", i18n.T(msgid.CliLocalHintRefresh))
	}
	return nil
}

func runLocalOff(opts *Options) error {
	l, err := localLayout(opts)
	if err != nil {
		return err
	}
	on, err := project.LocalModeOn(l)
	if err != nil {
		return err
	}
	if !on {
		opts.Printf("%s\n", i18n.T(msgid.CliLocalAlreadyOff, project.FileDeploy))
		return nil
	}
	if err := project.SetLocalMode(l, false); err != nil {
		return localSwitchError(l, err)
	}
	opts.Printf("%s\n", i18n.T(msgid.CliLocalOff, project.FileDeploy))
	if fileExists(l.DeployLocalPath()) {
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalFileKept, project.FileDeployLocal))
	}
	return nil
}

func runLocalStatus(opts *Options) error {
	l, err := localLayout(opts)
	if err != nil {
		return err
	}
	on, err := project.LocalModeOn(l)
	if err != nil {
		return err
	}
	has := fileExists(l.DeployLocalPath())
	state := i18n.T(msgid.CliLocalStateOff)
	if on {
		state = i18n.T(msgid.CliLocalStateOn)
	}
	opts.Printf("%s\n", i18n.T(msgid.CliLocalStatusMode, state))
	switch {
	case !has:
		opts.Printf("%s\n", i18n.T(msgid.CliLocalStatusFileNone, project.FileDeployLocal))
	case on:
		opts.Printf("%s\n", i18n.T(msgid.CliLocalStatusFileInUse, project.FileDeployLocal))
	default:
		opts.Printf("%s\n", i18n.T(msgid.CliLocalStatusFileIdle, project.FileDeployLocal))
	}
	if !on {
		return nil
	}
	// 与 up 同一处校验（提案 §6.3）：过期就把那一块原样打印出来，status 本身不算失败
	if _, err := project.Load(l.Root, project.LoadOptions{}); err != nil {
		opts.Printf("%s", clierr.As(err).Format())
		return nil
	}
	opts.Printf("%s\n", i18n.T(msgid.CliLocalStatusConsistent, project.FileDeployLocal, project.FileDecl))
	return nil
}

func runLocalRefresh(opts *Options) error {
	l, err := localLayout(opts)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(l.DeployLocalPath())
	if errors.Is(err, fs.ErrNotExist) {
		return clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.CliLocalNothingToRefresh, project.FileDeployLocal)).
			WithHint(i18n.T(msgid.CliLocalHintOn))
	}
	if err != nil {
		return localIOError(l.DeployLocalPath(), err)
	}
	team, err := os.ReadFile(l.DeployPath())
	if err != nil {
		return localIOError(l.DeployPath(), err)
	}
	fresh := project.LocalDeployContent(team)
	// 团队文件本身写坏了：复制过来只会把一份能用的本地文件换成坏的，再刷新一次连备份也没了。
	// 一个文件都不动，点名 deploy.yaml
	if _, _, err := deployfile.ParseFile(l.DeployPath(), deployfile.RoleTeam); err != nil {
		inner := clierr.As(err)
		e := clierr.New(inner.Code, i18n.T(msgid.CliLocalTeamFileInvalid, project.FileDeploy, project.FileDeployLocal)).
			WithDetail(i18n.T(msgid.LabelReason), strings.TrimPrefix(strings.TrimSpace(inner.Message), "❌ "))
		for _, d := range inner.Details {
			e = e.WithDetail(d.Key, d.Value)
		}
		return e.WithHint(i18n.T(msgid.CliLocalHintFixTeamFile, project.FileDeploy)).WithCause(err)
	}
	if bytes.Equal(old, fresh) {
		opts.Printf("%s\n", i18n.T(msgid.CliLocalAlreadyFresh, project.FileDeployLocal, project.FileDeploy))
		return nil
	}
	// 先算摘要：旧文件读不懂（YAML 写坏了）也要能刷新——那正是最需要重新生成的时候
	changes, diffErr := deployfile.DiffLocal(old, fresh)

	replacedBackup := fileExists(l.DeployLocalBackupPath())
	if err := os.WriteFile(l.DeployLocalBackupPath(), old, 0o644); err != nil {
		return localIOError(l.DeployLocalBackupPath(), err)
	}
	if err := os.WriteFile(l.DeployLocalPath(), fresh, 0o644); err != nil {
		return localIOError(l.DeployLocalPath(), err)
	}

	opts.Printf("%s\n", i18n.T(msgid.CliLocalRefreshed, project.FileDeployLocal, project.FileDeploy, project.FileDeployLocalBackup))
	if replacedBackup {
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalBackupReplaced, project.FileDeployLocalBackup))
	}
	switch {
	case diffErr != nil:
		opts.Printf("⚠️ %s\n", i18n.T(msgid.CliLocalDiffFailed, project.FileDeployLocalBackup, diffErr.Error()))
	case len(changes) == 0:
		opts.Printf("ℹ️ %s\n", i18n.T(msgid.CliLocalNoChanges))
	default:
		opts.Printf("ℹ️ %s\n", i18n.T(msgid.CliLocalChangesHeader, i18n.Count(msgid.CountLocalChanges, len(changes)), project.FileDeployLocal))
		for _, c := range changes {
			opts.Printf("   - %s\n", renderLocalChange(c))
		}
	}
	return nil
}

func renderLocalChange(c deployfile.LocalChange) string {
	switch {
	case c.Field == "":
		return i18n.T(msgid.CliLocalChangeEntryGone, c.Scope, project.FileDeploy)
	case c.Field == deployfile.FieldPlacement:
		return i18n.T(msgid.CliLocalChangePlacement, c.Scope, placement(c.Old), placement(*c.New))
	case c.New == nil:
		return i18n.T(msgid.CliLocalChangeUnset, c.Scope, c.Field, c.Old)
	default:
		return i18n.T(msgid.CliLocalChangeDiffers, c.Scope, c.Field, c.Old, *c.New)
	}
}

// placement 把条目所在位置说成人话：顶层，或者在某个外壳下面。
func placement(shell string) string {
	if shell == "" {
		return i18n.T(msgid.CliLocalPlacementTop)
	}
	return i18n.T(msgid.CliLocalPlacementShell, shell)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// copyDeployToLocal 写出第一份 deploy.local.yaml（内容见 project.LocalDeployContent）。
func copyDeployToLocal(l project.Layout) error {
	data, err := os.ReadFile(l.DeployPath())
	if err != nil {
		return localIOError(l.DeployPath(), err)
	}
	if err := os.WriteFile(l.DeployLocalPath(), project.LocalDeployContent(data), 0o644); err != nil {
		return localIOError(l.DeployLocalPath(), err)
	}
	return nil
}

func localIOError(path string, cause error) error {
	return clierr.New(clierr.CodeInternal, i18n.T(msgid.IOFailed, i18n.T(msgid.ActionWriteFile))).
		WithDetail(i18n.T(msgid.LabelPath), path).
		WithDetail(i18n.T(msgid.LabelReason), cause.Error()).
		WithHint(i18n.T(msgid.HintCheckDiskAccess)).
		WithCause(cause)
}

func localSwitchError(l project.Layout, cause error) error {
	return localIOError(l.LocalModePath(), cause)
}
