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

// 附录 A20/A5：依赖要的是另一个版本——加进来，写 requiredBy，部署条目带版本，配置文件带版本号。
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

// 附录 A24：add 外壳时按它编进的成员版本写三份文件——
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

// §8.5：add 一个成员，项目里的外壳编进的正是这个版本——直接嵌到外壳下面；
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

// 附录 A20：依赖方移除后没人再要的版本一并清掉（配置归档、条目删掉）。
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

// §8.7：删外壳，成员挪回顶层独立运行；只因外壳而在的成员版本一并移除。
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
