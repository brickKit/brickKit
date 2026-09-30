package cli

// 本文件实现 brickkit release：无 Market 的组件发布（提案 §10.2、§9.6.2）——校验 component.yaml、
// 确认代码都已提交并推送，然后打 tag、推送；推送失败回滚本地 tag。
//
// 与 publish 并存（附录 A14）：publish 发布到市场，release 发布到组件自己的 Git 仓库。
// 实际的 git 操作在 internal/release；这里只管选组件、按顺序跑、把结果说清楚。

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/release"
	"github.com/brickkit/brickkit/internal/source"
)

func newReleaseCommand(opts *Options) *cobra.Command {
	var path string
	var local bool
	cmd := &cobra.Command{
		Use:     "release",
		Short:   i18n.T(msgid.CliReleaseShort),
		GroupID: groupComponent,
		Long:    i18n.T(msgid.CliReleaseLong),
		Example: i18n.T(msgid.CliReleaseExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if local {
				if cmd.Flags().Changed("path") {
					return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliReleaseLocalWithPath)).
						WithExit(clierr.ExitUsage).WithHint(i18n.T(msgid.CliReleaseHintLocalOrPath))
				}
				return runReleaseLocal(opts)
			}
			return runRelease(opts, path)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", i18n.T(msgid.CliReleaseFlagPath))
	cmd.Flags().BoolVar(&local, "local", false, i18n.T(msgid.CliReleaseFlagLocal))
	return cmd
}

func runRelease(opts *Options, path string) error {
	dir := path
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(opts.WorkDir, dir)
	}
	target, err := release.PrepareAs(dir, opts.display(dir))
	if err != nil {
		return err
	}
	state, err := target.Check()
	if err != nil {
		return err
	}
	if state == release.Released {
		return clierr.New(clierr.CodeReleaseBlocked, i18n.T(msgid.ReleaseAlreadyReleased, target.Ref(), target.Tag)).
			WithHint(i18n.T(msgid.ReleaseHintBumpVersion, manifest.FileName))
	}
	if err := target.Publish(); err != nil {
		return err
	}
	opts.Printf("%s\n", i18n.T(msgid.CliReleaseDone, target.Ref(), target.Tag))
	return nil
}

// runReleaseLocal 发布项目本地源里的每一个组件（提案 §9.6.2）。先把所有组件都检查一遍，
// 全部通过才打第一个 tag——检查阶段发现的问题不留任何痕迹；打 tag 阶段遇到推送失败立即停：
// 之前发布的保留，这一个回滚，之后的没动过。已经发布过（tag 就在当前提交上）的跳过。
func runReleaseLocal(opts *Options) error {
	layout := project.NewLayout(opts.WorkDir)
	decl, err := projfile.ParseFile(layout.DeclPath())
	if err != nil {
		return err
	}
	// 只列目录、不取 Manifest：与 lint 一样不经签名策略，不联网
	client, err := source.New(layout, decl, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	files, err := client.LocalManifestFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliReleaseLocalNone))
		return nil
	}

	var pending []*release.Target
	// 本地源可能重叠（两个源指向同一目录，或同一个组件 ID 出现在两个源里）：
	// 与 add --local 一样按源的顺序取第一个，一个组件只发布一次
	seenID, seenDir := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		dir := filepath.Dir(f.Path)
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dir = resolved
		}
		if seenID[f.ID] || seenDir[dir] {
			continue
		}
		seenID[f.ID], seenDir[dir] = true, true
		target, err := release.PrepareAs(dir, opts.display(dir))
		if err != nil {
			return err
		}
		state, err := target.Check()
		if err != nil {
			return err
		}
		if state == release.Released {
			opts.Printf("   ⏭️  %s\n", i18n.T(msgid.CliReleaseLocalSkipped, target.Ref(), target.Tag))
			continue
		}
		pending = append(pending, target)
	}

	for i, target := range pending {
		if err := target.Publish(); err != nil {
			e := clierr.As(err)
			for _, done := range pending[:i] {
				e = e.WithDetail(i18n.T(msgid.CliReleaseLocalKept), done.Ref())
			}
			for _, rest := range pending[i+1:] {
				e = e.WithDetail(i18n.T(msgid.CliReleaseLocalNotAttempted), rest.Ref())
			}
			return e
		}
		opts.Printf("   %s\n", i18n.T(msgid.CliReleaseDone, target.Ref(), target.Tag))
	}
	if len(pending) == 0 {
		opts.Printf("%s\n", i18n.T(msgid.CliReleaseLocalNothing))
		return nil
	}
	opts.Printf("%s\n", i18n.T(msgid.CliReleaseLocalSummary, i18n.Count(msgid.CountComponents, len(pending))))
	return nil
}
