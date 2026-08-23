package source

// 本文件实现"不指定版本时装哪个版本"：brickkit add <组件ID> 不带 @版本 时，
// 由这里按安装源优先级解析出一个**精确版本**，再走原本的安装流程。
//
// 解析发生在 add 那一刻，结果直接钉进 brickkit.yaml——配置里永远只有精确版本，
// 不存在任何范围约束（012 §2.2）。

import (
	"context"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// Latest 是一次"最新版本"解析的结果。
type Latest struct {
	// Version 是解析出的精确版本。
	Version string
	// SourceID 是给出这个版本的安装源 id。
	SourceID string
	// SourceKind 是该安装源的类型（local / git / market）。
	SourceKind string
}

// LatestVersion 按安装源优先级解析组件的最新可安装版本。
//
// **第一个有这个组件的源说了算**，不跨源比大小（003 §6.5）。跨源比会让
// "到底装了哪个源的东西"变得不可预测：本地源里正在开发的 1.0.0 会被市场上的
// 9.9.9 顶掉，而把本地源排在前面的人要的恰恰是相反的结果。
func (c *Client) LatestVersion(ctx context.Context, id string) (*Latest, error) {
	if problem := manifest.ComponentIDProblem(id); problem != "" {
		return nil, clierr.Newf(clierr.CodeInvalidArgument, "错误：组件 ID 不合法：%s", id).
			WithDetail("原因", problem).
			WithHint("组件 ID 格式为 <scope>/<name>，如 people/basic（002 §2.3）")
	}
	if len(c.fetchers) == 0 {
		return nil, noSourcesError()
	}

	var failures []failure
	for _, f := range c.fetchers {
		version, err := f.latestVersion(ctx, id)
		if err != nil {
			failures = append(failures, failure{sourceID: f.id(), err: err})
			continue
		}
		if !manifest.IsExactVersion(version) {
			// 源给回了一个不是 major.minor.patch 的东西：不能钉进配置，
			// 当作"这个源给不了"，继续下一个源。
			failures = append(failures, failure{sourceID: f.id(), err: errNotFound})
			continue
		}
		return &Latest{Version: version, SourceID: f.id(), SourceKind: f.kind()}, nil
	}

	return nil, c.notFoundError(id, failures,
		"检查安装源配置（brickkit.yaml → sources）",
		"确认组件 ID 是否正确，以及它是否已发布到市场",
		"指定精确版本重试：brickkit add "+id+"@1.0.0",
	)
}
