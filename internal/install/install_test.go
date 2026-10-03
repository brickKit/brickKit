package install_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/install"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// ---- 构造 ----

// mf 造一个组件；deps 写 id@version，前面加 ? 是弱依赖。
func mf(ref string, deps ...string) *manifest.Manifest {
	id, version, _ := manifest.SplitRef(ref)
	m := &manifest.Manifest{
		APIVersion: manifest.APIVersion, Kind: manifest.Kind,
		Metadata:    manifest.Metadata{ID: id, Version: version, Name: id, Description: id},
		Deployment:  manifest.Deployment{Type: manifest.DeploymentTypeContainer, Image: "img/" + id + ":" + version, Port: 8080},
		HealthCheck: manifest.HealthCheck{Type: manifest.HealthCheckNone},
	}
	for _, d := range deps {
		optional := strings.HasPrefix(d, "?")
		depID, depVersion, _ := manifest.SplitRef(strings.TrimPrefix(d, "?"))
		if m.Dependencies == nil {
			m.Dependencies = &manifest.Dependencies{}
		}
		m.Dependencies.Components = append(m.Dependencies.Components,
			manifest.ComponentDep{Ref: depID + "@" + depVersion, ID: depID, Version: depVersion, Optional: optional})
	}
	return m
}

func withSchema(m *manifest.Manifest) *manifest.Manifest {
	m.ConfigSchema = &manifest.ConfigSchema{Type: "object", Properties: map[string]manifest.ConfigProperty{"DB_HOST": {Type: "string"}}}
	return m
}

func shellMf(ref string, members ...string) *manifest.Manifest {
	m := mf(ref)
	m.Shell = &manifest.Shell{Members: members}
	return m
}

type stub map[resolver.Ref]*manifest.Manifest

func (s stub) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	if m, ok := s[resolver.Ref{ID: id, Version: version}]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("no %s@%s", id, version)
}

func catalog(ms ...*manifest.Manifest) stub {
	s := stub{}
	for _, m := range ms {
		s[resolver.Ref{ID: m.Metadata.ID, Version: m.Metadata.Version}] = m
	}
	return s
}

func ref(s string) resolver.Ref {
	id, version, _ := manifest.SplitRef(s)
	return resolver.Ref{ID: id, Version: version}
}

// proj 装载一个项目：decl 是 brickkit.yaml 的 components 部分，deploy 是部署文件的 components 部分。
func proj(t *testing.T, decl, deploy string) *project.Project {
	t.Helper()
	return projecttest.Load(t, projecttest.Files{
		"brickkit.yaml": "project: p\ncomponents:" + orEmpty(decl) + "\n",
		"deploy.yaml":   "target: docker\ncomponents:" + orEmpty(deploy) + "\n",
	})
}

// projFiles 同 proj，另外写进 files（配置文件等）。
func projFiles(t *testing.T, decl, deploy string, files projecttest.Files) *project.Project {
	t.Helper()
	all := projecttest.Files{
		"brickkit.yaml": "project: p\ncomponents:" + orEmpty(decl) + "\n",
		"deploy.yaml":   "target: docker\ncomponents:" + orEmpty(deploy) + "\n",
	}
	for k, v := range files {
		all[k] = v
	}
	return projecttest.Load(t, all)
}

func orEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return " []"
	}
	return "\n" + s
}

// graphFor 以项目里声明的全部组件加上 extra 为根解析依赖图——add 就是这样建图的。
func graphFor(t *testing.T, p *project.Project, cat stub, extra ...string) *resolver.Graph {
	t.Helper()
	var roots []resolver.Ref
	for _, c := range p.Decl.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}
	for _, e := range extra {
		roots = append(roots, ref(e))
	}
	g, err := resolver.New(cat).Resolve(context.Background(), roots...)
	require.NoError(t, err)
	return g
}

func hints(err error) string { return strings.Join(clierr.As(err).Hints, "\n") }

// ---- add ----

func TestPlanAddNewComponentIsDefault(t *testing.T) {
	p := proj(t, "", "")
	cat := catalog(withSchema(mf("erp/api@1.0.0", "erp/db@2.0.0")), mf("erp/db@2.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/api@1.0.0"), ref("erp/api@1.0.0"))
	require.NoError(t, err)

	assert.Equal(t, []install.Line{{ID: "erp/db", Version: "2.0.0"}, {ID: "erp/api", Version: "1.0.0"}}, plan.AddLines,
		"依赖在前；新组件是默认版本（没有 requiredBy）")
	assert.Equal(t, []install.Entry{{ID: "erp/db"}, {ID: "erp/api"}}, plan.AddEntries, "默认版本的部署条目写裸 ID")
	require.Len(t, plan.AddConfigs, 1, "没有 configSchema 的组件不生成配置文件")
	assert.Equal(t, "erp/api", plan.AddConfigs[0].ID)
	assert.False(t, plan.AddConfigs[0].Versioned)
}

// 依赖要的是另一个版本——加进来，写 requiredBy，部署条目带版本，配置文件带版本号。
func TestPlanAddDependencyAtOtherVersionGetsRequiredBy(t *testing.T) {
	p := proj(t, "  - {id: erp/db, version: 2.0.0}", "  - id: erp/db")
	cat := catalog(mf("erp/api@1.0.0", "erp/db@1.0.0"), withSchema(mf("erp/db@1.0.0")), mf("erp/db@2.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/api@1.0.0"), ref("erp/api@1.0.0"))
	require.NoError(t, err)

	assert.Equal(t, []install.Line{
		{ID: "erp/db", Version: "1.0.0", RequiredBy: []string{"erp/api"}},
		{ID: "erp/api", Version: "1.0.0"},
	}, plan.AddLines)
	assert.Equal(t, []install.Entry{{ID: "erp/db@1.0.0"}, {ID: "erp/api"}}, plan.AddEntries)
	require.Len(t, plan.AddConfigs, 1)
	assert.True(t, plan.AddConfigs[0].Versioned)
}

// 已有的兼容版本又多了一个依赖方：追加进 requiredBy；默认版本不写 requiredBy。
func TestPlanAddExistingNonDefaultAppendsDependent(t *testing.T) {
	p := proj(t, `  - {id: erp/db, version: 2.0.0}
  - {id: erp/db, version: 1.0.0, requiredBy: [erp/old]}
  - {id: erp/old, version: 1.0.0}`, `  - id: erp/db
  - id: erp/db@1.0.0
  - id: erp/old`)
	cat := catalog(mf("erp/api@1.0.0", "erp/db@1.0.0", "erp/x@1.0.0"), mf("erp/db@1.0.0"), mf("erp/db@2.0.0"),
		mf("erp/old@1.0.0", "erp/db@1.0.0"), mf("erp/x@1.0.0", "erp/db@2.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/api@1.0.0"), ref("erp/api@1.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []install.Line{{ID: "erp/db", Version: "1.0.0", RequiredBy: []string{"erp/api", "erp/old"}}}, plan.SetRequiredBy)
}

// 用户裁定（2026-09-27）：直接 add 另一个版本不是 add 的事——换默认版本是 upgrade。
func TestPlanAddOtherVersionDirectlyIsAnError(t *testing.T) {
	p := proj(t, "  - {id: erp/api, version: 1.0.0}", "  - id: erp/api")
	cat := catalog(mf("erp/api@1.0.0"), mf("erp/api@2.0.0"))
	_, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/api@2.0.0"), ref("erp/api@2.0.0"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/api@1.0.0")
	assert.Contains(t, hints(err), "brickkit upgrade erp/api@2.0.0")
}

// add 外壳时按它编进的成员版本写三份文件——
// 新成员成为默认版本、嵌在外壳下面写裸 ID；默认版本不同的成员加一行 requiredBy: [外壳]、嵌套条目写 @版本；
// 已在顶层、版本正好是外壳编进的那个的成员挪进外壳。
func TestPlanAddShellNestsDeclaredMembers(t *testing.T) {
	p := proj(t, `  - {id: erp/b, version: 2.0.0}
  - {id: erp/c, version: 1.0.0}`, `  - id: erp/b
  - id: erp/c`)
	cat := catalog(shellMf("erp/shell@1.0.0", "erp/a@1.0.0", "erp/b@1.0.0", "erp/c@1.0.0"),
		mf("erp/a@1.0.0"), mf("erp/b@1.0.0"), mf("erp/b@2.0.0"), mf("erp/c@1.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/shell@1.0.0", "erp/a@1.0.0", "erp/b@1.0.0", "erp/c@1.0.0"), ref("erp/shell@1.0.0"))
	require.NoError(t, err)

	assert.ElementsMatch(t, []install.Line{
		{ID: "erp/shell", Version: "1.0.0", Shell: true},
		{ID: "erp/a", Version: "1.0.0"},
		{ID: "erp/b", Version: "1.0.0", RequiredBy: []string{"erp/shell"}},
	}, plan.AddLines)
	assert.Equal(t, []install.Entry{
		{ID: "erp/shell"},
		{ID: "erp/a", Under: "erp/shell"},
		{ID: "erp/b@1.0.0", Under: "erp/shell"},
	}, plan.AddEntries, "顶层条目在前，外壳条目要先在")
	assert.Equal(t, []install.Entry{{ID: "erp/c", Under: "erp/shell"}}, plan.NestEntries)
}

// add 一个成员，项目里的外壳编进的正是这个版本——直接嵌到外壳下面；
// 外壳编进的是别的版本——放在顶层，说明为什么。
func TestPlanAddMemberJoinsProjectShell(t *testing.T) {
	p := proj(t, "  - {id: erp/shell, version: 1.0.0, kind: shell}", "  - id: erp/shell")
	cat := catalog(shellMf("erp/shell@1.0.0", "erp/a@1.0.0"), mf("erp/a@1.0.0"), mf("erp/a@2.0.0"))

	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/a@1.0.0"), ref("erp/a@1.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []install.Entry{{ID: "erp/a", Under: "erp/shell"}}, plan.AddEntries)
	assert.Equal(t, []install.Line{{ID: "erp/a", Version: "1.0.0"}}, plan.AddLines,
		"它是默认版本：外壳承载默认版本，不需要 requiredBy")

	plan, err = install.PlanAdd(p, graphFor(t, p, cat, "erp/a@2.0.0"), ref("erp/a@2.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []install.Entry{{ID: "erp/a"}}, plan.AddEntries)
	require.Len(t, plan.Notes, 1)
	assert.Contains(t, plan.Notes[0], "erp/a@1.0.0")
	assert.Contains(t, plan.Notes[0], "erp/shell")
}

func TestPlanAddIdempotent(t *testing.T) {
	p := proj(t, "  - {id: erp/api, version: 1.0.0}", "  - id: erp/api")
	cat := catalog(mf("erp/api@1.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/api@1.0.0"), ref("erp/api@1.0.0"))
	require.NoError(t, err)
	assert.True(t, plan.Empty())
}

// 已声明、有 configSchema、却没有配置文件的组件：补上骨架。
func TestPlanAddFillsMissingConfig(t *testing.T) {
	p := proj(t, "  - {id: erp/api, version: 1.0.0}", "  - id: erp/api")
	cat := catalog(withSchema(mf("erp/api@1.0.0")))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/api@1.0.0"), ref("erp/api@1.0.0"))
	require.NoError(t, err)
	require.Len(t, plan.AddConfigs, 1)
	assert.Empty(t, plan.AddLines)
}

func TestPlanAddNoSchemaNoConfig(t *testing.T) {
	p := proj(t, "", "")
	cat := catalog(mf("erp/api@1.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/api@1.0.0"), ref("erp/api@1.0.0"))
	require.NoError(t, err)
	assert.Empty(t, plan.AddConfigs)
}

// ---- remove ----

func TestPlanRemoveBlockedByDependents(t *testing.T) {
	p := proj(t, `  - {id: erp/db, version: 1.0.0}
  - {id: erp/api, version: 1.0.0}
  - {id: erp/web, version: 1.0.0}`, `  - id: erp/db
  - id: erp/api
  - id: erp/web`)
	cat := catalog(mf("erp/db@1.0.0"), mf("erp/api@1.0.0", "erp/db@1.0.0"), mf("erp/web@1.0.0", "?erp/db@1.0.0"))
	_, err := install.PlanRemove(p, graphFor(t, p, cat), ref("erp/db@1.0.0"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/api@1.0.0")
	assert.NotContains(t, err.Error(), "erp/web", "弱依赖方不拦")
}

// 依赖方移除后没人再要的版本一并清掉（配置归档、条目删掉）。
func TestPlanRemoveCascadesRequiredBy(t *testing.T) {
	p := proj(t, `  - {id: erp/db, version: 2.0.0}
  - {id: erp/db, version: 1.0.0, requiredBy: [erp/old]}
  - {id: erp/old, version: 1.0.0}`, `  - id: erp/db
  - id: erp/db@1.0.0
  - id: erp/old`)
	cat := catalog(mf("erp/db@1.0.0"), mf("erp/db@2.0.0"), mf("erp/old@1.0.0", "erp/db@1.0.0"))
	plan, err := install.PlanRemove(p, graphFor(t, p, cat), ref("erp/old@1.0.0"))
	require.NoError(t, err)

	assert.Equal(t, []resolver.Ref{ref("erp/old@1.0.0"), ref("erp/db@1.0.0")}, plan.RemoveLines)
	assert.Equal(t, []string{"erp/old", "erp/db@1.0.0"}, plan.RemoveEntries)
	assert.Equal(t, []install.ConfigRef{{ID: "erp/old", Version: "1.0.0"}, {ID: "erp/db", Version: "1.0.0", Versioned: true}}, plan.ArchiveConfigs)
	assert.Equal(t, plan.RemoveLines, plan.Removed)
}

// 删外壳，成员挪回顶层独立运行；只因外壳而在的成员版本一并移除。
func TestPlanRemoveShellPromotesMembers(t *testing.T) {
	p := proj(t, `  - {id: erp/shell, version: 1.0.0, kind: shell}
  - {id: erp/a, version: 1.0.0}
  - {id: erp/b, version: 2.0.0}
  - {id: erp/b, version: 1.0.0, requiredBy: [erp/shell]}`, `  - id: erp/shell
    members:
      - id: erp/a
      - id: erp/b@1.0.0
  - id: erp/b`)
	cat := catalog(shellMf("erp/shell@1.0.0", "erp/a@1.0.0", "erp/b@1.0.0"), mf("erp/a@1.0.0"), mf("erp/b@1.0.0"), mf("erp/b@2.0.0"))
	plan, err := install.PlanRemove(p, graphFor(t, p, cat), ref("erp/shell@1.0.0"))
	require.NoError(t, err)

	assert.Equal(t, []string{"erp/shell"}, plan.UnnestShells)
	assert.Equal(t, []resolver.Ref{ref("erp/shell@1.0.0"), ref("erp/b@1.0.0")}, plan.RemoveLines)
	assert.Equal(t, []string{"erp/shell", "erp/b@1.0.0"}, plan.RemoveEntries)
}

// 用户裁定（2026-09-27）：删默认版本、只剩一个版本——它转正：去掉 requiredBy，
// 部署条目 id@v → id，配置文件 <base>@v.yaml → <base>.yaml。
func TestPlanRemovePromotesSoleRemainingVersion(t *testing.T) {
	p := proj(t, `  - {id: erp/db, version: 2.0.0}
  - {id: erp/db, version: 1.0.0, requiredBy: [erp/old]}
  - {id: erp/old, version: 1.0.0}`, `  - id: erp/db
  - id: erp/db@1.0.0
  - id: erp/old`)
	cat := catalog(withSchema(mf("erp/db@1.0.0")), mf("erp/db@2.0.0"), mf("erp/old@1.0.0", "erp/db@1.0.0"))
	plan, err := install.PlanRemove(p, graphFor(t, p, cat), ref("erp/db@2.0.0"))
	require.NoError(t, err)

	assert.Equal(t, []resolver.Ref{ref("erp/db@2.0.0")}, plan.RemoveLines)
	assert.Equal(t, []install.Line{{ID: "erp/db", Version: "1.0.0"}}, plan.SetRequiredBy)
	assert.Equal(t, []install.Rename{{From: "erp/db@1.0.0", To: "erp/db"}}, plan.RenameEntries)
	assert.Equal(t, []install.ConfigRef{{ID: "erp/db", Version: "1.0.0", Versioned: true}}, plan.RenameConfigs)
	assert.Equal(t, ref("erp/db@1.0.0"), plan.Promoted)
}

func TestPlanRemoveDefaultAmbiguous(t *testing.T) {
	p := proj(t, `  - {id: erp/db, version: 3.0.0}
  - {id: erp/db, version: 1.0.0, requiredBy: [erp/a]}
  - {id: erp/db, version: 2.0.0, requiredBy: [erp/b]}
  - {id: erp/a, version: 1.0.0}
  - {id: erp/b, version: 1.0.0}`, `  - id: erp/db
  - id: erp/db@1.0.0
  - id: erp/db@2.0.0
  - id: erp/a
  - id: erp/b`)
	cat := catalog(mf("erp/db@1.0.0"), mf("erp/db@2.0.0"), mf("erp/db@3.0.0"),
		mf("erp/a@1.0.0", "erp/db@1.0.0"), mf("erp/b@1.0.0", "erp/db@2.0.0"))
	_, err := install.PlanRemove(p, graphFor(t, p, cat), ref("erp/db@3.0.0"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/db@1.0.0")
	assert.Contains(t, err.Error(), "erp/db@2.0.0")
}

// 只补这次 add 的那个组件的配置：别的组件的配置文件可能是使用者特意删掉的（键都有默认值）。
func TestPlanAddFillsOnlyTargetConfig(t *testing.T) {
	p := proj(t, "  - {id: erp/other, version: 1.0.0}", "  - id: erp/other")
	cat := catalog(withSchema(mf("erp/other@1.0.0")), mf("erp/api@1.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/api@1.0.0"), ref("erp/api@1.0.0"))
	require.NoError(t, err)
	assert.Empty(t, plan.AddConfigs)
}

// 成员被使用者移出外壳独立运行是合法状态——之后 add 一个无关的组件，不能把它塞回去。
// 只在外壳是这次新加的，或 add 的正是这个成员时才挪进外壳。
func TestPlanAddLeavesMembersTakenOutOfTheShell(t *testing.T) {
	p := proj(t, `  - {id: erp/shell, version: 1.0.0, kind: shell}
  - {id: erp/a, version: 1.0.0}`, `  - id: erp/shell
  - id: erp/a`)
	cat := catalog(shellMf("erp/shell@1.0.0", "erp/a@1.0.0"), mf("erp/a@1.0.0"), mf("erp/x@1.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/x@1.0.0"), ref("erp/x@1.0.0"))
	require.NoError(t, err)
	assert.Empty(t, plan.NestEntries)
}

// add 一个新外壳，它编进的成员已经嵌在项目里另一个外壳下面——成员不动（一个版本只能在一个外壳里），
// 说一声：加外壳的人以为成员会由它承载。
func TestPlanAddShellNotesMemberNestedUnderAnotherShell(t *testing.T) {
	p := proj(t, `  - {id: erp/shell, version: 1.0.0, kind: shell}
  - {id: erp/a, version: 1.0.0}`, `  - id: erp/shell
    members:
      - id: erp/a`)
	for _, added := range []string{"erp/shell2", "erp/ashell"} { // ID 排在旧外壳之后、之前都一样
		cat := catalog(shellMf("erp/shell@1.0.0", "erp/a@1.0.0"), shellMf(added+"@1.0.0", "erp/a@1.0.0", "erp/b@1.0.0"),
			mf("erp/a@1.0.0"), mf("erp/b@1.0.0"))
		plan, err := install.PlanAdd(p, graphFor(t, p, cat, added+"@1.0.0", "erp/a@1.0.0", "erp/b@1.0.0"), ref(added+"@1.0.0"))
		require.NoError(t, err)

		assert.Empty(t, plan.NestEntries, added)
		assert.Equal(t, []install.Entry{{ID: added}, {ID: "erp/b", Under: added}}, plan.AddEntries)
		require.Len(t, plan.Notes, 1, added)
		assert.Contains(t, plan.Notes[0], "erp/a@1.0.0")
		assert.Contains(t, plan.Notes[0], "shell erp/shell,")
		assert.Contains(t, plan.Notes[0], "shell "+added+" ")
	}
}

// 成员被移出旧外壳、在顶层独立运行；add 一个也编进了它的新外壳——add 外壳就是决定把成员合进去，挪进新外壳。
func TestPlanAddShellNestsTopLevelMemberAnotherShellAlsoCompiles(t *testing.T) {
	p := proj(t, `  - {id: erp/shell, version: 1.0.0, kind: shell}
  - {id: erp/a, version: 1.0.0}`, `  - id: erp/shell
  - id: erp/a`)
	cat := catalog(shellMf("erp/shell@1.0.0", "erp/a@1.0.0"), shellMf("erp/shell2@1.0.0", "erp/a@1.0.0"), mf("erp/a@1.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/shell2@1.0.0", "erp/a@1.0.0"), ref("erp/shell2@1.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []install.Entry{{ID: "erp/a", Under: "erp/shell2"}}, plan.NestEntries)
	assert.Empty(t, plan.Notes)
}

// 新外壳带进来的新成员，项目里的旧外壳也编进了同一个版本——嵌在这次加的外壳下面。
func TestPlanAddShellHostsItsNewMembersEvenIfAnOlderShellCompilesThem(t *testing.T) {
	p := proj(t, "  - {id: erp/shell, version: 1.0.0, kind: shell}", "  - id: erp/shell")
	cat := catalog(shellMf("erp/shell@1.0.0", "erp/a@1.0.0"), shellMf("erp/shell2@1.0.0", "erp/a@1.0.0"), mf("erp/a@1.0.0"))
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/shell2@1.0.0", "erp/a@1.0.0"), ref("erp/shell2@1.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []install.Entry{{ID: "erp/shell2"}, {ID: "erp/a", Under: "erp/shell2"}}, plan.AddEntries)
	assert.Empty(t, plan.Notes, "旧外壳不是这次加的，没什么要说的")
}

// 一次加进两个外壳，都编进了同一个成员版本——第一个承载它，第二个说一声。
func TestPlanAddTwoShellsCompilingTheSameMember(t *testing.T) {
	cat := catalog(shellMf("erp/s1@1.0.0", "erp/a@1.0.0", "erp/b@1.0.0"), shellMf("erp/s2@1.0.0", "erp/a@1.0.0", "erp/b@1.0.0"),
		mf("erp/a@1.0.0"), mf("erp/b@1.0.0"))
	p := proj(t, "  - {id: erp/b, version: 1.0.0}", "  - id: erp/b")
	plan, err := install.PlanAdd(p, graphFor(t, p, cat, "erp/s1@1.0.0", "erp/a@1.0.0", "erp/s2@1.0.0"),
		ref("erp/s1@1.0.0"), ref("erp/s2@1.0.0"))
	require.NoError(t, err)

	assert.Contains(t, plan.AddEntries, install.Entry{ID: "erp/a", Under: "erp/s1"}, "新成员")
	assert.Equal(t, []install.Entry{{ID: "erp/b", Under: "erp/s1"}}, plan.NestEntries, "已在顶层的成员")
	require.Len(t, plan.Notes, 2)
	for _, note := range plan.Notes {
		assert.Contains(t, note, "erp/s1")
		assert.Contains(t, note, "erp/s2")
	}
}

// requiredBy 点名的组件还有别的版本留着，但留下的版本并不依赖这个兼容版本——它没人要了，一并移除。
func TestPlanRemoveCascadeLooksAtWhatRemainingVersionsNeed(t *testing.T) {
	p := proj(t, `  - {id: erp/api, version: 2.0.0}
  - {id: erp/api, version: 1.0.0, requiredBy: [erp/portal]}
  - {id: erp/portal, version: 2.0.0}
  - {id: erp/portal, version: 1.0.0, requiredBy: [erp/web]}
  - {id: erp/web, version: 1.0.0}`, `  - id: erp/api
  - id: erp/api@1.0.0
  - id: erp/portal
  - id: erp/portal@1.0.0
  - id: erp/web`)
	cat := catalog(mf("erp/api@1.0.0"), mf("erp/api@2.0.0"),
		mf("erp/portal@2.0.0", "erp/api@2.0.0"), mf("erp/portal@1.0.0", "erp/api@1.0.0"),
		mf("erp/web@1.0.0", "erp/portal@1.0.0"))
	plan, err := install.PlanRemove(p, graphFor(t, p, cat), ref("erp/web@1.0.0"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []resolver.Ref{ref("erp/web@1.0.0"), ref("erp/portal@1.0.0"), ref("erp/api@1.0.0")}, plan.Removed)
}

// 连带移除的版本同样要查强依赖方：requiredBy 没写全（手改过）时，不能把别人还在用的版本删掉。
func TestPlanRemoveChecksDependentsOfCascadedVersions(t *testing.T) {
	p := proj(t, `  - {id: erp/db, version: 2.0.0}
  - {id: erp/db, version: 1.0.0, requiredBy: [erp/old]}
  - {id: erp/old, version: 1.0.0}
  - {id: erp/other, version: 1.0.0}`, `  - id: erp/db
  - id: erp/db@1.0.0
  - id: erp/old
  - id: erp/other`)
	cat := catalog(mf("erp/db@1.0.0"), mf("erp/db@2.0.0"),
		mf("erp/old@1.0.0", "erp/db@1.0.0"), mf("erp/other@1.0.0", "erp/db@1.0.0"))
	plan, err := install.PlanRemove(p, graphFor(t, p, cat), ref("erp/old@1.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []resolver.Ref{ref("erp/old@1.0.0")}, plan.Removed, "erp/other 还依赖 db 1.0.0，它留下")
	assert.Equal(t, []install.Line{{ID: "erp/db", Version: "1.0.0", RequiredBy: []string{"erp/other"}}}, plan.SetRequiredBy,
		"requiredBy 改成真正还需要它的组件")
}

// 弱依赖方不拦 remove，但要说一声：它照样运行，只是少了这个依赖（地址不再注入）。
func TestPlanRemoveNotesOptionalDependents(t *testing.T) {
	p := proj(t, `  - {id: erp/db, version: 1.0.0}
  - {id: erp/web, version: 1.0.0}`, `  - id: erp/db
  - id: erp/web`)
	cat := catalog(mf("erp/db@1.0.0"), mf("erp/web@1.0.0", "?erp/db@1.0.0"))
	plan, err := install.PlanRemove(p, graphFor(t, p, cat), ref("erp/db@1.0.0"))
	require.NoError(t, err)
	require.Len(t, plan.Notes, 1)
	assert.Contains(t, plan.Notes[0], "erp/web@1.0.0")
	assert.Contains(t, plan.Notes[0], "erp/db@1.0.0")
}
