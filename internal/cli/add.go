package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// addFlags 是 brickkit add 的参数（004 §3.3）。
type addFlags struct {
	yes     bool
	repo    bool
	repoAll bool
	// local 对应 --local：批量添加本地安装源里的所有组件。
	local bool
}

// newAddCommand 实现 brickkit add（004 §3.3）。
func newAddCommand(opts *Options) *cobra.Command {
	var f addFlags

	cmd := &cobra.Command{
		Use:     i18n.T(msgid.CliAddAddComponentIdExactVersion),
		Short:   i18n.T(msgid.CliAddShort),
		GroupID: groupComponent,
		Long:    i18n.T(msgid.CliAddLong),
		Example: i18n.T(msgid.CliAddExample),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.local {
				if err := checkLocalFlagCombo(args, f); err != nil {
					return err
				}
				return runAddLocal(cmd.Context(), opts, f)
			}
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliAddPleaseSpecifyTheComponentTo)).
					WithDetail(i18n.T(msgid.LabelUsage), i18n.T(msgid.CliAddBrickkitAddComponentIdExact)).
					WithDetail(i18n.T(msgid.LabelExample), "brickkit add people/basic@1.0.0").
					WithDetail(i18n.T(msgid.CliAddWithoutAVersion), i18n.T(msgid.CliAddBrickkitAddPeopleBasicInstalls)).
					WithDetail(i18n.T(msgid.CliAddAddLocalComponentsInBulk), "brickkit add --local").
					WithExit(clierr.ExitUsage)
			}
			return runAdd(cmd.Context(), opts, args[0], f)
		},
	}

	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, i18n.T(msgid.CliAddNonInteractiveModeSkipEvery))
	cmd.Flags().BoolVar(&f.repo, "repo", false, i18n.T(msgid.CliAddAlsoCloneTheComponentS))
	cmd.Flags().BoolVar(&f.repoAll, "repo-all", false, i18n.T(msgid.CliAddCloneTheGitRepositoriesOf))
	cmd.Flags().BoolVar(&f.local, "local", false, i18n.T(msgid.CliAddAddEveryComponentInThe2))
	return cmd
}

// resolveLatest 解析"不写版本时装哪个版本"（004 §3.3）。
//
// 解析出的是一个**精确版本**，随后照常钉进 brickkit.yaml——配置里永远不会出现
// 范围约束（012 §2.2）。cancelled 为 true 表示使用者在共存确认处回答了 n。
func resolveLatest(
	ctx context.Context, opts *Options, client *source.Client,
	cfg *config.Config, id string, f addFlags,
) (version string, cancelled bool, err error) {
	latest, err := client.LatestVersion(ctx, id)
	if err != nil {
		return "", false, err
	}
	opts.Printf("%s\n", i18n.T(msgid.CliFetchNoVersionGivenResolvedTo, id, latest.Version, latest.SourceID, latest.SourceKind))

	// 同 ID 已经装了别的版本：这一步会**多起一个容器**。显式写版本号的人知道
	// 自己在搞多版本共存，不写版本号的人多半没这个预期，所以先问一句。
	others := otherVersions(cfg, id, latest.Version)
	if len(others) == 0 {
		return latest.Version, false, nil
	}
	if f.yes {
		opts.Printf("%s\n", i18n.T(msgid.CliAddAlreadyHasAndYesWas, id, strings.Join(others, i18n.T(msgid.ListSeparator)), latest.Version))
		return latest.Version, false, nil
	}
	opts.Printf("%s\n", i18n.T(msgid.CliAddAlreadyHasWillBeAdded, id, strings.Join(others, i18n.T(msgid.ListSeparator)), latest.Version))
	if !confirm(opts, i18n.T(msgid.CliAddContinueYN)) {
		opts.Printf("%s\n", i18n.T(msgid.CliAddCancelledBrickkitYamlWasNot))
		opts.Printf("%s\n", i18n.T(msgid.CliAddToOnlyUpgradeRunBrickkit, id, others[0]))
		return "", true, nil
	}
	return latest.Version, false, nil
}

// otherVersions 返回配置里同 ID、但不是 version 的那些版本。
func otherVersions(cfg *config.Config, id, version string) []string {
	var out []string
	for _, c := range cfg.Components {
		if c.ID == id && c.Version != version {
			out = append(out, c.Version)
		}
	}
	return out
}

// checkLocalFlagCombo 挡住 --local 的两条互斥规则。
//
// 都是"说的不是一件事"，不是"暂不支持"：
//   - --local 是批量添加本地源里的全部组件，接了组件 ID 就自相矛盾；
//   - --repo / --repo-all 是"从市场登记的 Git 地址再 clone 一份源码下来"，
//     而本地源里的组件源码本来就在盘上（默认约定里 local 源就指向 ./components），
//     真 clone 起来目标目录已存在，只会报错。
func checkLocalFlagCombo(args []string, f addFlags) error {
	if len(args) > 0 {
		return clierr.New(clierr.CodeInvalidArgument,
			i18n.T(msgid.CliAddErrorLocalTakesNoComponent, args[0])).
			WithDetail("--local", i18n.T(msgid.CliAddAddEveryComponentInThe)).
			WithHint(
				i18n.T(msgid.CliAddAddLocalComponentsInBulk2),
				i18n.T(msgid.CliAddAddASingleComponentBrickkit, args[0]),
			).WithExit(clierr.ExitUsage)
	}
	for _, bad := range []struct {
		on   bool
		flag string
	}{{f.repo, "--repo"}, {f.repoAll, "--repo-all"}} {
		if !bad.on {
			continue
		}
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliAddErrorLocalCannotBeCombined, bad.flag)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliAddTheSourceOfComponentsIn)).
			WithHint(
				i18n.T(msgid.CliAddAddLocalComponentsInBulk2),
				i18n.T(msgid.CliAddToCloneOneComponentS, bad.flag),
			).WithExit(clierr.ExitUsage)
	}
	return nil
}

func runAdd(ctx context.Context, opts *Options, arg string, f addFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}

	id, version, err := parseComponentRef(arg)
	if err != nil {
		return err
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}

	// 客户端建在备份之前：不写版本时要先靠它解析出最新版本，
	// 而"要不要刷新缓存"又取决于解析结果是否已在配置里。
	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if version == "" {
		resolved, cancelled, err := resolveLatest(ctx, opts, client, cfg, id, f)
		if err != nil || cancelled {
			return err
		}
		version = resolved
	}
	target := resolver.Ref{ID: id, Version: version}

	// 同 ID 同版本已存在：确认是否刷新缓存（--yes 直接刷新，004 §3.3）
	existing := hasComponent(cfg, id, version)
	if existing {
		if f.yes {
			opts.Printf("%s\n", i18n.T(msgid.CliAddAlreadyExistsInBrickkitYaml2, target))
		} else {
			opts.Printf("%s\n", i18n.T(msgid.CliAddAlreadyExistsInBrickkitYaml, target))
			if !confirm(opts, i18n.T(msgid.CliAddRefreshTheManifestAndArtifacts)) {
				opts.Printf("%s\n", i18n.T(msgid.CliAddCancelledBrickkitYamlWasNot))
				return nil
			}
		}
		client.SetRefresh(true)
	}

	graph, err := resolver.New(resolver.FromSource(client)).Resolve(ctx, target)
	if err != nil {
		return err
	}

	// --repo / --repo-all 的资格检查放在写配置之前：
	// 闭源组件或目录已存在时直接失败，不留下"配置写了一半"的现场。
	clones, err := planClones(ctx, opts, client, layout, graph, target, f)
	if err != nil {
		return err
	}

	artifacts := downloadArtifacts(ctx, client, graph)
	added, err := writeComponents(layout, graph)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		if err := syncOverrideAfterAdd(opts, layout); err != nil {
			return err
		}
	}

	renderAddTree(opts, graph, target, artifacts)
	renderSignatures(opts, client.SignatureStatuses())
	renderWarnings(opts, graph.Warnings)
	renderWarnings(opts, artifacts.warnings)

	switch {
	case len(added) > 0:
		opts.Printf("%s\n", i18n.T(msgid.CliAddLocalWrittenToBrickkitYamlComponents, i18n.Count(msgid.CountComponents, len(added))))
	case existing:
		opts.Printf("%s\n", i18n.T(msgid.CliAddRefreshedTheManifestAndArtifacts, target))
	default:
		opts.Printf("%s\n", i18n.T(msgid.CliAddLocalBrickkitYamlIsUnchangedThe))
	}
	if artifacts.downloaded > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliAddDownloadedArtifactsIntoBrickkitArtifacts, i18n.Count(msgid.CountFiles, artifacts.downloaded)))
	} else if artifacts.cached > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliAddArtifactsAreUpToDate, i18n.Count(msgid.CountFiles, artifacts.cached)))
	}
	renderCoexistence(opts, layout, id)

	if err := runClones(ctx, opts, layout, clones, f); err != nil {
		return err
	}

	logging.Info(i18n.T(msgid.LogComponentAdded),
		"component", target.String(),
		"written", len(added),
		"artifacts", artifacts.downloaded,
	)
	return nil
}

// ============================================================
// 产物下载
// ============================================================

type artifactSummary struct {
	downloaded int
	cached     int
	// perNode 是每个组件下载/命中的文件数，用于渲染树状输出。
	perNode  map[resolver.Ref]int
	warnings []*clierr.Error
}

// ============================================================
// 写入 brickkit.yaml
// ============================================================

// writeComponents 把依赖图中尚未配置的组件写入 brickkit.yaml，返回**这次新增**的那些。
func writeComponents(layout config.Layout, graph *resolver.Graph) ([]resolver.Ref, error) {
	edit, err := config.OpenEdit(layout.ConfigPath())
	if err != nil {
		return nil, err
	}

	var added []resolver.Ref
	for _, node := range graph.Nodes {
		if edit.AddComponent(node.Ref.ID, node.Ref.Version) {
			added = append(added, node.Ref)
		}
	}
	if len(added) == 0 {
		return nil, nil
	}
	if err := edit.Save(); err != nil {
		return nil, err
	}
	return added, nil
}

// ============================================================
// 输出渲染（004 §3.3 输出样例）
// ============================================================

func renderAddTree(opts *Options, graph *resolver.Graph, target resolver.Ref, artifacts *artifactSummary) {
	opts.Printf("%s\n", i18n.T(msgid.CliAddAdding, target))

	optionalOnly := dependencyKinds(graph)
	lines := make([]string, 0, len(graph.Nodes)+1)
	lines = append(lines, "Manifest ✅")
	for _, node := range graph.Nodes {
		if node.Ref == target {
			continue
		}
		label := i18n.T(msgid.CliAddDependency)
		if optionalOnly[node.Ref] {
			label = i18n.T(msgid.CliAddOptionalDependency)
		}
		line := i18n.T(msgid.CliAddPulled, label, node.Ref.String())
		if n := artifacts.perNode[node.Ref]; n > 0 {
			line += i18n.T(msgid.CliAddArtifactsFiles2, i18n.Count(msgid.CountFiles, n))
		}
		lines = append(lines, line)
	}
	if n := artifacts.perNode[target]; n > 0 {
		lines = append(lines, i18n.T(msgid.CliAddArtifactsFiles, i18n.Count(msgid.CountFiles, n)))
	}

	for i, line := range lines {
		branch := "├──"
		if i == len(lines)-1 {
			branch = "└──"
		}
		opts.Printf("   %s %s\n", branch, line)
	}
}

// renderSignatures 输出签名校验结果（008 §8 的「签名：✅ 已校验」）。
//
// 只报**验过的**：没验过的组件不列一行"未校验"，那会把正常状态渲染成一屏噪音
// ——绝大多数项目还没用上签名。真正需要使用者知道的落差（要求强制却没配公钥、
// 发布者不在信任列表）走警告，那才是该占屏幕的东西。
func renderSignatures(opts *Options, statuses []source.SignatureStatus) {
	for _, st := range statuses {
		if !st.Verified {
			continue
		}
		line := i18n.T(msgid.CliAddSignatureVerified, st.Ref())
		if st.Signature != nil && st.Signature.SignedBy != "" {
			line += i18n.T(msgid.CliAddPublisher, st.Signature.SignedBy)
		}
		opts.Printf("%s\n", line)
	}
	renderWarnings(opts, dedupeWarnings(statuses))
}

// dedupeWarnings 把逐组件产生、内容却完全一样的签名警告合成一条。
//
// # 为什么必须合
//
// `installer.requireSignature` 默认为 true，而绝大多数项目还没配过 publicKeys
// （008 §8.5.1：没有信任锚点就没有"强制"可言，此时放行 + 警告）。这条警告
// **讲的是项目的配置，与具体是哪个组件无关**——可它是在取每个组件的 Manifest
// 时产生的，于是 `add erp/backend` 拉下六个依赖就刷六份一模一样的多行块。
//
// warnTargetOnlyFields 早就把这条理由写下来了："同一件事说 N 遍会把警告区刷满，
// 而使用者一旦开始整块跳过警告，真正要紧的那几条也一起被跳过。"
//
// 判据是**标题 + 建议**是否相同，而不是"是不是那一条"：这样任何将来新增的、
// 同样与组件无关的签名提醒都自动受益，不用回来改这里。明细里的组件引用被丢掉
// ——它恰恰是唯一不同的那部分，而合并之后它也不再是重点。
func dedupeWarnings(statuses []source.SignatureStatus) []*clierr.Error {
	var out []*clierr.Error
	seen := map[string]bool{}
	for _, st := range statuses {
		for _, w := range st.Warnings {
			key := w.Message + "\x00" + strings.Join(w.Hints, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, w)
		}
	}
	return out
}

func renderWarnings(opts *Options, warnings []*clierr.Error) {
	for _, w := range warnings {
		opts.Printf("%s", w.Format())
	}
}

// renderCoexistence 在同 ID 出现多个版本时提示多版本共存（002 §3.6）。
func renderCoexistence(opts *Options, layout config.Layout, id string) {
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return
	}
	versions := make([]string, 0, 2)
	for _, c := range cfg.Components {
		if c.ID == id {
			versions = append(versions, c.Version)
		}
	}
	if len(versions) < 2 {
		return
	}
	opts.Printf("%s\n", i18n.T(msgid.CliAddHasSeveralVersionsCoexistingEach, id, strings.Join(versions, i18n.T(msgid.ListSeparator))))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ============================================================
// --repo / --repo-all（004 §3.3）
// ============================================================

// clonePlan 是一个待 clone（或跳过）的组件。
type clonePlan struct {
	ref     resolver.Ref
	gitURL  string
	skip    bool
	skipMsg string
}

// planClones 解析 --repo / --repo-all 涉及组件的来源信息，并做资格检查。
// existingSourceSkipMessage 是 --repo-all 跳过某个组件时那一行说明。
//
// 归档的要单独说：`--repo-all` 打印一行"已有源码目录，跳过"，而使用者去
// components/ 下**看不到**它——那句话在他眼里就是假的。
func existingSourceSkipMessage(layout config.Layout, componentID string) string {
	if workspace.Locate(layout, componentID) == workspace.StateArchived {
		return i18n.T(msgid.CliAddSourceIsAtArchivedSkipping, workspace.DisplayArchivedDir(componentID))
	}
	return i18n.T(msgid.CliAddTheSourceDirectoryAlreadyExists)
}

func planClones(
	ctx context.Context,
	opts *Options,
	client *source.Client,
	layout config.Layout,
	graph *resolver.Graph,
	target resolver.Ref,
	f addFlags,
) ([]clonePlan, error) {
	if !f.repo && !f.repoAll {
		return nil, nil
	}

	refs := []resolver.Ref{target}
	if f.repoAll {
		refs = graph.Refs()
	}

	plans := make([]clonePlan, 0, len(refs))
	for _, ref := range refs {
		origin, err := client.Origin(ctx, ref.ID, ref.Version)
		if err != nil {
			return nil, err
		}

		switch {
		case origin.Type == source.OriginRegistry:
			if f.repo {
				// 004 §3.3 输出样例（闭源组件）
				return nil, clierr.New(clierr.CodeCloneFailed, i18n.T(msgid.CliAddCloneFailedThisComponentIs)).
					WithDetail(i18n.T(msgid.LabelComponent), ref.String()).
					WithDetail(i18n.T(msgid.CliAddSourceType), i18n.T(msgid.CliAddRegistryClosedSource)).
					WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliAddAClosedSourceComponentHas)).
					WithTip(i18n.T(msgid.CliAddYouCanStillInstallAnd),
						"   brickkit add "+ref.String())
			}
			plans = append(plans, clonePlan{ref: ref, skip: true, skipMsg: i18n.T(msgid.CliAddClosedSourceComponentSkippingClone)})
		case workspace.Locate(layout, ref.ID) != workspace.StateMissing:
			// **先问"源码是不是已经在盘上"，再问"来源有没有 Git 地址"。**
			//
			// 顺序不是随意的：`init` 生成的项目把 local-dev（./components）排在所有
			// 安装源前面，而 --repo 克隆完，源码正躺在 ./components/ 里——从此 local-dev
			// 先于任何 git 源认领这个组件。若先问后一个问题，"克隆过一次再 --repo"
			// 会得到"没有可用的 Git 仓库地址"（用户明明克隆过），"被 sync 归档后
			// --repo"还会被劝去"直接用 components/x/ 里的源码"——那个目录此刻并不存在。
			// 专门讲这两种情况的提示早就写好了，只是在默认布局下永远走不到。
			//
			// 活跃目录与归档目录都算"已经有了"：往活跃目录再 clone 一份，
			// 会打破"一个组件 ID 只有一个源码目录"（004 §8.1），
			// 而下一次 sync 就卡死在"目标目录已存在"上
			if f.repo {
				return nil, workspace.ExistingSourceError(layout, ref.ID, ref.String())
			}
			// --repo-all 是批量操作：已有源码跳过即可，不该因为一个组件就整批失败
			plans = append(plans, clonePlan{
				ref: ref, skip: true, skipMsg: existingSourceSkipMessage(layout, ref.ID)})
		case !origin.IsOpenSource():
			if f.repo {
				return nil, clierr.New(clierr.CodeCloneFailed, i18n.T(msgid.CliAddCloneFailedNoUsableGit)).
					WithDetail(i18n.T(msgid.LabelComponent), ref.String()).
					WithDetail(i18n.T(msgid.LabelSource), origin.SourceID).
					WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliAddTheComponentComesFromA)).
					WithHint(
						// 不能笼统说"改用 git 类型的安装源"：使用者多半**已经配了**一个，
						// 只是它排在 local 后面（003 §6.5）。一条照着做不通的建议比不给建议
						// 更浪费时间。
						// 走到这里说明源码**不在** components/ 里（在的话上面已经拦下了）：
						// 是某个 path 指到别处的本地安装源在提供它，所以第二条建议指向那个源，
						// 而不是一个不存在的 components/x/
						i18n.T(msgid.CliAddThatComponentIsCurrentlyProvided, origin.SourceID),
						i18n.T(msgid.CliAddOrDropRepoAndUse, origin.SourceID),
					)
			}
			plans = append(plans, clonePlan{ref: ref, skip: true, skipMsg: i18n.T(msgid.CliAddNoGitRepositoryAddressSkipping)})
		default:
			plans = append(plans, clonePlan{ref: ref, gitURL: origin.GitURL})
		}
	}
	_ = opts
	return plans, nil
}

// runClones 执行 clone 并渲染结果。
func runClones(ctx context.Context, opts *Options, layout config.Layout, plans []clonePlan, f addFlags) error {
	if len(plans) == 0 {
		return nil
	}

	cloned, skipped := 0, 0
	for _, p := range plans {
		if p.skip {
			skipped++
			if f.repoAll {
				opts.Printf("   ⏭️ %-22s → %s\n", p.ref.ID, p.skipMsg)
			}
			continue
		}
		if _, err := workspace.Clone(ctx, layout, p.ref.ID, p.ref.String(), p.gitURL); err != nil {
			return err
		}
		cloned++
		if f.repoAll {
			opts.Printf("%s\n", i18n.T(msgid.CliAddSCloneFinished, p.ref.ID, workspace.DisplayDir(p.ref.ID)))
		}
	}

	switch {
	case f.repoAll:
		// 不能笼统说"跳过 N 个闭源组件"：跳过的理由有三种（闭源、本地源没有
		// 仓库地址、源码已经在盘上），而上面每一行 ⏭️ 已经逐个说清了是哪一种。
		// 汇总行再断言一个具体理由，只会与它上面那几行自相矛盾
		opts.Printf("%s\n", i18n.TN(msgid.CliAddClonedOpenSourceComponentRepositories, cloned, cloned, skipped))
	case cloned > 0:
		opts.Printf("%s\n", i18n.T(msgid.CliAddClonedTheSourceInto, workspace.DisplayDir(plans[0].ref.ID)))
		opts.Printf("%s\n", i18n.T(msgid.CliAddForHowToPushSource))
	}

	logging.Info(i18n.T(msgid.LogSourceCloneDone), "cloned", cloned, "skipped", skipped)
	return nil
}
