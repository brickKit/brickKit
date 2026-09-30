package cli

// 本文件实现 brickkit add（命令表 6，提案 §5.5、§6.7、§7.6、§8.5，附录 A20–A24）：
// 拉取组件与它的依赖，一次写好三份文件——brickkit.yaml 的声明、部署文件的条目、
// config/ 的骨架。
//
// 判断（谁是默认版本、谁带 requiredBy、条目嵌不嵌进外壳）全在 internal/install；
// 这里只做四件事：取 Manifest、问使用者（$var: 引用）、按计划改文件（改不好就全部
// 还原）、说清楚改了什么。

import (
	"context"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/install"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

type addFlags struct {
	yes, repo, repoAll, local, init bool
}

func newAddCommand(opts *Options) *cobra.Command {
	var f addFlags
	cmd := &cobra.Command{
		Annotations: findsProjectAnnotation(),
		Use:         i18n.T(msgid.CliAddAddComponentIDExactVersion),
		Short:       i18n.T(msgid.CliAddShort),
		Long:        i18n.T(msgid.CliAddLong),
		Example:     i18n.T(msgid.CliAddExample),
		GroupID:     groupComponent,
		Args:        cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if f.local {
				if err := checkLocalFlagCombo(args, f); err != nil {
					return err
				}
				return runAddLocal(ctx, opts, f)
			}
			if f.init {
				return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliAddInitNeedsLocal)).
					WithHint(i18n.T(msgid.CliAddHintInitNeedsLocal)).WithExit(clierr.ExitUsage)
			}
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliAddNeedsComponent)).
					WithHint(i18n.T(msgid.CliAddHintNeedsComponent)).WithExit(clierr.ExitUsage)
			}
			if f.repo || f.repoAll {
				if err := refuseRepoInNestedWorkbench(opts); err != nil {
					return err
				}
			}
			return runAdd(ctx, opts, args[0], f)
		},
	}
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, i18n.T(msgid.CliAddFlagYes))
	cmd.Flags().BoolVar(&f.repo, "repo", false, i18n.T(msgid.CliAddFlagRepo))
	cmd.Flags().BoolVar(&f.repoAll, "repo-all", false, i18n.T(msgid.CliAddFlagRepoAll))
	cmd.Flags().BoolVar(&f.local, "local", false, i18n.T(msgid.CliAddFlagLocal))
	cmd.Flags().BoolVar(&f.init, "init", false, i18n.T(msgid.CliAddFlagInit))
	return cmd
}

func runAdd(ctx context.Context, opts *Options, arg string, f addFlags) error {
	id, version, err := parseComponentRef(arg)
	if err != nil {
		return err
	}
	proj, err := loadForInstall(opts)
	if err != nil {
		return err
	}
	client, err := newSourceClient(opts, proj.Layout, proj.Decl, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if version == "" {
		latest, err := client.LatestVersion(ctx, id)
		if err != nil {
			return err
		}
		opts.Printf("%s\n", i18n.T(msgid.CliAddResolvedLatest, id, latest.Version, latest.SourceID))
		version = latest.Version
	}
	return installAdd(ctx, opts, proj, client, []resolver.Ref{{ID: id, Version: version}}, f)
}

// installAdd 是 add 与 add --local 共用的主流程。
func installAdd(ctx context.Context, opts *Options, proj *project.Project, client *source.Client, targets []resolver.Ref, f addFlags) error {
	roots := make([]resolver.Ref, 0, len(proj.Decl.Components)+len(targets))
	for _, c := range proj.Decl.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	for _, t := range targets {
		fetched, err := client.Manifest(ctx, t.ID, t.Version)
		if err != nil {
			return err
		}
		roots = append(roots, t)
		// 外壳编进的成员跟着外壳一起进来（附录 A24）
		if fetched.Manifest.IsShell() {
			for _, member := range fetched.Manifest.Shell.Members {
				id, version, _ := manifest.SplitRef(member)
				roots = append(roots, resolver.Ref{ID: id, Version: version})
			}
		}
	}
	graph, err := resolver.New(resolver.FromSource(client)).Resolve(ctx, roots...)
	if err != nil {
		return err
	}
	plan, err := install.PlanAdd(proj, graph, targets...)
	if err != nil {
		return err
	}
	if plan.Empty() {
		opts.Printf("%s\n", i18n.T(msgid.CliAddNothingChanged, joinRefs(targets)))
		// 组件早就在了、事后才要源码：三份文件不用改，照样克隆
		clones, err := planClones(ctx, opts, client, proj, plan, targets, f)
		if err != nil {
			return err
		}
		return runClones(ctx, opts, proj.Layout, clones, false)
	}

	// 克隆的资格检查在写文件之前：目录已存在时直接失败，不留下"写了一半"的现场
	clones, err := planClones(ctx, opts, client, proj, plan, targets, f)
	if err != nil {
		return err
	}
	res, err := applyPlanWith(opts, proj, plan, applyOptions{
		varRefs: askVarRefs(opts, proj, plan, f.yes),
		choose:  conflictChooser(opts, f.yes),
	})
	if err != nil {
		return err
	}
	artifacts, warnings := downloadAddedArtifacts(ctx, client, graph, plan.Added)

	renderAddResult(opts, targets, plan, res)
	if artifacts > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliAddArtifacts, i18n.Count(msgid.CountFiles, artifacts)))
	}
	renderVerifiedSignatures(opts, client.SignatureStatuses())
	renderWarnings(opts, graph.Warnings)
	renderWarnings(opts, warnings)

	if err := runClones(ctx, opts, proj.Layout, clones, true); err != nil {
		return err
	}
	refreshProjectDoc(opts, proj.Layout)
	logging.Info(i18n.T(msgid.LogComponentAdded), "components", joinRefs(targets), "added", len(plan.Added))
	return nil
}

// askVarRefs：新生成的配置骨架里，config/vars.yaml（或部署文件 vars:）里有同名变量的键，
// 问使用者要不要写成 $var: 引用（提案 §7.2.5）。--yes 一律引用；没有输入等于不引用。
//
// 要从归档恢复的配置不问：那里是使用者当初写的配置，以它为准——问了、再被恢复的文件盖掉，
// 输出就会说"已引用"而文件里没有。
func askVarRefs(opts *Options, proj *project.Project, plan *install.Plan, yes bool) map[install.ConfigRef]map[string]string {
	refs := map[install.ConfigRef]map[string]string{}
	for _, c := range plan.AddConfigs {
		ref := install.ConfigRef{ID: c.ID, Version: c.Version, Versioned: c.Versioned}
		if _, _, restored := archivedConfigFor(proj.Layout, c.ID, c.Version); restored {
			continue
		}
		keys := make([]string, 0, len(c.Schema.Properties))
		for key := range c.Schema.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			_, inVars := proj.Vars[key]
			_, inDeploy := proj.DeployVars[key]
			if !inVars && !inDeploy {
				continue
			}
			file := "config/" + configFileName(ref)
			if yes {
				opts.Printf("%s\n", i18n.T(msgid.CliAddVarReferenced, key, file))
			} else if !confirm(opts, i18n.T(msgid.CliAddVarPrompt, key, file)) {
				continue
			}
			if refs[ref] == nil {
				refs[ref] = map[string]string{}
			}
			refs[ref][key] = key
		}
	}
	return refs
}

// downloadAddedArtifacts 下载这次加进来的组件的产物。失败只警告：产物是开发时辅助，
// 组件本身已经装好了（fetch 反过来，那里产物就是全部目的）。
func downloadAddedArtifacts(ctx context.Context, client *source.Client, graph *resolver.Graph, added []resolver.Ref) (int, []*clierr.Error) {
	count := 0
	var warnings []*clierr.Error
	for _, ref := range added {
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		res, err := client.DownloadArtifacts(ctx, node.Manifest)
		if err != nil {
			warnings = append(warnings, clierr.As(err))
			continue
		}
		count += len(res.Downloaded)
		warnings = append(warnings, res.Warnings...)
	}
	return count, warnings
}

// renderAddResult 说清楚这次改了什么：加了哪些版本（默认 / requiredBy / 在哪个外壳里）、
// 挪了什么、写了哪些文件、有哪些要使用者自己留意的。
func renderAddResult(opts *Options, targets []resolver.Ref, plan *install.Plan, res *applied) {
	opts.Printf("%s\n", i18n.T(msgid.CliAddHeader, joinRefs(targets)))
	under := map[string]string{}
	for _, e := range plan.AddEntries {
		under[e.ID] = e.Under
	}
	for _, l := range plan.AddLines {
		shell := under[install.EntryID(l.ID, l.Version, len(l.RequiredBy) == 0)]
		switch {
		case shell != "" && len(l.RequiredBy) > 0:
			opts.Printf("   %s\n", i18n.T(msgid.CliAddLineRequiredByInShell, l.Ref().String(), strings.Join(l.RequiredBy, ", "), shell))
		case shell != "":
			opts.Printf("   %s\n", i18n.T(msgid.CliAddLineInShell, l.Ref().String(), shell))
		case len(l.RequiredBy) > 0:
			opts.Printf("   %s\n", i18n.T(msgid.CliAddLineRequiredBy, l.Ref().String(), strings.Join(l.RequiredBy, ", ")))
		default:
			opts.Printf("   %s\n", i18n.T(msgid.CliAddLine, l.Ref().String()))
		}
	}
	for _, e := range plan.NestEntries {
		opts.Printf("   %s\n", i18n.T(msgid.CliAddNested, e.ID, e.Under))
	}
	for _, l := range plan.SetRequiredBy {
		opts.Printf("   %s\n", i18n.T(msgid.CliAddRequiredByExtended, l.Ref().String(), strings.Join(l.RequiredBy, ", ")))
	}
	opts.Printf("%s\n", i18n.T(msgid.CliInstallWritten, strings.Join(append([]string{project.FileDecl}, res.DeployFiles...), ", ")))
	if len(res.ConfigsWritten) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliAddConfigsWritten, strings.Join(res.ConfigsWritten, ", ")))
	}
	for _, f := range res.ConfigsToFill {
		opts.Printf("   %s\n", i18n.T(msgid.CliAddConfigsToFill, f.File, strings.Join(f.Keys, ", ")))
	}
	for _, r := range res.Restored {
		opts.Printf("%s\n", i18n.T(msgid.CliAddConfigRestored, r.File, r.Archive))
		renderMigrationReport(opts, r.File, r.Report)
	}
	for _, note := range plan.Notes {
		opts.Printf("%s\n", i18n.T(msgid.CliInstallNote, note))
	}
	if len(res.OtherDeployFiles) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliInstallOtherDeployFiles, strings.Join(res.OtherDeployFiles, ", ")))
	}
}

// renderVerifiedSignatures 只报真的验过的签名：没验过的是绝大多数项目的常态，
// 不该占满屏幕；需要注意的落差（强制却没公钥、发布者不在信任列表）走警告。
func renderVerifiedSignatures(opts *Options, statuses []source.SignatureStatus) {
	seen := map[string]bool{}
	for _, st := range statuses {
		for _, w := range st.Warnings {
			if key := w.Message; !seen[key] {
				seen[key] = true
				opts.Printf("%s", w.Format())
			}
		}
		if st.Verified {
			opts.Printf("%s\n", i18n.T(msgid.CliAddSignatureVerified, st.Ref()))
		}
	}
}

func joinRefs(refs []resolver.Ref) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.String()
	}
	return strings.Join(parts, ", ")
}

// clonePlan 是一次 --repo 克隆：检出的 tag 就是这个版本（附录 A22：本地仓库 = 默认版本）。
type clonePlan struct {
	ref resolver.Ref
	url string
	tag string
	// from 是本机仓库缓存里的 bare 副本（有的话）：从它克隆，离线也行
	from string
}

// planClones 决定 --repo / --repo-all 要克隆哪些，并在写文件之前做完资格检查。
// 只克隆默认版本：本地仓库只能是默认版本（附录 A22），兼容版本的代码用不上它。
func planClones(ctx context.Context, opts *Options, client *source.Client, proj *project.Project, plan *install.Plan, targets []resolver.Ref, f addFlags) ([]clonePlan, error) {
	if !f.repo && !f.repoAll {
		return nil, nil
	}
	defaults := map[resolver.Ref]bool{}
	for _, c := range proj.Decl.Components {
		defaults[resolver.Ref{ID: c.ID, Version: c.Version}] = proj.Decl.IsDefault(c.ID, c.Version)
	}
	for _, l := range plan.AddLines {
		defaults[l.Ref()] = len(l.RequiredBy) == 0
	}
	var refs []resolver.Ref
	if f.repoAll {
		for _, ref := range plan.Added {
			if defaults[ref] {
				refs = append(refs, ref)
			}
		}
	} else {
		refs = targets
	}
	var clones []clonePlan
	for _, ref := range refs {
		origin, err := client.Origin(ctx, ref.ID, ref.Version)
		if err != nil {
			return nil, err
		}
		if !origin.IsOpenSource() {
			opts.Printf("%s\n", i18n.T(msgid.CliAddCloneSkippedNoRepo, ref.String()))
			continue
		}
		if err := workspace.ExistingSourceError(proj.Layout, ref.ID, ref.String()); err != nil {
			return nil, err
		}
		from := origin.GitURL
		if origin.CacheDir != "" {
			from = origin.CacheDir
		}
		clones = append(clones, clonePlan{ref: ref, url: origin.GitURL, tag: origin.Tag, from: from})
	}
	return clones, nil
}

// filesWritten 为 true 表示三份文件这次已经改好：克隆失败时要说出来，免得使用者以为什么都没发生。
func runClones(ctx context.Context, opts *Options, layout project.Layout, clones []clonePlan, filesWritten bool) error {
	for _, c := range clones {
		if _, err := workspace.CloneFrom(ctx, layout, c.ref.ID, c.ref.String(), c.from, c.url, c.tag); err != nil {
			if filesWritten {
				return clierr.As(err).WithHint(i18n.T(msgid.CliAddCloneAfterWrite, c.ref.String()))
			}
			return err
		}
		opts.Printf("%s\n", i18n.T(msgid.CliAddCloned, c.ref.String(), workspace.DisplayDir(c.ref.ID), c.tag))
	}
	return nil
}
