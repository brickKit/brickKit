package source

// 本文件实现"本地安装源里有哪些组件"：brickkit add --local 一次把它们全加进来。

import (
	"context"
	"sort"
)

// listableFetcher 是能枚举自己有哪些组件的安装源。
//
// 只有 local 实现它，和 signedFetcher 只有 market 实现是同一个路子：
//   - git 源可能是个单组件仓库、也可能是个按 <scope>/<name> 排布的大目录，
//     形状不固定，枚举出来的东西不可靠；
//   - market 有成千上万个组件，"全都装上"没有意义。
type listableFetcher interface {
	// listComponents 返回该源里的组件 ID（已排序），以及"像组件但用不了"的那些。
	listComponents() ([]string, []listProblem, error)
}

// listProblem 是扫描时发现的"这个目录像组件、但用不了"。
type listProblem struct {
	id     string
	reason string
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

// LocalProblem 是扫描时发现的"这个目录像组件、但用不了"。
type LocalProblem struct {
	// ID 是按目录名拼出来的组件 ID。
	ID string
	// SourceID 是发现它的安装源 id。
	SourceID string
	// Reason 是用不了的原因，直接说给使用者听。
	Reason string
}

// LocalScan 是一次本地枚举的结果。
//
// Problems 与 Components 分开返回而不是合成一个错误：一个坏组件不该拦住其余九个，
// 但也**不能不出声**——静默跳过之后，"扫到 9 个"和"本来就只有 9 个"长得一模一样。
type LocalScan struct {
	Components []LocalComponent
	Problems   []LocalProblem
}

// LocalComponents 列出所有本地安装源里的组件，按组件 ID 排序。
//
// 同一个 ID 出现在多个本地源里时，**靠前的源赢**（003 §6.5），后面的不再列出——
// 与 Manifest / LatestVersion 的取源规则保持一致，否则 add --local 装进来的东西
// 会和随后 up 时真正拉取的那份对不上。
func (c *Client) LocalComponents(ctx context.Context) (*LocalScan, error) {
	scan := &LocalScan{}
	seen := map[string]bool{}

	for _, f := range c.fetchers {
		lister, ok := f.(listableFetcher)
		if !ok {
			continue
		}
		ids, problems, err := lister.listComponents()
		if err != nil {
			return nil, err
		}
		for _, p := range problems {
			if seen[p.id] {
				// 靠前的源已经给出了可用的同 ID 组件，这个源里那份坏的就不必再提
				continue
			}
			scan.Problems = append(scan.Problems,
				LocalProblem{ID: p.id, SourceID: f.id(), Reason: p.reason})
		}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			version, err := f.latestVersion(ctx, id)
			if err != nil {
				// listComponents 刚校验过这份 component.yaml，走到这儿多半是
				// 目录被并发改动了。仍然出声，不静默吞掉。
				scan.Problems = append(scan.Problems,
					LocalProblem{ID: id, SourceID: f.id(), Reason: reasonOf(err)})
				continue
			}
			seen[id] = true
			scan.Components = append(scan.Components,
				LocalComponent{ID: id, Version: version, SourceID: f.id()})
		}
	}

	sort.Slice(scan.Components, func(i, j int) bool { return scan.Components[i].ID < scan.Components[j].ID })
	sort.Slice(scan.Problems, func(i, j int) bool { return scan.Problems[i].ID < scan.Problems[j].ID })
	return scan, nil
}
