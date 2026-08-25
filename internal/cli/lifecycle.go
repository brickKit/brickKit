package cli

// 本文件是 down / status 共用的部分：读配置、算启停、
// 把版本化服务名映射回"人认识的"组件 ID。
//
// up 之后的两个命令都在回答同一个问题的不同侧面："现在这个项目是什么样"。
// 让它们共用同一份读取逻辑，才不会出现 status 说在跑、down 却停不掉的情况。

import (
	"context"
	"fmt"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// project 是"这个项目现在的样子"：配置 + 级联结论。
//
// **刻意没有部署文件的位置，也没有"部署过没有"这个字段。**
// 从前两者都有：`deployed` 取自 `.brickkit/generated/` 下那份生成物在不在，
// `down` 与 `status` 据此提前返回"项目尚未启动过"，引擎一次都不调。
//
// 那是个会消失的判据——生成目录在 `.gitignore` 里，003 §7.1 还明说它
// "整个都是可再生的，删掉重新 up 就会重建"。于是一次 `git clean -xdf`
// 之后，两条命令双双谎报"尚未启动过"，而容器好好地跑着。
// 这与 `DownRequest` 拿掉 `File` 是同一件事，当时只修了引擎那一层，
// 闸门留在了这里。
//
// 现在两条命令都直接问引擎：容器跑没跑，只有引擎知道。
type project struct {
	layout config.Layout
	cfg    *config.Config
	graph  *resolver.Graph
	states *cascade.Result
	// order 是启动顺序（停止时倒着来）。
	order []resolver.Ref
	// localPorts 是 local: true 组件在宿主机上的监听端口。
	localPorts map[resolver.Ref]int
}

// loadProject 读配置、算级联。
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
		localPorts: map[resolver.Ref]int{},
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

// containerRefs 返回本次**由本项目**生成容器的组件，按启动顺序。
//
// `local: true` 要排除掉：它在依赖图里、也"在跑"，但跑在开发者的 IDE 里，
// 本项目不为它生成任何东西（003 §4.4）。`status` 把它们单列一节汇报，
// 不混在"未在运行"里——那会让人以为它们出问题了。
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

// logsCommand 拼出一条**真能用**的查看日志命令。
//
// `-p` 不能省：不带它时 compose 拿当前目录名当项目名，而容器在
// brickkit-<项目> 底下——那条命令会**静默返回空**，不报错也没有输出，
// 使用者会以为组件根本没打日志。真跑验证时撞到过。
//
// # 但 `-f` 与 `--project-directory` 都可以省
//
// 这条命令从前长这样：
//
//	docker compose --project-directory . -p brickkit-x -f .brickkit/generated/docker-compose.yaml logs -f <服务名>
//
// 两个多出来的参数是互为因果的：带了 `-f`，compose 就要插值那份文件，
// 于是要 `--project-directory` 指路去找项目根的 `.env`，否则每次看日志
// 都先刷三行 "variable is not set"。
//
// 而 `logs` 根本不需要那份文件——compose 从容器标签就认得出项目
// （实测 v5.3.1：删掉部署文件后 `-p X logs <服务>` 照常输出，且没有变量警告）。
// 去掉 `-f`，`--project-directory` 也就一起没了，两个坑同时消失。
func logsCommand(engineName, project, service string) string {
	if engineName == engine.K8s {
		target := "deployment/" + service
		if service == "" || service == "<服务名>" {
			target = "deployment/<服务名>"
		}
		return fmt.Sprintf("kubectl logs %s -n %s", target, project)
	}

	command := fmt.Sprintf("docker compose -p %s logs", project)
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
