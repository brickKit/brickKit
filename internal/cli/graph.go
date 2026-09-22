package cli

// 本文件实现 brickkit graph：把项目的依赖拓扑输出成 Mermaid。
//
// 它不生成部署文件、不碰引擎、不启动任何东西，但**不是**只读、也不保证离线：数据来源与
// up --dry-run 相同（依赖图 + 级联结果），再加 brickkit.yaml 里各组件条目自己写的
// local / servedBy，而解析依赖图取 Manifest 是 resolver 的既有行为——与 up --dry-run、
// status 一样，会把取到的 Manifest 写进 .brickkit/manifests/ 缓存，还没缓存的市场 / Git
// 组件还要联网去取。真正只读、不联网、不写任何文件的是 lint。
// 跳过 up 才需要的一切——镜像权限检查、迁移展示、引擎解析、环境变量注入、生成部署文件。
//
// 只输出 Mermaid、只写 stdout：GitHub 直接渲染 .mmd / .mermaid 文件，也渲染 Markdown 里
// mermaid 围栏里的内容，自己再造一个 HTML/SVG 渲染器是重新发明已经免费拿到的东西。
// 要存文件用 shell 重定向。

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
	"github.com/brickkit/brickkit/internal/source"
)

// newGraphCommand 实现 brickkit graph。
func newGraphCommand(opts *Options) *cobra.Command {
	var ignoreServedBy bool

	cmd := &cobra.Command{
		Use:     "graph",
		Short:   i18n.T(msgid.CliGraphShort),
		GroupID: groupProject,
		Long:    i18n.T(msgid.CliGraphLong),
		Example: i18n.T(msgid.CliGraphExample),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraph(cmd.Context(), opts, ignoreServedBy)
		},
	}

	cmd.Flags().BoolVar(&ignoreServedBy, "ignore-served-by", false,
		i18n.T(msgid.CliGraphClearEveryServedbyDeclarationIn))
	return cmd
}

// runGraph 执行 brickkit graph。
func runGraph(ctx context.Context, opts *Options, ignoreServedBy bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	if ignoreServedBy {
		clearServedBy(cfg)
	}

	// 没有组件就不必去碰安装源：那一步会读公钥文件，而这里根本用不上
	if len(cfg.Components) == 0 {
		opts.Printf("%s", renderMermaid(cfg, &resolver.Graph{}, &cascade.Result{}, ignoreServedBy))
		return nil
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	graph, states, err := resolveTopology(ctx, client, cfg)
	if err != nil {
		return err
	}

	// stdout 只留给 Mermaid：解析警告（弱依赖取不到之类）走 stderr
	for _, w := range graph.Warnings {
		_, _ = fmt.Fprint(opts.Stderr, w.Format())
	}
	opts.Printf("%s", renderMermaid(cfg, graph, states, ignoreServedBy))
	return nil
}

// 节点样式类。
//
// classLocal 这个名字历史上就是给 mode: debug 用的——local: true 是
// mode: debug 改名前的旧字段名，这个常量名字从那时候留到现在，没有跟着
// 改名。mode: local 用 classManaged，不复用 classLocal：改 classLocal
// 的名字会牵动它在 Mermaid 输出里的实际取值（"local"这个字符串本身，
// 不只是 Go 里的标识符），对已经生成过 .mmd 文件、拿它们做 git diff 的人
// 是一次没有功能收益的纯 churn，不做。
const (
	classDisabled = "disabled"
	classLocal    = "local"
	classManaged  = "managed"
	classMissing  = "missing"
)

// mermaidClassDefs 按输出顺序列出样式类。用 classDef + class 而不是逐节点 style：
// 一处改样式，全图跟着变。
var mermaidClassDefs = []struct{ name, style string }{
	{classDisabled, "fill:#eee,stroke:#999,color:#999"},
	{classLocal, "fill:#e6f2ff,stroke:#3673a8"},
	{classManaged, "fill:#e6ffe6,stroke:#2e8b57"},
	{classMissing, "fill:#fff4e5,stroke:#c77700,stroke-dasharray:4 3"},
}

// mermaidID 是组件在图里的节点 ID：版本化服务名，再把 - 全部换成 _。
//
// 不能直接用服务名——它并不总是合法的 Mermaid 标识符。组件 ID 的规则
// （manifest.componentIDRe）允许连续的 `--`（my--scope/a），也允许任何单词作 scope
// （graph/store、end/x）；而 Mermaid 把含 `--` 的 ID 当成边，把以 end / style /
// class / graph / subgraph / flowchart / interpolate 开头的 ID 当成关键字。`up` 对这些
// ID 完全正常，graph 若直接用服务名，却会退出码 0 地打印出任何渲染器都不认的文本。
//
// 服务名只含 [a-z0-9-]（组件 ID 的规则里没有 _），所以 - → _ 是单射：不会让两个
// 不同的服务名撞成同一个 ID。节点 ID 因此总以 _数字_数字_数字 结尾。
func mermaidID(ref resolver.Ref) string {
	return strings.ReplaceAll(manifest.ServiceName(ref.ID, ref.Version), "-", "_")
}

// renderMermaid 把拓扑渲染成 Mermaid 文本。
//
// 输出全由输入决定：节点、边、样式都按 graph.Nodes 的解析顺序（依赖先于依赖方）逐段写出，
// 同一份配置每次画出的图逐字节相同，才能放进版本控制里看 diff。这个顺序只在每一段之内成立——
// servedBy 分组整段写在其余节点之前，"未安装"占位节点整段写在所有节点之后——
// 所以文档里别把它说成"整份输出依赖在前"。
func renderMermaid(
	cfg *config.Config, graph *resolver.Graph, states *cascade.Result, ignoredServedBy bool,
) string {
	entries := make(map[resolver.Ref]config.Component, len(cfg.Components))
	for _, c := range cfg.Components {
		entries[resolver.Ref{ID: c.ID, Version: c.Version}] = c
	}

	var b strings.Builder
	b.WriteString("graph TD\n")
	if ignoredServedBy {
		b.WriteString(i18n.T(msgid.CliGraphLlServedbyDeclarationsAreIgnored) + "\n")
	}
	if len(graph.Nodes) == 0 {
		b.WriteString(i18n.T(msgid.CliGraphHeCurrentProjectHasNo) + "\n")
		return b.String()
	}

	// 样式类 → 用到它的节点 ID，按节点声明的顺序累积
	classes := map[string][]string{}
	tag := func(class string, ref resolver.Ref) {
		classes[class] = append(classes[class], mermaidID(ref))
	}
	declare := func(indent string, ref resolver.Ref) {
		running := states.IsRunning(ref)
		label := ref.String()
		if entry := entries[ref]; entry.Mode == config.ModeDebug {
			label += i18n.T(msgid.CliGraphBrLocalDebug)
			// localPort 不是必填：没写时由 up 在生成阶段分配（默认取组件自己声明的
			// 主端口，被占了才另选），这里算不出来。只写使用者写下的，不编一个端口
			if entry.LocalPort > 0 {
				label += fmt.Sprintf(" :%d", entry.LocalPort)
			}
			// local 样式只套给在跑的：cascade 从不读 mode 是不是 debug，mode: debug
			// 的组件与别的组件一样跟着上层走，也可能被跳过。不在跑的只套 disabled——
			// "置灰 = 这次不会启动"是唯一的信号，不靠各家渲染器怎么合并同一个节点上
			// 的两个 class。标签里的"本地调试"照留：那是声明的结构，不是运行状态
			if running {
				tag(classLocal, ref)
			}
		}
		if entry := entries[ref]; entry.Mode == config.ModeLocal {
			label += i18n.T(msgid.CliGraphBrManagedLocally)
			if entry.LocalPort > 0 {
				// mode: local 也接受 localPort 作为"固定端口"的手动覆盖
				// （005 §5：默认自动分配，只有想固定端口时才手动指定）
				label += fmt.Sprintf(" :%d", entry.LocalPort)
			}
			if running {
				tag(classManaged, ref)
			}
		}
		if !running {
			tag(classDisabled, ref)
		}
		fmt.Fprintf(&b, "%s%s[\"%s\"]\n", indent, mermaidID(ref), label)
	}

	// servedBy 分组：只看各条目自己写的 servedBy，不跑环境变量注入。
	// 外壳不在图里（目标不存在）时照样成组——目标在不在是 up 在生成阶段报的事。
	members := map[resolver.Ref][]resolver.Ref{}
	var shells []resolver.Ref
	inShell := map[resolver.Ref]bool{}
	for _, node := range graph.Nodes {
		entry, ok := entries[node.Ref]
		if !ok || entry.ServedBy == "" {
			continue
		}
		target, ok := shell.ParseRef(entry.ServedBy)
		if !ok {
			continue
		}
		if _, seen := members[target]; !seen {
			shells = append(shells, target)
		}
		members[target] = append(members[target], node.Ref)
		inShell[node.Ref] = true
	}

	// 子图 ID 带 _members 后缀：外壳自己也是一个以它的节点 ID 为 ID 的普通节点，
	// 两者不能同名。节点 ID 总以 _数字_数字_数字 结尾，所以不会撞上任何组件节点。
	for _, target := range shells {
		fmt.Fprintf(&b, "%s\n", i18n.T(msgid.CliGraphSubgraphMembersShell, mermaidID(target), target))
		for _, ref := range members[target] {
			declare("        ", ref)
		}
		b.WriteString("    end\n")
	}
	for _, node := range graph.Nodes {
		if !inShell[node.Ref] {
			declare("    ", node.Ref)
		}
	}

	// 取不到的弱依赖：画成占位节点。up 的"依赖图"一节把它们写成"（弱，未安装）"，
	// 图里不画等于悄悄丢掉一条声明过的依赖
	placeholder := map[resolver.Ref]bool{}
	for _, node := range graph.Nodes {
		for _, ref := range node.MissingOptional {
			if placeholder[ref] {
				continue
			}
			placeholder[ref] = true
			fmt.Fprintf(&b, "%s\n", i18n.T(msgid.CliGraphBrNotInstalled, mermaidID(ref), ref))
			tag(classMissing, ref)
		}
	}

	// 边总是画出来，不管对方这次有没有启动：图展示的是声明的结构
	for _, node := range graph.Nodes {
		from := mermaidID(node.Ref)
		for _, dep := range node.Requires {
			fmt.Fprintf(&b, "    %s --> %s\n", from, mermaidID(dep))
		}
		for _, dep := range node.Optional {
			fmt.Fprintf(&b, "    %s -.-> %s\n", from, mermaidID(dep))
		}
		for _, dep := range node.MissingOptional {
			fmt.Fprintf(&b, "    %s -.-> %s\n", from, mermaidID(dep))
		}
	}

	for _, def := range mermaidClassDefs {
		ids := classes[def.name]
		if len(ids) == 0 {
			continue
		}
		fmt.Fprintf(&b, "    classDef %s %s;\n", def.name, def.style)
		fmt.Fprintf(&b, "    class %s %s\n", strings.Join(ids, ","), def.name)
	}
	return b.String()
}
