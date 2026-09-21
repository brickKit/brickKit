package cli

// 本文件实现 brickkit restore（004 §3.14）：把 brickkit.yaml 的 mode 与组件
// 源码结构还原到最后一次提交，以及供 pre-commit hook 调用的 --check。

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/workspace"
)

// newRestoreCommand 实现 brickkit restore（004 §3.14）。
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
	id, version string
	// from 是工作区当前的值（"" = 没写），只用于如实汇报被覆盖的旧值。
	from string
	// to 是要设成的值；"" 表示**删掉这个字段**（最后一次提交里没写）——
	// mode 本身的零值就是"没写"，不需要再用指针区分"没写"与"写了空值"
	// （这跟 Component.Mode 自己的语义完全一致，不是又发明了一套表达方式）。
	to string
}

// ref 返回 id@version，用于输出。
func (c modeChange) ref() string { return c.id + "@" + c.version }

// restorePlan 算出要改哪些 mode。纯函数。
//
// 只动"工作区与 HEAD 都有的同一个 (id, version) 条目"，另外两种刻意不动：
//
//	工作区新增的条目     本地刚 add 的、或本地改了版本号。一个字不动——
//	                    这是"不吃掉未提交的 add"的解药。004 §3.10 批评
//	                    brickkit reset 的正是"救配置的命令自己救不回来"
//	HEAD 有而工作区没有   本地 remove 掉的。绝不加回来——restore 不是 revert
//
// 返回的 untouched 是那些"工作区有、提交里没有"的条目引用，要在输出里点名说
// "未动"：使用者得知道为什么它没变，否则会以为命令漏了它。
func restorePlan(work, head *config.Config) ([]modeChange, []string) {
	headMode := make(map[string]string, len(head.Components))
	for _, c := range head.Components {
		headMode[c.Ref()] = c.Mode
	}

	var changes []modeChange
	var untouched []string
	for _, c := range work.Components {
		want, ok := headMode[c.Ref()]
		if !ok {
			untouched = append(untouched, c.Ref())
			continue
		}
		if c.Mode == want {
			continue
		}
		changes = append(changes,
			modeChange{id: c.ID, version: c.Version, from: c.Mode, to: want})
	}
	return changes, untouched
}

// applyMode 把还原结果写进**内存里**的配置。
func applyMode(cfg *config.Config, changes []modeChange) {
	for _, ch := range changes {
		for i := range cfg.Components {
			if cfg.Components[i].ID == ch.id && cfg.Components[i].Version == ch.version {
				cfg.Components[i].Mode = ch.to
			}
		}
	}
}

// runRestore 执行 brickkit restore。
//
// # 顺序是硬约束
//
//	解析工作区 yaml → 内存里还原 mode → 算判定 → 落盘 yaml → 移动目录
//
// 反过来（先落盘再算判定）就会在 Manifest 缺失或需要联网时留下一个
// "yaml 改了、结构没动"的半成品——而那正是使用者最不希望在提交前撞上的状态。
// 按这个顺序，判定失败时 yaml 一个字没改，重跑即可。
func runRestore(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	repo, cfgRel, err := restoreBaseline(layout)
	if err != nil {
		return err
	}

	work, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	if err := restorePreflight(repo, layout, work); err != nil {
		return err
	}

	headData, err := repo.HeadBlob(cfgRel)
	if err != nil {
		return restoreErr(i18n.T(msgid.CliRestoreCannotReadFromTheLast, layout.ConfigName()), err).
			WithHint(i18n.T(msgid.CliRestoreMakeSureItExistsIn, cfgRel))
	}
	head, err := config.ParseConfig(headData, "HEAD:"+cfgRel)
	if err != nil {
		return restoreErr(i18n.T(msgid.CliRestoreInTheLastCommitIs, layout.ConfigName()), err).
			WithHint(
				i18n.T(msgid.CliRestoreTheBaselineForRestoringIs),
				i18n.T(msgid.CliRestoreFirstCommitAVersionThat),
			)
	}

	changes, untouched := restorePlan(work, head)

	// 先算判定，算成功了才落盘（见上面那段"顺序是硬约束"）
	applyMode(work, changes)
	f, err := syncFocus(ctx, opts, layout, work)
	if err != nil {
		return err
	}

	// 被覆盖的旧值必须在落盘之前印出来：restore 不可逆，旧的 mode 没有
	// 第二份副本，如实汇报是唯一的缓解措施。这两句之间如果被杀掉进程
	// （OOM、SIGKILL、断电），旧值不能既从磁盘上没了、又从未被报告过。
	printModeChanges(opts, layout, changes, untouched)
	if err := writeMode(layout, changes); err != nil {
		return err
	}

	return applyWorkspacePlan(opts, layout, planSync(layout, work, f))
}

// restoreBaseline 找出"最后一次提交"这个基准，没有基准就说清楚。
func restoreBaseline(layout config.Layout) (*gitrepo.Repo, string, error) {
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
	cfgRel, ok := repo.Rel(layout.ConfigPath())
	if !ok {
		return nil, "", clierr.New(clierr.CodeConfigInvalid,
			i18n.T(msgid.CliRestoreErrorIsNotInsideThis, layout.ConfigName())).
			WithDetail(i18n.T(msgid.CliRestoreConfig), layout.ConfigPath()).
			WithDetail(i18n.T(msgid.LabelRepo), repo.Root())
	}
	if !repo.Tracked(cfgRel) {
		return nil, "", clierr.New(clierr.CodeConfigInvalid,
			i18n.T(msgid.CliRestoreErrorIsNotTrackedBy, layout.ConfigName())).
			WithHint(i18n.T(msgid.CliRestoreFirstRunGitAddAnd, cfgRel))
	}
	return repo, cfgRel, nil
}

// restorePreflight 拦下两种"动手就会出事"的现场。
func restorePreflight(repo *gitrepo.Repo, layout config.Layout, cfg *config.Config) error {
	// ① components/ 下有已暂存的改动
	//
	// 004 §3.9.3 明说允许直接在 components/.archived/<id>/ 下改代码。如果那些改动
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
	for _, id := range declaredIDs(cfg) {
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
func writeMode(layout config.Layout, changes []modeChange) error {
	if len(changes) == 0 {
		return nil
	}
	edit, err := config.OpenEdit(layout.ConfigPath())
	if err != nil {
		return err
	}
	for _, ch := range changes {
		if ch.to == "" {
			edit.ClearComponentMode(ch.id, ch.version)
			continue
		}
		edit.SetComponentMode(ch.id, ch.version, ch.to)
	}
	return edit.Save()
}

// printModeChanges 汇报 yaml 那一半改了什么。
//
// **被覆盖的旧值必须印出来。** restore 不可逆：被覆盖的 mode 没有第二份副本，
// sync 那句"搞错了再执行一次就回来了"在这里不成立。处理办法不是加 --yes 确认
// （那两三行本来就是 004 §3.9.2 教人 git checkout 掉的东西），而是如实汇报——
// 使用者从终端 scrollback 里就能读回来。
func printModeChanges(
	opts *Options, layout config.Layout, changes []modeChange, untouched []string,
) {
	if len(changes) == 0 && len(untouched) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliRestoreMatchesTheLastCommit, layout.ConfigName()))
		return
	}
	opts.Printf("%s\n", i18n.T(msgid.CliRestoreEnabledRestoredFromTheLast, layout.ConfigName()))
	for _, ch := range changes {
		opts.Printf("   %-26s mode: %s → %s\n", ch.ref(), showMode(ch.from), toMode(ch.to))
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
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliRestoreError, message)).
		WithDetail(i18n.T(msgid.LabelReason), cause.Error()).
		WithCause(cause)
}
