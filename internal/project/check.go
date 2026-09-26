package project

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// checkCoverage 执行严格一致性校验（提案 §6.3）：brickkit.yaml 的每个组件版本恰好被一个
// 部署条目覆盖，没有覆盖不到任何组件的条目。
func (p *Project) checkCoverage() error {
	declared := map[string]bool{}
	for _, c := range p.Decl.Components {
		declared[c.Ref()] = true
	}

	explicit := map[string]bool{}
	bare := map[string]bool{}
	var extra []string
	for _, entry := range p.Deploy.Components {
		id, version := entry.Key()
		switch {
		case version != "":
			if declared[refKey(id, version)] {
				explicit[refKey(id, version)] = true
			} else {
				extra = append(extra, entry.ID)
			}
		case len(p.Decl.Versions(id)) == 0:
			extra = append(extra, entry.ID)
		default:
			bare[id] = true
		}
	}

	// 一个裸 ID 条目，如果它的每个版本都已有专属条目，它就什么也没覆盖
	for id := range bare {
		coversSomething := false
		for _, version := range p.Decl.Versions(id) {
			if !explicit[refKey(id, version)] {
				coversSomething = true
			}
		}
		if !coversSomething {
			extra = append(extra, id)
		}
	}

	var missing []string
	for _, c := range p.Decl.Components {
		if !explicit[c.Ref()] && !bare[c.ID] {
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
	if local {
		return err.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ProjectLocalStaleReason)).
			WithHint(i18n.T(msgid.ProjectHintLocalRefresh), i18n.T(msgid.ProjectHintLocalEdit), i18n.T(msgid.ProjectHintLocalOff))
	}
	return err.WithHint(i18n.T(msgid.ProjectHintDeploySync), i18n.T(msgid.ProjectHintDeployEdit))
}

// checkMembers 校验外壳成员关系（提案 §8.1、§8.4）：members 只能写在外壳条目上，
// 成员必须已声明、不能是外壳、只能有一个版本、最多属于一个外壳。
func (p *Project) checkMembers() error {
	problems := clierr.NewProblemSet(clierr.CodeConfigInvalid,
		i18n.T(msgid.ProblemValidationFailed, filepath.Base(p.DeployPath))).
		WithSource(i18n.T(msgid.LabelFile), p.DeployPath)
	p.shellOf = map[string]string{}
	p.memberVersion = map[string]string{}

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
		for j, written := range entry.Members {
			memberField := yamlfile.Indexed(field, j)
			member, version, pinned := strings.Cut(written, "@")
			versions := p.Decl.Versions(member)
			switch {
			case len(versions) == 0:
				problems.Add(memberField, i18n.T(msgid.ProjectMemberUndeclared, member))
				continue
			case p.Decl.IsShellID(member):
				problems.Add(memberField, i18n.T(msgid.ProjectMemberIsShell, member))
				continue
			case pinned && !slices.Contains(versions, version):
				problems.Add(memberField, i18n.T(msgid.ProjectMemberVersionUndeclared, member, version))
				continue
			case !pinned && len(versions) > 1:
				// 外壳里只能编进一个版本；平台不猜是哪一个
				problems.Add(memberField, i18n.T(msgid.ProjectMemberWhichVersion,
					member, strings.Join(versions, i18n.T(msgid.ListSeparator)), versions[len(versions)-1]))
				continue
			}
			if !pinned {
				version = versions[0]
			}
			if prev, ok := p.shellOf[member]; ok && prev != shellID {
				problems.Add(memberField, i18n.T(msgid.ProjectMemberTwoShells, member, prev))
				continue
			}
			p.shellOf[member] = shellID
			p.memberVersion[member] = version
		}
	}
	return problems.Err()
}

// checkHostPorts 拦下宿主机端口被两个组件版本同时占用（docker / podman 才有宿主机端口）。
//
// 单文件校验只看得到"两个条目写了同一个端口"；看不到的是一个裸 ID 条目带着 exposePort
// 覆盖了两个版本——那是两个容器抢同一个端口，docker 要到起第二个容器时才报 bind 失败。
func (p *Project) checkHostPorts() error {
	if p.Deploy.Target == deployfile.TargetK8s {
		return nil
	}
	type claim struct{ ref, field string }
	claims := map[int]claim{}
	for _, c := range p.Decl.Components {
		entry, ok := p.Deploy.Entry(c.ID, c.Version)
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
