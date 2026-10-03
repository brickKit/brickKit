package k8s

import (
	"sort"

	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/resolver"
)

// exposedEntry 是这次要对外开放的一个组件版本：有自己 Deployment 的组件，或者被外壳承载、
// 条目写了 expose 的成员。
//
// 成员这次没有自己的 Pod，可它有自己的 Service（servedServiceDoc：同名、选中外壳的 Pod、端口是它自己的），
// 外壳进程也按成员声明的端口监听——成员的 *_ENDPOINT 地址靠的就是这一条（shell.checkPortConflicts 在生成期守着）。
// 所以成员的 Ingress 照常指向成员的 Service，由外壳的 Pod 接住；成员条目上的 expose / hostname / tlsSecret
// 这样就有了落脚处，而不是静默不生效。
type exposedEntry struct {
	Ref resolver.Ref
	// Name 是 Ingress 的名字（见 ingressName）。
	Name string
	// Service 是它的后端 Service：版本化服务名。
	Service string
	Port    int
	Entry   deployfile.Entry
	// Shell 是承载它的外壳；组件自己跑时为零值。
	Shell resolver.Ref
}

// ingressName 是一个组件版本的 Ingress 的名字：项目里的默认版本用组件 ID（demo-web），不带版本号；
// 只因别的组件依赖才在项目里的兼容版本用版本化服务名（demo-web-1-0-0）。
//
// # 为什么默认版本的名字不带版本号
//
// 升级就是默认版本换了一个。Ingress 要是跟着版本改名，新的那份（同一个 host、同一条路径）会在旧的那份还在时
// 下发——nginx-ingress 的准入检查直接拒绝（host … and path … is already defined in ingress …），`up` 失败，
// 旧版本继续服务、新版本的 Deployment 留在集群里，再跑一次还是同样的错。这是在 minikube 上真跑撞到的。
// 名字不变，升级就是原地改这份 Ingress 的后端；引擎等新版本就绪之后才下发它（engine.Kubectl.Up），
// 流量一次切过去。Deployment 与 Service 仍然按版本命名：新旧并排，就绪了再删旧的。
//
// 兼容版本不会被升级（它被别的组件钉着），用版本化服务名与默认版本的那份分开。
func (p *plan) ingressName(ref resolver.Ref, service string) string {
	if p.proj.Decl.IsDefault(ref.ID, ref.Version) {
		return containerName(ref.ID)
	}
	return service
}

// exposed 返回这次要对外开放的全部条目，按服务名排序（报错与生成物都要稳定）。
func (p *plan) exposed() []exposedEntry {
	var out []exposedEntry
	for _, c := range p.components {
		if c.Entry.Expose {
			out = append(out, exposedEntry{Ref: c.Ref, Name: p.ingressName(c.Ref, c.Service), Service: c.Service,
				Port: c.Manifest.Deployment.Port, Entry: c.Entry})
		}
	}
	for _, m := range p.served {
		if m.Entry.Expose && m.Manifest != nil {
			out = append(out, exposedEntry{Ref: m.Ref, Name: p.ingressName(m.Ref, m.Service), Service: m.Service,
				Port: m.Manifest.Deployment.Port, Entry: m.Entry, Shell: m.Shell})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}
