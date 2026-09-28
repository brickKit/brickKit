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
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
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

// NewGraph 用给定的节点组一张图（节点按依赖先于依赖方的顺序给出）。给需要在解析结果之上
// 重组节点的调用方用——比如把外壳承载的成员并进外壳（shell.Workloads）。
func NewGraph(nodes []*Node) *Graph {
	g := &Graph{index: make(map[Ref]*Node, len(nodes))}
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, n)
		g.index[n.Ref] = n
	}
	return g
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

// ResolveProject 以 brickkit.yaml 声明的组件为根解析依赖（按声明顺序）。
//
// 这里不看 mode：启停是 cascade 的职责，解析阶段先把依赖图建全。
func (r *Resolver) ResolveProject(ctx context.Context, p *project.Project) (*Graph, error) {
	if p == nil {
		return r.Resolve(ctx)
	}
	roots := make([]Ref, 0, len(p.Decl.Components))
	declared := make(map[Ref]bool, len(p.Decl.Components))
	for _, c := range p.Decl.Components {
		ref := Ref{ID: c.ID, Version: c.Version}
		roots = append(roots, ref)
		declared[ref] = true
	}
	// brickkit.yaml 是锁文件：只在声明过的版本里解析。没声明的依赖没有部署条目、没有配置
	// 文件，替使用者从安装源拉下来运行，等于绕过了三份文件
	scoped := &Resolver{provider: declaredOnly{inner: r.provider, declared: declared}}
	return scoped.Resolve(ctx, roots...)
}

// declaredOnly 只提供 brickkit.yaml 里声明过的组件版本。
type declaredOnly struct {
	inner    Provider
	declared map[Ref]bool
}

// errNotDeclared 标记"这个版本不在 brickkit.yaml 里"：强依赖缺它给出 add 的出路，弱依赖缺它只警告。
var errNotDeclared = errors.New("not declared in brickkit.yaml")

func (d declaredOnly) Manifest(ctx context.Context, id, version string) (*manifest.Manifest, error) {
	ref := Ref{ID: id, Version: version}
	if !d.declared[ref] {
		return nil, clierr.New(clierr.CodeDependencyMissing, i18n.T(msgid.ResolverNotDeclared, ref.String())).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ResolverNotDeclared, ref.String())).
			WithCause(errNotDeclared).WithHint(i18n.T(msgid.ResolverHintAddDeclared, ref.String()))
	}
	return d.inner.Manifest(ctx, id, version)
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
	if errors.Is(cause, errNotDeclared) {
		return clierr.New(clierr.CodeDependencyMissing, i18n.T(msgid.ResolverStrongDependencyMissing)).
			WithDetail(i18n.T(msgid.LabelComponent), dependent.String()).
			WithDetail(i18n.T(msgid.ResolverLabelMissingDependency), missing.String()).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ResolverNotDeclared, missing.String())).
			WithHint(
				i18n.T(msgid.ResolverHintAddDependent, dependent.String()),
				i18n.T(msgid.ResolverHintAddMissing, missing.String()),
			)
	}
	e := clierr.New(clierr.CodeDependencyMissing, i18n.T(msgid.ResolverStrongDependencyMissing)).
		WithDetail(i18n.T(msgid.LabelComponent), dependent.String()).
		WithDetail(i18n.T(msgid.ResolverLabelMissingDependency), missing.String()).
		WithDetail(i18n.T(msgid.LabelReason), reasonOf(cause))

	inner := clierr.As(cause)
	if inner != nil {
		for _, d := range inner.Details {
			// 这三个上面已经写过（值也更贴题），不重复。明细的键是按当前语言
			// 渲染出来的文字，所以也按当前语言的同一份文案来比。
			if d.Key == i18n.T(msgid.LabelComponent) || d.Key == i18n.T(msgid.LabelReason) ||
				d.Key == i18n.T(msgid.SourceLabelWantedVersion) {
				continue
			}
			e = e.WithDetail(d.Key, d.Value)
		}
	}
	if inner != nil && len(inner.Hints) > 0 {
		return e.WithHint(inner.Hints...).WithCause(cause)
	}
	return e.WithHint(
		i18n.T(msgid.ResolverHintCheckSources),
		i18n.T(msgid.ResolverHintCheckPublished),
		i18n.T(msgid.ResolverHintCheckVersion),
	).WithCause(cause)
}

// optionalMissingWarning 逐字对齐 004 §4.5 的弱依赖警告块。
func optionalMissingWarning(dependent, missing Ref, cause error) *clierr.Error {
	return clierr.Warn(clierr.CodeDependencyMissing, i18n.T(msgid.ResolverOptionalDependencyMissing, missing.String())).
		WithDetail(i18n.T(msgid.ResolverLabelAffectedComponent), dependent.String()).
		WithDetail(i18n.T(msgid.LabelReason), reasonOf(cause)).
		WithDetail(i18n.T(msgid.LabelImpact), i18n.T(msgid.ResolverOptionalImpactDetail, manifest.EndpointEnvVar(missing.ID))).
		WithTip(i18n.T(msgid.ResolverOptionalTip))
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

	return clierr.New(clierr.CodeDependencyCycle, i18n.T(msgid.ResolverDependencyCycleDetected)).
		WithDetail(i18n.T(msgid.ResolverLabelCyclePath), strings.Join(cycle, " → ")).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ResolverCycleReasonDetail)).
		WithHint(
			i18n.T(msgid.ResolverHintCheckManifestDeps),
			i18n.T(msgid.ResolverHintMakeOptional),
		)
}

// reasonOf 把安装源的错误压成一行原因。
// 错误的明细键与标题前缀都是按当前语言渲染出来的文字，所以按当前语言的
// 同一份文案去比。
func reasonOf(err error) string {
	e := clierr.As(err)
	if e == nil {
		return ""
	}
	for _, d := range e.Details {
		if d.Key == i18n.T(msgid.LabelReason) {
			return d.Value
		}
	}
	return strings.TrimPrefix(e.Message, i18n.T(msgid.ErrorPrefix))
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
