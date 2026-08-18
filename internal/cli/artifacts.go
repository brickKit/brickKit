package cli

// 本文件负责**并发下载组件产物**（P40）。
//
// # 这里推翻了 Step 36 记下的一个判断
//
// P40 当初被登记为"暂不做"，理由是：`CheckImage` 是延迟受限（并发有效），
// 而产物下载是**带宽受限**（并发只是把同一条管道切成几份）。
//
// 那是**没有量就下的判断**。真量之后：
//
//	本项目真实产物大小   443 B – 6.5 KB（11 个文件全部实测）
//	一次往返的等待       30–350 ms
//	6.5 KB 的传输        ~0.3 ms
//
// 延迟主导 100–1000 倍，和 CheckImage 完全同一形状。产物是 proto、
// openapi.json 这类**契约文件**，本来就该是小的——"下载"这个词
// 让人先入为主想成了大文件传输。
//
// # 并发在组件这一层
//
// 一个组件通常只声明 1–2 个产物文件（本项目 8 个组件全都是），
// 而组件数会到几十。在组件层并发拿到的是接近组件数的倍数，
// 在文件层只有 2 倍，还得为两层各设一个上限。

import (
	"context"
	"sync"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// artifactConcurrency 是同时进行的产物下载数上限。
//
// 与镜像预检同一个理由：这些请求打到的多半是**同一个市场**，
// 无上限只会撞限流，那时不但不快，还会换来一堆 429。
const artifactConcurrency = 8

// artifactOutcome 是一个组件的下载结果。
type artifactOutcome struct {
	downloaded int
	cached     int
	// warning 非空表示整个组件的下载失败了（产物不阻断安装，004 §10.1）。
	warning string
	// warnings 是逐文件的警告。
	warnings []*clierr.Error
}

// fetchArtifactsOf 是"下载一个组件的产物"这个动作，测试可替换。
type fetchArtifactsOf func(ctx context.Context, node *resolver.Node) artifactOutcome

// downloadArtifacts 并发下载依赖图里每个组件的产物。
func downloadArtifacts(
	ctx context.Context, client *source.Client, graph *resolver.Graph,
) *artifactSummary {
	return downloadArtifactsWith(ctx, graph, func(ctx context.Context, node *resolver.Node) artifactOutcome {
		res, err := client.DownloadArtifacts(ctx, node.Manifest)
		if err != nil {
			// 产物是开发时辅助，不阻断安装（004 §10.1）
			return artifactOutcome{warning: clierr.As(err).Message}
		}
		return artifactOutcome{
			downloaded: len(res.Downloaded),
			cached:     len(res.Cached),
			warnings:   res.Warnings,
		}
	})
}

// downloadArtifactsWith 是可注入下载动作的版本，供测试验证并发与顺序。
func downloadArtifactsWith(
	ctx context.Context, graph *resolver.Graph, fetch fetchArtifactsOf,
) *artifactSummary {
	sum := &artifactSummary{perNode: map[resolver.Ref]int{}}
	if graph == nil {
		return sum
	}

	// 按下标存放结果，最后按**依赖图顺序**汇总。并发的完成顺序每次都不一样，
	// 直接按完成顺序追加的话，同一次 add 连跑两次会输出顺序不同的警告——
	// 使用者会以为发生了不同的事情，而 diff 两次输出也看不出真正的差别
	outcomes := make([]artifactOutcome, len(graph.Nodes))
	sem := make(chan struct{}, artifactConcurrency)
	var wg sync.WaitGroup

	for i, node := range graph.Nodes {
		wg.Add(1)
		go func(i int, node *resolver.Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			outcomes[i] = fetch(ctx, node)
		}(i, node)
	}
	wg.Wait()

	for i, node := range graph.Nodes {
		out := outcomes[i]
		if out.warning != "" {
			sum.warnings = append(sum.warnings,
				clierr.Warn(clierr.CodeNetworkUnreachable, out.warning))
			continue
		}
		sum.downloaded += out.downloaded
		sum.cached += out.cached
		sum.perNode[node.Ref] = out.downloaded + out.cached
		sum.warnings = append(sum.warnings, out.warnings...)
	}
	return sum
}
