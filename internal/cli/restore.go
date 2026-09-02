package cli

// 本文件实现 brickkit restore（004 §3.14）：把 brickkit.yaml 的 enabled 与组件
// 源码结构还原到最后一次提交，以及供 pre-commit hook 调用的 --check。

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/workspace"
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

// enabledChange 是一处 enabled 还原。
type enabledChange struct {
	id, version string
	// from 是工作区当前的值（nil = 没写），只用于如实汇报被覆盖的旧值。
	from *bool
	// to 是要设成的值；nil 表示**删掉这个字段**（最后一次提交里没写）。
	to *bool
}

// ref 返回 id@version，用于输出。
func (c enabledChange) ref() string { return c.id + "@" + c.version }

// restorePlan 算出要改哪些 enabled。纯函数。
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
func restorePlan(work, head *config.Config) ([]enabledChange, []string) {
	headEnabled := make(map[string]*bool, len(head.Components))
	for _, c := range head.Components {
		headEnabled[c.Ref()] = c.Enabled
	}

	var changes []enabledChange
	var untouched []string
	for _, c := range work.Components {
		want, ok := headEnabled[c.Ref()]
		if !ok {
			untouched = append(untouched, c.Ref())
			continue
		}
		if sameEnabled(c.Enabled, want) {
			continue
		}
		changes = append(changes,
			enabledChange{id: c.ID, version: c.Version, from: c.Enabled, to: want})
	}
	return changes, untouched
}

// sameEnabled 比较两个 enabled 值。nil（没写）与 false 不是一回事。
func sameEnabled(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// applyEnabled 把还原结果写进**内存里**的配置。
func applyEnabled(cfg *config.Config, changes []enabledChange) {
	for _, ch := range changes {
		for i := range cfg.Components {
			if cfg.Components[i].ID == ch.id && cfg.Components[i].Version == ch.version {
				cfg.Components[i].Enabled = ch.to
			}
		}
	}
}

// runRestore 执行 brickkit restore。
//
// # 顺序是硬约束
//
//	解析工作区 yaml → 内存里还原 enabled → 算判定 → 落盘 yaml → 移动目录
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
		return restoreErr("读不到最后一次提交里的 "+layout.ConfigName(), err).
			WithHint("确认它在最后一次提交里存在：git show HEAD:" + cfgRel)
	}
	head, err := config.ParseConfig(headData, "HEAD:"+cfgRel)
	if err != nil {
		return restoreErr("最后一次提交里的 "+layout.ConfigName()+" 不是合法配置——基准坏了", err).
			WithHint(
				"还原的基准就是它，它坏了就没有可还原的目标",
				"先修一个能解析的版本提交上去，再跑 brickkit restore",
			)
	}

	changes, untouched := restorePlan(work, head)

	// 先算判定，算成功了才落盘（见上面那段"顺序是硬约束"）
	applyEnabled(work, changes)
	f, err := syncFocus(ctx, opts, layout, work)
	if err != nil {
		return err
	}
	if err := writeEnabled(layout, changes); err != nil {
		return err
	}

	printEnabledChanges(opts, layout, changes, untouched)
	return applyWorkspacePlan(opts, layout, planSync(layout, work, f))
}

// restoreBaseline 找出"最后一次提交"这个基准，没有基准就说清楚。
func restoreBaseline(layout config.Layout) (*gitrepo.Repo, string, error) {
	repo, err := gitrepo.Open(layout.Root)
	if err != nil {
		return nil, "", restoreErr("这里不是一个 git 仓库", err).
			WithHint(
				"brickkit restore 把配置还原到**最后一次提交**，没有 git 就没有这个基准",
				"想收窄范围又不想提交，就手工改回 enabled 再跑 brickkit sync",
			)
	}
	if repo.Unmerged() {
		return nil, "", clierr.New(clierr.CodeConfigConflict, "错误：正在解决冲突，先把冲突处理完").
			WithHint("brickkit restore 要读最后一次提交，冲突中的 index 读不了")
	}
	if !repo.HasHEAD() {
		return nil, "", clierr.New(clierr.CodeConfigInvalid, "错误：这个仓库还没有任何提交").
			WithHint("还原的基准是最后一次提交，一次都没有就没有可还原的目标")
	}
	cfgRel, ok := repo.Rel(layout.ConfigPath())
	if !ok {
		return nil, "", clierr.New(clierr.CodeConfigInvalid,
			"错误："+layout.ConfigName()+" 不在这个 git 仓库里").
			WithDetail("配置", layout.ConfigPath()).
			WithDetail("仓库", repo.Root())
	}
	if !repo.Tracked(cfgRel) {
		return nil, "", clierr.New(clierr.CodeConfigInvalid,
			"错误："+layout.ConfigName()+" 没有被 git 跟踪").
			WithHint("先 git add " + cfgRel + " 并提交一次，它才有可还原的基准")
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
			"错误："+compRel+"/ 下有已暂存的改动，restore 会把它们悬空").
			WithHint(
				"restore 要移动源码目录，而已暂存的路径会跟着变成「删除」",
				"先把暂存区处理掉：git commit，或者 git reset "+compRel+"/",
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
		e := clierr.New(clierr.CodeConfigConflict, "错误：有组件的源码在两处都存在")
		for _, id := range both {
			e = e.WithDetail(id,
				workspace.DisplayDir(id)+"  与  "+workspace.DisplayArchivedDir(id))
		}
		return e.WithHint(
			"一个组件 ID 只能有一个源码目录（004 §8.1），restore 不知道该保留哪一份",
			"先检查两个目录里各是什么，确认无用后删除或重命名其中一份",
			"两处都有源码时，平台不替你决定保留哪一份",
		)
	}
	return nil
}

// writeEnabled 把还原结果落盘。走节点级编辑器：注释与排版原样。
func writeEnabled(layout config.Layout, changes []enabledChange) error {
	if len(changes) == 0 {
		return nil
	}
	edit, err := config.OpenEdit(layout.ConfigPath())
	if err != nil {
		return err
	}
	for _, ch := range changes {
		if ch.to == nil {
			edit.ClearComponentEnabled(ch.id, ch.version)
			continue
		}
		edit.SetComponentEnabled(ch.id, ch.version, *ch.to)
	}
	return edit.Save()
}

// printEnabledChanges 汇报 yaml 那一半改了什么。
//
// **被覆盖的旧值必须印出来。** restore 不可逆：被覆盖的 enabled 没有第二份副本，
// sync 那句"搞错了再执行一次就回来了"在这里不成立。处理办法不是加 --yes 确认
// （那两三行本来就是 004 §3.9.2 教人 git checkout 掉的东西），而是如实汇报——
// 使用者从终端 scrollback 里就能读回来。
func printEnabledChanges(
	opts *Options, layout config.Layout, changes []enabledChange, untouched []string,
) {
	if len(changes) == 0 && len(untouched) == 0 {
		opts.Printf("📄 %s 与最后一次提交一致\n", layout.ConfigName())
		return
	}
	opts.Printf("📄 %s：按最后一次提交还原 enabled（其余改动未动）\n", layout.ConfigName())
	for _, ch := range changes {
		opts.Printf("   %-26s enabled: %s → %s\n", ch.ref(), showEnabled(ch.from), toEnabled(ch.to))
	}
	for _, ref := range untouched {
		opts.Printf("   %-26s 未动（这个条目在最后一次提交里不存在）\n", ref)
	}
}

func showEnabled(v *bool) string {
	if v == nil {
		return "（没写）"
	}
	if *v {
		return "true"
	}
	return "false"
}

func toEnabled(v *bool) string {
	if v == nil {
		return "删除该字段（提交里没写）"
	}
	return showEnabled(v)
}

// restoreErr 是 restore 前置检查的统一错误壳子。
func restoreErr(message string, cause error) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, "错误："+message).
		WithDetail("原因", cause.Error()).
		WithCause(cause)
}
