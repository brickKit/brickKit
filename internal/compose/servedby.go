package compose

// 本文件实现 servedBy（外壳合并部署，servedBy 设计书）。
//
// local: true 与 servedBy 结构相似（都是"在依赖图里存在但不生成工作
// 负载"），但语义完全独立，实现也刻意不共享代码路径——local: true 是
// 本机调试，servedBy 是代码已经打进另一个外壳镜像，混在一起维护迟早
// 出现"改 local 的逻辑却影响了 servedBy"这种事故。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedComponent 是一个 servedBy 组件：不生成自己的容器/迁移，代码跑
// 在 Shell 那个组件的容器里。
type servedComponent struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    config.Component
	Shell    resolver.Ref
}

// applyShellGroups 把每个外壳分组合并进外壳自己的环境变量/labels，并
// 记下它要挂哪些网络别名（供 componentService 渲染 networks 段用）。
func (p *plan) applyShellGroups(groups []shell.Group) {
	byShell := make(map[resolver.Ref]shell.Group, len(groups))
	for _, g := range groups {
		byShell[g.Shell] = g
	}

	// shell.Resolve 只看 states.Running()：一个 servedBy 成员如果自己被
	// enabled: false 关掉，它压根不出现在 states.Running() 里，于是
	// shell.Resolve 不会为它的外壳产出任何 Group。但外壳本身如果还在跑，
	// BRICKKIT_SERVED_MEMBERS 依旧必须显式写成空字符串，不能让整个变量
	// 消失——"空字符串"（零个成员激活）与"变量不存在"（不受平台管辖）
	// 语义相反，不能合并处理（servedBy 设计书 §7）。这里只在本渲染器内部
	// 兜底一个空 Group，不改 shell.Resolve 的行为——那是与 K8s 渲染器
	// 共用的逻辑，不属于这个包的职责范围。
	referencedShells := map[resolver.Ref]bool{}
	for _, c := range p.cfg.Components {
		if c.ServedBy == "" {
			continue
		}
		if ref, ok := shell.ParseRef(c.ServedBy); ok {
			referencedShells[ref] = true
		}
	}

	for i := range p.components {
		ref := p.components[i].Ref
		g, ok := byShell[ref]
		if !ok {
			if !referencedShells[ref] {
				continue
			}
			g = shell.Group{Shell: ref}
		}
		p.components[i].Env.Env = shell.Apply(p.components[i].Env.Env, g)
		p.components[i].Env.Labels = manifest.MergeLabels(p.components[i].Env.Labels, g.Labels)

		aliases := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			aliases = append(aliases, manifest.ServiceName(m.Ref.ID, m.Ref.Version))
		}
		sort.Strings(aliases)
		p.shellAliases[p.components[i].Service] = aliases
	}
}

// servedMigrationWarnings 提醒"servedBy 组件的迁移由外壳自己负责编排"
// ——责任主体与 local: true 的对应警告（localMigrationWarnings）不同：
// 那边是调试者本人要手动执行，这边是外壳作者的编排责任。
func (p *plan) servedMigrationWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.Migration == nil {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeMigrationSkipped,
			"提示：servedBy 组件的数据库迁移不会自动执行").
			WithDetail("组件", refText(s.Ref)).
			WithDetail("外壳", refText(s.Shell)).
			WithDetail("原因", "servedBy 的组件不生成容器，它的迁移容器也一并跳过").
			WithHint(
				"确保外壳 "+s.Shell.ID+" 自己的启动逻辑覆盖了这个组件的迁移，"+
					"并按各模块真实的依赖顺序执行",
				"迁移命令："+strings.Join(s.Manifest.Migration.Command, " "),
			))
	}
	return out
}

// servedHealthCheckWarnings 提醒"servedBy 组件自己的健康检查不会独立
// 生效"——它没有自己的容器，健康检查完全是外壳实现者自己的责任，平台
// 不做任何聚合、也不替外壳生成任何健康检查逻辑。
func (p *plan) servedHealthCheckWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.HealthCheck.Type == manifest.HealthCheckNone {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			"提示：servedBy 组件自己的健康检查不会独立生效").
			WithDetail("组件", refText(s.Ref)).
			WithDetail("外壳", refText(s.Shell)).
			WithDetail("原因", "它没有自己的容器，健康检查完全是外壳实现者自己的责任，"+
				"平台不做任何聚合、也不替外壳生成任何健康检查逻辑"))
	}
	return out
}

// servedUnsupportedFieldWarnings 提醒"这些字段对 servedBy 组件不生效"。
//
// expose / exposePort / hostname / replicas / resources /
// serviceAccountName 描述的都是"我自己这个容器该怎么部署"——而 servedBy
// 组件没有自己的容器，这些字段天然没有对象可以落地（v1 范围裁剪，见
// 设计书实施记录）。
func (p *plan) servedUnsupportedFieldWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		var fields []string
		if s.Entry.Expose {
			fields = append(fields, "expose")
		}
		if s.Entry.ExposePort != 0 {
			fields = append(fields, "exposePort")
		}
		if s.Entry.Hostname != "" {
			fields = append(fields, "hostname")
		}
		if s.Entry.Replicas != nil {
			fields = append(fields, "replicas")
		}
		if s.Entry.Resources != nil {
			fields = append(fields, "resources")
		}
		if s.Entry.ServiceAccountName != "" {
			fields = append(fields, "serviceAccountName")
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			"提示：servedBy 组件上，"+strings.Join(fields, "/")+" 本次不生效").
			WithDetail("组件", refText(s.Ref)).
			WithDetail("原因", "这些字段描述的是它自己这个容器该怎么部署，而它没有自己的容器"+
				"（代码跑在外壳 "+s.Shell.ID+" 里）").
			WithHint("要单独部署这个组件，去掉它的 servedBy"))
	}
	return out
}
