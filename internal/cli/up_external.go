package cli

// 本文件是 `external` 组件（P39）在 Docker 目标下的启动前检查。
//
// K8s 那边不需要它：跨命名空间 DNS 原生可用，对方没部署时本项目照常起来，
// 直到第一次真的去调它才失败（003 §4.9）。
// Docker 不一样——服务名只在同一张网络里解析，依赖方必须被接进**对方项目的
// 网络**，而那张网络由对方创建。它不在，compose 直接拒绝启动。

import (
	"context"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/resolver"
)

// checkExternalNetworks 在启动前确认 external 依赖的项目确实部署过。
//
// # 为什么保留"快速失败"，只把错误做对
//
// 一眼看去，让它像 K8s 那样"照常起来、调用时才失败"更一致。但那是更差的：
// Docker 下依赖方拿不到对方的网络，连**地址都解析不了**，表现是运行时的
// `no such host`——而 K8s 那边至少 Service 名是解析得了的。
// 在 `up` 就失败反而是这里能给出的最好结果，问题只在于说了什么。
//
// compose 自己报的是：
//
//	network brickkit-platform-shared-net declared as external, but could not be found
//
// 这句话里没有"external 组件"、没有对方的项目名、也没有下一步该做什么。
// 使用者要先知道网络名是 `brickkit-<项目名>-net` 才能反推回去。
func checkExternalNetworks(ctx context.Context, eng engine.Engine, plan *upPlan) error {
	if len(plan.external) == 0 || plan.cfg.Deploy.Target == config.TargetK8s {
		return nil
	}
	checker, ok := eng.(engine.NetworkChecker)
	if !ok {
		return nil
	}

	// 按项目归集：一个项目部署了多个共享组件时，只报一次
	missing := map[string][]resolver.Ref{}
	for ref, project := range plan.external {
		exists, err := checker.HasNetwork(ctx, deploy.NetworkName(project))
		if err != nil || exists {
			continue
		}
		missing[project] = append(missing[project], ref)
	}
	if len(missing) == 0 {
		return nil
	}

	projects := make([]string, 0, len(missing))
	for project := range missing {
		projects = append(projects, project)
	}
	sort.Strings(projects)

	err := clierr.New(clierr.CodeEngineFailed, "错误：external 依赖的项目还没部署")
	for _, project := range projects {
		refs := missing[project]
		sort.Slice(refs, func(i, j int) bool { return refText(refs[i]) < refText(refs[j]) })
		for _, ref := range refs {
			err = err.WithDetailf("组件", "%s（external.project: %s）", refText(ref), project)
		}
		err = err.WithDetailf("缺少的网络", "%s（由项目 %s 创建）", deploy.NetworkName(project), project)
	}
	return err.
		WithDetail("原因", "Docker 下服务名只在同一张网络里解析，"+
			"依赖方必须接进对方项目的网络——而那张网络由对方 up 时创建").
		WithHint(
			"去部署了它的那个项目目录下执行一次 brickkit up",
			"确认 external.project 写的项目名与对方 brickkit.yaml 里的 project 一致",
			"暂时不需要它的话，把依赖它的组件改成 enabled: false",
		)
}
