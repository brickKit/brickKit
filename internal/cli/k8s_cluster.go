package cli

// 本文件负责"别部错地方"。
//
// kubectl 的默认行为是部到 `kubectl config current-context` 指的集群。
// 一份写着生产的部署文件，在一个 context 停在预发的终端里执行，
// **会成功**——没有任何一处提示你部错了。这一类错误在真集群上最贵，
// 而在本地（minikube 只有一个集群）永远试不出来。

import (
	"context"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

// requireContext 校验"现在连着的集群"就是配置里钉住的那个。
//
// 没钉住（部署文件的 k8s.context 为空）就不校验：
// 不是所有人都需要钉住，本地开发钉住反而碍事。
func requireContext(
	ctx context.Context, opts *Options, proj *project.Project, eng engine.Engine, want string,
) error {
	if want == "" || proj.Deploy.Target != deployfile.TargetK8s {
		return nil
	}

	current, err := eng.CurrentContext(ctx)
	if err != nil {
		return clierr.As(err)
	}
	if current == "" || current == want {
		// 取不到就不拦：kubeconfig 的形态千奇百怪（比如 in-cluster），
		// 因为读不到 context 就拒绝部署，只会让人绕开 CLI
		opts.Printf("%s\n", i18n.T(msgid.CliK8sClusterTargetCluster, want))
		return nil
	}

	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliK8sClusterErrorTheClusterCurrentlyConnected)).
		WithDetail(i18n.T(msgid.CliK8sClusterRequiredByTheConfigurationDeploy), want).
		WithDetail(i18n.T(msgid.CliK8sClusterCurrentContext), current).
		WithHint(
			i18n.T(msgid.CliK8sClusterSwitchToItKubectlConfig, want),
			i18n.T(msgid.CliK8sClusterOrSpecifyItExplicitlyFor, current),
			i18n.T(msgid.CliK8sClusterDonTContinueUntilYou),
		)
}
