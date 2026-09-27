package shell

// 本文件核对外壳的三处声明是否说的是同一件事（提案 §8.4、附录 A11）：
//
//	brickkit.yaml  kind: shell          "这是一个外壳"（由 CLI 维护）
//	component.yaml shell.members        "构建时编进我的是这些成员的这些版本"（附录 A24）
//	部署文件       members              "这些组件这次确实在我里面"（实际关系，不写版本 = 默认版本）
//
// 三处对不上时生成出来的东西必然是错的——外壳镜像里根本没有成员的代码、编进的是另一个
// 版本，或者外壳以为自己不是外壳、从不读 BRICKKIT_SERVED_MEMBERS_CONFIG——而错误要到
// 容器跑起来才露面。所以在生成之前大声失败。平台只核对，不替使用者推导该跑哪个版本。

import (
	"fmt"
	"slices"
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
func Check(p *project.Project, graph *resolver.Graph, states *cascade.Result) error {
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
			member, _ := written.Key()
			if _, ok := node.Manifest.HostedVersion(member); !ok {
				return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ShellMemberNotHostable, member, ref.String())).
					WithDetail(i18n.T(msgid.LabelFile), p.DeployPath).
					WithDetail(i18n.T(msgid.ShellLabelCanHost), strings.Join(node.Manifest.Shell.Members, ", ")).
					WithHint(i18n.T(msgid.ShellHintMemberNotHostable))
			}
		}
	}
	if err := checkMemberVersions(p, graph, states); err != nil {
		return err
	}
	return checkSkipWaitFor(p, graph)
}

// checkMemberVersions 核对这次被外壳承载的每个成员版本，正是外壳 component.yaml 里声明编进去的
// 那个版本（附录 A24）。版本由 brickkit.yaml 与部署文件决定（成员条目不写版本 = 默认版本），
// 外壳镜像里是什么由外壳自己说，两边对不上时外壳进程里跑的代码与平台注入的版本、地址、配置
// 说的不是同一个东西。
//
// 这次没被承载的成员不查（HostOf 为假）：裸进程成员在宿主机上自己跑、外壳没在跑、
// --ignore-shells——外壳这次都不加载它，镜像里编进的是哪个版本与它无关。
func checkMemberVersions(p *project.Project, graph *resolver.Graph, states *cascade.Result) error {
	problems := clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ShellMemberVersionsMismatch)).
		WithSource(i18n.T(msgid.LabelFile), p.DeployPath)
	for _, c := range p.Decl.Components {
		if !c.IsShell() {
			continue
		}
		shellRef := resolver.Ref{ID: c.ID, Version: c.Version}
		node := graph.Node(shellRef)
		if node == nil || node.Manifest == nil {
			continue
		}
		var contains, hosted, keepBoth []string
		for _, written := range p.MembersOf(c.ID) {
			id, version := written.Key()
			if version == "" {
				version, _ = p.Decl.DefaultVersion(id)
			}
			ref := resolver.Ref{ID: id, Version: version}
			declared, ok := node.Manifest.HostedVersion(id)
			if host, hostedNow := states.HostOf(p, ref); !ok || !hostedNow || host != shellRef || declared == version {
				continue
			}
			compiled := resolver.Ref{ID: id, Version: declared}
			l, _ := p.Deploy.EntryAt(id, version, p.Decl.IsDefault(id, version))
			problems.Add(l.Field, i18n.T(msgid.ShellMemberVersionMismatch, shellRef.String(), compiled.String(), ref.String()))
			contains = append(contains, ref.String())
			hosted = append(hosted, id)
			keepBoth = append(keepBoth, keepBothHint(p, c.ID, ref, compiled))
		}
		if len(hosted) == 0 {
			continue
		}
		problems.WithHint(
			i18n.T(msgid.ShellHintUpgradeShell, c.ID, strings.Join(contains, ", ")),
			i18n.T(msgid.ShellHintMoveMemberOut, strings.Join(hosted, ", "), c.ID),
		)
		problems.WithHint(keepBoth...)
	}
	return problems.Err()
}

// keepBothHint 是第三条出路（两个版本都留）的具体改法：照着改完 up 就要能过。
// 成员条目钉到外壳编进的版本；这次承载的版本挪到部署文件顶层独立运行（默认版本写裸 ID）。
// 外壳编进的版本 brickkit.yaml 里已经留着时，部署文件里也已经有它的条目——两个条目对调，
// 否则同一个版本会有两个条目。
func keepBothHint(p *project.Project, shellID string, hosted, compiled resolver.Ref) string {
	pinned := fmt.Sprintf("`- id: %s`", compiled.String())
	standalone := fmt.Sprintf("`- id: %s`", hosted.String())
	if p.Decl.IsDefault(hosted.ID, hosted.Version) {
		standalone = fmt.Sprintf("`- id: %s`", hosted.ID)
	}
	if slices.Contains(p.Decl.Versions(compiled.ID), compiled.Version) {
		if existing, ok := p.Deploy.EntryAt(compiled.ID, compiled.Version, p.Decl.IsDefault(compiled.ID, compiled.Version)); ok {
			return i18n.T(msgid.ShellHintPinDeclaredVersion, shellID, pinned, existing.Field, standalone, hosted.String())
		}
	}
	line := fmt.Sprintf("`- {id: %s, version: %s, requiredBy: [%s]}`", compiled.ID, compiled.Version, shellID)
	return i18n.T(msgid.ShellHintKeepBothVersions, line, shellID, pinned, standalone, hosted.String())
}

// checkSkipWaitFor 核对每个 skipWaitFor 写的都是那个组件版本真实的强依赖（附录 A23）：
// 拼错的名字、弱依赖（它本来就不等）、根本不依赖的组件，写了都等于没写——使用者却以为
// 那条等待已经去掉了。只看解析到了 Manifest 的组件。
func checkSkipWaitFor(p *project.Project, graph *resolver.Graph) error {
	problems := clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ShellSkipWaitForInvalid)).
		WithSource(i18n.T(msgid.LabelFile), p.DeployPath)
	onShell := false
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
				onShell = onShell || p.Decl.IsShellID(id)
			}
		}
	}
	if onShell {
		// 外壳条目上的 skipWaitFor 只管外壳自己的依赖：多半是把成员的依赖写到了外壳上
		problems.WithHint(i18n.T(msgid.ShellHintSkipWaitForOnMember))
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
