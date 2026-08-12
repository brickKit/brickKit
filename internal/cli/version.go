package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/version"
)

// newVersionCommand 实现 brickkit version（004 §11.3）。
//
// 输出格式严格对齐设计书：
//
//	BrickKit CLI v1.0.0
//	支持 Manifest 版本：brickkit/v1
//	支持部署目标：docker, k8s
func newVersionCommand(opts *Options) *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "查看 CLI 版本、支持的 Manifest 版本与部署目标",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Printf("BrickKit CLI %s\n", version.Display())
			opts.Printf("支持 Manifest 版本：%s\n", version.ManifestAPIVersion)
			opts.Printf("支持部署目标：%s\n", version.SupportedTargets())
			if verbose {
				opts.Printf("Git commit：%s\n", version.Commit)
				opts.Printf("构建时间：%s\n", version.BuildDate)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "额外输出 Git commit 与构建时间")
	return cmd
}
