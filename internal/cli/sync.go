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
		Short:   "整理组件源码工作区（按级联计算结果归档/激活）",
		GroupID: groupComponent,
		Long: `根据级联计算结果双向整理 components/ 目录（004 §3.9）。

  会启动的组件            → 保留在 components/（在归档目录中则移回来）
  显式关闭（enabled: false）→ 移到 components/.archived/
  被级联跳过的组件         → 移到 components/.archived/

特点：
  - 与 brickkit up 复用同一套级联计算逻辑，但不影响运行中的容器
  - 只操作已有源码的组件，没有 clone 过源码的组件不受影响
  - local: true 组件一视同仁
  - 不提供 --dry-run：搞错了再执行一次就回来了`,
		Example: "  brickkit sync",
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
	reasonCascade  = "级联跳过（强依赖未启动）"
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

	states, err := syncStates(ctx, opts, layout, cfg)
	if err != nil {
		return err
	}

	actions := planSync(layout, cfg, states)
	if len(actions) == 0 {
		opts.Printf("📂 工作区无需整理\n")
		opts.Printf("   %s 下没有需要归档或激活的组件源码\n", config.DirComponents)
		return nil
	}

	return applySync(opts, layout, actions)
}

// syncStates 算出"哪些组件会启动"。
//
// 与 up 完全同一套逻辑（003 §4.3）：两处各判一次，迟早会出现
// "up 会启动它、sync 却把它源码归档了"这种自相矛盾的局面。
func syncStates(ctx context.Context, opts *Options, layout config.Layout, cfg *config.Config) (*cascade.Result, error) {
	if len(cfg.Components) == 0 {
		return &cascade.Result{}, nil
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
	return cascade.Compute(cfg, graph)
}

// planSync 决定每个**有源码**的组件该去哪。
//
// 只看 brickkit.yaml 里声明过的组件：`components/` 下还可能有使用者正在开发、
// 尚未 add 进来的组件源码——级联计算里没有它，不代表"它该被归档"，
// 只代表"这不归我们管"。
func planSync(layout config.Layout, cfg *config.Config, states *cascade.Result) []syncAction {
	running := map[string]bool{}
	for _, ref := range states.Running() {
		running[ref.ID] = true
	}

	// 一个组件 ID 只有一份源码目录（与版本无关），因此按 ID 归集
	reasons := map[string]string{}
	var ids []string
	for _, c := range cfg.Components {
		if _, seen := reasons[c.ID]; !seen {
			ids = append(ids, c.ID)
		}
		if running[c.ID] {
			reasons[c.ID] = ""
			continue
		}
		// 多个版本都不启动时，取第一个能解释清楚的原因
		if reasons[c.ID] == "" {
			reasons[c.ID] = skipReason(c, states)
		}
	}
	sort.Strings(ids)

	var actions []syncAction
	for _, id := range ids {
		active := workspace.Exists(layout, id)
		archived := workspace.IsArchived(layout, id)

		switch {
		case running[id] && archived:
			actions = append(actions, syncAction{id, actionActivate, reasonRestored})
		case running[id] && active:
			actions = append(actions, syncAction{id, actionActive, ""})
		case !running[id] && active:
			actions = append(actions, syncAction{id, actionArchive, reasons[id]})
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
	// 级联结果里带着更具体的原因（"强依赖 xxx 不启动"），优先用它
	for _, state := range states.Components {
		if state.Ref.ID == c.ID && state.Reason != "" {
			return state.Reason
		}
	}
	return reasonCascade
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
