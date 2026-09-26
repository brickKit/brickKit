package cli

// 本文件是"这个项目的拓扑是什么样"的共用部分：解析依赖图、算级联。
//
// up、down/status、sync、graph 四处问的都是同一个问题的不同侧面。前三处从前各自
// 写了一遍这两步——以后其中任何一步的调用方式变了，要记得同步三处，而
// "记得"正是会出事的地方。共用的只该是它们都需要的这一部分。

import (
	"context"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// resolveTopology 解析依赖图并算出级联状态（哪些组件这次会启动）。
//
// client 由调用方创建、也由调用方关闭：up 在这之后还要用同一个客户端补升级摘要，
// 不能在这里替它关。出错时两个结果都是 nil。
func resolveTopology(
	ctx context.Context, client *source.Client, proj *project.Project,
) (*resolver.Graph, *cascade.Result, error) {
	graph, err := resolver.New(resolver.FromSource(client)).ResolveProject(ctx, proj)
	if err != nil {
		return nil, nil, err
	}
	states, err := cascade.Compute(proj, graph)
	if err != nil {
		return nil, nil, err
	}
	return graph, states, nil
}
