package source

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/source/gittest"
)

// 仓库缓存里已有的 tag 就是本机知道的版本：远端没了照样答得上，而且按版本排好。
func TestCachedVersionsReadsTheRepoCacheOffline(t *testing.T) {
	org := newGitOrg(t)
	remote := org.release(componentSpec{ID: "erp/api", Version: "1.10.0"})
	org.release(componentSpec{ID: "erp/api", Version: "1.2.0"})
	first, _ := org.client()
	_, err := first.Manifest(context.Background(), "erp/api", "1.2.0")
	require.NoError(t, err)

	remote.Remove()
	second, _ := org.client()
	assert.Equal(t, []string{"1.2.0", "1.10.0"}, second.CachedVersions("erp/api"))
}

// 缓存里没有这个仓库：什么都不答，也不去克隆（补全从不联网）。
func TestCachedVersionsNeverClones(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	c, _ := org.client()

	assert.Empty(t, c.CachedVersions("erp/api"))
	entries, err := os.ReadDir(org.cache)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// 子目录里的组件：tag 的 <scope>-<name>/ 前缀去掉，仓库里别的 tag 不算。
func TestCachedVersionsStripsTheMonorepoPrefix(t *testing.T) {
	mono := gittest.NewRemote(t, "platform")
	mono.Tag("9.0.0", map[string]string{"README.md": "not a component"})
	mono.TagAt("packages/erp-api", "erp-api/1.0.0", specFiles(componentSpec{ID: "erp/api", Version: "1.0.0"}))

	layout := newProject(t)
	cfg := cfgWithSources()
	cfg.Components = []projfile.Component{{ID: "erp/api", Version: "1.0.0",
		Source: &projfile.ComponentSource{Type: projfile.SourceTypeGit, Repo: mono.URL(), Path: "packages/erp-api"}}}
	c := newClient(t, layout, cfg, Options{RepoCacheDir: t.TempDir()})
	_, err := c.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)

	assert.Equal(t, []string{"1.0.0"}, c.CachedVersions("erp/api"))
}

// 本地源给它目录里那一个版本；项目的清单缓存里见过的版本也算。
func TestCachedVersionsFromLocalSourceAndManifestCache(t *testing.T) {
	src := t.TempDir()
	writeComponent(t, src, componentSpec{ID: "erp/api", Version: "2.0.0"})
	layout := newProject(t)
	writeFile(t, layout.CachedManifestPath("erp/api", "1.0.0"), "metadata: {id: erp/api, version: 1.0.0}\n")
	c := newClient(t, layout, cfgWithSources(projfile.Source{Name: "dev", Type: projfile.SourceTypeLocal, Path: src}), Options{})

	assert.Equal(t, []string{"1.0.0", "2.0.0"}, c.CachedVersions("erp/api"))
	assert.Empty(t, c.CachedVersions("erp/nothing"))
	assert.Empty(t, c.CachedVersions("../erp/api"), "an ID that is not one never becomes a path")
}
