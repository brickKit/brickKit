package compose

// 本文件让 Docker 目标支持 `external:`（P39）：引用一个**已经由别的项目部署好**的组件。
//
// # 生成侧只有两件事
//
//	不生成它     没有 service、没有迁移容器，`down` 时自然也碰不到它
//	接通可达性   把**依赖它的**服务接进对方项目的网络
//
// 第二件事是 Docker 特有的。K8s 上跨命名空间 DNS 原生可用，拼个后缀就行；
// 而 Docker 的服务名**只在同一张网络里**解析。少了这一步，
// 注入的地址长得完全正确，容器里却 `no such host`——
// 使用者会去查那个组件是不是没起来，查半天发现它好好地跑着。
//
// # 为什么只接依赖方，不是全部接进去
//
// 网络是可达性边界。把所有组件都接进共享网络，等于把"谁能访问共享服务"
// 从**按依赖声明**变成了**默认全通**——那正是 NetworkPolicy 在 K8s 侧要防的东西，
// 没理由在 Docker 侧自己先破坏掉。

import (
	"sort"

	"github.com/brickkit/brickkit/internal/resolver"
)

// externalComponent 是一个由别的项目部署的组件（P39）。
//
// 它没有 Manifest 以外的渲染需求——本项目不生成它的任何东西，
// 只需要知道"它叫什么、在哪个项目"。
type externalComponent struct {
	Ref     resolver.Ref
	Service string
	Project string
}

// externalProjects 返回本次用到的外部项目名，去重且有序。
//
// 有序是为了生成物稳定：map 遍历顺序随机的话，同一份配置两次生成的
// 网络段顺序会不一样，diff 里全是噪声。
func (p *plan) externalProjects() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range p.externals {
		if !seen[e.Project] {
			seen[e.Project] = true
			out = append(out, e.Project)
		}
	}
	sort.Strings(out)
	return out
}

// externalNetworks 渲染要引用的外部项目网络。
//
// `external: true` 是关键：那张网络由**对方项目**创建，本项目只引用。
// 抢着创建的话，对方执行 `down` 时会把网络一起带走，
// 而本项目的容器还挂在上面——表现为对方重新部署后本项目连不上，
// 且本项目自己毫无变化，极难联想到是网络归属问题。
func (p *plan) externalNetworks() map[string]any {
	out := map[string]any{}
	for _, project := range p.externalProjects() {
		name := networkName(project)
		out[name] = map[string]any{"name": name, "external": true}
	}
	return out
}

// externalNetworksFor 返回某个组件除本项目网络之外，还要接入的网络。
//
// 判据是**它是否依赖了某个外部组件**——不是"本项目有没有用到 external"。
// 两者的区别就是可达性按声明还是默认全通。
func (p *plan) externalNetworksFor(ref resolver.Ref) []string {
	if len(p.externals) == 0 {
		return nil
	}

	byRef := map[resolver.Ref]string{}
	for _, e := range p.externals {
		byRef[e.Ref] = e.Project
	}

	node := p.graph.Node(ref)
	if node == nil {
		return nil
	}

	seen := map[string]bool{}
	var out []string
	// 强弱依赖都算：弱依赖取不到时本来就不会注入地址，
	// 但只要它在图里且是 external，依赖方就得连得上
	for _, dep := range append(append([]resolver.Ref{}, node.Requires...), node.Optional...) {
		project, ok := byRef[dep]
		if !ok {
			continue
		}
		if name := networkName(project); !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// networksFor 是某个组件最终的 networks 列表：本项目网络 + 依赖到的外部网络。
func (p *plan) networksFor(ref resolver.Ref) []string {
	return append([]string{networkAlias}, p.externalNetworksFor(ref)...)
}
