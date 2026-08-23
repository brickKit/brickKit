package source

// 本文件实现"本地安装源里有哪些组件"：brickkit add --local 一次把它们全加进来。

import (
	"context"
	"sort"

	"github.com/brickkit/brickkit/internal/manifest"
)

// listableFetcher 是能枚举自己有哪些组件的安装源。
//
// 只有 local 实现它，和 signedFetcher 只有 market 实现是同一个路子：
//   - git 源可能是个单组件仓库、也可能是个按 <scope>/<name> 排布的大目录，
//     形状不固定，枚举出来的东西不可靠；
//   - market 有成千上万个组件，"全都装上"没有意义。
type listableFetcher interface {
	// listComponents 返回该源里的组件 ID，已排序。
	listComponents() ([]string, error)
}

// LocalComponent 是本地安装源里的一个组件。
type LocalComponent struct {
	// ID 是组件 ID。
	ID string
	// Version 是该组件在这个源里的版本（本地源一个组件只有一份目录、一个版本）。
	Version string
	// SourceID 是提供它的安装源 id。
	SourceID string
}

// Ref 返回 "<id>@<version>" 形式的组件引用。
func (lc LocalComponent) Ref() string { return lc.ID + "@" + lc.Version }

// LocalComponents 列出所有本地安装源里的组件，按组件 ID 排序。
//
// 同一个 ID 出现在多个本地源里时，**靠前的源赢**（003 §6.5），后面的不再列出——
// 与 Manifest / LatestVersion 的取源规则保持一致，否则 add --local 装进来的东西
// 会和随后 up 时真正拉取的那份对不上。
func (c *Client) LocalComponents(ctx context.Context) ([]LocalComponent, error) {
	var out []LocalComponent
	seen := map[string]bool{}

	for _, f := range c.fetchers {
		lister, ok := f.(listableFetcher)
		if !ok {
			continue
		}
		ids, err := lister.listComponents()
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			version, err := f.latestVersion(ctx, id)
			if err != nil {
				// 刚扫到就取不到版本：目录被并发改动了，或 component.yaml 有问题。
				// 跳过而不是报错——它只是这个源里的一个目录，不该拖垮整次枚举。
				continue
			}
			if !manifest.IsExactVersion(version) {
				continue
			}
			seen[id] = true
			out = append(out, LocalComponent{ID: id, Version: version, SourceID: f.id()})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
