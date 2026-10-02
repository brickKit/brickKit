package deployfile

import (
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// Validate 校验整份部署文件；返回的切片是不阻断的警告（某字段在当前 target 下不起作用）。
func (f *File) Validate(role Role) ([]*clierr.Error, error) {
	p := newProblems(f.Source)
	f.validateTarget(p)
	f.validateFocus(p, role)
	f.validateK8s(p)
	f.validateVarNames(p)
	f.validateComponents(p, role)
	if err := p.Err(); err != nil {
		return nil, err
	}
	return f.targetWarnings(), nil
}

func (f *File) validateTarget(p *clierr.ProblemSet) {
	switch {
	case f.Target == "":
		p.Missing("target")
	case !slices.Contains(Targets, f.Target):
		p.Add("target", i18n.T(msgid.ProblemMustBeOneOfThree, TargetDocker, TargetPodman, TargetK8s, f.Target))
	}
}

// validateFocus：焦点只写在个人文件里；k8s 上不行（集群里的 Pod 够不着你的机器）；值是组件 ID。
func (f *File) validateFocus(p *clierr.ProblemSet, role Role) {
	if f.Focus == "" {
		return
	}
	switch {
	case role != RoleLocal:
		p.Add("focus", i18n.T(msgid.DeployfileFocusOnlyLocal))
	case manifest.ComponentIDProblem(f.Focus) != "":
		p.Add("focus", i18n.T(msgid.DeployfileFocusInvalid, f.Focus))
	case f.Target == TargetK8s:
		p.Add("focus", i18n.T(msgid.ConfigFocusK8sUnsupported))
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
		if !configdir.IsValidName(name) {
			p.Add("vars."+name, i18n.T(msgid.DeployfileVarNameInvalid))
		}
	}
}

func (f *File) validateComponents(p *clierr.ProblemSet, role Role) {
	seen := map[string]string{}
	localPorts := map[int]string{}
	exposePorts := map[int]string{}
	for _, l := range f.All() {
		c, field := l.Entry, l.Field
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
				p.Add(field+".id", i18n.T(msgid.DeployfileEntryDuplicate, prev, c.ID))
			} else {
				seen[c.ID] = field
			}
		}

		f.validateMode(p, field, c, role)
		f.validatePorts(p, field, c, localPorts, exposePorts)
		if c.TLSSecret != "" && !c.Expose {
			p.Add(field+".tlsSecret", i18n.T(msgid.ConfigTLSSecretNeedsExpose))
		}
		if c.Expose && c.Hostname == "" && f.Target == TargetK8s {
			p.Add(field+".hostname", i18n.T(msgid.ConfigHostnameMissing))
		}
		validateReplicas(p, field, c)
		manifest.ValidateStopGracePeriod(c.StopGracePeriodSeconds, field+".stopGracePeriodSeconds", p.Add)
		manifest.ValidateLabels(c.Labels, field+".labels", p.Add)
		validateSkipWaitFor(p, field, id, c.SkipWaitFor)
	}
	for i, c := range f.Components {
		id, _ := c.Key()
		validateMembers(p, yamlfile.Indexed("components", i), id, c.Members)
	}
}

func (f *File) validateMode(p *clierr.ProblemSet, field string, c Entry, role Role) {
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

// runsAsBareProcess：条目以裸进程运行——写了 local / debug，或者它是焦点组件的裸 ID 条目
// （按 local 跑，见 withFocus），这时 localPort 对它有效。
func (f *File) runsAsBareProcess(c Entry) bool {
	return c.IsBareProcess() || (f.Focus != "" && c.ID == f.Focus)
}

func (f *File) validatePorts(p *clierr.ProblemSet, field string, c Entry, localPorts, exposePorts map[int]string) {
	if c.LocalPort != 0 {
		switch {
		case !f.runsAsBareProcess(c):
			p.Add(field+".localPort", i18n.T(msgid.DeployfileLocalPortNeedsMode))
		case c.LocalPort < MinPort || c.LocalPort > MaxPort:
			p.Add(field+".localPort", i18n.T(msgid.ProblemPortOutOfRange, MinPort, MaxPort, c.LocalPort))
		default:
			if prev, ok := localPorts[c.LocalPort]; ok {
				p.Add(field+".localPort", i18n.T(msgid.ConfigLocalPortConflict, prev, c.LocalPort))
			} else {
				localPorts[c.LocalPort] = field
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
				p.Add(field+".exposePort", i18n.T(msgid.ConfigExposePortConflict, prev, c.ExposePort))
			} else {
				exposePorts[c.ExposePort] = field
			}
		}
	}
}

// validateSkipWaitFor 做 skipWaitFor 的单文件检查：列的是组件 ID（依赖在一个组件里只出现一次，
// 不需要版本）、不重复、不是自己。它是不是真的强依赖要看 Manifest，由 shell.Check 查。
func validateSkipWaitFor(p *clierr.ProblemSet, field, ownID string, ids []string) {
	seen := map[string]bool{}
	for i, id := range ids {
		itemField := yamlfile.Indexed(field+".skipWaitFor", i)
		switch {
		case strings.Contains(id, "@"):
			p.Add(itemField, i18n.T(msgid.DeployfileSkipWaitForVersioned, id))
		case manifest.ComponentIDProblem(id) != "":
			p.Add(itemField, manifest.ComponentIDProblem(id))
		case id == ownID:
			p.Add(itemField, i18n.T(msgid.DeployfileSkipWaitForSelf))
		case seen[id]:
			p.Add(itemField, i18n.T(msgid.DeployfileSkipWaitForDuplicate, id))
		}
		seen[id] = true
	}
}

func validateReplicas(p *clierr.ProblemSet, field string, c Entry) {
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

// validateMembers 做外壳条目下面成员的单文件检查：不能收编自己，一个外壳只承载一个组件 ID
// 的一个版本（外壳进程里只编进了一份代码）。id 格式、重复条目等由 validateComponents 统一检查。
func validateMembers(p *clierr.ProblemSet, field, ownID string, members []Entry) {
	seen := map[string]bool{}
	for i, member := range members {
		if member.ID == "" {
			continue
		}
		memberField := yamlfile.Indexed(field+".members", i) + ".id"
		id, _ := member.Key()
		switch {
		case id == ownID:
			p.Add(memberField, i18n.T(msgid.DeployfileMemberSelf))
		case seen[id]:
			p.Add(memberField, i18n.T(msgid.DeployfileMemberDuplicate, id))
		}
		seen[id] = true
	}
}

// targetWarnings 提醒"这个字段在当前 target 下不起作用"：不阻断，但绝不静默忽略。
//
// 按字段归并：同一个字段被好几个组件写了只报一行，并点名是哪些组件（用条目的 id，
// 不用下标——人要拿它去文件里找）。k8s: 块点名写了的那几个键。
func (f *File) targetWarnings() []*clierr.Error {
	var out []*clierr.Error
	warn := func(field string, components []string) {
		w := clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.DeployfileFieldIgnoredForTarget, field, f.Target)).
			WithDetail(i18n.T(msgid.LabelFile), f.Source)
		if len(components) > 0 {
			w = w.WithDetail(i18n.T(msgid.LabelComponents), strings.Join(components, ", "))
		}
		out = append(out, w)
	}
	type fieldCheck struct {
		name string
		set  func(Entry) bool
	}
	var checks []fieldCheck
	if f.Target == TargetK8s {
		checks = []fieldCheck{
			{"exposePort", func(c Entry) bool { return c.ExposePort != 0 }},
			{"skipWaitFor", func(c Entry) bool { return len(c.SkipWaitFor) > 0 }},
		}
	} else {
		if keys := setKeys(f.K8s); len(keys) > 0 {
			warn(strings.Join(keys, ", "), nil)
		}
		checks = []fieldCheck{
			{"replicas", func(c Entry) bool { return c.Replicas != nil }},
			{"serviceAccountName", func(c Entry) bool { return c.ServiceAccountName != "" }},
			{"tlsSecret", func(c Entry) bool { return c.TLSSecret != "" }},
			{"hostname", func(c Entry) bool { return c.Hostname != "" }},
		}
	}
	for _, check := range checks {
		var ids []string
		for _, l := range f.All() {
			if check.set(l.Entry) {
				ids = append(ids, l.ID)
			}
		}
		if len(ids) > 0 {
			warn(check.name, ids)
		}
	}
	return out
}

// setKeys 列出 k8s: 块里写了的键，形如 k8s.context。
func setKeys(settings *K8s) []string {
	if settings == nil {
		return nil
	}
	var doc yaml.Node
	if err := doc.Encode(settings); err != nil || doc.Kind != yaml.MappingNode {
		return []string{"k8s"}
	}
	var keys []string
	for i := 0; i < len(doc.Content); i += 2 {
		keys = append(keys, "k8s."+doc.Content[i].Value)
	}
	if len(keys) == 0 {
		return []string{"k8s"}
	}
	return keys
}
