// Package resolver 实现依赖解析引擎。
//
// 设计依据：002 §3 组件依赖与拼装规范、§7.7 升级时依赖兼容性检查、004 §4 依赖解析引擎。
//
// 核心行为：
//
//	递归解析   从根组件出发递归拉取依赖的 Manifest，按 ID + 版本去重（004 §4.2）
//	强依赖缺失 报错阻断，指出是谁依赖了它（002 §3.3）
//	弱依赖缺失 警告但继续，并说明哪个环境变量不会被注入（004 §4.5）
//	循环依赖   报错阻断，打印完整循环路径（004 §4.3）
//	多版本共存 同一 ID 的不同版本是两个独立节点，不冲突、不报错（002 §3.6）
//
// 不在本包职责内：级联启停计算、拓扑排序输出（brickkit order）、环境变量注入。
// 解析结果的 Nodes 已按"依赖先于依赖方"排列，Step 10 的拓扑排序在此之上做输出与分组。
package resolver

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/source"
)

// Ref 是一个精确的组件引用（002 §3.3：版本必须精确）。
type Ref struct {
	ID      string
	Version string
}

func (r Ref) String() string { return r.ID + "@" + r.Version }

// Provider 提供组件 Manifest。生产环境由 internal/source 实现。
type Provider interface {
	Manifest(ctx context.Context, id, version string) (*manifest.Manifest, error)
}

// FromSource 把安装源客户端适配为 Provider。
func FromSource(c *source.Client) Provider { return sourceProvider{c: c} }

type sourceProvider struct{ c *source.Client }

func (s sourceProvider) Manifest(ctx context.Context, id, version string) (*manifest.Manifest, error) {
	fetched, err := s.c.Manifest(ctx, id, version)
	if err != nil {
		return nil, err
	}
	return fetched.Manifest, nil
}

// Node 是依赖图中的一个组件（按 ID + 版本去重后唯一）。
type Node struct {
	// Ref 是该组件的精确引用。
	Ref Ref
	// Manifest 是该组件的 Manifest。
	Manifest *manifest.Manifest
	// Requires 是已解析的强依赖。
	Requires []Ref
	// Optional 是已解析的弱依赖。
	Optional []Ref
	// MissingOptional 是获取失败的弱依赖：CLI 不会为它们注入环境变量（002 §3.4）。
	MissingOptional []Ref
	// Dependents 是依赖本组件的组件，供卸载检查（002 §3.9）与错误提示使用。
	Dependents []Ref
}

// Graph 是一次依赖解析的结果。
type Graph struct {
	// Nodes 按解析顺序排列：依赖先于依赖方。
	Nodes []*Node
	// Roots 是本次解析的根组件。
	Roots []Ref
	// Warnings 是弱依赖缺失等不阻断的问题（⚠️，退出码 0）。
	Warnings []*clierr.Error

	index map[Ref]*Node
}

// Node 按引用查找节点，不存在时返回 nil。
func (g *Graph) Node(ref Ref) *Node { return g.index[ref] }

// Has 判断某个组件版本是否在图中。
func (g *Graph) Has(ref Ref) bool { _, ok := g.index[ref]; return ok }

// Versions 返回图中某个组件 ID 的全部版本（按版本号升序）。
func (g *Graph) Versions(id string) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Ref.ID == id {
			out = append(out, n.Ref.Version)
		}
	}
	sort.Slice(out, func(i, j int) bool { return manifest.CompareVersions(out[i], out[j]) < 0 })
	return out
}

// Refs 按解析顺序返回全部组件引用。
func (g *Graph) Refs() []Ref {
	out := make([]Ref, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		out = append(out, n.Ref)
	}
	return out
}

// Subgraph 返回只含给定组件的子图，依赖关系裁剪到子图内部。
//
// 用于"先算出本次启动哪些组件（级联），再对这批组件排序"：
// 直接对全图排序会把根本不会启动的组件也排进去。
func (g *Graph) Subgraph(refs []Ref) *Graph {
	keep := make(map[Ref]bool, len(refs))
	for _, ref := range refs {
		keep[ref] = true
	}

	out := &Graph{index: map[Ref]*Node{}, Warnings: g.Warnings}
	for _, node := range g.Nodes {
		if !keep[node.Ref] {
			continue
		}
		copied := &Node{
			Ref:             node.Ref,
			Manifest:        node.Manifest,
			Requires:        filterRefs(node.Requires, keep),
			Optional:        filterRefs(node.Optional, keep),
			MissingOptional: node.MissingOptional,
			Dependents:      filterRefs(node.Dependents, keep),
		}
		out.Nodes = append(out.Nodes, copied)
		out.index[copied.Ref] = copied
	}
	for _, ref := range g.Roots {
		if keep[ref] {
			out.Roots = append(out.Roots, ref)
		}
	}
	return out
}

func filterRefs(refs []Ref, keep map[Ref]bool) []Ref {
	var out []Ref
	for _, ref := range refs {
		if keep[ref] {
			out = append(out, ref)
		}
	}
	return out
}

// Resolver 递归解析组件依赖。
type Resolver struct {
	provider Provider
}

// New 构造解析器。
func New(p Provider) *Resolver { return &Resolver{provider: p} }

// Resolve 从若干根组件出发递归解析依赖。
//
// 根组件取不到时，直接上抛安装源的错误（组件未找到 / 市场不可达……）；
// 强依赖取不到时报"强依赖缺失"并指出依赖方；弱依赖取不到时只记警告。
func (r *Resolver) Resolve(ctx context.Context, roots ...Ref) (*Graph, error) {
	rs := &resolution{
		ctx:      ctx,
		provider: r.provider,
		graph:    &Graph{index: map[Ref]*Node{}},
		state:    map[Ref]nodeState{},
	}

	for _, root := range roots {
		if containsRef(rs.graph.Roots, root) {
			continue
		}
		rs.graph.Roots = append(rs.graph.Roots, root)
		if _, err := rs.visit(root, nil); err != nil {
			return nil, unwrapFetch(err)
		}
	}
	return rs.graph, nil
}

// ResolveConfig 以 brickkit.yaml 中的 components 为根解析依赖。
//
// 这里不过滤 enabled：级联启停是 Step 11 的职责，解析阶段先把依赖图建全。
func (r *Resolver) ResolveConfig(ctx context.Context, cfg *config.Config) (*Graph, error) {
	if cfg == nil {
		return r.Resolve(ctx)
	}
	roots := make([]Ref, 0, len(cfg.Components))
	for _, c := range cfg.Components {
		roots = append(roots, Ref{ID: c.ID, Version: c.Version})
	}
	return r.Resolve(ctx, roots...)
}

// ============================================================
// 递归解析
// ============================================================

type nodeState int

const (
	stateUnvisited nodeState = iota
	stateVisiting            // 正在解析其子树：再次进入即为循环
	stateDone
)

type resolution struct {
	ctx      context.Context
	provider Provider
	graph    *Graph
	state    map[Ref]nodeState
}

// visit 解析 ref 及其整棵依赖子树，返回该节点。
//
// strongPath 是从"最近一条弱依赖边之后"到当前节点的**强依赖**链，用于判环。
//
// # 为什么只在强依赖边上判环
//
// 环之所以是错误，是因为它让启动顺序无解。而启动顺序只由强依赖约束——
// `Order` 里的拓扑排序压根不看 `Optional`（弱依赖可能根本不启动，
// 让它约束顺序等于把"可选"偷偷变成"必选"）。
//
// 所以只要环上有一条弱依赖边，它就不影响任何顺序，是合法的形状。
// 两个组件互相"有就用、没有就降级"（通知组件可选地调审计、审计可选地调通知）
// 是很现实的写法，早先会被一句"检测到循环依赖"整个拦下来。
//
// 跨过一条弱边时 strongPath 清空：那条边断开了强依赖链，
// 从它往下重新开始算。
func (rs *resolution) visit(ref Ref, strongPath []Ref) (*Node, error) {
	switch rs.state[ref] {
	case stateDone:
		return rs.graph.index[ref], nil
	case stateVisiting:
		if containsRef(strongPath, ref) {
			return nil, cycleError(strongPath, ref)
		}
		// 弱依赖环：节点还没解析完，但它已经建出来了，原样返回即可。
		// 依赖方拿到的是同一个指针，等它自己那层解析完就填齐了。
		return rs.graph.index[ref], nil
	}

	m, err := rs.provider.Manifest(rs.ctx, ref.ID, ref.Version)
	if err != nil {
		return nil, &fetchError{ref: ref, err: err}
	}

	rs.state[ref] = stateVisiting
	node := &Node{Ref: ref, Manifest: m}
	// 必须在下探之前登记：弱依赖环再次走到这里时要取得同一个节点
	rs.graph.index[ref] = node

	for _, d := range dependenciesOf(m) {
		child := Ref{ID: d.ID, Version: d.Version}
		childPath := append(append([]Ref{}, strongPath...), ref)
		if d.Optional {
			childPath = nil
		}
		childNode, err := rs.visit(child, childPath)
		if err != nil {
			var fe *fetchError
			// 只有"直接子依赖取不到"才由本层处理；更深处的失败已经被包装过，原样上抛。
			if !errors.As(err, &fe) || fe.ref != child {
				return nil, err
			}
			if d.Optional {
				node.MissingOptional = append(node.MissingOptional, child)
				rs.graph.Warnings = append(rs.graph.Warnings, optionalMissingWarning(ref, child, fe.err))
				continue
			}
			return nil, missingDependencyError(ref, child, fe.err)
		}
		childNode.Dependents = append(childNode.Dependents, ref)
		if d.Optional {
			node.Optional = append(node.Optional, child)
		} else {
			node.Requires = append(node.Requires, child)
		}
	}

	rs.state[ref] = stateDone
	// 后序追加：依赖一定排在依赖方之前。
	// 弱依赖环上这条不成立（环上总有一个先被追加），但顺序只被拓扑排序当作
	// 起点提示，而那一步只看强依赖——环上没有强边，也就无所谓。
	rs.graph.Nodes = append(rs.graph.Nodes, node)
	return node, nil
}

func dependenciesOf(m *manifest.Manifest) []manifest.ComponentDep {
	if m == nil || m.Dependencies == nil {
		return nil
	}
	return m.Dependencies.Components
}

// fetchError 标记"这个组件的 Manifest 没取到"，由上一层决定是报错还是警告。
type fetchError struct {
	ref Ref
	err error
}

func (e *fetchError) Error() string { return e.ref.String() + ": " + e.err.Error() }
func (e *fetchError) Unwrap() error { return e.err }

// unwrapFetch 把根组件的获取失败还原成安装源本身的错误。
func unwrapFetch(err error) error {
	var fe *fetchError
	if errors.As(err, &fe) {
		return fe.err
	}
	return err
}

// ============================================================
// 错误与警告
// ============================================================

// missingDependencyError 逐字对齐 004 §10.2 的"强依赖缺失"错误块。
//
// # 底下那层的明细与建议要带上来
//
// "强依赖缺失"只是标题，**为什么**缺才是使用者要解决的东西，而那句话在安装源
// 给出的错误里。从前这里只用 reasonOf 取一行"原因"，其余明细与建议全丢掉：
//
//	市场不可达      丢掉了具体地址与网络错误
//	源里版本不符    丢掉了"哪个源、里面实际是哪个版本"，以及那三条真正管用的建议
//	                ——而剩下的通用三条会把人指向 sources 配置，
//	                可问题在他自己刚改过的那份 component.yaml 里
//
// 所以：底层给了建议就用它的（它更具体），没给才回落到通用的三条。
func missingDependencyError(dependent, missing Ref, cause error) error {
	e := clierr.New(clierr.CodeDependencyMissing, "错误：强依赖缺失").
		WithDetail("组件", dependent.String()).
		WithDetail("缺失依赖", missing.String()).
		WithDetail("原因", reasonOf(cause))

	inner := clierr.As(cause)
	if inner != nil {
		for _, d := range inner.Details {
			// 这三个上面已经写过（值也更贴题），不重复
			if d.Key == "组件" || d.Key == "原因" || d.Key == "要的版本" {
				continue
			}
			e = e.WithDetail(d.Key, d.Value)
		}
	}
	if inner != nil && len(inner.Hints) > 0 {
		return e.WithHint(inner.Hints...).WithCause(cause)
	}
	return e.WithHint(
		"检查安装源配置（brickkit.yaml → sources）",
		"确认组件是否已发布到市场",
		"确认版本号是否正确",
	).WithCause(cause)
}

// optionalMissingWarning 逐字对齐 004 §4.5 的弱依赖警告块。
func optionalMissingWarning(dependent, missing Ref, cause error) *clierr.Error {
	return clierr.Warn(clierr.CodeDependencyMissing, "警告：弱依赖缺失："+missing.String()).
		WithDetail("影响组件", dependent.String()).
		WithDetail("原因", reasonOf(cause)).
		WithDetailf("影响", "该组件的环境变量 %s 不会被注入", manifest.EndpointEnvVar(missing.ID)).
		WithTip("弱依赖降级由组件自行处理（002 §3.4）；如需启用，请确认该组件已发布并可从安装源获取")
}

// cycleError 打印完整循环路径（004 §4.3）。
func cycleError(path []Ref, repeated Ref) error {
	cycle := make([]string, 0, len(path)+1)
	started := false
	for _, ref := range path {
		if ref == repeated {
			started = true
		}
		if started {
			cycle = append(cycle, ref.String())
		}
	}
	cycle = append(cycle, repeated.String())

	return clierr.New(clierr.CodeDependencyCycle, "错误：检测到循环依赖").
		WithDetail("循环路径", strings.Join(cycle, " → ")).
		WithDetail("原因", "这几个组件互相**强依赖**，谁都要等对方先起来，启动顺序无解").
		WithHint(
			"检查 Manifest 中的依赖声明（dependencies.components）",
			"其中一方改成弱依赖（optional: true）即可——弱依赖不约束启动顺序，"+
				"环上有一条弱边就不再是死结",
		)
}

// reasonOf 把安装源的错误压成一行原因。
func reasonOf(err error) string {
	e := clierr.As(err)
	if e == nil {
		return ""
	}
	for _, d := range e.Details {
		if d.Key == "原因" {
			return d.Value
		}
	}
	return strings.TrimPrefix(e.Message, "错误：")
}

// CheckRunningResourceBindings 校验**本次会启动的**组件的资源依赖都已绑定
// （006 §4.4、011 §5.3：`brickkit up` 时检查，未绑定则报错阻断）。
//
// # 为什么只查会启动的那些
//
// 试用指南 02 §2.5 教的做法是"用 enabled: false 把暂时不用的组件关掉，
// 而不是删掉"——那些组件当然还没绑资源。拿它们卡住 up，等于逼使用者
// 要么删组件，要么为一个根本不跑的容器编一份数据库配置。
//
// # 为什么要一次报全
//
// 拼装一个新项目时往往几个组件同时缺绑定。一次只报一个，使用者就得
// "改一行、跑一次"地来回好几趟，而每一趟的报错看上去都像新问题。
//
// # 返回 *clierr.Error 而不是 error
//
// "该不该阻断"是命令层的决定：真的 `up` 必须拦下，而 `--dry-run` 的语义是
// "告诉我会发生什么"，那时它该以警告出现（否则一个还没配资源的项目
// 连"看看会生成什么"都做不到）。把判断留给调用方，这里只负责说清楚是什么问题。
func CheckRunningResourceBindings(cfg *config.Config, graph *Graph, running []Ref) *clierr.Error {
	if cfg == nil || graph == nil {
		return nil
	}
	var details []clierr.Detail
	for _, ref := range running {
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		details = append(details, unboundResourceDetails(cfg, node.Manifest)...)
	}
	if len(details) == 0 {
		return nil
	}

	e := clierr.New(clierr.CodeResourceUnbound, "错误：资源依赖未满足")
	e.Details = append(e.Details, details...)
	// 这一条报的是多个组件，给不出具体的 componentId
	return e.WithHint(resourceHints("", "暂时不想跑这个组件的话，给它写 enabled: false")...)
}

// CheckResourceBindings 校验组件声明的资源依赖是否已在 brickkit.yaml 中
// 声明（kind + engine）且绑定给该组件（003 §5.3）。
//
// 一次报出全部未满足的资源依赖，不是遇到第一个就返回。
func CheckResourceBindings(cfg *config.Config, m *manifest.Manifest) error {
	if m == nil || m.Dependencies == nil || len(m.Dependencies.Resources) == 0 {
		return nil
	}
	problems := unboundResourceDetails(cfg, m)
	if len(problems) == 0 {
		return nil
	}

	e := clierr.New(clierr.CodeResourceUnbound, "错误：资源依赖未满足")
	e.Details = append(e.Details, problems...)
	return e.WithHint(resourceHints(m.Metadata.ID)...)
}

// unboundResourceDetails 列出一个组件没被满足的资源依赖。
//
// 每条都带上组件引用：一次报多个组件时，光说"缺 database"没法定位是谁缺。
func unboundResourceDetails(cfg *config.Config, m *manifest.Manifest) []clierr.Detail {
	if m == nil || m.Dependencies == nil || len(m.Dependencies.Resources) == 0 {
		return nil
	}
	ref := Ref{ID: m.Metadata.ID, Version: m.Metadata.Version}

	out := make([]clierr.Detail, 0, len(m.Dependencies.Resources))
	for _, dep := range m.Dependencies.Resources {
		if problem := matchResource(cfg, dep, m.Metadata.ID); problem != "" {
			out = append(out, clierr.Detail{
				Key:   ref.String(),
				Value: "需要 kind: " + dep.Kind + "、engine: " + dep.Engine + "（" + problem + "）",
			})
		}
	}
	return out
}

// resourceHints 给出三条出路，与 matchResource 报出的三种明细一一对应。
//
// 从前只有两条通用建议（"去 resources 声明它" + "去 bindings 加一行"），
// 而三种明细里有一种是 engine 拼法不同——那时第一条是**误导**：他明明声明了。
// 一条照着做不管用的建议，比不给建议更浪费时间。
func resourceHints(componentID string, extra ...string) []string {
	bind := "声明了但没绑 → 在该资源的 bindings 中加一行 componentId"
	if componentID != "" {
		bind += ": " + componentID
	}
	return append([]string{
		"没声明这一类资源 → 在 brickkit.yaml → resources 中加一条",
		"engine 写的不一样 → 改两处中的一处让它们逐字相同" +
			"（平台不认别名：postgres 与 postgresql 是两个不同的值，006 §4.4）",
		bind,
	}, extra...)
}

// matchResource 检查这条资源依赖有没有被满足；满足时返回空串，否则返回**具体**哪儿不对。
//
// # 三种不满足，指向配置里三个不同的地方
//
//	这一类资源压根没声明     去 resources 加一条
//	声明了、engine 对不上    去改那两个词里的一个
//	声明了、没绑给这个组件   去 bindings 加一行
//
// 从前只区分前两种，而且"engine 对不上"被归进了第一种——因为 declared 只在
// kind 与 engine **都**对时才赋值。于是使用者会看到一句**假话**：
//
//	demo/hello@1.0.0：需要 kind: database、engine: postgresql
//	                  （brickkit.yaml 的 resources 中未声明）
//
// 可他明明声明了 pg-main，也把 demo/hello 绑上去了——只是他写的是 postgres。
// 那句话会让他去翻 sources 与 resources 找一个"没声明"的东西，
// 而问题只在 engine 那个词上。
//
// # 为什么 engine 值得参与匹配
//
// 006 §2.2 从前说"engine 是自由字符串，平台对它一无所知——只是写给人看的"，
// 那与这里的行为直接矛盾（也与 §4.4 矛盾）。真相是：**engine 不参与决定注入
// 哪组变量**（那由 kind 决定），但它**参与匹配**——项目里同时有 postgres 与
// mysql 时，它是平台唯一能看出"组件要的和管理员绑的不是同一样东西"的依据。
// 而绑定是管理员显式写下的，这个不一致平台看得见，就不该放过去。
//
// 代价是两个人写的两份文件里那个词必须逐字相同。所以报错必须把两个词都摆出来
// ——平台不认别名，postgres 与 postgresql 在它眼里就是两个不同的值。
func matchResource(cfg *config.Config, dep manifest.ResourceDep, componentID string) string {
	if cfg == nil {
		return "brickkit.yaml 的 resources 中未声明"
	}

	// 同 kind 但 engine 不同的那些：留着，报错时要点名
	var otherEngines []config.Resource
	var declaredSameEngine string

	for _, res := range cfg.Resources {
		if res.Kind != dep.Kind {
			continue
		}
		if res.Engine != dep.Engine {
			otherEngines = append(otherEngines, res)
			continue
		}
		if declaredSameEngine == "" {
			declaredSameEngine = res.ID
		}
		for _, b := range res.Bindings {
			if b.ComponentID == componentID {
				return "" // 满足
			}
		}
	}

	if declaredSameEngine != "" {
		return "资源 " + declaredSameEngine + " 已声明，但未绑定给该组件"
	}
	if len(otherEngines) > 0 {
		return engineMismatch(otherEngines, dep, componentID)
	}
	return "brickkit.yaml 的 resources 中未声明"
}

// engineMismatch 说清楚"这一类资源有，但 engine 那个词对不上"。
//
// 已经绑给这个组件的那个优先点名——那是最常见的情形（管理员绑对了，
// 只是拼法不同），也最需要一眼看出问题在哪。
func engineMismatch(candidates []config.Resource, dep manifest.ResourceDep, componentID string) string {
	pick := candidates[0]
	boundToMe := false
	for _, res := range candidates {
		for _, b := range res.Bindings {
			if b.ComponentID == componentID {
				pick, boundToMe = res, true
				break
			}
		}
		if boundToMe {
			break
		}
	}

	who := "资源 " + pick.ID
	if boundToMe {
		who += " 已经绑给它了"
	} else {
		who += " 是同一类资源"
	}
	return who + "，但它的 engine 写的是 " + pick.Engine
}

// ============================================================
// 工具
// ============================================================

func containsRef(refs []Ref, ref Ref) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}
