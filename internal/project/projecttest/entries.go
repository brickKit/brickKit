package projecttest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
)

// Entry 是测试里对一个组件的全部项目侧声明。字段名与旧的 brickkit.yaml 组件条目一致，
// 旧测试可以原样搬过来：部署字段进 deploy 文件，Config 进 config/<id>@<ver>.yaml，
// ServedBy（成员视角的 "外壳@版本"）翻译成外壳条目上的 members 与 kind: shell。
type Entry struct {
	ID, Version            string
	Mode                   string
	LocalPort              int
	Expose                 bool
	ExposePort             int
	Hostname               string
	TLSSecret              string
	Replicas               *int
	ServiceAccountName     string
	Resources              *manifest.Resources
	Labels                 map[string]string
	SkipWaitFor            []string
	StopGracePeriodSeconds int
	Config                 map[string]any
	ServedBy               string
	// Shell 让这个组件即使一个成员都没有也标成 kind: shell。
	Shell bool
	// RequiredBy 写进 brickkit.yaml。同一个 ID 的第一行是默认版本；后面的版本没写时自动补上
	// 项目里另一个组件的 ID（多版本兼容的用例大多不关心是谁依赖它）。
	RequiredBy []string
}

// Spec 是一个测试项目。
type Spec struct {
	Project string
	Target  string
	K8s     *deployfile.K8s
	Entries []Entry
	// Vars 写 config/vars.yaml；DeployVars 写部署文件的 vars:。
	Vars       map[string]any
	DeployVars map[string]any
	// Files 是额外要写的文件（相对项目根），比如 file:// 指向的密钥。
	Files Files
}

// Build 把 Spec 写成三层文件并装载。有组件用 mode: debug 时写 deploy.local.yaml 并打开本地模式
// （debug 只允许出现在那里）。返回的项目 Layout.Root 就是临时目录。
func Build(t testing.TB, spec Spec) *project.Project {
	t.Helper()
	root := t.TempDir()
	Write(t, root, Render(t, spec))
	for _, e := range spec.Entries {
		if e.Mode == deployfile.ModeDebug {
			require.NoError(t, project.SetLocalMode(project.NewLayout(root), true))
			break
		}
	}
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	return p
}

// Render 只生成文件内容，不装载（需要自己控制装载方式的测试用）。
func Render(t testing.TB, spec Spec) Files {
	t.Helper()
	if spec.Project == "" {
		spec.Project = "my-erp"
	}
	if spec.Target == "" {
		spec.Target = deployfile.TargetDocker
	}
	decl := projfile.File{Project: spec.Project}
	deploy := deployfile.File{Target: spec.Target, K8s: spec.K8s}
	files := Files{}
	hasDebug := false
	// 成员条目嵌在外壳条目下面，一律写 id@version
	members := map[string][]deployfile.Entry{}
	seenID := map[string]bool{}
	for _, e := range spec.Entries {
		entry := deployfile.Entry{
			ID: e.ID + "@" + e.Version, Mode: e.Mode, LocalPort: e.LocalPort,
			Expose: e.Expose, ExposePort: e.ExposePort, Hostname: e.Hostname, TLSSecret: e.TLSSecret,
			Replicas: e.Replicas, ServiceAccountName: e.ServiceAccountName,
			Resources: e.Resources, Labels: e.Labels, SkipWaitFor: e.SkipWaitFor,
			StopGracePeriodSeconds: e.StopGracePeriodSeconds,
		}
		if e.ServedBy != "" {
			shell, _, _ := manifest.SplitRef(e.ServedBy)
			members[shell] = append(members[shell], entry)
		}
		c := projfile.Component{ID: e.ID, Version: e.Version, RequiredBy: e.RequiredBy}
		if seenID[e.ID] && len(c.RequiredBy) == 0 {
			c.RequiredBy = []string{otherID(spec, e.ID)}
		}
		seenID[e.ID] = true
		decl.Components = append(decl.Components, c)
		if len(e.Config) > 0 {
			files["config/"+configdir.FileName(e.ID, e.Version)] = mustYAML(t, e.Config)
		}
		hasDebug = hasDebug || e.Mode == deployfile.ModeDebug
	}
	for i := range decl.Components {
		c := &decl.Components[i]
		if _, isShell := members[c.ID]; isShell || spec.Entries[i].Shell {
			c.Kind = projfile.KindShell
		}
	}
	for _, e := range spec.Entries {
		if e.ServedBy != "" {
			continue
		}
		deploy.Components = append(deploy.Components, deployfile.Component{Entry: deployfile.Entry{
			ID: e.ID + "@" + e.Version, Mode: e.Mode, LocalPort: e.LocalPort,
			Expose: e.Expose, ExposePort: e.ExposePort, Hostname: e.Hostname, TLSSecret: e.TLSSecret,
			Replicas: e.Replicas, ServiceAccountName: e.ServiceAccountName,
			Resources: e.Resources, Labels: e.Labels, SkipWaitFor: e.SkipWaitFor,
			StopGracePeriodSeconds: e.StopGracePeriodSeconds,
		}, Members: members[e.ID]})
	}
	deployDoc := mustYAML(t, deploy)
	if len(spec.DeployVars) > 0 {
		deployDoc += mustYAML(t, map[string]any{"vars": spec.DeployVars})
	}
	files["brickkit.yaml"] = mustYAML(t, decl)
	files[project.FileDeploy] = deployDoc
	if hasDebug {
		files[project.FileDeployLocal] = deployDoc
		// 团队文件里不能有 debug：写一份把 debug 换成空 mode 的
		team := deploy
		team.Components = withoutDebug(deploy.Components)
		files[project.FileDeploy] = mustYAML(t, team)
	}
	if spec.Vars != nil {
		files["config/vars.yaml"] = mustYAML(t, spec.Vars)
	}
	for name, content := range spec.Files {
		files[name] = content
	}
	return files
}

// withoutDebug 复制条目（连同外壳下面的成员），把 mode: debug 换成空 mode。
func withoutDebug(components []deployfile.Component) []deployfile.Component {
	strip := func(e deployfile.Entry) deployfile.Entry {
		if e.Mode == deployfile.ModeDebug {
			e.Mode, e.LocalPort = "", 0
		}
		return e
	}
	out := make([]deployfile.Component, len(components))
	for i, c := range components {
		out[i] = deployfile.Component{Entry: strip(c.Entry)}
		for _, m := range c.Members {
			out[i].Members = append(out[i].Members, strip(m))
		}
	}
	return out
}

// otherID 返回 spec 里第一个不是 id 的组件 ID（给自动补的 requiredBy 用）。
func otherID(spec Spec, id string) string {
	for _, e := range spec.Entries {
		if e.ID != id {
			return e.ID
		}
	}
	return id
}

func mustYAML(t testing.TB, v any) string {
	t.Helper()
	data, err := yaml.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

// IntPtr 方便写 Replicas。
func IntPtr(n int) *int { return &n }

// FillShellCapability 给 spec 里被成员指向的外壳 Manifest 补上声明
// （shell.members 列出指向它的成员及其版本）。三处外壳声明一致是生成的前提（shell.Check），
// 大多数用例关心的不是它；manifests 以 "id@version" 为键。
func FillShellCapability(spec Spec, manifests map[string]*manifest.Manifest) {
	for _, e := range spec.Entries {
		m := manifests[e.ServedBy]
		if e.ServedBy == "" || m == nil {
			continue
		}
		if m.Shell == nil {
			m.Shell = &manifest.Shell{}
		}
		if _, ok := m.HostedVersion(e.ID); !ok {
			m.Shell.Members = append(m.Shell.Members, e.ID+"@"+e.Version)
		}
	}
}
