package cli

// 本文件负责"别部错地方"（005 §5.11）。
//
// kubectl 的默认行为是部到 `kubectl config current-context` 指的集群。
// 一份写着生产的 brickkit.yaml，在一个 context 停在预发的终端里执行，
// **会成功**——没有任何一处提示你部错了。这一类错误在真集群上最贵，
// 而在本地（minikube 只有一个集群）永远试不出来。

import (
	"context"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
)

// requireContext 校验"现在连着的集群"就是配置里钉住的那个。
//
// 没钉住（deploy.context 为空、也没给 --context）就不校验：
// 不是所有人都需要钉住，本地开发钉住反而碍事。
func requireContext(
	ctx context.Context, opts *Options, cfg *config.Config, eng engine.Engine, want string,
) error {
	if want == "" || cfg.Deploy.Target != config.TargetK8s {
		return nil
	}

	current, err := eng.CurrentContext(ctx)
	if err != nil {
		return clierr.As(err)
	}
	if current == "" || current == want {
		// 取不到就不拦：kubeconfig 的形态千奇百怪（比如 in-cluster），
		// 因为读不到 context 就拒绝部署，只会让人绕开 CLI
		opts.Printf("☸️  目标集群：%s\n", want)
		return nil
	}

	return clierr.New(clierr.CodeConfigInvalid, "错误：当前连着的不是配置里指定的集群").
		WithDetail("配置要求（deploy.context）", want).
		WithDetail("当前 context", current).
		WithHint(
			"切过去：kubectl config use-context "+want,
			"或本次显式指定：brickkit up --context "+current,
			"确认无误前不要继续——部到错误的集群是不可逆的",
		)
}

// contextOf 返回本次要用的 kubeconfig 上下文：命令行参数优先。
func contextOf(cfg *config.Config, flag string) string {
	if flag != "" {
		return flag
	}
	return cfg.Deploy.Context
}

// warnK8sOnlyFields 提醒 K8s 专用字段在 Docker 目标下不生效。
//
// 写了却没生效，比报错更让人困惑：使用者会以为已经钉住了集群。
func warnK8sOnlyFields(opts *Options, cfg *config.Config) {
	if cfg.Deploy.Target == config.TargetK8s {
		return
	}

	// 003 §3.2：`deploy` 下除 target 外**全部**只对 K8s 生效。
	// 逐个列出而不是"有非零字段就笼统提一句"——使用者要知道是哪一个。
	deployFields := []struct {
		name string
		set  bool
	}{
		{"deploy.context", cfg.Deploy.Context != ""},
		{"deploy.namespace", cfg.Deploy.Namespace != ""},
		{"deploy.createNamespace", cfg.Deploy.CreateNamespace != nil},
		{"deploy.podSecurity", cfg.Deploy.PodSecurity != ""},
		{"deploy.imagePullSecrets", len(cfg.Deploy.ImagePullSecrets) > 0},
		{"deploy.ingressClass", cfg.Deploy.IngressClass != ""},
		{"deploy.ingressAnnotations", len(cfg.Deploy.IngressAnnotations) > 0},
		{"deploy.serviceAccount", cfg.Deploy.ServiceAccount != nil},
		// networkPolicy 最要紧：写了它的人以为自己收紧了网络，
		// 而 Docker 下一条策略都不会生成，网络照旧全通
		{"deploy.networkPolicy", cfg.Deploy.NetworkPolicy != nil},
	}

	var fields []string
	for _, f := range deployFields {
		if f.set {
			fields = append(fields, f.name)
		}
	}

	// 组件级的三个：replicas / tlsSecret / serviceAccountName（003 §4.1）。
	// replicas 尤其容易被当成自己写错了字段名——`up` 一切正常，
	// 而 `docker ps` 里只有一个容器，字段名却是对的
	for _, c := range cfg.Components {
		entry := "components[" + c.ID + "]."
		if c.Replicas != nil {
			fields = append(fields, entry+"replicas")
		}
		if c.TLSSecret != "" {
			fields = append(fields, entry+"tlsSecret")
		}
		if c.ServiceAccountName != "" {
			fields = append(fields, entry+"serviceAccountName")
		}
	}
	if len(fields) == 0 {
		return
	}

	err := clierr.Warn(clierr.CodeConfigInvalid, "配置里有只对 K8s 生效的字段")
	for _, field := range fields {
		err = err.WithDetail("字段", field)
	}
	renderWarnings(opts, []*clierr.Error{err.
		WithDetail("当前目标", "deploy.target: "+cfg.Deploy.Target).
		WithDetail("说明", "这些字段只在 deploy.target: k8s 下生效，本次被忽略").
		WithHint("要部署到 K8s 请把 deploy.target 改成 k8s")})
}
