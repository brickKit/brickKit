package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// managedAgents 是带 CLI 维护区的项目 AGENTS.md：作者写的开头与结尾 + 维护区。
const managedAgents = "# shop\n\nhand-written intro\n\n<!-- brickkit:managed:begin lang=en -->\n<!-- brickkit:managed:end -->\n\nhand-written tail\n"

func agentsProject(t *testing.T, g *gitOrgProject, doc string) string {
	t.Helper()
	dir := g.project()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(doc), 0o644))
	return dir
}

func TestAddUpdatesAgentsTable(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"}, map[string]string{"BRICKKIT.md": "# erp/api\n"})
	dir := agentsProject(t, g, managedAgents)

	g.mustRun(dir, "add", "erp/api@1.0.0")
	doc := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.Regexp(t, `\| erp/api \| 1\.0\.0 \| [^|]+ \| BRICKKIT\.md \| — \|`, doc)
	assert.Contains(t, doc, "hand-written intro")
	assert.Contains(t, doc, "hand-written tail")
	assert.Contains(t, doc, "lang=en")
}

func TestRemoveUpdatesAgentsTable(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/web", Version: "1.0.0"})
	dir := agentsProject(t, g, managedAgents)
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.mustRun(dir, "add", "erp/web@1.0.0")
	require.Contains(t, readFile(t, filepath.Join(dir, "AGENTS.md")), "| erp/web |")

	g.mustRun(dir, "remove", "erp/web")
	doc := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.NotContains(t, doc, "| erp/web |")
	assert.Contains(t, doc, "| erp/api | 1.0.0 |")
}

func TestUpgradeUpdatesAgentsTable(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := agentsProject(t, g, managedAgents)
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.release(comp{ID: "erp/api", Version: "2.0.0"})

	g.mustRun(dir, "upgrade", "erp/api", "--yes")
	doc := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.Contains(t, doc, "| erp/api | 2.0.0 |")
	assert.NotContains(t, doc, "| erp/api | 1.0.0 |")
}

// 没有维护区的 AGENTS.md 是作者自己的：add / remove 之后一个字节都不变；没有 AGENTS.md 也不凭空造一份。
func TestApplierLeavesAgentsWithoutBlock(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	mine := "# my notes\n"
	dir := agentsProject(t, g, mine)
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.mustRun(dir, "remove", "erp/api")
	assert.Equal(t, mine, readFile(t, filepath.Join(dir, "AGENTS.md")))

	bare := g.project()
	g.mustRun(bare, "add", "erp/api@1.0.0")
	assert.NoFileExists(t, filepath.Join(bare, "AGENTS.md"))
}

// 本地源组件：文档在源码目录里，缓存里也有一份（取清单时就缓存了），表的"文档"一列照写。
// 表里不写"在不在本地"——那是各人机器上的事，写进提交的文件会让同事来回改写。
func TestAgentsTableForLocalComponent(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml":                     "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents: []\n",
		"deploy.yaml":                       "target: docker\ncomponents: []\n",
		"AGENTS.md":                         managedAgents,
		"components/erp/api/component.yaml": comp{ID: "erp/api", Version: "1.0.0"}.yamlText(),
		"components/erp/api/BRICKKIT.md":    "# erp/api\n",
	})
	r := runWith(t, func(o *Options) { o.Engine = newFakeEngine() }, dir, "add", "--local", "--yes")
	require.Equal(t, 0, r.code, r.stdout+r.stderr)
	doc := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.Regexp(t, `\| erp/api \| 1\.0\.0 \| [^|]+ \| BRICKKIT\.md \| — \|`, doc)
	assert.NotContains(t, doc, "components/erp/api/")
}
