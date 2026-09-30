package cli

// 本文件实现 brickkit remove（命令表 7，提案 §6.7、§7.7、§8.7，附录 A20）：检查强依赖方后
// 移除组件——配置归档而不是删除、部署条目同步删掉、外壳的成员挪回顶层独立运行、只因它而
// 保留的兼容版本一并移除。判断在 internal/install，落盘与还原在 install_apply.go。
//
// 源码目录（components/ 下 --repo 克隆的、或自己写的）只在这个组件的最后一个版本走了时才删，
// 而且先确认删了还找得回来：未提交、未推送的改动只在这一份工作区里（--force 才删）。
// 这一步在改文件之前：拦下时不该留下"配置改了、源码还在"的现场。

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/gitrepo"
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

func newRemoveCommand(opts *Options) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Annotations: findsProjectAnnotation(),
		Use:         i18n.T(msgid.CliRemoveRemoveComponentIDVersion),
		Short:       i18n.T(msgid.CliRemoveShort),
		Long:        i18n.T(msgid.CliRemoveLong),
		Example:     i18n.T(msgid.CliRemoveExample),
		GroupID:     groupComponent,
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runRemove(ctx, opts, args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, i18n.T(msgid.CliRemoveFlagForce))
	return cmd
}

func runRemove(ctx context.Context, opts *Options, arg string, force bool) error {
	id, version, err := parseComponentRef(arg)
	if err != nil {
		return err
	}
	proj, err := loadForInstall(opts)
	if err != nil {
		return err
	}
	target, err := resolveRemoveTarget(proj, id, version)
	if err != nil {
		return err
	}
	client, err := newSourceClient(opts, proj.Layout, proj.Decl, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	graph, err := resolver.New(resolver.FromSource(client)).ResolveProject(ctx, proj)
	if err != nil {
		return err
	}
	plan, err := install.PlanRemove(proj, graph, target)
	if err != nil {
		return err
	}

	// 不在 git 仓库里时 repo 为 nil：submodule 的检查自己会跳过
	repo, _ := gitrepo.Open(proj.Layout.Root)
	gone := idsLeavingEntirely(proj, plan.Removed)
	for _, id := range gone {
		if err := checkSourceDeletable(proj.Layout, id, repo, force); err != nil {
			return err
		}
	}
	released := releasedMembers(proj, plan.UnnestShells)

	res, err := applyPlan(opts, proj, plan, nil)
	if err != nil {
		return err
	}
	var removedDirs []string
	for _, id := range gone {
		dirs, err := removeSources(proj.Layout, id, repo)
		if err != nil {
			return err
		}
		removedDirs = append(removedDirs, dirs...)
	}
	for _, ref := range plan.Removed {
		_ = os.RemoveAll(client.ArtifactDir(ref.ID, ref.Version))
	}

	renderRemoveResult(opts, target, plan, res, released, removedDirs)
	refreshProjectDoc(opts, proj.Layout)
	logging.Info(i18n.T(msgid.LogComponentRemoved), "component", target.String(), "removed", len(plan.Removed))
	return nil
}

// resolveRemoveTarget 确定要移除的精确版本：不写版本时只在这个组件只有一个版本时才行。
func resolveRemoveTarget(proj *project.Project, id, version string) (resolver.Ref, error) {
	versions := proj.Decl.Versions(id)
	switch {
	case len(versions) == 0 || (version != "" && !slices.Contains(versions, version)):
		want := id
		if version != "" {
			want = id + "@" + version
		}
		return resolver.Ref{}, withDidYouMean(clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.CliRemoveNotInProject, want)).
			WithDetail(i18n.T(msgid.CliRemoveLabelDeclared), declaredVersions(proj, id)).
			WithHint(i18n.T(msgid.CliRemoveHintCheckID)), id, proj.Decl.IDs())
	case version != "":
		return resolver.Ref{ID: id, Version: version}, nil
	case len(versions) > 1:
		return resolver.Ref{}, clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliRemoveNeedsVersion, id, strings.Join(versions, ", "))).
			WithHint(i18n.T(msgid.CliRemoveHintNameVersion, id, versions[0])).WithExit(clierr.ExitUsage)
	default:
		return resolver.Ref{ID: id, Version: versions[0]}, nil
	}
}

func declaredVersions(proj *project.Project, id string) string {
	if versions := proj.Decl.Versions(id); len(versions) > 0 {
		return id + "@" + strings.Join(versions, ", ")
	}
	return i18n.T(msgid.CliRemoveNoneDeclared)
}

// idsLeavingEntirely 是这次一个版本都不剩的组件 ID——只有它们的源码目录可以删。
func idsLeavingEntirely(proj *project.Project, removed []resolver.Ref) []string {
	gone := map[resolver.Ref]bool{}
	for _, r := range removed {
		gone[r] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range removed {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		left := false
		for _, v := range proj.Decl.Versions(r.ID) {
			if !gone[resolver.Ref{ID: r.ID, Version: v}] {
				left = true
			}
		}
		if !left {
			out = append(out, r.ID)
		}
	}
	return out
}

// releasedMembers 是删外壳时挪回顶层的成员（给输出用）。
func releasedMembers(proj *project.Project, shells []string) []string {
	var out []string
	for _, shell := range shells {
		for _, m := range proj.MembersOf(shell) {
			id, _, _ := manifest.SplitRef(m.ID)
			out = append(out, id)
		}
	}
	return out
}

// checkSourceDeletable 在删源码之前确认删了还找得回来（workspace.DeletionRisk）。
// 已登记的 git submodule 不受 --force 影响：直接删只会把 .gitmodules 与索引的账目搞乱。
func checkSourceDeletable(l project.Layout, id string, repo *gitrepo.Repo, force bool) error {
	candidates := []struct{ dir, display string }{
		{workspace.SourceDir(l, id), workspace.DisplayDir(id)},
		{workspace.ArchivedDir(l, id), workspace.DisplayArchivedDir(id)},
	}
	for _, c := range candidates {
		if err := workspace.SubmoduleRemoveGuard(repo, c.dir, id, c.display); err != nil {
			return err
		}
	}
	if force {
		return nil
	}
	for _, c := range candidates {
		if risk := workspace.DeletionRisk(c.dir); risk != "" {
			return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliRemoveSourceNotRecoverable)).
				WithDetail(i18n.T(msgid.LabelComponent), id).
				WithDetail(i18n.T(msgid.LabelDir), c.display).
				WithDetail(i18n.T(msgid.LabelReason), risk).
				WithHint(
					i18n.T(msgid.CliRemoveHintSaveFirst),
					i18n.T(msgid.CliRemoveHintForce, id),
				)
		}
	}
	return nil
}

// removeSources 删掉组件的源码目录（活跃的与 sync 归档的），返回删了哪些（给输出用）。
func removeSources(l project.Layout, id string, repo *gitrepo.Repo) ([]string, error) {
	var out []string
	removed, err := workspace.RemoveSource(l, id, repo)
	if err != nil {
		return nil, err
	}
	if removed {
		out = append(out, workspace.DisplayDir(id))
	}
	removed, err = workspace.RemoveArchived(l, id, repo)
	if err != nil {
		return nil, err
	}
	if removed {
		out = append(out, workspace.DisplayArchivedDir(id))
	}
	return out, nil
}

func renderRemoveResult(opts *Options, target resolver.Ref, plan *install.Plan, res *applied, released, dirs []string) {
	opts.Printf("%s\n", i18n.T(msgid.CliRemoveHeader, target.String()))
	for _, ref := range plan.Removed {
		if ref == target {
			continue
		}
		opts.Printf("   %s\n", i18n.T(msgid.CliRemoveCascaded, ref.String()))
	}
	if len(released) > 0 {
		opts.Printf("   %s\n", i18n.T(msgid.CliRemoveMembersReleased, strings.Join(released, ", ")))
	}
	if plan.Promoted != (resolver.Ref{}) {
		opts.Printf("   %s\n", i18n.T(msgid.CliRemovePromoted, plan.Promoted.String()))
	}
	for _, l := range plan.SetRequiredBy {
		if l.Ref() != plan.Promoted {
			opts.Printf("   %s\n", i18n.T(msgid.CliRemoveRequiredByNow, l.Ref().String(), strings.Join(l.RequiredBy, ", ")))
		}
	}
	for _, note := range plan.Notes {
		opts.Printf("%s\n", i18n.T(msgid.CliInstallNote, note))
	}
	for _, a := range res.ConfigsArchived {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveConfigArchived, a[0], a[1]))
	}
	opts.Printf("%s\n", i18n.T(msgid.CliInstallWritten, strings.Join(append([]string{project.FileDecl}, res.DeployFiles...), ", ")))
	for _, d := range dirs {
		opts.Printf("%s\n", i18n.T(msgid.CliRemoveSourceDeleted, d))
	}
	if len(res.OtherDeployFiles) > 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliInstallOtherDeployFiles, strings.Join(res.OtherDeployFiles, ", ")))
	}
}
