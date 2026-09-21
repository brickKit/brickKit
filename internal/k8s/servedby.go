package k8s

// 本文件实现 servedBy（外壳合并部署）在 K8s 下的渲染。
//
// mode: debug 在 K8s 下完全不支持——这条拒绝在 internal/config/validate.go
// 的解析阶段就挡住了，本文件不需要管，servedBy 是完全独立的代码路径。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// servedPlan 是一个 servedBy 组件：只生成一个指向外壳 Pod 的 Service，
// 不生成 Deployment/Job——它没有自己的工作负载。
type servedPlan struct {
	Ref      resolver.Ref
	Service  string
	Manifest *manifest.Manifest
	Entry    config.Component
	Shell    resolver.Ref
}

// applyShellGroups 把每个外壳分组合并进外壳自己的环境变量/labels。
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
	// 兜底一个空 Group，不改 shell.Resolve 的行为——那是与 Docker 渲染器
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
	}
}

// servedServiceDoc 渲染一个 servedBy 组件的 Service：selector 指向外壳
// 的 Pod（labelApp: 外壳的服务名），而不是它自己——它没有自己的
// Deployment，这个 Service 存在的唯一目的是让它自己的版本化服务名解析
// 到外壳的 Pod（servedBy 设计书 §9）。
func (p *plan) servedServiceDoc(m servedPlan) map[string]any {
	shellService := manifest.ServiceName(m.Shell.ID, m.Shell.Version)
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      m.Service,
			"namespace": p.namespace,
			"labels": map[string]any{
				labelComponent:        containerName(m.Ref.ID),
				labelComponentVersion: m.Ref.Version,
				labelProject:          p.cfg.Project,
			},
			"annotations": map[string]any{annotationComponentID: m.Ref.ID},
		},
		"spec": map[string]any{
			"selector": map[string]any{labelApp: shellService},
			"ports":    servicePorts(m.Manifest),
			"type":     "ClusterIP",
		},
	}
}

// servedMigrationWarnings 与 compose 侧同名函数职责相同——责任主体是
// 外壳作者，不是本机调试者。
func (p *plan) servedMigrationWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.Migration == nil {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeMigrationSkipped,
			i18n.T(msgid.ServedMigrationSkipped)).
			WithDetail(i18n.T(msgid.LabelComponent), s.Ref.String()).
			WithDetail(i18n.T(msgid.LabelShell), s.Shell.String()).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.K8sServedMigrationReasonDetail)).
			WithHint(
				i18n.T(msgid.HintShellCoversMigration, s.Shell.ID),
				i18n.T(msgid.HintMigrationCommand, strings.Join(s.Manifest.Migration.Command, " ")),
			))
	}
	return out
}

func (p *plan) servedHealthCheckWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		if s.Manifest == nil || s.Manifest.HealthCheck.Type == manifest.HealthCheckNone {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.ServedHealthCheckNotIndependent)).
			WithDetail(i18n.T(msgid.LabelComponent), s.Ref.String()).
			WithDetail(i18n.T(msgid.LabelShell), s.Shell.String()).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.K8sServedHealthCheckReasonDetail)))
	}
	return out
}

// labels 原先走的是合并路线，直到真机复现出 prometheus.io/port 这类
// "语义上必然因组件而异"的键必然合并冲突，才改成跟这一批字段同样的
// "警告 + 忽略"（brickKit 反馈：servedBy 的 labels 合并漏了排除规则；
// 见 internal/shell.mergeGroup 的注释）。
func (p *plan) servedUnsupportedFieldWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, s := range p.served {
		var fields []string
		if s.Entry.Expose {
			fields = append(fields, "expose")
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
		if shell.MemberLabels(s.Manifest, s.Entry) != nil {
			fields = append(fields, "labels")
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.ServedFieldsIgnored, strings.Join(fields, "/"))).
			WithDetail(i18n.T(msgid.LabelComponent), s.Ref.String()).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.K8sServedFieldsReasonDetail, s.Shell.ID)).
			WithHint(i18n.T(msgid.HintDropServedBy)))
	}
	return out
}

// shellOf 判断 ref 是不是某个 servedBy 成员，是则返回它指向的外壳 ref。
//
// 供 hardening.go/egress.go 在依赖图上走边时用：一条边的另一端如果是
// servedBy 成员，它没有自己的 Pod，真正的流量目的地是它的外壳。
func (p *plan) shellOf(ref resolver.Ref) (resolver.Ref, bool) {
	for _, s := range p.served {
		if s.Ref == ref {
			return s.Shell, true
		}
	}
	return resolver.Ref{}, false
}

// servedComponentIDs 是 servedBy 组件的组件 ID，供 componentIDs 复用。
func servedComponentIDs(served []servedPlan) []string {
	out := make([]string, 0, len(served))
	for _, s := range served {
		out = append(out, s.Ref.ID)
	}
	sort.Strings(out)
	return out
}
