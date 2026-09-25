package cli

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
	"github.com/brickkit/brickkit/internal/procsup"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// composeFileName 是生成的部署文件名（004 §3.5 输出样例）。
//
// 叫 compose.yaml 而不是 docker-compose.yaml：这份文件遵循的是 Compose
// 规范（compose-spec.io），Docker、Podman 都能消费同一份——带上 docker
// 前缀会让人误以为它是 Docker 专属的，而 target: podman（§5.10）读的正是
// 同一份文件。
const composeFileName = "compose.yaml"

// newUpCommand 实现 brickkit up（004 §3.5）。
func newUpCommand(opts *Options) *cobra.Command {
	var (
		dryRun         bool
		kubeContext    string
		ignoreServedBy bool
		crashLines     int
	)

	cmd := &cobra.Command{
		Use:     "up",
		Short:   i18n.T(msgid.CliUpShort),
		GroupID: groupLifecycle,
		Long:    i18n.T(msgid.CliUpLong),
		Example: i18n.T(msgid.CliUpExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUp(cmd.Context(), opts, upOptions{
				dryRun: dryRun, kubeContext: kubeContext, ignoreServedBy: ignoreServedBy,
				crashLines: crashLines, crashLinesSet: cmd.Flags().Changed("crash-lines"),
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T(msgid.CliUpOnlyGenerateTheDeploymentFiles))
	cmd.Flags().StringVar(&kubeContext, "context", "", i18n.T(msgid.CliDownKubeconfigContextOverridesDeployContext))
	cmd.Flags().BoolVar(&ignoreServedBy, "ignore-served-by", false,
		i18n.T(msgid.CliUpClearEveryServedbyDeclarationIn))
	cmd.Flags().IntVar(&crashLines, "crash-lines", procsup.DefaultTailLines,
		i18n.T(msgid.CliUpCrashLinesHowManyLinesOfOutput))
	return cmd
}

// upPlan 是"这次 up 要做什么"的全部结论。
//
// 生成与启动共用同一份计划：--dry-run 与真启动之间唯一的差别应当是
// "有没有真的调引擎"，而不是两条各自算一遍、可能算出不同结果的路径。
type upPlan struct {
	layout    config.Layout
	cfg       *config.Config
	graph     *resolver.Graph
	states    *cascade.Result
	generated *compose.Result
	// k8s 是 deploy.target: k8s 时的生成结果（与 generated 互斥）。
	k8s *k8s.Result
	// kubeContext 是本次钉住的 kubeconfig 上下文（可能来自 --context）。
	kubeContext string
	// services 是本次要交给引擎启动的 service（不含 local 组件与迁移容器）。
	services []string
	// migrations 是本次会执行的迁移，供输出（15.25）。
	migrations []migrationInfo
	// images 是要检查拉取权限的镜像（15.19）。
	images []imageInfo
	// upgrades 是本次检测到的版本变更（004 §3.5.1）。
	upgrades []upgradeInfo
	// localComponents 是本次要真正拉起的 mode: local 组件（按拓扑序）。
	localComponents []localComponentPlan
	// crashLines 是 --crash-lines 的值，从 upOptions 原样抄过来——upPlan 本来
	// 就是"这次 up 要做什么的全部结论"，start 不接收 upOptions，靠这里传下去。
	crashLines int
	// done 为 true 表示"没什么可启动的"，已经把话说清楚了。
	done bool
}

type migrationInfo struct {
	component string
	command   string
}

type imageInfo struct {
	component string
	image     string
}

// upOptions 是 up 的命令行选项。
type upOptions struct {
	dryRun bool
	// kubeContext 是 --context 的值，覆盖 deploy.context。
	kubeContext string
	// ignoreServedBy 是 --ignore-served-by 的值：内存里清空全部 servedBy
	// 声明再跑一次，验证"每个组件必须能独立 brickkit up 起来"这条设计
	// 原则，从不写回 brickkit.yaml（brickKit 反馈：两个降低 servedBy
	// 运维摩擦的架构提案，提案二）。
	ignoreServedBy bool
	// crashLines 是 --crash-lines 的值；crashLinesSet 为 true 才说明用户真的
	// 传了这个旗位（不能靠"值等不等于默认值"判断——用户完全可能手写
	// --crash-lines 20，跟不传时拿到的默认值撞在一起）。
	crashLines    int
	crashLinesSet bool
}

// runUp 执行 brickkit up。
func runUp(ctx context.Context, opts *Options, flags upOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	plan, err := buildUpPlan(ctx, opts, flags)
	if err != nil || plan.done {
		return err
	}
	if plan.k8s != nil {
		return upK8s(ctx, opts, flags, plan)
	}

	path, err := writeGenerated(plan.layout, plan.generated.YAML)
	if err != nil {
		return err
	}
	if err := writeLocalEnvFiles(opts, plan.layout, plan.generated.LocalEnvFiles); err != nil {
		return err
	}
	opts.Printf("%s\n", i18n.T(msgid.CliUpGenerated, displayPath(opts.WorkDir, path)))
	renderResourceRequirements(opts, plan.generated.Resources)
	// 在 --dry-run 的分岔**之前**："这次会动哪些库"正是 dry-run 最该回答的问题
	// 之一，而它与升不升级无关。从前它在分岔之后，于是 dry-run 里一个字都没有，
	// 唯一提到迁移的地方是升级摘要里那一行——还得先检测到升级才会出现
	renderMigrations(opts, plan.migrations)

	if flags.dryRun {
		renderUpgradeSummary(opts, plan)
		renderLocalComponentCommands(opts, plan.localComponents)
		opts.Printf("\n%s\n", i18n.T(msgid.CliUpDryRunOnlyGeneratesThe))
		opts.Printf("%s\n", i18n.T(msgid.CliUpViewItCat, displayPath(opts.WorkDir, path)))
		logging.Info(i18n.T(msgid.LogDeployFilesGenerated), "path", path)
		return nil
	}

	if len(plan.services) == 0 {
		// 这次没有任何组件需要容器（可能全是 mode: local / mode: debug，
		// 或者全被 servedBy 吸收进了外壳）——不该去起一个引擎：`docker compose
		// up` 对着一份 `services: {}` 的空文件会报 "no service selected"，
		// 而且一个纯 mode: local 的项目本不该被要求装 Docker（手动验证 Task 6
		// Step 5 时用真实 docker 跑出来的：demo/hello 单组件、mode: local，
		// 之前这里会直接报 ENGINE_FAILED，明明这个项目一个容器都不需要）。
		return runLocalComponents(ctx, opts, plan.layout, plan.localComponents, plan.crashLines)
	}

	eng, err := resolveEngineFor(opts, plan.cfg)
	if err != nil {
		return err
	}
	if err := checkImages(ctx, opts, eng, plan.images); err != nil {
		return err
	}

	return start(ctx, opts, eng, plan, path, projectSelector(plan.cfg))
}

// buildUpPlan 从配置一路算到"要启动哪些 service"。
func buildUpPlan(ctx context.Context, opts *Options, flags upOptions) (*upPlan, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return nil, err
	}
	ov, err := loadOverride(opts, layout, cfg)
	if err != nil {
		return nil, err
	}
	for _, note := range override.Drift(cfg, ov) {
		opts.Printf("%s\n", i18n.T(msgid.CliOverrideDriftNote, note.Field, note.Message))
	}
	if err := applyOverride(cfg, ov); err != nil {
		return nil, err
	}
	if flags.ignoreServedBy {
		clearServedBy(cfg)
		opts.Printf("%s\n", i18n.T(msgid.CliUpAllServedbyDeclarationsAreIgnored))
	}

	plan := &upPlan{layout: layout, cfg: cfg, kubeContext: contextOf(cfg, flags.kubeContext), crashLines: flags.crashLines}
	if len(cfg.Components) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliStatusTheCurrentProjectHasNo))
		// init 的骨架已经把 ./components 配成了本地安装源，所以 --local 是最短的一条路。
		// 两条都给：有的人手上已经有组件源码，有的人要从市场装。
		opts.Printf("%s\n", i18n.T(msgid.CliStatusAddAllTheComponentsUnder, config.DirComponents))
		opts.Printf("%s\n", i18n.T(msgid.CliStatusOrAddOneFromAn))
		plan.done = true
		return plan, nil
	}

	opts.Printf("%s\n", i18n.T(msgid.CliUpStartingProjectDeployTarget, cfg.Project, cfg.Deploy.Target))
	warnTargetOnlyFields(opts, cfg)
	if flags.crashLinesSet && !anyModeLocal(cfg.Components) {
		renderWarnings(opts, []*clierr.Error{clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.CliUpCrashLinesHasNoEffect))})
	}

	// 先确认"要部到哪"，再做任何生成与拉取：部错集群是不可逆的，
	// 而且这时连一份生成物都还没落盘
	if cfg.Deploy.Target == config.TargetK8s && !flags.dryRun {
		eng, err := resolveEngineFor(opts, cfg)
		if err != nil {
			return nil, err
		}
		if err := requireContext(ctx, opts, cfg, eng, contextOf(cfg, flags.kubeContext)); err != nil {
			return nil, err
		}
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	// 版本号变了就报一句（004 §3.5.1）。检测只读配置与本地缓存，不碰网络；
	// 差异描述与产物下载要等依赖图建好，见 describeUpgrades
	plan.upgrades = detectUpgrades(layout, cfg)
	renderUpgradeBanner(opts, plan.upgrades)

	plan.graph, plan.states, err = resolveTopology(ctx, client, cfg)
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, plan.graph.Warnings)
	renderStates(opts, plan.states)
	// "一个都不启动"紧跟在状态表后面：它解释的就是那张全 ⬜ 的表，
	// 中间隔着几条资源警告的话，使用者读到的顺序就成了"先看一堆无关的警告"
	if plan.states.Empty() {
		renderNothingRunning(opts, plan.states)
		renderSyncHint(opts, layout, plan.states)
		plan.done = true
		return plan, nil
	}
	renderDegradedWeakDeps(opts, plan.graph, plan.states)
	renderSyncHint(opts, layout, plan.states)
	warnDanglingBindings(opts, cfg)
	warnHardcodedPasswords(opts, cfg)
	warnConfigSecrets(opts, cfg, plan.graph)
	warnExistingSecretConfigIssues(opts, cfg, plan.graph)

	// 资源绑定必须在生成之前查（006 §4.4、011 §5.3）：没绑定就一个
	// DATABASE_* 都注不进去，而那份 compose 看上去完全正常——
	// 组件要到运行时才炸成"连不上库"，一句把配置遗漏指向别处的错误。
	//
	// `--dry-run` 时降级成警告：那条命令的语义是"告诉我会发生什么"，
	// 拿它阻断的话，一个还没配资源的项目连"看看会生成什么"都做不到
	// （试用指南 04 讲 mode 时用的正是这条命令，那时资源还没登场）。
	if problem := resolver.CheckRunningResourceBindings(
		cfg, plan.graph, plan.states.Running()); problem != nil {
		if !flags.dryRun {
			return nil, problem
		}
		renderWarnings(opts, []*clierr.Error{dryRunResourceWarning(problem)})
	}

	// egress 覆盖不全是同一类问题：运行期会出事，不是生成不出来。
	// 所以它也归命令层决定阻不阻断——从前它在生成器内部硬拦，于是一个正在配
	// egress 的人连"看看会生成什么策略"都做不到（k8s.CheckEgressCoverage）
	if problem := k8s.CheckEgressCoverage(cfg, runningIDs(plan.states)); problem != nil {
		if !flags.dryRun {
			return nil, problem
		}
		renderWarnings(opts, []*clierr.Error{dryRunEgressWarning(problem)})
	}

	order, err := resolver.Order(plan.graph.Subgraph(plan.states.Running()))
	if err != nil {
		return nil, err
	}
	renderOrder(opts, order, plan.graph)

	if err := checkLocalSources(layout, cfg, plan.states.Running()); err != nil {
		return nil, err
	}

	env, err := inject.Build(cfg, plan.graph, plan.states)
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, env.Warnings)

	if err := plan.generate(opts, env); err != nil {
		return nil, err
	}
	plan.collectTargets(order)
	// plan.generated 只在 docker 目标下才有值（k8s 目标 generate() 只填 plan.k8s，
	// 见上面 generate 的实现）；k8s 目标下 mode: local 已经在配置解析阶段被拒绝
	// （005 §5.6：docker only），所以这里判空跳过既不会漏掉真实的 local 组件，
	// 也避免对 nil 的 plan.generated 取字段直接 panic。
	if plan.generated != nil {
		plan.localComponents, err = collectLocalComponents(
			layout, cfg, plan.graph, order, plan.generated.LocalEnvFiles, envLookup(opts.WorkDir))
		if err != nil {
			return nil, err
		}
	}
	// 放在生成之后：这一步只补摘要用的差异描述与新版本产物，
	// 它取不到东西也不该拦住已经算好的这份计划（004 §10.1）
	describeUpgrades(ctx, opts, layout, client, plan.graph, plan.upgrades)
	return plan, nil
}

// renderNothingRunning 解释"一个组件都不启动"，并说清楚该去改哪一行。
//
// 把顶层逐个列出来，而不是替使用者总结成一句话。顶层有两种死法：自己被
// mode: disable 关掉，或者它的强依赖被关掉、它跟着倒下——两者指向配置里
// **不同的**行。每个顶层自己的理由已经写明是哪一种，照抄比概括可靠。
//
// 从前这里只要看到**任何**组件是 StateDisabled 就断言"顶层都被关掉了"。
// 关掉一个底层组件时那句话是错的：顶层根本没写过 mode: disable，
// 照着去找只会扑空，还把人从上面那张表已经写对的答案上引开
// （表里写的是"不启动（强依赖 X 不启动）"）。
//
// 还有一种成因是**图里根本没有顶层**：每个组件都被别的组件依赖着
// （只可能是弱依赖成环，强依赖成环在解析阶段就报错了）。那时"跟着上层走"
// 解释不了任何事——上层是谁？没有上层。
func renderNothingRunning(opts *Options, states *cascade.Result) {
	opts.Printf("%s\n", i18n.T(msgid.CliUpNoComponentWillStartThis))

	tops := states.TopLevel()
	if len(tops) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliUpNoTopLevelComponentWas))
		opts.Printf("%s\n", i18n.T(msgid.CliUpWriteEnabledTrueForThe))
		return
	}

	opts.Printf("%s\n", i18n.T(msgid.CliUpNoneOfTheTopLevel))
	for _, c := range tops {
		opts.Printf("      %s  %s\n", c.Ref, c.Reason)
	}
	opts.Printf("   %s\n", nothingRunningHint(tops))
}

// nothingRunningHint 指出最短的一条出路。
//
// 只要有一个顶层是被显式关掉的，那就是最省事的一行——删掉它，它下面整条链
// 跟着回来。一个都没有时，顶层全是被强依赖拖下水的：那时配置里压根没有
// 可删的 mode: disable，得顺着上面每行的理由往下找真正被关掉的那个。
func nothingRunningHint(tops []cascade.Component) string {
	for _, c := range tops {
		if c.State == cascade.StateDisabled {
			return i18n.T(msgid.CliUpRemoveEnabledFalseFromOne)
		}
	}
	return i18n.T(msgid.CliUpTheTopLevelItselfIsn)
}

// renderDegradedWeakDeps 说清楚"这次哪些弱依赖没跑、谁因此拿不到什么"。
//
// # 为什么值得单独说一句
//
// 状态表里只有一行「⬜ demo/hello 显式禁用」，依赖图里只有一条「（弱）」。
// 两处都对，但"于是 demo/caller 这次会走降级分支"要使用者自己把它们对起来——
// 而弱依赖的整个约定就建立在"调用方拿不到 *_ENDPOINT 时自己降级"上（002 §3.4）。
// 003 §4.3 与 004 §4.1 / §4.5 都承诺过这一句。
//
// # 为什么是 💡 而不是 ⚠️
//
// 关掉只被弱依赖引用的组件，正是 003 §4.3 推荐的"嫌容器多就下手"的做法，
// `up --dry-run` 甚至专门列出那份可以下手的名单。给一个推荐动作配警告，
// 只会训练使用者整块跳过警告区——而真正要紧的那几条也一起被跳过。
//
// 与它对应的**警告**是另一回事：弱依赖**取不到**（安装源里没有）那是异常，
// 由解析器报 ⚠️（resolver.optionalMissingWarning）。两者从前被 004 §4.5
// 合成一条，说辞也只有一种，现已拆开。
func renderDegradedWeakDeps(opts *Options, graph *resolver.Graph, states *cascade.Result) {
	if graph == nil || states == nil {
		return
	}

	type pair struct{ dep, dependent resolver.Ref }
	var pairs []pair
	for _, node := range graph.Nodes {
		if !states.IsRunning(node.Ref) {
			continue // 它自己都不跑，谈不上"它拿不到"
		}
		for _, dep := range node.Optional {
			if !states.IsRunning(dep) {
				pairs = append(pairs, pair{dep: dep, dependent: node.Ref})
			}
		}
	}
	if len(pairs) == 0 {
		return
	}

	// 按依赖方排序：同一份配置每次都要给出同一个顺序
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].dep != pairs[j].dep {
			return pairs[i].dep.String() < pairs[j].dep.String()
		}
		return pairs[i].dependent.String() < pairs[j].dependent.String()
	})

	opts.Printf("%s\n", i18n.T(msgid.CliUpSomeOptionalDependenciesArenT))
	for _, p := range pairs {
		opts.Printf("%s\n", i18n.T(msgid.CliUpDoesnTStartCanT, p.dep, p.dependent.ID, manifest.EndpointEnvVar(p.dep.ID)))
	}
}

// renderSyncHint 提醒可以把不启动的组件源码收起来。
//
// sync 不由 up 自动执行（012 §2.17：up 管运行时，sync 管源码目录），
// 但"忘了 sync"是最常见的落差——改完 enabled 跑了 up，源码目录还是老样子。
// 只在真有源码可收时才提，否则每次 up 都多一行噪音。
func renderSyncHint(opts *Options, layout config.Layout, states *cascade.Result) {
	n := 0
	for _, c := range states.Components {
		if c.State != cascade.StateRunning && workspace.Exists(layout, c.Ref.ID) {
			n++
		}
	}
	if n == 0 {
		return
	}
	opts.Printf("%s\n", i18n.TN(msgid.CliUpComponentsArenTStartingThis, n, n, workspace.DisplayArchivedRoot()))
}

// dryRunResourceWarning 把"资源未绑定"降级成 --dry-run 下的警告。
//
// 换掉标题与建议里"已阻断"那层意思，其余明细原样保留——
// 使用者要看的是"哪个组件缺哪个资源"，那部分两种模式下完全一样。
func dryRunResourceWarning(problem *clierr.Error) *clierr.Error {
	w := clierr.Warn(problem.Code, i18n.T(msgid.CliUpWarningResourceDependenciesAreNot))
	w.Details = problem.Details
	return w.WithHint(
		i18n.T(msgid.CliUpTheGeneratedDeploymentFilesWill),
		i18n.T(msgid.CliUpDeclareAndBindThemUnder),
	)
}

// runningIDs 是本次会启动的组件 ID（去重）。
func runningIDs(states *cascade.Result) []string {
	seen := map[string]bool{}
	var out []string
	for _, ref := range states.Running() {
		if !seen[ref.ID] {
			seen[ref.ID] = true
			out = append(out, ref.ID)
		}
	}
	return out
}

// dryRunEgressWarning 把"egress 没配全"降级成 --dry-run 下的警告。
//
// 与 dryRunResourceWarning 同一个手法：换掉标题与建议里"已阻断"那层意思，
// 其余明细原样保留——使用者要看的是"哪个资源没声明"，那部分两种模式下完全一样。
func dryRunEgressWarning(problem *clierr.Error) *clierr.Error {
	w := clierr.Warn(problem.Code, i18n.T(msgid.CliUpWarningTheEgressPolicyDoesn))
	w.Details = problem.Details
	return w.WithHint(append(problem.Hints,
		i18n.T(msgid.CliUpWithoutDryRunThisBlocks))...)
}

// generate 按部署目标渲染部署文件（005 §5）。
//
// 两种目标共用到这一步为止的**全部**结论（依赖图、级联、注入），
// 只有渲染方式不同——规则写在渲染器里迟早会分叉（D138）。
func (p *upPlan) generate(opts *Options, env *inject.Result) error {
	if p.cfg.Deploy.Target == config.TargetK8s {
		result, err := k8s.Generate(p.cfg, p.graph, p.states, env, k8s.Options{
			Now:    opts.Now,
			Lookup: envLookup(opts.WorkDir),
		})
		if err != nil {
			return err
		}
		p.k8s = result
		renderWarnings(opts, result.Warnings)
		return nil
	}

	result, err := compose.Generate(p.cfg, p.graph, p.states, env, compose.Options{
		Now:    opts.Now,
		Engine: engineName(opts, p.cfg),
		// 只作用于 local-debug 文件：IDE 不做变量替换（见 compose.Options.Lookup）
		Lookup: envLookup(opts.WorkDir),
	})
	if err != nil {
		return err
	}
	p.generated = result
	renderWarnings(opts, result.Warnings)
	return nil
}

// collectTargets 按启动顺序列出要交给引擎的 service、要检查的镜像、会跑的迁移。
//
// mode: debug、mode: local 与 servedBy 的组件全部跳过：三者都没有自己的容器
// （前两个是裸进程——一个在宿主机上跑，一个由 brickkit 自己拉起；第三个代码
// 打进了外壳镜像），镜像也不必检查。跟 compose 渲染器判断"该不该生成
// workload"用的是同一条件（internal/compose/compose.go 的
// entry.Mode == config.ModeDebug / config.ModeLocal / entry.ServedBy != ""）——
// 漏了任何一半的话，它的版本化服务名会混进传给 `docker compose up` 的目标
// 列表，而生成的 compose 文件里根本没有这个 service，真机执行直接报
// no such service，整个命令失败、一个容器都起不来（brickKit 反馈：真机
// brickkit up 对 servedBy 成员报 no_such_service）。
func (p *upPlan) collectTargets(order *resolver.Plan) {
	noWorkload := map[resolver.Ref]bool{}
	for _, c := range p.cfg.Components {
		if c.Mode == config.ModeDebug || c.Mode == config.ModeLocal {
			noWorkload[resolver.Ref{ID: c.ID, Version: c.Version}] = true
			continue
		}
		if c.ServedBy != "" {
			shellRef, ok := shell.ParseRef(c.ServedBy)
			if ok && p.states.IsRunning(shellRef) {
				noWorkload[resolver.Ref{ID: c.ID, Version: c.Version}] = true
			}
			// 外壳没跑（或者 servedBy 格式不对，config.Validate 会挡）：这个
			// 组件按普通组件对待，交给引擎启动——判据必须跟 internal/shell、
			// internal/compose、internal/k8s 保持一致。
		}
	}

	for _, step := range order.Steps {
		ref := step.Ref
		if noWorkload[ref] {
			continue
		}
		node := p.graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		p.services = append(p.services, manifest.ServiceName(ref.ID, ref.Version))
		p.images = append(p.images, imageInfo{
			component: ref.ID + "@" + ref.Version,
			image:     node.Manifest.Deployment.Image,
		})
		if node.Manifest.Migration != nil {
			p.migrations = append(p.migrations, migrationInfo{
				component: ref.ID + "@" + ref.Version,
				command:   strings.Join(node.Manifest.Migration.Command, " "),
			})
		}
	}
}

// start 调引擎把项目跑起来，然后如实汇报每个 service 的状态。
//
// pruneSelector 与 K8s 侧同源，见 projectSelector。Docker 这边只用它的
// "空 / 非空"决定带不带 `--remove-orphans`，值本身用不上。
func start(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan,
	file, pruneSelector string,
) error {
	project := engine.ProjectName(plan.cfg.Project)

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpStarting, eng.Name()))
	if err := eng.Up(ctx, engine.UpRequest{
		File: file, Project: project, ProjectDir: opts.WorkDir, Services: plan.services,
		PruneSelector: pruneSelector,
	}); err != nil {
		return engineFailure(i18n.T(msgid.CliUpStart), err)
	}

	statuses, err := eng.Status(ctx, project)
	if err != nil {
		// 起是起了，只是问不到状态：不该因此判定失败
		opts.Printf("%s\n", i18n.T(msgid.CliUpTheContainerStateCouldNot, clierr.As(err).Message))
		opts.Printf("%s\n", i18n.T(msgid.CliUpK8sCheckAgainWithBrickkitStatus))
		return nil
	}
	if err := reportStarted(opts, plan, statuses); err != nil {
		return err
	}

	return runLocalComponents(ctx, opts, plan.layout, plan.localComponents, plan.crashLines)
}

// reportStarted 汇报启动结果，并在有组件没起来时给出非零退出码。
func reportStarted(
	opts *Options, plan *upPlan, statuses []engine.Status,
) error {
	byService := map[string]engine.Status{}
	for _, s := range statuses {
		byService[s.Service] = s
	}

	var failed []string
	for _, service := range plan.services {
		status, ok := byService[service]
		switch {
		case !ok:
			failed = append(failed, i18n.T(msgid.CliUpNotCreated, service))
		case status.Running():
			opts.Printf("   %-28s %s\n", service, describeStatus(status))
		default:
			failed = append(failed, service+"  "+describeStatus(status))
		}
	}

	if len(failed) > 0 {
		err := clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.CliUpErrorSomeComponentsDidNot))
		for _, item := range failed {
			err = err.WithDetail(i18n.T(msgid.LabelComponent), item)
		}
		if plan.k8s != nil {
			return err.WithHint(
				i18n.T(msgid.CliUpViewTheLogsToFind, logsCommand(engine.K8s, plan.k8s.Namespace, i18n.T(msgid.ServiceNamePlaceholder))),
				i18n.T(msgid.CliUpViewTheEventsKubectlDescribe, plan.k8s.Namespace),
			)
		}
		return err.WithHint(
			i18n.T(msgid.CliUpViewTheLogsToFind, logsCommand(engineName(opts, plan.cfg),
				engine.ProjectName(plan.cfg.Project), i18n.T(msgid.ServiceNamePlaceholder))),
			i18n.T(msgid.CliUpAFailedMigrationLeavesThe),
		)
	}

	opts.Printf("%s\n", i18n.T(msgid.CliUpAllComponentsStarted, len(plan.services)))
	renderNextSteps(opts, plan)
	logging.Info(i18n.T(msgid.LogProjectStarted), "project", plan.cfg.Project, "services", len(plan.services))
	return nil
}

// engineFailure 把引擎的失败变成一条能看的错误。
//
// 引擎已经给出结构化错误时原样透传——它比这里更清楚发生了什么
// （P18 的教训：自作主张换掉下层的说法，会把人引向错误的方向）。
// 只有裸 error 才在这里兜住：不然它会被顶层当成"命令用法不正确"，
// 明明是 docker 挂了，却让使用者去查自己的命令怎么写。
func engineFailure(action string, err error) error {
	if e, ok := clierr.Structured(err); ok {
		return e
	}
	return clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.IOFailed, action)).
		WithDetail(i18n.T(msgid.LabelReason), err.Error()).
		WithHint(i18n.T(msgid.CliUpTheLineAboveIsThe)).
		WithCause(err)
}

// describeStatus 把引擎的状态说成人话。
func describeStatus(s engine.Status) string {
	switch {
	case s.State == "running" && s.Health != "":
		return i18n.T(msgid.CliUpRunning, s.Health)
	case s.State == "exited":
		return i18n.T(msgid.CliUpExitedExitCode, itoa(s.ExitCode))
	default:
		return s.State
	}
}

// renderNextSteps 给出启动之后的常用动作。
func renderNextSteps(opts *Options, plan *upPlan) {
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpViewTheStatusBrickkitStatus))

	if plan.k8s != nil {
		opts.Printf("%s\n", i18n.T(msgid.CliUpViewTheLogs, logsCommand(engine.K8s, plan.k8s.Namespace, "")))
		opts.Printf("%s\n", i18n.T(msgid.CliUpViewThePodsKubectlGet, plan.k8s.Namespace))
		return
	}

	opts.Printf("%s\n", i18n.T(msgid.CliUpViewTheLogsF, logsCommand(engineName(opts, plan.cfg), engine.ProjectName(plan.cfg.Project), "")))
	for _, env := range plan.generated.LocalEnvFiles {
		// mode: local 不提示"去 IDE 里加载"：它已经被 runLocalComponents 启动了，
		// 提示 mode: debug 那句话对它是假的（见 writeLocalEnvFiles 的同一处说明）。
		if env.Mode != config.ModeDebug {
			continue
		}
		opts.Printf("%s\n", i18n.T(msgid.CliUpLocalDebuggingLoadInThe, filepath.Join(".brickkit", "generated", env.Name), env.Ref.ID))
	}
}

// renderMigrations 说明本次会跑哪些迁移（15.25）。
//
// 迁移由部署文件里的一次性容器执行（002 §8.3），CLI 不自己跑；
// 但使用者需要知道"这次会动哪些库"，出问题时也才知道去看哪个容器。
func renderMigrations(opts *Options, migrations []migrationInfo) {
	if len(migrations) == 0 {
		return
	}
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpDatabaseMigrationsThatRunBefore))
	for _, m := range migrations {
		opts.Printf("   %s  %s\n", m.component, m.command)
	}
}

// warnDanglingBindings 提醒"这条资源绑定指向一个配置里没有的组件"。
//
// 只警告不阻断（理由见 config.Config.DanglingBindings）：它的唯一后果是
// 那条绑定不生效。但必须说一句——最常见的成因是使用者手工删掉了组件条目、
// 却漏了绑定，而他多半以为那个组件"还配着库"。
func warnDanglingBindings(opts *Options, cfg *config.Config) {
	dangling := cfg.DanglingBindings()
	if len(dangling) == 0 {
		return
	}

	err := clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpSomeResourceBindingsPointAt))
	for _, d := range dangling {
		err = err.WithDetail(i18n.T(msgid.CliUpResource, d.ResourceID), i18n.T(msgid.CliUpBoundThatComponentIsNot, d.ComponentID))
	}
	renderWarnings(opts, []*clierr.Error{err.
		WithDetail(i18n.T(msgid.LabelImpact), i18n.T(msgid.CliUpThisBindingHasNoEffect)).
		WithHint(
			i18n.T(msgid.CliUpIfYouNoLongerNeed),
			i18n.T(msgid.CliUpIfTheComponentWasDeleted, dangling[0].ComponentID),
		)})
}

// checkImageConcurrency 是同时进行的镜像检查数上限。
//
// 有上限而不是全放出去：这些请求打到的是**同一个 registry**，
// 几百个并发只会撞上限流，那时候不但不快，还会换来一堆 429
// 让人误以为是凭据出了问题。8 足够把串行的时间摊掉一个数量级。
const checkImageConcurrency = 8

// checkImages 检测镜像拉取权限（15.19、004 §10.2）。
//
// 放在启动之前：镜像取不到还硬启，只会得到一堆 ImagePullBackOff，
// 而真正的原因（没登录）埋在引擎的输出里。
//
// # 为什么要并发（36.1）
//
// 本地没有该镜像时，`CheckImage` 会走一次 **registry 往返**。
// 原来这里是串行的，于是 50 个组件就是 50 次串行网络请求：
// 按健康网络 0.3–0.5 秒一次算已经是 15–25 秒，而计划给整个 `up`
// 的预算是 30 秒——第一个容器都还没开始启动。实测这台机器上
// registry 路径慢的时候单次要 12 秒，50 个就是十分钟。
//
// 这是整个 up 链路上唯一不随组件数伸缩的地方（相比之下解析 100 个依赖
// 只要 42µs），而这些检查彼此完全独立，串行没有任何理由。
//
// # 为什么不在第一个失败时就掐断
//
// 掐断能更早报错，但会丢掉两样东西：**哪个**错误被报出来变得不确定
// （同一份配置连跑两次给出不同的错误，使用者会以为问题在飘），
// 以及"还有几个也拉不到"这个信息。让它们跑完，就能一次说清
// 要修几个——总比修一个、重跑、再冒出一个强。
func checkImages(ctx context.Context, opts *Options, eng engine.Engine, images []imageInfo) error {
	if len(images) == 0 {
		return nil
	}

	opts.Printf("\n%s", i18n.T(msgid.CliUpCheckingImagePullPermissions))

	// 按下标存放结果，取错误时才能按**输入顺序**来，与并发完成的顺序无关
	failures := make([]error, len(images))
	sem := make(chan struct{}, checkImageConcurrency)
	var wg sync.WaitGroup

	for i, item := range images {
		wg.Add(1)
		go func(i int, item imageInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := eng.CheckImage(ctx, item.image); err != nil {
				failures[i] = clierr.As(err).WithDetail(i18n.T(msgid.LabelComponent), item.component)
			}
		}(i, item)
	}
	wg.Wait()

	first, total := -1, 0
	for i, err := range failures {
		if err == nil {
			continue
		}
		total++
		if first < 0 {
			first = i
		}
	}
	if first < 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliUpAllPassed))
		return nil
	}

	opts.Printf(" ❌\n")
	err := clierr.As(failures[first])
	if total > 1 {
		err = err.WithDetail(i18n.T(msgid.CliUpAlso),
			i18n.TN(msgid.CliUpTheImagesOfMoreComponents, total-1, total-1))
	}
	return err
}

// resolveEngine 返回要用的容器引擎：注入优先，否则自动检测（005 §7.3）。
func resolveEngine(opts *Options) (engine.Engine, error) {
	if opts.Engine != nil {
		return opts.Engine, nil
	}
	if forced := os.Getenv("BRICKKIT_ENGINE"); strings.TrimSpace(forced) != "" {
		if strings.EqualFold(strings.TrimSpace(forced), engine.Docker) {
			return engine.NewDocker(), nil
		}
	}
	return engine.Detect()
}

// engineName 是生成部署文件时记录的引擎名——注入的引擎优先，否则按生效的
// deploy.target 推断（override.yaml 能把它改成 podman，见 up_k8s.go 的
// resolveEngineFor）。生成文件与"引擎可不可用"是两回事：--dry-run 在没装
// Docker/Podman 的机器上也该能跑。
func engineName(opts *Options, cfg *config.Config) string {
	if opts.Engine != nil {
		return opts.Engine.Name()
	}
	if cfg != nil && cfg.Deploy.Target == config.TargetPodman {
		return compose.EnginePodman
	}
	return compose.EngineDocker
}

// writeGenerated 把部署文件写进 .brickkit/generated/。
func writeGenerated(layout config.Layout, content []byte) (string, error) {
	dir := layout.GeneratedDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpErrorFailedToCreateThe)).
			WithDetail(i18n.T(msgid.LabelPath), dir).
			WithCause(err)
	}

	path := filepath.Join(dir, composeFileName)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpErrorFailedToWriteThe)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithCause(err)
	}
	return path, nil
}

// writeLocalEnvFiles 写出 mode: debug 组件的调试环境变量文件（005 §4.9）。
//
// mode: local 的组件也会出现在 files 里（两者共用同一套"算出本地化环境"的
// 生成逻辑，见 compose.LocalEnvFile.Mode 的文档），但这里要跳过：它由
// brickkit 自己拉起（internal/cli/up_local.go 的 buildLocalEnv 直接用
// LocalEnvFile.Vars 严格展开，不落盘），"No container is generated; start
// it in your IDE" 这句对它是一句假话，会跟紧随其后 mode: local 自己那段
// "会启动"的输出自相矛盾（手动验证 Task 6 Step 5 时发现）。
func writeLocalEnvFiles(opts *Options, layout config.Layout, files []compose.LocalEnvFile) error {
	debugFiles := make([]compose.LocalEnvFile, 0, len(files))
	for _, file := range files {
		if file.Mode == config.ModeDebug {
			debugFiles = append(debugFiles, file)
		}
	}
	if len(debugFiles) == 0 {
		return nil
	}

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpLocalDebuggingLocalTrue))
	for _, file := range debugFiles {
		path := filepath.Join(layout.GeneratedDir(), file.Name)
		if err := os.WriteFile(path, file.Content, 0o600); err != nil {
			return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpErrorFailedToWriteThe2)).
				WithDetail(i18n.T(msgid.LabelPath), path).
				WithCause(err)
		}

		relative := displayPath(opts.WorkDir, path)
		opts.Printf("   %s@%s\n", file.Ref.ID, file.Ref.Version)
		opts.Printf("%s\n", i18n.T(msgid.CliUpNoContainerIsGeneratedStart, file.Port))
		opts.Printf("%s\n", i18n.T(msgid.CliUpEnvironmentVariables, relative))
		opts.Printf("%s\n", i18n.T(msgid.CliUpVsCodeSetEnvfileWorkspacefolder, relative))
	}
	return nil
}

// devResourcesCompose 是仓库里那份开箱即用的开发资源栈（postgres + redis）。
//
// 它是一份**手写**的 compose 文件，不是生成的——与 deploy/market/ 同一个做法。
// 平台不部署基础资源，但"本地想快速起一套"是真实需求，给一条能直接粘的命令
// 比让每个人自己去查 postgres 镜像怎么配要便宜得多。
const devResourcesCompose = "deploy/dev-resources/docker-compose.yaml"

// renderResourceRequirements 告诉使用者哪些基础资源必须先跑起来。
//
// 006 §9.1：平台不部署基础资源，也不建库。但"不代为部署"不等于"不说清楚"——
// 不列出来的话，组件会在启动或迁移时抛出 `connection refused` 或
// `database "xxx" does not exist`，一句把**环境没准备好**指向**平台或组件**的错误。
//
// 每次 up 都打印，不是只在出错时：建库是一次性动作，而"资源得先跑着"
// 是每次启动都要满足的前提。
func renderResourceRequirements(opts *Options, requirements []deploy.ResourceRequirement) {
	if len(requirements) == 0 {
		return
	}

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpTheseBaseResourcesHaveTo))
	needDatabase := false
	for _, r := range requirements {
		opts.Printf("%s\n", i18n.T(msgid.CliUpSSUsedBy, r.ID, r.Engine, r.Host, r.Port, joinComponents(r.Components)))
		for _, db := range r.Databases {
			needDatabase = true
			opts.Printf("%s\n", i18n.T(msgid.CliUpNeedsDatabaseUsedBy, db.Name, joinComponents(db.Components), db.CreateSQL))
		}
	}
	if needDatabase {
		opts.Printf("%s\n", i18n.T(msgid.CliUpTheDatabasesAlsoHaveTo))
	}
	opts.Printf("%s\n", i18n.T(msgid.CliUpToBringOneUpQuickly, devResourcesCompose))
}

func joinComponents(items []string) string {
	return strings.Join(items, i18n.T(msgid.ListSeparator))
}

// displayPath 把绝对路径显示成相对项目根目录的形式。
func displayPath(workDir, path string) string {
	if rel, err := filepath.Rel(workDir, path); err == nil {
		return rel
	}
	return path
}
