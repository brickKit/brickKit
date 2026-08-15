package cli

// 本文件是 down / status 共用的部分：读部署文件、按 --only 选组件、
// 把版本化服务名映射回"人认识的"组件 ID。
//
// up 之后的两个命令都在回答同一个问题的不同侧面："现在这个项目是什么样"。
// 让它们共用同一份读取逻辑，才不会出现 status 说在跑、down 却停不掉的情况。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// project 是"这个项目现在的样子"：配置 + 级联结论 + 部署文件位置。
type project struct {
	layout config.Layout
	cfg    *config.Config
	graph  *resolver.Graph
	states *cascade.Result
	// file 是生成的部署文件路径。
	file string
	// deployed 表示部署文件存在（即至少 up 过一次）。
	deployed bool
	// order 是启动顺序（停止时倒着来）。
	order []resolver.Ref
	// localPorts 是 local: true 组件在宿主机上的监听端口。
	localPorts map[resolver.Ref]int
}

// loadProject 读配置、算级联、定位部署文件。
//
// 不重新生成部署文件：down / status 面对的是**已经跑起来的东西**，
// 重新生成只会掩盖"配置改了但还没 up"这个事实。
func loadProject(ctx context.Context, opts *Options) (*project, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return nil, err
	}

	p := &project{
		layout:     layout,
		cfg:        cfg,
		file:       deployedPath(layout, cfg),
		localPorts: map[resolver.Ref]int{},
	}
	if _, err := os.Stat(p.file); err == nil {
		p.deployed = true
	}

	if len(cfg.Components) == 0 {
		return p, nil
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	if p.graph, err = resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg); err != nil {
		return nil, err
	}
	if p.states, err = cascade.Compute(cfg, p.graph); err != nil {
		return nil, err
	}
	if plan, err := resolver.Order(p.graph.Subgraph(p.states.Running())); err == nil {
		for _, step := range plan.Steps {
			p.order = append(p.order, step.Ref)
		}
	}
	return p, nil
}

// entry 返回 brickkit.yaml 中该组件的条目。
func (p *project) entry(ref resolver.Ref) config.Component {
	for _, c := range p.cfg.Components {
		if c.ID == ref.ID && c.Version == ref.Version {
			return c
		}
	}
	return config.Component{}
}

// containerRefs 返回本次会有容器的组件（排除 local: true），按启动顺序。
func (p *project) containerRefs() []resolver.Ref {
	var out []resolver.Ref
	for _, ref := range p.order {
		if !p.entry(ref).Local {
			out = append(out, ref)
		}
	}
	return out
}

// localRefs 返回 local: true 且本次会启动的组件。
func (p *project) localRefs() []resolver.Ref {
	var out []resolver.Ref
	for _, ref := range p.order {
		if p.entry(ref).Local {
			out = append(out, ref)
		}
	}
	return out
}

// refOfService 把版本化服务名映射回组件引用。
func (p *project) refOfService(service string) (resolver.Ref, bool) {
	for _, c := range p.cfg.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		if manifest.ServiceName(c.ID, c.Version) == service {
			return ref, true
		}
	}
	return resolver.Ref{}, false
}

// selectRefs 把 --only 的写法解析成组件引用（004 §3.5）。
//
//	people/basic          该组件的**所有**版本
//	people/basic@1.0.0    只这一个版本
func (p *project) selectRefs(only []string) ([]resolver.Ref, error) {
	var out []resolver.Ref
	seen := map[resolver.Ref]bool{}

	for _, item := range only {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, version, hasVersion := strings.Cut(item, "@")

		var matched []resolver.Ref
		for _, c := range p.cfg.Components {
			if c.ID != id || (hasVersion && c.Version != version) {
				continue
			}
			matched = append(matched, resolver.Ref{ID: c.ID, Version: c.Version})
		}
		if len(matched) == 0 {
			return nil, unknownComponentError(item, p.cfg)
		}
		for _, ref := range matched {
			if !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
		}
	}
	return out, nil
}

// unknownComponentError 指出 --only 里那个名字不存在，并列出可选项。
func unknownComponentError(item string, cfg *config.Config) error {
	err := clierr.Newf(clierr.CodeComponentNotFound,
		"错误：--only 指定的组件不在 brickkit.yaml 中").
		WithDetail("指定的组件", item)

	var available []string
	for _, c := range cfg.Components {
		available = append(available, c.Ref())
	}
	sort.Strings(available)
	if len(available) > 0 {
		err = err.WithDetail("可选的组件", strings.Join(available, "、"))
	}
	return err.WithHint("组件 ID 要写全（scope/name），版本可选（people/basic@1.0.0）")
}

// servicesOf 把组件引用转成版本化服务名。
func servicesOf(refs []resolver.Ref) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, manifest.ServiceName(ref.ID, ref.Version))
	}
	return out
}

// reverse 反转一份引用列表（停止顺序 = 启动顺序的倒序）。
func reverse(refs []resolver.Ref) []resolver.Ref {
	out := make([]resolver.Ref, 0, len(refs))
	for i := len(refs) - 1; i >= 0; i-- {
		out = append(out, refs[i])
	}
	return out
}

// inStartOrder 按启动顺序排列给定的组件；不在启动集合里的排在最后。
func (p *project) inStartOrder(refs []resolver.Ref) []resolver.Ref {
	position := map[resolver.Ref]int{}
	for i, ref := range p.order {
		position[ref] = i
	}

	out := append([]resolver.Ref{}, refs...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, oki := position[out[i]]
		pj, okj := position[out[j]]
		switch {
		case oki && okj:
			return pi < pj
		case oki:
			return true
		case okj:
			return false
		default:
			return refText(out[i]) < refText(out[j])
		}
	})
	return out
}

// deployedPath 是"上次 up 生成了什么"的位置。
//
// 两种目标的形态不同：Docker 是一份文件，K8s 是一整个目录。
// down / status 只关心"它在不在"——在，说明这个项目 up 过。
func deployedPath(layout config.Layout, cfg *config.Config) string {
	if cfg.Deploy.Target == config.TargetK8s {
		return filepath.Join(layout.GeneratedDir(), k8sDirName)
	}
	return filepath.Join(layout.GeneratedDir(), composeFileName)
}

// logsCommand 拼出一条**真能用**的查看日志命令。
//
// `-p` 不能省：compose 会拿部署文件所在目录名（generated）当项目名，
// 而容器在 brickkit-<项目> 底下——不带 -p 的命令会**静默返回空**，
// 不报错也没有输出，使用者会以为组件根本没打日志。真跑验证时撞到过。
func logsCommand(engineName, project, file, service string) string {
	if engineName == engine.K8s {
		// K8s 侧看的是 Deployment 的日志，与部署文件路径无关
		target := "deployment/" + service
		if service == "" || service == "<服务名>" {
			target = "deployment/<服务名>"
		}
		return fmt.Sprintf("kubectl logs %s -n %s", target, project)
	}

	bin := "docker compose"
	if engineName == engine.Podman {
		bin = "podman-compose"
	}
	// --project-directory 也不能省：compose 会在**部署文件旁边**找 .env，
	// 而使用者的 .env 在项目根。少了它，每次看日志都会先刷三行
	// "The XXX variable is not set. Defaulting to a blank string."，
	// 让人以为配置出了问题
	command := fmt.Sprintf("%s --project-directory . -p %s -f %s logs", bin, project, file)
	if service != "" {
		command += " " + service
	}
	return command
}

// engineProject 是引擎侧的项目名：Docker 下是 compose 项目名，K8s 下是命名空间。
//
// 两者取值相同（brickkit-<项目名>），但来源不该混——各自的命名规则由各自那一侧定义。
func (p *project) engineProject() string {
	if p.cfg.Deploy.Target == config.TargetK8s {
		return k8s.NamespaceOf(p.cfg)
	}
	return engine.ProjectName(p.cfg.Project)
}

func refText(ref resolver.Ref) string { return ref.ID + "@" + ref.Version }
