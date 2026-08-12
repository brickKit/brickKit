package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
)

// newStatusCommand 实现 brickkit status（004 §3.7，完整实现见 Step 15）。
func newStatusCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "status",
		Short:   "查看组件运行状态（读取底层引擎）",
		GroupID: groupLifecycle,
		Long: `查看当前项目所有组件的运行状态（004 §3.7）。

CLI 本身不存储运行状态，查询时直接调用底层引擎：
  Docker  docker compose ps --format json
  Podman  podman-compose ps
  K8s     kubectl get pods -l brickkit.io/project=<项目名>

输出包含：运行中的组件表格、已禁用/级联跳过的组件及原因、
本地调试组件（local: true）、基础资源可达性。`,
		Example: "  brickkit status",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return clierr.NotImplemented("brickkit status", 15)
		},
	}
	return cmd
}
