package cli

// 本文件把测试里沿用的"单文件 brickkit.yaml"写法翻译成三层文件。
//
// 为什么要有它：CLI 的几千行行为测试描述的是"项目里有哪些组件、怎么部署、配了什么"，
// 这些意图没有随三层重构改变，变的只是落在哪个文件里。逐个手改每个测试里的 YAML，
// 既费事又容易在搬运中改坏断言；让测试继续用熟悉的单文件写法、由这里负责拆成
// brickkit.yaml + deploy.yaml（+ deploy.local.yaml）+ config/，改动面最小、也最不容易错。
//
// 翻译规则：
//
//	sources[].id            → sources[].name
//	deploy.target / k8s 字段 → deploy.yaml 的 target 与 k8s:
//	组件的部署字段           → deploy.yaml 的组件条目（id@version）
//	组件的 config           → config/<scope>-<name>@<version>.yaml（键名转成环境变量名）
//	servedBy: 外壳@版本      → 外壳条目的 members + brickkit.yaml 里外壳的 kind: shell
//	mode: debug             → 只进 deploy.local.yaml，并打开本地模式
//	resources               → 丢弃（基础资源已从平台移除）

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
)

type legacySource struct {
	ID        string `yaml:"id"`
	Type      string `yaml:"type"`
	URL       string `yaml:"url"`
	Path      string `yaml:"path"`
	AuthToken string `yaml:"authToken"`
	Enabled   *bool  `yaml:"enabled"`
}

type legacyDeploy struct {
	Target         string `yaml:"target"`
	deployfile.K8s `yaml:",inline"`
}

type legacyComponent struct {
	deployfile.Component `yaml:",inline"`
	Version              string         `yaml:"version"`
	ServedBy             string         `yaml:"servedBy"`
	Config               map[string]any `yaml:"config"`
}

type legacyDoc struct {
	Project    string               `yaml:"project"`
	Deploy     legacyDeploy         `yaml:"deploy"`
	Sources    []legacySource       `yaml:"sources"`
	Components []legacyComponent    `yaml:"components"`
	Installer  *projfile.Installer  `yaml:"installer"`
	Resources  any                  `yaml:"resources"`
	Vars       map[string]yaml.Node `yaml:"vars"`
}

// writeLegacy 把一份旧式单文件配置翻译成三层文件写进 dir，并按有无 mode: debug 开关本地模式。
func writeLegacy(t *testing.T, dir, text string) {
	t.Helper()
	var doc legacyDoc
	require.NoError(t, yaml.Unmarshal([]byte(text), &doc), "测试里的旧式配置写错了：\n%s", text)

	decl := projfile.File{Project: doc.Project, Installer: doc.Installer}
	for _, s := range doc.Sources {
		decl.Sources = append(decl.Sources, projfile.Source{
			Name: s.ID, Type: s.Type, URL: s.URL, Path: s.Path, AuthToken: s.AuthToken, Enabled: s.Enabled,
		})
	}
	target := doc.Deploy.Target
	if target == "" {
		target = deployfile.TargetDocker
	}
	var k8s *deployfile.K8s
	if !reflect.DeepEqual(doc.Deploy.K8s, deployfile.K8s{}) {
		settings := doc.Deploy.K8s
		k8s = &settings
	}

	shells := map[string]bool{}
	for _, c := range doc.Components {
		if c.ServedBy != "" {
			shells[strings.SplitN(c.ServedBy, "@", 2)[0]] = true
		}
	}
	otherID := func(id string) string {
		for _, c := range doc.Components {
			if c.ID != id {
				return c.ID
			}
		}
		return id
	}

	// 成员条目嵌在外壳条目下面；同一个 ID 的第一行是默认版本，后面的版本补上 requiredBy
	teamMembers := map[string][]deployfile.Entry{}
	localMembers := map[string][]deployfile.Entry{}
	var teamTop, localTop []deployfile.Component
	hasDebug := false
	seenID := map[string]bool{}
	layout := project.NewLayout(dir)
	require.NoError(t, os.MkdirAll(layout.ConfigDir(), 0o755))
	for _, c := range doc.Components {
		entry := projfile.Component{ID: c.ID, Version: c.Version}
		if shells[c.ID] {
			entry.Kind = projfile.KindShell
		}
		if seenID[c.ID] {
			entry.RequiredBy = []string{otherID(c.ID)}
		}
		seenID[c.ID] = true
		decl.Components = append(decl.Components, entry)

		d := c.Component
		d.ID = c.ID + "@" + c.Version
		d.Members = nil
		teamEntry := d.Entry
		if d.Mode == deployfile.ModeDebug {
			hasDebug = true
			teamEntry.Mode, teamEntry.LocalPort = "", 0
		}
		if c.ServedBy != "" {
			shell := strings.SplitN(c.ServedBy, "@", 2)[0]
			localMembers[shell] = append(localMembers[shell], d.Entry)
			teamMembers[shell] = append(teamMembers[shell], teamEntry)
		} else {
			localTop = append(localTop, d)
			teamTop = append(teamTop, deployfile.Component{Entry: teamEntry})
		}

		if len(c.Config) > 0 {
			cfg := make(map[string]any, len(c.Config))
			for k, v := range c.Config {
				cfg[legacyEnvKey(k)] = v
			}
			writeYAML(t, filepath.Join(layout.ConfigDir(), configdir.FileName(c.ID, c.Version)), cfg)
		}
	}
	nest := func(top []deployfile.Component, members map[string][]deployfile.Entry) []deployfile.Component {
		for i := range top {
			id, _ := top[i].Key()
			top[i].Members = members[id]
		}
		return top
	}
	team := deployfile.File{Target: target, K8s: k8s, Components: nest(teamTop, teamMembers)}
	local := deployfile.File{Target: target, K8s: k8s, Components: nest(localTop, localMembers)}
	if len(doc.Vars) > 0 {
		team.Vars, local.Vars = doc.Vars, doc.Vars
	}

	writeYAML(t, layout.DeclPath(), decl)
	writeYAML(t, layout.DeployPath(), team)
	if hasDebug {
		writeYAML(t, layout.DeployLocalPath(), local)
	} else {
		_ = os.Remove(layout.DeployLocalPath())
	}
	require.NoError(t, project.SetLocalMode(layout, hasDebug))
}

func writeYAML(t *testing.T, path string, v any) {
	t.Helper()
	data, err := yaml.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

// legacyEnvKey 把旧式 camelCase 配置键转成大写下划线的环境变量名（键名就是
// 变量名，旧测试的断言按旧规则写的是大写形式）。已经是大写形式的原样返回。
func legacyEnvKey(key string) string {
	if manifest.IsEnvName(key) && key == strings.ToUpper(key) {
		return key
	}
	var b strings.Builder
	for i, r := range key {
		switch {
		case r == '-' || r == '.':
			b.WriteByte('_')
		case unicode.IsUpper(r) && i > 0:
			b.WriteByte('_')
			b.WriteRune(r)
		default:
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}

type legacyOverrideEntry struct {
	ID        string                `yaml:"id"`
	Mode      string                `yaml:"mode"`
	LocalPort int                   `yaml:"localPort"`
	Members   []legacyOverrideEntry `yaml:"members"`
}

type legacyOverride struct {
	Target     string                `yaml:"target"`
	Components []legacyOverrideEntry `yaml:"components"`
}

// writeOverride 把旧式 override.yaml 翻译成本地部署文件：以当前 deploy.yaml 为底
// （deploy.local.yaml 是它的完整副本），套上 target 与各组件的 mode / localPort，
// 再打开本地模式。旧 override 按组件 ID 覆盖全部版本，这里也一样。
func (f *projectFixture) writeOverride(t *testing.T, body string) {
	t.Helper()
	var ov legacyOverride
	require.NoError(t, yaml.Unmarshal([]byte(body), &ov))

	layout := project.NewLayout(f.Dir)
	team, _, err := deployfile.ParseFile(layout.DeployPath(), deployfile.RoleTeam)
	require.NoError(t, err)
	local := *team
	local.Components = append([]deployfile.Component(nil), team.Components...)
	if ov.Target != "" {
		local.Target = ov.Target
	}
	setMode := func(entries []deployfile.Entry, e legacyOverrideEntry) {
		for i := range entries {
			if id, _ := entries[i].Key(); id == e.ID && e.Mode != "" {
				entries[i].Mode, entries[i].LocalPort = e.Mode, e.LocalPort
			}
		}
	}
	for i := range local.Components {
		local.Components[i].Members = append([]deployfile.Entry(nil), local.Components[i].Members...)
	}
	var apply func([]legacyOverrideEntry)
	apply = func(entries []legacyOverrideEntry) {
		for _, e := range entries {
			for i := range local.Components {
				if id, _ := local.Components[i].Key(); id == e.ID && e.Mode != "" {
					local.Components[i].Mode, local.Components[i].LocalPort = e.Mode, e.LocalPort
				}
				setMode(local.Components[i].Members, e)
			}
			apply(e.Members)
		}
	}
	apply(ov.Components)
	writeYAML(t, layout.DeployLocalPath(), local)
	require.NoError(t, project.SetLocalMode(layout, true))
}

// project 按默认规则装载项目（与 up 同一条路）。
func (f *projectFixture) project(t *testing.T) *project.Project {
	t.Helper()
	p, err := project.Load(f.Dir, project.LoadOptions{})
	require.NoError(t, err)
	return p
}

// deployText 返回 deploy.yaml 的原文。
func (f *projectFixture) deployText(t *testing.T) string {
	t.Helper()
	return readFile(t, f.Layout.DeployPath())
}

// deployEntry 返回 deploy.yaml 里 id 对应的那一条（裸 id 或 id@version 都认）。
func (f *projectFixture) deployEntry(t *testing.T, id string) deployfile.Entry {
	t.Helper()
	d, _, err := deployfile.ParseFile(f.Layout.DeployPath(), deployfile.RoleTeam)
	require.NoError(t, err)
	for _, l := range d.All() {
		if entryID, _ := l.Key(); entryID == id {
			return l.Entry
		}
	}
	require.Failf(t, "deploy.yaml 里没有这个组件", "%s", id)
	return deployfile.Entry{}
}

// seedInstalled 造出"这个版本以前装过"的现场：Manifest 缓存与产物，正是旧 add 留下的那些。
// 升级检测按缓存里出现过、如今配置里已经没有的版本来认（add 在 P4 重建前不经过它）。
func (f *projectFixture) seedInstalled(t *testing.T, c comp) {
	t.Helper()
	writeTree(t, f.Layout.CachedManifestDir(c.ID, c.Version), map[string]string{project.FileCachedManifest: c.yamlText()})
	// 以前装过、跑过：上次运行记录里有它（版本变更的基线）
	previous, _ := project.ReadLastRun(f.Layout)
	var refs []string
	for id, versions := range previous {
		for _, v := range versions {
			refs = append(refs, id+"@"+v)
		}
	}
	project.WriteLastRun(f.Layout, append(refs, c.ref()))
	for _, a := range c.Artifacts {
		typ, file, _ := strings.Cut(a, ":")
		dir := filepath.Join(f.Layout.ArtifactsDir(), manifest.ServiceName(c.ID, c.Version), typ)
		writeTree(t, dir, map[string]string{file: "// " + file + " of " + c.ref() + "\n"})
	}
}
