package cascade

// 本文件回答"这次谁在谁的进程里跑"（提案 §8）。它放在 cascade，是因为答案一半来自
// 声明（部署文件的 members），一半来自这次的启停判定（外壳跑没跑、成员是不是裸进程）——
// 两样都在这里才齐。inject（地址重写）、shell（JSON 注入）、compose / k8s（生不生成容器）、
// graph（怎么画）都问这一个函数，不各自再判一遍：判据一旦分叉，就会出现"这里生成了容器，
// 那里又把它当成外壳成员"的双重归类。

import (
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// ShellOf 返回 ref 在声明上属于哪个外壳：成员关系只看部署文件的 members（提案 §8.4），
// 外壳在 brickkit.yaml 里只有一个版本（单版本约束，提案 §8.6）。与这次跑不跑、mode 无关。
func ShellOf(p *project.Project, ref resolver.Ref) (resolver.Ref, bool) {
	shellID, ok := p.ShellOf(ref.ID, ref.Version)
	if !ok {
		return resolver.Ref{}, false
	}
	versions := p.Decl.Versions(shellID)
	if len(versions) != 1 {
		return resolver.Ref{}, false
	}
	return resolver.Ref{ID: shellID, Version: versions[0]}, true
}

// HostOf 返回这次承载 ref 的外壳：声明上是成员、自己不是裸进程（mode: debug / local 的成员
// 这次在宿主机上自己跑，附录 A18），并且外壳这次在跑（外壳没跑时成员回落成独立组件）。
func (r *Result) HostOf(p *project.Project, ref resolver.Ref) (resolver.Ref, bool) {
	shell, ok := ShellOf(p, ref)
	if !ok || p.DeployEntry(ref.ID, ref.Version).IsBareProcess() || !r.IsRunning(shell) {
		return resolver.Ref{}, false
	}
	return shell, true
}

// hosting 是声明上的承载关系（与这次谁跑无关）：成员 → 外壳，外壳 → 成员。自己以裸进程运行的
// 成员（mode: debug / local）不算：它这次不在外壳里。焦点的可达性要把承载当成一条边。
type hosting struct {
	shellOf map[resolver.Ref]resolver.Ref
	members map[resolver.Ref][]resolver.Ref
}

func hostingOf(p *project.Project, graph *resolver.Graph) hosting {
	h := hosting{shellOf: map[resolver.Ref]resolver.Ref{}, members: map[resolver.Ref][]resolver.Ref{}}
	if p == nil {
		return h
	}
	for _, node := range graph.Nodes {
		shell, ok := ShellOf(p, node.Ref)
		if !ok || p.DeployEntry(node.Ref.ID, node.Ref.Version).IsBareProcess() {
			continue
		}
		h.shellOf[node.Ref] = shell
		h.members[shell] = append(h.members[shell], node.Ref)
	}
	return h
}
