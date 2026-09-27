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

// 附录 A20：还有组件依赖旧版本——旧版本以 requiredBy 留下，条目 id@1.0.0，配置文件改名成带版本号的
// （使用者的旧值还在里面），新的无版本号文件按新版本迁移。
func TestUpgradeKeepsOldVersionForDependent(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(withConfig(comp{ID: "erp/api", Version: "1.0.0"}, "DB_HOST:x"))
	g.release(comp{ID: "erp/old", Version: "1.0.0", Requires: []string{"erp/api@1.0.0"}, Port: 8081})
	dir := g.project()
	g.mustRun(dir, "add", "erp/old@1.0.0")
	editFile(t, filepath.Join(dir, "config", "erp-api.yaml"), "# DB_HOST: ", "DB_HOST: pg.internal  # ")
	g.release(withConfig(comp{ID: "erp/api", Version: "2.0.0"}, "DB_HOST:x"))

	g.mustRun(dir, "upgrade", "erp/api@2.0.0")
	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/api\n    version: 2.0.0")
	assert.Contains(t, decl, "version: 1.0.0\n    requiredBy: [erp/old]")
	assert.Contains(t, readFile(t, filepath.Join(dir, "deploy.yaml")), "- id: erp/api@1.0.0\n")
	assert.Contains(t, readFile(t, filepath.Join(dir, "config", "erp-api@1.0.0.yaml")), "DB_HOST: pg.internal")
	assert.Contains(t, readFile(t, filepath.Join(dir, "config", "erp-api.yaml")), "DB_HOST: pg.internal")
	g.mustRun(dir, "up", "--dry-run")
}

// 附录 A24：升级外壳就是换成新外壳编进的那一套成员版本；新外壳不再编进的成员挪到顶层独立运行。
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

// §12.1：全量升级要么全做、要么一个字都不写——第二个升不了，第一个也不改。
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

// TTY 下逐条问：m 留自己的值，n 用新默认值（附录 A4）。
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

// 本地源的组件：仓库里升到了 1.1.0（附录 A22 的检查会建议的 upgrade）——upgrade 把 brickkit.yaml 跟上。
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
