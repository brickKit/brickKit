package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// withSchema 给测试组件一份 configSchema：键 → 默认值（空串表示必填、没有默认值）。
func withConfig(c comp, keys ...string) comp {
	c.ConfigSchema = keys
	return c
}

func TestUpgradeSingleToLatest(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.release(comp{ID: "erp/api", Version: "1.2.0"})

	r := g.mustRun(dir, "upgrade", "erp/api")
	assert.Contains(t, r.stdout, "1.0.0 → 1.2.0")
	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/api\n    version: 1.2.0")
	assert.NotContains(t, decl, "1.0.0")
	g.mustRun(dir, "up", "--dry-run")
}

func TestUpgradeToExplicitVersionAndNothingToDo(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/api", Version: "1.1.0"})
	g.release(comp{ID: "erp/api", Version: "2.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")

	g.mustRun(dir, "upgrade", "erp/api@1.1.0")
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "version: 1.1.0")
	r := g.mustRun(dir, "upgrade", "erp/api@1.1.0")
	assert.Contains(t, r.stdout, "up to date")
}

// 计划的"完成标准"：使用者改过 LOG_LEVEL、新版本也改了默认值——upgrade --yes 写重复键；up 以友好的
// 冲突报错拒绝启动；使用者删掉一行和注释之后 up 照常通过。
func TestUpgradeRoundTripConflict(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "LOG_LEVEL:info"))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	cfg := filepath.Join(dir, "config", "erp-api.yaml")
	editFile(t, cfg, "# LOG_LEVEL: ", "LOG_LEVEL: debug  # was: ")
	g.release(withConfig(comp{ID: "erp/api", Version: "2.0.0"}, "LOG_LEVEL:warn"))

	r := g.mustRun(dir, "upgrade", "erp/api", "--yes")
	assert.Contains(t, r.stdout, "config/erp-api.yaml")
	text := readFile(t, cfg)
	assert.Equal(t, 2, strings.Count(text, "\nLOG_LEVEL: "), text)

	r = g.run(dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "LOG_LEVEL")

	// 使用者解决冲突：留下自己的那一行，删掉另一行和说明注释
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "brickkit:conflict proposed") || strings.HasPrefix(line, "# ⚠️") || strings.HasPrefix(line, "# Keep one line") {
			continue
		}
		kept = append(kept, line)
	}
	require.NoError(t, os.WriteFile(cfg, []byte(strings.Join(kept, "\n")), 0o644))
	g.mustRun(dir, "up", "--dry-run")
}

// 还有组件依赖旧版本——旧版本以 requiredBy 留下，条目 id@1.0.0，配置文件改名成带版本号的
// （使用者的旧值还在里面），新的无版本号文件按新版本迁移。
func TestUpgradeKeepsOldVersionForDependent(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "DB_HOST:x"))
	g.release(comp{ID: "erp/old", Version: "1.0.0", Requires: []string{"erp/api@1.0.0"}, Port: 8081})
	dir := g.project()
	g.mustRun(dir, "add", "erp/old@1.0.0")
	editFile(t, filepath.Join(dir, "config", "erp-api.yaml"), "# DB_HOST: ", "DB_HOST: pg.internal  # ")
	g.release(withConfig(comp{ID: "erp/api", Version: "2.0.0"}, "DB_HOST:x"))

	r := g.mustRun(dir, "upgrade", "erp/api@2.0.0")
	assert.Contains(t, r.stdout, "erp/api@1.0.0 runs beside erp/api@2.0.0, a separate deployment with its own config, because erp/old depends on it; "+
		"if one version was meant, upgrade erp/old to versions that depend on erp/api@2.0.0", "FRR-008")
	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/api\n    version: 2.0.0")
	assert.Contains(t, decl, "version: 1.0.0\n    requiredBy: [erp/old]")
	assert.Contains(t, readFile(t, filepath.Join(dir, "deploy.yaml")), "- id: erp/api@1.0.0\n")
	assert.Contains(t, readFile(t, filepath.Join(dir, "config", "erp-api@1.0.0.yaml")), "DB_HOST: pg.internal")
	assert.Contains(t, readFile(t, filepath.Join(dir, "config", "erp-api.yaml")), "DB_HOST: pg.internal")
	g.mustRun(dir, "up", "--dry-run")
}

// 升级外壳就是换成新外壳编进的那一套成员版本；新外壳不再编进的成员挪到顶层独立运行。
func TestUpgradeShellMovesMembers(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "1.0.0", Port: 8082})
	g.release(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0", "erp/b@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/shell@1.0.0")
	g.release(comp{ID: "erp/a", Version: "2.0.0", Port: 8081})
	g.release(comp{ID: "erp/shell", Version: "2.0.0", ShellMembers: []string{"erp/a@2.0.0"}})

	g.mustRun(dir, "upgrade", "erp/shell")
	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/shell\n    version: 2.0.0")
	assert.Contains(t, decl, "id: erp/a\n    version: 2.0.0")
	deploy := readFile(t, filepath.Join(dir, "deploy.yaml"))
	assert.Contains(t, deploy, "  - id: erp/shell\n    members:\n      - id: erp/a\n")
	assert.Contains(t, deploy, "\n  - id: erp/b\n", "erp/b 不在新外壳里：独立运行")
	g.mustRun(dir, "up", "--dry-run")
}

// 两个外壳编进同一组成员、成员分在两个外壳里：升级其中一个外壳只换它承载的成员，别的外壳承载的
// 成员留在原处、原版本，说一声（反馈 FRB-006：之前把成员同时写进两个外壳，升级失败）。
func TestUpgradeShellKeepsMembersHostedByAnotherShell(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "1.0.0", Port: 8082})
	g.release(comp{ID: "erp/core", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0", "erp/b@1.0.0"}})
	g.release(comp{ID: "erp/infra", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0", "erp/b@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/core@1.0.0")
	g.mustRun(dir, "add", "erp/infra@1.0.0")
	deployPath := filepath.Join(dir, "deploy.yaml")
	deploy := readFile(t, deployPath)
	split := strings.Replace(strings.Replace(deploy, "      - id: erp/b\n", "", 1),
		"  - id: erp/infra\n", "  - id: erp/infra\n    members:\n      - id: erp/b\n", 1)
	require.NotEqual(t, deploy, split)
	require.NoError(t, os.WriteFile(deployPath, []byte(split), 0o644))
	g.mustRun(dir, "lint")

	g.release(comp{ID: "erp/a", Version: "2.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "2.0.0", Port: 8082})
	g.release(comp{ID: "erp/core", Version: "2.0.0", ShellMembers: []string{"erp/a@2.0.0", "erp/b@2.0.0"}})
	r := g.mustRun(dir, "upgrade", "erp/core@2.0.0", "--yes")
	assert.Contains(t, r.stdout, "erp/b is nested under shell erp/infra, which contains erp/b@1.0.0: it stays at 1.0.0")

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/a\n    version: 2.0.0")
	assert.Contains(t, decl, "id: erp/b\n    version: 1.0.0")
	assert.NotContains(t, decl, "erp/b\n    version: 2.0.0", "core 不承载 b：它编进的 b@2.0.0 没人跑")
	assert.NotContains(t, decl, "requiredBy", "infra 不承载 a：不为它留 a@1.0.0")
	deploy = readFile(t, deployPath)
	assert.Contains(t, deploy, "  - id: erp/core\n    members:\n      - id: erp/a\n")
	assert.Contains(t, deploy, "  - id: erp/infra\n    members:\n      - id: erp/b\n")
	g.mustRun(dir, "up", "--dry-run")

	// 再升级另一个外壳：它承载的成员跟着它走
	g.release(comp{ID: "erp/infra", Version: "2.0.0", ShellMembers: []string{"erp/a@2.0.0", "erp/b@2.0.0"}})
	r = g.mustRun(dir, "upgrade", "erp/infra@2.0.0", "--yes")
	assert.NotContains(t, r.stdout, "stays at")
	decl = readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/b\n    version: 2.0.0")
	assert.NotContains(t, decl, "version: 1.0.0")
	deploy = readFile(t, deployPath)
	assert.Contains(t, deploy, "  - id: erp/core\n    members:\n      - id: erp/a\n")
	assert.Contains(t, deploy, "  - id: erp/infra\n    members:\n      - id: erp/b\n")
	g.mustRun(dir, "up", "--dry-run")
}

// 使用者把成员移出外壳独立运行；之后升级外壳不把它挪回去、也不换它的版本，说一声。
func TestUpgradeShellLeavesStandaloneMember(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "1.0.0", Port: 8082})
	g.release(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0", "erp/b@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/shell@1.0.0")
	deployPath := filepath.Join(dir, "deploy.yaml")
	deploy := readFile(t, deployPath)
	lifted := strings.Replace(deploy, "      - id: erp/b\n", "", 1) + "  - id: erp/b\n"
	require.NoError(t, os.WriteFile(deployPath, []byte(lifted), 0o644))
	g.mustRun(dir, "lint")

	g.release(comp{ID: "erp/a", Version: "2.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "2.0.0", Port: 8082})
	g.release(comp{ID: "erp/shell", Version: "2.0.0", ShellMembers: []string{"erp/a@2.0.0", "erp/b@2.0.0"}})
	r := g.mustRun(dir, "upgrade", "erp/shell@2.0.0", "--yes")
	assert.Contains(t, r.stdout, "erp/b runs on its own (it is not under the shell in the deploy file): it stays at 1.0.0")

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/a\n    version: 2.0.0")
	assert.Contains(t, decl, "id: erp/b\n    version: 1.0.0")
	assert.NotContains(t, decl, "erp/b\n    version: 2.0.0")
	deploy = readFile(t, deployPath)
	assert.Contains(t, deploy, "  - id: erp/shell\n    members:\n      - id: erp/a\n")
	assert.Contains(t, deploy, "\n  - id: erp/b\n", "b 仍在顶层独立运行")
	g.mustRun(dir, "up", "--dry-run")
}

// 使用者移除了外壳编进的一个成员；升级外壳不把它加回来（它不在项目里也是使用者定的承载位置），
// 说一声；新外壳新编进的成员照常进来。之后 add 它，它嵌在外壳下面（反馈 FRB-007）。
func TestUpgradeShellDoesNotReaddRemovedMember(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "1.0.0", Port: 8082})
	g.release(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0", "erp/b@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/shell@1.0.0")
	g.mustRun(dir, "remove", "erp/b")

	g.release(comp{ID: "erp/a", Version: "2.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "2.0.0", Port: 8082})
	g.release(comp{ID: "erp/c", Version: "1.0.0", Port: 8083})
	g.release(comp{ID: "erp/shell", Version: "2.0.0", ShellMembers: []string{"erp/a@2.0.0", "erp/b@2.0.0", "erp/c@1.0.0"}})
	r := g.mustRun(dir, "upgrade", "erp/shell@2.0.0", "--yes")
	assert.Contains(t, r.stdout, "erp/b is not in the project: it stays out (brickkit add erp/b@2.0.0 to host it)")
	assert.Contains(t, r.stdout, "✅ erp/c@1.0.0 (in shell erp/shell)", "升级加进来的成员也说在哪个外壳里")

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/a\n    version: 2.0.0")
	assert.Contains(t, decl, "id: erp/c\n    version: 1.0.0", "新外壳新编进的成员照常进来")
	assert.NotContains(t, decl, "erp/b")
	deployPath := filepath.Join(dir, "deploy.yaml")
	deploy := readFile(t, deployPath)
	assert.Contains(t, deploy, "  - id: erp/shell\n    members:\n      - id: erp/a\n      - id: erp/c\n")
	assert.NotContains(t, deploy, "erp/b")
	assert.NoFileExists(t, filepath.Join(dir, "config", "erp-b.yaml"))
	g.mustRun(dir, "up", "--dry-run")

	g.mustRun(dir, "add", "erp/b@2.0.0")
	deploy = readFile(t, deployPath)
	assert.Contains(t, deploy, "      - id: erp/b\n", "add 回来的成员嵌在外壳下面")
	g.mustRun(dir, "up", "--dry-run")
}

// 移除过的成员成了新版本里某个组件的强依赖：它加回来、由编进它的外壳承载，说一声是谁要它；
// 配置从归档恢复，使用者当初写的值还在（反馈 FRR-007）。
func TestUpgradeShellBringsBackRemovedMemberThatBecameRequired(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(withConfig(comp{ID: "erp/b", Version: "1.0.0", Port: 8082}, "B_URL:"))
	g.release(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0", "erp/b@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/shell@1.0.0")
	cfg := filepath.Join(dir, "config", "erp-b.yaml")
	editFile(t, cfg, "# B_URL: ", "B_URL: http://b.internal  # ")
	g.mustRun(dir, "remove", "erp/b")

	g.release(comp{ID: "erp/a", Version: "2.0.0", Port: 8081, Requires: []string{"erp/b@2.0.0"}})
	g.release(withConfig(comp{ID: "erp/b", Version: "2.0.0", Port: 8082}, "B_URL:"))
	g.release(comp{ID: "erp/shell", Version: "2.0.0", ShellMembers: []string{"erp/a@2.0.0", "erp/b@2.0.0"}})
	preview := g.mustRun(dir, "upgrade", "erp/shell@2.0.0", "--dry-run")
	assert.Contains(t, preview.stdout, "Would restore config/erp-b.yaml from the archive")
	r := g.mustRun(dir, "upgrade", "erp/shell@2.0.0", "--yes")
	assert.Contains(t, r.stdout, "erp/b was removed from the project, but erp/a@2.0.0 requires it: it comes back, hosted by shell erp/shell")
	assert.Contains(t, r.stdout, "✅ erp/b@2.0.0 (in shell erp/shell)")
	assert.Contains(t, r.stdout, "config/erp-b.yaml restored from the archive")
	assert.NotContains(t, r.stdout, "stays out")

	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "id: erp/b\n    version: 2.0.0")
	assert.Contains(t, readFile(t, filepath.Join(dir, "deploy.yaml")), "      - id: erp/b\n")
	assert.Contains(t, readFile(t, cfg), "B_URL: http://b.internal", "配置从归档恢复")
	g.mustRun(dir, "up", "--dry-run")
}

// 全量升级要么全做、要么一个字都不写——第二个升不了，第一个也不改。
func TestUpgradeAllIsAllOrNothing(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "1.0.0", Port: 8082})
	dir := g.project()
	g.mustRun(dir, "add", "erp/a@1.0.0")
	g.mustRun(dir, "add", "erp/b@1.0.0")
	g.release(comp{ID: "erp/a", Version: "2.0.0", Port: 8081})
	// erp/b 2.0.0 依赖一个哪里都没有的组件：它的新版本解析不了
	g.release(comp{ID: "erp/b", Version: "2.0.0", Port: 8082, Requires: []string{"erp/ghost@1.0.0"}})
	before := readFile(t, filepath.Join(dir, "brickkit.yaml"))

	r := g.run(dir, "upgrade")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "erp/ghost")
	assert.Equal(t, before, readFile(t, filepath.Join(dir, "brickkit.yaml")))
}

// --dry-run 说清楚会发生什么，一个字节都不写。
func TestUpgradeDryRunWritesNothing(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "LOG_LEVEL:info", "OLD:x"))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	editFile(t, filepath.Join(dir, "config", "erp-api.yaml"), "# LOG_LEVEL: ", "LOG_LEVEL: debug  # ")
	editFile(t, filepath.Join(dir, "config", "erp-api.yaml"), "# OLD: ", "OLD: keep  # ")
	g.release(withConfig(comp{ID: "erp/api", Version: "2.0.0"}, "LOG_LEVEL:warn", "NEW:y"))
	snapshot := func() string {
		return readFile(t, filepath.Join(dir, "brickkit.yaml")) + readFile(t, filepath.Join(dir, "deploy.yaml")) +
			readFile(t, filepath.Join(dir, "config", "erp-api.yaml"))
	}
	before := snapshot()

	r := g.mustRun(dir, "upgrade", "--dry-run")
	assert.Equal(t, before, snapshot())
	for _, want := range []string{"1.0.0 → 2.0.0", "LOG_LEVEL", "OLD", "NEW"} {
		assert.Contains(t, r.stdout, want)
	}
	assert.NoDirExists(t, filepath.Join(dir, "config", ".archive"))
}

// TTY 下逐条问：m 留自己的值，n 用新默认值。
func TestUpgradeInteractiveChoice(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "LOG_LEVEL:info"))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	cfg := filepath.Join(dir, "config", "erp-api.yaml")
	editFile(t, cfg, "# LOG_LEVEL: ", "LOG_LEVEL: debug  # ")
	g.release(withConfig(comp{ID: "erp/api", Version: "2.0.0"}, "LOG_LEVEL:warn"))

	r := g.runStdin(dir, "m\n", "upgrade", "erp/api")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	text := readFile(t, cfg)
	assert.Contains(t, text, "LOG_LEVEL: debug")
	assert.Equal(t, 1, strings.Count(text, "LOG_LEVEL:"))
	g.mustRun(dir, "up", "--dry-run")
}

// 本地源的组件：仓库里升到了 1.1.0（本地仓库版本对不上时，up 的报错建议的正是这条 upgrade）——upgrade 把 brickkit.yaml 跟上。
func TestUpgradeLocalSourceToRepoVersion(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml":                     "project: shop\nsources:\n  - {name: dev, type: local, path: ./components}\ncomponents: []\n",
		"deploy.yaml":                       "target: docker\ncomponents: []\n",
		"components/erp/api/component.yaml": comp{ID: "erp/api", Version: "1.0.0"}.yamlText(),
	})
	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), dir, "add", "--local").code)
	writeTree(t, dir, map[string]string{"components/erp/api/component.yaml": comp{ID: "erp/api", Version: "1.1.0"}.yamlText()})

	r := runWithEngine(t, newFakeEngine(), dir, "upgrade", "erp/api")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "version: 1.1.0")
}

func TestUpgradeRejectsUnknownAndCompatibilityTargets(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	r := g.run(dir, "upgrade", "erp/nope")
	assert.Equal(t, clierr.ExitError, r.code)
}

// 外壳的新版本不再是外壳：kind 去掉，成员挪回顶层独立运行，up 照常。
func TestUpgradeShellThatStopsBeingAShell(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/s", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/s@1.0.0")
	g.release(comp{ID: "erp/s", Version: "2.0.0"})

	g.mustRun(dir, "upgrade", "erp/s")
	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/s\n    version: 2.0.0\n")
	assert.NotContains(t, decl, "kind: shell")
	assert.NotContains(t, readFile(t, filepath.Join(dir, "deploy.yaml")), "members")
	g.mustRun(dir, "up", "--dry-run")
}

// 不写版本时只往上走：安装源里最新的比项目里的旧，那就是"已是最新"，绝不悄悄降级。
func TestUpgradeWithoutVersionNeverDowngrades(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "2.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@2.0.0")
	editFile(t, filepath.Join(dir, "brickkit.yaml"), "version: 2.0.0", "version: 3.0.0")
	cacheManifest(t, dir, "erp/api", "3.0.0", comp{ID: "erp/api", Version: "3.0.0"}.yamlText())

	r := g.mustRun(dir, "upgrade", "erp/api")
	assert.Contains(t, r.stdout, "up to date")
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "version: 3.0.0")
}

// --dry-run 在一份临时副本上真的做一遍、核对一遍：会失败的升级在预览里就失败，项目一个字不动。
func TestUpgradeDryRunFailsLikeTheRealThing(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.release(comp{ID: "erp/api", Version: "2.0.0"})
	// deploy.local.yaml 多一个 brickkit.yaml 没有的条目：真升级改完之后的核对会拦下
	writeTree(t, dir, map[string]string{"deploy.local.yaml": "target: docker\ncomponents:\n  - id: erp/api\n  - id: erp/ghost\n"})
	before := readFile(t, filepath.Join(dir, "brickkit.yaml"))

	r := g.run(dir, "upgrade", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Equal(t, before, readFile(t, filepath.Join(dir, "brickkit.yaml")))
}

// 本地源里的组件，"最新"就是工作区里的版本——哪怕 Git 上有更新的 tag。
// 只说"都是最新"会让人以为远端没有新版本；要点名是哪几个组件、答案来自哪个本地源。
func TestUpgradeSaysWhenTheAnswerCameFromALocalSource(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/api", Version: "1.1.0"})
	dir := g.project()
	editFile(t, filepath.Join(dir, "brickkit.yaml"), "sources:\n", "sources:\n  - name: local-dev\n    type: local\n    path: ./components\n")
	g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")

	for _, args := range [][]string{{"upgrade"}, {"upgrade", "erp/api"}} {
		r := g.mustRun(dir, args...)
		assert.Contains(t, r.stdout, "erp/api@1.0.0", "%v", args)
		assert.Contains(t, r.stdout, "local source local-dev", "%v", args)
	}
}

// --dry-run 在副本上真做一遍，但报告要用"会"的语气：说"已写"、"留下了冲突块"，
// 与最后那句"一个文件都没有改"自相矛盾。
func TestUpgradeDryRunSpeaksInTheFuture(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "LOG_LEVEL:info"))
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	editFile(t, filepath.Join(dir, "config", "erp-api.yaml"), "# LOG_LEVEL: ", "LOG_LEVEL: debug  # was: ")
	g.release(withConfig(comp{ID: "erp/api", Version: "2.0.0"}, "LOG_LEVEL:warn"))

	r := g.mustRun(dir, "upgrade", "--dry-run")
	assert.NotContains(t, r.stdout, "Written:")
	assert.Contains(t, r.stdout, "Would write: brickkit.yaml, deploy.yaml")
	assert.NotContains(t, r.stdout, "contain conflict blocks")
	assert.Contains(t, r.stdout, "would get conflict blocks")
	assert.NotContains(t, r.stdout, "Config archived:")
	assert.Contains(t, r.stdout, "Would archive config: config/erp-api.yaml")
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "version: 1.0.0")
}
