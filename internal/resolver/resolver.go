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
	"strconv"
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
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i], out[j]) < 0 })
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
// path 是从根到当前节点的解析路径，用于打印循环路径。
func (rs *resolution) visit(ref Ref, path []Ref) (*Node, error) {
	switch rs.state[ref] {
	case stateDone:
		return rs.graph.index[ref], nil
	case stateVisiting:
		return nil, cycleError(path, ref)
	}

	m, err := rs.provider.Manifest(rs.ctx, ref.ID, ref.Version)
	if err != nil {
		return nil, &fetchError{ref: ref, err: err}
	}

	rs.state[ref] = stateVisiting
	path = append(path, ref)

	node := &Node{Ref: ref, Manifest: m}
	for _, d := range dependenciesOf(m) {
		child := Ref{ID: d.ID, Version: d.Version}
		childNode, err := rs.visit(child, path)
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
	rs.graph.index[ref] = node
	// 后序追加：依赖一定排在依赖方之前
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
func missingDependencyError(dependent, missing Ref, cause error) error {
	return clierr.New(clierr.CodeDependencyMissing, "错误：强依赖缺失").
		WithDetail("组件", dependent.String()).
		WithDetail("缺失依赖", missing.String()).
		WithDetail("原因", reasonOf(cause)).
		WithHint(
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
		WithDetail("原因", "组件之间形成了依赖环，无法确定启动顺序").
		WithHint("检查 Manifest 中的依赖声明（dependencies.components）")
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

// ============================================================
// 升级兼容性检查（002 §7.7）
// ============================================================

// UpgradeReport 是升级兼容性检查的结果。
type UpgradeReport struct {
	// Target 是要升级到的版本。
	Target Ref
	// Graph 是新版本的依赖图。
	Graph *Graph
	// NewDependencies 是新版本引入、但 brickkit.yaml 中尚不存在的组件。
	NewDependencies []Ref
	// Warnings 是弱依赖缺失等不阻断的问题。
	Warnings []*clierr.Error
}

// CheckUpgrade 执行 002 §7.7 的升级依赖兼容性检查：
//
//	1 新版本 Manifest 可获取        ❌ 报错阻断
//	2 新版本的强依赖可满足          ❌ 报错阻断
//	3 新版本的资源依赖已绑定        ❌ 报错阻断
//	4 新版本的弱依赖可获取          ⚠️ 警告但继续
//	5 新版本无循环依赖              ❌ 报错阻断
//
// CLI **不检查** API 兼容性、数据库结构兼容性、configSchema 值兼容性（002 §7.7）。
func (r *Resolver) CheckUpgrade(ctx context.Context, cfg *config.Config, target Ref) (*UpgradeReport, error) {
	if cfg == nil || len(cfg.ComponentsByID(target.ID)) == 0 {
		return nil, clierr.Newf(clierr.CodeComponentNotFound, "错误：组件不在项目中：%s", target.ID).
			WithDetail("原因", "brickkit.yaml 的 components 中没有该组件").
			WithHint(
				"确认组件 ID 是否正确",
				"如果是新增组件，请使用 brickkit add "+target.String(),
			)
	}

	// 检查项 1、2、4、5：一次递归解析全部覆盖
	graph, err := r.Resolve(ctx, target)
	if err != nil {
		return nil, err
	}

	// 检查项 3：新版本的资源依赖必须已在 brickkit.yaml 中声明并绑定
	targetNode := graph.Node(target)
	if err := CheckResourceBindings(cfg, targetNode.Manifest); err != nil {
		return nil, err
	}

	return &UpgradeReport{
		Target:          target,
		Graph:           graph,
		NewDependencies: newDependencies(cfg, graph, target),
		Warnings:        graph.Warnings,
	}, nil
}

// newDependencies 列出新版本引入、而 brickkit.yaml 中还没有的组件版本。
func newDependencies(cfg *config.Config, graph *Graph, target Ref) []Ref {
	installed := map[Ref]bool{}
	for _, c := range cfg.Components {
		installed[Ref{ID: c.ID, Version: c.Version}] = true
	}

	var out []Ref
	for _, ref := range graph.Refs() {
		if ref == target || installed[ref] {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// CheckResourceBindings 校验组件声明的资源依赖是否已在 brickkit.yaml 中
// 声明（kind + engine）且绑定给该组件（003 §5.3）。
//
// 一次报出全部未满足的资源依赖，不是遇到第一个就返回。
func CheckResourceBindings(cfg *config.Config, m *manifest.Manifest) error {
	if m == nil || m.Dependencies == nil || len(m.Dependencies.Resources) == 0 {
		return nil
	}
	ref := Ref{ID: m.Metadata.ID, Version: m.Metadata.Version}

	problems := make([]clierr.Detail, 0, len(m.Dependencies.Resources))
	for _, dep := range m.Dependencies.Resources {
		declared, bound := matchResource(cfg, dep, m.Metadata.ID)
		switch {
		case bound:
			continue
		case declared == "":
			problems = append(problems, clierr.Detail{
				Key:   "缺失",
				Value: "kind: " + dep.Kind + "、engine: " + dep.Engine + "（brickkit.yaml 的 resources 中未声明）",
			})
		default:
			problems = append(problems, clierr.Detail{
				Key:   "缺失",
				Value: "kind: " + dep.Kind + "、engine: " + dep.Engine + "（资源 " + declared + " 已声明，但未绑定给该组件）",
			})
		}
	}
	if len(problems) == 0 {
		return nil
	}

	e := clierr.New(clierr.CodeResourceUnbound, "错误：资源依赖未满足").
		WithDetail("组件", ref.String())
	e.Details = append(e.Details, problems...)
	return e.WithHint(
		"在 brickkit.yaml → resources 中声明该资源（kind + engine 必须匹配）",
		"在该资源的 bindings 中加入 componentId: "+m.Metadata.ID,
	)
}

// matchResource 返回匹配该资源依赖的资源 ID，以及是否已绑定给该组件。
func matchResource(cfg *config.Config, dep manifest.ResourceDep, componentID string) (declared string, bound bool) {
	if cfg == nil {
		return "", false
	}
	for _, res := range cfg.Resources {
		if res.Kind != dep.Kind || res.Engine != dep.Engine {
			continue
		}
		if declared == "" {
			declared = res.ID
		}
		for _, b := range res.Bindings {
			if b.ComponentID == componentID {
				return res.ID, true
			}
		}
	}
	return declared, false
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

// compareVersions 比较两个精确版本（major.minor.patch），返回 -1 / 0 / 1。
// 版本号的合法性由 Manifest 校验保证，这里按数字比较，避免 "10.0.0" < "2.0.0" 的字符串陷阱。
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		if aerr != nil || berr != nil {
			return strings.Compare(a, b)
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return len(as) - len(bs)
}
