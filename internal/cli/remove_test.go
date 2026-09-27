package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// removeFixture：erp/db 有两个版本（2.0.0 默认，1.0.0 为 erp/old 保留），erp/api 依赖默认版本，
// erp/old 依赖 1.0.0。每个组件都有配置骨架。
func removeFixture(t *testing.T) (*gitOrgProject, string) {
	t.Helper()
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/db", Version: "1.0.0", ConfigSchema: []string{"DB_HOST:localhost"}, Port: 5432})
	g.release(comp{ID: "erp/db", Version: "2.0.0", ConfigSchema: []string{"DB_HOST:localhost"}, Port: 5432})
	g.release(comp{ID: "erp/api", Version: "1.0.0", Requires: []string{"erp/db@2.0.0"}, ConfigSchema: []string{"LOG:info"}, Port: 8081})
	g.release(comp{ID: "erp/old", Version: "1.0.0", Requires: []string{"erp/db@1.0.0"}, Port: 8082})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.mustRun(dir, "add", "erp/old@1.0.0")
	require.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "requiredBy: [erp/old]")
	return g, dir
}

// §7.7 / §6.7：配置不删、移进 config/.archive/（带版本号），部署条目一起删。
func TestRemoveArchivesConfigAndEntries(t *testing.T) {
	g, dir := removeFixture(t)
	r := g.mustRun(dir, "remove", "erp/api")

	assert.NotContains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "erp/api")
	assert.NotContains(t, readFile(t, filepath.Join(dir, "deploy.yaml")), "erp/api")
	assert.NoFileExists(t, filepath.Join(dir, "config", "erp-api.yaml"))
	assert.FileExists(t, filepath.Join(dir, "config", ".archive", "erp-api@1.0.0.yaml"))
	assert.Contains(t, r.stdout, "config/.archive/erp-api@1.0.0.yaml")
	g.mustRun(dir, "up", "--dry-run")
}

func TestRemoveBlockedByDependent(t *testing.T) {
	g, dir := removeFixture(t)
	before := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	r := g.run(dir, "remove", "erp/db@1.0.0")
	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "erp/old@1.0.0")
	assert.Equal(t, before, readFile(t, filepath.Join(dir, "brickkit.yaml")))
}

// 附录 A20：依赖方走了，只为它保留的版本一起走（配置归档、条目删掉）。
func TestRemoveCascadesVersionKeptForDependent(t *testing.T) {
	g, dir := removeFixture(t)
	r := g.mustRun(dir, "remove", "erp/old")

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.NotContains(t, decl, "1.0.0\n    requiredBy")
	assert.Contains(t, decl, "id: erp/db\n    version: 2.0.0")
	assert.NotContains(t, readFile(t, filepath.Join(dir, "deploy.yaml")), "erp/db@1.0.0")
	assert.FileExists(t, filepath.Join(dir, "config", ".archive", "erp-db@1.0.0.yaml"))
	assert.FileExists(t, filepath.Join(dir, "config", "erp-db.yaml"), "默认版本的配置不动")
	assert.Contains(t, r.stdout, "erp/db@1.0.0")
	g.mustRun(dir, "up", "--dry-run")
}

// 用户裁定（2026-09-27）：删默认版本、只剩一个——它转正。
func TestRemovePromotesSoleRemainingVersion(t *testing.T) {
	g, dir := removeFixture(t)
	g.mustRun(dir, "remove", "erp/api")
	r := g.mustRun(dir, "remove", "erp/db@2.0.0")

	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Contains(t, decl, "id: erp/db\n    version: 1.0.0\n")
	assert.NotContains(t, decl, "requiredBy")
	deploy := readFile(t, filepath.Join(dir, "deploy.yaml"))
	assert.Contains(t, deploy, "- id: erp/db\n")
	assert.NotContains(t, deploy, "erp/db@1.0.0")
	assert.FileExists(t, filepath.Join(dir, "config", "erp-db.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "config", "erp-db@1.0.0.yaml"))
	assert.FileExists(t, filepath.Join(dir, "config", ".archive", "erp-db@2.0.0.yaml"))
	assert.Contains(t, r.stdout, "erp/db@1.0.0")
	g.mustRun(dir, "up", "--dry-run")
}

func TestRemoveNeedsVersionWhenSeveral(t *testing.T) {
	g, dir := removeFixture(t)
	r := g.run(dir, "remove", "erp/db")
	require.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "1.0.0")
	assert.Contains(t, r.stderr, "2.0.0")

	r = g.run(dir, "remove", "erp/nope")
	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "erp/nope")
}

// §8.7：删外壳，成员挪回顶层独立运行（部署字段原样），只因外壳而在的成员版本一并移除。
func TestRemoveShellReleasesMembers(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Port: 8081})
	g.release(comp{ID: "erp/b", Version: "1.0.0", Port: 8082})
	g.release(comp{ID: "erp/b", Version: "2.0.0", Port: 8082})
	g.release(comp{ID: "erp/shell", Version: "1.0.0", ShellMembers: []string{"erp/a@1.0.0", "erp/b@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/b@2.0.0")
	g.mustRun(dir, "add", "erp/shell@1.0.0")

	r := g.mustRun(dir, "remove", "erp/shell")
	decl := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	assert.NotContains(t, decl, "erp/shell")
	assert.NotContains(t, decl, "version: 1.0.0\n    requiredBy", "只为外壳保留的 erp/b 1.0.0 一并移除")
	deploy := readFile(t, filepath.Join(dir, "deploy.yaml"))
	assert.Equal(t, "target: docker\ncomponents:\n  - id: erp/b\n  - id: erp/a\n", deploy)
	assert.Contains(t, r.stdout, "erp/a")
	g.mustRun(dir, "up", "--dry-run")
}

// 源码目录里有没推送的改动：不删（找不回来），--force 才删；最后一个版本走了才动源码目录。
func TestRemoveDeletesSourceDirUnlessDirty(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")
	src := filepath.Join(dir, "components", "erp", "api")
	require.NoError(t, os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644))

	r := g.run(dir, "remove", "erp/api")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.DirExists(t, src)
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "erp/api", "拦下时三份文件都不动")

	r = g.mustRun(dir, "remove", "erp/api", "--force")
	assert.NoDirExists(t, src)
	assert.Contains(t, r.stdout, "components/erp/api/")
}

// 干净、推过的克隆照常删。
func TestRemoveDeletesCleanClone(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")
	src := filepath.Join(dir, "components", "erp", "api")
	// 检出的是 tag（分离头指针）：提交都在远端 tag 上，删了找得回来
	out, err := exec.Command("git", "-C", src, "status", "--porcelain").CombinedOutput()
	require.NoError(t, err, string(out))

	g.mustRun(dir, "remove", "erp/api")
	assert.NoDirExists(t, src)
}
