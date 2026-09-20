package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/version"
)

// newVersionCommand 实现 brickkit version（004 §11.3）。
//
// 输出格式严格对齐设计书（中文示例，英文按 internal/i18n 目录对应变化）：
//
//	BrickKit CLI v1.0.0
//	支持 Manifest 版本：brickkit/v1
//	支持部署目标：docker, k8s
//
// "BrickKit CLI %s" 这一行不接 i18n：纯产品名 + 版本号，不含任何语言
// 相关的词。Short 与 --verbose 的参数说明仍是硬编码中文，留给子项目 2
// （全量迁移）处理——本命令只转换实际打印给用户看的三行输出。
func newVersionCommand(opts *Options) *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "查看 CLI 版本、支持的 Manifest 版本与部署目标",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Printf("BrickKit CLI %s\n", version.Display())
			opts.Printf("%s\n", i18n.T(msgid.VersionManifestLine, version.ManifestAPIVersion))
			opts.Printf("%s\n", i18n.T(msgid.VersionTargetsLine, version.SupportedTargets()))
			if verbose {
				opts.Printf("%s\n", i18n.T(msgid.VersionCommitLine, version.Commit))
				opts.Printf("%s\n", i18n.T(msgid.VersionBuildDateLine, version.BuildDate))
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "额外输出 Git commit 与构建时间")
	return cmd
}
