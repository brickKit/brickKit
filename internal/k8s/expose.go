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
	// Service 是 Ingress 的名字，也是它的后端 Service。
	Service string
	Port    int
	Entry   deployfile.Entry
	// Shell 是承载它的外壳；组件自己跑时为零值。
	Shell resolver.Ref
}

// exposed 返回这次要对外开放的全部条目，按服务名排序（报错与生成物都要稳定）。
func (p *plan) exposed() []exposedEntry {
	var out []exposedEntry
	for _, c := range p.components {
		if c.Entry.Expose {
			out = append(out, exposedEntry{Ref: c.Ref, Service: c.Service, Port: c.Manifest.Deployment.Port, Entry: c.Entry})
		}
	}
	for _, m := range p.served {
		if m.Entry.Expose && m.Manifest != nil {
			out = append(out, exposedEntry{Ref: m.Ref, Service: m.Service, Port: m.Manifest.Deployment.Port, Entry: m.Entry, Shell: m.Shell})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}
