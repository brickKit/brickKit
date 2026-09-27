package install_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/install"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// planUpgrade 按 CLI 的做法建新旧两个世界（默认版本 + 外壳编进的成员的闭包），再规划升级。
func planUpgrade(t *testing.T, p *project.Project, cat stub, moves ...install.Move) (*install.Plan, error) {
	t.Helper()
	r := resolver.New(cat)
	oldGraph, err := install.ResolveWorld(context.Background(), r, install.Defaults(p, nil))
	require.NoError(t, err)
	newGraph, err := install.ResolveWorld(context.Background(), r, install.Defaults(p, moves))
	require.NoError(t, err)
	return install.PlanUpgrade(p, oldGraph, newGraph, moves)
}

func mv(id, from, to string) install.Move { return install.Move{ID: id, From: from, To: to} }

// 默认版本 1.0.0 → 2.0.0：brickkit.yaml 那一行原地改版本，部署条目（裸 ID）不动，配置迁移。
func TestPlanUpgradeMovesDefault(t *testing.T) {
	p := proj(t, "  - {id: erp/api, version: 1.0.0}", "  - id: erp/api")
	cat := catalog(withSchema(mf("erp/api@1.0.0")), withSchema(mf("erp/api@2.0.0")))
	plan, err := planUpgrade(t, p, cat, mv("erp/api", "1.0.0", "2.0.0"))
	require.NoError(t, err)

	assert.Equal(t, []install.Move{mv("erp/api", "1.0.0", "2.0.0")}, plan.ChangeVersions)
	assert.Empty(t, plan.AddLines)
	assert.Empty(t, plan.RemoveLines)
	assert.Empty(t, plan.RenameEntries)
	require.Len(t, plan.MigrateConfigs, 1)
	m := plan.MigrateConfigs[0]
	assert.Equal(t, install.ConfigRef{ID: "erp/api", Version: "1.0.0"}, m.Source)
	assert.Equal(t, install.ConfigRef{ID: "erp/api", Version: "2.0.0"}, m.Target)
	assert.Equal(t, "1.0.0", m.From)
	assert.Equal(t, "2.0.0", m.To)
	assert.NotNil(t, m.OldSchema)
	assert.Equal(t, []install.ConfigRef{{ID: "erp/api", Version: "1.0.0"}}, plan.ArchiveConfigs, "旧版本没人要了：配置归档")
}

// 附录 A20：还有组件依赖旧版本——旧版本留下（requiredBy），条目 id@1.0.0，配置改名成带版本号的文件。
func TestPlanUpgradeKeepsOldVersionForDependent(t *testing.T) {
	p := proj(t, `  - {id: erp/api, version: 1.0.0}
  - {id: erp/old, version: 1.0.0}`, `  - id: erp/api
  - id: erp/old`)
	cat := catalog(withSchema(mf("erp/api@1.0.0")), withSchema(mf("erp/api@2.0.0")), mf("erp/old@1.0.0", "erp/api@1.0.0"))
	plan, err := planUpgrade(t, p, cat, mv("erp/api", "1.0.0", "2.0.0"))
	require.NoError(t, err)

	assert.Equal(t, []install.Line{{ID: "erp/api", Version: "1.0.0", RequiredBy: []string{"erp/old"}}}, plan.AddLines)
	assert.Equal(t, []install.Move{mv("erp/api", "1.0.0", "2.0.0")}, plan.ChangeVersions)
	assert.Equal(t, []install.Entry{{ID: "erp/api@1.0.0"}}, plan.AddEntries)
	assert.Equal(t, []install.ConfigRef{{ID: "erp/api", Version: "1.0.0"}}, plan.DemoteConfigs, "无版本号文件改名成 erp-api@1.0.0.yaml")
	require.Len(t, plan.MigrateConfigs, 1)
	assert.Equal(t, install.ConfigRef{ID: "erp/api", Version: "1.0.0", Versioned: true}, plan.MigrateConfigs[0].Source)
	assert.Empty(t, plan.ArchiveConfigs)
}

// 新版本原本就是兼容版本（带 requiredBy 的行）：它转正，旧默认版本没人要了就移除。
func TestPlanUpgradePromotesExistingCompatibilityLine(t *testing.T) {
	p := projFiles(t, `  - {id: erp/api, version: 1.0.0}
  - {id: erp/api, version: 2.0.0, requiredBy: [erp/new]}
  - {id: erp/new, version: 1.0.0}`, `  - id: erp/api
  - id: erp/api@2.0.0
  - id: erp/new`, projecttest.Files{"config/erp-api@2.0.0.yaml": "DB_HOST: pg2\n"})
	cat := catalog(withSchema(mf("erp/api@1.0.0")), withSchema(mf("erp/api@2.0.0")), mf("erp/new@1.0.0", "erp/api@2.0.0"))
	plan, err := planUpgrade(t, p, cat, mv("erp/api", "1.0.0", "2.0.0"))
	require.NoError(t, err)

	assert.Equal(t, []resolver.Ref{{ID: "erp/api", Version: "1.0.0"}}, plan.RemoveLines)
	assert.Equal(t, []install.Line{{ID: "erp/api", Version: "2.0.0"}}, plan.SetRequiredBy)
	assert.Equal(t, []string{"erp/api"}, plan.RemoveEntries, "旧默认版本的裸条目删掉")
	assert.Equal(t, []install.Rename{{From: "erp/api@2.0.0", To: "erp/api"}}, plan.RenameEntries)
	assert.Equal(t, []install.ConfigRef{{ID: "erp/api", Version: "1.0.0"}}, plan.ArchiveConfigs)
	assert.Equal(t, []install.ConfigRef{{ID: "erp/api", Version: "2.0.0", Versioned: true}}, plan.RenameConfigs)
	assert.Empty(t, plan.MigrateConfigs, "2.0.0 自己的配置已经在了")
}

// 新版本多了依赖：像 add 一样加进来。
func TestPlanUpgradeAddsNewDependencies(t *testing.T) {
	p := proj(t, "  - {id: erp/api, version: 1.0.0}", "  - id: erp/api")
	cat := catalog(mf("erp/api@1.0.0"), mf("erp/api@2.0.0", "erp/cache@1.0.0"), mf("erp/cache@1.0.0"))
	plan, err := planUpgrade(t, p, cat, mv("erp/api", "1.0.0", "2.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []install.Line{{ID: "erp/cache", Version: "1.0.0"}}, plan.AddLines)
	assert.Equal(t, []install.Entry{{ID: "erp/cache"}}, plan.AddEntries)
}

// 旧版本的依赖只为它而在（兼容版本）：旧版本走了，它也走。
func TestPlanUpgradeCascadesDroppedDependencies(t *testing.T) {
	p := proj(t, `  - {id: erp/api, version: 1.0.0}
  - {id: erp/db, version: 2.0.0}
  - {id: erp/db, version: 1.0.0, requiredBy: [erp/api]}`, `  - id: erp/api
  - id: erp/db
  - id: erp/db@1.0.0`)
	cat := catalog(mf("erp/api@1.0.0", "erp/db@1.0.0"), mf("erp/api@2.0.0", "erp/db@2.0.0"), mf("erp/db@1.0.0"), mf("erp/db@2.0.0"))
	plan, err := planUpgrade(t, p, cat, mv("erp/api", "1.0.0", "2.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []resolver.Ref{{ID: "erp/db", Version: "1.0.0"}}, plan.RemoveLines)
	assert.Equal(t, []string{"erp/db@1.0.0"}, plan.RemoveEntries)
}

// 裸条目嵌在一个编进了旧版本的外壳下面，旧版本为外壳留下：嵌套条目改成 id@旧版本（留在外壳里），
// 新默认版本在顶层有自己的裸条目——P3f 的核对照样成立。
func TestPlanUpgradeNestedEntryUnderShellCompilingOld(t *testing.T) {
	p := proj(t, `  - {id: erp/shell, version: 1.0.0, kind: shell}
  - {id: erp/a, version: 1.0.0}`, `  - id: erp/shell
    members:
      - id: erp/a`)
	cat := catalog(shellMf("erp/shell@1.0.0", "erp/a@1.0.0"), mf("erp/a@1.0.0"), mf("erp/a@2.0.0"))
	plan, err := planUpgrade(t, p, cat, mv("erp/a", "1.0.0", "2.0.0"))
	require.NoError(t, err)
	assert.Equal(t, []install.Line{{ID: "erp/a", Version: "1.0.0", RequiredBy: []string{"erp/shell"}}}, plan.AddLines)
	assert.Equal(t, []install.Rename{{From: "erp/a", To: "erp/a@1.0.0"}}, plan.RenameEntries)
	assert.Equal(t, []install.Entry{{ID: "erp/a"}}, plan.AddEntries)
}

// 附录 A24：外壳升级展开成成员的版本移动——默认版本是旧外壳编进的那个的成员跟着走；
// 新外壳不再编进的成员挪到顶层独立运行；新成员加进来、嵌在外壳下面。
func TestShellMovesFollowNewMembers(t *testing.T) {
	p := proj(t, `  - {id: erp/shell, version: 1.0.0, kind: shell}
  - {id: erp/a, version: 1.0.0}
  - {id: erp/b, version: 1.0.0}`, `  - id: erp/shell
    members:
      - id: erp/a
      - id: erp/b`)
	s1 := shellMf("erp/shell@1.0.0", "erp/a@1.0.0", "erp/b@1.0.0")
	s2 := shellMf("erp/shell@2.0.0", "erp/a@2.0.0", "erp/c@1.0.0")
	cat := catalog(s1, s2, mf("erp/a@1.0.0"), mf("erp/a@2.0.0"), mf("erp/b@1.0.0"), mf("erp/c@1.0.0"))

	moves := append([]install.Move{mv("erp/shell", "1.0.0", "2.0.0")}, install.ShellMoves(p, s1, s2)...)
	assert.Equal(t, []install.Move{mv("erp/shell", "1.0.0", "2.0.0"), mv("erp/a", "1.0.0", "2.0.0")}, moves)

	plan, err := planUpgrade(t, p, cat, moves...)
	require.NoError(t, err)
	assert.ElementsMatch(t, []install.Move{mv("erp/shell", "1.0.0", "2.0.0"), mv("erp/a", "1.0.0", "2.0.0")}, plan.ChangeVersions)
	assert.Equal(t, []string{"erp/b"}, plan.LiftEntries, "erp/b 不在新外壳里：挪到顶层")
	assert.Equal(t, []install.Line{{ID: "erp/c", Version: "1.0.0"}}, plan.AddLines)
	assert.Equal(t, []install.Entry{{ID: "erp/c", Under: "erp/shell"}}, plan.AddEntries)
}

func TestPlanUpgradeRejectsCompatibilityTarget(t *testing.T) {
	p := proj(t, `  - {id: erp/db, version: 2.0.0}
  - {id: erp/db, version: 1.0.0, requiredBy: [erp/old]}
  - {id: erp/old, version: 1.0.0}`, `  - id: erp/db
  - id: erp/db@1.0.0
  - id: erp/old`)
	cat := catalog(mf("erp/db@1.0.0"), mf("erp/db@2.0.0"), mf("erp/db@1.5.0"), mf("erp/old@1.0.0", "erp/db@1.0.0"))
	_, err := install.PlanUpgrade(p, nil, nil, []install.Move{mv("erp/db", "1.0.0", "1.5.0")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/db@1.0.0")
	_ = cat
}
