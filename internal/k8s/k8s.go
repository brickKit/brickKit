// Package k8s 把解析、级联、注入的结果渲染成 Kubernetes 清单（005 §5）。
//
// 与 compose 包是并列关系：同一份注入结果（inject.Result），两种目标各自渲染。
// 它同样是纯函数——进去的是配置与计算结果，出来的是一组文件内容；
// 不碰磁盘、不调 kubectl，那是命令层的事。
//
// 与 Docker 目标的三处根本差别：
//
//	基础资源   K8s 环境里由运维部署（005 §5.1），CLI 不生成资源容器
//	密码       走 Secret + secretKeyRef，绝不明文写进 Deployment（005 §5.6）
//	${VAR}     必须在**生成时**求值：compose 会自己读 .env，kubectl 不会
package k8s

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/deploy"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// Options 是生成选项。
type Options struct {
	// Now 用于文件头的生成时间，测试可注入。
	Now func() time.Time
	// Lookup 解析 ${VAR}；为空时取进程环境变量。
	//
	// 由调用方提供是因为 .env 文件的位置是项目的事，不是渲染器的事。
	Lookup func(name string) (string, bool)
}

// File 是一份生成出来的清单。
type File struct {
	// Path 相对 .brickkit/generated/k8s/，如 deployments/people-basic-1-0-0.yaml。
	Path string
	YAML []byte
}

// Result 是一次生成的产物。
type Result struct {
	// Namespace 是本项目的命名空间：kubectl 的每条命令都要 -n 它。
	Namespace string
	// Files 按路径排序，保证同一份配置每次生成的顺序一致。
	Files []File
	// Databases 是需要使用者预先创建的数据库（006 §9.5）。
	//
	// K8s 环境下基础资源由运维部署，CLI 一行不碰；但"要建哪些库"照样得说清楚。
	Databases []deploy.DatabaseRequirement
	// MigrationJobs 是本次会执行的迁移 Job 名，按启动顺序无关的字典序排列。
	//
	// 命令层要用它清理上一次残留的 Job 并等待本次跑完（005 §6.3）。
	MigrationJobs []string
	// Warnings 是不阻断的问题。
	Warnings []*clierr.Error
}

// Generate 渲染本次要部署的全部 K8s 清单。
func Generate(
	cfg *config.Config, graph *resolver.Graph, states *cascade.Result,
	env *inject.Result, opts Options,
) (*Result, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	p, err := newPlan(cfg, graph, states, env, opts)
	if err != nil {
		return nil, err
	}

	result := &Result{
		Namespace: p.namespace,
		Databases: deploy.Databases(cfg, p.componentIDs()),
		Warnings:  p.warnings,
	}
	now := opts.Now()

	if cfg.Deploy.ShouldCreateNamespace() {
		if err := p.emit(result, cfg, now, "namespace.yaml", p.namespaceDoc()); err != nil {
			return nil, err
		}
	}
	if docs := p.secretDocs(); len(docs) > 0 {
		if err := p.emitAll(result, cfg, now, "secrets/resource-secrets.yaml", docs); err != nil {
			return nil, err
		}
	}
	for _, c := range p.components {
		if err := p.emit(result, cfg, now,
			"deployments/"+c.Service+".yaml", p.deploymentDoc(c)); err != nil {
			return nil, err
		}
		if err := p.emit(result, cfg, now,
			"services/"+c.Service+".yaml", p.serviceDoc(c)); err != nil {
			return nil, err
		}
		if c.Entry.Expose {
			if err := p.emit(result, cfg, now,
				"ingress/"+c.Service+".yaml", p.ingressDoc(c)); err != nil {
				return nil, err
			}
		}
		if c.Manifest.Migration != nil {
			job := MigrationJobName(c.Service)
			if err := p.emit(result, cfg, now,
				"migrations/"+job+".yaml", p.migrationJobDoc(c)); err != nil {
				return nil, err
			}
			result.MigrationJobs = append(result.MigrationJobs, job)
		}
	}

	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	return result, nil
}

// NamespaceOf 返回本项目实际使用的命名空间。
//
// deploy.namespace 优先：组织的命名空间名往往是他们定的，
// 而且只给你这一个命名空间的权限。
func NamespaceOf(cfg *config.Config) string {
	if cfg != nil && cfg.Deploy.Namespace != "" {
		return cfg.Deploy.Namespace
	}
	if cfg == nil {
		return Namespace("")
	}
	return Namespace(cfg.Project)
}

// Namespace 是项目的默认命名空间：brickkit-<项目名>（005 §5.2）。
//
// 与引擎侧的 compose 项目名同源——同一个项目在两种目标下叫同一个名字，
// 换目标时不用重新学一套命名。
func Namespace(project string) string {
	if project == "" {
		// 配置校验保证项目名非空，这里只是不生成一个以 - 结尾的非法命名空间
		return "brickkit"
	}
	return "brickkit-" + project
}

// ============================================================
// 生成计划
// ============================================================

// componentPlan 是一个要渲染成 Deployment 的组件。
type componentPlan struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    config.Component
	Env      inject.Component
}

// plan 是整次生成的计划。
type plan struct {
	cfg       *config.Config
	graph     *resolver.Graph
	namespace string

	// components 按服务名排序。
	components []componentPlan
	// secrets 按 Secret 名排序。
	secrets []secretPlan

	expand   *expander
	warnings []*clierr.Error
}

func newPlan(
	cfg *config.Config, graph *resolver.Graph, states *cascade.Result,
	env *inject.Result, opts Options,
) (*plan, error) {
	p := &plan{
		cfg:       cfg,
		graph:     graph,
		namespace: NamespaceOf(cfg),
		expand:    newExpander(opts.Lookup),
	}

	entries := map[resolver.Ref]config.Component{}
	for _, c := range cfg.Components {
		entries[resolver.Ref{ID: c.ID, Version: c.Version}] = c
	}
	envByRef := map[resolver.Ref]inject.Component{}
	for _, c := range env.Components {
		envByRef[c.Ref] = c
	}

	var locals []resolver.Ref
	for _, ref := range states.Running() {
		node := graph.Node(ref)
		if node == nil {
			continue
		}
		if entries[ref].Local {
			locals = append(locals, ref)
			continue
		}
		p.components = append(p.components, componentPlan{
			Ref:      ref,
			Service:  manifest.ServiceName(ref.ID, ref.Version),
			Manifest: node.Manifest,
			Entry:    entries[ref],
			Env:      envByRef[ref],
		})
	}
	if len(locals) > 0 {
		return nil, localNotSupported(locals)
	}

	sort.Slice(p.components, func(i, j int) bool { return p.components[i].Service < p.components[j].Service })

	if err := p.checkHostnames(); err != nil {
		return nil, err
	}
	if err := p.collectSecrets(); err != nil {
		return nil, err
	}
	p.warnings = append(p.warnings, p.privilegedPortWarnings()...)
	return p, nil
}

// privilegedPortWarnings 提醒"restricted + 特权端口"这个必然起不来的组合。
//
// restricted 级别 drop 掉了全部 capabilities，其中包括 NET_BIND_SERVICE——
// 组件绑不了 1024 以下的端口。Pod 会**建出来**然后一直崩溃重启，
// 而错误信息在容器日志最深处（permission denied），非常难查。
func (p *plan) privilegedPortWarnings() []*clierr.Error {
	if p.cfg.Deploy.PodSecurity != config.PodSecurityRestricted {
		return nil
	}

	var out []*clierr.Error
	for _, c := range p.components {
		ports := []int{c.Manifest.Deployment.Port}
		for _, extra := range c.Manifest.Deployment.ExtraPorts {
			ports = append(ports, extra.Port)
		}
		for _, port := range ports {
			if port >= 1024 {
				continue
			}
			out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
				fmt.Sprintf("组件监听特权端口 %d，但 podSecurity: restricted 下绑不了", port)).
				WithDetail("组件", c.Ref.ID+"@"+c.Ref.Version).
				WithDetailf("端口", "%d", port).
				WithDetail("原因", "restricted 会 drop 掉全部 capabilities，包括 NET_BIND_SERVICE").
				WithHint(
					"让组件监听 1024 以上的端口（对外端口由 Service / Ingress 映射，不必是 80）",
					"或去掉 deploy.podSecurity: restricted",
				))
		}
	}
	return out
}

// localNotSupported 拒绝 local: true + deploy.target: k8s。
//
// local 的语义是"这个组件跑在你的 IDE 里，其他组件通过宿主机地址访问它"——
// 集群里的 Pod 连不到开发者的笔记本。悄悄跳过的后果是依赖方拿到一个指向
// 不存在 Service 的地址，表现成随机的连接超时，很难查。
func localNotSupported(refs []resolver.Ref) error {
	err := clierr.New(clierr.CodeConfigInvalid,
		"错误：local: true 只能在 deploy.target: docker 下使用")
	for _, ref := range refs {
		err = err.WithDetail("组件", ref.ID+"@"+ref.Version+"（local: true）")
	}
	return err.
		WithDetail("原因", "集群里的 Pod 访问不到开发者本机上的进程").
		WithHint(
			"本地调试请把 deploy.target 改成 docker",
			"或去掉这些组件的 local: true，让它们照常部署到集群里",
		)
}

// componentIDs 是本次会跑起来的组件 ID。
func (p *plan) componentIDs() []string {
	out := make([]string, 0, len(p.components))
	for _, c := range p.components {
		out = append(out, c.Ref.ID)
	}
	return out
}

// ============================================================
// 落盘内容
// ============================================================

// emit 把一份文档渲染成文件并追加到结果里。
func (p *plan) emit(result *Result, cfg *config.Config, now time.Time, path string, doc map[string]any) error {
	return p.emitAll(result, cfg, now, path, []map[string]any{doc})
}

// emitAll 把多份文档渲染进同一个文件（用 --- 分隔，K8s 的惯例写法）。
func (p *plan) emitAll(
	result *Result, cfg *config.Config, now time.Time, path string, docs []map[string]any,
) error {
	var b bytes.Buffer
	b.Write(header(cfg, path, now))

	for i, doc := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		body, err := marshal(doc)
		if err != nil {
			return err
		}
		b.Write(body)
	}

	result.Files = append(result.Files, File{Path: path, YAML: b.Bytes()})
	return nil
}

// header 是生成文件的头注释。
//
// 这些文件会被人打开看、被 kubectl 读、被 git 记录，所以要写清楚
// "这是谁生成的、别手改"。
func header(cfg *config.Config, path string, now time.Time) []byte {
	var b bytes.Buffer
	b.WriteString("# ============================================================\n")
	b.WriteString("# 由 BrickKit CLI 自动生成，请勿手动编辑\n")
	b.WriteString("# 手工改动会在下次 brickkit up 时被覆盖；请改 brickkit.yaml\n")
	fmt.Fprintf(&b, "# 生成时间：%s\n", now.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "# 项目：%s\n", cfg.Project)
	fmt.Fprintf(&b, "# 文件：k8s/%s\n", path)
	b.WriteString("# ============================================================\n\n")
	return b.Bytes()
}

// marshal 渲染 YAML。缩进 2 空格，与设计书样例一致。
func marshal(doc map[string]any) ([]byte, error) {
	var b bytes.Buffer
	encoder := yaml.NewEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("渲染 K8s 清单失败：%w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// namespaceDoc 渲染 Namespace（005 §5.2）。
func (p *plan) namespaceDoc() map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   p.namespace,
			"labels": map[string]any{labelProject: p.cfg.Project},
		},
	}
}
