package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// managedDoc 是 init 写出的那种项目 BRICKKIT.md：使用者的开头 + CLI 维护区。
const managedDoc = "# shop\n\nhand-written intro\n\n<!-- brickkit:managed:begin -->\n<!-- brickkit:managed:end -->\n\nhand-written tail\n"

func docProject(t *testing.T, g *gitOrgProject, doc string) string {
	t.Helper()
	dir := g.project()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "BRICKKIT.md"), []byte(doc), 0o644))
	return dir
}

func TestAddUpdatesProjectDoc(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"}, map[string]string{"BRICKKIT.md": "# erp/api\n"})
	dir := docProject(t, g, managedDoc)

	g.mustRun(dir, "add", "erp/api@1.0.0")
	doc := readFile(t, filepath.Join(dir, "BRICKKIT.md"))
	assert.Contains(t, doc, "| erp/api | 1.0.0 | `.brickkit/manifests/erp/api/1.0.0/BRICKKIT.md` |")
	assert.Contains(t, doc, "hand-written intro")
	assert.Contains(t, doc, "hand-written tail")
}

func TestRemoveUpdatesProjectDoc(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/web", Version: "1.0.0"})
	dir := docProject(t, g, managedDoc)
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.mustRun(dir, "add", "erp/web@1.0.0")
	require.Contains(t, readFile(t, filepath.Join(dir, "BRICKKIT.md")), "| erp/web |")

	g.mustRun(dir, "remove", "erp/web")
	doc := readFile(t, filepath.Join(dir, "BRICKKIT.md"))
	assert.NotContains(t, doc, "| erp/web |")
	assert.Contains(t, doc, "| erp/api | 1.0.0 |")
}

func TestUpgradeUpdatesProjectDoc(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := docProject(t, g, managedDoc)
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.release(comp{ID: "erp/api", Version: "2.0.0"})

	g.mustRun(dir, "upgrade", "erp/api", "--yes")
	doc := readFile(t, filepath.Join(dir, "BRICKKIT.md"))
	assert.Contains(t, doc, "| erp/api | 2.0.0 |")
	assert.NotContains(t, doc, "| erp/api | 1.0.0 |")
}

// 没有维护区的 BRICKKIT.md 是使用者自己的：add / remove 之后一个字节都不变，也不会凭空造一份。
func TestApplierLeavesUnmanagedBrickkitMd(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	mine := "# my notes\n"
	dir := docProject(t, g, mine)
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.mustRun(dir, "remove", "erp/api")
	assert.Equal(t, mine, readFile(t, filepath.Join(dir, "BRICKKIT.md")))

	bare := g.project()
	g.mustRun(bare, "add", "erp/api@1.0.0")
	assert.NoFileExists(t, filepath.Join(bare, "BRICKKIT.md"))
}
