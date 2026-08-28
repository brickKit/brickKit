package cli

// 本文件是 down / status 共用的部分：读配置、算启停、
// 把版本化服务名映射回"人认识的"组件 ID。
//
// up 之后的两个命令都在回答同一个问题的不同侧面："现在这个项目是什么样"。
// 但**共用的只该是它们都需要的那部分**：两条命令都要项目名，只有 status
// 要依赖图。从前它们共用一个把两件事一起做完的 loadProject，
// 于是解析依赖图成了停容器的前置条件——那是一次沉默的越界。

import (
	"context"
	"fmt"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
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
	// degraded 非 nil 表示**依赖图没解析出来**，本次只能给出部分结论。
	//
	// 它是一个结论，不是一个错误：读不到 Manifest 并不妨碍回答
	// "现在什么在跑"——那个答案只来自引擎。谁需要依赖图、需要它回答哪一句，
	// 由各个渲染函数自己决定（见 status.go 的 buildView）。
	degraded *clierr.Error
}

// loadConfig 只读 brickkit.yaml，**不碰安装源**。
//
// down 走这条：它交给引擎的只有项目名（"停掉 brickkit-<项目名> 名下的一切"，
// 005 §5.9.3），依赖图里的任何东西它都用不上。
//
// 从前它和 status 共用下面那个 loadProject，于是解析依赖图成了停容器的前置条件——
// component.yaml 里一处笔误、本地源目录被删（`components/` 本来就在 .gitignore 里）、
// 市场连不上，任何一种都让 down 直接失败，而容器好好跑着。
// 一条停不掉项目的 down 比没有 down 更糟：使用者以为自己有退路。
func loadConfig(opts *Options) (*project, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return nil, err
	}
	return &project{layout: layout, cfg: cfg}, nil
}

// loadProject 读配置，并**尽力**解析依赖图。
//
// 不重新生成部署文件：down / status 面对的是**已经跑起来的东西**，
// 重新生成只会掩盖"配置改了但还没 up"这个事实。
//
// 解析失败不算命令失败，只记进 degraded：status 的五节里只有"未启动"
// 那一列**原因**真的需要依赖图，为它把整条命令拖死，换来的是
// 使用者连"容器还在不在"都问不到。
func loadProject(ctx context.Context, opts *Options) (*project, error) {
	p, err := loadConfig(opts)
	if err != nil {
		return nil, err
	}
	if len(p.cfg.Components) == 0 {
		return p, nil
	}
	if err := p.resolve(ctx, opts); err != nil {
		p.graph, p.states, p.order = nil, nil, nil
		p.degraded = clierr.As(err)
	}
	return p, nil
}

// resolve 解析依赖图并算出级联与启动顺序。
func (p *project) resolve(ctx context.Context, opts *Options) error {
	client, err := newSourceClient(opts, p.layout, p.cfg, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if p.graph, err = resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, p.cfg); err != nil {
		return err
	}
	if p.states, err = cascade.Compute(p.cfg, p.graph); err != nil {
		return err
	}
	if plan, err := resolver.Order(p.graph.Subgraph(p.states.Running())); err == nil {
		for _, step := range plan.Steps {
			p.order = append(p.order, step.Ref)
		}
	}
	return nil
}

// componentRefs 是本次要汇报的组件。
//
//	正常   级联判定为"会启动"的那些，按启动顺序
//	降级   brickkit.yaml 里声明的全部，按声明顺序——判不出谁该跑，
//	       那就一个都不漏地列出来，由调用方去说明各自的处境
func (p *project) componentRefs() []resolver.Ref {
	if p.degraded == nil {
		return p.order
	}
	out := make([]resolver.Ref, 0, len(p.cfg.Components))
	for _, c := range p.cfg.Components {
		out = append(out, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	return out
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
	for _, ref := range p.componentRefs() {
		if !p.entry(ref).Local {
			out = append(out, ref)
		}
	}
	return out
}

// localRefs 返回 local: true 且本次会启动的组件。
func (p *project) localRefs() []resolver.Ref {
	var out []resolver.Ref
	for _, ref := range p.componentRefs() {
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
