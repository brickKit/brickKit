package shell

// 本文件核对外壳的三处声明是否说的是同一件事（提案 §8.4、附录 A11）：
//
//	brickkit.yaml  kind: shell          "这是一个外壳"（由 CLI 维护）
//	component.yaml shell.members        "我能承载这些组件"（能力声明）
//	部署文件       members              "这些组件这次确实在我里面"（实际关系）
//
// 三处对不上时生成出来的东西必然是错的——外壳镜像里根本没有成员的代码，或者外壳以为
// 自己不是外壳、从不读 BRICKKIT_SERVED_MEMBERS_CONFIG——而错误要到容器跑起来才露面。
// 所以在生成之前大声失败。

import (
	"strings"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// Check 核对依赖图里每个 brickkit.yaml 组件的外壳声明。只看解析到了 Manifest 的组件。
//
// "成员必须有独立镜像"（提案 §8.1 规则 1）这里不查：Manifest 校验已经要求每个组件
// 写 deployment.image 或 deployment.build，这条规则因此对任何组件都自然成立。
func Check(p *project.Project, graph *resolver.Graph, _ *cascade.Result) error {
	for _, c := range p.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		node := graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		if err := checkKind(ref, c.IsShell(), node.Manifest); err != nil {
			return err
		}
		if !c.IsShell() {
			continue
		}
		for _, written := range p.MembersOf(c.ID) {
			// 能力声明只列组件 ID；成员条目可能写了 id@version（外壳承载的是哪个版本）
			member, _ := written.Key()
			if !node.Manifest.CanHost(member) {
				return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellMemberNotHostable, member, ref.String())).
					WithDetail(i18n.T(msgid.LabelFile), p.DeployPath).
					WithDetail(i18n.T(msgid.ShellLabelCanHost), strings.Join(node.Manifest.Shell.Members, ", ")).
					WithHint(i18n.T(msgid.ShellHintMemberNotHostable))
			}
		}
	}
	return checkSkipWaitFor(p, graph)
}

// checkSkipWaitFor 核对每个 skipWaitFor 写的都是那个组件版本真实的强依赖（附录 A23）：
// 拼错的名字、弱依赖（它本来就不等）、根本不依赖的组件，写了都等于没写——使用者却以为
// 那条等待已经去掉了。只看解析到了 Manifest 的组件。
func checkSkipWaitFor(p *project.Project, graph *resolver.Graph) error {
	problems := clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ShellSkipWaitForInvalid)).
		WithSource(i18n.T(msgid.LabelFile), p.DeployPath)
	for _, l := range p.Deploy.All() {
		if len(l.SkipWaitFor) == 0 {
			continue
		}
		id, version := l.Key()
		if version == "" {
			version, _ = p.Decl.DefaultVersion(id)
		}
		node := graph.Node(resolver.Ref{ID: id, Version: version})
		if node == nil || node.Manifest == nil {
			continue
		}
		required := map[string]bool{}
		var names []string
		for _, dep := range node.Requires {
			required[dep.ID] = true
			names = append(names, dep.ID)
		}
		list := strings.Join(names, ", ")
		if list == "" {
			list = i18n.T(msgid.ShellSkipWaitForNoRequired)
		}
		for i, skipped := range l.SkipWaitFor {
			if !required[skipped] {
				problems.Add(yamlfile.Indexed(l.Field+".skipWaitFor", i),
					i18n.T(msgid.ShellSkipWaitForNotRequired, skipped, node.Ref.String(), list))
			}
		}
	}
	return problems.Err()
}

// checkKind 核对 brickkit.yaml 的 kind: shell 与 component.yaml 的 shell 块。
func checkKind(ref resolver.Ref, declared bool, m *manifest.Manifest) *clierr.Error {
	switch {
	case declared && !m.IsShell():
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellKindWithoutBlock, ref.String())).
			WithHint(i18n.T(msgid.ShellHintRemoveKind))
	case !declared && m.IsShell():
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellBlockWithoutKind, ref.String())).
			WithDetail(i18n.T(msgid.ShellLabelCanHost), strings.Join(m.Shell.Members, ", ")).
			WithHint(i18n.T(msgid.ShellHintAddKind))
	}
	return nil
}
