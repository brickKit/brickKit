package cli

// 本文件负责"别部错地方"（005 §5.11）。
//
// kubectl 的默认行为是部到 `kubectl config current-context` 指的集群。
// 一份写着生产的 brickkit.yaml，在一个 context 停在预发的终端里执行，
// **会成功**——没有任何一处提示你部错了。这一类错误在真集群上最贵，
// 而在本地（minikube 只有一个集群）永远试不出来。

import (
	"context"
	"fmt"
	"strings"

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

// warnTargetOnlyFields 提醒"写了、但在当前部署目标下不生效"的字段。
//
// 003 §3.2 立的规矩：**写了不生效就得出声**——不提醒的话，使用者会以为
// 配置生效了而行为没变。这里守两个方向：
//
//	Docker 目标   deploy.* 全部（除 target）+ 组件的 replicas / hostname / tlsSecret /
//	              serviceAccountName
//	K8s 目标      组件的 exposePort
//
// `local` / `localPort` 不在此列：K8s 下它们是**报错**，不是警告
// （k8s.localNotSupported）。跳过的后果是依赖方拿到一个指向不存在 Service
// 的地址，表现成随机的连接超时（005 §5.3.1），性质与"这一行没生效"不同。
func warnTargetOnlyFields(opts *Options, cfg *config.Config) {
	if cfg.Deploy.Target == config.TargetK8s {
		warnFields(opts, cfg, "Docker", dockerOnlyFields(cfg),
			"K8s 通过 Ingress + 域名路由对外暴露，不映射宿主机端口（003 §4.5）",
			"对外暴露请给组件填 hostname")
		return
	}
	warnFields(opts, cfg, "K8s", k8sOnlyFields(cfg),
		"这些字段只在 deploy.target: k8s 下生效，本次被忽略",
		"要部署到 K8s 请把 deploy.target 改成 k8s")
}

// fieldUse 是"某个字段被哪些组件写了"。
//
// 按**字段**归集而不是按组件：同一件事说 N 遍会把警告区刷满，
// 而使用者一旦开始整块跳过警告，真正要紧的那几条也一起被跳过。
// 真跑过：4 个组件各写 replicas + tlsSecret，就是 8 行；20 个组件就是 40 行。
type fieldUse struct {
	name string
	// components 为空表示它是项目级字段（deploy.* 那些）。
	components []string
}

// k8sOnlyFields 收集只在 K8s 下生效、而当前是 Docker 目标的字段。
func k8sOnlyFields(cfg *config.Config) []fieldUse {
	// 003 §3.2：`deploy` 下除 target 外**全部**只对 K8s 生效
	project := []struct {
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

	var out []fieldUse
	for _, f := range project {
		if f.set {
			out = append(out, fieldUse{name: f.name})
		}
	}

	// 组件级的四个（003 §4.1）。replicas 尤其容易被当成自己写错了字段名——
	// `up` 一切正常，而 `docker ps` 里只有一个容器，字段名却是对的。
	//
	// hostname 与 tlsSecret 是一对，都只服务于 Ingress。从前只报后者，
	// 没有理由——那是同一个函数里的自相矛盾。
	return append(out, componentFields(cfg, []componentField{
		{"replicas", func(c config.Component) bool { return c.Replicas != nil }},
		{"hostname", func(c config.Component) bool { return c.Hostname != "" }},
		{"tlsSecret", func(c config.Component) bool { return c.TLSSecret != "" }},
		{"serviceAccountName", func(c config.Component) bool { return c.ServiceAccountName != "" }},
	})...)
}

// dockerOnlyFields 收集只在 Docker 下生效、而当前是 K8s 目标的字段。
func dockerOnlyFields(cfg *config.Config) []fieldUse {
	return componentFields(cfg, []componentField{
		{"exposePort", func(c config.Component) bool { return c.ExposePort != 0 }},
	})
}

// componentField 是一个组件级字段与"它写了没有"的判断。
type componentField struct {
	name string
	set  func(config.Component) bool
}

func componentFields(cfg *config.Config, fields []componentField) []fieldUse {
	var out []fieldUse
	for _, f := range fields {
		var users []string
		for _, c := range cfg.Components {
			if f.set(c) {
				users = append(users, c.ID)
			}
		}
		if len(users) > 0 {
			out = append(out, fieldUse{name: "components[]." + f.name, components: users})
		}
	}
	return out
}

// maxListedComponents 是每个字段最多点名几个组件。
//
// 有上限是因为组件多起来之后，一行会长到换行三四次，而"是哪几个"
// 到那时已经不重要了——使用者要的是"哪个字段没生效"。
const maxListedComponents = 5

func warnFields(opts *Options, cfg *config.Config, target string, fields []fieldUse, note, hint string) {
	if len(fields) == 0 {
		return
	}

	err := clierr.Warn(clierr.CodeConfigInvalid, "配置里有只对 "+target+" 生效的字段")
	for _, f := range fields {
		if len(f.components) == 0 {
			err = err.WithDetail("字段", f.name)
			continue
		}
		err = err.WithDetailf("字段", "%s（%s）", f.name, describeUsers(f.components))
	}
	renderWarnings(opts, []*clierr.Error{err.
		WithDetail("当前目标", "deploy.target: "+cfg.Deploy.Target).
		WithDetail("说明", note).
		WithHint(hint)})
}

// describeUsers 说清是哪几个组件写了它。
func describeUsers(components []string) string {
	if len(components) <= maxListedComponents {
		return fmt.Sprintf("%d 个组件：%s", len(components), strings.Join(components, "、"))
	}
	return fmt.Sprintf("%d 个组件：%s 等",
		len(components), strings.Join(components[:maxListedComponents], "、"))
}
