package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// newNewCommand 实现 brickkit new：生成一个新组件的最小骨架。
//
// 只生成一份能通过 Parse + Validate 的 component.yaml（外加可选的契约占位
// 文件），不生成 Dockerfile、不生成任何源码——平台语言无关，不替组件作者
// 选语言；起步代码由 docs/{en,zh}/04-go-component-template.md 这类"带读一个
// 真实组件"的文档承担。
func newNewCommand(opts *Options) *cobra.Command {
	var path string
	var contract string
	cmd := &cobra.Command{
		Use:     "new <scope>/<name>",
		Short:   i18n.T(msgid.CliNewShort),
		GroupID: groupComponent,
		Long:    i18n.T(msgid.CliNewLong),
		Example: i18n.T(msgid.CliNewExample),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(opts, args[0], path, contract)
		},
	}
	cmd.Flags().StringVar(&path, "path", "",
		i18n.T(msgid.CliNewWhichDirectoryToWriteTo))
	cmd.Flags().StringVar(&contract, "contract", "",
		i18n.T(msgid.CliNewContractPlaceholderFormatOpenapiOr))
	return cmd
}

func runNew(opts *Options, id, path, contract string) error {
	files, err := manifest.Scaffold(id, manifest.ScaffoldOptions{Contract: contract})
	if err != nil {
		return err
	}

	rel := path
	if rel == "" {
		rel = filepath.Join(config.DirComponents, id)
	}
	// 绝对路径就是它自己：filepath.Join 会把它当成相对路径接在 WorkDir 后面，
	// 写出去的位置和屏幕上打印的对不上。
	dir := rel
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(opts.WorkDir, rel)
	}

	if _, statErr := os.Stat(dir); statErr == nil {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliNewErrorTheTargetDirectoryAlready)).
			WithDetail(i18n.T(msgid.LabelDir), dir).
			WithHint(
				i18n.T(msgid.CliNewIfThisWasAMistake),
				i18n.T(msgid.CliNewToWriteSomewhereElseSpecify),
			).
			WithExit(clierr.ExitUsage)
	}

	for _, f := range files {
		full := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliNewErrorFailedToCreateThe)).
				WithDetail(i18n.T(msgid.LabelDir), filepath.Dir(full)).WithCause(err)
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliNewErrorFailedToWriteThe)).
				WithDetail(i18n.T(msgid.LabelFile), full).WithCause(err)
		}
	}

	opts.Printf("%s\n", i18n.T(msgid.CliNewComponentSkeletonGenerated, id))
	for _, f := range files {
		opts.Printf("   📄 %s\n", filepath.Join(rel, f.Path))
	}
	opts.Printf("\n")
	opts.Printf("%s\n", i18n.T(msgid.CliNewNextSteps))
	opts.Printf("%s\n", i18n.T(msgid.CliNewFinishTheTodosInThe))
	opts.Printf("%s\n", i18n.T(msgid.CliNewBrickkitAddLocalAddIt))
	opts.Printf("%s\n", i18n.T(msgid.CliNewBrickkitUpDryRunCheck))
	return nil
}
