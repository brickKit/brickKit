// 本文件测 Client.LocalComponents：brickkit add --local 靠它知道本地源里有什么。
package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// mkdirs 在本地源根目录下建一批目录（用于造"不是组件"的干扰项）。
func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755))
	}
}

func TestLocalComponentsListsAllInSource(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "people/basic", Version: "1.0.0"})
	writeComponent(t, root, componentSpec{ID: "department/tree", Version: "2.1.0"})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)

	// 按组件 ID 排序，输出才稳定
	assert.Equal(t, "department/tree", got[0].ID)
	assert.Equal(t, "2.1.0", got[0].Version)
	assert.Equal(t, "local-dev", got[0].SourceID)
	assert.Equal(t, "people/basic", got[1].ID)
	assert.Equal(t, "1.0.0", got[1].Version)
}

// 默认约定里 local 源就指向 ./components，而 components/.archived/ 也在那底下。
// 点开头的目录一律不当作 scope——归档的组件不该被 --local 拽回来。
func TestLocalComponentsSkipsDotDirectories(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "people/basic", Version: "1.0.0"})
	writeComponent(t, filepath.Join(root, ".archived"), componentSpec{ID: "demo/hello", Version: "1.0.0"})
	writeComponent(t, filepath.Join(root, ".git"), componentSpec{ID: "demo/caller", Version: "1.0.0"})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "people/basic", got[0].ID)
}

// 目录名拼不出合法组件 ID 的（大写、下划线……）一律跳过：
// 它们进不了 brickkit.yaml，扫出来只会在后面炸。
func TestLocalComponentsSkipsInvalidComponentIDs(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "people/basic", Version: "1.0.0"})
	// 目录名非法，但里面确实有一份 component.yaml
	writeFile(t, filepath.Join(root, "People", "Basic", "component.yaml"),
		componentSpec{ID: "people/basic", Version: "9.9.9"}.yamlText())

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "people/basic", got[0].ID)
}

// 只有目录、没有 component.yaml 的不算组件（散落的源码目录、空 scope 目录）。
func TestLocalComponentsIgnoresDirsWithoutManifest(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mkdirs(t, root, "empty-scope", "demo/no-manifest", "demo/no-manifest/src")

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "people/basic", got[0].ID)
}

// component.yaml 里写的 ID 与目录对不上：以**目录**为准去取版本会取错东西，
// 这种目录直接跳过，不猜。
func TestLocalComponentsSkipsMismatchedManifest(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "people/basic", Version: "1.0.0"})
	writeFile(t, filepath.Join(root, "demo", "hello", "component.yaml"),
		componentSpec{ID: "demo/goodbye", Version: "1.0.0"}.yamlText())

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "people/basic", got[0].ID)
}

// 多个 local 源：同 ID 靠前的赢（003 §6.5），后面的不再重复列出。
func TestLocalComponentsFirstSourceWinsOnDuplicateID(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "a"), componentSpec{ID: "people/basic", Version: "1.0.0"})
	writeComponent(t, filepath.Join(layout.Root, "b"), componentSpec{ID: "people/basic", Version: "2.0.0"})
	writeComponent(t, filepath.Join(layout.Root, "b"), componentSpec{ID: "demo/hello", Version: "1.0.0"})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-a", Type: config.SourceTypeLocal, Path: "./a"},
		config.Source{ID: "local-b", Type: config.SourceTypeLocal, Path: "./b"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)

	byID := map[string]LocalComponent{}
	for _, lc := range got {
		byID[lc.ID] = lc
	}
	assert.Equal(t, "1.0.0", byID["people/basic"].Version, "靠前的 local-a 说了算")
	assert.Equal(t, "local-a", byID["people/basic"].SourceID)
	assert.Equal(t, "local-b", byID["demo/hello"].SourceID)
}

// 非 local 源不参与枚举：git 源的形状不固定，market 有成千上万个组件。
func TestLocalComponentsIgnoresNonLocalSources(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	c := newClient(t, newProject(t), cfgWithSources(
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// 空目录不是错误：就是还没有组件而已。
func TestLocalComponentsEmptyDirectory(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.MkdirAll(filepath.Join(layout.Root, "components"), 0o755))

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// 路径根本不存在是**配置错误**，必须报出来，而不是当成"这里没有组件"。
func TestLocalComponentsReportsMissingRoot(t *testing.T) {
	c := newClient(t, newProject(t), cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./nope"},
	), Options{})

	_, err := c.LocalComponents(context.Background())
	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "本地安装源路径不存在")
	assert.Contains(t, text, "local-dev")
}
