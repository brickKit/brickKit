// 本文件测 Client.LocalComponents：brickkit add --local 靠它知道本地源里有什么。
package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	require.Len(t, got.Components, 2)

	// 按组件 ID 排序，输出才稳定
	assert.Equal(t, "department/tree", got.Components[0].ID)
	assert.Equal(t, "2.1.0", got.Components[0].Version)
	assert.Equal(t, "local-dev", got.Components[0].SourceID)
	assert.Equal(t, "people/basic", got.Components[1].ID)
	assert.Equal(t, "1.0.0", got.Components[1].Version)
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
	require.Len(t, got.Components, 1)
	assert.Equal(t, "people/basic", got.Components[0].ID)
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
	require.Len(t, got.Components, 1)
	assert.Equal(t, "people/basic", got.Components[0].ID)
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
	require.Len(t, got.Components, 1)
	assert.Equal(t, "people/basic", got.Components[0].ID)
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
	require.Len(t, got.Components, 1)
	assert.Equal(t, "people/basic", got.Components[0].ID)
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
	require.Len(t, got.Components, 2)

	byID := map[string]LocalComponent{}
	for _, lc := range got.Components {
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
	assert.Empty(t, got.Components)
	assert.Empty(t, got.Problems)
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
	assert.Empty(t, got.Components)
	assert.Empty(t, got.Problems)
}

// component.yaml 里版本号非法：这是"组件坏了"，不是"没有这个组件"。
//
// 静默跳过的话，components/ 下明明躺着 10 个目录，add --local 只说"扫到 9 个"，
// 少的那个连名字都不出现——使用者会去翻安装源配置，而问题在他自己的 component.yaml 里。
func TestLocalComponentsReportsBrokenComponents(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "people/basic", Version: "1.0.0"})
	// 版本号非法
	writeFile(t, filepath.Join(root, "demo", "broken", "component.yaml"),
		strings.Replace(componentSpec{ID: "demo/broken", Version: "1.0.0"}.yamlText(),
			"version: 1.0.0", "version: latest", 1))
	// YAML 根本解析不了
	writeFile(t, filepath.Join(root, "demo", "garbage", "component.yaml"), "：: 这不是 YAML ：:")

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err, "有坏组件不该让整次枚举失败——好的那些还得能装")
	require.Len(t, got.Components, 1)
	assert.Equal(t, "people/basic", got.Components[0].ID)

	require.Len(t, got.Problems, 2, "两个坏组件都要被点名")
	byID := map[string]string{}
	for _, p := range got.Problems {
		byID[p.ID] = p.Reason
	}
	assert.Contains(t, byID, "demo/broken")
	assert.Contains(t, byID["demo/broken"], "latest", "要说清楚是哪个版本号不合法")
	assert.Contains(t, byID, "demo/garbage")
}

// 不写版本、而组件的 component.yaml 又是坏的：报错要说中要害。
//
// 从前一律回落成"组件未找到，检查安装源配置"——把人引向完全无关的方向，
// 而组件就躺在他指定的目录里，错的只是一行版本号。
func TestLatestVersionReportsBrokenManifest(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeFile(t, filepath.Join(root, "demo", "broken", "component.yaml"),
		strings.Replace(componentSpec{ID: "demo/broken", Version: "1.0.0"}.yamlText(),
			"version: 1.0.0", "version: latest", 1))

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	_, err := c.LatestVersion(context.Background(), "demo/broken")
	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "latest", "要点出那个非法的版本号")
	assert.NotContains(t, text, "该组件在所有安装源中均未找到",
		"组件就在那儿，不该说找不到")
}

// 靠前的源里组件是坏的，靠后的源里是好的：坏的那个不该挡住后面。
func TestLatestVersionFallsThroughBrokenSource(t *testing.T) {
	layout := newProject(t)
	writeFile(t, filepath.Join(layout.Root, "a", "people", "basic", "component.yaml"),
		strings.Replace(componentSpec{ID: "people/basic", Version: "1.0.0"}.yamlText(),
			"version: 1.0.0", "version: latest", 1))
	writeComponent(t, filepath.Join(layout.Root, "b"), componentSpec{ID: "people/basic", Version: "2.0.0"})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-a", Type: config.SourceTypeLocal, Path: "./a"},
		config.Source{ID: "local-b", Type: config.SourceTypeLocal, Path: "./b"},
	), Options{})

	got, err := c.LatestVersion(context.Background(), "people/basic")
	require.NoError(t, err, "坏的源不该挡住好的源")
	assert.Equal(t, "2.0.0", got.Version)
	assert.Equal(t, "local-b", got.SourceID)
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
