package cli

// 本文件实现 brickkit build（命令表 17，提案 §9.10、§11.2）：构建需要在本机构建的镜像。
// 构建与部署分离——up 从不构建，只检查镜像在不在（up.go 的镜像检查）。
//
// 要构建的是这些版本：
//
//	没有 deployment.image     镜像只能从源码来，名字由组件 ID 与版本推出（manifest.ImageRef）
//	本地安装源给出的版本       正在开发的代码：它的镜像必须从这份代码构建，不能拿 registry 里的顶替
//
// 源码从哪来：本地仓库正是这个版本时用它；否则从这个版本的 git tag 导出（兼容版本、没克隆
// 过的 git 组件）。镜像 tag 与 metadata.version 一致（§9.10.4），外壳镜像记下编进去的成员
// 版本（附录 A24），up 用它核对。

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// 镜像标签（附录 A24）：up 读 labelShellMembers 核对外壳镜像里编进的成员版本。
const (
	labelComponent    = "io.brickkit.component"
	labelVersion      = "io.brickkit.version"
	labelShellMembers = "io.brickkit.shell.members"
)

func newBuildCommand(opts *Options) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     i18n.T(msgid.CliBuildUse),
		Short:   i18n.T(msgid.CliBuildShort),
		Long:    i18n.T(msgid.CliBuildLong),
		Example: i18n.T(msgid.CliBuildExample),
		GroupID: groupComponent,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			return runBuild(ctx, opts, arg, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, i18n.T(msgid.CliBuildFlagForce))
	return cmd
}

func runBuild(ctx context.Context, opts *Options, arg string, force bool) error {
	var id, version string
	if arg != "" {
		var err error
		if id, version, err = parseComponentRef(arg); err != nil {
			return err
		}
	}
	proj, err := project.Load(opts.WorkDir, opts.loadOptions())
	if err != nil {
		return err
	}
	if id != "" && !declared(proj, id, version) {
		return clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.CliRemoveNotInProject, strings.TrimSuffix(id+"@"+version, "@"))).
			WithDetail(i18n.T(msgid.CliRemoveLabelDeclared), declaredVersions(proj, id)).
			WithHint(i18n.T(msgid.CliRemoveHintCheckID))
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
	images := resolveImages(opts, proj)

	built, skipped := 0, 0
	for _, node := range graph.Nodes {
		ref := node.Ref
		if id != "" && (ref.ID != id || (version != "" && ref.Version != version)) {
			continue
		}
		tag := manifest.ImageRef(node.Manifest)
		if !needsLocalBuild(ctx, client, node) {
			if id != "" {
				opts.Printf("%s\n", i18n.T(msgid.CliBuildNotNeeded, ref.String(), tag))
			}
			continue
		}
		if !force {
			exists, err := images.ImageExists(ctx, tag)
			if err != nil {
				return err
			}
			if exists {
				opts.Printf("%s\n", i18n.T(msgid.CliBuildSkippedExists, ref.String(), tag))
				skipped++
				continue
			}
		}
		opts.Printf("%s\n", i18n.T(msgid.CliBuildBuilding, ref.String(), tag))
		if err := buildOne(ctx, proj, client, images, node, tag); err != nil {
			return clierr.As(err).WithDetail(i18n.T(msgid.LabelComponent), ref.String())
		}
		opts.Printf("%s\n", i18n.T(msgid.CliBuildBuilt, ref.String(), tag))
		built++
	}
	if built == 0 && skipped == 0 && id == "" {
		opts.Printf("%s\n", i18n.T(msgid.CliBuildNothing))
	}
	return nil
}

// needsLocalBuild：没有 image 的版本、本地安装源给出的版本要在本机构建；其余的镜像是拉的。
func needsLocalBuild(ctx context.Context, client *source.Client, node *resolver.Node) bool {
	return node.Manifest.Deployment.Image == "" || client.IsLocal(ctx, node.Ref.ID, node.Ref.Version)
}

// buildOne 找到源码、构建一个版本。
func buildOne(ctx context.Context, proj *project.Project, client *source.Client, images engine.Images, node *resolver.Node, tag string) error {
	root, cleanup, err := sourceRoot(ctx, proj, client, node.Ref)
	if err != nil {
		return err
	}
	defer cleanup()
	b := node.Manifest.Deployment.Build
	contextDir, dockerfile := ".", "Dockerfile"
	if b != nil {
		if b.Context != "" {
			contextDir = b.Context
		}
		if b.Dockerfile != "" {
			dockerfile = b.Dockerfile
		}
	}
	return images.Build(ctx, engine.BuildRequest{
		Tag:        tag,
		Context:    filepath.Join(root, filepath.FromSlash(contextDir)),
		Dockerfile: filepath.Join(root, filepath.FromSlash(dockerfile)),
		Labels:     imageLabels(node),
	})
}

// sourceRoot 是这个版本的源码目录：本地仓库正是这个版本时用它，否则从 tag 导出到临时目录。
func sourceRoot(ctx context.Context, proj *project.Project, client *source.Client, ref resolver.Ref) (string, func(), error) {
	if dir, ok := proj.LocalRepo(ref.ID); ok {
		if v, err := project.LocalRepoVersion(dir); err == nil && v == ref.Version {
			return dir, func() {}, nil
		}
	}
	tmp, err := os.MkdirTemp("", "brickkit-build-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if err := client.ExportSource(ctx, ref.ID, ref.Version, tmp); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmp, cleanup, nil
}

// imageLabels 是写进镜像的标签：组件与版本；外壳还有编进去的成员版本（排好序）。
func imageLabels(node *resolver.Node) map[string]string {
	labels := map[string]string{labelComponent: node.Ref.ID, labelVersion: node.Ref.Version}
	if node.Manifest.IsShell() {
		labels[labelShellMembers] = shellMembersLabel(node.Manifest.Shell.Members)
	}
	return labels
}

func shellMembersLabel(members []string) string {
	sorted := append([]string{}, members...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// resolveImages 是本机镜像的操作：注入优先，否则按生效的部署目标用 podman 或 docker。
func resolveImages(opts *Options, proj *project.Project) engine.Images {
	if opts.Images != nil {
		return opts.Images
	}
	if proj.Deploy.Target == deployfile.TargetPodman {
		return engine.NewPodman()
	}
	return engine.NewDocker()
}

func declared(proj *project.Project, id, version string) bool {
	for _, v := range proj.Decl.Versions(id) {
		if version == "" || v == version {
			return true
		}
	}
	return false
}
