package cli

// 本文件实现 brickkit restore：把 deploy.yaml 的 mode 与组件
// 源码结构还原到最后一次提交，以及供 pre-commit hook 调用的 --check。

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/workspace"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// newRestoreCommand 实现 brickkit restore。
func newRestoreCommand(opts *Options) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:     "restore",
		Short:   i18n.T(msgid.CliRestoreShort),
		GroupID: groupProject,
		Long:    i18n.T(msgid.CliRestoreLong),
		Example: i18n.T(msgid.CliRestoreExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if check {
				return runRestoreCheck(cmd.Context(), opts)
			}
			return runRestore(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false,
		i18n.T(msgid.CliRestoreOnlyCheckWhetherTheYaml))
	return cmd
}

// modeChange 是一处 mode 还原。
type modeChange struct {
	// entry 是部署文件里那一条的 id 原文（`people/basic` 或 `people/basic@1.0.0`）。
	entry string
	// from 是工作区当前的值（"" = 没写），只用于如实汇报被覆盖的旧值。
	from string
	// to 是要设成的值；"" 表示**删掉这个字段**（最后一次提交里没写）——
	// mode 本身的零值就是"没写"，不需要再用指针区分"没写"与"写了空值"。
	to string
}

// restorePlan 算出 deploy.yaml 里要改哪些 mode。纯函数。
//
// 按条目的 id 原文配对（裸 id 与 id@version 是两条不同的条目），只动
// "工作区与 HEAD 都有的同一条"，另外两种刻意不动：
//
//	工作区新增的条目     本地刚 add 的、或本地把裸 id 改成了带版本的。一个字不动——
//	                    这是"不吃掉未提交的 add"的解药
//	HEAD 有而工作区没有   本地 remove 掉的。绝不加回来——restore 不是 revert
//
// 返回的 untouched 是那些"工作区有、提交里没有"的条目，要在输出里点名说
// "未动"：使用者得知道为什么它没变，否则会以为命令漏了它。
func restorePlan(work, head *deployfile.File) ([]modeChange, []string) {
	headMode := map[string]string{}
	for _, c := range head.All() {
		headMode[c.ID] = c.Mode
	}

	var changes []modeChange
	var untouched []string
	for _, c := range work.All() {
		want, ok := headMode[c.ID]
		if !ok {
			untouched = append(untouched, c.ID)
			continue
		}
		if c.Mode == want {
			continue
		}
		changes = append(changes, modeChange{entry: c.ID, from: c.Mode, to: want})
	}
	return changes, untouched
}

// applyMode 把还原结果写进**内存里**的部署文件。
func applyMode(f *deployfile.File, changes []modeChange) {
	for _, ch := range changes {
		for i := range f.Components {
			c := &f.Components[i]
			if c.ID == ch.entry {
				c.Mode = ch.to
			}
			for j := range c.Members {
				if c.Members[j].ID == ch.entry {
					c.Members[j].Mode = ch.to
				}
			}
		}
	}
}

// runRestore 执行 brickkit restore。
//
// 还原的是团队文件 deploy.yaml——它才进版本库、才是提交里的那份 mode。
// deploy.local.yaml 是个人文件，不进 git，没有"最后一次提交"可言；
// 本地模式开着时点一句，免得使用者以为本地那份也被还原了。
//
// # 顺序是硬约束
//
//	解析工作区文件 → 内存里还原 mode → 算判定 → 落盘 deploy.yaml → 移动目录
//
// 反过来（先落盘再算判定）就会在 Manifest 缺失或需要联网时留下一个
// "yaml 改了、结构没动"的半成品——而那正是使用者最不希望在提交前撞上的状态。
// 按这个顺序，判定失败时 yaml 一个字没改，重跑即可。
func runRestore(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	layout := project.NewLayout(opts.WorkDir)

	repo, deployRel, err := restoreBaseline(layout)
	if err != nil {
		return err
	}

	decl, err := projfile.ParseFile(layout.DeclPath())
	if err != nil {
		return err
	}
	work, _, err := deployfile.ParseFile(layout.DeployPath(), deployfile.RoleTeam)
	if err != nil {
		return err
	}
	if err := restorePreflight(repo, layout, decl.IDs()); err != nil {
		return err
	}

	headData, err := repo.HeadBlob(deployRel)
	if err != nil {
		return restoreErr(i18n.T(msgid.CliRestoreCannotReadFromTheLast, project.FileDeploy), err).
			WithHint(i18n.T(msgid.CliRestoreMakeSureItExistsIn, deployRel))
	}
	head, _, err := deployfile.Parse(headData, "HEAD:"+deployRel, deployfile.RoleTeam)
	if err != nil {
		return restoreErr(i18n.T(msgid.CliRestoreInTheLastCommitIs, project.FileDeploy), err).
			WithHint(
				i18n.T(msgid.CliRestoreTheBaselineForRestoringIs),
				i18n.T(msgid.CliRestoreFirstCommitAVersionThat),
			)
	}

	changes, untouched := restorePlan(work, head)

	applyMode(work, changes)
	proj, err := project.Assemble(layout, decl, work)
	if err != nil {
		return err
	}
	f, err := syncFocus(ctx, opts, proj)
	if err != nil {
		return err
	}

	printModeChanges(opts, changes, untouched)
	if err := writeMode(layout, changes); err != nil {
		return err
	}
	if err := applyWorkspacePlan(opts, layout, planSync(layout, decl.IDs(), f)); err != nil {
		return err
	}
	noteLocalMode(opts, layout)
	return nil
}

// noteLocalMode 在本地模式开着时提醒：deploy.local.yaml 没被还原。
func noteLocalMode(opts *Options, layout project.Layout) {
	if on, err := project.LocalModeOn(layout); err == nil && on {
		opts.Printf("%s\n", i18n.T(msgid.CliRestoreLocalModeUntouched, project.FileDeployLocal))
	}
}

// restoreBaseline 找出"最后一次提交"这个基准，没有基准就说清楚。
func restoreBaseline(layout project.Layout) (*gitrepo.Repo, string, error) {
	repo, err := gitrepo.Open(layout.Root)
	if err != nil {
		return nil, "", restoreErr(i18n.T(msgid.CliRestoreThisIsNotAGit), err).
			WithHint(
				i18n.T(msgid.CliRestoreBrickkitRestorePutsTheConfig),
				i18n.T(msgid.CliRestoreIfYouWantToNarrow),
			)
	}
	if repo.Unmerged() {
		return nil, "", clierr.New(clierr.CodeConfigConflict, i18n.T(msgid.CliRestoreErrorAConflictIsBeing)).
			WithHint(i18n.T(msgid.CliRestoreBrickkitRestoreHasToRead))
	}
	if !repo.HasHEAD() {
		return nil, "", clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliRestoreErrorThisRepositoryHasNo)).
			WithHint(i18n.T(msgid.CliRestoreTheBaselineForRestoringIs2))
	}
	deployRel, ok := repo.Rel(layout.DeployPath())
	if !ok {
		return nil, "", clierr.New(clierr.CodeConfigInvalid,
			i18n.T(msgid.CliRestoreErrorIsNotInsideThis, project.FileDeploy)).
			WithDetail(i18n.T(msgid.LabelPath), layout.DeployPath()).
			WithDetail(i18n.T(msgid.LabelRepo), repo.Root()).WithHint(i18n.T(msgid.CliRestoreHintInsideRepo))
	}
	if !repo.Tracked(deployRel) {
		return nil, "", clierr.New(clierr.CodeConfigInvalid,
			i18n.T(msgid.CliRestoreErrorIsNotTrackedBy, project.FileDeploy)).
			WithHint(i18n.T(msgid.CliRestoreFirstRunGitAddAnd, deployRel))
	}
	return repo, deployRel, nil
}

// restorePreflight 拦下两种"动手就会出事"的现场。
func restorePreflight(repo *gitrepo.Repo, layout project.Layout, ids []string) error {
	// ① components/ 下有已暂存的改动
	//
	// 设计上允许直接在 components/.archived/<id>/ 下改代码。如果那些改动
	// 已经 git add 过，restore 一 rename 目录，index 里那些路径就变成"删除"——
	// 提交出去等于删文件。
	if compRel, ok := repo.Rel(layout.ComponentsDir()); ok && repo.StagedUnder(compRel) {
		return clierr.New(clierr.CodeConfigConflict,
			i18n.T(msgid.CliRestoreErrorThereAreStagedChanges, compRel)).
			WithHint(
				i18n.T(msgid.CliRestoreRestoreMovesTheSourceDirectories),
				i18n.T(msgid.CliRestoreFirstDealWithTheStaging, compRel),
			)
	}

	// ② 某个组件两处都有源码
	//
	// planSync 会判它"已经在该在的位置"、什么都不做。不报出来就会与提交前的闸门
	// 形成死循环：闸门拦下提交、restore 说没事可做，人却没有任何出路。
	var both []string
	for _, id := range ids {
		if workspace.InBothPlaces(layout, id) {
			both = append(both, id)
		}
	}
	if len(both) > 0 {
		e := clierr.New(clierr.CodeConfigConflict, i18n.T(msgid.CliRestoreErrorTheSourceOfSome))
		for _, id := range both {
			e = e.WithDetail(id,
				i18n.T(msgid.CliRestoreAnd, workspace.DisplayDir(id), workspace.DisplayArchivedDir(id)))
		}
		return e.WithHint(
			i18n.T(msgid.CliRestoreAComponentIdCanHave),
			i18n.T(msgid.CliRestoreFirstCheckWhatEachOf),
			i18n.T(msgid.CliRestoreWhenBothPlacesHaveSource),
		)
	}
	return nil
}

// writeMode 把还原结果落盘。走节点级编辑器：注释与排版原样。
func writeMode(layout project.Layout, changes []modeChange) error {
	if len(changes) == 0 {
		return nil
	}
	edit, err := yamlfile.OpenEdit(layout.DeployPath())
	if err != nil {
		return err
	}
	for _, ch := range changes {
		if ch.to == "" {
			edit.DeleteField(keyComponents, ch.entry, keyMode)
			continue
		}
		edit.SetField(keyComponents, ch.entry, keyMode, ch.to)
	}
	return edit.Save()
}

// 部署文件里组件列表与 mode 字段的键名。
const (
	keyComponents = "components"
	keyMode       = "mode"
)

// printModeChanges 在动手之前如实汇报要改什么、哪些没动。
func printModeChanges(opts *Options, changes []modeChange, untouched []string) {
	if len(changes) == 0 && len(untouched) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliRestoreMatchesTheLastCommit, project.FileDeploy))
		return
	}
	opts.Printf("%s\n", i18n.T(msgid.CliRestoreEnabledRestoredFromTheLast, project.FileDeploy))
	for _, ch := range changes {
		opts.Printf("   %-26s mode: %s → %s\n", ch.entry, showMode(ch.from), toMode(ch.to))
	}
	for _, ref := range untouched {
		opts.Printf("%s\n", i18n.T(msgid.CliRestoreSLeftAsIsThis, ref))
	}
}

func showMode(v string) string {
	if v == "" {
		return i18n.T(msgid.CliRestoreNotSet)
	}
	return v
}

func toMode(v string) string {
	if v == "" {
		return i18n.T(msgid.CliRestoreRemoveTheFieldTheCommit)
	}
	return v
}

// restoreErr 是 restore 前置检查的统一错误壳子。
func restoreErr(message string, cause error) *clierr.Error {
	// clierr:nohint 这是 restore 前置检查的公共壳子；三个调用方都按自己的情形接着补建议
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliRestoreError, message)).
		WithDetail(i18n.T(msgid.LabelReason), cause.Error()).
		WithCause(cause)
}
