package deployfile

import (
	"regexp"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate 校验整份部署文件；返回的切片是不阻断的警告（某字段在当前 target 下不起作用）。
func (f *File) Validate(role Role) ([]*clierr.Error, error) {
	p := newProblems(f.Source)
	f.validateTarget(p)
	f.validateK8s(p)
	f.validateVarNames(p)
	f.validateComponents(p, role)
	if err := p.Err(); err != nil {
		return nil, err
	}
	return f.targetWarnings(), nil
}

func (f *File) validateTarget(p *clierr.ProblemSet) {
	switch f.Target {
	case "":
		p.Missing("target")
	case TargetDocker, TargetPodman, TargetK8s:
	default:
		p.Add("target", i18n.T(msgid.ProblemMustBeOneOfThree, TargetDocker, TargetPodman, TargetK8s, f.Target))
	}
}

func (f *File) validateK8s(p *clierr.ProblemSet) {
	k := f.K8s
	if k == nil {
		return
	}
	switch k.PodSecurity {
	case "", PodSecurityRestricted:
	default:
		p.Add("k8s.podSecurity", i18n.T(msgid.ConfigPodSecurityOnly, PodSecurityRestricted, k.PodSecurity))
	}
	if k.Namespace != "" {
		if reason := projfile.ProjectNameProblem(k.Namespace); reason != "" {
			p.Add("k8s.namespace", i18n.T(msgid.ConfigNamespaceSameRule, reason))
		}
	}
	validateNetworkPolicy(p, k.NetworkPolicy)
}

func validateNetworkPolicy(p *clierr.ProblemSet, np *NetworkPolicy) {
	if np == nil {
		return
	}
	const base = "k8s.networkPolicy"
	if np.IngressController != nil && np.IngressController.Namespace == "" {
		p.Missing(base + ".ingressController.namespace")
	}
	for i, source := range np.AllowFrom {
		field := yamlfile.Indexed(base+".allowFrom", i)
		if source.Name == "" {
			p.Missing(field + ".name")
		}
		if source.Namespace == "" {
			p.Add(field+".namespace", i18n.T(msgid.ConfigAllowFromNamespaceMissing, entryLabel(source.Name, i)))
		}
		validatePortList(p, field+".ports", source.Ports)
	}
	if np.Egress == nil {
		return
	}
	for i, target := range np.Egress.AllowTo {
		field := yamlfile.Indexed(base+".egress.allowTo", i)
		if target.Name == "" {
			p.Missing(field + ".name")
		}
		label := entryLabel(target.Name, i)
		switch {
		case target.Namespace != "" && target.CIDR != "":
			p.Add(field, i18n.T(msgid.ConfigEgressBothNamespaceAndCIDR, label))
		case target.Namespace == "" && target.CIDR == "":
			p.Add(field, i18n.T(msgid.ConfigEgressNoTarget, label))
		}
		validatePortList(p, field+".ports", target.Ports)
	}
}

func validatePortList(p *clierr.ProblemSet, field string, ports []int) {
	for _, port := range ports {
		if port < MinPort || port > MaxPort {
			p.Add(field, i18n.T(msgid.ConfigPortInvalid, port))
		}
	}
}

func entryLabel(name string, i int) string {
	if name != "" {
		return name
	}
	return i18n.T(msgid.ConfigEntryOrdinal, i+1)
}

func (f *File) validateVarNames(p *clierr.ProblemSet) {
	names := make([]string, 0, len(f.Vars))
	for name := range f.Vars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !varNameRe.MatchString(name) {
			p.Add("vars."+name, i18n.T(msgid.DeployfileVarNameInvalid))
		}
	}
}

func (f *File) validateComponents(p *clierr.ProblemSet, role Role) {
	seen := map[string]int{}
	localPorts := map[int]int{}
	exposePorts := map[int]int{}
	for i, c := range f.Components {
		field := yamlfile.Indexed("components", i)
		id, version := c.Key()

		if c.ID == "" {
			p.Missing(field + ".id")
		} else {
			if reason := manifest.ComponentIDProblem(id); reason != "" {
				p.Add(field+".id", reason)
			}
			if strings.Contains(c.ID, "@") && !manifest.IsExactVersion(version) {
				p.Add(field+".id", i18n.T(msgid.ConfigVersionNotExactRange, version))
			}
			if prev, ok := seen[c.ID]; ok {
				p.Add(field+".id", i18n.T(msgid.DeployfileEntryDuplicate, yamlfile.Indexed("components", prev), c.ID))
			} else {
				seen[c.ID] = i
			}
		}

		f.validateMode(p, field, c, role)
		validatePorts(p, field, i, c, localPorts, exposePorts)
		if c.TLSSecret != "" && !c.Expose {
			p.Add(field+".tlsSecret", i18n.T(msgid.ConfigTLSSecretNeedsExpose))
		}
		if c.Expose && c.Hostname == "" && f.Target == TargetK8s {
			p.Add(field+".hostname", i18n.T(msgid.ConfigHostnameMissing))
		}
		validateReplicas(p, field, c)
		manifest.ValidateLabels(c.Labels, field+".labels", p.Add)
		validateMembers(p, field, id, c.Members)
	}
}

func (f *File) validateMode(p *clierr.ProblemSet, field string, c Component, role Role) {
	switch c.Mode {
	case "", ModeEnabled, ModeDisable, ModeLocal:
	case ModeDebug:
		if role != RoleLocal {
			p.Add(field+".mode", i18n.T(msgid.DeployfileDebugOnlyLocal))
			return
		}
	default:
		p.Add(field+".mode", i18n.T(msgid.DeployfileModeInvalid, c.Mode))
		return
	}
	if c.IsBareProcess() && f.Target == TargetK8s {
		p.Add(field+".mode", i18n.T(msgid.ConfigModeK8sUnsupported, c.Mode))
	}
}

func validatePorts(p *clierr.ProblemSet, field string, index int, c Component, localPorts, exposePorts map[int]int) {
	if c.LocalPort != 0 {
		switch {
		case !c.IsBareProcess():
			p.Add(field+".localPort", i18n.T(msgid.DeployfileLocalPortNeedsMode))
		case c.LocalPort < MinPort || c.LocalPort > MaxPort:
			p.Add(field+".localPort", i18n.T(msgid.ProblemPortOutOfRange, MinPort, MaxPort, c.LocalPort))
		default:
			if prev, ok := localPorts[c.LocalPort]; ok {
				p.Add(field+".localPort", i18n.T(msgid.ConfigLocalPortConflict, yamlfile.Indexed("components", prev), c.LocalPort))
			} else {
				localPorts[c.LocalPort] = index
			}
		}
	}
	if c.ExposePort != 0 {
		switch {
		case !c.Expose:
			p.Add(field+".exposePort", i18n.T(msgid.ConfigExposePortNeedsExpose))
		case c.ExposePort < MinPort || c.ExposePort > MaxPort:
			p.Add(field+".exposePort", i18n.T(msgid.ProblemPortOutOfRange, MinPort, MaxPort, c.ExposePort))
		default:
			if prev, ok := exposePorts[c.ExposePort]; ok {
				p.Add(field+".exposePort", i18n.T(msgid.ConfigExposePortConflict, yamlfile.Indexed("components", prev), c.ExposePort))
			} else {
				exposePorts[c.ExposePort] = index
			}
		}
	}
}

func validateReplicas(p *clierr.ProblemSet, field string, c Component) {
	if c.Replicas == nil {
		return
	}
	if *c.Replicas < 1 {
		p.Add(field+".replicas", i18n.T(msgid.ConfigReplicasTooSmall, *c.Replicas))
		return
	}
	if c.IsBareProcess() {
		p.Add(field+".replicas", i18n.T(msgid.ConfigReplicasWithLocal))
	}
}

func validateMembers(p *clierr.ProblemSet, field, ownID string, members []string) {
	seen := map[string]bool{}
	for i, member := range members {
		memberField := yamlfile.Indexed(field+".members", i)
		switch {
		case member == "":
			p.Missing(memberField)
		case strings.Contains(member, "@"):
			p.Add(memberField, i18n.T(msgid.DeployfileMemberMustBeBareID, member))
		case member == ownID:
			p.Add(memberField, i18n.T(msgid.DeployfileMemberSelf))
		case seen[member]:
			p.Add(memberField, i18n.T(msgid.DeployfileMemberDuplicate, member))
		default:
			if reason := manifest.ComponentIDProblem(member); reason != "" {
				p.Add(memberField, reason)
			}
		}
		seen[member] = true
	}
}

// targetWarnings 提醒"这个字段在当前 target 下不起作用"：不阻断，但绝不静默忽略。
func (f *File) targetWarnings() []*clierr.Error {
	var out []*clierr.Error
	warn := func(field string) {
		out = append(out, clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.DeployfileFieldIgnoredForTarget, field, f.Target)).
			WithDetail(i18n.T(msgid.LabelFile), f.Source))
	}
	if f.Target == TargetK8s {
		for i, c := range f.Components {
			if c.ExposePort != 0 {
				warn(yamlfile.Indexed("components", i) + ".exposePort")
			}
		}
		return out
	}
	if f.K8s != nil {
		warn("k8s")
	}
	for i, c := range f.Components {
		field := yamlfile.Indexed("components", i)
		if c.Replicas != nil {
			warn(field + ".replicas")
		}
		if c.ServiceAccountName != "" {
			warn(field + ".serviceAccountName")
		}
		if c.TLSSecret != "" {
			warn(field + ".tlsSecret")
		}
		if c.Hostname != "" {
			warn(field + ".hostname")
		}
	}
	return out
}
