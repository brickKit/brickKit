package cli

// 本文件是"这个项目的拓扑是什么样"的共用部分：解析依赖图、算级联。
//
// up、down/status、sync、graph 四处问的都是同一个问题的不同侧面。前三处从前各自
// 写了一遍这两步——以后其中任何一步的调用方式变了，要记得同步三处，而
// "记得"正是会出事的地方。共用的只该是它们都需要的这一部分。

import (
	"context"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// resolveTopology 解析依赖图并算出级联状态（哪些组件这次会启动）。
//
// client 由调用方创建、也由调用方关闭：up 在这之后还要用同一个客户端补升级摘要，
// 不能在这里替它关。出错时两个结果都是 nil。
func resolveTopology(
	ctx context.Context, client *source.Client, cfg *config.Config,
) (*resolver.Graph, *cascade.Result, error) {
	graph, err := resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	states, err := cascade.Compute(cfg, graph)
	if err != nil {
		return nil, nil, err
	}
	return graph, states, nil
}

// clearServedBy 在内存里清空全部 servedBy 声明（--ignore-served-by 的实现）。
//
// 格式校验在 ParseConfigFile 里已经跑完，不会被这一步绕过。下游 resolver /
// shell.Resolve / compose / k8s 全部只读 ServedBy 这一个字段，没有任何一处维护
// 自己的派生状态，清空一次就够，不需要逐处打补丁。只改内存里的 cfg，从不写回
// 磁盘上的 brickkit.yaml。
//
// 只清空、不打印：up 用一行 ⚠️ 告诉使用者，graph 的 stdout 必须是纯 Mermaid，
// 只能写成注释——各自决定怎么说。
func clearServedBy(cfg *config.Config) {
	for i := range cfg.Components {
		cfg.Components[i].ServedBy = ""
	}
}
