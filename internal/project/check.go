package project

import (
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// checkCoverage 执行严格一致性校验（提案 §6.3）：brickkit.yaml 的每个组件版本恰好被一个
// 部署条目覆盖（顶层或外壳下面），没有覆盖不到任何组件的条目。
//
// id@version 条目覆盖那个版本；裸 ID 条目只覆盖默认版本（附录 A20）——因依赖而存在的版本
// 必须有自己的条目，不能悄悄继承默认版本的 expose、端口。默认版本已有专属条目时，
// 裸 ID 条目什么也没覆盖，算多余。
func (p *Project) checkCoverage() error {
	declared := map[string]bool{}
	for _, c := range p.Decl.Components {
		declared[c.Ref()] = true
	}

	claimed := map[string]bool{}
	var extra []string
	entries := p.Deploy.All()
	for _, l := range entries {
		id, version := l.Key()
		if version == "" {
			continue
		}
		if declared[refKey(id, version)] {
			claimed[refKey(id, version)] = true
		} else {
			extra = append(extra, l.ID)
		}
	}
	for _, l := range entries {
		id, version := l.Key()
		if version != "" {
			continue
		}
		def, ok := p.Decl.DefaultVersion(id)
		if !ok || claimed[refKey(id, def)] {
			extra = append(extra, l.ID)
			continue
		}
		claimed[refKey(id, def)] = true
	}

	var missing []string
	for _, c := range p.Decl.Components {
		if !claimed[c.Ref()] {
			missing = append(missing, p.displayRef(c.ID, c.Version))
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return p.inconsistencyError(missing, extra)
}

// displayRef 在只有一个版本时只写 ID（与使用者的写法一致），多版本时写全。
func (p *Project) displayRef(id, version string) string {
	if len(p.Decl.Versions(id)) == 1 {
		return id
	}
	return refKey(id, version)
}

func (p *Project) inconsistencyError(missing, extra []string) *clierr.Error {
	local := p.DeploySource == DeployLocal
	message := i18n.T(msgid.ProjectDeployInconsistent, filepath.Base(p.DeployPath))
	if local {
		message = i18n.T(msgid.ProjectLocalStale)
	}
	err := clierr.New(clierr.CodeDeployInconsistent, message).
		WithDetail(i18n.T(msgid.LabelFile), p.DeployPath)
	for _, ref := range missing {
		err = err.WithDetail(i18n.T(msgid.ProjectLabelMissingEntry), ref)
	}
	for _, ref := range extra {
		err = err.WithDetail(i18n.T(msgid.ProjectLabelExtraEntry), ref)
	}
	if local && !p.LocalModeOn {
		// lint 不管开关都查个人文件：开关关着时，它只是"下次打开时会过期"
		return err.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ProjectLocalStaleReasonOff)).
			WithHint(i18n.T(msgid.ProjectHintLocalRefresh), i18n.T(msgid.ProjectHintLocalEdit), i18n.T(msgid.ProjectHintLocalDelete))
	}
	if local {
		return err.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ProjectLocalStaleReason)).
			WithHint(i18n.T(msgid.ProjectHintLocalRefresh), i18n.T(msgid.ProjectHintLocalEdit), i18n.T(msgid.ProjectHintLocalOff))
	}
	return err.WithHint(i18n.T(msgid.ProjectHintDeploySync), i18n.T(msgid.ProjectHintDeployEdit))
}

// checkMembers 校验外壳成员关系（提案 §8.1、§8.4、附录 A21）：成员条目只能嵌在外壳条目下面，
// 外壳不能被收编，一个组件 ID 只进一个外壳（外壳进程里编进的是那一份代码）。
// 成员条目是否对得上 brickkit.yaml 由 checkCoverage 负责（它先跑）。
func (p *Project) checkMembers() error {
	problems := clierr.NewProblemSet(clierr.CodeConfigInvalid,
		i18n.T(msgid.ProblemValidationFailed, filepath.Base(p.DeployPath))).
		WithSource(i18n.T(msgid.LabelFile), p.DeployPath).WithHint(i18n.T(msgid.DeployfileHintFieldReference))
	p.shellOf = map[string]string{}
	hostedBy := map[string]string{}

	for i, entry := range p.Deploy.Components {
		if len(entry.Members) == 0 {
			continue
		}
		shellID, _ := entry.Key()
		field := yamlfile.Indexed("components", i) + ".members"
		if !p.Decl.IsShellID(shellID) {
			problems.Add(field, i18n.T(msgid.ProjectMembersOnNonShell, shellID))
			continue
		}
		for j, m := range entry.Members {
			memberField := yamlfile.Indexed(field, j)
			member, version := m.Key()
			if p.Decl.IsShellID(member) {
				problems.Add(memberField, i18n.T(msgid.ProjectMemberIsShell, member))
				continue
			}
			if version == "" {
				version, _ = p.Decl.DefaultVersion(member)
			}
			if prev, ok := hostedBy[member]; ok && prev != shellID {
				problems.Add(memberField, i18n.T(msgid.ProjectMemberTwoShells, member, prev))
				continue
			}
			hostedBy[member] = shellID
			p.shellOf[refKey(member, version)] = shellID
		}
	}
	return problems.Err()
}

// checkHostPorts 拦下宿主机端口被两个组件版本同时占用（docker / podman 才有宿主机端口）。
//
// 单文件校验按端口种类各查各的（localPort 与 localPort、exposePort 与 exposePort）；
// 一个条目的 localPort 撞上另一个条目的 exposePort 要到这里才看得到——那是两个进程抢同一个
// 端口，docker 要到起第二个容器时才报 bind 失败。
func (p *Project) checkHostPorts() error {
	if p.Deploy.Target == deployfile.TargetK8s {
		return nil
	}
	type claim struct{ ref, field string }
	claims := map[int]claim{}
	for _, c := range p.Decl.Components {
		entry, ok := p.Deploy.Entry(c.ID, c.Version, p.Decl.IsDefault(c.ID, c.Version))
		if !ok {
			continue
		}
		var ports []claim
		var numbers []int
		if entry.Expose && entry.ExposePort != 0 {
			ports, numbers = append(ports, claim{c.Ref(), "exposePort"}), append(numbers, entry.ExposePort)
		}
		if entry.IsBareProcess() && entry.LocalPort != 0 {
			ports, numbers = append(ports, claim{c.Ref(), "localPort"}), append(numbers, entry.LocalPort)
		}
		for i, port := range numbers {
			prev, taken := claims[port]
			if !taken {
				claims[port] = ports[i]
				continue
			}
			return clierr.New(clierr.CodePortConflict, i18n.T(msgid.ProjectHostPortCollision, port)).
				WithDetail(i18n.T(msgid.LabelFile), p.DeployPath).
				WithDetail(i18n.T(msgid.ProjectLabelPortClaim), prev.ref+" ("+prev.field+")").
				WithDetail(i18n.T(msgid.ProjectLabelPortClaim), ports[i].ref+" ("+ports[i].field+")").
				WithHint(i18n.T(msgid.ProjectHintHostPortPerVersion))
		}
	}
	return nil
}
