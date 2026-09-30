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
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/procsup"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// composeFileName 是生成的部署文件名。
//
// 叫 compose.yaml 而不是 docker-compose.yaml：这份文件遵循的是 Compose
// 规范（compose-spec.io），Docker、Podman 都能消费同一份——带上 docker
// 前缀会让人误以为它是 Docker 专属的，而 target: podman（附录 A15）读的正是
// 同一份文件。
const composeFileName = "compose.yaml"

// newUpCommand 实现 brickkit up。
func newUpCommand(opts *Options) *cobra.Command {
	var (
		dryRun       bool
		ignoreShells bool
		crashLines   int
		focus        string
		all          bool
	)

	cmd := &cobra.Command{
		Annotations: findsProjectAnnotation(),
		Use:         "up",
		Short:       i18n.T(msgid.CliUpShort),
		GroupID:     groupLifecycle,
		Long:        i18n.T(msgid.CliUpLong),
		Example:     i18n.T(msgid.CliUpExample),
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUp(cmd.Context(), opts, upOptions{
				dryRun: dryRun, ignoreShells: ignoreShells,
				crashLines: crashLines, crashLinesSet: cmd.Flags().Changed("crash-lines"),
				focus: focus, all: all,
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, i18n.T(msgid.CliUpOnlyGenerateTheDeploymentFiles))
	addDeployFileFlags(cmd, opts)
	cmd.Flags().BoolVar(&ignoreShells, "ignore-shells", false,
		i18n.T(msgid.CliUpIgnoreShellsFlag))
	cmd.Flags().IntVar(&crashLines, "crash-lines", procsup.DefaultTailLines,
		i18n.T(msgid.CliUpCrashLinesHowManyLinesOfOutput))
	cmd.Flags().StringVar(&focus, "focus", "", i18n.T(msgid.CliUpFlagFocus))
	_ = cmd.RegisterFlagCompletionFunc("focus", completeProjectIDs(opts))
	cmd.Flags().BoolVar(&all, "all", false, i18n.T(msgid.CliUpFlagAll))
	return cmd
}

// upPlan 是"这次 up 要做什么"的全部结论。
//
// 生成与启动共用同一份计划：--dry-run 与真启动之间唯一的差别应当是
// "有没有真的调引擎"，而不是两条各自算一遍、可能算出不同结果的路径。
type upPlan struct {
	proj      *project.Project
	graph     *resolver.Graph
	states    *cascade.Result
	generated *compose.Result
	// k8s 是 deploy.target: k8s 时的生成结果（与 generated 互斥）。
	k8s *k8s.Result
	// kubeContext 是本次钉住的 kubeconfig 上下文（部署文件的 k8s.context）。
	kubeContext string
	// services 是本次要交给引擎启动的 service（不含 local 组件与迁移容器）。
	services []string
	// migrations 是本次会执行的迁移，供输出。
	migrations []migrationInfo
	// images 是要检查拉取权限的镜像。
	images []imageInfo
	// upgrades 是本次检测到的版本变更。
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
	ref       resolver.Ref
	manifest  *manifest.Manifest
	// needsLocal：没有 image 的版本、本地安装源给出的版本，镜像必须已经在本机（brickkit build）
	needsLocal bool
	// localSource：本地安装源给出的版本——本机镜像还必须是从本地代码构建的（io.brickkit.build=local）
	localSource bool
}

// upOptions 是 up 的命令行选项。
type upOptions struct {
	dryRun bool
	// ignoreShells 是 --ignore-shells 的值：内存里当作没有任何外壳成员关系
	// 再跑一次，验证"每个组件必须能独立 brickkit up 起来"这条设计原则，
	// 从不写回部署文件（project.IgnoreShells）。
	ignoreShells bool
	// crashLines 是 --crash-lines 的值；crashLinesSet 为 true 才说明用户真的
	// 传了这个旗位（不能靠"值等不等于默认值"判断——用户完全可能手写
	// --crash-lines 20，跟不传时拿到的默认值撞在一起）。
	crashLines    int
	crashLinesSet bool
	// focus 是 --focus 的值，all 是 --all：写进 deploy.local.yaml 的焦点意图。
	focus string
	all   bool
}

// runUp 执行 brickkit up。
func runUp(ctx context.Context, opts *Options, flags upOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := applyFocusIntent(opts, flags); err != nil {
		return err
	}

	plan, err := buildUpPlan(ctx, opts, flags)
	if err != nil || plan.done {
		return err
	}
	if plan.k8s != nil {
		return upK8s(ctx, opts, flags, plan)
	}

	if err := pruneOtherTarget(plan.proj.Layout, false); err != nil {
		return err
	}
	path, err := writeGenerated(plan.proj.Layout, plan.generated.YAML)
	if err != nil {
		return err
	}
	if err := writeEnvFiles(plan.proj.Layout, plan.generated.EnvFiles); err != nil {
		return err
	}
	if err := writeLocalEnvFiles(opts, plan.proj.Layout, plan.generated.LocalEnvFiles); err != nil {
		return err
	}
	opts.Printf("%s\n", i18n.T(msgid.CliUpGenerated, opts.display(path)))
	// 在 --dry-run 的分岔**之前**："这次会动哪些库"正是 dry-run 最该回答的问题
	// 之一，而它与升不升级无关。从前它在分岔之后，于是 dry-run 里一个字都没有，
	// 唯一提到迁移的地方是升级摘要里那一行——还得先检测到升级才会出现
	renderMigrations(opts, plan.migrations)

	if flags.dryRun {
		renderUpgradeSummary(opts, plan)
		renderLocalComponentCommands(opts, plan.localComponents)
		opts.Printf("\n%s\n", i18n.T(msgid.CliUpDryRunOnlyGeneratesThe))
		opts.Printf("%s\n", i18n.T(msgid.CliUpViewItCat, opts.display(path)))
		logging.Info(i18n.T(msgid.LogDeployFilesGenerated), "path", path)
		return nil
	}

	if len(plan.services) == 0 && len(plan.runAfter()) == 0 {
		// 这次没有任何组件需要容器（可能全是 mode: local / mode: debug，
		// 或者全被外壳承载）——不该去起一个引擎：`docker compose
		// up` 对着一份 `services: {}` 的空文件会报 "no service selected"，
		// 而且一个纯 mode: local 的项目本不该被要求装 Docker（手动验证
		// 时用真实 docker 跑出来的：demo/hello 单组件、mode: local，
		// 之前这里会直接报 ENGINE_FAILED，明明这个项目一个容器都不需要）。
		stopPreviousContainers(ctx, opts, plan)
		return runLocalComponents(ctx, opts, plan.proj.Layout, plan.localComponents, plan.crashLines)
	}

	eng, err := resolveEngineFor(opts, plan.proj)
	if err != nil {
		return err
	}
	if err := checkUpImages(ctx, opts, eng, resolveImages(opts, plan.proj), plan.images); err != nil {
		return err
	}

	return start(ctx, opts, eng, plan, path, projectSelector(plan.proj))
}

// buildUpPlan 从三层文件一路算到"要启动哪些 service"。
func buildUpPlan(ctx context.Context, opts *Options, flags upOptions) (*upPlan, error) {
	proj, err := project.Load(opts.WorkDir, opts.loadOptions())
	if err != nil {
		return nil, err
	}
	renderDeploySource(opts, proj)
	renderFocus(opts, proj)
	renderWarnings(opts, proj.Warnings)
	if err := checkNestedCopies(opts, proj); err != nil {
		return nil, err
	}
	if flags.ignoreShells {
		proj.IgnoreShells()
		opts.Printf("%s\n", i18n.T(msgid.CliUpShellsIgnoredBanner))
	}

	plan := &upPlan{proj: proj, kubeContext: proj.Deploy.Settings().Context, crashLines: flags.crashLines}
	if len(proj.Decl.Components) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliStatusTheCurrentProjectHasNo))
		opts.Printf("%s\n", i18n.T(msgid.CliStatusAddAllTheComponentsUnder, project.DirComponents))
		opts.Printf("%s\n", i18n.T(msgid.CliStatusOrAddOneFromAn))
		plan.done = true
		return plan, nil
	}

	opts.Printf("%s\n", i18n.T(msgid.CliUpStartingProjectDeployTarget, proj.Decl.Project, proj.Deploy.Target))
	if flags.crashLinesSet && !anyModeLocal(proj) {
		renderWarnings(opts, []*clierr.Error{clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.CliUpCrashLinesHasNoEffect))})
	}

	// 先确认"要部到哪"，再做任何生成与拉取：部错集群是不可逆的，
	// 而且这时连一份生成物都还没落盘
	if proj.Deploy.Target == deployfile.TargetK8s && !flags.dryRun {
		eng, err := resolveEngineFor(opts, proj)
		if err != nil {
			return nil, err
		}
		if err := requireContext(ctx, opts, proj, eng, plan.kubeContext); err != nil {
			return nil, err
		}
	}

	client, err := newSourceClient(opts, proj.Layout, proj.Decl, source.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	// 版本号变了就报一句。检测只读配置与本地缓存，不碰网络
	plan.upgrades = detectUpgrades(proj)
	renderUpgradeBanner(opts, plan.upgrades)

	plan.graph, plan.states, err = resolveTopology(ctx, client, proj)
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, plan.graph.Warnings)
	renderStates(opts, plan.states)
	if plan.states.Empty() {
		renderNothingRunning(opts, plan.states)
		renderSyncHint(opts, proj.Layout, plan.states)
		plan.done = true
		return plan, nil
	}
	renderDegradedWeakDeps(opts, plan.graph, plan.states)
	renderSyncHint(opts, proj.Layout, plan.states)
	warnConfigSecrets(opts, proj, plan.graph)
	warnExistingSecretOnDocker(opts, proj)

	order, err := resolver.Order(plan.graph.Subgraph(plan.states.Running()))
	if err != nil {
		return nil, err
	}
	// 外壳的三处声明先对上，再谈把成员并进外壳（对不上时并出来的图本身就是错的）
	if err := shell.Check(proj, plan.graph, plan.states); err != nil {
		return nil, err
	}
	// 启动顺序按这次真正要起的工作负载排：外壳承载的成员并进外壳（与 depends_on 一致）
	workGraph := shell.Workloads(proj, plan.graph, plan.states)
	workloads, err := resolver.Order(workGraph)
	hosted := hostedMembers(proj, plan.states)
	if err != nil {
		// 组件层面没有环（order 已经排出来了），环是把成员并进外壳才有的。Docker 下外壳是容器时
		// depends_on 真的成环、起不来：点名是哪几条成员依赖造成的。K8s 的 Pod 之间没有启动顺序、
		// 以裸进程运行的外壳不在 compose 里，这两种情况部署完全可行，启动顺序退回按组件排
		if proj.Deploy.Target != deployfile.TargetK8s {
			if cycle := shell.MergeCycleError(proj, plan.graph, plan.states, workGraph); cycle != nil {
				return nil, cycle
			}
		}
		workloads, hosted = order, nil
	}
	renderOrder(opts, workloads, order, plan.graph, hosted, skippedWaits(proj, plan.graph, plan.states))

	if err := checkLocalRepos(proj, plan.states); err != nil {
		return nil, err
	}

	env, err := inject.Build(proj, plan.graph, plan.states)
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, env.Warnings)

	if err := plan.generate(opts, env); err != nil {
		return nil, err
	}
	plan.collectTargets(order)
	for i := range plan.images {
		info := &plan.images[i]
		info.localSource = client.IsLocal(ctx, info.ref.ID, info.ref.Version)
		info.needsLocal = info.manifest.Deployment.Image == "" || info.localSource
	}
	// plan.generated 只在 docker / podman 目标下才有值；k8s 目标下 mode: local 在部署文件
	// 解析阶段就被拒绝了，所以这里判空既不会漏掉真实的 local 组件，也不会对 nil 取字段
	if plan.generated != nil {
		plan.localComponents, err = collectLocalComponents(
			proj, plan.graph, order, plan.generated.LocalEnvFiles)
		if err != nil {
			return nil, err
		}
	}
	// 放在生成之后：这一步只补摘要用的差异描述与新版本产物，
	// 它取不到东西也不该拦住已经算好的这份计划
	describeUpgrades(ctx, opts, proj.Layout, client, plan.graph, plan.upgrades)
	// 下一次的版本变更提示以这次为基线
	refs := make([]string, 0, len(proj.Decl.Components))
	for _, c := range proj.Decl.Components {
		refs = append(refs, c.Ref())
	}
	project.WriteLastRun(proj.Layout, refs)
	return plan, nil
}

// renderDeploySource 说一句这次读的是哪份部署文件：本地模式与 -f 都会改变答案，
// 不说的话使用者很容易以为自己跑的是团队的 deploy.yaml。
func renderDeploySource(opts *Options, proj *project.Project) {
	name := filepath.Base(proj.DeployPath)
	switch proj.DeploySource {
	case project.DeployLocal:
		opts.Printf("%s\n", i18n.T(msgid.CliUpUsingLocalDeployFile, name))
	case project.DeployExplicit:
		if proj.LocalModeOn {
			opts.Printf("%s\n", i18n.T(msgid.CliUpUsingExplicitDeployFile, name))
		} else {
			opts.Printf("%s\n", i18n.T(msgid.CliUpUsingDeployFile, name))
		}
	}
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
		opts.Printf("%s\n", i18n.T(msgid.CliUpHintEnableTheOneToRun))
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
			return i18n.T(msgid.CliUpHintRemoveDisableFromOne)
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
// 而弱依赖的整个约定就建立在"调用方拿不到 *_ENDPOINT 时自己降级"上。
// 弱依赖的约定本身就承诺了这一句。
//
// # 为什么是 💡 而不是 ⚠️
//
// 关掉只被弱依赖引用的组件，正是"嫌容器多就下手"的推荐做法，
// `up --dry-run` 甚至专门列出那份可以下手的名单。给一个推荐动作配警告，
// 只会训练使用者整块跳过警告区——而真正要紧的那几条也一起被跳过。
//
// 与它对应的**警告**是另一回事：弱依赖**取不到**（安装源里没有）那是异常，
// 由解析器报 ⚠️（resolver.optionalMissingWarning）。两者从前被
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
// sync 不由 up 自动执行（up 管运行时，sync 管源码目录），
// 但"忘了 sync"是最常见的落差——改完 enabled 跑了 up，源码目录还是老样子。
// 只在真有源码可收时才提，否则每次 up 都多一行噪音。
func renderSyncHint(opts *Options, layout project.Layout, states *cascade.Result) {
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

// generate 按部署目标渲染部署文件。
//
// 两种目标共用到这一步为止的**全部**结论（依赖图、级联、注入），
// 只有渲染方式不同——规则写在渲染器里迟早会分叉。
func (p *upPlan) generate(opts *Options, env *inject.Result) error {
	root := p.proj.Layout.Root
	if p.proj.Deploy.Target == deployfile.TargetK8s {
		result, err := k8s.Generate(p.proj, p.graph, p.states, env, k8s.Options{
			Now:    opts.Now,
			Lookup: envref.Lookup(root),
			Root:   root,
		})
		if err != nil {
			return err
		}
		p.k8s = result
		renderWarnings(opts, result.Warnings)
		return nil
	}

	result, err := compose.Generate(p.proj, p.graph, p.states, env, compose.Options{
		Now:    opts.Now,
		Engine: engineName(opts, p.proj),
		Root:   root,
		Lookup: envref.Lookup(root),
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
// mode: debug、mode: local 与 外壳成员全部跳过：三者都没有自己的容器
// （前两个是裸进程——一个在宿主机上跑，一个由 brickkit 自己拉起；第三个代码
// 打进了外壳镜像），镜像也不必检查。跟 compose 渲染器判断"该不该生成
// workload"用的是同一条件（internal/compose/compose.go 的
// IsBareProcess / cascade.Result.HostOf）——
// 漏了任何一半的话，它的版本化服务名会混进传给 `docker compose up` 的目标
// 列表，而生成的 compose 文件里根本没有这个 service，真机执行直接报
// no such service，整个命令失败、一个容器都起不来（brickKit 反馈：真机
// brickkit up 对 外壳成员报 no_such_service）。
func (p *upPlan) collectTargets(order *resolver.Plan) {
	noWorkload := map[resolver.Ref]bool{}
	for _, c := range p.proj.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		if p.proj.DeployEntry(c.ID, c.Version).IsBareProcess() {
			noWorkload[ref] = true
			continue
		}
		// 外壳没跑时成员按普通组件对待，交给引擎启动——与 internal/shell、internal/compose、
		// internal/k8s 同一个判据（cascade.Result.HostOf）
		if _, hosted := p.states.HostOf(p.proj, ref); hosted {
			noWorkload[ref] = true
		}
	}

	for _, step := range order.Steps {
		ref := step.Ref
		if noWorkload[ref] {
			// 被外壳承载的成员没有主容器，但它的迁移照样用它自己的镜像跑（提案 §8.9.4）
			if _, hosted := p.states.HostOf(p.proj, ref); hosted {
				if node := p.graph.Node(ref); node != nil && node.Manifest != nil && node.Manifest.Migration != nil {
					p.images = append(p.images, newImageInfo(node))
					p.migrations = append(p.migrations, migrationInfo{
						component: ref.ID + "@" + ref.Version,
						command:   strings.Join(node.Manifest.Migration.Command, " "),
					})
				}
			}
			continue
		}
		node := p.graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		p.services = append(p.services, manifest.ServiceName(ref.ID, ref.Version))
		p.images = append(p.images, newImageInfo(node))
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
	project := engine.ProjectName(plan.proj.Decl.Project)

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpStarting, eng.Name()))
	if err := eng.Up(ctx, engine.UpRequest{
		File: file, Project: project, ProjectDir: opts.WorkDir, Services: plan.services,
		RunAfter:      plan.runAfter(),
		PruneSelector: pruneSelector,
	}); err != nil {
		if failed := failedMigration(ctx, opts, eng, plan, project, err); failed != nil {
			return failed
		}
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

	return runLocalComponents(ctx, opts, plan.proj.Layout, plan.localComponents, plan.crashLines)
}

// failedMigration 在 up 失败后问一次引擎：有迁移容器非零退出，就是它挡住了主服务——点名是哪个
// 组件的迁移、日志怎么看（K8s 下的同一件事由 engine 报 MIGRATION_FAILED）。认不出时返回 nil，
// 由 engineFailure 原样报引擎的说法。
func failedMigration(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan, project string, cause error,
) error {
	statuses, err := eng.Status(ctx, project)
	if err != nil {
		return nil
	}
	byService := map[string]resolver.Ref{}
	for _, ref := range plan.states.Running() {
		byService[compose.MigrationService(manifest.ServiceName(ref.ID, ref.Version))] = ref
	}
	for _, s := range statuses {
		ref, ok := byService[s.Service]
		if !ok || s.State != "exited" || s.ExitCode == 0 {
			continue
		}
		return clierr.New(clierr.CodeMigrationFailed, i18n.T(msgid.EngineMigrationFailed)).
			WithDetail(i18n.T(msgid.LabelComponent), ref.String()).
			WithDetail(i18n.T(msgid.EngineLabelLogs), logsCommand(engineName(opts, plan.proj), project, s.Service)).
			WithHint(i18n.T(msgid.CliUpMigrationBlocksMain), i18n.T(msgid.CliUpMigrationFixAndRerun)).
			WithCause(cause)
	}
	return nil
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
			i18n.T(msgid.CliUpViewTheLogsToFind, logsCommand(engineName(opts, plan.proj),
				engine.ProjectName(plan.proj.Decl.Project), i18n.T(msgid.ServiceNamePlaceholder))),
			i18n.T(msgid.CliUpAFailedMigrationLeavesThe),
		)
	}

	opts.Printf("%s\n", i18n.T(msgid.CliUpAllComponentsStarted, len(plan.services)))
	renderNextSteps(opts, plan)
	logging.Info(i18n.T(msgid.LogProjectStarted), "project", plan.proj.Decl.Project, "services", len(plan.services))
	return nil
}

// engineFailure 把引擎的失败变成一条能看的错误。
//
// 引擎已经给出结构化错误时原样透传——它比这里更清楚发生了什么
// （教训：自作主张换掉下层的说法，会把人引向错误的方向）。
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

	opts.Printf("%s\n", i18n.T(msgid.CliUpViewTheLogsF, logsCommand(engineName(opts, plan.proj), engine.ProjectName(plan.proj.Decl.Project), "")))
	for _, env := range plan.generated.LocalEnvFiles {
		// mode: local 不提示"去 IDE 里加载"：它已经被 runLocalComponents 启动了，
		// 提示 mode: debug 那句话对它是假的（见 writeLocalEnvFiles 的同一处说明）。
		if env.Mode != deployfile.ModeDebug {
			continue
		}
		opts.Printf("%s\n", i18n.T(msgid.CliUpLocalDebuggingLoadInThe, filepath.Join(".brickkit", "generated", env.Name), env.Ref.ID))
	}
}

// renderMigrations 说明本次会跑哪些迁移。
//
// 迁移由部署文件里的一次性容器执行，CLI 不自己跑；
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

// checkImageConcurrency 是同时进行的镜像检查数上限。
//
// 有上限而不是全放出去：这些请求打到的是**同一个 registry**，
// 几百个并发只会撞上限流，那时候不但不快，还会换来一堆 429
// 让人误以为是凭据出了问题。8 足够把串行的时间摊掉一个数量级。
const checkImageConcurrency = 8

// newImageInfo 是一个工作负载要用的镜像：名字一律来自 manifest.ImageRef（只有 build 的组件
// 没有 image 字段，名字是推出来的）。
func newImageInfo(node *resolver.Node) imageInfo {
	return imageInfo{
		component: node.Ref.String(), image: manifest.ImageRef(node.Manifest),
		ref: node.Ref, manifest: node.Manifest,
	}
}

// checkUpImages 是 up 的镜像检查（提案 §9.10.3，命令表 10 第 3 步）：up 从不构建。
//
//	本机构建的镜像（没有 image、或本地安装源的版本）   必须已经在本机，否则一次列出全部，提示 build
//	拉取的镜像                                          在本机、或 registry 取得到（checkImages）
//	本机的外壳镜像                                      记下的成员版本要与 shell.members 一致（附录 A24）
func checkUpImages(ctx context.Context, opts *Options, eng engine.Engine, local engine.Images, images []imageInfo) error {
	var pulled, missing []imageInfo
	for _, info := range images {
		if !info.needsLocal {
			pulled = append(pulled, info)
			continue
		}
		exists, err := local.ImageExists(ctx, info.image)
		if err != nil {
			return err
		}
		if exists && info.localSource {
			labels, _, err := local.ImageLabels(ctx, info.image)
			if err != nil {
				return err
			}
			exists = labels[labelBuild] == buildLocal
		}
		if !exists {
			missing = append(missing, info)
		}
	}
	if len(missing) > 0 {
		e := clierr.New(clierr.CodeImageMissing, i18n.T(msgid.CliUpImagesNeedBuild))
		for _, m := range missing {
			e = e.WithDetail(m.component, m.image)
		}
		// 每条建议是一句完整的话：只缺一个就直接给它的命令；缺好几个时第一条全部构建，其余逐个点名。
		if len(missing) == 1 {
			return e.WithHint(i18n.T(msgid.CliUpHintBuildNeverAutomatic, "brickkit build "+missing[0].component))
		}
		hints := []string{i18n.T(msgid.CliUpHintBuildNeverAutomatic, "brickkit build")}
		for _, m := range missing {
			hints = append(hints, i18n.T(msgid.CliUpHintBuildJustOne, "brickkit build "+m.component))
		}
		return e.WithHint(hints...)
	}
	if err := checkImages(ctx, opts, eng, pulled); err != nil {
		// 拉不到时还有一条出路：从源码在本机构建（提案 §9.10.3）
		e := clierr.As(err)
		for _, d := range e.Details {
			if d.Key == i18n.T(msgid.LabelComponent) {
				e = e.WithHint(i18n.T(msgid.CliUpHintBuildInstead, d.Value))
				break
			}
		}
		return e
	}
	return checkShellImageLabels(ctx, opts, local, images)
}

// checkShellImageLabels：本机上的外壳镜像是构建时编进的成员版本的快照（brickkit build 记在标签里）。
// 与 shell.members 对不上说明改了成员版本却没重建——跑起来的是旧代码。没有标签（手工构建、第三方
// 镜像）只能警告；只在 registry 里的镜像是发布出去的版本，天然一致，不查。
func checkShellImageLabels(ctx context.Context, opts *Options, local engine.Images, images []imageInfo) error {
	for _, info := range images {
		// 只核对本机构建的外壳镜像：拉取的镜像（哪怕已经缓存在本机）是发布出去的版本
		if !info.manifest.IsShell() || !info.needsLocal {
			continue
		}
		labels, present, err := local.ImageLabels(ctx, info.image)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		declared := shellMembersLabel(info.manifest.Shell.Members)
		recorded, labelled := labels[labelShellMembers]
		switch {
		case !labelled:
			opts.Printf("%s", clierr.Warn(clierr.CodeImageUnverified, i18n.T(msgid.CliUpShellImageUnlabelled, info.component)).
				WithDetail(i18n.T(msgid.LabelImage), info.image).Format())
		case recorded != declared:
			return clierr.New(clierr.CodeImageStale, i18n.T(msgid.CliUpShellImageStale, info.component)).
				WithDetail(i18n.T(msgid.LabelImage), info.image).
				WithDetail(i18n.T(msgid.CliUpLabelImageMembers), recorded).
				WithDetail(i18n.T(msgid.CliUpLabelDeclaredMembers), declared).
				WithHint(i18n.T(msgid.CliUpHintRebuildShell, info.component))
		}
	}
	return nil
}

// checkImages 检测镜像拉取权限。
//
// 放在启动之前：镜像取不到还硬启，只会得到一堆 ImagePullBackOff，
// 而真正的原因（没登录）埋在引擎的输出里。
//
// # 为什么要并发
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

// resolveEngine 返回要用的容器引擎：注入优先，否则自动检测。
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
// 部署文件的 target 推断（podman 也写在那里，见 up_k8s.go 的
// resolveEngineFor）。生成文件与"引擎可不可用"是两回事：--dry-run 在没装
// Docker/Podman 的机器上也该能跑。
func engineName(opts *Options, proj *project.Project) string {
	if opts.Engine != nil {
		return opts.Engine.Name()
	}
	if proj != nil && proj.Deploy.Target == deployfile.TargetPodman {
		return compose.EnginePodman
	}
	return compose.EngineDocker
}

// writeGenerated 把部署文件写进 .brickkit/generated/。
func writeGenerated(layout project.Layout, content []byte) (string, error) {
	dir := layout.GeneratedDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpErrorFailedToCreateThe)).
			WithDetail(i18n.T(msgid.LabelPath), dir).
			WithCause(err).WithHint(i18n.T(msgid.HintCheckDiskAccess))
	}

	path := filepath.Join(dir, composeFileName)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpDeployFilesWriteFailed)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithCause(err).WithHint(i18n.T(msgid.HintCheckDiskAccess))
	}
	return path, nil
}

// writeLocalEnvFiles 写出 mode: debug 组件的调试环境变量文件。
//
// mode: local 的组件也会出现在 files 里（两者共用同一套"算出本地化环境"的
// 生成逻辑，见 compose.LocalEnvFile.Mode 的文档），但这里要跳过：它由
// brickkit 自己拉起（internal/cli/up_local.go 的 buildLocalEnv 直接用
// LocalEnvFile.Vars 严格展开，不落盘），"No container is generated; start
// it in your IDE" 这句对它是一句假话，会跟紧随其后 mode: local 自己那段
// "会启动"的输出自相矛盾（用真实进程手动验证时发现）。
func writeLocalEnvFiles(opts *Options, layout project.Layout, files []compose.LocalEnvFile) error {
	debugFiles := make([]compose.LocalEnvFile, 0, len(files))
	keep := map[string]bool{}
	for _, file := range files {
		if file.Mode == deployfile.ModeDebug {
			debugFiles = append(debugFiles, file)
			keep[file.Name] = true
		}
	}
	// 这次不再是 mode: debug 的组件：它上一次的调试文件（可能带着密钥）不能继续躺在磁盘上
	if err := removeLocalEnvFiles(layout, keep); err != nil {
		return err
	}
	if len(debugFiles) == 0 {
		return nil
	}

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpDebugSectionTitle))
	for _, file := range debugFiles {
		path := filepath.Join(layout.GeneratedDir(), file.Name)
		if err := os.WriteFile(path, file.Content, 0o600); err != nil {
			return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpDebugEnvWriteFailed)).
				WithDetail(i18n.T(msgid.LabelPath), path).
				WithCause(err).WithHint(i18n.T(msgid.HintCheckDiskAccess))
		}

		relative := opts.display(path)
		opts.Printf("   %s@%s\n", file.Ref.ID, file.Ref.Version)
		opts.Printf("%s\n", i18n.T(msgid.CliUpNoContainerIsGeneratedStart, file.Port))
		opts.Printf("%s\n", i18n.T(msgid.CliUpEnvironmentVariables, relative))
		opts.Printf("%s\n", i18n.T(msgid.CliUpVsCodeSetEnvfileWorkspacefolder, relative))
		// 这份文件对找不到的 ${VAR} 是宽松的（留着占位符，看得出漏了哪个）；不说出来，
		// 进程就带着字面量的 ${VAR} 跑起来，没有任何报错
		names := make([]string, 0, len(file.Unresolved))
		for name := range file.Unresolved {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			opts.Printf("%s\n", i18n.T(msgid.CliUpDebugEnvUnresolved, file.Ref.String(), name, file.Unresolved[name]))
		}
	}
	return nil
}

// localEnvPattern 匹配 mode: debug 组件的调试环境变量文件（compose.LocalEnvFile.Name）；
// legacyLocalEnvFile 是按服务名分文件之前的单文件格式，同样可能带着密钥。
const (
	localEnvPattern    = "local-debug.*.env"
	legacyLocalEnvFile = "local-debug.env"
)

// removeLocalEnvFiles 删掉 keep 之外的调试环境变量文件。
func removeLocalEnvFiles(layout project.Layout, keep map[string]bool) error {
	matches, err := filepath.Glob(filepath.Join(layout.GeneratedDir(), localEnvPattern))
	if err != nil {
		return err
	}
	matches = append(matches, filepath.Join(layout.GeneratedDir(), legacyLocalEnvFile))
	for _, path := range matches {
		if !keep[filepath.Base(path)] {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

// pruneOtherTarget 删掉另一种部署目标上一次留下的生成物：生成目录只属于这一次的目标。
// 从 docker 切到 k8s 时，0600 的 env 文件与调试文件里有密钥与 file:// 的内容（附录 A7）；
// 从 k8s 切回来时，k8s/secrets/ 里有明文的 Secret 清单。换了目标还留着它们，
// 就是一份没人再用、也没人记得去删的密钥副本。compose.yaml 不含密钥，但它引用的 env 文件
// 已经删了，一并收走，免得有人对着一份过期的文件手动 docker compose up。
func pruneOtherTarget(layout project.Layout, k8sTarget bool) error {
	if !k8sTarget {
		return os.RemoveAll(filepath.Join(layout.GeneratedDir(), k8sDirName))
	}
	if err := os.RemoveAll(filepath.Join(layout.Root, filepath.FromSlash(compose.EnvFileDir))); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(layout.GeneratedDir(), composeFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeLocalEnvFiles(layout, nil)
}

// stopPreviousContainers 在这次一个容器都不需要时（组件全在宿主机上），停掉本项目上一次留下的
// 容器：它们可能还占着端口，而宿主机上的进程马上要绑同样的端口。down 不删数据卷。
//
// 先问引擎这个项目有没有容器：取不到引擎（没装 Docker）、问不通（守护进程没起）或者一个都没有，
// 就什么都不做——一个纯宿主机的项目不该被要求装 Docker、也不该每次都收到一句警告。有容器却
// 停不掉时照样继续（宿主机进程未必撞端口），但要说一声，否则接下来的"端口被占用"没人看得懂。
func stopPreviousContainers(ctx context.Context, opts *Options, plan *upPlan) {
	eng, err := resolveEngineFor(opts, plan.proj)
	if err != nil {
		return
	}
	project := engine.ProjectName(plan.proj.Decl.Project)
	statuses, err := eng.Status(ctx, project)
	if err != nil || len(statuses) == 0 {
		return
	}
	err = eng.Down(ctx, engine.DownRequest{Project: project, Selector: projectSelector(plan.proj)})
	if err != nil {
		renderWarnings(opts, []*clierr.Error{
			clierr.Warn(clierr.CodeEngineFailed, i18n.T(msgid.CliUpStopPreviousFailed)).
				WithDetail(i18n.T(msgid.LabelReason), err.Error()).
				WithHint(i18n.T(msgid.CliUpHintStopPreviousByHand)),
		})
	}
}

// writeEnvFiles 以 0600 写出密钥与 file:// 内容的 env 文件（附录 A7），并删掉这次
// 没再生成的旧文件——留着的话，一个不再是密钥的值会在磁盘上多躺一份。
func writeEnvFiles(layout project.Layout, files []compose.EnvFile) error {
	dir := filepath.Join(layout.Root, filepath.FromSlash(compose.EnvFileDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpErrorFailedToCreateThe)).
			WithDetail(i18n.T(msgid.LabelPath), dir).WithCause(err).WithHint(i18n.T(msgid.HintCheckDiskAccess))
	}
	keep := map[string]bool{}
	for _, file := range files {
		path := filepath.Join(layout.Root, filepath.FromSlash(file.Path))
		keep[filepath.Base(path)] = true
		if err := os.WriteFile(path, file.Content, 0o600); err != nil {
			return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpDebugEnvWriteFailed)).
				WithDetail(i18n.T(msgid.LabelPath), path).WithCause(err).WithHint(i18n.T(msgid.HintCheckDiskAccess))
		}
		// WriteFile 不改已存在文件的权限：旧文件是 0644 时照样得收紧
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".env") && !keep[e.Name()] {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// displayPath 把绝对路径显示成相对项目根目录的形式。
func displayPath(workDir, path string) string {
	if rel, err := filepath.Rel(workDir, path); err == nil {
		return rel
	}
	return path
}

// runAfter 是引擎要在 up 之后单独跑完的一次性 service（compose.Result.RunAfter）。
func (p *upPlan) runAfter() []string {
	if p.generated == nil {
		return nil
	}
	return p.generated.RunAfter
}

// hostedMembers 返回这次每个外壳承载的成员（按启动判定的顺序）。
func hostedMembers(proj *project.Project, states *cascade.Result) map[resolver.Ref][]resolver.Ref {
	out := map[resolver.Ref][]resolver.Ref{}
	for _, ref := range states.Running() {
		if host, ok := states.HostOf(proj, ref); ok {
			out[host] = append(out[host], ref)
		}
	}
	return out
}

// skippedWaits 返回每个工作负载因 skipWaitFor 而不等的强依赖（启动顺序里照实写出来）。
func skippedWaits(proj *project.Project, graph *resolver.Graph, states *cascade.Result) map[resolver.Ref][]resolver.Ref {
	out := map[resolver.Ref][]resolver.Ref{}
	for _, ref := range states.Running() {
		if skipped := shell.SkippedWaits(proj, graph, states, ref); len(skipped) > 0 {
			out[ref] = skipped
		}
	}
	return out
}
