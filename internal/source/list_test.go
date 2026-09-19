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

// 本地源是作者自己的工作副本：属性声明里拼错的键在扫描时就该听到，不必等到 publish。
// 只是警告——组件照常被列出、照常能装。
func TestLocalComponentsWarnsOnMisspelledPropertyKey(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	dir := writeComponent(t, root, componentSpec{ID: "people/basic", Version: "1.0.0"})
	path := filepath.Join(dir, "component.yaml")
	writeFile(t, path, readFile(t, path)+
		"configSchema:\n  type: object\n  properties:\n    pageSize:\n      type: integer\n      defualt: 20\n")

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Components, 1, "警告不影响装配")
	require.Len(t, got.Warnings, 1)
	assert.True(t, got.Warnings[0].Warning)
	assert.Contains(t, got.Warnings[0].Format(), "configSchema.properties.pageSize.defualt")
}

func TestLocalComponentsNoWarningsForWellFormedComponents(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "people/basic", Version: "1.0.0"})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got.Warnings)
}

const localDev = "local-dev"

func localDevConfig() *config.Config {
	return cfgWithSources(config.Source{ID: localDev, Type: config.SourceTypeLocal, Path: "./components"})
}

// 内容坏掉的文件也要列出来：那正是 lint 要报告的，不能在枚举这一步就丢掉。
func TestLocalManifestFilesIncludesBrokenFiles(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "demo/ok", Version: "1.0.0"})
	writeFile(t, filepath.Join(root, "demo", "broken", "component.yaml"), "这不是合法的 YAML: [\n")
	writeFile(t, filepath.Join(root, "demo", "noheader", "component.yaml"), "apiVersion: brickkit/v1\n")

	c := newClient(t, layout, localDevConfig(), Options{})
	got, err := c.LocalManifestFiles()
	require.NoError(t, err)

	require.Len(t, got, 3)
	assert.Equal(t, []string{"demo/broken", "demo/noheader", "demo/ok"},
		[]string{got[0].ID, got[1].ID, got[2].ID}, "ID 取目录名，按目录顺序，输出稳定")
	assert.Equal(t, localDev, got[0].SourceID)
	assert.Equal(t, filepath.Join(root, "demo", "broken", "component.yaml"), got[0].Path)
}

// 与 LocalComponents 同一套目录规则：点开头的目录、没有 component.yaml 的目录、非法组件 ID 都不算。
func TestLocalManifestFilesSkipsArchivedAndNonComponents(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "demo/ok", Version: "1.0.0"})
	writeComponent(t, filepath.Join(root, ".archived"), componentSpec{ID: "demo/old", Version: "1.0.0"})
	writeComponent(t, filepath.Join(root, ".git"), componentSpec{ID: "demo/git", Version: "1.0.0"})
	mkdirs(t, root, "demo/nofile")                                                 // 有目录、没有 component.yaml
	writeFile(t, filepath.Join(root, "Demo", "Upper", "component.yaml"), "x: 1\n") // 大写：非法组件 ID

	c := newClient(t, layout, localDevConfig(), Options{})
	got, err := c.LocalManifestFiles()
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "demo/ok", got[0].ID)
}

func TestLocalManifestFilesSkipsDisabledAndNonLocalSources(t *testing.T) {
	layout := newProject(t)
	shared := filepath.Join(layout.Root, "components")
	off := filepath.Join(layout.Root, "off")
	writeComponent(t, shared, componentSpec{ID: "demo/ok", Version: "1.0.0"})
	writeComponent(t, off, componentSpec{ID: "demo/hidden", Version: "1.0.0"})

	disabled := false
	cfg := cfgWithSources(
		config.Source{ID: localDev, Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "off", Type: config.SourceTypeLocal, Path: "./off", Enabled: &disabled},
		// 这两个地址连不上：如果这个方法碰了它们，调用会失败或卡住
		config.Source{ID: "git", Type: config.SourceTypeGit, URL: "http://127.0.0.1:1/x.git"},
		config.Source{ID: "market", Type: config.SourceTypeMarket, URL: "http://127.0.0.1:1/api/v1"},
	)
	c := newClient(t, layout, cfg, Options{})

	got, err := c.LocalManifestFiles()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "demo/ok", got[0].ID)
}

func TestLocalManifestFilesListsEverySourceInOrder(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "a"), componentSpec{ID: "demo/one", Version: "1.0.0"})
	writeComponent(t, filepath.Join(layout.Root, "b"), componentSpec{ID: "demo/two", Version: "1.0.0"})
	cfg := cfgWithSources(
		config.Source{ID: "first", Type: config.SourceTypeLocal, Path: "./a"},
		config.Source{ID: "second", Type: config.SourceTypeLocal, Path: "./b"},
	)

	got, err := newClient(t, layout, cfg, Options{}).LocalManifestFiles()
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].SourceID)
	assert.Equal(t, "demo/one", got[0].ID)
	assert.Equal(t, "second", got[1].SourceID)
	assert.Equal(t, "demo/two", got[1].ID)
}

func TestLocalManifestFilesReportsMissingRoot(t *testing.T) {
	layout := newProject(t)
	cfg := cfgWithSources(config.Source{ID: "gone", Type: config.SourceTypeLocal, Path: "./nowhere"})

	_, err := newClient(t, layout, cfg, Options{}).LocalManifestFiles()
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Message, "本地安装源路径不存在")
}

// lint 靠它保证"纯只读"：调用前后，项目目录下没有多出任何文件（尤其是 .brickkit/manifests）。
func TestLocalManifestFilesWritesNothing(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{ID: "demo/ok", Version: "1.0.0"})
	c := newClient(t, layout, localDevConfig(), Options{})

	tree := func() []string {
		var out []string
		require.NoError(t, filepath.WalkDir(layout.Root, func(p string, _ os.DirEntry, err error) error {
			require.NoError(t, err)
			out = append(out, p)
			return nil
		}))
		return out
	}
	before := tree()
	_, err := c.LocalManifestFiles()
	require.NoError(t, err)
	assert.Equal(t, before, tree())
}

// 读不动的 component.yaml：枚举只看文件在不在，读不读得动是调用方的事。
//
// LocalManifestFiles 必须照样列出它——lint 要报"读不了这份文件"，不能在枚举这一步悄悄丢掉；
// LocalComponents 则保持重构前的样子：读不动的文件不进 Components，也不进 Problems。
func TestLocalManifestFilesListsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限位不生效")
	}
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "demo/ok", Version: "1.0.0"})
	lockedPath := filepath.Join(writeComponent(t, root, componentSpec{ID: "demo/locked", Version: "1.0.0"}), "component.yaml")
	require.NoError(t, os.Chmod(lockedPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(lockedPath, 0o644) })

	c := newClient(t, layout, localDevConfig(), Options{})

	files, err := c.LocalManifestFiles()
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "demo/locked", files[0].ID)
	assert.Equal(t, lockedPath, files[0].Path)

	scan, err := c.LocalComponents(context.Background())
	require.NoError(t, err)
	require.Len(t, scan.Components, 1)
	assert.Equal(t, "demo/ok", scan.Components[0].ID)
	assert.Empty(t, scan.Problems)
}
