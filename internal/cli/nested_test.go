package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

// nestCopy 在 erp/portal 的目录里放一份 erp/api 的副本（带 component.yaml）。
func nestCopy(t *testing.T, dir string) string {
	t.Helper()
	nested := in(dir, "components", "erp", "portal", "components", "erp", "api")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "component.yaml"),
		[]byte(readFile(t, in(dir, "components", "erp", "api", "component.yaml"))), 0o644))
	return nested
}

// 组件目录里又有一份组件：找出来，说明上层有没有同一个 ID、这份在别处有没有副本。
func TestNestedCopiesAreFound(t *testing.T) {
	dir := focusFixture(t)
	nested := nestCopy(t, dir)
	p, err := project.Load(dir, project.LoadOptions{})
	require.NoError(t, err)
	copies, err := p.NestedCopies()
	require.NoError(t, err)
	require.Len(t, copies, 1)
	assert.Equal(t, "erp/api", copies[0].ID)
	assert.Equal(t, nested, copies[0].Dir)
	assert.Equal(t, "erp/portal", copies[0].Inside)
	assert.True(t, copies[0].TopHasIt)
}

// submodule 留下的空目录不算副本。
func TestEmptySubmoduleDirectoryIsNotACopy(t *testing.T) {
	dir := focusFixture(t)
	require.NoError(t, os.MkdirAll(in(dir, "components", "erp", "portal", "components", "demo", "lib"), 0o755))
	p, err := project.Load(dir, project.LoadOptions{})
	require.NoError(t, err)
	copies, err := p.NestedCopies()
	require.NoError(t, err)
	assert.Empty(t, copies)
}

// up / lint / sync 遇到嵌套副本时报 CONFIG_CONFLICT，并且一个目录都不挪。
func TestCommandsRefuseANestedCopyAndMoveNothing(t *testing.T) {
	for _, args := range [][]string{{"up", "--dry-run"}, {"lint"}, {"sync"}} {
		dir := focusFixture(t)
		nested := nestCopy(t, dir)
		r := runWithEngine(t, newFakeEngine(), dir, args...)
		assert.Equal(t, clierr.ExitError, r.code, "%v", args)
		assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.ProjectNestedCopies), "%v", args)
		// 这份副本不在 git 仓库里：挪走或删掉之前，使用者要知道它在别处没有副本
		assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.WorkspaceRiskNotGitRepo), "%v", args)
		assert.FileExists(t, filepath.Join(nested, "component.yaml"), "%v", args)
	}
}

// 嵌在项目本地源里的工作台里 add --repo：拒绝（会克隆出第二份），点名外面的项目。
func TestAddRepoInANestedWorkbenchIsRefused(t *testing.T) {
	dir := focusFixture(t)
	bench := in(dir, "components", "erp", "portal")
	writeTree(t, bench, map[string]string{
		"brickkit.yaml": "project: erp-portal\ncomponents: []\n",
		"deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	r := runIn(t, bench, "add", "erp/api@1.0.0", "--repo")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, i18n.T(msgid.CliAddRepoInNestedWorkbench, filepath.Join("..", "..", "..")))
	assert.NoDirExists(t, in(bench, "components"))
}

// 从组件目录（没有工作台）add --repo：克隆进顶层 components/，组件目录里不出现 components/。
func TestAddRepoFromAComponentDirectoryClonesIntoTheTop(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/web", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/web@1.0.0", "--repo")
	g.mustRun(in(dir, "components", "erp", "web"), "add", "erp/api@1.0.0", "--repo")
	assert.DirExists(t, in(dir, "components", "erp", "api"))
	assert.NoDirExists(t, in(dir, "components", "erp", "web", "components"))
}

// 在项目里的组件目录 init：照常补全出工作台，另外说一句——焦点运行不需要工作台，建了之后这个目录不再向上找。
func TestInitInsideAProjectsComponentAddsANote(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runIn(t, in(dir, "components", "erp", "portal"), "init", "--yes", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliInitInsideProjectNote, "erp/portal"))
}
