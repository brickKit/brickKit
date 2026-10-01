package project_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
)

func cacheComponent(t *testing.T, l project.Layout, id, version, manifest string, docs ...string) {
	t.Helper()
	dir := l.CachedManifestDir(id, version)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.yaml"), []byte(manifest), 0o644))
	for _, d := range docs {
		require.NoError(t, os.WriteFile(filepath.Join(dir, d), []byte("# d\n"), 0o644))
	}
}

func TestAgentsRowsFromCache(t *testing.T) {
	l := project.NewLayout(t.TempDir())
	cacheComponent(t, l, "erp/sales", "1.0.26",
		"metadata: {id: erp/sales, version: 1.0.26, description: Sales orders, repository: \"https://git.example.com/erp-sales\"}\n",
		"BRICKKIT.md", "BRICKKIT.zh.md", "BRICKKIT.zh-CN.md")
	decl := &projfile.File{Project: "p", Components: []projfile.Component{{ID: "erp/sales", Version: "1.0.26"}}}
	c := project.AgentsContent(l, decl, "en")
	require.Len(t, c.Rows, 1)
	assert.Equal(t, agentsmd.Row{ID: "erp/sales", Version: "1.0.26", Does: "Sales orders", Docs: "BRICKKIT.md +zh", Home: "https://git.example.com/erp-sales"}, c.Rows[0])
	assert.True(t, c.Project)
	assert.False(t, c.Component)
	assert.Equal(t, "p", c.Title)
}

func TestAgentsRowsKeepCellsWhenCacheMissing(t *testing.T) {
	root := t.TempDir()
	l := project.NewLayout(root)
	old := agentsmd.Render(agentsmd.Content{Lang: "en", Project: true, Rows: []agentsmd.Row{{ID: "a/b", Version: "1.0.0", Does: "kept", Docs: "BRICKKIT.md", Home: "https://h"}}})
	require.NoError(t, os.WriteFile(l.AgentsPath(), []byte("# P\n\n"+old), 0o644))
	decl := &projfile.File{Project: "p", Components: []projfile.Component{{ID: "a/b", Version: "1.0.0"}, {ID: "c/d", Version: "2.0.0"}}}
	c := project.AgentsContent(l, decl, "en")
	assert.Equal(t, "kept", c.Rows[0].Does, "a fresh clone has no cache yet: the committed cells stay")
	assert.Equal(t, "—", c.Rows[1].Does)
}

func TestWriteAgentsBlockReplacesOnlyTheBlock(t *testing.T) {
	root := t.TempDir()
	complete(t, root, "my-shop")
	l := project.NewLayout(root)
	edited := strings.Replace(readText(t, l.AgentsPath()), "# my-shop\n", "# my-shop\n\nNotes written by hand.\n", 1)
	require.NoError(t, os.WriteFile(l.AgentsPath(), []byte(edited), 0o644))
	require.NoError(t, os.WriteFile(l.DeclPath(), []byte("project: my-shop\ncomponents:\n  - id: erp/backend\n    version: 2.0.0\n"), 0o644))
	require.NoError(t, os.WriteFile(l.DeployPath(), []byte("target: docker\ncomponents:\n  - id: erp/backend\n"), 0o644))
	cacheComponent(t, l, "erp/backend", "2.0.0", "metadata: {id: erp/backend, version: 2.0.0, description: The ERP backend}\n", "BRICKKIT.md")
	decl, err := projfile.ParseFile(l.DeclPath())
	require.NoError(t, err)

	res, err := project.WriteAgentsBlock(l, decl)
	require.NoError(t, err)
	assert.True(t, res.BlockRewritten)
	got := readText(t, l.AgentsPath())
	assert.Contains(t, got, "Notes written by hand.")
	assert.Contains(t, got, "| erp/backend | 2.0.0 | The ERP backend | BRICKKIT.md | — |")
}

func TestWriteAgentsBlockLeavesFileWithoutBlock(t *testing.T) {
	root := t.TempDir()
	complete(t, root, "my-shop")
	l := project.NewLayout(root)
	mine := "# my own guide\n"
	require.NoError(t, os.WriteFile(l.AgentsPath(), []byte(mine), 0o644))
	decl, err := projfile.ParseFile(l.DeclPath())
	require.NoError(t, err)
	res, err := project.WriteAgentsBlock(l, decl)
	require.NoError(t, err)
	assert.False(t, res.BlockRewritten)
	assert.Equal(t, mine, readText(t, l.AgentsPath()))

	require.NoError(t, os.Remove(l.AgentsPath()))
	_, err = project.WriteAgentsBlock(l, decl)
	require.NoError(t, err)
	assert.NoFileExists(t, l.AgentsPath(), "add / remove / upgrade never create it: that is init's job")
}

func TestObsoleteProjectMap(t *testing.T) {
	root := t.TempDir()
	l := project.NewLayout(root)
	assert.False(t, project.ObsoleteProjectMap(l))
	require.NoError(t, os.WriteFile(filepath.Join(root, "BRICKKIT.md"), []byte("# p\n<!-- brickkit:managed:begin -->\n<!-- brickkit:managed:end -->\n"), 0o644))
	assert.True(t, project.ObsoleteProjectMap(l))
	plan, err := project.PlanComplete(l, "p")
	require.NoError(t, err)
	assert.True(t, plan.ObsoleteMap)
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"), []byte("x"), 0o644))
	assert.False(t, project.ObsoleteProjectMap(l), "in a component repository BRICKKIT.md is the component's own doc")
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// 旧版 CLI 写的缓存（只缓存了 BRICKKIT.md，不知道还有没有译本）：文档一列沿用已有的单元格，不来回改写。
func TestAgentsDocsCellKeptWhenTheCacheDoesNotKnow(t *testing.T) {
	root := t.TempDir()
	l := project.NewLayout(root)
	cacheComponent(t, l, "a/b", "1.0.0", "metadata: {id: a/b, version: 1.0.0, description: d}\n", "BRICKKIT.md") // 没有记录：旧缓存
	old := agentsmd.Render(agentsmd.Content{Lang: "en", Project: true, Rows: []agentsmd.Row{{ID: "a/b", Version: "1.0.0", Does: "d", Docs: "BRICKKIT.md +zh", Home: "—"}}})
	require.NoError(t, os.WriteFile(l.AgentsPath(), []byte("# P\n\n"+old), 0o644))
	decl := &projfile.File{Project: "p", Components: []projfile.Component{{ID: "a/b", Version: "1.0.0"}}}
	assert.Equal(t, "BRICKKIT.md +zh", project.AgentsContent(l, decl, "en").Rows[0].Docs)

	require.NoError(t, os.WriteFile(filepath.Join(l.CachedManifestDir("a/b", "1.0.0"), project.FileDocsList), []byte("BRICKKIT.md\n"), 0o644))
	assert.Equal(t, "BRICKKIT.md", project.AgentsContent(l, decl, "en").Rows[0].Docs, "a cache that knows wins")
}
