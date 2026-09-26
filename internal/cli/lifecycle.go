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
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// liveProject 是"这个项目现在的样子"：装载好的三层文件 + 级联结论。
//
// **刻意没有部署文件的位置，也没有"部署过没有"这个字段。**
// 从前两者都有：`deployed` 取自 `.brickkit/generated/` 下那份生成物在不在，
// 而那个目录整个都是可再生的，一次 `git clean -xdf` 之后 down 与 status 双双谎报
// "尚未启动过"，而容器好好地跑着。现在两条命令都直接问引擎：容器跑没跑，只有引擎知道。
type liveProject struct {
	proj   *project.Project
	graph  *resolver.Graph
	states *cascade.Result
	// order 是启动顺序（停止时倒着来）。
	order []resolver.Ref
	// degraded 非 nil 表示**依赖图没解析出来**，本次只能给出部分结论。
	//
	// 它是一个结论，不是一个错误：读不到 Manifest 并不妨碍回答"现在什么在跑"——
	// 那个答案只来自引擎。谁需要依赖图、需要它回答哪一句，由各个渲染函数自己决定。
	degraded *clierr.Error
	// teamModes 是团队 deploy.yaml 里各组件的 mode（本地模式下 status 比对用，懒加载）。
	teamModes map[string]string
}

// loadConfig 只解析 brickkit.yaml 与部署文件，**不碰安装源、不读 config/、不做跨文件校验**。
//
// down 走这条：它交给引擎的只有项目名、目标与 k8s 设置，依赖图、配置值它都用不上。
// 一条因为 component.yaml 笔误、config/ 里的 $var: 写错、安装源连不上就停不掉项目的 down，
// 比没有 down 更糟。
func loadConfig(opts *Options) (*liveProject, error) {
	proj, err := project.LoadFiles(opts.WorkDir, opts.loadOptions())
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, proj.Warnings)
	return &liveProject{proj: proj}, nil
}

// loadProject 装载三层文件，并**尽力**解析依赖图。
//
// 不重新生成部署文件：down / status 面对的是**已经跑起来的东西**，
// 重新生成只会掩盖"配置改了但还没 up"这个事实。解析失败不算命令失败，只记进 degraded。
//
// 不读 config/（project.LoadTopology）：status 回答"现在什么在跑"，配置值一个都用不上。
func loadProject(ctx context.Context, opts *Options) (*liveProject, error) {
	proj, err := project.LoadTopology(opts.WorkDir, opts.loadOptions())
	if err != nil {
		return nil, err
	}
	renderWarnings(opts, proj.Warnings)
	p := &liveProject{proj: proj}
	if len(p.proj.Decl.Components) == 0 {
		return p, nil
	}
	if err := p.resolve(ctx, opts); err != nil {
		p.graph, p.states, p.order = nil, nil, nil
		p.degraded = clierr.As(err)
	}
	return p, nil
}

// resolve 解析依赖图并算出级联与启动顺序。
func (p *liveProject) resolve(ctx context.Context, opts *Options) error {
	client, err := newSourceClient(opts, p.proj.Layout, p.proj.Decl, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if p.graph, p.states, err = resolveTopology(ctx, client, p.proj); err != nil {
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
func (p *liveProject) componentRefs() []resolver.Ref {
	if p.degraded == nil {
		return p.order
	}
	out := make([]resolver.Ref, 0, len(p.proj.Decl.Components))
	for _, c := range p.proj.Decl.Components {
		out = append(out, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	return out
}

// entry 返回该组件版本的部署条目。
func (p *liveProject) entry(ref resolver.Ref) deployfile.Component {
	return p.proj.DeployEntry(ref.ID, ref.Version)
}

// containerRefs 返回本次**在容器里**跑的组件，按启动顺序——包括外壳承载的成员
// （它们在外壳的容器里，见 hostOf）。
//
// mode: debug / local 都要排除：它们在依赖图里、也"在跑"，但跑在开发者的 IDE 里或
// brickkit 前台监管的裸进程里，本项目对它们不生成任何容器。裸进程外壳承载的成员同理。
func (p *liveProject) containerRefs() []resolver.Ref {
	var out []resolver.Ref
	for _, ref := range p.componentRefs() {
		if p.entry(ref).IsBareProcess() {
			continue
		}
		if shellRef, ok := p.hostOf(ref); ok && p.entry(shellRef).IsBareProcess() {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// hostOf 返回这次承载 ref 的外壳（cascade.Result.HostOf）；依赖图取不到时判不出，一律 false。
func (p *liveProject) hostOf(ref resolver.Ref) (resolver.Ref, bool) {
	if p.states == nil {
		return resolver.Ref{}, false
	}
	return p.states.HostOf(p.proj, ref)
}

// debugRefs 返回 mode: debug 且本次会启动的组件（mode: local 走会话锁提示，不在这里）。
func (p *liveProject) debugRefs() []resolver.Ref {
	var out []resolver.Ref
	for _, ref := range p.componentRefs() {
		if p.entry(ref).Mode == deployfile.ModeDebug {
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
//	docker compose --project-directory . -p brickkit-x -f .brickkit/generated/compose.yaml logs -f <服务名>
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
		if service == "" || service == i18n.T(msgid.ServiceNamePlaceholder) {
			target = i18n.T(msgid.CliLifecycleDeploymentServiceName)
		}
		return fmt.Sprintf("kubectl logs %s -n %s", target, project)
	}

	command := fmt.Sprintf("%s compose -p %s logs", engineName, project)
	if service != "" {
		command += " " + service
	}
	return command
}

// engineProject 是引擎侧的项目名：Docker 下是 compose 项目名，K8s 下是命名空间。
//
// 两者取值相同（brickkit-<项目名>），但来源不该混——各自的命名规则由各自那一侧定义。
func (p *liveProject) engineProject() string {
	if p.proj.Deploy.Target == deployfile.TargetK8s {
		return k8s.NamespaceOf(p.proj)
	}
	return engine.ProjectName(p.proj.Decl.Project)
}

func refText(ref resolver.Ref) string { return ref.ID + "@" + ref.Version }

// loadDecl 只读 brickkit.yaml。
//
// fetch / login / publish 只关心"有哪些安装源"：部署文件与 config/ 缺了、写错了，
// 都不该挡住它们——取个契约、登个录，不需要项目可部署。
func loadDecl(opts *Options) (project.Layout, *projfile.File, error) {
	layout := project.NewLayout(opts.WorkDir)
	decl, err := projfile.ParseFile(layout.DeclPath())
	return layout, decl, err
}
