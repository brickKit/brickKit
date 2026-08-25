package cli

// 本文件实现 brickkit sync（004 §3.9）：按级联计算结果整理组件源码工作区。
//
// 它与 up 共用同一套级联计算，但**只动目录，不碰引擎**：
// 运行中的容器一个都不受影响（004 §3.9 的职责对照表）。

import (
	"context"
	"sort"
	"strings"

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
	var only []string

	cmd := &cobra.Command{
		Use:     "sync",
		Short:   "整理组件源码工作区：把这次用不上的组件源码收进 .archived/",
		GroupID: groupComponent,
		Long: `把当前用不上的组件源码从 components/ 收进 components/.archived/（004 §3.9）。

项目里组件一多，那些当下根本不碰的源码仍然堆在 components/ 下：
IDE 索引、全局搜索、git status 全被它们拖着，排查问题时也得一路略过。
sync 把它们挪进一个固定的目录——不打开就不用关心，要找时又一眼知道在哪。

两种用法：

  brickkit sync --only <组件>    这几天只搞这几个：只留它们与它们的强依赖，
                                其余全部归档。**brickkit.yaml 一个字节不动**
  brickkit sync                 回到与 brickkit up 一致：会启动的留下，
                                不启动的归档。也是 --only 之后的"恢复"

共同规则：

  - 双向：该归档的归档，该激活的移回来
  - 不影响运行中的容器，也不改变 up 会启动谁——它只动目录
  - 只操作 brickkit.yaml 里声明过、且已有源码的组件
  - 整个目录连 .git 一起搬，归档后 git 命令照常
  - 不提供 --dry-run：搞错了再执行一次就回来了`,
		Example: `  brickkit sync --only people/basic              只留人员组件与它的强依赖
  brickkit sync --only people/basic,demo/hello   同时搞两个
  brickkit sync                                  恢复成与 brickkit up 一致`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), opts, only)
		},
	}

	cmd.Flags().StringSliceVar(&only, "only", nil,
		"只保留指定组件及其强依赖，其余全部归档，逗号分隔，支持 @版本（不修改 brickkit.yaml）")
	return cmd
}

// 归档 / 激活的原因（17.12）。
const (
	reasonDisabled = "显式禁用（enabled: false）"
	reasonCascade  = "级联跳过（强依赖未启动）"
	reasonRestored = "恢复启用"
	// --only 的两个原因。与 up --only 用同一句话，两个命令说的是同一件事。
	reasonNotSelected = "未被 --only 选中"
	reasonSelected    = "被 --only 选中"
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
func runSync(ctx context.Context, opts *Options, only []string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	keep, err := syncFocus(ctx, opts, layout, cfg, only)
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
// 两条来路各自填这同一个结构，planSync 因此不需要知道这次是哪一种：
//
//	不带 --only   keep = 级联算出来会启动的那些（与 brickkit up 一致）
//	带 --only     keep = 被点名的那些 + 它们的强依赖
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

// syncFocus 算出这次要留下哪些组件。
//
// 两种来路都要先解析依赖图：级联需要它，--only 的强依赖闭包也需要它。
func syncFocus(
	ctx context.Context, opts *Options, layout config.Layout, cfg *config.Config, only []string,
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

	if len(only) > 0 {
		return focusOnly(opts, cfg, graph, only)
	}
	states, err := cascade.Compute(cfg, graph)
	if err != nil {
		return nil, err
	}
	return focusCascade(cfg, states), nil
}

// focusCascade 走级联计算：留下的正是 brickkit up 这次会启动的那些。
//
// 与 up 完全同一套逻辑（003 §4.3）：两处各判一次，迟早会出现
// "up 会启动它、sync 却把它源码归档了"这种自相矛盾的局面。
func focusCascade(cfg *config.Config, states *cascade.Result) *focus {
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

// focusOnly 走 `--only`：只留被点名的组件与它们的**强依赖**。
//
// # 为什么值得有这个参数
//
// 不带 --only 时，"留哪些"完全由 brickkit.yaml 里的 enabled 决定。于是
// "这两天只搞人员组件"要先把不想要的根组件逐个写上 enabled: false、
// 再把想要的写上 enabled: true（不写会被级联一起跳过）——试用指南 02
// 演示这件事时，十个组件动了八行。
//
// 而 brickkit.yaml 是**提交进 Git、团队共享、带部署语义**的文件（003 §10.2），
// "我今天看哪几个组件"却是个人的、临时的、只关工作区的偏好。绑在一起只剩两个坏选项：
// 把个人偏好提交上去（别人拉下来发现半个系统被关了），或者背一个永远脏着的 diff。
//
// --only 把这件事从配置里摘出来：**brickkit.yaml 一个字节不动**，
// 干完活一条 `brickkit sync` 就回到与 up 一致的状态。
//
// # 与 up --only 的一处刻意不同
//
// `up --only` 点到 enabled: false 的组件会报错（两个意图直接冲突，004 §3.5）。
// 这里**不报错**：up 决定"跑什么"，sync 决定"看什么"，而"我要看一个已经关掉的
// 组件的源码"完全说得通——多数时候正是因为要重写它才把它关掉的。
func focusOnly(opts *Options, cfg *config.Config, graph *resolver.Graph, only []string) (*focus, error) {
	selected, err := selectRefsIn(cfg, only)
	if err != nil {
		return nil, err
	}

	keepRefs := map[resolver.Ref]bool{}
	for _, ref := range selected {
		addWithRequires(graph, ref, keepRefs)
	}

	f := newFocus(reasonSelected)
	for ref := range keepRefs {
		f.keep[ref.ID] = true
	}
	for _, c := range cfg.Components {
		if !f.keep[c.ID] {
			f.reason[c.ID] = reasonNotSelected
		}
	}

	opts.Printf("🎯 --only：只保留 %s 及其强依赖（brickkit.yaml 未修改）\n",
		strings.Join(only, "、"))
	return f, nil
}

// planSync 决定每个**有源码**的组件该去哪。
//
// 只看 brickkit.yaml 里声明过的组件：`components/` 下还可能有使用者正在开发、
// 尚未 add 进来的组件源码——判定结果里没有它，不代表"它该被归档"，
// 只代表"这不归我们管"。
func planSync(layout config.Layout, cfg *config.Config, f *focus) []syncAction {
	// 一个组件 ID 只有一份源码目录（与版本无关），因此按 ID 去重
	var ids []string
	seen := map[string]bool{}
	for _, c := range cfg.Components {
		if !seen[c.ID] {
			seen[c.ID] = true
			ids = append(ids, c.ID)
		}
	}
	sort.Strings(ids)

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
