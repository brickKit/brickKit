package cli

// 本文件实现 brickkit sync（004 §3.9）：按级联计算结果整理组件源码工作区。
//
// 它与 up 共用同一套级联计算，但**只动目录，不碰引擎**：
// 运行中的容器一个都不受影响（004 §3.9 的职责对照表）。

import (
	"context"
	"sort"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// newSyncCommand 实现 brickkit sync（004 §3.9）。
func newSyncCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sync",
		Short:   i18n.T(msgid.CliSyncShort),
		GroupID: groupComponent,
		Long:    i18n.T(msgid.CliSyncLong),
		Example: i18n.T(msgid.CliSyncExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), opts)
		},
	}
	return cmd
}

// 归档 / 激活的原因（17.12）。
//
// 是函数而不是常量：文案要跟着语言变，包初始化时语言还没确定。
func reasonDisabled() string { return i18n.T(msgid.CascadeReasonDisabled) }
func reasonStopped() string  { return i18n.T(msgid.CliSyncReasonStopped) }
func reasonRestored() string { return i18n.T(msgid.CliSyncReasonRestored) }

// syncAction 是对一个组件源码目录要做的事。
type syncAction struct {
	componentID string
	// kind 是 active / archive / activate。
	kind   string
	reason string
}

const (
	actionActive   = "active"
	actionArchive  = "archive"
	actionActivate = "activate"
)

// runSync 执行 brickkit sync。
func runSync(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	ov, err := loadOverride(opts, layout, cfg)
	if err != nil {
		return err
	}
	if err := applyOverride(cfg, ov); err != nil {
		return err
	}

	keep, err := syncFocus(ctx, opts, layout, cfg)
	if err != nil {
		return err
	}

	return applyWorkspacePlan(opts, layout, planSync(layout, cfg, keep))
}

// applyWorkspacePlan 执行工作区整理计划，并如实汇报。
//
// sync 与 restore 共用它：同一件事只有一处渲染代码，两个命令的输出也就不可能
// 各说一套。
func applyWorkspacePlan(opts *Options, layout config.Layout, actions []syncAction) error {
	if len(actions) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliSyncTheWorkspaceNeedsNoTidying))
		opts.Printf("%s\n", i18n.T(msgid.CliSyncThereIsNoComponentSource, config.DirComponents))
		return nil
	}
	return applySync(opts, layout, actions)
}

// focus 是"这一次哪些组件留在活跃目录"的判定结果，按**组件 ID** 归集。
//
// 按 ID 而不是按版本：一个组件 ID 只有一份源码目录（004 §8.1），
// 同 ID 的多个版本共用它。
//
// keep 就是 brickkit up 这次会启动的那些（003 §4.3）。
type focus struct {
	keep map[string]bool
	// reason 是**没留下**的组件各自的理由，直接出现在输出里。
	reason map[string]string
	// restored 是把组件从归档目录移回来时给出的理由。
	restored string
}

func newFocus(restored string) *focus {
	return &focus{keep: map[string]bool{}, reason: map[string]string{}, restored: restored}
}

// syncFocus 算出这次要留下哪些组件：与 brickkit up 同一套启停判定。
func syncFocus(
	ctx context.Context, opts *Options, layout config.Layout, cfg *config.Config,
) (*focus, error) {
	if len(cfg.Components) == 0 {
		return newFocus(reasonRestored()), nil
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	_, states, err := resolveTopology(ctx, client, cfg)
	if err != nil {
		return nil, err
	}
	return focusFrom(cfg, states), nil
}

// focusFrom 把启停判定结果折成"哪些源码留在活跃目录"。
//
// 与 up 完全同一套判定（003 §4.3）：两处各判一次，迟早会出现
// "up 会启动它、sync 却把它源码归档了"这种自相矛盾的局面。
func focusFrom(cfg *config.Config, states *cascade.Result) *focus {
	f := newFocus(reasonRestored())
	for _, ref := range states.Running() {
		f.keep[ref.ID] = true
	}
	for _, c := range cfg.Components {
		if f.keep[c.ID] {
			// 同 ID 的另一个版本会启动 → 这份源码要留着
			delete(f.reason, c.ID)
			continue
		}
		if _, done := f.reason[c.ID]; !done {
			f.reason[c.ID] = skipReason(c, states)
		}
	}
	return f
}

// declaredIDs 返回配置里声明过的组件 ID，排序去重。
//
// 一个组件 ID 只有一份源码目录（004 §8.1），与版本无关，所以按 ID 去重。
// 排序是为了输出与判据结果稳定——错误信息每次顺序不同会让人以为在变。
func declaredIDs(cfg *config.Config) []string {
	var ids []string
	seen := map[string]bool{}
	for _, c := range cfg.Components {
		if !seen[c.ID] {
			seen[c.ID] = true
			ids = append(ids, c.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// planSync 决定每个**有源码**的组件该去哪。
//
// 只看 brickkit.yaml 里声明过的组件：`components/` 下还可能有使用者正在开发、
// 尚未 add 进来的组件源码——判定结果里没有它，不代表"它该被归档"，
// 只代表"这不归我们管"。
func planSync(layout config.Layout, cfg *config.Config, f *focus) []syncAction {
	ids := declaredIDs(cfg)

	var actions []syncAction
	for _, id := range ids {
		active := workspace.Exists(layout, id)
		archived := workspace.IsArchived(layout, id)

		switch {
		case f.keep[id] && archived:
			actions = append(actions, syncAction{id, actionActivate, f.restored})
		case f.keep[id] && active:
			actions = append(actions, syncAction{id, actionActive, ""})
		case !f.keep[id] && active:
			actions = append(actions, syncAction{id, actionArchive, f.reason[id]})
		}
		// 其余情况（没有源码、已经在该在的位置）什么都不做
	}
	return actions
}

// skipReason 说明这个组件为什么不启动（17.12）。
func skipReason(c config.Component, states *cascade.Result) string {
	if c.IsDisabled() {
		return reasonDisabled()
	}
	// 判定结果里带着更具体的原因（"上层都不启动"等），优先用它
	for _, state := range states.Components {
		if state.Ref.ID == c.ID && state.Reason != "" {
			return state.Reason
		}
	}
	return reasonStopped()
}

// applySync 真的去移动目录，并如实汇报（17.11 / 17.12）。
func applySync(opts *Options, layout config.Layout, actions []syncAction) error {
	opts.Printf("%s\n", i18n.T(msgid.CliSyncWorkspaceTidying))

	// 不在 git 仓库里时 repo 为 nil：Archive/Activate 自己会跳过 submodule 阻断，
	// 现有行为完全不变。只查一次：这一轮里每个组件共用同一份 .gitmodules 判断。
	repo, _ := gitrepo.Open(layout.Root)

	var active, archived, activated int
	for _, a := range actions {
		switch a.kind {
		case actionActive:
			active++
			opts.Printf("%s\n", i18n.T(msgid.CliSyncSActive, workspace.DisplayDir(a.componentID)))

		case actionArchive:
			if err := workspace.Archive(layout, a.componentID, repo); err != nil {
				return err
			}
			archived++
			opts.Printf("   📦 %-36s → %s\n",
				workspace.DisplayDir(a.componentID), workspace.DisplayArchivedDir(a.componentID))
			opts.Printf("%s\n", i18n.T(msgid.CliSyncReason, a.reason))

		case actionActivate:
			if err := workspace.Activate(layout, a.componentID, repo); err != nil {
				return err
			}
			activated++
			opts.Printf("   📂 %-36s → %s\n",
				workspace.DisplayArchivedDir(a.componentID), workspace.DisplayDir(a.componentID))
			opts.Printf("%s\n", i18n.T(msgid.CliSyncReason, a.reason))
		}
	}

	opts.Printf("%s\n", i18n.T(msgid.CliSyncWorkspaceTidiedActiveArchivedActivated, active, archived, activated))
	logging.Info(i18n.T(msgid.LogWorkspaceTidied),
		"active", active, "archived", archived, "activated", activated)
	return nil
}
