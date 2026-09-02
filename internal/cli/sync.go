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
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// newSyncCommand 实现 brickkit sync（004 §3.9）。
func newSyncCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sync",
		Short:   "整理组件源码工作区：把这次用不上的组件源码收进 .archived/",
		GroupID: groupComponent,
		Long: `把当前用不上的组件源码从 components/ 收进 components/.archived/（004 §3.9）。

项目里组件一多，那些当下根本不碰的源码仍然堆在 components/ 下：
IDE 索引、全局搜索、grep、以及替你读代码的 AI 都得连它们一起扫。
sync 把它们挪进一个固定的目录——不打开就不用关心，要找时又一眼知道在哪。

**判据与 brickkit up 完全一致**：这次会启动的留在活跃目录，不启动的归档。
想收窄范围就改 brickkit.yaml 的 enabled（顶层关掉，下面一串跟着走，003 §4.3），
sync 跟着走就行。

规则：

  - 双向：该归档的归档，该激活的移回来
  - 不影响运行中的容器，也不改变 up 会启动谁——它只动目录
  - 只操作 brickkit.yaml 里声明过、且已有源码的组件
  - 整个目录连 .git 一起搬，归档后 git 命令照常
  - 不提供 --dry-run：搞错了再执行一次就回来了`,
		Example: `  brickkit sync   把这次不启动的组件源码收进 .archived/，该回来的移回来`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), opts)
		},
	}
	return cmd
}

// 归档 / 激活的原因（17.12）。
const (
	reasonDisabled = "显式禁用（enabled: false）"
	reasonStopped  = "本次不启动"
	reasonRestored = "恢复启用"
)

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

	keep, err := syncFocus(ctx, opts, layout, cfg)
	if err != nil {
		return err
	}

	actions := planSync(layout, cfg, keep)
	if len(actions) == 0 {
		opts.Printf("📂 工作区无需整理\n")
		opts.Printf("   %s 下没有需要归档或激活的组件源码\n", config.DirComponents)
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
		return newFocus(reasonRestored), nil
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	graph, err := resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	states, err := cascade.Compute(cfg, graph)
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
	f := newFocus(reasonRestored)
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
		return reasonDisabled
	}
	// 判定结果里带着更具体的原因（"上层都不启动"等），优先用它
	for _, state := range states.Components {
		if state.Ref.ID == c.ID && state.Reason != "" {
			return state.Reason
		}
	}
	return reasonStopped
}

// applySync 真的去移动目录，并如实汇报（17.11 / 17.12）。
func applySync(opts *Options, layout config.Layout, actions []syncAction) error {
	opts.Printf("📂 工作区整理：\n")

	var active, archived, activated int
	for _, a := range actions {
		switch a.kind {
		case actionActive:
			active++
			opts.Printf("   ✅ %-36s 活跃\n", workspace.DisplayDir(a.componentID))

		case actionArchive:
			if err := workspace.Archive(layout, a.componentID); err != nil {
				return err
			}
			archived++
			opts.Printf("   📦 %-36s → %s\n",
				workspace.DisplayDir(a.componentID), workspace.DisplayArchivedDir(a.componentID))
			opts.Printf("      原因：%s\n", a.reason)

		case actionActivate:
			if err := workspace.Activate(layout, a.componentID); err != nil {
				return err
			}
			activated++
			opts.Printf("   📂 %-36s → %s\n",
				workspace.DisplayArchivedDir(a.componentID), workspace.DisplayDir(a.componentID))
			opts.Printf("      原因：%s\n", a.reason)
		}
	}

	opts.Printf("✅ 工作区整理完成（%d 个活跃，%d 个归档，%d 个激活）\n", active, archived, activated)
	logging.Info("工作区已整理",
		"active", active, "archived", archived, "activated", activated)
	return nil
}
